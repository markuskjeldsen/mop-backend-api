package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/MOPDev/mop-backend-api/initializers"
	"github.com/MOPDev/mop-backend-api/internal"
	"github.com/MOPDev/mop-backend-api/internal/excel"
	"github.com/MOPDev/mop-backend-api/internal/logger"
	"github.com/MOPDev/mop-backend-api/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type debitorData struct {
	DebitorId int64  `json:"debitorId"`
	Navn      string `json:"navn"`
}

type visitData struct {
	Sagsnr           int64            `json:"sagsnr"`
	Adresse          string           `json:"adresse"`
	Postnr           string           `json:"postnr"`
	Bynavn           string           `json:"bynavn"`
	Noter            *string          `json:"noter"`
	Debtors          []debitorData    `json:"debtors"`
	VisitType        models.VisitType `json:"visit_type"`
	KlientRef        string           `json:"klientRef"`
	Klientnavn       string           `json:"klientnavn"`
	Klientnr         int64            `json:"klientnr"`
	Sagvedr          string           `json:"sagvedr"`
	FristDato        string           `json:"frist_dato"`
	Latitude         string           `json:"latitude"`
	Longitude        string           `json:"longitude"`
	GeocodingAddress string           `json:"geocoding_address"`
}

// this function creates the visits that the user chooses,
// the visit is created
// and they are then initalized in the database and created as an excel file
func VisitCreation(c *gin.Context) {
	user, _ := getVerifyUser(c)

	var visitsData []visitData

	if err := c.ShouldBindBodyWithJSON(&visitsData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(visitsData) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No data is sent"})
		return
	}

	visitsData = mergeByGeocode(visitsData)

	var sagsIds []uint
	for _, vd := range visitsData {
		sagsIds = append(sagsIds, uint(vd.Sagsnr))
	}

	advoDataMap, err := internal.FetchBulkCaseData(sagsIds)
	if err != nil {
		logger.Errorf("failed to fetch bulk case data: %s", err.Error())
	}

	var createdVisits []models.Visit
	err = initializers.DB.Transaction(func(tx *gorm.DB) error {
		for _, visitData := range visitsData {
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

				Latitude:         visitData.Latitude,
				Longitude:        visitData.Longitude,
				GeocodingAddress: visitData.GeocodingAddress,

				AdvoproStatus:       uint(extData.Status),
				AdvoproStatusText:   extData.StatusText,
				AdvoproDeadlineDate: deadlinestr,
				AdvoproKlient:       visitData.KlientRef,
			}
			if err := tx.Create(&visit).Error; err != nil {
				logger.Error(err.Error())
				return err
			}
			createdVisits = append(createdVisits, visit)

			for _, debtor := range visitData.Debtors {
				debitorData := internal.FetchDebitorData(debtor.DebitorId)
				if debitorData == nil {
					logger.Errorf("debitor %d does not exist in advopro", debtor.DebitorId)
					return fmt.Errorf("debitor %d does not exist in advopro", debtor.DebitorId)
				}

				var existingDebitor models.Debitor
				result := tx.Where("advopro_debitor_id = ?", debtor.DebitorId).First(&existingDebitor)
				if result.Error != nil {
					if result.Error == gorm.ErrRecordNotFound {
						newDebitor := models.Debitor{
							Name:             debitorData.Name,
							Phone:            debitorData.Phone,
							PhoneWork:        debitorData.PhoneWork,
							Email:            debitorData.Email,
							Gender:           debitorData.Gender,
							Birthday:         debitorData.Birthday,
							AdvoproDebitorId: int(debtor.DebitorId),
							Risk:             debitorData.Risk,
							SSN:              debitorData.SSN,
							Iscompany:        debitorData.Iscompany,
						}
						if err := tx.Create(&newDebitor).Error; err != nil {
							return err
						}
						existingDebitor = newDebitor
					} else {
						return result.Error
					}
				}

				if err := tx.Model(&visit).Association("Debitors").Append(&existingDebitor); err != nil {
					return err
				}
			}
		}
		return nil
	})

	if err != nil {
		logger.Error(err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	logger.Infof("Created visits count: %d", len(createdVisits))

	var createdIDs []uint
	for _, v := range createdVisits {
		createdIDs = append(createdIDs, v.ID)
	}

	var fullyLoadedVisits []models.Visit
	if err := initializers.DB.Preload("Debitors").Where("id IN ?", createdIDs).Find(&fullyLoadedVisits).Error; err != nil {
		logger.Errorf("failed to load created visits for audit log: %s", err.Error())
	}

	for _, object := range fullyLoadedVisits {
		internal.LogVisitCreate(user, object)
	}

	c.JSON(http.StatusOK, gin.H{"created": len(createdVisits)})
}

// mergeByGeocode collapses visits that share the same sagsnr and the same
// normalized geocoding address, merging their debtors into one entry.
// ponytail: normalization is just lower+trim+collapse-spaces; good enough
// since the geocoder already sanitizes the address. Add real dedupe (e.g.
// lat/long rounding) only if geocoded strings start diverging for same spot.
func mergeByGeocode(in []visitData) []visitData {
	normalize := func(s string) string {
		s = strings.ToLower(strings.TrimSpace(s))
		return strings.Join(strings.Fields(s), " ")
	}

	order := []string{}
	merged := make(map[string]*visitData)

	for i := range in {
		v := in[i]
		key := fmt.Sprintf("%d_%s", v.Sagsnr, normalize(v.GeocodingAddress))

		if existing, ok := merged[key]; ok {
			existing.Debtors = append(existing.Debtors, v.Debtors...)
			continue
		}
		vCopy := v
		merged[key] = &vCopy
		order = append(order, key)
	}

	out := make([]visitData, 0, len(order))
	for _, key := range order {
		out = append(out, *merged[key])
	}
	return out
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
	if err := initializers.DB.Preload("Debitors").Where("id IN ?", planData.VisitIds).Find(&visits).Error; err != nil {
		logger.Errorf("failed to load visits for excel: %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load visits"})
		return
	}

	f, err := excel.GenerateVisitsExcel(visits)
	if err != nil {
		logger.Errorf("failed to generate excel: %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate excel"})
		return
	}
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
