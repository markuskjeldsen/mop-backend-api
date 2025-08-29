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
	pdf.Cell(float64(len(message))*2.1, 10, message)
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
	pdfwrite(pdf, "Besøget var af typen: "+visit.Type.Text)
	pdfwrite(pdf, "Dato og tidspunkt for besøget: "+visit.VisitResponse.ActTime[:8]+" "+date)
	pdfwrite(pdf, "Besøget tog sted ved "+visit.Address)
	// svaret for besøget
	if visit.VisitResponse.AssetAtAddress {
		pdfwrite(pdf, "Aktivet var på addressen")
	} else if visit.VisitResponse.AssetAtWorkshop {
		pdfwrite(pdf, "Aktivet var på værksted")
	} else {
		pdfwrite(pdf, "Aktivets lokation er: "+visit.VisitResponse.AssetLocation)
	}

	if visit.VisitResponse.AssetDamaged {
		pdfwrite(pdf, "Aktivet er skadet")
	} else {
		pdfwrite(pdf, "Aktivet er ikke skadet")
	}

	if visit.VisitResponse.AssetCleaned {
		pdfwrite(pdf, "Aktivet er rent")
	} else {
		pdfwrite(pdf, "Aktivet er ikke rent")
	}

	if visit.VisitResponse.PaymentReceived {
		pdfwrite(pdf, "Betaling er modtaget")
	} else {
		pdfwrite(pdf, "Betaling er ikke modtaget")
	}
	pdfwrite(pdf, "Kommentar til aktivet: "+visit.VisitResponse.AssetComments)
	pdfwrite(pdf, "Kilometer tal: "+fmt.Sprintf("%d", visit.VisitResponse.OdometerKm))

	if visit.VisitResponse.KeysReceived {
		pdfwrite(pdf, "nøglerne er modtaget")
	} else if !visit.VisitResponse.KeysReceived {
		pdfwrite(pdf, "nøglerne er ikke modtaget")
	}

	if visit.VisitResponse.DebitorIsHome {
		if visit.VisitResponse.HasWork {
			pdfwrite(pdf, "Debitor var hjemme, "+
				"civilstatus:"+string(visit.VisitResponse.CivilStatus)+
				". Debitor har  arbejde")
		} else {
			pdfwrite(pdf, "Debitor var hjemme, "+
				"civilstatus:"+string(visit.VisitResponse.CivilStatus)+
				". Debitor har ikke arbejde")
		}
	} else {
		pdfwrite(pdf, "Debitor var ikke hjemme")
	}

	// visit.VisitResponse.PropertyType

	pdfwrite(pdf, "Bolig typen er: "+string(visit.VisitResponse.PropertyType))
	pdfwrite(pdf, "Standen af huset er: "+string(visit.VisitResponse.MaintenanceStatus))
	pdfwrite(pdf, "Skyldners ejerskabs status er: "+string(visit.VisitResponse.OwnershipStatus))

	pdfwrite(pdf, "Besøgs id: "+fmt.Sprintf("%d", visit.ID))
	pdfwrite(pdf, "Besøgssvar id: "+fmt.Sprintf("%d", visit.VisitResponse.ID))

	pdfwrite(pdf, "Lat: "+visit.VisitResponse.ActLat+" long: "+visit.VisitResponse.ActLong+" positions sikkerhed: "+visit.VisitResponse.PosAccuracy)

	if visit.VisitResponse.SFSigned {
		pdfwrite(pdf, "salgsfuldmagt er underskevet")
	}
	if visit.VisitResponse.SESigned {
		pdfwrite(pdf, "salgs-/eftergivelseaftale underskrevet")
	}

	// børn
	pdfwrite(pdf, "antal børn under 18: "+fmt.Sprintf("%d", visit.VisitResponse.ChildrenUnder18))

	if visit.VisitResponse.HasWork {
		pdfwrite(pdf, "skyldner har arbejde")
		pdfwrite(pdf, "de har stillingen: "+visit.VisitResponse.Position)
		pdfwrite(pdf, "og skyldner tjener følgende: "+fmt.Sprintf("%f", visit.VisitResponse.Salary))

	} else if !visit.VisitResponse.HasWork {
		pdfwrite(pdf, "skyldner har ikke arbejde")
	}
	pdfwrite(pdf, "skyldner får følgende i pension: "+fmt.Sprintf("%f", visit.VisitResponse.PensionPayment))
	pdfwrite(pdf, "skyldner får følgende i alt: "+fmt.Sprintf("%f", visit.VisitResponse.IncomePayment))
	pdfwrite(pdf, "skyldner har følgende a rutte med: "+fmt.Sprintf("%f", visit.VisitResponse.MonthlyDisposableAmount))

	// Write creditor information
	if visit.VisitResponse.Creditor != "" {
		pdfwrite(pdf, "Kreditor 1: "+visit.VisitResponse.Creditor)
		pdfwrite(pdf, "Beløb: "+fmt.Sprintf("%f", visit.VisitResponse.DebtAmount))
		pdfwrite(pdf, "Aftale: "+visit.VisitResponse.Settlement)
	}

	if visit.VisitResponse.Creditor2 != "" {
		pdfwrite(pdf, "Kreditor 2: "+visit.VisitResponse.Creditor2)
		pdfwrite(pdf, "Beløb: "+fmt.Sprintf("%f", visit.VisitResponse.DebtAmount2))
		pdfwrite(pdf, "Aftale: "+visit.VisitResponse.Settlement2)
	}

	if visit.VisitResponse.Creditor3 != "" {
		pdfwrite(pdf, "Kreditor 3: "+visit.VisitResponse.Creditor3)
		pdfwrite(pdf, "Beløb: "+fmt.Sprintf("%f", visit.VisitResponse.DebtAmount3))
		pdfwrite(pdf, "Aftale: "+visit.VisitResponse.Settlement3)
	}

	pdfwrite(pdf, "kommentarer: "+visit.VisitResponse.Comments)

	// til slut billederne
	for _, image := range visit.VisitResponse.Images {
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
