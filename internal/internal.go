package internal

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"mime/multipart"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/markuskjeldsen/mop-backend-api/initializers"
	"github.com/markuskjeldsen/mop-backend-api/models"
	fpdf "github.com/phpdave11/gofpdf"
	"github.com/phpdave11/gofpdf/contrib/gofpdi"
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

func addImageFit(pdf *fpdf.Fpdf, path string) {
	// Page size, margins, cursor
	pw, ph := pdf.GetPageSize()
	lm, _, rm, _ := pdf.GetMargins()
	y := pdf.GetY()

	// Bottom margin (via auto page break)

	_, bm := pdf.GetAutoPageBreak() //ab

	maxW := pw - lm - rm
	maxH := ph - bm - y
	if maxH <= 0 {
		pdf.AddPage()
		y = pdf.GetY()
		maxH = ph - bm - y
	}

	// Image natural size (respect DPI)
	info := pdf.RegisterImageOptions(path, fpdf.ImageOptions{ReadDpi: true})
	iw, ih := info.Extent()

	scale := math.Min(math.Min(maxW/iw, maxH/ih), 1.0)
	w, h := iw*scale, ih*scale

	// Draw
	pdf.ImageOptions(path, lm, y, w, h, false, fpdf.ImageOptions{ReadDpi: true}, 0, "")
	pdf.SetY(y + h + 2) // move cursor below image
}

func pdfwrite(pdf *fpdf.Fpdf, message string) {
	const desiredWidth = 180.0 // Adjust this value based on your PDF layout (e.g., page width minus margins)
	const lineHeight = 10.0    // Height of each line of text

	// Use MultiCell for automatic text wrapping.
	// Parameters: width, line_height, text, border_flags (0 for no border), align ("L" for left), fill (false for no fill)
	pdf.MultiCell(desiredWidth, lineHeight, message, "0", "L", false)

	// MultiCell automatically advances the Y position to the line after the last text line.
	// pdf.Ln() is not usually needed directly after MultiCell unless you want extra spacing.
}

func PdfReport(pdf *fpdf.Fpdf, v models.Visit) {
	pdf.SetAutoPageBreak(false, 15)
	pdf.AddUTF8Font("Arial", "", "./static/Arial_Unicode_MS_Regular.ttf")
	pdf.SetFont("Arial", "", 11)

	tpl := gofpdi.ImportPage(pdf, "./static/blanco.pdf", 1, "/MediaBox")
	pdf.AddPage()

	gofpdi.UseImportedTemplate(pdf, tpl, 0, 0, 210, 0)

	// Now position your fields on top, same as with the image approach
	write := func(x, y float64, txt string) {
		pdf.SetXY(x, y)
		pdf.CellFormat(0, 5, txt, "", 0, "", false, 0, "")
	}

	for i, deb := range v.Debitors {
		write(30, float64(53+i*5), deb.Name)
	}

	for i, deb := range v.Debitors {
		write(100, float64(50+i*5), deb.SSN)
	}

	if v.VisitResponse.DebitorIsHome {
		write(20, 80, "X")
	} else {
		write(20, 90, "X")
	}

	switch v.VisitResponse.CivilStatus {
	case models.Cohabiting:
		write(20, 200, "X")
	case models.Married:
		write(20, 230, "X")
	case models.Single:
		write(20, 250, "X")

	}

	pdf.AddPage()
	pdfwrite(pdf, "kommentarer: "+v.VisitResponse.Comments)

	// til slut billederne
	for _, image := range v.VisitResponse.Images {
		pdf.AddPage()
		addImageFit(pdf, image.ImagePath)
	}

}

func GeneratePDFVisit(visitID uint) []byte {

	var visit models.Visit
	initializers.DB.Preload("Type").Preload("Debitors").Preload("VisitResponse").Preload("VisitResponse.Images").First(&visit, visitID)

	re := regexp.MustCompile(`[<>:"/\\|?*\s]`)
	sanitizedAddress := re.ReplaceAllString(visit.Address, "_")
	sanitizedAddress = strings.ReplaceAll(sanitizedAddress, "__", "_")
	filename := fmt.Sprintf("pdfs/visit_%d_%s.pdf", visitID, sanitizedAddress)
	os.MkdirAll("pdfs", os.ModePerm)

	pdfBuf := fpdf.New("P", "mm", "A4", "")
	pdfFile := fpdf.New("P", "mm", "A4", "")

	PdfReport(pdfBuf, visit)
	PdfReport(pdfFile, visit)

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

func LogUserDelete(actinguser models.User, targetuser models.User) error {
	prevJSON, err := json.Marshal(targetuser)
	if err != nil {
		return err
	}

	activity := models.ActivityLog{
		ActingUserID: actinguser.ID,
		TargetID:     targetuser.ID,
		ActionType:   "DELETE USER",
		PrevVal:      prevJSON,
	}
	initializers.DB.Create(&activity)
	return nil
}

func LogUserCreate(actinguser models.User, targetuser models.User) error {
	currJSON, err := json.Marshal(targetuser)
	if err != nil {
		return err
	}

	activity := models.ActivityLog{
		ActingUserID: actinguser.ID,
		TargetID:     targetuser.ID,
		CurrentVal:   currJSON,
		ActionType:   "CREATE USER",
	}

	initializers.DB.Create(&activity)
	return nil
}

func LogUserPatch(actinguser models.User, targetuserPrev models.User, targetuserCurrent models.User) error {
	prevJSON, err := json.Marshal(targetuserPrev)
	if err != nil {
		return err
	}
	currJSON, err := json.Marshal(targetuserCurrent)
	if err != nil {
		return err
	}

	activity := models.ActivityLog{
		ActingUserID: actinguser.ID,
		TargetID:     targetuserPrev.ID,

		PrevVal:    prevJSON,
		CurrentVal: currJSON,

		ActionType: "PATCH USER",
	}

	initializers.DB.Create(&activity)
	return nil
}

func LogVisitDelete(actinguser models.User, targetVisit models.Visit) error {
	prevJSON, err := json.Marshal(targetVisit)
	if err != nil {
		return err
	}

	activity := models.ActivityLog{
		ActingUserID: actinguser.ID,
		TargetID:     targetVisit.ID,
		ActionType:   "DELETE VISIT",
		PrevVal:      prevJSON,
	}

	initializers.DB.Create(&activity)
	return nil
}

func LogVisitCreate(actinguser models.User, targetVisit models.Visit) error {
	currJSON, err := json.Marshal(targetVisit)
	if err != nil {
		return err
	}

	activity := models.ActivityLog{
		ActingUserID: actinguser.ID,
		TargetID:     targetVisit.ID,
		CurrentVal:   currJSON,
		ActionType:   "CREATE VISIT",
	}

	initializers.DB.Create(&activity)
	return nil
}
