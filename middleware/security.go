package middleware

import (
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/MOPDev/mop-backend-api/initializers"
	"github.com/MOPDev/mop-backend-api/internal/logger"
	"github.com/MOPDev/mop-backend-api/models"
	"github.com/gin-gonic/gin"
	"github.com/oschwald/geoip2-golang"
	"gorm.io/gorm"
)

type BodyLogin struct {
	Username string `json:"username" form:"username" binding:"required"`
	Password string `json:"password" form:"password" binding:"required"`
}

func LoginLogCleanup() {
	go func() {
		t := time.NewTicker(6 * time.Hour)
		for range t.C {
			initializers.DB.Where("created_at < ?", time.Now().Add(-7*24*time.Hour)).
				Delete(&models.LoginAttempt{})
		}
	}()
}

func LoginAttemptLog(c *gin.Context) {
	var body BodyLogin
	datatype := c.ContentType()

	switch datatype {
	case "application/json":
		if err := c.ShouldBindJSON(&body); err != nil { //before it was c.Bind
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}
	case "application/x-www-form-urlencoded":
		if err := c.ShouldBind(&body); err != nil { //before it was c.Bind
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}
	default:
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	// setting up some logging
	c.Set("body", body)
	addr := c.ClientIP()
	var attempt models.LoginAttempt
	attempt.Username = body.Username
	attempt.IP = addr
	attempt.Successful = false
	attempt.FailureReason = "Failed to bind values"

	if isRateLimited(initializers.DB, body.Username, addr) {
		attempt.FailureReason = "Too many requests"
		if result := initializers.DB.Create(&attempt); result.Error != nil {
			logger.Errorf("failed to log rate-limited login attempt: %s", result.Error.Error())
		}
		c.AbortWithStatus(http.StatusTooManyRequests)
		return
	}

	if result := initializers.DB.Create(&attempt); result.Error != nil {
		logger.Errorf("failed to log login attempt: %s", result.Error.Error())
	}
	c.Set("attemptID", attempt.ID)
	attempt.FailureReason = "Failed to bind values"
	c.Next()
}

func isRateLimited(db *gorm.DB, username, ip string) bool {
	// Each tier: {max_failures, window}
	// Exponential: 5 fails/1min, 10/5min, 15/30min, 20/12hr
	tiers := []struct {
		limit  int64
		window time.Duration
	}{
		{5, 1 * time.Minute},
		{10, 5 * time.Minute},
		{15, 30 * time.Minute},
		{20, 12 * time.Hour},
	}

	for _, tier := range tiers {
		var userCount, ipCount int64
		since := time.Now().Add(-tier.window)

		db.Model(&models.LoginAttempt{}).
			Where("username = ? AND created_at > ?", username, since).
			Count(&userCount)

		db.Model(&models.LoginAttempt{}).
			Where("ip = ? AND created_at > ?", ip, since).
			Count(&ipCount)

		if userCount >= tier.limit || ipCount >= int64(float64(tier.limit)*1.5) {
			return true
		}
	}
	return false
}

func isLocalIP(ip net.IP) bool {
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast())
}

func isBannedIP(ip net.IP) bool {
	var attempt models.AuthAttempt
	initializers.DB.Where("ip = ? AND created_at > ?", ip.String(), time.Now().Add(-12*time.Hour)).First(&attempt)
	return attempt.ID != 0 // if id is not zero then its banned
}

func GeoIPBlocker(allowedCountry string, dbFile string) gin.HandlerFunc {
	db, err := geoip2.Open(dbFile)
	if err != nil {
		log.Fatalf("GeoIPBlocker: failed to open %s: %v", dbFile, err)
	}
	return func(c *gin.Context) {
		ip := net.ParseIP(c.ClientIP())
		if (os.Getenv("PRODUCTION")) != "True" && len(c.GetHeader("REAL-IP")) > 4 {
			ip = net.ParseIP(c.GetHeader("REAL-IP"))
		}
		if isLocalIP(ip) {
			c.Next()
			return
		}
		if isBannedIP(ip) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}

		record, err := db.Country(ip)
		if err != nil {
			c.AbortWithStatusJSON(500, gin.H{"error": "geo lookup failed"})
			return
		}
		if record == nil {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		if record.Country.IsoCode != allowedCountry {
			name := record.Country.Names["en"]
			if name == "" {
				name = record.Country.IsoCode
			}
			c.Set("geoblocked", true)
			//logger.Warnf("IP: %s Country: %s (%s)", ip, name, record.Country.IsoCode)
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Next()
	}
}
