package middleware

import (
	"os"

	"github.com/gin-gonic/gin"
)

func CORSMiddleware(c *gin.Context) {
	// Set CORS headers

	c.Writer.Header().Set("Access-Control-Allow-Origin", os.Getenv("ALLOW_ORIGIN")) // Change to specific origin if needed
	c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	c.Writer.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization")
	c.Writer.Header().Set("Access-Control-Allow-Credentials", "true") // delete if not needed

	if c.Request.Method == "OPTIONS" {
		c.AbortWithStatus(204)
		return
	}
	c.Next()
}
