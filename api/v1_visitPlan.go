package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/360EntSecGroup-Skylar/excelize"
	"github.com/gin-gonic/gin"
	"github.com/markuskjeldsen/mop-backend-api/initializers"
	"github.com/markuskjeldsen/mop-backend-api/internal"
	"github.com/markuskjeldsen/mop-backend-api/models"
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

func PlanVisit(c *gin.Context) {
	user, ok := getVerifyUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "User could not be found from the token"})
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"error": "No file uploaded"})
		return
	}

	userID := c.PostForm("userId")
	dateData := c.PostForm("date")
	if userID == "" || dateData == "" {
		c.JSON(400, gin.H{"error": "userId and date are required"})
		return
	}

	parsedDate, err := time.Parse("2006-01-02", dateData)
	if err != nil {
		c.JSON(400, gin.H{"error": "Invalid date format. Use YYYY-MM-DD"})
		return
	}

	src, err := file.Open()
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to open file"})
		return
	}
	defer src.Close()

	f, err := excelize.OpenReader(src)
	if err != nil {
		c.JSON(400, gin.H{"error": "Invalid Excel file"})
		return
	}

	// Adjust "Route_1" if the sheet name changes in the new config
	rows := f.GetRows("Route_1")
	if len(rows) < 2 {
		c.JSON(400, gin.H{"error": "Failed to read Excel data or sheet is empty"})
		return
	}

	// to find the next group id
	var visit models.Visit
	var nextGroupId uint
	result := initializers.DB.First(&visit).Where("group_id is not null").Order("group_id DESC")
	if result.Error != nil {
		nextGroupId = uint(1)
	} else {
		nextGroupId = *visit.GroupId + 1
	}

	headers := rows[0]
	userIDUint, _ := strconv.ParseUint(userID, 10, 64)

	for i, row := range rows[1:] {
		// Create map for easy access by column name
		rowData := make(map[string]string)
		for j, header := range headers {
			if j < len(row) {
				rowData[header] = strings.TrimSpace(row[j])
			} else {
				rowData[header] = ""
			}
		}

		// 1. Parse IDs (Crucial for finding the record)
		// Based on your notes: [16] is Comment 6 / besoegsId
		visitIDUint, _ := strconv.ParseUint(rowData["Comment 6"], 10, 64) // besoegsId
		// Based on your notes: [2] is Title / sagsnr
		sagsnrUint, _ := strconv.ParseUint(rowData["Title"], 10, 64) // sagsnr
		// Based on your notes: [1] is Stop
		stopNrUint, _ := strconv.ParseUint(rowData["Stop"], 10, 64)

		// 2. Parse Advopro Status (Comment 2)
		advoproStatusUint, _ := strconv.ParseUint(rowData["Comment 2"], 10, 64) // , statuskode

		if visitIDUint == 0 {
			fmt.Printf("Row %d: Missing Visit ID, skipping\n", i+2)
			continue
		}

		// 3. Prepare Update Object
		updatedVisit := models.Visit{
			Latitude:      rowData["Lattitude"],  // Keep typo if it matches Excel header
			Longitude:     rowData["longtitude"], // Keep typo if it matches Excel header
			VisitTime:     rowData["Arrival Time"],
			VisitInterval: visitIntervalRange(rowData["Arrival Time"]),
			VisitDate:     parsedDate,
			Stopnr:        uint(stopNrUint),
			Address:       rowData["Address"],
			UserID:        uint(userIDUint),
			Sagsnr:        uint(sagsnrUint),
			// New Advopro fields
			AdvoproStatus:       uint(advoproStatusUint),
			AdvoproStatusText:   rowData["Comment 3"], // , statustekst
			AdvoproDeadlineDate: rowData["Comment 4"], // , fristDato
			AdvoproKlient:       rowData["Comment 5"], // , Klientnavn
			// new group id
			GroupId: &nextGroupId,
		}

		// 4. Database logic
		query := initializers.DB.Model(&models.Visit{}).Where("id = ? AND sagsnr = ?", visitIDUint, sagsnrUint)

		// If not developer, only allow updating visits that are still in 'New' status (status_id = 1)
		if user.Rights != models.RightsDeveloper {
			query = query.Where("status_id = 1")
		}

		result := query.Updates(updatedVisit)

		if result.Error != nil {
			fmt.Printf("Database error row %d: %v\n", i+2, result.Error)
			continue
		}

		if result.RowsAffected > 0 {
			// Update the internal status to 2 (Planned/Assigned)
			internal.UpdateVisitStatus(uint(visitIDUint), 2, user.ID)
		}
	}

	c.JSON(200, gin.H{"message": "Visits processed successfully"})
}

func PlannedVisits(c *gin.Context) {
	// this endpoint gets the visits that are planned and who is going to visit them
	// query the database users and their visits there the visit is in status code 2
	// return the data

	var users []models.User
	initializers.DB.
		Where("id != ?", 1).
		Preload("Visits", "status_id = ?", 2).
		Preload("Visits.Debitors").
		Find(&users)

	c.JSON(200, users)
}

func PatchVisit(c *gin.Context) {
	var visit models.Visit
	visitIDStr := c.Param("id")

	visitID, err := strconv.ParseUint(visitIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	if err := c.ShouldBindJSON(&visit); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid data"})
		return
	}

	var existingVisit models.Visit
	if err := initializers.DB.First(&existingVisit, visitID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Visit not found"})
		return
	}

	// Only update non-zero value fields
	if err := initializers.DB.Model(&existingVisit).Updates(visit).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update visit"})
		return
	}

	c.JSON(http.StatusOK, existingVisit)
}
