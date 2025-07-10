package internal

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"mime/multipart"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-pdf/fpdf"
	"github.com/google/uuid"
	"github.com/markuskjeldsen/mop-backend-api/initializers"
	"github.com/markuskjeldsen/mop-backend-api/models"
)

// Example function to update status and log the change
func UpdateVisitStatus(visitID uint, newStatusID uint, userID uint) error {
	var visit models.Visit
	if err := initializers.DB.First(&visit, visitID).Error; err != nil {
		fmt.Println(err.Error())
		return err
	}
	oldStatusID := visit.StatusID

	if oldStatusID == newStatusID {
		return errors.New("the record is already in that status code")

	}

	// Update status
	if err := initializers.DB.Model(&visit).Update("status_id", newStatusID).Error; err != nil {
		fmt.Println(err.Error())
		return err
	}

	// Log the change
	log := models.VisitStatusLog{
		VisitID:     visitID,
		OldStatusID: oldStatusID,
		NewStatusID: newStatusID,
		ChangedByID: userID,
	}
	return initializers.DB.Create(&log).Error
}

func SaveFile(c *gin.Context, file *multipart.FileHeader) (string, error) {
	// Create upload directory if not exists
	uploadDir := "uploads/visit_images"
	os.MkdirAll(uploadDir, 0755)

	// Generate unique filename
	ext := filepath.Ext(file.Filename)
	filename := fmt.Sprintf("%d_%s%s", time.Now().Unix(),
		strings.ReplaceAll(uuid.New().String(), "-", ""), ext)

	filepath := filepath.Join(uploadDir, filename)

	// Save file
	if err := c.SaveUploadedFile(file, filepath); err != nil {
		return "", err
	}

	return filepath, nil
}

func pdfwrite(pdf *fpdf.Fpdf, message string) {
	pdf.Cell(float64(len(message)*2), 10, message)
	pdf.Ln(10)
}

func PdfRepport(pdf *fpdf.Fpdf, visit models.Visit) {
	pdf.AddPage()
	pdf.AddUTF8Font("Arial", "", "./static/Arial_Unicode_MS_Regular.ttf")

	// titlen er sagsnr
	pdf.SetFont("Arial", "B", 16)
	pdf.Cell(40, 10, fmt.Sprintf("Sagsnr: %d", visit.Sagsnr))
	pdf.SetFont("Arial", "", 12)
	pdf.Ln(12)

	// og så kommer debitorerne
	pdf.Cell(float64(len("Debitorer:")*2), 10, "Debitorer: ")
	for i, debitor := range visit.Debitors {
		if i == (len(visit.Debitors) - 1) {
			pdf.Cell(float64(len(debitor.Name))*2, 10, debitor.Name)
		} else {
			pdf.Cell(float64(len(debitor.Name+" og "))*2, 10, debitor.Name+" og ")
		}
	}
	pdf.Ln(10)

	// dato tidspunkt og sted for besøget
	date := visit.VisitResponse.ActDate.Format("2006-01-02")
	pdfwrite(pdf, "Dato og tidspunkt for besøget: "+visit.VisitResponse.ActTime[:8]+" "+date)
	pdfwrite(pdf, "Besøget tog sted ved "+visit.Address)

	// til slut billederne
	pdf.AddPage()
	if len(visit.VisitResponse.Images) > 0 {
		imagepath := visit.VisitResponse.Images[0].ImagePath
		pdf.Image(imagepath, 10, 10, 0, 0, false, "", 0, "")
	}
}

func GeneratePDFVisit(visitID uint) []byte {

	var visit models.Visit
	initializers.DB.Preload("Debitors").Preload("VisitResponse").Preload("VisitResponse.Images").First(&visit, visitID)

	re := regexp.MustCompile(`[<>:"/\\|?*\s]`)
	sanitizedAddress := re.ReplaceAllString(visit.Address, "_")
	sanitizedAddress = strings.ReplaceAll(sanitizedAddress, "__", "_")
	filename := fmt.Sprintf("pdfs/visit_%d_%s.pdf", visitID, sanitizedAddress)
	os.MkdirAll("pdfs", os.ModePerm)

	pdfBuf := fpdf.New("P", "mm", "A4", "")
	pdfFile := fpdf.New("P", "mm", "A4", "")

	PdfRepport(pdfBuf, visit)
	PdfRepport(pdfFile, visit)

	var buf bytes.Buffer
	err := pdfBuf.Output(&buf)
	if err != nil {
		log.Fatal(err)
	}

	err = pdfFile.OutputFileAndClose(filename)
	if err != nil {
		log.Fatal(err)
	}

	return buf.Bytes()
}
