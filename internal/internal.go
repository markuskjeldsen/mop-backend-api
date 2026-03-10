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
	"golang.org/x/text/language"
	"golang.org/x/text/message"
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

func floatToDKKmoney(number float32) string {
	p := message.NewPrinter(language.Danish)
	return p.Sprintf("%.2f kr", number)
}
func write(pdf *fpdf.Fpdf, x, y float64, txt string) {
	pdf.SetXY(x, y)
	pdf.CellFormat(0, 5, txt, "", 0, "", false, 0, "")
}
func field(pdf *fpdf.Fpdf, label string, value string) {
	pdf.CellFormat(40, 6, label, "", 0, "", false, 0, "")
	pdf.CellFormat(80, 6, value, "", 1, "", false, 0, "")
}
func checkbox(pdf *fpdf.Fpdf, checked bool, label string) {
	boxSize := 4.0

	x, y := pdf.GetXY()

	// label
	pdf.CellFormat(15, 6, label, "", 0, "", false, 0, "")

	// draw box
	pdf.Rect(x+30, y+1, boxSize, boxSize, "D")

	// mark if checked
	if checked {
		pdf.SetXY(x+30, y+0.5)
		pdf.CellFormat(boxSize, 6, "X", "", 0, "C", false, 0, "")
	}

}
func boolToString(value bool) string {
	if value {
		return "Ja"
	} else {
		return "nej"
	}
}

func questionRow(pdf *fpdf.Fpdf, label string, answer string, details string) {

	// should sum to 90
	//
	labelW := 40.0
	answerW := 20.0
	detailW := 30.0
	x, _ := pdf.GetXY()

	pdf.SetFontStyle("B")
	pdf.CellFormat(labelW, 6, label, "", 0, "", false, 0, "")
	pdf.SetFontStyle("")
	pdf.CellFormat(answerW, 6, answer, "", 0, "", false, 0, "")
	pdf.CellFormat(detailW, 6, details, "", 1, "", false, 0, "")
	_, y := pdf.GetXY()
	pdf.SetXY(x, y)
}

var pdfnormalFontSize float64 = 10
var pdflargerFontSize float64 = 30

func pdfHeader(pdf *fpdf.Fpdf, v models.Visit) {
	// this will be the overview box or header

	//box dimensions
	box_width := float64(190)
	box_heigt := float64(50)
	box_cornerX := float64(10)
	box_cornerY := float64(20)

	// LEASING
	// |---------------------------------------------------------------------------|
	// | Sagsnr    xxx-xxx         Adresse:  hyldekærparken    besøgsid:xxxxx      |
	// | dato for besøg 2026-03-04  kl: 21:30                                      |
	// | Debitorer:                                                                |
	// |  peter hansen, tlf: xxxx xxxx   mail: peter@hansen.com  debitorId:XXXXX   |
	// |  berit hansen, tlf: xxxx xxxx   mail: berit@hansen.com  debitorId:XXXXX   |
	// | konsulent: Markus kjeldsen,   tlfnr: xxxx xxxx                            |
	// |                                                                           |
	// |---------------------------------------------------------------------------|
	// the page is 210 wide and the box is 190 wide
	//0_ 10 ---------------------------------------------------- 200_210

	pdf.SetXY(10, 10)
	pdf.SetFont("Arial", "", pdflargerFontSize)

	pdf.CellFormat(0, 10, strings.ToUpper(v.Type.Text), "", 0, "", false, 0, "")
	pdf.SetFont("Arial", "", pdfnormalFontSize)

	// first a large box
	pdf.Rect(box_cornerX, box_cornerY, box_width, box_heigt, "D")
	pdf.SetXY(10, 20)

	// case information
	pdf.CellFormat(30, 6, "Sagsnr", "", 0, "", false, 0, "")
	pdf.CellFormat(30, 6, fmt.Sprint(v.Sagsnr), "", 0, "", false, 0, "")
	pdf.CellFormat(30, 6, "Adresse", "", 0, "", false, 0, "")
	pdf.CellFormat(30, 6, v.Address, "", 0, "", false, 0, "")
	pdf.CellFormat(30, 6, "BesøgsId:", "", 0, "R", false, 0, "")
	pdf.CellFormat(40, 6, fmt.Sprint(v.ID), "", 1, "R", false, 0, "")

	// visit information
	pdf.CellFormat(30, 6, "Dato", "", 0, "", false, 0, "")
	pdf.CellFormat(30, 6, v.VisitDate.Format("2006-01-02"), "", 0, "", false, 0, "")
	pdf.CellFormat(30, 6, "Kl:", "", 0, "", false, 0, "")
	pdf.CellFormat(30, 6, v.VisitResponse.ActTime, "", 1, "", false, 0, "")

	//debitor information
	pdf.CellFormat(40, 6, "Debitorer:", "", 1, "", false, 0, "")
	for _, deb := range v.Debitors {
		phone := strings.TrimSpace(deb.Phone)
		if phone == "" {
			phone = deb.PhoneWork
		}
		AdvoproDebitor := fmt.Sprint(deb.AdvoproDebitorId)
		fmt.Println(deb.AdvoproDebitorId)

		pdf.CellFormat(5, 6, "", "", 0, "", false, 0, "")
		pdf.CellFormat(10, 6, "Navn:", "", 0, "", false, 0, "")
		pdf.CellFormat(50, 6, deb.Name, "", 0, "", false, 0, "")
		pdf.CellFormat(4, 6, "tlf:", "", 0, "", false, 0, "")
		pdf.CellFormat(25, 6, phone, "", 0, "", false, 0, "")
		pdf.CellFormat(8, 6, "mail:", "", 0, "", false, 0, "")
		pdf.CellFormat(50, 6, deb.Email, "", 0, "", false, 0, "")
		pdf.CellFormat(15, 6, "debitorId:", "", 0, "", false, 0, "")
		pdf.CellFormat(20, 6, AdvoproDebitor, "", 1, "R", false, 0, "")
	}
	//pdf.CellFormat(0.5, 6, "", "", 0, "", false, 0, "")
	checkbox(pdf, true, "Debitor hjemme")
	pdf.CellFormat(5, 6, "", "", 1, "", false, 0, "")
	// time spent
	duration := time.Duration(v.VisitResponse.Duration) * time.Millisecond

	pdf.CellFormat(20, 6, "tidsforbrug", "", 0, "", false, 0, "")
	pdf.CellFormat(40, 6, duration.String(), "", 1, "", false, 0, "")

	// worker information
	pdf.SetXY(box_cornerX, box_cornerY+box_heigt-5)
	pdf.CellFormat(20, 6, "Konsulent:", "", 0, "", false, 0, "")
	pdf.CellFormat(40, 6, v.User.Name, "", 0, "", false, 0, "")
	pdf.CellFormat(20, 6, "tlfnr:", "", 0, "", false, 0, "")
	pdf.CellFormat(20, 6, v.User.Phone, "", 0, "", false, 0, "")
}

func pdfBody(pdf *fpdf.Fpdf, v models.Visit) {
	// -----------------------------------------
	// |HEADER already prefilled               |
	// -----------------------------------------
	//
	// CAR                             life satus
	// ----------------------------    -------------------------
	// | Q?       A!     details  |    | Q?       A!    details|
	// |Destryed?  YES            |    |Civilstatus married    |
	// |                          |    |kids u/18 home  3      |
	// |                          |    |kids u/18 nothome 5    |
	// |                          |    |childsupport   500kr/md|
	// |                          |    |work?      yes   janitor|
	// |                          |    |work income 1000kr/md  |
	// |                          |    |off.ydelser  1000kr/md |
	// |                          |    |totaludbetalt 2000kr/md|
	// |                          |    |rådighedsbeløb 200kr/md|
	// |                          |    |house?                 |
	// |                          |    |owneship of home?      |
	// ----------------------------    ------------------------
	//

	startY := 90.0

	leftX := 10.0
	rightX := 110.0

	boxWidth := 90.0
	boxHeight := 110.0

	// left box (CAR)
	pdf.Rect(leftX, startY, boxWidth, boxHeight, "D")

	// right box (LIFE STATUS)
	pdf.Rect(rightX, startY, boxWidth, boxHeight, "D")

	pdf.SetXY(leftX, startY-6)
	pdf.SetFont("Arial", "B", 11)
	pdf.CellFormat(boxWidth, 6, "BIL", "", 0, "L", false, 0, "")

	pdf.SetXY(rightX, startY-6)
	pdf.CellFormat(boxWidth, 6, "LIVSSITUATION", "", 0, "L", false, 0, "")

	pdf.SetFont("Arial", "", 9)

	paddingX := 4.0
	paddingY := 6.0
	pdf.SetXY(leftX+paddingX, startY+paddingY)

	y := startY + 5

	pdf.SetXY(leftX, y)
	questionRow(pdf, "Destroyed?", "YES", "")
	questionRow(pdf, "Received keys", "YES", "")

	// right side
	pdf.SetXY(rightX+paddingX, startY+paddingY)

	questionRow(pdf, "Civilstatus", "Married", "")
	questionRow(pdf, "Kids u/18 home", fmt.Sprint(v.VisitResponse.ChildrenUnder18), "")
	questionRow(pdf, "Kids u/18 not home", fmt.Sprint(v.VisitResponse.ChildrenOver18), "")
	questionRow(pdf, "Child support", "", fmt.Sprint(v.VisitResponse.ChildSupport))
	questionRow(pdf, "Work?", boolToString(v.VisitResponse.HasWork), v.VisitResponse.Position)
	questionRow(pdf, "Work income", "", floatToDKKmoney(v.VisitResponse.Salary))
	questionRow(pdf, "Off. ydelser", "", floatToDKKmoney(v.VisitResponse.IncomePayment))
	questionRow(pdf, "Total udbetalt", "", floatToDKKmoney(v.VisitResponse.Salary+v.VisitResponse.IncomePayment))
	questionRow(pdf, "Rådighedsbeløb", "", floatToDKKmoney(v.VisitResponse.MonthlyDisposableAmount))
	questionRow(pdf, "House?", string(v.VisitResponse.PropertyType), string(v.VisitResponse.MaintenanceStatus))
	questionRow(pdf, "Ownership", "", v.VisitResponse.OwnershipStatus)
}

func pdfGenerate(pdf *fpdf.Fpdf, v models.Visit) {
	pdf.SetAutoPageBreak(false, 15)
	pdf.AddUTF8Font("Arial", "", "./static/Arial_Unicode_MS_Regular.ttf")
	pdf.SetFont("Arial", "", pdfnormalFontSize)

	pdf.AddPage()

	//tpl := gofpdi.ImportPage(pdf, "./static/Besøgsbrev bilbesøg.pdf", 1, "/MediaBox")
	//gofpdi.UseImportedTemplate(pdf, tpl, 0, 0, 210, 0)

	// Now position your fields on top, same as with the image approach
	// helper functions

	pdfHeader(pdf, v)
	pdfBody(pdf, v)

	pdf.AddPage()
	pdfwrite(pdf, "kommentarer: "+v.VisitResponse.Comments)

	// til slut billederne
	for _, image := range v.VisitResponse.Images {
		pdf.AddPage()
		addImageFit(pdf, image.ImagePath)
	}

}

func PdfReport(pdf *fpdf.Fpdf, v models.Visit) {
	pdf.SetAutoPageBreak(false, 15)
	pdf.AddUTF8Font("Arial", "", "./static/Arial_Unicode_MS_Regular.ttf")
	pdf.SetFont("Arial", "", 11)

	pdf.AddPage()

	//tpl := gofpdi.ImportPage(pdf, "./static/Besøgsbrev bilbesøg.pdf", 1, "/MediaBox")
	//gofpdi.UseImportedTemplate(pdf, tpl, 0, 0, 210, 0)

	// Now position your fields on top, same as with the image approach
	// helper functions
	write := func(x, y float64, txt string) {
		pdf.SetXY(x, y)
		pdf.CellFormat(0, 5, txt, "", 0, "", false, 0, "")
	}
	Field := func(label, value string) {
		pdf.CellFormat(40, 6, label, "", 0, "", false, 0, "")
		pdf.CellFormat(80, 6, value, "", 1, "", false, 0, "")
	}

	Field("Sagsnr", fmt.Sprint(v.Sagsnr))
	Field("Dato", v.VisitDate.Format("2006-01-02"))

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
	write(108, 56, "MARKUS")

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

	if true || v.VisitResponse.PaymentReceived {
		//  PaymentReceived bool   `json:"payment_received"`
		//	PaymentReceivedAmount float32 `json:"payment_received_amount"`

		write(48, 82, "X")
		write(59, 82, "N")
		write(54, 86, floatToDKKmoney(v.VisitResponse.PaymentReceivedAmount))

	}

	if true || v.VisitResponse.AssetAtAddress {
		write(82, 82, "Y")
		write(92.5, 82, "N")
	}

	if true || v.VisitResponse.AssetDelivered {
		write(112, 82, "Y")
		write(122, 82, "N")
	}

	if true || v.VisitResponse.KeysGiven {
		write(141, 82, "Y")
		write(152, 82, "N")
	}

	if true || v.VisitResponse.AssetDamaged {
		write(20, 102, "X")
		write(30, 102, "X")
	}

	if true || v.VisitResponse.SFSigned {
		write(148, 102.2, "X")
		write(159, 102.2, "X")
	}

	if true || v.VisitResponse.SESigned {
		write(145, 122.5, "X")
		write(156, 122.5, "X")
	}

	switch v.VisitResponse.CivilStatus {
	case models.Married:
		write(20, 135, "X")
	case models.Cohabiting:
		write(20, 141, "X")
	case models.Single:
		write(20, 147, "X")
	}
	// for testing
	/*
		write(20, 135, "M")
		write(20, 141, "C")
		write(20, 147, "S")
	*/
	write(80, 135, fmt.Sprint(v.VisitResponse.ChildrenUnder18))

	// TODO: arbejde?

	if v.VisitResponse.HasWork {
		write(91, 135, "X")
		write(95, 142, v.VisitResponse.Position)
		write(105, 147, floatToDKKmoney(v.VisitResponse.Salary)) // udbetalt månnedligt
	} else {
		write(135, 135, "X")
	}

	if true || v.VisitResponse.IncomePayment > 10 {
		write(150, 147, floatToDKKmoney(v.VisitResponse.IncomePayment+1000)) // udbetalt månedligt fra offentlige ydelser
	}

	if true || v.VisitResponse.HasWork || (v.VisitResponse.IncomePayment > 10) {
		write(70, 153, floatToDKKmoney(v.VisitResponse.MonthlyDisposableAmount)) // månedligt rådigheds beløb
	}

	// er der anden gæld? og afvikles der i så fald på den?
	if v.VisitResponse.DebtAmount > 0 {
		write(32, 160, floatToDKKmoney(v.VisitResponse.DebtAmount))
		write(95, 161, v.VisitResponse.Creditor)
	}
	if v.VisitResponse.DebtAmount2 > 0 {
		write(32, 167, floatToDKKmoney(v.VisitResponse.DebtAmount2))
		write(95, 167, v.VisitResponse.Creditor2)
	}
	if v.VisitResponse.DebtAmount3 > 0 {
		write(32, 173, floatToDKKmoney(v.VisitResponse.DebtAmount3))
		write(95, 173, v.VisitResponse.Creditor3)
	}

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
	write(102, 191.8, "D") // models.Deteriorated

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
	initializers.DB.Preload("Type").Preload("Debitors").Preload("VisitResponse").Preload("VisitResponse.Images").Preload("User").First(&visit, visitID)

	re := regexp.MustCompile(`[<>:"/\\|?*\s]`)
	sanitizedAddress := re.ReplaceAllString(visit.Address, "_")
	sanitizedAddress = strings.ReplaceAll(sanitizedAddress, "__", "_")
	filename := fmt.Sprintf("pdfs/visit_%d_%s.pdf", visitID, sanitizedAddress)
	os.MkdirAll("pdfs", os.ModePerm)

	pdfBuf := fpdf.New("P", "mm", "A4", "")
	pdfFile := fpdf.New("P", "mm", "A4", "")

	//PdfReport(pdfBuf, visit)
	//PdfReport(pdfFile, visit)
	pdfGenerate(pdfBuf, visit)
	pdfGenerate(pdfFile, visit)

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
