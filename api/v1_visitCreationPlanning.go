package api

import (
	"bufio"
	"bytes"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/360EntSecGroup-Skylar/excelize"
	"github.com/gin-gonic/gin"
	"github.com/markuskjeldsen/mop-backend-api/initializers"
	"github.com/markuskjeldsen/mop-backend-api/internal"
	"github.com/markuskjeldsen/mop-backend-api/models"
	"gorm.io/gorm"
)

func VisitCreation(c *gin.Context) {
	// this function creates the visits that the user chooses,
	// the visit is created
	// and they are then initalized in the database and created as an excel file
	type debitorData struct {
		DebitorId int64  `json:"debitorId"`
		Navn      string `json:"navn"`
	}

	type visitData struct {
		Sagsnr int64 `json:"sagsnr"`

		//ForlobInfo string  `json:"forlobInfo"`
		Adresse string        `json:"adresse"`
		Postnr  string        `json:"postnr"`
		Bynavn  string        `json:"bynavn"`
		Noter   *string       `json:"noter"`
		Debtors []debitorData `json:"debtors"`
	}
	var visitsData []visitData

	if err := c.ShouldBindBodyWithJSON(&visitsData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// when creating visits, remember that the address it defined at the debitor
	//

	var createdVisits []models.Visit
	for _, visitData := range visitsData {
		// create visit
		var notes string
		if visitData.Noter != nil {
			notes = *visitData.Noter
		}

		visit := models.Visit{
			UserID:  1,
			Address: visitData.Adresse + "," + visitData.Postnr + " " + visitData.Bynavn,
			Notes:   notes,
			Sagsnr:  uint(visitData.Sagsnr),
		}
		initializers.DB.Create(&visit)
		createdVisits = append(createdVisits, visit)

		for _, debtor := range visitData.Debtors {
			debitorData := internal.FetchDebitorData(debtor.DebitorId)
			if debitorData == nil {
				log.Fatal("Debitor didnt exist in advopro")
				return
			}

			// create debitor in local database if not exists
			var existingDebitor models.Debitor
			result := initializers.DB.Where("advopro_debitor_id = ?", debtor.DebitorId).First(&existingDebitor)
			if result.Error != nil {
				if result.Error == gorm.ErrRecordNotFound {
					debitor := models.Debitor{
						Name:             debitorData.Name,
						Phone:            debitorData.Phone,
						PhoneWork:        debitorData.PhoneWork,
						Email:            debitorData.Email,
						Gender:           debitorData.Gender,
						Birthday:         debitorData.Birthday,
						AdvoproDebitorId: int(debtor.DebitorId),
						Risk:             debitorData.Risk,
						SSN:              debitorData.SSN,
					}
					initializers.DB.Create(&debitor)
					existingDebitor = debitor
				} else {
					log.Fatal(result.Error)
					return
				}
			}

			// associate debitor with visit
			initializers.DB.Model(&visit).Association("Debitors").Append(&existingDebitor)
		}
	}

	fmt.Printf("Created visits count: %d\n", len(createdVisits))

	// then return an excel sheet with the visits on it
	// Generate Excel
	f := excelize.NewFile()
	sheetName := "Sheet1"

	// Excel Header
	// 					ID 		Sagsnr
	header := []string{"Title", "Title", "Address", "Notes", "Debitors", "Service Time"}
	for i, h := range header {
		cell := fmt.Sprintf("%c1", 'A'+i)
		f.SetCellValue(sheetName, cell, h)
	}

	// Excel Data
	for row, visit := range createdVisits {
		var debitorNames []string
		initializers.DB.Preload("Debitors").First(&visit, visit.ID)

		for _, debitor := range visit.Debitors {
			cleanName := strings.ReplaceAll(debitor.Name, "\n", " ")
			cleanName = strings.ReplaceAll(cleanName, "\r", " ")
			debitorNames = append(debitorNames, cleanName)
		}

		cleanAddress := strings.ReplaceAll(visit.Address, "\n", " ")
		cleanAddress = strings.ReplaceAll(cleanAddress, "\r", " ")
		cleanNotes := strings.ReplaceAll(visit.Notes, "\n", " ")
		cleanNotes = strings.ReplaceAll(cleanNotes, "\r", " ")

		data := []interface{}{
			fmt.Sprintf("%d", visit.ID),
			visit.Sagsnr,
			cleanAddress,
			cleanNotes,
			strings.Join(debitorNames, ", "),
			"15",
		}

		for col, value := range data {
			cell := fmt.Sprintf("%c%d", 'A'+col, row+2)
			f.SetCellValue(sheetName, cell, value)
		}
	}

	cellValue := f.GetCellValue(sheetName, "A1")
	fmt.Println(cellValue)

	c.Header("Content-Disposition", "attachment; filename=visits.xlsx")
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")

	// Create a buffer and save to it
	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)
	if err := f.Write(writer); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate Excel file"})
		return
	}
	writer.Flush()

	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", buf.Bytes())

}

func VisitFile(c *gin.Context) {
	user, ok := getVerifyUser(c)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "User could not be found from the token"})
		return
	}
	fmt.Println(user.ID)

	var planData struct {
		VisitIds []int `json:"visitIds"`
	}

	if err := c.ShouldBindJSON(&planData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Generate Excel
	f := excelize.NewFile()
	sheetName := "Sheet1"

	// Excel Header
	// 					ID 		Sagsnr
	header := []string{"Title", "Title", "Address", "Notes", "Debitors", "Service Time"}
	for i, h := range header {
		cell := fmt.Sprintf("%c1", 'A'+i)
		f.SetCellValue(sheetName, cell, h)
	}

	// Excel Data
	for row, visitid := range planData.VisitIds {
		var visit models.Visit
		var debitorNames []string
		initializers.DB.Preload("Debitors").First(&visit, visitid)

		for _, debitor := range visit.Debitors {
			cleanName := strings.ReplaceAll(debitor.Name, "\n", " ")
			cleanName = strings.ReplaceAll(cleanName, "\r", " ")
			debitorNames = append(debitorNames, cleanName)
		}

		cleanAddress := strings.ReplaceAll(visit.Address, "\n", " ")
		cleanAddress = strings.ReplaceAll(cleanAddress, "\r", " ")
		cleanNotes := strings.ReplaceAll(visit.Notes, "\n", " ")
		cleanNotes = strings.ReplaceAll(cleanNotes, "\r", " ")

		data := []interface{}{
			fmt.Sprintf("%d", visit.ID),
			visit.Sagsnr,
			cleanAddress,
			cleanNotes,
			strings.Join(debitorNames, ", "),
			"15",
		}

		for col, value := range data {
			cell := fmt.Sprintf("%c%d", 'A'+col, row+2)
			f.SetCellValue(sheetName, cell, value)
		}
	}

	cellValue := f.GetCellValue(sheetName, "A1")
	fmt.Println(cellValue)

	c.Header("Content-Disposition", "attachment; filename=visits.xlsx")
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")

	// Create a buffer and save to it
	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)
	if err := f.Write(writer); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate Excel file"})
		return
	}
	writer.Flush()

	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", buf.Bytes())
}

func VisitLetterSent(c *gin.Context) {
	user, ok := getVerifyUser(c)
	id := c.Query("id")
	visitID, err := strconv.ParseInt(id, 10, 32)

	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "User could not be found from the token"})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "the id is not correct", "error": err.Error()})
		return
	}

	internal.UpdateVisitStatus(uint(visitID), 3, user.ID) // now its ready to visit
}
