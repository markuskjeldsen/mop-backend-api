package api

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/markuskjeldsen/mop-backend-api/initializers"
	"github.com/markuskjeldsen/mop-backend-api/models"
	"gorm.io/gorm"
)

func AvailableVisitCreation(c *gin.Context) {
	// This function returns a list of visits which can be created.
	// these visits are in status kode 5 in the AdvoPro db
	// this is for viewing the visits not for creating them
	results, err := initializers.ExecuteQuery(initializers.Server, initializers.AdvoPro, initializers.StatusFemQuery)
	if err != nil {
		log.Fatal(err)
	}

	c.JSON(http.StatusOK, gin.H{
		"results": results,
	})
}

func VisitCreation(c *gin.Context) {
	// this function creates the visits that the user chooses, and they are then initalized in the database and created as an excel file
	type visitData struct {
		Sagsnr     int64   `json:"sagsnr"`
		Status     int64   `json:"status"`
		ForlobInfo string  `json:"forlobInfo"`
		Navn       string  `json:"navn"`
		Adresse    string  `json:"adresse"`
		Postnr     string  `json:"postnr"`
		Bynavn     string  `json:"bynavn"`
		Noter      *string `json:"noter"`
		DebitorId  int64   `json:"debitorId"`
	}
	var visitsData []visitData

	c.ShouldBindBodyWithJSON(&visitsData)

	fmt.Println(visitsData)

	for _, visitData := range visitsData {
		debitorData := initializers.FetchDebitorData(visitData.DebitorId)
		if debitorData == nil {
			// handle error
			log.Fatal("Debitor didnt exist")
			return
		}

		// create debitor in local database
		var existingDebitor models.Debitor
		result := initializers.DB.Where("advopro_debitor_id = ?", visitData.DebitorId).First(&existingDebitor)
		if result.Error != nil {
			if result.Error == gorm.ErrRecordNotFound {
				debitor := models.Debitor{
					Name:             debitorData.Name,
					Phone:            debitorData.Phone,
					PhoneWork:        debitorData.PhoneWork,
					Email:            debitorData.Email,
					Gender:           debitorData.Gender,
					Birthday:         debitorData.Birthday,
					AdvoproDebitorId: int(visitData.DebitorId),
					Risk:             debitorData.Risk,
					SSN:              debitorData.SSN,
				}
				initializers.DB.Create(&debitor)
				existingDebitor = debitor
			} else {
				// handle error
				return
			}
		}

		// create visit
		visit := models.Visit{
			UserID:  0, // assuming you have the user ID,
			Address: visitData.Adresse + "," + visitData.Postnr + " " + visitData.Bynavn,
			Notes:   *visitData.Noter,
			Sagsnr:  uint(visitData.Sagsnr),
			// other fields can be populated as needed
			Debitors: []models.Debitor{existingDebitor},
		}
		initializers.DB.Create(&visit)

		// associate debitor with visit
		initializers.DB.Model(&visit).Association("Debitors").Append(&existingDebitor)
	}
}

// when the backend has verified that this data is what is supposed to be created,
// we should create the debitors in our local database so we can reference them
// then
