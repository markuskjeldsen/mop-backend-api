package api

import (
	"fmt"
	"log"
	"mime/multipart"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/markuskjeldsen/mop-backend-api/initializers"
	"github.com/markuskjeldsen/mop-backend-api/internal"
	"github.com/markuskjeldsen/mop-backend-api/models"
	"gorm.io/gorm"
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
func AvailableVisitCreation(c *gin.Context) {
	results, err := internal.ExecuteQuery(internal.Server, internal.AdvoPro, internal.StatusFemQuery)
	if err != nil {
		log.Fatal(err)
	}

	// Process results - group by address + sagsnr combination
	var processedVisits = make(map[string]map[string]interface{})

	for index, result := range results {
		sagsnr := result["sagsnr"].(int64)
		adresse := result["adresse"].(string)
		postnr := result["postnr"].(string)
		bynavn := result["bynavn"].(string)
		statuskode := result["status"].(int64)
		fristDato := result["Fristdato"].(time.Time).Format("2006-01-02")

		// Create combined key: address + case number
		addressCaseKey := fmt.Sprintf("%s%s%s_%d", adresse, postnr, bynavn, sagsnr)

		if _, ok := processedVisits[addressCaseKey]; !ok {
			// Create new visit entry
			processedVisits[addressCaseKey] = map[string]interface{}{
				"index":      index,
				"sagsnr":     sagsnr,
				"adresse":    adresse,
				"postnr":     postnr,
				"bynavn":     bynavn,
				"status":     statuskode,
				"frist_dato": fristDato,
				"debtors":    []map[string]interface{}{},
			}
		}

		// Add debtor to this visit
		processedVisits[addressCaseKey]["debtors"] = append(
			processedVisits[addressCaseKey]["debtors"].([]map[string]interface{}),
			map[string]interface{}{
				"debitorId": result["debitorId"],
				"navn":      result["navn"],
			},
		)
	}

	// Convert map to slice
	var finalResults []map[string]interface{}
	for _, value := range processedVisits {
		finalResults = append(finalResults, value)
	}

	for _, m := range finalResults {
		fmt.Println(m["index"])
	}

	c.JSON(http.StatusOK, gin.H{
		"results": finalResults,
	})
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

func GetVisitsById(c *gin.Context) {
	id := c.Query("id")
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

func GetVisitsByStatus(c *gin.Context) {
	status := c.Query("status")
	var visits []models.Visit

	user, ok := getVerifyUser(c)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{})
		return
	}

	if user.Rights == models.RightsUser {
		c.JSON(http.StatusForbidden, gin.H{})
		return
	}

	result := initializers.DB.
		Preload("Status").
		Preload("VisitResponse").
		Preload("Debitors").
		Preload("User", func(db *gorm.DB) *gorm.DB {
			return db.Select("id", "name", "email", "phone")
		}).
		Where("Status_id = ?", status).
		Find(&visits)

	if result.Error != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Visits not found", "message": result.Error.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"visit":  visits,
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

func DebtInformation(c *gin.Context) {
	visitIDdata := c.Query("VisitId")
	visitID, _ := strconv.ParseUint(visitIDdata, 10, 64)

	var visit models.Visit
	result := initializers.DB.First(&visit, visitID)
	if result.Error != nil {
		fmt.Println(result.Error.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": result.Error.Error(),
		})
		return
	}
	data := internal.CurrentDebtCase(visit.Sagsnr)

	c.JSON(http.StatusOK, data)

}
