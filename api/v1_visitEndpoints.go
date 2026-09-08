package api

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/MOPDev/mop-backend-api/initializers"
	"github.com/MOPDev/mop-backend-api/internal"
	"github.com/MOPDev/mop-backend-api/internal/logger"
	"github.com/MOPDev/mop-backend-api/models"
	"github.com/gin-gonic/gin"
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

func groupVisits(results []map[string]interface{}) []map[string]interface{} {

	var processedVisits = make(map[string]map[string]interface{})

	for index, result := range results {
		sagsnr, ok1 := result["sagsnr"].(int64)
		adresse := result["adresse"].(string)
		postnr := result["postnr"].(string)
		bynavn := result["bynavn"].(string)
		statuskode := result["status"].(int64)
		fristDato := result["Fristdato"].(time.Time).Format("2006-01-02")
		klientnr := result["klientnr"].(int64)
		klientnavn := result["klientnavn"].(string)
		sagVedr := result["sagVedr"].(string)

		if !ok1 {
			// we skip this one if not okay
			continue
		}

		normalized := strings.ToLower(adresse)
		if idx := strings.Index(normalized, ","); idx != -1 {
			normalized = strings.TrimSpace(normalized[:idx])
		}

		addressCaseKey := fmt.Sprintf("%s%s_%d", normalized, postnr, sagsnr)

		if _, ok := processedVisits[addressCaseKey]; !ok {
			processedVisits[addressCaseKey] = map[string]interface{}{
				"index":      index,
				"sagsnr":     sagsnr,
				"adresse":    adresse, // Set initial address
				"postnr":     postnr,
				"bynavn":     bynavn,
				"status":     statuskode,
				"frist_dato": fristDato,
				"debtors":    []map[string]interface{}{},
				"klientnr":   klientnr,
				"klientnavn": klientnavn,
				"sagvedr":    sagVedr,
			}
		} else {
			existing := processedVisits[addressCaseKey]
			// Always update to longest address
			if len(adresse) > len(existing["adresse"].(string)) {
				existing["adresse"] = adresse
				processedVisits[addressCaseKey] = existing // Write back explicitly
			}
		}

		// After the if/else block, always check and update address
		existing := processedVisits[addressCaseKey]
		if len(adresse) > len(existing["adresse"].(string)) {
			existing["adresse"] = adresse
			processedVisits[addressCaseKey] = existing
		}

		// Then add debtor
		processedVisits[addressCaseKey]["debtors"] = append(
			processedVisits[addressCaseKey]["debtors"].([]map[string]interface{}),
			map[string]interface{}{
				"debitorId": result["debitorId"],
				"navn":      result["navn"],
			},
		)
	}

	var finalResults []map[string]interface{}
	for _, value := range processedVisits {
		finalResults = append(finalResults, value)
	}
	return finalResults
}

func AvailableVisitCreation(c *gin.Context) {
	results, err := internal.ExecuteQuery(context.Background(), internal.StatusFemQuery)
	if err != nil {
		logger.Error(err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
	}

	finalResults := groupVisits(results) // extracted from AvailableVisitCreation

	c.JSON(http.StatusOK, gin.H{
		"results": finalResults,
	})
}

type SagsnrRequest struct {
	Sagsnr []int64 `json:"sagsnr" binding:"required,min=1"`
}

func AvailableVisitBySagsnr(c *gin.Context) {
	var req SagsnrRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or missing sagsnr list"})
		return
	}

	placeholders := make([]string, len(req.Sagsnr))
	for i, s := range req.Sagsnr {
		placeholders[i] = strconv.FormatInt(s, 10) // numeric, safe to inline
	}
	query := fmt.Sprintf(internal.CreateSagsnrQuery, strings.Join(placeholders, ","))

	results, err := internal.ExecuteQuery(context.Background(), query)
	if err != nil {
		logger.Errorf("AvailableVisitBySagsnr query failed: %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}

	finalResults := groupVisits(results) // extracted from AvailableVisitCreation
	c.JSON(http.StatusOK, gin.H{"results": finalResults})
}

func GetVisits(c *gin.Context) {
	var users []models.User
	user, ok := getVerifyUser(c)
	if !ok {
		logger.Warn("Visit_responses: failed to verify user")
		c.JSON(http.StatusInternalServerError, gin.H{})
	}

	switch user.Rights {
	case models.RightsUser, models.RightsAuditor:
		initializers.DB.Preload("Visits").Preload("Visits.Debitors").Find(&users, user.ID)
	case models.RightsOfficeWorker, models.RightsAdmin:
		initializers.DB.Preload("Visits").Preload("Visits.Debitors").Where("id != 1").Find(&users)
	case models.RightsDeveloper:
		initializers.DB.Preload("Visits").Preload("Visits.Debitors").Find(&users)
	}
	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"users":  users,
	})
}

func GetVisitTypes(c *gin.Context) {
	var visitTypes []models.VisitType
	initializers.DB.Find(&visitTypes)
	c.JSON(http.StatusOK, visitTypes)
}

func CreatedVisits(c *gin.Context) {
	_, ok := getVerifyUser(c) //user , ok
	if !ok {
		logger.Warn("CreatedVisits: failed to verify user")
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": "User could not be found from the token",
		})
		return
	}
	var planned []models.Visit
	result := initializers.DB.
		Preload("Type").
		Preload("Debitors").
		Where(&models.Visit{StatusID: 1}).Find(&planned)
	if result.Error != nil {
		logger.Error(result.Error.Error())
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

	query := initializers.DB.Preload("Type").Preload("Debitors").Preload("User")

	switch user.Rights {
	case models.RightsUser, models.RightsAuditor:
		query = query.Where("user_id = ?", user.ID)
	case models.RightsOfficeWorker, models.RightsAdmin:
		query = query.Where("user_id != 1")
	case models.RightsDeveloper:
		break
	}

	if err := query.First(&visit, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Visit not found"})
		return
	}

	visit.User.Password = ""
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

	if user.Rights != models.RightsAdmin && user.Rights != models.RightsDeveloper && user.Rights != models.RightsOfficeWorker {
		c.JSON(http.StatusForbidden, gin.H{})
		return
	}

	result := initializers.DB.
		Preload("Type").
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
		return
	}

	var users []models.User
	switch user.Rights {
	case models.RightsUser, models.RightsAuditor:
		initializers.DB.
			Preload("Visits").
			Preload("Visits.Type").
			Preload("Visits.Debitors").
			Preload("Visits.VisitResponse").
			Preload("Visits.Status").
			First(&users, user.ID)
	case models.RightsAdmin, models.RightsOfficeWorker:
		initializers.DB.
			Preload("Visits").
			Preload("Visits.Type").
			Preload("Visits.Debitors").
			Preload("Visits.VisitResponse").
			Preload("Visits.Status").
			Where("rights != ?", models.RightsOfficeWorker).
			Where("id != 1").Find(&users)
	case models.RightsDeveloper:
		initializers.DB.
			Preload("Visits").
			Preload("Visits.Type").
			Preload("Visits.Debitors").
			Preload("Visits.VisitResponse").
			Preload("Visits.Status").Find(&users)
	default:
		logger.Warnf("user %s with following rights tried visitresponses: %s", string(user.Name), string(user.Rights))
		c.JSON(http.StatusMethodNotAllowed, gin.H{
			"status": "Failure",
			"users":  nil,
		})
		return
	}

	for i := range users {
		users[i].Password = ""
	}

	c.JSON(
		http.StatusOK,
		gin.H{
			"status": "sucess",
			"users":  users,
		})
}

func Visit_responses_user(c *gin.Context) {
	requester, ok := getVerifyUser(c)
	if !ok {
		logger.Warn("Visit_responses_user: failed to verify user")
		c.JSON(http.StatusInternalServerError, gin.H{})
		return
	}

	targetID, err := strconv.Atoi(c.Param("userid"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "Failure", "error": "invalid userid"})
		return
	}

	// ponytail: same rights check as Visit_responses, own record always allowed
	if requester.ID != uint(targetID) {
		switch requester.Rights {
		case models.RightsAdmin, models.RightsOfficeWorker, models.RightsDeveloper:
			// allowed
		default:
			logger.Warnf("user %s with rights %s tried visit-response for user %d", requester.Name, requester.Rights, targetID)
			c.JSON(http.StatusMethodNotAllowed, gin.H{"status": "Failure", "user": nil})
			return
		}
	}

	var target models.User
	err = initializers.DB.
		Preload("Visits").
		Preload("Visits.Type").
		Preload("Visits.Debitors").
		Preload("Visits.VisitResponse").
		Preload("Visits.Status").
		First(&target, targetID).Error
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"status": "Failure"})
		return
	}

	target.Password = ""

	c.JSON(http.StatusOK, gin.H{
		"status": "sucess",
		"user":   target,
	})
}

// ponytail: swap so Min/Max is always ordered instead of rejecting the request;
// caller doesn't care which field was typed as "higher"
func orderMoney(min, max **models.Money) {
	if *min != nil && *max != nil && **min > **max {
		*min, *max = *max, *min
	}
}

// POST /visit-response
func CreateVisitResponse(c *gin.Context) {
	user, ok := getVerifyUser(c)
	if !ok {
		c.JSON(401, gin.H{"error": "verification failed"})
		return
	}

	var visitResponse models.VisitResponse
	if err := c.ShouldBindJSON(&visitResponse); err != nil {
		logger.Error(err.Error())
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// order the money, before it goes into the DB
	m := &visitResponse.Monetary
	orderMoney(&m.NetSalaryMin, &m.NetSalaryMax)
	orderMoney(&m.IncomePaymentMin, &m.IncomePaymentMax)
	orderMoney(&m.MonthlyDisposableMin, &m.MonthlyDisposableMax)

	// ponytail: upsert on visit_id so a retry after failed image upload
	// re-uses the existing response instead of erroring out
	err := initializers.DB.
		Where(models.VisitResponse{VisitID: visitResponse.VisitID}).
		Assign(visitResponse).
		FirstOrCreate(&visitResponse).Error
	if err != nil {
		logger.Error(err.Error())
		c.JSON(500, gin.H{"error": "Failed to save visit response"})
		return
	}

	// ponytail: retries re-post the full other_assets list; wipe old rows for
	// this visit response first so we don't accumulate duplicates per retry
	if err := initializers.DB.
		Where("visit_response_id = ?", visitResponse.ID).
		Delete(&models.Asset{}).Error; err != nil {
		logger.Error(err.Error())
		c.JSON(500, gin.H{"error": "Failed to reset other assets"})
		return
	}
	if len(visitResponse.OtherAssets) > 0 {
		for i := range visitResponse.OtherAssets {
			visitResponse.OtherAssets[i].ID = 0
			visitResponse.OtherAssets[i].VisitResponseID = visitResponse.ID
		}
		if err := initializers.DB.Create(&visitResponse.OtherAssets).Error; err != nil {
			logger.Error(err.Error())
			c.JSON(500, gin.H{"error": "Failed to save other assets"})
			return
		}
	}

	// Update status - handle the case where the visit is already in target status (retry scenario)
	err = internal.UpdateVisitStatus(visitResponse.VisitID, 6, user.ID)
	if err != nil {
		if err.Error() == "the record is already in that status code" {
			// This is expected on retries - log as info and continue
			logger.Info("Visit " + strconv.Itoa(int(visitResponse.VisitID)) + " is already in status 6 (retry scenario)")
		} else {
			logger.Error("Failed to update status: " + err.Error())
			c.JSON(500, gin.H{"error": "Failed to update status"})
			return
		}
	}

	c.JSON(200, visitResponse)
}

// POST /visit-response/:id/images
func UploadVisitImage(c *gin.Context) {
	visitResponseIDdata := c.Param("id")
	visitResponseID, _ := strconv.ParseUint(visitResponseIDdata, 10, 32)

	// 1. Fetch the Visit to get Sagsnr and ID
	var visitResponse models.VisitResponse
	if err := initializers.DB.First(&visitResponse, visitResponseID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Visit not found"})
		return
	}

	var visit models.Visit
	if err := initializers.DB.First(&visit, visitResponse.VisitID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return

	}

	// 2. Get the file from the request
	file, err := c.FormFile("image")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 3. Create the database record first (to get the image.ID)
	image := models.VisitResponseImage{
		VisitResponseID: uint(visitResponseID),
		OriginalName:    file.Filename,
		ImagePath:       "pending", // Temporary placeholder
	}

	if err := initializers.DB.Create(&image).Error; err != nil {
		logger.Errorf("UploadVisitImage: failed to create image record: %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not create database record"})
		return
	}

	// 4. Construct the new filename
	// Format: {Sagsnr}_{visitResponseID}_{ImageID}.extension
	extension := filepath.Ext(file.Filename)
	newFileName := fmt.Sprintf("%d_%d_%d%s", visit.Sagsnr, visitResponse.ID, image.ID, extension)

	// Define your upload directory (ensure this folder exists)
	uploadDir := "uploads/visit_images"
	finalPath := filepath.Join(uploadDir, newFileName)

	// 5. Save the file to the disk
	// Using Gin's built-in SaveUploadedFile
	if err := c.SaveUploadedFile(file, finalPath); err != nil {
		// If saving fails, you might want to delete the DB record or handle the error
		err1 := initializers.DB.Delete(&image).Error
		if err1 != nil {
			logger.Errorf("UploadVisitImage: failed to delete image record: %s", err1.Error())
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to Delete record when saving file: " + err.Error()})
			return
		}
		logger.Errorf("UploadVisitImage: failed to save file: %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save file: " + err.Error()})
		return
	}

	// 6. Update the database record with the actual path
	image.ImagePath = finalPath
	initializers.DB.Save(&image)

	c.JSON(http.StatusOK, image)
}

// POST /asset/:id/image
func UploadAssetImage(c *gin.Context) {
	assetIDdata := c.Param("id")
	assetID, _ := strconv.ParseUint(assetIDdata, 10, 32)

	var asset models.Asset
	if err := initializers.DB.First(&asset, assetID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Asset not found"})
		return
	}

	var visitResponse models.VisitResponse
	if err := initializers.DB.First(&visitResponse, asset.VisitResponseID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	var visit models.Visit
	if err := initializers.DB.First(&visit, visitResponse.VisitID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	file, err := c.FormFile("image")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	extension := filepath.Ext(file.Filename)
	newFileName := fmt.Sprintf("%d_%d_%d%s", visit.Sagsnr, asset.ID, time.Now().UnixNano(), extension)

	uploadDir := "uploads/asset_images"
	finalPath := filepath.Join(uploadDir, newFileName)

	if err := c.SaveUploadedFile(file, finalPath); err != nil {
		logger.Errorf("UploadAssetImage: failed to save file: %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save file: " + err.Error()})
		return
	}

	asset.ImagePath = finalPath
	asset.OriginalName = file.Filename
	if err := initializers.DB.Save(&asset).Error; err != nil {
		logger.Errorf("UploadAssetImage: failed to update asset: %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not update asset record"})
		return
	}

	c.JSON(http.StatusOK, asset)
}

// POST /visit-response/:id/complete
func CompleteVisitResponse(c *gin.Context) {
	user, ok := getVerifyUser(c)
	if !ok {
		c.JSON(401, gin.H{"error": "verification failed"})
		return
	}
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var vr models.VisitResponse
	if err := initializers.DB.First(&vr, id).Error; err != nil {
		c.JSON(404, gin.H{"error": "not found"})
		return
	}

	vr.Completed = true
	if err := initializers.DB.Save(&vr).Error; err != nil {
		c.JSON(500, gin.H{"error": "Failed to complete"})
		return
	}

	if err := internal.UpdateVisitStatus(vr.VisitID, 4, user.ID); err != nil &&
		err.Error() != "the record is already in that status code" {
		c.JSON(500, gin.H{"error": "Failed to update status"})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func AktivitersRapport(c *gin.Context) {
	visitIDdata := c.Query("VisitId")
	visitID, _ := strconv.ParseUint(visitIDdata, 10, 64)

	var visit models.Visit
	result := initializers.DB.First(&visit, visitID)
	if result.Error != nil {
		logger.Error(result.Error.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
		return
	}

	filepath, err := internal.GetAktivitetsrapporten(visitID)
	if err != nil {
		logger.Errorf("error occured during filepath retrival: %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	pdfPath, err := internal.ConvertDocxToPdf(filepath)
	if err != nil {
		logger.Errorf("error occured during docx to pdf: %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Header("Content-Disposition", "inline; filename=aktivitetsrapport.pdf")
	c.Header("Content-Type", "application/pdf")
	c.File(pdfPath)
}

func DebtInformation(c *gin.Context) {
	visitIDdata := c.Query("VisitId")
	visitID, _ := strconv.ParseUint(visitIDdata, 10, 64)

	var visit models.Visit
	result := initializers.DB.First(&visit, visitID)
	if result.Error != nil {
		logger.Error(result.Error.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
		return
	}

	debtData, err := internal.CurrentDebtCase(visit.Sagsnr)
	if err != nil {
		logger.Error(result.Error.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
		return
	}

	c.JSON(http.StatusOK, debtData)
}

func GetBesogsbrevHandler(c *gin.Context) {
	visitId, err := strconv.ParseUint(c.Param("visitId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid visit ID"})
		return
	}

	fileBytes, err := internal.GetBesogsbrev(visitId)
	if err != nil {
		logger.Errorf("GetBesogsbrevHandler: %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Header("Content-Disposition", `inline; filename="besogsbrev.pdf"`)
	c.Data(http.StatusOK, "application/pdf", fileBytes)
}

func GetSFHandler(c *gin.Context) {
	visitId, err := strconv.ParseUint(c.Param("visitId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid visit ID"})
		return
	}

	fileBytes, err := internal.GetSF(visitId)
	if err != nil {
		logger.Errorf("GetSFHandler: %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Header("Content-Disposition", `inline; filename="salgsfuldmagt.pdf"`)
	c.Data(http.StatusOK, "application/pdf", fileBytes)
}

func CheckBatchHandler(c *gin.Context) {
	idsStr := c.Query("ids")
	if idsStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ids query param required"})
		return
	}

	results := make(map[string]bool)
	for _, part := range strings.Split(idsStr, ",") {
		id, err := strconv.ParseUint(strings.TrimSpace(part), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id: " + part})
			return
		}

		var visit models.Visit
		if err := initializers.DB.First(&visit, id).Error; err != nil {
			results[part] = false
			continue
		}

		var reqType *uint
		if visit.TypeID == 1 {
			// kobekontrakt also needs SF; check both
			one := uint(1)
			reqType = &one
			results[part] = internal.DocExists(id, nil) == nil && internal.DocExists(id, reqType) == nil
			continue
		}

		results[part] = internal.DocExists(id, nil) == nil
	}

	c.JSON(http.StatusOK, gin.H{"results": results})
}

// BatchHandler merges besogsbrev (page 2) + SF (page 3) for each visit into one PDF.
// SF is skipped silently if the visit is not a kobekontrakt.
func GetBatchHandler(c *gin.Context) {
	idsStr := c.Query("ids")
	if idsStr == "" {
		logger.Errorf("Missing 'ids' query parameter in request")
		c.JSON(http.StatusBadRequest, gin.H{"error": "ids query param required"})
		return
	}

	var pdfs [][]byte
	for _, part := range strings.Split(idsStr, ",") {
		id, err := strconv.ParseUint(strings.TrimSpace(part), 10, 64)
		if err != nil {
			logger.Errorf("Failed to parse ID '%s' to uint64: %s", strings.TrimSpace(part), err.Error())
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id: " + part})
			return
		}

		var visit models.Visit
		err = initializers.DB.First(&visit, id).Error
		if err != nil {
			logger.Errorf("cant find visit in DB: %s", err.Error())
		}

		switch visit.TypeID {
		case 1: // kobekontrakt, get SF also
			b, err := internal.GetBesogsbrev(id)
			if err != nil {
				logger.Errorf("Besogsbrev error for kobekontrakt, visitid %d, err: %s", visit.ID, err.Error())
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("besøgid %d besøgsbrev: %s", id, err)})
				return
			}
			pdfs = append(pdfs, b)

			// getting salgsfuldmagt
			sf, err := internal.GetSF(id)
			if err != nil {
				logger.Errorf("salgsfuldmagt error for kobekontrakt, visitid %d, err: %s", visit.ID, err.Error())
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("besøgid %d besøgsbrev: %s", id, err)})
				return

			}
			pdfs = append(pdfs, sf)

		case 2: // Leasing
			b, err := internal.GetBesogsbrev(id)
			if err != nil {
				logger.Errorf("Besogsbrev error for Leasing, visitid %d, err: %s", visit.ID, err.Error())
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("besøgid %d besøgsbrev: %s", id, err)})
				return

			}
			pdfs = append(pdfs, b)

		case 3: // blanco
			b, err := internal.GetBesogsbrev(id)
			if err != nil {
				logger.Errorf("Besogsbrev error for blanco, visitid %d, err: %s", visit.ID, err.Error())
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("besøgid %d besøgsbrev: %s", id, err)})
				return

			}
			pdfs = append(pdfs, b)

		case 4: // brev
			visitLetter, err := internal.Getbrev(id)
			if err != nil {
				logger.Errorf("brev error for blanco, visitid %d, err: %s", visit.ID, err.Error())
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("besøgid %d besøgsbrev: %s", id, err)})
				return

			}
			pdfs = append(pdfs, visitLetter)
		default:
			logger.Warn("No visittype is assigned defaulting to besogsbrev and SF")
			b, err := internal.GetBesogsbrev(id)
			if err != nil {
				logger.Errorf("Besogsbrev error for kobekontrakt, visitid %d, err: %s", visit.ID, err.Error())
				//silent error
			}
			pdfs = append(pdfs, b)

			// getting salgsfuldmagt
			sf, err := internal.GetSF(id)
			if err != nil {
				logger.Errorf("Besogsbrev error for kobekontrakt, visitid %d, err: %s", visit.ID, err.Error())
				// silent error
			}
			pdfs = append(pdfs, sf)
		}
	}

	merged, err := internal.MergePDFs(pdfs)
	if err != nil {
		logger.Errorf("Failed to merge PDFs for visit IDs '%s': %s", idsStr, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to merge PDFs: " + err.Error()})
		return
	}

	c.Header("Content-Disposition", `inline; filename="batch.pdf"`)
	c.Data(http.StatusOK, "application/pdf", merged)
}

func DeleteVisit(c *gin.Context) {
	dataid := c.Query("id")
	id, _ := strconv.ParseUint(dataid, 10, 32)

	actinguser, ok := getVerifyUser(c)
	if !ok {
		logger.Warn("DeleteVisit: failed to verify acting user")
		c.JSON(http.StatusInternalServerError, gin.H{})
		return
	}
	var visit models.Visit
	visit.ID = uint(id)

	result := initializers.DB.Delete(&visit)
	if result.Error != nil {
		logger.Errorf("DeleteVisit: failed to delete: %s", result.Error.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": result.Error.Error(),
		})
		return
	}

	internal.LogVisitDelete(actinguser, visit)

	c.Status(http.StatusOK)
}
