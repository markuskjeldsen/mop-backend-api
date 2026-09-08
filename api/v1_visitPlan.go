package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/360EntSecGroup-Skylar/excelize"
	"github.com/MOPDev/mop-backend-api/initializers"
	"github.com/MOPDev/mop-backend-api/internal"
	"github.com/MOPDev/mop-backend-api/internal/excel"
	"github.com/MOPDev/mop-backend-api/internal/logger"
	"github.com/MOPDev/mop-backend-api/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func visitIntervalRange(arrivalTime string) (string, bool) {
	t, err := time.Parse("15:04", arrivalTime)
	if err != nil {
		return "", false
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

	return fmt.Sprintf("%s - %s", start.Format("15:04"), end.Format("15:04")), true
}

// preparedRow holds one already-validated row, ready to write.
type preparedRow struct {
	rowNum        int // for error messages, 1-based excel row (i+2)
	visitID       uint
	sagsnr        uint
	stopnr        uint
	advoproStatus uint
	visit         models.Visit
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

	userIDUint, err := strconv.ParseUint(userID, 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"error": "Invalid userId"})
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

	// one group id per upload (aesthetic grouping: "this route belongs together")
	var lastVisit models.Visit
	var nextGroupId uint = 1
	if result := initializers.DB.Where("group_id IS NOT NULL").Order("group_id DESC").First(&lastVisit); result.Error == nil && lastVisit.GroupId != nil {
		nextGroupId = *lastVisit.GroupId + 1
	}

	headers := rows[0]

	// --- Pass 1: validate every row before writing anything. ---
	// Any bad row rejects the whole file, so the user gets one clear report
	// instead of a partially-applied upload.
	var prepared []preparedRow
	var rowErrors []string

	for i, row := range rows[1:] {
		rowNum := i + 2
		rowData := make(map[string]string)
		for j, header := range headers {
			if j < len(row) {
				rowData[header] = strings.TrimSpace(row[j])
			} else {
				rowData[header] = ""
			}
		}

		visitIDUint, err1 := strconv.ParseUint(rowData["Comment 6"], 10, 64) // besoegsId
		sagsnrUint, err2 := strconv.ParseUint(rowData["Title"], 10, 64)      // sagsnr
		stopNrUint, err3 := strconv.ParseUint(rowData["Stop"], 10, 64)
		advoproStatusUint, err4 := strconv.ParseUint(rowData["Comment 2"], 10, 64) // statuskode

		switch {
		case err1 != nil || visitIDUint == 0:
			rowErrors = append(rowErrors, fmt.Sprintf("row %d: missing/invalid Visit ID (Comment 6)", rowNum))
			continue
		case err2 != nil:
			rowErrors = append(rowErrors, fmt.Sprintf("row %d: invalid Sagsnr (Title)", rowNum))
			continue
		case err3 != nil:
			rowErrors = append(rowErrors, fmt.Sprintf("row %d: invalid Stop", rowNum))
			continue
		case err4 != nil:
			rowErrors = append(rowErrors, fmt.Sprintf("row %d: invalid Advopro status (Comment 2)", rowNum))
			continue
		}

		visitInterval, ok := visitIntervalRange(rowData["Arrival Time"])
		if !ok {
			rowErrors = append(rowErrors, fmt.Sprintf("row %d: invalid Arrival Time %q", rowNum, rowData["Arrival Time"]))
			continue
		}

		// sagsnr is never updated by this action - the file must reference
		// the same visit/sagsnr pair that's already in the DB, or reject.
		var existing models.Visit
		if err := initializers.DB.Select("id", "sagsnr").First(&existing, visitIDUint).Error; err != nil {
			rowErrors = append(rowErrors, fmt.Sprintf("row %d: visit id %d not found", rowNum, visitIDUint))
			continue
		}
		if existing.Sagsnr != uint(sagsnrUint) {
			rowErrors = append(rowErrors, fmt.Sprintf("row %d: sagsnr mismatch for visit %d (file has %d, db has %d)", rowNum, visitIDUint, sagsnrUint, existing.Sagsnr))
			continue
		}

		stopNrUintConv := uint(stopNrUint)
		stopnruintptr := &stopNrUintConv

		prepared = append(prepared, preparedRow{
			rowNum:        rowNum,
			visitID:       uint(visitIDUint),
			sagsnr:        uint(sagsnrUint),
			stopnr:        uint(stopNrUint),
			advoproStatus: uint(advoproStatusUint),
			visit: models.Visit{
				Latitude:            rowData["Lattitude"],  // Keep typo if it matches Excel header
				Longitude:           rowData["longtitude"], // Keep typo if it matches Excel header
				VisitTime:           rowData["Arrival Time"],
				VisitInterval:       visitInterval,
				VisitDate:           parsedDate,
				Stopnr:              stopnruintptr,
				Address:             rowData["Address"],
				UserID:              uint(userIDUint),
				Sagsnr:              uint(sagsnrUint),
				AdvoproStatus:       uint(advoproStatusUint),
				AdvoproStatusText:   rowData["Comment 3"],
				AdvoproDeadlineDate: rowData["Comment 4"],
				AdvoproKlient:       rowData["Comment 5"],
				GroupId:             &nextGroupId,
			},
		})
	}

	if len(rowErrors) > 0 {
		c.JSON(400, gin.H{"error": "File rejected, fix the following rows and re-upload", "rows": rowErrors})
		return
	}

	// --- Pass 2: everything validated, write it all in one transaction. ---
	err = initializers.DB.Transaction(func(tx *gorm.DB) error {
		for _, p := range prepared {
			result := tx.Model(&models.Visit{}).
				Where("id = ? AND sagsnr = ?", p.visitID, p.sagsnr).
				Updates(p.visit)
			if result.Error != nil {
				return fmt.Errorf("row %d: %w", p.rowNum, result.Error)
			}
			if result.RowsAffected > 0 {
				internal.UpdateVisitStatus(p.visitID, 2, user.ID)
			}
		}
		return nil
	})
	if err != nil {
		logger.Errorf("Upload failed, rolled back: %v", err)
		c.JSON(500, gin.H{"error": "Failed to apply visits, no changes were made", "detail": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "Visits processed successfully"})
}

func PlannedVisits(c *gin.Context) {
	// this endpoint gets the visits that are planned and who is going to visit them
	// query the database users and their visits there the visit is in status code 2
	// return the data

	var users []models.User
	initializers.DB.
		//Where("id != ?", 1). // if problems arise because of this, then find another way to get non assigned visits to show up on the routing page.
		Preload("Visits", "status_id = ?", 2).
		Preload("Visits.Debitors").
		Find(&users)

	c.JSON(200, users)
}

// we need a new function that gives patrick an excel sheet that
// it is used for pulling the visit data out to Inkasso afdelingen which then send out letters
// get them by the group
func PlannedVisitsExcel(c *gin.Context) {

	_, ok := getVerifyUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "User could not be found from the token"})
		return
	}

	groupIDStr := c.Param("groupId")
	groupID, err := strconv.ParseUint(groupIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	// get all visits in a group
	var visits []models.Visit
	result := initializers.DB.Preload("User").Preload("Debitors").Where("group_id = ?", groupID).Find(&visits)
	if result.Error != nil {
		logger.Errorf("PlannedVisitsExcel: %s", result.Error.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
		return
	}

	file, err := excel.GenerateVisitsPlanExcel(visits)
	if err != nil {
		logger.Errorf("PlannedVisitsExcel: %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	excel.SendExcelResponse(c, file, "PlanlagteBesog.xlsx")
}

func PatchVisit(c *gin.Context) {
	visitIDStr := c.Param("id")

	visitID, err := strconv.ParseUint(visitIDStr, 10, 64)
	if err != nil {
		logger.Errorf("Failed to parse visit ID '%s' to uint64: %s", visitIDStr, err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		logger.Errorf("Failed to bind JSON request body for visit ID %d: %s", visitID, err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid data"})
		return
	}

	var existingVisit models.Visit
	if err := initializers.DB.First(&existingVisit, visitID).Error; err != nil {
		logger.Errorf("Visit with ID %d not found in database: %s", visitID, err.Error())
		c.JSON(http.StatusNotFound, gin.H{"error": "Visit not found"})
		return
	}

	if vt, ok := updates["visit_time"].(string); ok {
		if iv, ok := visitIntervalRange(vt); ok {
			updates["visit_interval"] = iv
		}
	}

	if err := initializers.DB.Model(&existingVisit).Updates(updates).Error; err != nil {
		logger.Errorf("Failed to update visit with ID %d: %s", visitID, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	initializers.DB.Preload("User").Preload("Status").Preload("Debitors").Preload("Type").Preload("VisitResponse").First(&existingVisit, visitID)

	c.JSON(http.StatusOK, existingVisit)
}
