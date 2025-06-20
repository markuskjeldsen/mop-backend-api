package middleware

import (
	"os"

	"github.com/gin-gonic/gin"
)

func CORSMiddleware(c *gin.Context) {
	// Set CORS headers
	if os.Getenv("PRODUCTION") == "true" {
		c.Writer.Header().Set("Access-Control-Allow-Origin", os.Getenv("ALLOW-ORIGIN")) // Change to specific origin if needed
	} else {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "http://localhost:5137")
	}

	c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	c.Writer.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization")
	c.Writer.Header().Set("Access-Control-Allow-Credentials", "true") // delete if not needed

	if c.Request.Method == "OPTIONS" {
		c.AbortWithStatus(204)
		return
	}
	c.Next()
}
