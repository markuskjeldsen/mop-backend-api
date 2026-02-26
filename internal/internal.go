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

	tpl := gofpdi.ImportPage(pdf, "./static/Besøgsbrev bilbesøg.pdf", 1, "/MediaBox")
	pdf.AddPage()

	gofpdi.UseImportedTemplate(pdf, tpl, 0, 0, 210, 0)

	// Now position your fields on top, same as with the image approach
	write := func(x, y float64, txt string) {
		pdf.SetXY(x, y)
		pdf.CellFormat(0, 5, txt, "", 0, "", false, 0, "")
	}

	write(175, 24, fmt.Sprint(v.Sagsnr))

	for i, deb := range v.Debitors {
		write(31, float64(25+i*5), deb.Name)
	}

	for i, deb := range v.Debitors {
		write(130, float64(25+i*5), deb.SSN)
	}

	write(31, 35, v.Address)
	write(168, 36, v.Debitors[0].Phone)
	write(168, 41, v.Debitors[0].Email)

	write(47, 58, v.VisitDate.Format("2006-01-02")) // YYYY-MM-DD
	write(105, 50, v.User.Name)
	write(108, 58, "MARKUS")

	// ACTUAL DATA

	/*
		DebitorIsHome   bool   `json:"debitor_is_home"`
		PaymentReceived bool   `json:"payment_received"`

		AssetAtAddress  bool   `json:"asset_at_address"`
		AssetAtWorkshop bool   `json:"asset_at_workshop"`
		AssetLocation   string `json:"asset_location"`

		AssetComments string `json:"asset_comments"`
		AssetCleaned    bool   `json:"asset_cleaned"`

		AssetDelivered bool `json:"asset_delivered"`
		AssetDamaged   bool `json:"asset_damaged"` // if then discribe
		KeysGiven      bool `json:"keys_given"`
		KeysReceived   bool `json:"keys_received"`

		OdometerKm uint `json:"odometer_km"`
	*/

	if v.VisitResponse.DebitorIsHome {
		write(20, 82, "X")
	} else {
		write(30, 82, "X")
	}

	write(25, 87, v.VisitResponse.ActTime)

	/*
		switch v.VisitResponse.CivilStatus {
		case models.Married:
			write(20, 119.8, "X")
		case models.Cohabiting:
			write(20, 125.7, "X")
		case models.Single:
			write(20, 137.5, "X")
		}
		// for testing
	*/
	write(20, 135, "M")
	write(20, 141, "C")
	write(20, 147, "S")

	write(80, 135, fmt.Sprint(v.VisitResponse.ChildrenUnder18))

	// TODO: arbejde?
	//v.VisitResponse.HasWork
	//v.VisitResponse.Position

	// TODO: udbetalt månnedligt
	// TODO: månedligt rådigheds beløb

	//v.VisitResponse.IncomePayment
	//v.VisitResponse.MonthlyDisposableAmount

	// TODO: Gæld i alt
	// TODO: afvikles der på gælden?
	//v.VisitResponse.DebtAmount
	//v.VisitResponse.Creditor

	//v.VisitResponse.DebtAmount2
	//v.VisitResponse.Creditor2

	//v.VisitResponse.DebtAmount3
	//v.VisitResponse.Creditor3

	/*
		switch v.VisitResponse.PropertyType {
		case models.PropertyFreestandingHouse:
			write(20, 184.5, "X")
		case models.PropertyTownhouse: // byhus
			write(51, 184.5, "X")
		case models.PropertyTerracedHouse: //rækkehus
			write(76, 184.5, "X")
		case models.PropertySummerHouse:
			write(20, 190, "X")
		case models.PropertyGardenColony:
			write(51, 190, "X")
		case models.PropertyApartment:
			write(76, 190, "X")
		}
	*/

	write(20, 184.7, "FS") // models.PropertyFreestandingHouse
	write(48, 184.7, "BY") // models.PropertyTownhouse
	write(72, 184.7, "TH") // models.PropertyTerracedHouse

	write(20, 191.8, "SH") // models.PropertySummerHouse
	write(48, 191.8, "GC") // models.PropertyGardenColony
	write(72, 191.8, "L")  // models.PropertyApartment
	/*
		switch v.VisitResponse.MaintenanceStatus {
		case models.WellMaintained:
			write(109, 184.5, "X")
		case models.Deteriorated:
			write(109, 190, "X")
		}
	*/

	write(101, 184.7, "M") // models.WellMaintained
	write(95, 191.8, "D")  // models.Deteriorated

	/*
		switch v.VisitResponse.OwnershipStatus {
		case "Ejer":
			write(20, 136, "X") // need to implement a models.propertyOwns or something
		case "LejerBolig":
			write(43, 204, "X")
		}
	*/
	write(20, 206, "O") // owner
	write(42, 206, "R") // renter
	write(66, 206, "P") // Part andelsbolig

	write(20, 212, "A") // alone
	write(49, 212, "W") // with Others
	write(87, 212, "S") // spouse

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
