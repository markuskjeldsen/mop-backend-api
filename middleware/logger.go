package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/markuskjeldsen/mop-backend-api/models"
)

func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		status := c.Writer.Status()
		username := "-"

		if v, ok := c.Get("user"); ok {
			if u, ok := v.(models.User); ok {
				username = u.Username
			}
		}

		end := time.Since(start)

		attributes := []slog.Attr{
			slog.Int("status", status),
			slog.String("method", c.Request.Method),
			slog.String("path", path),
			slog.String("query", query),
			slog.String("ip", c.ClientIP()),
			slog.Duration("latency", end),
			slog.String("user", username),
		}

		if len(c.Errors) > 0 {
			attributes = append(attributes, slog.String("errors", c.Errors.String()))
			slog.LogAttrs(c.Request.Context(), slog.LevelError, "request error", attributes...)
		} else {
			slog.LogAttrs(c.Request.Context(), slog.LevelInfo, "request", attributes...)
		}
	}
}
