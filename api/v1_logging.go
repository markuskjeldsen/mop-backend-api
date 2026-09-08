package api

import (
	"encoding/json"
	"net/http"

	"github.com/MOPDev/mop-backend-api/initializers"
	"github.com/MOPDev/mop-backend-api/internal/logger"
	"github.com/MOPDev/mop-backend-api/models"
	"github.com/gin-gonic/gin"
)

func ErrorLog(c *gin.Context) {
	user, ok := getVerifyUser(c)
	if !ok {
		logger.Warn("ErrorLog: failed to verify user")
		c.JSON(http.StatusInternalServerError, gin.H{})
		return
	}

	var body struct {
		Text string `json:"text"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	currentVal, err := json.Marshal(gin.H{"text": body.Text})
	if err != nil {
		logger.Errorf("ErrorLog: failed to encode log: %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to encode log"})
		return
	}

	entry := models.ActivityLog{
		ActingUserID: user.ID,
		ActionType:   "ERROR LOG",
		CurrentVal:   currentVal,
	}

	if err := initializers.DB.Create(&entry).Error; err != nil {
		logger.Errorf("ErrorLog: failed to save log: %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save log"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "error logged", "error": body.Text})
}
