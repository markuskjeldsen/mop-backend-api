package middleware

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/markuskjeldsen/mop-backend-api/initializers"
	"github.com/markuskjeldsen/mop-backend-api/models"
	"github.com/oschwald/geoip2-golang"
)

type BodyLogin struct {
	Username string `json:"username" form:"username" binding:"required"`
	Password string `json:"password" form:"password" binding:"required"`
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

	c.Set("body", body)
	addr := c.ClientIP()
	var attempt models.LoginAttempt
	attempt.Username = body.Username
	attempt.IP = addr
	attempt.Successful = false
	attempt.FailureReason = "Failed to bind values"

	var count int64
	initializers.DB.Model(&models.LoginAttempt{}).
		Where("username = ? AND ip = ? AND created_at > ?", body.Username, addr, time.Now().Add(-12*time.Hour)).
		Count(&count)
	if count >= 100 {
		attempt.FailureReason = "Too many requests"
		initializers.DB.Create(&attempt)
		c.AbortWithStatus(http.StatusTooManyRequests)
		return
	}

	initializers.DB.Create(&attempt)
	c.Set("attemptID", attempt.ID)
	attempt.FailureReason = "Failed to bind values"
	c.Next()
}

func isLocalIP(ip net.IP) bool {
	// Loopback
	//strIp := ip.To4()
	//fmt.Println(strIp)
	if ip.IsLoopback() {
		return true
	}
	// IPv4: 10.0.0.0/8
	if ip.To4() != nil && ip[0] == 10 {
		return true
	}
	// IPv4: 192.168.0.0/16
	if ip.To4() != nil && ip[0] == 192 && ip[1] == 168 {
		return true
	}
	// IPv4: 172.16.0.0/12
	if ip.To4() != nil && ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31 {
		return true
	}
	// IPv6 Unique local
	if ip.IsPrivate() { // Go 1.17+
		return true
	}
	return false
}

func isBannedIP(ip net.IP) bool {
	var attempt models.AuthAttempt
	initializers.DB.First(&attempt).Where("ip = ? AND created_at > ?", ip, time.Now().Add(-12*time.Hour))
	return attempt.ID != 0 // if id is not zero then its banned
}

func GeoIPBlocker(allowedCountry string, dbFile string) gin.HandlerFunc {
	db, _ := geoip2.Open(dbFile)
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
			c.AbortWithStatusJSON(403, gin.H{"error": "Access forbidden"})
			return
		}

		record, err := db.Country(ip)
		if err != nil {
			c.AbortWithStatusJSON(500, gin.H{"error": "geo lookup failed"})
			return
		}
		if record == nil || record.Country.IsoCode != allowedCountry {
			name := record.Country.Names["en"] // or pick from Accept-Language
			if name == "" {
				name = record.Country.IsoCode
			}
			fmt.Printf("IP: %s Country: %s (%s)\n", ip, name, record.Country.IsoCode)
			c.AbortWithStatusJSON(403, gin.H{"error": "Access forbidden"})
			return
		}
		c.Next()
	}
}
