package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/MOPDev/mop-backend-api/initializers"
	"github.com/MOPDev/mop-backend-api/internal"
	"github.com/MOPDev/mop-backend-api/internal/logger"
	"github.com/MOPDev/mop-backend-api/internal/tsp"
	"github.com/MOPDev/mop-backend-api/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// getRouteSetting returns the single route-setting row, creating it with
// defaults on first access.
func getRouteSetting() (models.RouteSetting, error) {
	var s models.RouteSetting
	if err := initializers.DB.First(&s).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			s = models.RouteSetting{StartTime: "13:00", ServiceMinutes: 15, EndTime: "20:00", Anchor: "start"}
			if err := initializers.DB.Create(&s).Error; err != nil {
				return s, err
			}
			return s, nil
		}
		return s, err
	}
	return s, nil
}

func GetRouteSettings(c *gin.Context) {
	s, err := getRouteSetting()
	if err != nil {
		logger.Errorf("GetRouteSettings: get failed: %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, s)
}

func PatchRouteSettings(c *gin.Context) {
	var input struct {
		StartTime      *string `json:"start_time"`
		ServiceMinutes *uint   `json:"service_minutes"`
		EndTime        *string `json:"end_time"`
		Anchor         *string `json:"anchor"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid data"})
		return
	}

	s, err := getRouteSetting()
	if err != nil {
		logger.Errorf("getRouteSettings: get failed: %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if input.StartTime != nil {
		if _, err := time.Parse("15:04", *input.StartTime); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "start_time must be HH:MM"})
			return
		}
		s.StartTime = *input.StartTime
	}
	if input.ServiceMinutes != nil {
		s.ServiceMinutes = *input.ServiceMinutes
	}
	if input.EndTime != nil {
		if _, err := time.Parse("15:04", *input.EndTime); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "end_time must be HH:MM"})
			return
		}
		s.EndTime = *input.EndTime
	}
	if input.Anchor != nil {
		if *input.Anchor != "start" && *input.Anchor != "end" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "anchor must be 'start' or 'end'"})
			return
		}
		s.Anchor = *input.Anchor
	}

	if err := initializers.DB.Save(&s).Error; err != nil {
		logger.Errorf("PatchRouteSettings: save failed: %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, s)
}

// computeArrivalTimes schedules the route anchored on start_time (forward:
// first stop at start_time, each next = prev + service + travel) or on
// end_time (backward: last stop arrives at end_time, each prev = next -
// service - travel). Overrun reports when the route does not fit between the
// two bounds.
func computeArrivalTimes(startTime string, serviceMinutes uint, endTime string, anchor string, legTimes []float64) ([]string, bool) {
	service := time.Duration(serviceMinutes) * time.Minute
	start, _ := time.Parse("15:04", startTime)
	end, endErr := time.Parse("15:04", endTime)
	hasEnd := endErr == nil && endTime != ""

	times := make([]time.Time, len(legTimes)+1)
	overrun := false

	if anchor == "end" && hasEnd {
		// backward schedule: last stop lands on end_time
		times[len(times)-1] = end
		for i := len(times) - 2; i >= 0; i-- {
			times[i] = times[i+1].Add(-service).Add(-time.Duration(legTimes[i] * float64(time.Second)))
		}
		if times[0].Before(start) {
			overrun = true // would have to leave before start_time
		}
	} else {
		// forward schedule: first stop at start_time
		times[0] = start
		for i, leg := range legTimes {
			times[i+1] = times[i].Add(service).Add(time.Duration(leg * float64(time.Second)))
		}
		if hasEnd && times[len(times)-1].After(end) {
			overrun = true
		}
	}

	out := make([]string, len(times))
	for i, t := range times {
		out[i] = t.Format("15:04")
	}
	return out, overrun
}

// computeAndStoreRoute fetches road geometry over the group's current stop
// order, computes arrival times from the route settings, persists visit_time
// for every visit (logged via UpdateVisitValue), and stores the route row.
func computeAndStoreRoute(userID uint, groupId uint, costing, mode string) ([]string, float64, float64, bool, error) {
	var visits []models.Visit
	if err := initializers.DB.Where("group_id = ?", groupId).
		Order("stop_nr ASC").Find(&visits).Error; err != nil {
		return nil, 0, 0, false, err
	}
	if len(visits) < 2 {
		return []string{}, 0, 0, false, nil
	}

	waypoints := make([]tsp.Waypoint, 0, len(visits))
	for i, v := range visits {
		lat, _ := strconv.ParseFloat(v.Latitude, 64)
		lon, _ := strconv.ParseFloat(v.Longitude, 64)
		waypoints = append(waypoints, tsp.Waypoint{
			ID:    strconv.FormatUint(uint64(v.ID), 10),
			Label: fmt.Sprintf("%d", i),
			Lat:   lat,
			Lon:   lon,
		})
	}

	result, err := tsp.RouteGeometry(waypoints, costing, mode)
	if err != nil {
		return nil, 0, 0, false, err
	}

	settings, err := getRouteSetting()
	if err != nil {
		return nil, 0, 0, false, err
	}

	times, overrun := computeArrivalTimes(settings.StartTime, settings.ServiceMinutes, settings.EndTime, settings.Anchor, result.LegTimes)

	geometryJSON, _ := json.Marshal(result.Geometry)
	err = initializers.DB.Transaction(func(tx *gorm.DB) error {
		for i, v := range visits {
			interval, _ := visitIntervalRange(times[i])
			if v.VisitTime == times[i] && v.VisitInterval == interval {
				continue
			}
			if v.VisitTime != times[i] {
				if err := internal.UpdateVisitValue(tx, v.ID, times[i], userID, "visit_time"); err != nil {
					return err
				}
			}
			if err := tx.Model(&models.Visit{}).
				Where("id = ?", v.ID).
				Updates(map[string]interface{}{"visit_time": times[i], "visit_interval": interval}).Error; err != nil {
				return err
			}
		}

		// one route row per group: drop any previous row (including a soft
		// deleted tombstone, which would still hold the unique group_id)
		if err := tx.Unscoped().Where("group_id = ?", groupId).
			Delete(&models.VisitRoute{}).Error; err != nil {
			return err
		}
		return tx.Create(&models.VisitRoute{
			GroupID:  groupId,
			Geometry: string(geometryJSON),
			Overrun:  overrun,
		}).Error
	})
	if err != nil {
		return nil, 0, 0, false, err
	}

	return result.Geometry, result.Distance, result.Time, overrun, nil
}

// clearGroupRoute removes the stored route for a group. Order-changing
// operations call this so a stale route is never drawn. Unscoped: a soft
// delete would leave a tombstone row holding the unique group_id, and the
// next compute would fail to recreate it.
func clearGroupRoute(tx *gorm.DB, groupId uint) error {
	return tx.Unscoped().Where("group_id = ?", groupId).Delete(&models.VisitRoute{}).Error
}

func GetGroupRoute(c *gin.Context) {
	groupId, err := strconv.ParseUint(c.Param("groupId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Group ID"})
		return
	}

	var route models.VisitRoute
	err = initializers.DB.Where("group_id = ?", groupId).First(&route).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusOK, gin.H{"geometry": []string{}, "overrun": false})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var geometry []string
	if err := json.Unmarshal([]byte(route.Geometry), &geometry); err != nil {
		geometry = []string{}
	}
	c.JSON(http.StatusOK, gin.H{"geometry": geometry, "overrun": route.Overrun})
}

// RecomputeGroupRoute recomputes geometry + arrival times for the current
// order without re-optimizing. Used when auto-recompute is on after a
// reorder/split/join.
func RecomputeGroupRoute(c *gin.Context) {
	user, ok := getVerifyUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "Unauthorized"})
		return
	}

	groupId, err := strconv.ParseUint(c.Param("groupId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Group ID"})
		return
	}

	var input struct {
		Costing string `json:"costing"`
		Mode    string `json:"mode"`
	}
	_ = c.ShouldBindJSON(&input)
	costing := input.Costing
	if costing == "" {
		costing = "auto"
	}
	mode := input.Mode
	if mode == "" {
		mode = "distance"
	}

	geometry, _, _, overrun, err := computeAndStoreRoute(user.ID, uint(groupId), costing, mode)
	if err != nil {
		logger.Errorf("RecomputeGroupRoute: %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"geometry": geometry, "overrun": overrun})
}
