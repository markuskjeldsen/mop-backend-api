package api

import (
	"bufio"
	"bytes"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/360EntSecGroup-Skylar/excelize"
	"github.com/gin-gonic/gin"
	"github.com/markuskjeldsen/mop-backend-api/initializers"
	"github.com/markuskjeldsen/mop-backend-api/internal"
	"github.com/markuskjeldsen/mop-backend-api/models"
	"gorm.io/gorm"
)

func visitIntervalRange(arrivalTime string) string {
	t, err := time.Parse("15:04", arrivalTime)
	if err != nil {
		return ""
	}

	// Round to nearest hour
	rounded := time.Date(0, 1, 1, t.Hour(), 0, 0, 0, time.UTC)
	if t.Minute() >= 30 {
		rounded = rounded.Add(1 * time.Hour)
	}

	hour := rounded.Hour()
	var start, end time.Time

	if hour >= 18 { // late arrival
		start = rounded.Add(-2 * time.Hour)
		end = rounded.Add(1 * time.Hour)
	} else {
		start = rounded.Add(-1 * time.Hour)
		end = rounded.Add(2 * time.Hour)
	}

	// Cap end time at 20:00
	maxEnd := time.Date(0, 1, 1, 20, 0, 0, 0, time.UTC)
	if end.After(maxEnd) {
		end = maxEnd
	}

	return fmt.Sprintf("%s - %s", start.Format("15:04"), end.Format("15:04"))
}

func AvailableVisitCreation(c *gin.Context) {
	results, err := initializers.ExecuteQuery(initializers.Server, initializers.AdvoPro, initializers.StatusFemQuery)
	if err != nil {
		log.Fatal(err)
	}

	// Process results
	var processedResults = make(map[int64]map[string]interface{})
	for _, result := range results {
		sagsnr := result["sagsnr"].(int64)
		if _, ok := processedResults[sagsnr]; !ok {
			processedResults[sagsnr] = map[string]interface{}{
				"adresse": result["adresse"],
				"bynavn":  result["bynavn"],
				"postnr":  result["postnr"],
				"sagsnr":  sagsnr,
				"debtors": []map[string]interface{}{},
			}
		}
		processedResults[sagsnr]["debtors"] = append(processedResults[sagsnr]["debtors"].([]map[string]interface{}), map[string]interface{}{
			"debitorId": result["debitorId"],
			"navn":      result["navn"],
		})
	}

	// Convert map to slice
	var finalResults []map[string]interface{}
	for _, value := range processedResults {
		finalResults = append(finalResults, value)
	}

	c.JSON(http.StatusOK, gin.H{
		"results": finalResults,
	})
}

func VisitCreation(c *gin.Context) {
	// this function creates the visits that the user chooses, and they are then initalized in the database and created as an excel file
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

	var createdVisits []models.Visit
	for _, visitData := range visitsData {
		// create visit
		var notes string
		if visitData.Noter != nil {
			notes = *visitData.Noter
		}

		visit := models.Visit{
			UserID:  1, // assuming you have the user ID,
			Address: visitData.Adresse + "," + visitData.Postnr + " " + visitData.Bynavn,
			Notes:   notes,
			Sagsnr:  uint(visitData.Sagsnr),
		}
		initializers.DB.Create(&visit)
		createdVisits = append(createdVisits, visit)

		for _, debtor := range visitData.Debtors {
			debitorData := initializers.FetchDebitorData(debtor.DebitorId)
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

func PlanVisit(c *gin.Context) {

	user, ok := getVerifyUser(c)
	if !ok {
		fmt.Println("User could not be found")
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": "User could not be found from the token",
		})
	}

	// this function should accept the sent to it by the frontend.
	// verify the visits exist
	// then update the information
	// then change statuskode to 2 (or sthm else)
	// assign the visits to the given user

	// Parse multipart form
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"error": "No file uploaded"})
		return
	}

	// Get userId from form data
	userID := c.PostForm("userId")
	if userID == "" {
		c.JSON(400, gin.H{"error": "userId is required"})
		return
	}

	DateData := c.PostForm("date")
	if DateData == "" {
		c.JSON(400, gin.H{"error": "userId is required"})
		return
	}
	parsedDate, err := time.Parse("2006-01-02", DateData)
	if err != nil {
		c.JSON(400, gin.H{
			"error": "invalid date format",
		})
	}

	// Open the uploaded file
	src, err := file.Open()
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to open file"})
		return
	}
	defer src.Close()

	// Parse Excel file (assuming you're using excelize)
	f, err := excelize.OpenReader(src)
	if err != nil {
		c.JSON(400, gin.H{"error": "Invalid Excel file"})
		return
	}

	// Read data from Excel (adjust sheet name and columns as needed)
	rows := f.GetRows("Route_1")
	if rows == nil {
		c.JSON(400, gin.H{"error": "Failed to read Excel data"})
		return
	}

	headers := rows[0]
	// Process each data row
	for _, row := range rows[1:] {
		// Create map from headers and row data
		rowData := make(map[string]string)
		for j, header := range headers {
			if j < len(row) {
				rowData[header] = row[j]
			} else {
				rowData[header] = ""
			}
		}

		title := strings.Split(row[2], ",")
		visitIDInt, err := strconv.ParseUint(strings.TrimSpace(title[0]), 10, 64)
		if err != nil {
			fmt.Println(err)
		}
		visitID := uint(visitIDInt)
		sagsnrInt, err := strconv.ParseUint(strings.TrimSpace(title[1]), 10, 64)
		if err != nil {
			fmt.Println(err)
		}
		sagsnr := uint(sagsnrInt)

		userIDUint, err := strconv.ParseUint(userID, 10, 64)
		if err != nil {
			// handle error
		}

		// Update visit in database
		result := initializers.DB.Model(&models.Visit{}).
			Where("id = ? AND sagsnr = ?", visitID, sagsnr).
			Updates(models.Visit{
				Latitude:      rowData["Latitude"],
				Longitude:     rowData["Longitude"],
				VisitTime:     rowData["Arrival Time"],
				VisitInterval: visitIntervalRange(rowData["Arrival Time"]),
				VisitDate:     parsedDate,
				UserID:        uint(userIDUint),
			})

		if result.Error != nil {
			fmt.Println(result.Error.Error())
			c.JSON(500, gin.H{"error": "Database update failed"})
			return
		}

		if result.RowsAffected == 0 {
			c.JSON(400, gin.H{"error": fmt.Sprintf("Visit ID %d not found", visitID)})
			return
		}

		internal.UpdateVisitStatus(visitID, 2, user.ID)
	}

	c.JSON(200, gin.H{"message": "Visits planned successfully"})
}

func CreatedVisits(c *gin.Context) {
	user, ok := getVerifyUser(c)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": "User could not be found from the token",
		})
		return
	}
	fmt.Println(user.Username)
	var planned []models.Visit
	result := initializers.DB.Preload("Debitors").Where(&models.Visit{StatusID: 1}).Find(&planned)
	if result.Error != nil {
		fmt.Println(result.Error.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": "the database happend upon an error",
			"error":   result.Error.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "everything went well",
		"data":    planned,
	})

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
