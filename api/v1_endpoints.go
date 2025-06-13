package api

import (
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
	initializers.DB.Preload("Visits").Find(&users)

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
		initializers.DB.Preload("Visits").Preload("VisitResponses").First(&users, user.ID)
	}
	if user.Rights == models.RightsDeveloper || user.Rights == models.RightsAdmin {
		// Fetch all users and preload VisitResponses
		initializers.DB.Preload("Visits").Preload("VisitResponses").Find(&users)
	}

	// Structure the data as desired

	var response []gin.H
	for _, u := range users {
		response = append(response, gin.H{
			"ID":              u.ID,
			"username":        u.Username,
			"email":           u.Email,
			"phone":           u.Phone,
			"visits":          u.Visits,
			"visit_responses": u.VisitResponses,
		})
	}

	c.JSON(
		http.StatusOK,
		gin.H{
			"status": "sucess",
			"users":  response,
		})
}

func Create_response(c *gin.Context) {
	// this function creates a response and then marks the given visit as visited and returns the response

	user, ok := getVerifyUser(c)
	if !ok {
		c.JSON(
			http.StatusInternalServerError,
			gin.H{"message": "something went wrong"})
		return
	}

	contentType := c.ContentType()
	var visit_resp models.VisitResponse
	if contentType == "application/JSON" {
		c.ShouldBindBodyWithJSON(&visit_resp)
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
	// we have revived the visit response
	visit_resp.UserID = user.ID

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
		// handle the error
		return
	}

	c.JSON(http.StatusOK,
		gin.H{
			"message":        "everything went well",
			"visit response": visit_resp,
		})
}
