package api

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v4"

	"github.com/gin-gonic/gin"
	"github.com/markuskjeldsen/mop-backend-api/initializers"
	"github.com/markuskjeldsen/mop-backend-api/internal"
	"github.com/markuskjeldsen/mop-backend-api/middleware"
	"github.com/markuskjeldsen/mop-backend-api/models"
	"golang.org/x/crypto/bcrypt"
)

func Hello(c *gin.Context) {
	c.JSON(200, gin.H{
		"message": "Hello to the API!",
	})
}

func GetUser(c *gin.Context) {
	user, _ := getVerifyUser(c)
	//var user []models.User
	initializers.DB.Find(&user, user.ID) // Preload visits for each user
	user.Password = ""                   // Remove password from the response

	c.JSON(200, gin.H{
		"user": user,
	})
}
func GetUserByParam(c *gin.Context) {
	user, _ := getVerifyUser(c)
	id := c.Param("id")

	//var user []models.User
	initializers.DB.Find(&user, id) // Preload visits for each user
	user.Password = ""              // Remove password from the response

	c.JSON(200, user)
}
func GetUsers(c *gin.Context) {
	var user []models.User
	initializers.DB.Where("id != 1").Find(&user) // Preload visits for each user
	for i := range user {
		user[i].Password = "" // Remove password from the response
	}

	c.JSON(200, gin.H{
		"users": user,
	})
}

func CreateUser(c *gin.Context) {
	actingUser, _ := getVerifyUser(c)

	// get data
	var user models.User
	var body struct {
		FullName string `json:"fullName" form:"fullName" binding:"required"`
		Username string `json:"username" form:"name" binding:"required"`
		Password string `json:"password" form:"password" binding:"required"`
		Email    string `json:"email" form:"email"`
		Rights   string `json:"rights"`
	}

	// bind the data to the user var
	datatype := c.ContentType()

	switch datatype {
	case "application/json":
		if err := c.Bind(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"Status": "ERROR could not bind",
				"user_structure": map[string]string{
					"username": "string",
					"password": "string",
				},
				"error": err.Error(),
			})
			return
		}
	case "application/x-www-form-urlencoded":
		if err := c.ShouldBind(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"Status": "ERROR could not bind",
				"user_structure": map[string]string{
					"username": "string",
					"password": "string",
				},
				"error": err.Error(),
			})
			return
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), 10)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "couldnt generate password hash",
			"error":  err.Error(),
		})
		return
	}
	switch body.Rights {
	case "admin":
		user.Rights = models.RightsAdmin
	case "user":
		user.Rights = models.RightsUser
	default:
		user.Rights = models.RightsUser
	}

	user.Name = body.FullName
	user.Username = body.Username
	user.Email = body.Email

	user.Password = string(hash)

	result := initializers.DB.Create(&user)
	if result.Error != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "Error with Database",
			"error":  result.Error.Error(),
		})
		return
	}

	internal.LogUserCreate(actingUser, user)

	switch datatype {
	case "application/json":
		c.JSON(http.StatusOK, gin.H{
			"message": "sucessfully created new user",
			"user":    user,
		})
	case "application/x-www-form-urlencoded":
		c.Redirect(http.StatusFound, "/")
		c.Set("message", "created new user, head to login to login and see your profile")
	}
}

func Login(c *gin.Context) {
	// bind the data from req

	bodydata, _ := c.Get("body")
	body := bodydata.(middleware.BodyLogin)
	attemptID, _ := c.Get("attemptID")

	// if it goes wrong after these, then its because the user dosnt exist or the
	initializers.DB.Model(&models.LoginAttempt{}).
		Where("id = ?", attemptID).
		Update("failure_reason", "User does not exist")

	var user models.User
	fmt.Println("Username:", body.Username)
	initializers.DB.First(&user, "username = ?", body.Username)
	if user.ID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "ERROR could find user",
		})
		return
	}
	initializers.DB.Model(&models.LoginAttempt{}).
		Where("id = ?", attemptID).
		Update("failure_reason", "Incorrect password")
	err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(body.Password))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "ERROR could find user",
		})
		return
	}

	// generate JWT token

	initializers.DB.Model(&models.LoginAttempt{}).
		Where("id = ?", attemptID).
		Update("failure_reason", "failure to create token")

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": user.ID,
		"exp": time.Now().Add(time.Hour * 24 * 7).Unix(),
	})

	tokenString, err := token.SignedString([]byte(os.Getenv("JWT_secret")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"Status": "failed to create token",
			"error":  err.Error(),
		})
		return
	}

	if os.Getenv("PRODUCTION") == "true" {
		c.SetCookie("Authorization", tokenString, 3600*24*7, "/", "mopsrv03", true, true) // should be true, true in prod
		c.SetSameSite(http.SameSiteLaxMode)
	} else {
		//          name            value         age     path domain secure, httpOnly
		c.SetCookie("Authorization", tokenString, 3600*17, "/", os.Getenv("ALLOW_ORIGIN"), false, true) // should be true, true in prod
		c.SetSameSite(http.SameSiteNoneMode)
	}
	datatype := c.ContentType()

	switch datatype {
	case "application/json":
		// return JWT token
		c.JSON(http.StatusOK, gin.H{
			"token":   tokenString, //type tokenString if important
			"message": "sucessfully logged in",
			"user":    user,
		})
	case "application/x-www-form-urlencoded":
		c.Redirect(http.StatusFound, "/profile")
	}

	initializers.DB.Model(&models.LoginAttempt{}).
		Where("id = ?", attemptID).
		Update("failure_reason", "None").
		Update("successful", true)
}

func Logout(c *gin.Context) {
	// remove the cookie
	c.SetCookie("Authorization", "", -1, "", "", false, true)
	c.JSON(http.StatusOK, gin.H{
		"message": "sucessfully logged out",
	})
}

func Patch(c *gin.Context) {
	actingUser, _ := getVerifyUser(c)
	id := c.Param("id")

	/*
		var userPatch struct {
			Username string `json:"username,omitempty"`
			Password string `json:"password,omitempty"`
			Rights   string `json:"rights,omitempty"`
			Email    string `json:"email,omitempty"`
			Phone    string `json:"phone,omitempty"`
		}
	*/
	var user models.User
	var oldUserInfo models.User

	// Bind the JSON to userin
	if err := c.ShouldBindBodyWithJSON(&oldUserInfo); err != nil {
		c.JSON(400, gin.H{"error": "Invalid input"})
		return
	}

	// Find the user by ID
	if err := initializers.DB.First(&user, id).Error; err != nil {
		c.JSON(404, gin.H{"error": "User not found"})
		return
	}

	//only allow updating chosen fields
	updates := map[string]interface{}{
		"Email": oldUserInfo.Email,
		"Phone": oldUserInfo.Phone,
		"Name":  oldUserInfo.Name,
	}
	olduser := user

	// Update the user fields
	if err := initializers.DB.Model(&user).Updates(updates).Error; err != nil {
		c.JSON(500, gin.H{"error": "Failed to update user"})
		return
	}
	internal.LogUserPatch(actingUser, olduser, user)

	c.JSON(200, user)
}

func DeleteUser(c *gin.Context) {
	actingUser, _ := getVerifyUser(c)
	var user models.User

	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing user id"})
		return
	}

	initializers.DB.Find(&user, id)
	if err := initializers.DB.Delete(&models.User{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	internal.LogUserDelete(actingUser, user)
	c.Status(http.StatusNoContent)
}

func ChangePassword(c *gin.Context) {
	actingUser, _ := getVerifyUser(c)
	var user models.User

	var body struct {
		NewPassword string `json:"new_password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing user id"})
		return
	}

	if err := initializers.DB.First(&user, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if actingUser.ID != user.ID && actingUser.Rights != models.RightsDeveloper {
		c.JSON(http.StatusForbidden, gin.H{"message": "Cannot change another users password"})
		return
	}

	/* validate password if the request was made by api TODO:make this function
	if !validateStrength(body.NewPassword) { // implement same rules as FE
		c.JSON(http.StatusBadRequest, gin.H{"error": "weak password"})
		return
	}
	*/

	// changes the users password

	// calculate the hash
	hash, err := bcrypt.GenerateFromPassword([]byte(body.NewPassword), 10)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "couldnt generate password hash",
			"error":  err.Error(),
		})
		return
	}
	olduser := user

	// assign hash to user.password and merge with database
	result := initializers.DB.Model(&user).Update("password", string(hash))
	if result.Error != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "Error with Database",
			"error":  result.Error.Error(),
		})
		return
	}

	internal.LogUserPatch(actingUser, olduser, user)
	c.Status(http.StatusNoContent)
}
