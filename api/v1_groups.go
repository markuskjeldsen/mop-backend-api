package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/markuskjeldsen/mop-backend-api/initializers"
	"github.com/markuskjeldsen/mop-backend-api/internal"
	"github.com/markuskjeldsen/mop-backend-api/models"
	"gorm.io/gorm"
)

func ChangeGroupId(c *gin.Context) {
	// 1. Verify User
	user, ok := getVerifyUser(c)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "something went wrong doing verifyUser"})
		return
	}

	// 2. Get Visit ID from Param
	visitIDStr := c.Param("id")
	visitID, err := strconv.ParseUint(visitIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	// 3. TODO: Get target group from request body instead of hardcoding
	// Example: targetGroupId := c.PostForm("targetGroupId")
	targetGroupId := uint64(10)

	// 4. Run in a Transaction
	err = initializers.DB.Transaction(func(tx *gorm.DB) error {
		var visit models.Visit

		// Fetch existing record to verify ownership and get the "old" GroupID
		// We add a check for user_id to ensure the user owns this visit
		if err := tx.Where("id = ? AND user_id = ?", visitID, user.ID).First(&visit).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("visit not found or access denied")
			}
			return err
		}

		// 1. LOG FIRST (while DB still has old value)
		// We pass 'tx' so it's part of this atomic block
		err := internal.UpdateVisitValue(tx, uint(visitID), fmt.Sprintf("%v", targetGroupId), user.ID, "group_id")
		if err != nil {
			return err
		}

		// 2. UPDATE SECOND
		return tx.Model(&models.Visit{}).Where("id = ?", visitID).Update("group_id", targetGroupId).Error
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Visit moved successfully"})
}

func ChangeGroupDate(c *gin.Context) {
	user, ok := getVerifyUser(c)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "something went wrong doing verifyUser"})
		return
	}

	groupIdStr := c.Param("groupId")
	groupId, err := strconv.ParseUint(groupIdStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	newDate := time.Now()
	newDateStr := newDate.Format(time.RFC3339)

	err = initializers.DB.Transaction(func(tx *gorm.DB) error {
		var visits []models.Visit

		// 1. Find all visits in this group BEFORE updating
		if err := tx.Where("group_id = ?", groupId).Find(&visits).Error; err != nil {
			return err
		}

		// 2. Log for each visit
		for _, v := range visits {
			if err := internal.UpdateVisitValue(tx, v.ID, newDateStr, user.ID, "visit_date"); err != nil {
				return err
			}
		}

		// 3. Perform the bulk update
		return tx.Model(&models.Visit{}).Where("group_id = ?", groupId).Update("visit_date", newDate).Error
	})
	// check if the transacion went well
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Group date updated successfully"})
}

func GetInGroup(c *gin.Context) { // gets all the visits in a given group
	// TODO: get all the visits in a given group
	_, ok := getVerifyUser(c)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "something went wrong doing verifyUser"})
		return
	}

	// get the group id
	groupIdStr := c.Param("groupId")
	groupId, err := strconv.ParseUint(groupIdStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	var visits []models.Visit

	result := initializers.DB.Find(&visits).Where("group_id = ?", uint(groupId))
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
		return
	}

	c.JSON(http.StatusOK, visits)
}

// removes the visits from a group. sets GroupID = 0 for all visits in that group
func RemoveFromGroup(c *gin.Context) {
	user, ok := getVerifyUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "Unauthorized"})
		return
	}

	groupIDStr := c.Param("id")
	groupID, err := strconv.ParseUint(groupIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	newGroupID := uint64(0)

	err = initializers.DB.Transaction(func(tx *gorm.DB) error {
		var visits []models.Visit

		// 1. Fetch records
		if err := tx.Where("group_id = ?", groupID).Find(&visits).Error; err != nil {
			return err
		}

		if len(visits) == 0 {
			return errors.New("no visits found")
		}

		// 2. Log for each visit (BEFORE the update)
		for _, v := range visits {
			err := internal.UpdateVisitValue(tx, v.ID, fmt.Sprintf("%d", newGroupID), user.ID, "group_id")
			if err != nil {
				return err
			}
		}

		// 3. Perform the bulk update
		result := tx.Model(&models.Visit{}).
			Where("group_id = ? AND user_id = ?", groupID, user.ID).
			Update("group_id", newGroupID)

		if result.Error != nil {
			return result.Error
		}

		return nil
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Visits successfully removed from group"})
}
