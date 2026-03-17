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

// Helper to handle optional numbers (uint)
func optionalUintToStr(val *uint) string {
	if val == nil {
		return "-"
	}
	return fmt.Sprint(*val)
}

// Helper to handle optional money (float32)
func optionalMoneyToStr(val *float32) string {
	if val == nil {
		return "-"
	}
	p := message.NewPrinter(language.Danish)
	return p.Sprintf("%.2f kr", *val)
}

// Optional string formatter (just checks for empty string)
func formatStr(val string) string {
	if val == "" {
		return "-"
	}
	return val
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

func optionalBoolToStr(val *bool) string {
	if val == nil {
		return "-" // Or "Ingen data"
	}
	if *val {
		return "JA"
	}
	return "NEJ"
}

func boolToString(value bool) string {
	if value {
		return "JA"
	} else {
		return "NEJ"
	}
}

func questionRow(pdf *fpdf.Fpdf, label string, answer string, details string) {

	// should sum to 90
	//
	labelW := 42.0
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

func civilStatusToString(status *models.CivilStatus) string {
	// Check if it's nil OR if it's pointing to an empty string
	if status == nil || *status == "" {
		return "-"
	}

	switch *status {
	case models.Married:
		return "Gift"
	case models.Cohabiting:
		return "Samboende"
	case models.Single:
		return "Enlig"
	}

	// If it has a value, but it's not one of the 3 recognized constants
	return "Angivet forkert"
}

func optionalpropertyTypeToString(propertytype *models.PropertyType) string {
	if propertytype == nil || *propertytype == "" {
		return "-"
	}

	return string(*propertytype)
}

func optionalMaintenanceToString(maintain_status *models.MaintenanceStatus) string {
	if maintain_status == nil || *maintain_status == "" {
		return "-"
	}

	return string(*maintain_status)
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
	pdf.SetFont("Roboto", "", pdflargerFontSize)

	pdf.CellFormat(0, 10, strings.ToUpper(v.Type.Text), "", 0, "", false, 0, "")
	pdf.SetFont("Roboto", "", pdfnormalFontSize)

	// first a large box
	pdf.Rect(box_cornerX, box_cornerY, box_width, box_heigt, "D")
	pdf.SetXY(10, 20)

	// case information
	pdf.CellFormat(30, 6, "Sagsnr:", "", 0, "", false, 0, "")
	pdf.CellFormat(30, 6, fmt.Sprint(v.Sagsnr), "", 0, "", false, 0, "")
	pdf.CellFormat(20, 6, "Adresse:", "", 0, "", false, 0, "")
	pdf.CellFormat(40, 6, v.Address, "", 0, "", false, 0, "")
	pdf.CellFormat(30, 6, "BesøgsId:", "", 0, "R", false, 0, "")
	pdf.CellFormat(40, 6, fmt.Sprint(v.ID), "", 1, "R", false, 0, "")

	// visit information
	pdf.CellFormat(30, 6, "Dato", "", 0, "", false, 0, "")
	pdf.CellFormat(30, 6, v.VisitDate.Format("2006-01-02"), "", 0, "", false, 0, "")
	pdf.CellFormat(20, 6, "Kl:", "", 0, "", false, 0, "")
	pdf.CellFormat(40, 6, v.VisitResponse.ActTime, "", 1, "", false, 0, "")

	pdf.CellFormat(40, 6, "", "", 1, "", false, 0, "")
	//debitor information
	pdf.CellFormat(40, 6, "Debitorer:", "", 1, "", false, 0, "")
	for _, deb := range v.Debitors {
		phone := strings.TrimSpace(deb.Phone)
		if phone == "" {
			phone = deb.PhoneWork
		}
		AdvoproDebitor := fmt.Sprint(deb.AdvoproDebitorId)

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
	// time spent
	duration := time.Duration(v.VisitResponse.Duration) * time.Millisecond

	// worker information
	pdf.SetXY(box_cornerX, box_cornerY+box_heigt-5)
	pdf.CellFormat(20, 6, "Konsulent:", "", 0, "", false, 0, "")
	pdf.CellFormat(40, 6, v.User.Name, "", 0, "", false, 0, "")
	pdf.CellFormat(10, 6, "tlf:", "", 0, "", false, 0, "")
	pdf.CellFormat(40, 6, v.User.Phone, "", 0, "", false, 0, "")
	pdf.CellFormat(20, 6, "tidsforbrug:", "", 0, "", false, 0, "")
	pdf.CellFormat(40, 6, duration.String(), "", 1, "R", false, 0, "")

}

func fillLifeBox(pdf *fpdf.Fpdf, v models.Visit, LifeBoxX float64, LifeBoxY float64, LifeBoxWidth float64) {
	pdf.SetXY(LifeBoxX, LifeBoxY-6)
	pdf.SetFont("Roboto", "B", pdfnormalFontSize+3)
	pdf.CellFormat(LifeBoxWidth, 6, "LIVSSITUATION", "", 0, "L", false, 0, "")
	pdf.SetFont("Roboto", "", pdfnormalFontSize-1)

	paddingX := 4.0
	paddingY := 6.0
	// right side
	pdf.SetXY(LifeBoxX+paddingX, LifeBoxY+paddingY)

	questionRow(pdf, "Debitor hjemme", optionalBoolToStr(v.VisitResponse.DebitorIsHome), "")
	questionRow(pdf, "Civilstatus", civilStatusToString(v.VisitResponse.CivilStatus), "")
	questionRow(pdf, "Kids u/18 hjemme", optionalUintToStr(v.VisitResponse.ChildrenUnder18), "")
	questionRow(pdf, "Kids u/18 udeboende", optionalUintToStr(v.VisitResponse.ChildrenOver18), "")

	// Complex logic for child support
	childSupportDetails := ""
	if v.VisitResponse.ChildSupport != nil {
		childSupportDetails = optionalMoneyToStr(v.VisitResponse.ChildSupport)
	}

	// Assuming ChildSupport existence depends on if the float is > 0 or if the pointer is just present
	hasChildSupportStr := "-"
	if v.VisitResponse.ChildSupport != nil {
		if *v.VisitResponse.ChildSupport > 0 {
			hasChildSupportStr = "JA"
		} else {
			hasChildSupportStr = "NEJ"
		}
	}
	questionRow(pdf, "Børnepenge", hasChildSupportStr, childSupportDetails)

	salary := ""
	if v.VisitResponse.HasWork != nil {
		if *v.VisitResponse.HasWork {
			salary = optionalMoneyToStr(v.VisitResponse.Salary)
		}
	}

	questionRow(pdf, "Arbejde", optionalBoolToStr(v.VisitResponse.HasWork), v.VisitResponse.Position)
	questionRow(pdf, "Arbejde inkosmt", "", salary)
	questionRow(pdf, "Off. ydelser", "", optionalMoneyToStr(v.VisitResponse.IncomePayment))

	totalStr := "-"
	if v.VisitResponse.Salary != nil && v.VisitResponse.IncomePayment != nil {
		total := *v.VisitResponse.Salary + *v.VisitResponse.IncomePayment
		totalStr = optionalMoneyToStr(&total)
	}

	questionRow(pdf, "Total udbetalt", "", totalStr)
	questionRow(pdf, "Rådighedsbeløb", "", optionalMoneyToStr(v.VisitResponse.MonthlyDisposableAmount))

	questionRow(pdf, "Hus?", optionalpropertyTypeToString(v.VisitResponse.PropertyType), optionalMaintenanceToString((v.VisitResponse.MaintenanceStatus)))

	questionRow(pdf, "Ejerskab?", "", v.VisitResponse.OwnershipStatus)
}

func fillCarBox(pdf *fpdf.Fpdf, v models.Visit, CarBoxX float64, CarBoxY float64, CarWidth float64) {

	pdf.SetXY(CarBoxX, CarBoxY-6)
	pdf.SetFont("Roboto", "B", pdfnormalFontSize+3)
	pdf.CellFormat(CarWidth, 6, "BIL", "", 0, "L", false, 0, "")
	pdf.SetFont("Roboto", "", pdfnormalFontSize-1)

	paddingX := 4.0
	paddingY := 6.0
	pdf.SetXY(CarBoxX+paddingX, CarBoxY+paddingY)

	y := CarBoxY + 5

	pdf.SetXY(CarBoxX, y)

	questionRow(pdf, "Aktiv Skadet?", optionalBoolToStr(v.VisitResponse.AssetDamaged), "")
	questionRow(pdf, "Received keys", optionalBoolToStr(v.VisitResponse.KeysReceived), "")
	questionRow(pdf, "Er den på adressen?", optionalBoolToStr(v.VisitResponse.AssetAtAddress), "")
	questionRow(pdf, "Er den ren?", optionalBoolToStr(v.VisitResponse.AssetCleaned), "")
	questionRow(pdf, "Bilen afleveret?", optionalBoolToStr(v.VisitResponse.AssetDelivered), "")
	questionRow(pdf, "Salgsfuldmagt underskrevet", optionalBoolToStr(v.VisitResponse.SFSigned), "")
	questionRow(pdf, "Salgsaftale underskrevet", optionalBoolToStr(v.VisitResponse.SESigned), "SE")

}

func fillFinanceBox(pdf *fpdf.Fpdf, v models.Visit, FinanceBoxX float64, FinanceBoxY float64, FinanceWidth float64) {
	pdf.SetXY(FinanceBoxX, FinanceBoxY-6)
	pdf.SetFont("Roboto", "B", pdfnormalFontSize+3)
	pdf.CellFormat(FinanceWidth, 6, "Anden gæld", "", 0, "L", false, 0, "")
	pdf.SetFont("Roboto", "", pdfnormalFontSize-1)

	paddingX := 4.0
	paddingY := 6.0
	pdf.SetXY(FinanceBoxX+paddingX, FinanceBoxY+paddingY)

	y := FinanceBoxY + 5

	pdf.SetXY(FinanceBoxX, y)

	// ask about
	// v.VisitResponse.Creditor
	// v.VisitResponse.DebtAmount

	questionRow(pdf, "anden gæld 1", v.VisitResponse.Creditor, optionalMoneyToStr(v.VisitResponse.DebtAmount))
	questionRow(pdf, "anden gæld 2", v.VisitResponse.Creditor2, optionalMoneyToStr(v.VisitResponse.DebtAmount2))
	questionRow(pdf, "anden gæld 3", v.VisitResponse.Creditor3, optionalMoneyToStr(v.VisitResponse.DebtAmount3))

}

func fillCommentsBox(pdf *fpdf.Fpdf, v models.Visit, CommentsBoxX float64, CommentsBoxY float64, CommentsWidth float64) {
	pdf.SetXY(CommentsBoxX, CommentsBoxY-6)
	pdf.SetFont("Roboto", "B", pdfnormalFontSize+3)
	pdf.CellFormat(CommentsWidth, 6, "Kommentarer", "", 0, "L", false, 0, "")
	pdf.SetFont("Roboto", "", pdfnormalFontSize-1)

	paddingX := 4.0
	paddingY := 6.0
	pdf.SetXY(CommentsBoxX+paddingX, CommentsBoxY+paddingY)

	y := CommentsBoxY + 5

	pdf.SetXY(CommentsBoxX, y)

	comment := v.VisitResponse.Comments
	pdf.MultiCell(CommentsWidth, 5, comment, "", "TL", false)

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

	// how low the boxes are
	CarLifeY := 80.0
	financeY := 130.0 + 10.0  // padding
	CommentsY := 190.0 + 10.0 // padding

	CarX := 10.0
	LifeX := 110.0
	financeX := 10.0
	CommentsX := 10.0

	boxWidth := 90.0
	CommentsWidth := 190.0

	boxHeightCar := 50.0
	boxHeightLife := 110.0
	boxHeightFinance := 25.0
	boxHeightComments := 50.0

	// left box (CAR)
	pdf.Rect(CarX, CarLifeY, boxWidth, boxHeightCar, "D")
	fillCarBox(pdf, v, CarX, CarLifeY, boxWidth)

	// right box (LIFE STATUS)
	pdf.Rect(LifeX, CarLifeY, boxWidth, boxHeightLife, "D")
	fillLifeBox(pdf, v, LifeX, CarLifeY, boxWidth)

	// gæld
	pdf.Rect(financeX, financeY, boxWidth, boxHeightFinance, "D")
	fillFinanceBox(pdf, v, financeX, financeY, boxWidth)

	// commentarer
	pdf.Rect(CommentsX, CommentsY, CommentsWidth, boxHeightComments, "D")
	fillCommentsBox(pdf, v, CommentsX, CommentsY, CommentsWidth)

}

func pdfGenerate(pdf *fpdf.Fpdf, v models.Visit) {
	pdf.SetAutoPageBreak(false, 15)
	pdf.AddUTF8Font("Roboto", "", "./static/Roboto-light.ttf")
	pdf.AddUTF8Font("Roboto", "B", "./static/Roboto-Bold.ttf")

	pdf.SetFont("Roboto", "", pdfnormalFontSize)

	pdf.AddPage()

	//tpl := gofpdi.ImportPage(pdf, "./static/Besøgsbrev bilbesøg.pdf", 1, "/MediaBox")
	//gofpdi.UseImportedTemplate(pdf, tpl, 0, 0, 210, 0)

	// Now position your fields on top, same as with the image approach
	// helper functions
	pdfHeader(pdf, v)
	// header includes top info about the case, who is involved, where and when

	pdfBody(pdf, v)
	// more descriptive about the visit

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
