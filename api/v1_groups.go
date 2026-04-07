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
	// 1. Get current Admin user for logging purposes
	// (Assuming getVerifyUser or your middleware provides the user object)
	adminUser, ok := getVerifyUser(c)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Unauthorized or session expired"})
		return
	}

	// 2. Get Visit ID from Param
	visitIDStr := c.Param("id")
	visitID, err := strconv.ParseUint(visitIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Visit ID"})
		return
	}

	// 3. Get target group from request body
	var input struct {
		TargetGroupId *uint `json:"targetGroupId"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Target Group ID is required"})
		return
	}

	// 4. Run in a Transaction
	err = initializers.DB.Transaction(func(tx *gorm.DB) error {
		var visit models.Visit

		// Fetch the visit (No ownership check here, as admin has full access)
		if err := tx.First(&visit, visitID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("visit not found")
			}
			return err
		}

		// Logic for synchronizing fields based on the new group
		var newVisitDate time.Time
		var newUserID uint // This represents the Konsulent

		if input.TargetGroupId != nil {
			var sibling models.Visit
			// Try to find another member of the target group
			err := tx.Where("group_id = ? AND id != ?", *input.TargetGroupId, visitID).
				Select("user_id", "visit_date").
				First(&sibling).Error

			if err == nil {
				// Member found: Copy their visit date and konsulent (UserID)
				newVisitDate = sibling.VisitDate
				newUserID = sibling.UserID
			} else if errors.Is(err, gorm.ErrRecordNotFound) {
				// No members found: Set to zero values (frontend will handle assignment)
				newVisitDate = time.Time{}
				newUserID = 0
			} else {
				return err
			}
		}

		// 1. LOG changes (before updating the record)
		groupLogVal := "NULL"
		if input.TargetGroupId != nil {
			groupLogVal = fmt.Sprintf("%v", *input.TargetGroupId)
		}
		if err := internal.UpdateVisitValue(tx, uint(visitID), groupLogVal, adminUser.ID, "group_id"); err != nil {
			return err
		}

		// 2. UPDATE Visit
		// We use a map with Updates to ensure GORM doesn't ignore "zero values" (like 0 or empty time)
		return tx.Model(&visit).Updates(map[string]interface{}{
			"group_id":   input.TargetGroupId,
			"visit_date": newVisitDate,
			"user_id":    newUserID,
		}).Error
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Visit group and konsulent updated successfully"})
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

	var input struct {
		NewDate string `json:"newDate"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "New date is required"})
		return
	}

	parsedDate, err := time.Parse("2006-01-02", input.NewDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date format. Use YYYY-MM-DD"})
		return
	}

	err = initializers.DB.Transaction(func(tx *gorm.DB) error {
		var visits []models.Visit

		if err := tx.Where("group_id = ?", groupId).Find(&visits).Error; err != nil {
			return err
		}

		if len(visits) == 0 {
			return errors.New("no visits found in group")
		}

		for _, v := range visits {
			if v.StatusID == 3 {
				return errors.New("cannot change date: letter has already been sent for one or more visits in this group")
			}
		}

		newDateStr := parsedDate.Format(time.RFC3339)
		for _, v := range visits {
			if err := internal.UpdateVisitValue(tx, v.ID, newDateStr, user.ID, "visit_date"); err != nil {
				return err
			}
		}

		return tx.Model(&models.Visit{}).Where("group_id = ?", groupId).Update("visit_date", parsedDate).Error
	})

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

func ChangeKonsulent(c *gin.Context) {
	// 1. Get current Admin user for logging
	adminUser, ok := getVerifyUser(c)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Unauthorized"})
		return
	}

	// 2. Get Group ID from Param
	groupIdStr := c.Param("groupId")
	groupId, err := strconv.ParseUint(groupIdStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Group ID"})
		return
	}

	// 3. Get the new Konsulent (User ID) from request body
	var input struct {
		NewUserID uint `json:"newUserId" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "NewUserID is required"})
		return
	}

	// 4. Run in a Transaction
	err = initializers.DB.Transaction(func(tx *gorm.DB) error {
		// A. Find all visits in this group to log the changes
		var visits []models.Visit
		if err := tx.Where("group_id = ?", groupId).Find(&visits).Error; err != nil {
			return err
		}

		if len(visits) == 0 {
			return errors.New("no visits found in this group")
		}

		// B. Log the change for every visit in the group
		for _, v := range visits {
			// Skip logging if the konsulent is already the same
			if v.UserID == input.NewUserID {
				continue
			}

			err := internal.UpdateVisitValue(
				tx,
				v.ID,
				fmt.Sprintf("%v", input.NewUserID),
				adminUser.ID,
				"user_id",
			)
			if err != nil {
				return err
			}
		}

		// C. Perform Batch Update for the whole group
		// We use .Model(&models.Visit{}) to specify the table and .Where to filter
		result := tx.Model(&models.Visit{}).
			Where("group_id = ?", groupId).
			Update("user_id", input.NewUserID)

		if result.Error != nil {
			return result.Error
		}

		return nil
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Every visit in the group has been assigned to the new konsulent",
	})
}
