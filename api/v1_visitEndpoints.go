package api

import (
	"fmt"
	"mime/multipart"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/markuskjeldsen/mop-backend-api/initializers"
	"github.com/markuskjeldsen/mop-backend-api/internal"
	"github.com/markuskjeldsen/mop-backend-api/models"
)

func getVerifyUser(c *gin.Context) (models.User, bool) {
	u, ok := c.Get("user")
	if !ok {
		return models.User{}, false
	}
	user, ok := u.(models.User)
	if !ok {
		return models.User{}, false
	}
	return user, true
}

func Verifytoken(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Token is valid",
	})
}

func bindFormValues(form *multipart.Form, vr *models.VisitResponse) error {
	// Helper to get form value
	getValue := func(key string) string {
		if values, ok := form.Value[key]; ok && len(values) > 0 {
			return values[0]
		}
		return ""
	}

	// Parse required fields
	visitID, _ := strconv.ParseUint(getValue("visit_id"), 10, 32)
	vr.VisitID = uint(visitID)

	// Parse date
	if dateStr := getValue("actual_date"); dateStr != "" {
		if date, err := time.Parse(time.RFC3339, dateStr); err == nil {
			vr.ActDate = date
		}
	}

	vr.ActTime = getValue("actual_time")
	vr.ActLat = getValue("actual_latitude")
	vr.ActLong = getValue("actual_longitude")

	// Parse bools
	vr.DebitorIsHome = getValue("debitor_is_home") == "true"
	vr.PaymentReceived = getValue("payment_received") == "true"
	vr.AssetAtAddress = getValue("asset_at_address") == "true"
	vr.HasWork = getValue("has_work") == "true"

	vr.Position = getValue("position")

	// Parse numbers
	if salary, err := strconv.ParseFloat(getValue("salary"), 32); err == nil {
		vr.Salary = float32(salary)
	}

	if children, err := strconv.ParseUint(getValue("children_under_18"), 10, 32); err == nil {
		vr.ChildrenUnder18 = uint(children)
	}

	if children, err := strconv.ParseUint(getValue("children_over_18"), 10, 32); err == nil {
		vr.ChildrenOver18 = uint(children)
	}

	vr.Comments = getValue("comments")

	return nil
}

func GetVisits(c *gin.Context) {
	var users []models.User
	user, ok := getVerifyUser(c)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{})
	}

	if user.Rights == models.RightsUser {
		initializers.DB.Preload("Visits").Preload("Visits.Debitors").Find(&users, user.ID)
	} else if user.Rights == models.RightsAdmin {
		initializers.DB.Preload("Visits").Preload("Visits.Debitors").Where("id != 1").Find(&users)
	} else if user.Rights == models.RightsDeveloper {
		initializers.DB.Preload("Visits").Preload("Visits.Debitors").Find(&users)
	}
	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"users":  users,
	})
}

func GetVisitsById(c *gin.Context) {
	id := c.Param("id")
	var visit models.Visit

	user, ok := getVerifyUser(c)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{})
		return
	}

	query := initializers.DB.Preload("Debitors")

	if user.Rights == models.RightsUser {
		query = query.Where("user_id = ?", user.ID)
	} else if user.Rights == models.RightsAdmin {
		query = query.Where("user_id != 1")
	}

	if err := query.First(&visit, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Visit not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"visit":  visit,
	})
}

func Visit_responses(c *gin.Context) {
	user, ok := getVerifyUser(c)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{})
	}

	var users []models.User
	if user.Rights == models.RightsUser {
		initializers.DB.
			Preload("Visits").
			Preload("Visits.Debitors").
			Preload("Visits.VisitResponse").
			Preload("Visits.Status").
			First(&users, user.ID)
	}
	if user.Rights == models.RightsAdmin {
		initializers.DB.
			Preload("Visits").
			Preload("Visits.Debitors").
			Preload("Visits.VisitResponse").
			Preload("Visits.Status").
			Where("id != 1").Find(&users)
	}
	if user.Rights == models.RightsDeveloper {
		initializers.DB.
			Preload("Visits").
			Preload("Visits.Debitors").
			Preload("Visits.VisitResponse").
			Preload("Visits.Status").Find(&users)
	}

	c.JSON(
		http.StatusOK,
		gin.H{
			"status": "sucess",
			"users":  users,
		})
}

// POST /visit-response (form data only)
func CreateVisitResponse(c *gin.Context) {
	user, _ := getVerifyUser(c)
	var visitResponse models.VisitResponse
	if err := c.ShouldBindJSON(&visitResponse); err != nil {
		fmt.Println(err.Error())
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	if err := initializers.DB.Create(&visitResponse).Error; err != nil {
		fmt.Println(err.Error())
		c.JSON(500, gin.H{"error": "Failed to save visit response"})
		return
	}

	internal.UpdateVisitStatus(visitResponse.VisitID, 4, user.ID)
	c.JSON(200, visitResponse)
}

// POST /visit-response/:id/images (one image at a time)
func UploadVisitImage(c *gin.Context) {
	visitIDdata := c.Param("id")
	visitID, _ := strconv.ParseUint(visitIDdata, 10, 32)

	file, err := c.FormFile("image")
	if err != nil {
		c.JSON(400, gin.H{"error": "No file uploaded"})
		return
	}

	savedPath, err := internal.SaveFile(c, file)
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to save image"})
		return
	}

	image := models.VisitResponseImage{
		VisitResponseID: uint(visitID),
		ImagePath:       savedPath,
		OriginalName:    file.Filename,
	}

	initializers.DB.Create(&image)
	c.JSON(200, image)
}
