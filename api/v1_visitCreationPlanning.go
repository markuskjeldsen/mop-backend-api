package api

import (
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/markuskjeldsen/mop-backend-api/initializers"
	"github.com/markuskjeldsen/mop-backend-api/internal"
	"github.com/markuskjeldsen/mop-backend-api/internal/excel"
	"github.com/markuskjeldsen/mop-backend-api/models"
	"gorm.io/gorm"
)

func VisitCreation(c *gin.Context) {
	user, _ := getVerifyUser(c)

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

		//
		VisitType models.VisitType `json:"visit_type"`
	}
	var visitsData []visitData

	if err := c.ShouldBindBodyWithJSON(&visitsData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(visitsData) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No data is sent"})
		return
	}

	var sagsIds []uint
	for _, vd := range visitsData {
		sagsIds = append(sagsIds, uint(vd.Sagsnr))
	}

	advoDataMap, err := internal.FetchBulkCaseData(sagsIds)
	if err != nil {
		log.Println("Error fetching bulk case data:", err)
		// Decide if you want to fail or just continue with empty fields
	}

	var createdVisits []models.Visit
	for _, visitData := range visitsData {
		// create visit
		var notes string
		if visitData.Noter != nil {
			notes = *visitData.Noter
		}

		extData := advoDataMap[uint(visitData.Sagsnr)]

		deadlinestr := ""
		if !extData.DeadlineDate.IsZero() {
			deadlinestr = extData.DeadlineDate.Format("02/01/2006")
		}

		visit := models.Visit{
			UserID:  1,
			Address: visitData.Adresse + "," + visitData.Postnr + " " + visitData.Bynavn,
			Notes:   notes,
			Sagsnr:  uint(visitData.Sagsnr),
			TypeID:  visitData.VisitType.ID,

			// THE ADVOPRO DATA
			AdvoproStatus:       uint(extData.Status),
			AdvoproStatusText:   extData.StatusText,
			AdvoproDeadlineDate: deadlinestr, // possibly change to time.Time if needed in the future
			AdvoproKlient:       extData.KlientNavn,
		}
		result := initializers.DB.Create(&visit)
		if result.Error != nil {
			fmt.Println(result.Error.Error())
		}
		createdVisits = append(createdVisits, visit)

		for _, debtor := range visitData.Debtors {
			debitorData := internal.FetchDebitorData(debtor.DebitorId)
			if debitorData == nil {
				log.Fatal("Debitor didnt exist in advopro")
				return
			}

			// create debitor in local database if not exists
			// if exists then assign the visit
			var existingDebitor models.Debitor
			result := initializers.DB.Where("advopro_debitor_id = ?", debtor.DebitorId).First(&existingDebitor)
			if result.Error != nil { //if debitor isnt there then create them
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

	// re fetch the visits to ensure debitor is there

	// first collect all ids
	var createdIDs []uint
	for _, v := range createdVisits {
		createdIDs = append(createdIDs, v.ID)
	}

	// then get from database
	var fullyLoadedVisits []models.Visit
	initializers.DB.Preload("Debitors").Where("id IN ?", createdIDs).Find(&fullyLoadedVisits)

	// logging
	for _, object := range fullyLoadedVisits {
		internal.LogVisitCreate(user, object)
	}
	// then return an excel sheet with the visits on it
	// Generate Excel
	f, err := excel.GenerateVisitsExcel(fullyLoadedVisits)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
	excel.SendExcelResponse(c, f, "visits.xlsx")
}

func VisitFile(c *gin.Context) {
	_, ok := getVerifyUser(c)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "User could not be found from the token"})
		return
	}

	var planData struct {
		VisitIds []int `json:"visitIds"`
	}

	if err := c.ShouldBindJSON(&planData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var visits []models.Visit
	// Efficiently fetch all visits at once
	initializers.DB.Preload("Debitors").Where("id IN ?", planData.VisitIds).Find(&visits)

	f, _ := excel.GenerateVisitsExcel(visits)
	excel.SendExcelResponse(c, f, "plan_visits.xlsx")
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
