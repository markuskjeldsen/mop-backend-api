package middleware

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
	"github.com/markuskjeldsen/mop-backend-api/initializers"
	"github.com/markuskjeldsen/mop-backend-api/models"
)

func RequireAuthUser(c *gin.Context) {
	//fmt.Println("auth middleware")

	// get cookie
	tokenString, err := c.Cookie("Authorization")

	if err != nil || tokenString == "" {
		fmt.Println("There is no token")
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"error": "No token provided",
		})
		return
	}

	//decode
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// hmacSampleSecret is a []byte containing your secret, e.g. []byte("my_secret_key")
		return []byte(os.Getenv("JWT_secret")), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil {
		fmt.Println("the decode has failed")
		log.Fatal(err)
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		// check exp
		if float64(time.Now().Unix()) > claims["exp"].(float64) {
			fmt.Println("The token is too old")
			c.AbortWithStatus(http.StatusUnauthorized)
		}
		//find user with token

		// attach to request

		var user models.User
		initializers.DB.First(&user, claims["sub"])
		if user.ID == 0 {
			fmt.Println("The token does not belong to any user")
			var attempt models.AuthAttempt
			attempt.IP = c.ClientIP()
			attempt.FailureReason = "Token does not belong to any user"
			initializers.DB.Create(&attempt)
			c.AbortWithStatus(http.StatusUnauthorized)
		}

		c.Set("user", user)
		c.Next()
	}
}

func RequireAuthAdmin(c *gin.Context) {
	// get cookie
	tokenString, err := c.Cookie("Authorization")
	if err != nil {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	if tokenString == "" {
		fmt.Println("There is no token")
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	//decode
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// hmacSampleSecret is a []byte containing your secret, e.g. []byte("my_secret_key")
		return []byte(os.Getenv("JWT_secret")), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil {
		fmt.Println("the decode has failed")
		log.Fatal(err)
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		// check exp
		if float64(time.Now().Unix()) > claims["exp"].(float64) {
			c.AbortWithStatus(http.StatusUnauthorized)
		}
		//find user with token

		// attach to request

		var user models.User
		initializers.DB.First(&user, claims["sub"])
		if user.ID == 0 {
			var attempt models.AuthAttempt
			attempt.IP = c.ClientIP()
			attempt.FailureReason = "Token does not belong to any user"
			initializers.DB.Create(&attempt)
			c.AbortWithStatus(http.StatusUnauthorized)
		}

		if user.Rights != "admin" && user.Rights != "developer" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Not high enough rights"})
			return
		}
		c.Set("user", user)
		c.Next()
	}
}
