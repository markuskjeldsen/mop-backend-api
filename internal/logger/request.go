package logger

import (
	"fmt"
	"strings"
	"time"

	"github.com/MOPDev/mop-backend-api/models"
	"github.com/gin-gonic/gin"
)

func colorStatus(status int) string {
	switch {
	case status >= 500:
		return fmt.Sprintf("\033[31m%d\033[0m", status) // red
	case status >= 400:
		return fmt.Sprintf("\033[33m%d\033[0m", status) // yellow
	case status >= 300:
		return fmt.Sprintf("\033[36m%d\033[0m", status) // cyan
	default:
		return fmt.Sprintf("\033[32m%d\033[0m", status) // green
	}
}

func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path

		// remove the logging on specific paths
		// c.Set("geoblocked", true)

		geoblock, _ := c.Get("geoblocked")
		skip, _ := geoblock.(bool)
		if strings.HasPrefix(path, "/api/v1/tiles") || strings.HasPrefix(path, "/api/v1/geocode") || skip {
			c.Next()
			return
		}

		start := time.Now()
		query := c.Request.URL.RawQuery

		c.Next()

		status := c.Writer.Status()
		username := "-"

		if v, ok := c.Get("user"); ok {
			if u, ok := v.(models.User); ok {
				username = u.Username
			}
		}

		shortPath := strings.TrimPrefix(path, "/api/v1")
		if shortPath == "" {
			shortPath = "/"
		}

		latency := time.Since(start).Round(time.Microsecond)

		line := fmt.Sprintf("%s | s=%s | m=%-4s | p=%-35s | L=%-12s | u=%s",
			time.Now().Format("2006/01/02 15:04:05"),
			colorStatus(status),
			c.Request.Method,
			shortPath,
			time.Since(start).Round(time.Microsecond),
			username,
		)
		if latency > 500*time.Millisecond {
			line += " | ⚠ SLOW"
		}

		if query != "" {
			line += " | q=" + query
		}
		if len(c.Errors) > 0 {
			line += " | err=" + c.Errors.String()
		}
		fmt.Println(line)
	}
}
