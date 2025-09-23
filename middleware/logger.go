package middleware

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/markuskjeldsen/mop-backend-api/models"
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
	429: "Too Many Requests",
	500: "Internal Server Error",
	// Add more as needed
}

func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		status := c.Writer.Status()
		desc := http.StatusText(status)
		if desc == "" {
			desc = "Unknown Status"
		}

		username := "-"
		if v, ok := c.Get("user"); ok {
			if u, ok := v.(models.User); ok && u.Name != "" {
				username = u.Username
			}
		}

		fmt.Printf(
			"%s | user=%s | %3d %s | %v | %s | %s %q\n",
			time.Now().Format("2006/01/02-15:04:05"),
			username,
			status, desc,
			time.Since(start),
			c.ClientIP(),
			c.Request.Method, c.Request.URL.Path,
		)
	}
}
