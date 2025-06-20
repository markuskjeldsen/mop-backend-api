package api

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/markuskjeldsen/mop-backend-api/initializers"
	"github.com/markuskjeldsen/mop-backend-api/models"
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

func GetVisits(c *gin.Context) {
	var users []models.User
	user, ok := getVerifyUser(c)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{})
	}

	if user.Rights == models.RightsUser {
		initializers.DB.Preload("Visits").Preload("Visits.Debitors").Find(&users, user.ID)
	} else if user.Rights == models.RightsAdmin || user.Rights == models.RightsDeveloper {
		initializers.DB.Preload("Visits").Preload("Visits.Debitors").Find(&users)
	}
	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"users":  users,
	})
}

func Visit_responses(c *gin.Context) {
	user, ok := getVerifyUser(c)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{})
	}

	var users []models.User
	if user.Rights == models.RightsUser {
		// Fetch the user and preload VisitResponses
		initializers.DB.Preload("Visits").Preload("Visits.Debitors").Preload("Visits.VisitResponse").First(&users, user.ID)
	}
	if user.Rights == models.RightsDeveloper || user.Rights == models.RightsAdmin {
		// Fetch all users and preload VisitResponses
		initializers.DB.Preload("Visits").Preload("Visits.Debitors").Preload("Visits.VisitResponse").Find(&users)
	}

	c.JSON(
		http.StatusOK,
		gin.H{
			"status": "sucess",
			"users":  users,
		})
}

func Create_response(c *gin.Context) {
	// this function creates a response and then marks the given visit as visited and returns the response

	_, ok := getVerifyUser(c) // perhaps use user but rn there is no use
	if !ok {
		c.JSON(
			http.StatusInternalServerError,
			gin.H{"message": "something went wrong"})
		return
	}

	contentType := c.ContentType()
	var visit_resp models.VisitResponse
	if contentType == "application/json" {
		if err := c.ShouldBindBodyWithJSON(&visit_resp); err != nil {
			fmt.Println(err)
			c.JSON(http.StatusBadRequest, gin.H{
				"status":  "Error",
				"message": "Invalid JSON: " + err.Error(),
				"msg":     "either the data type is wrong or the data is missing",
			})
			return
		}
	} else if contentType == "x-www-form-urlencoded" {
		c.JSON(http.StatusNotImplemented, gin.H{
			"status":  "Error",
			"message": "Content type not supported",
		})
		return
	} else {
		c.JSON(http.StatusNotImplemented, gin.H{
			"status":  "Error",
			"message": "Content type not supported",
		})
		return
	}

	// we have revived the visit response, now check if
	result := initializers.DB.Create(&visit_resp)
	if result.Error != nil {
		c.JSON(
			http.StatusInternalServerError,
			gin.H{
				"error":   result.Error.Error(),
				"message": "Something went wrong",
			},
		)
		return
	}

	// Update the corresponding Visit record
	var visit models.Visit
	result = initializers.DB.Model(&visit).Where("id = ?", visit_resp.VisitID).Update("visited", true)
	if result.Error != nil {
		c.JSON(http.StatusOK,
			gin.H{
				"message":        "An error ocurred updating the visit",
				"visit response": result.Error.Error(),
			})
		return
	}

	c.JSON(http.StatusOK,
		gin.H{
			"message":        "everything went well",
			"visit response": visit_resp,
		})
}
