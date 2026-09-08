package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/MOPDev/mop-backend-api/initializers"
	"github.com/MOPDev/mop-backend-api/internal"
	"github.com/MOPDev/mop-backend-api/internal/logger"
	"github.com/MOPDev/mop-backend-api/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// nextVacantGroupId returns the next free group ID.
// ponytail: caller must run this inside a transaction with row locking
// (e.g. tx.Clauses(clause.Locking{Strength: "UPDATE"})) to avoid two
// concurrent visits getting the same group ID. Add advisory locking or a
// DB sequence if this path sees real concurrency.
func nextVacantGroupId(tx *gorm.DB) (uint, error) {
	if tx == nil {
		tx = initializers.DB // ponytail: fallback for callers with no active transaction
	}

	var visitGroup models.Visit
	err := tx.Where("group_id IS NOT NULL").
		Order("group_id DESC").
		First(&visitGroup).Error

	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return 1, nil
	case err != nil:
		return 0, err
	default:
		return *visitGroup.GroupId + 1, nil
	}
}

type AssignVisitsRequest struct {
	VisitIDs []uint64 `json:"visitIds" binding:"required"`
}

func AssignVisitsToGroup(c *gin.Context) {
	actingUser, ok := getVerifyUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "Unauthorized or session expired"})
		return
	}

	var req AssignVisitsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	err := initializers.DB.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&models.Visit{}).
			Where("id IN ?", req.VisitIDs).
			Count(&count).Error; err != nil {
			return err
		}
		if int(count) != len(req.VisitIDs) {
			return fmt.Errorf("one or more visit IDs not found")
		}

		nextId, err := nextVacantGroupId(tx)
		if err != nil {
			return err
		}

		for stop, visitId := range req.VisitIDs {
			err := tx.Model(&models.Visit{}).
				Where("id = ?", visitId).
				Updates(map[string]any{
					"group_id":      nextId,
					"stop_nr":       uint(stop),
					"segment_index": 0,
					"status_id":     2,
				}).Error
			if err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		logger.Errorf("Assign visit to group: %s", err.Error())
		c.Status(http.StatusInternalServerError)
		return
	}

	// use actingUser for audit/logging if required
	for _, visitId := range req.VisitIDs {
		internal.UpdateVisitStatus(uint(visitId), uint(2), actingUser.ID)
	}

	c.Status(http.StatusOK)
}

func ChangeGroupId(c *gin.Context) {
	// 1. Get current Admin user for logging purposes
	// (Assuming getVerifyUser or your middleware provides the user object)
	adminUser, ok := getVerifyUser(c)
	if !ok {
		logger.Warn("ChangeGroupId: failed to verify user")
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
		// if the target value is nil, removing a visit from a group, then it keeps its values just not the group id
		var newVisitDate time.Time = visit.VisitDate
		var newUserID uint = visit.UserID // This represents the Konsulent 1 is a default value since a visit must belong to a user

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
				// No members found: the group will contain the current values of the visit
				// this means that this is the first visit of the group also meaning it should get a new group ID

				nextId, err := nextVacantGroupId(tx)
				if err != nil {
					logger.Errorf("nextvacantGroupId: %s", err.Error())
					return err
				}
				input.TargetGroupId = &nextId
			} else {
				return err
			}
		} else {
			// if the value is nil then re just remove it
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
		err := tx.Model(&visit).Updates(map[string]interface{}{
			"group_id":   input.TargetGroupId,
			"visit_date": newVisitDate,
			"user_id":    newUserID,
		}).Error
		if err != nil {
			logger.Errorf("updating visit with group: %s", err.Error())
		}

		// only re-sequence stop_nr when the visit landed in a real group
		if input.TargetGroupId != nil {
			if err := fixRouteOrder(tx, *input.TargetGroupId, uint(visitID)); err != nil {
				logger.Errorf("FixRouteOrder: %s", err.Error())
				return err
			}
		}

		// the stored route of any affected group is stale now
		if visit.GroupId != nil && *visit.GroupId > 0 {
			if err := clearGroupRoute(tx, *visit.GroupId); err != nil {
				return err
			}
		}
		if input.TargetGroupId != nil {
			if err := clearGroupRoute(tx, *input.TargetGroupId); err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		logger.Errorf("ChangeGroupId error: %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Visit group and konsulent updated successfully"})
}

func ChangeGroupDate(c *gin.Context) {
	user, ok := getVerifyUser(c)
	if !ok {
		logger.Warn("ChangeGroupDate: failed to verify user")
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
		logger.Errorf("ChangeGroupDate: %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Group date updated successfully"})
}

func GetInGroup(c *gin.Context) { // gets all the visits in a given group
	_, ok := getVerifyUser(c)
	if !ok {
		logger.Warn("GetInGroup: failed to verify user")
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
		logger.Errorf("GetInGroup: %s", result.Error.Error())
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
		logger.Errorf("RemoveFromGroup: %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Visits successfully removed from group"})
}

func ChangeKonsulent(c *gin.Context) {
	// 1. Get current Admin user for logging
	adminUser, ok := getVerifyUser(c)
	if !ok {
		logger.Warn("ChangeKonsulent: failed to verify user")
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
		logger.Errorf("ChangeKonsulent: %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Every visit in the group has been assigned to the new konsulent",
	})
}
