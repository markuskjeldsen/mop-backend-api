package middleware

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
)

var statusText = map[int]string{
	200: "OK",
	201: "Created",
	204: "No Content",
	400: "Bad Request",
	401: "Unauthorized (Access Denied)",
	403: "Forbidden",
	404: "Not Found",
	422: "Unprocessable Entity",
	500: "Internal Server Error",
	// Add more as needed
}

func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Start time
		start := time.Now()

		// Process request
		c.Next()

		// End time
		duration := time.Since(start)

		// Get status code and description
		status := c.Writer.Status()
		desc, ok := statusText[status]
		if !ok {
			desc = "Unknown Status"
		}

		// Print log line
		fmt.Printf("[GIN] %s | %d - %s | %v | %s | %s %q\n",
			time.Now().Format("2006/01/02 - 15:04:05"),
			status, desc,
			duration,
			c.ClientIP(),
			c.Request.Method, c.Request.URL.Path)
	}
}
