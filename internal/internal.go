package internal

import (
	"github.com/markuskjeldsen/mop-backend-api/initializers"
	"github.com/markuskjeldsen/mop-backend-api/models"
)

// Example function to update status and log the change
func UpdateVisitStatus(visitID uint, newStatusID uint, userID uint) error {
	var visit models.Visit
	if err := initializers.DB.First(&visit, visitID).Error; err != nil {
		return err
	}
	oldStatusID := visit.StatusID

	// Update status
	if err := initializers.DB.Model(&visit).Update("status_id", newStatusID).Error; err != nil {
		return err
	}

	// Log the change
	log := models.VisitStatusLog{
		VisitID:     visitID,
		OldStatusID: oldStatusID,
		NewStatusID: newStatusID,
		ChangedByID: userID,
	}
	return initializers.DB.Create(&log).Error
}
