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
		var result *gorm.DB
		if user.Rights == models.RightsDeveloper {
			// Update visit in database
			result = initializers.DB.Model(&models.Visit{}).
				Where("id = ? AND sagsnr = ?", visitID, sagsnr).
				Updates(models.Visit{
					Latitude:      rowData["Latitude"],
					Longitude:     rowData["Longitude"],
					VisitTime:     rowData["Arrival Time"],
					VisitInterval: visitIntervalRange(rowData["Arrival Time"]),
					VisitDate:     parsedDate,
					UserID:        uint(userIDUint),
				})
		} else {
			// Update visit in database
			result = initializers.DB.Model(&models.Visit{}).
				Where("id = ? AND sagsnr = ? AND status_id = 1", visitID, sagsnr).
				Updates(models.Visit{
					Latitude:      rowData["Latitude"],
					Longitude:     rowData["Longitude"],
					VisitTime:     rowData["Arrival Time"],
					VisitInterval: visitIntervalRange(rowData["Arrival Time"]),
					VisitDate:     parsedDate,
					UserID:        uint(userIDUint),
				})
		}

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
