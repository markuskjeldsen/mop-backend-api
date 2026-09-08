package middleware

import (
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/MOPDev/mop-backend-api/initializers"
	"github.com/MOPDev/mop-backend-api/internal/logger"
	"github.com/MOPDev/mop-backend-api/models"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
)

func RequireAuthUser(c *gin.Context) {
	//logger.Info("auth middleware") this is both auditor and office

	// get cookie
	tokenString, err := c.Cookie("Authorization")

	if err != nil || tokenString == "" {
		logger.Info("There is no token")
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"error": "No token provided",
		})
		return
	}

	//decode
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		return []byte(os.Getenv("JWT_secret")), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))

	if err != nil {
		switch {
		case errors.Is(err, jwt.ErrTokenExpired):
			logger.Info("Token expired")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token_expired"})
		default:
			logger.Info("Token is invalid: " + err.Error())
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token_invalid"})
		}
		return
	}

	if !token.Valid {
		logger.Info("Token is invalid: parsed but not valid")
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token_invalid"})
		return
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		// check exp
		if float64(time.Now().Unix()) > claims["exp"].(float64) {
			logger.Info("The token is too old")
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		//find user with token

		// attach to request

		var user models.User
		initializers.DB.First(&user, claims["sub"])
		if user.ID == 0 {
			logger.Warn("The token does not belong to any user")
			var attempt models.AuthAttempt
			attempt.IP = c.ClientIP()
			attempt.FailureReason = "Token does not belong to any user"
			if result := initializers.DB.Create(&attempt); result.Error != nil {
				logger.Errorf("failed to log auth attempt: %s", result.Error.Error())
			}
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		c.Set("user", user)
		c.Next()
	} else {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

}

func RequireAuthOfficeWorker(c *gin.Context) {
	// get cookie
	tokenString, err := c.Cookie("Authorization")
	if err != nil {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	if tokenString == "" {
		logger.Info("There is no token")
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	//decode
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// hmacSampleSecret is a []byte containing your secret, e.g. []byte("my_secret_key")
		return []byte(os.Getenv("JWT_secret")), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		// check exp
		if float64(time.Now().Unix()) > claims["exp"].(float64) {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		//find user with token

		// attach to request

		var user models.User
		initializers.DB.First(&user, claims["sub"])
		if user.ID == 0 {
			logger.Warn("The token does not belong to any user")
			var attempt models.AuthAttempt
			attempt.IP = c.ClientIP()
			attempt.FailureReason = "Token does not belong to any user"
			if result := initializers.DB.Create(&attempt); result.Error != nil {
				logger.Errorf("failed to log auth attempt: %s", result.Error.Error())
			}
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		if user.Rights != models.RightsAdmin &&
			user.Rights != models.RightsDeveloper &&
			user.Rights != models.RightsOfficeWorker {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Not high enough rights"})
			return
		}
		c.Set("user", user)
		c.Next()
	} else {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

}

func RequireAuthAuditor(c *gin.Context) {
	// get cookie
	tokenString, err := c.Cookie("Authorization")
	if err != nil {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	if tokenString == "" {
		logger.Info("There is no token")
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	//decode
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// hmacSampleSecret is a []byte containing your secret, e.g. []byte("my_secret_key")
		return []byte(os.Getenv("JWT_secret")), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		// check exp
		if float64(time.Now().Unix()) > claims["exp"].(float64) {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		//find user with token

		// attach to request

		var user models.User
		initializers.DB.First(&user, claims["sub"])
		if user.ID == 0 {
			var attempt models.AuthAttempt
			attempt.IP = c.ClientIP()
			attempt.FailureReason = "Token does not belong to any user"
			if result := initializers.DB.Create(&attempt); result.Error != nil {
				logger.Errorf("failed to log auth attempt: %s", result.Error.Error())
			}
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		// if auditor is permitted then admin and dev are aswell but not office worker
		if user.Rights != models.RightsAdmin &&
			user.Rights != models.RightsDeveloper &&
			user.Rights != models.RightsAuditor {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Not high enough rights"})
			return
		}
		c.Set("user", user)
		c.Next()
	} else {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
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
		logger.Info("There is no token")
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	//decode
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// hmacSampleSecret is a []byte containing your secret, e.g. []byte("my_secret_key")
		return []byte(os.Getenv("JWT_secret")), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		// check exp
		if float64(time.Now().Unix()) > claims["exp"].(float64) {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		//find user with token

		// attach to request

		var user models.User
		initializers.DB.First(&user, claims["sub"])
		if user.ID == 0 {
			var attempt models.AuthAttempt
			attempt.IP = c.ClientIP()
			attempt.FailureReason = "Token does not belong to any user"
			if result := initializers.DB.Create(&attempt); result.Error != nil {
				logger.Errorf("failed to log auth attempt: %s", result.Error.Error())
			}
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		if user.Rights != models.RightsAdmin &&
			user.Rights != models.RightsDeveloper {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Not high enough rights"})
			return
		}
		c.Set("user", user)
		c.Next()
	} else {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

}
