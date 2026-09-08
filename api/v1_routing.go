package api

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/MOPDev/mop-backend-api/initializers"
	"github.com/MOPDev/mop-backend-api/internal"
	"github.com/MOPDev/mop-backend-api/internal/logger"
	"github.com/MOPDev/mop-backend-api/internal/tsp"
	"github.com/MOPDev/mop-backend-api/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func derefUint(number *uint) uint {
	if number == nil {
		return 0
	}
	return *number
}

func fixRouteOrder(tx *gorm.DB, groupId uint, insertedVisitID uint) error {
	if tx == nil {
		tx = initializers.DB // ponytail: fallback for callers with no active transaction
	}

	var visits []models.Visit
	if err := tx.Where("group_id = ?", groupId).
		Order("stop_nr ASC").
		Find(&visits).Error; err != nil {
		return err
	}

	// pull the inserted visit out, re-insert before the last one
	ordered := make([]models.Visit, 0, len(visits))
	var inserted *models.Visit
	for i := range visits {
		if visits[i].ID == insertedVisitID {
			inserted = &visits[i]
			continue
		}
		ordered = append(ordered, visits[i])
	}
	if inserted == nil {
		return fmt.Errorf("visit %d not in group %d", insertedVisitID, groupId)
	}

	if len(ordered) == 0 {
		ordered = append(ordered, *inserted)
	} else {
		last := len(ordered) - 1
		ordered = append(ordered[:last], append([]models.Visit{*inserted}, ordered[last:]...)...)
	}

	for i := range ordered {
		stop := uint(i)
		if err := tx.Model(&models.Visit{}).
			Where("id = ?", ordered[i].ID).
			Update("stop_nr", stop).Error; err != nil {
			return err
		}
	}
	return nil
}

// ReorderVisit swaps stop_nr with the neighbour above or below inside the group.
// body: {"visitId": 12, "direction": "up"|"down"}
func ReorderVisit(c *gin.Context) {
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
		VisitId   uint   `json:"visitId" binding:"required"`
		Direction string `json:"direction" binding:"required,oneof=up down"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "visitId and direction (up|down) are required"})
		return
	}

	err = initializers.DB.Transaction(func(tx *gorm.DB) error {
		// lock the rows of the group so two users cannot swap at the same time
		var visit models.Visit
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("group_id = ? AND id = ?", groupId, input.VisitId).
			First(&visit).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("visit not found in this group")
			}
			return err
		}

		if visit.Stopnr == nil {
			return errors.New("visit has no stop_nr")
		}

		// find the neighbour: the nearest stop_nr above or below
		var neighbour models.Visit
		q := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("group_id = ? AND id != ?", groupId, visit.ID)

		if input.Direction == "up" {
			q = q.Where("stop_nr < ?", *visit.Stopnr).Order("stop_nr DESC")
		} else {
			q = q.Where("stop_nr > ?", *visit.Stopnr).Order("stop_nr ASC")
		}

		if err := q.First(&neighbour).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("visit is already at the end of the route")
			}
			return err
		}

		if derefUint(visit.SegmentIndex) != derefUint(neighbour.SegmentIndex) {
			return errors.New("cannot reorder across a segment boundary")
		}

		visitStop, neighbourStop := *visit.Stopnr, *neighbour.Stopnr

		// log both changes before the update
		if err := internal.UpdateVisitValue(tx, visit.ID, fmt.Sprintf("%d", neighbourStop), user.ID, "stop_nr"); err != nil {
			return err
		}
		if err := internal.UpdateVisitValue(tx, neighbour.ID, fmt.Sprintf("%d", visitStop), user.ID, "stop_nr"); err != nil {
			return err
		}

		// park visit out of the way so a unique (group_id, stop_nr) index
		// does not trip mid-swap
		if err := tx.Model(&models.Visit{}).Where("id = ?", visit.ID).
			Update("stop_nr", 0).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.Visit{}).Where("id = ?", neighbour.ID).
			Update("stop_nr", visitStop).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.Visit{}).Where("id = ?", visit.ID).
			Update("stop_nr", neighbourStop).Error; err != nil {
			return err
		}

		// verify the invariant on the whole group, not just the shifted tail.
		// a return here rolls the transaction back.
		var all []models.Visit
		if err := tx.Where("group_id = ?", groupId).Find(&all).Error; err != nil {
			return err
		}
		if err := assertSegmentsValid(all); err != nil {
			return err
		}
		return clearGroupRoute(tx, uint(groupId))
	})

	if err != nil {
		logger.Errorf("ReorderVisit: %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Route order updated successfully"})
}

// SplitSegment starts a new segment at this visit. The visit and every visit
// after it (in stop_nr order) get segment_index + 1.
// body: {"visitId": 12}
func SplitSegment(c *gin.Context) {
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
		VisitId uint `json:"visitId" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "visitId is required"})
		return
	}

	err = initializers.DB.Transaction(func(tx *gorm.DB) error {
		var visit models.Visit
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("group_id = ? AND id = ?", groupId, input.VisitId).
			First(&visit).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("visit not found in this group")
			}
			return err
		}

		if visit.Stopnr == nil {
			return errors.New("visit has no stop_nr")
		}

		// the first stop cannot start a new segment, there is nothing before it
		var before int64
		if err := tx.Model(&models.Visit{}).
			Where("group_id = ? AND stop_nr < ?", groupId, *visit.Stopnr).
			Count(&before).Error; err != nil {
			return err
		}
		if before == 0 {
			return errors.New("cannot split at the first stop of the route")
		}

		// all visits from this stop and onwards
		var affected []models.Visit
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("group_id = ? AND stop_nr >= ?", groupId, *visit.Stopnr).
			Order("stop_nr ASC").Find(&affected).Error; err != nil {
			return err
		}

		for _, v := range affected {
			newIndex := uint(1)
			if v.SegmentIndex != nil {
				newIndex = *v.SegmentIndex + 1
			}
			if err := internal.UpdateVisitValue(tx, v.ID, fmt.Sprintf("%d", newIndex), user.ID, "segment_index"); err != nil {
				return err
			}
		}

		// MAX keeps segment_index at 0 or higher
		if err := tx.Model(&models.Visit{}).
			Where("group_id = ? AND stop_nr >= ?", groupId, *visit.Stopnr).
			Update("segment_index", gorm.Expr("COALESCE(segment_index, 0) + 1")).Error; err != nil {
			return err
		} // IF DATABASE CHANGE TO SOMETHING ELSE THEN SQLITE THEN USE GREATEST() INSTEAD OF MAX

		// verify the invariant on the whole group, not just the shifted tail.
		// a return here rolls the transaction back.
		var all []models.Visit
		if err := tx.Where("group_id = ?", groupId).Find(&all).Error; err != nil {
			return err
		}
		if err := assertSegmentsValid(all); err != nil {
			return err
		}
		return clearGroupRoute(tx, uint(groupId))
	})

	if err != nil {
		logger.Errorf("SplitSegment: %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Segment split successfully"})
}

// JoinSegment merges the segment of this visit into the segment of the stop
// before it. The visit and every visit after it get segment_index - 1.
// body: {"visitId": 12}
func JoinSegment(c *gin.Context) {
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
		VisitId uint `json:"visitId" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "visitId is required"})
		return
	}

	err = initializers.DB.Transaction(func(tx *gorm.DB) error {
		var visit models.Visit
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("group_id = ? AND id = ?", groupId, input.VisitId).
			First(&visit).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("visit not found in this group")
			}
			return err
		}

		if visit.Stopnr == nil || visit.SegmentIndex == nil {
			return errors.New("visit has no stop_nr or segment_index")
		}

		// the stop before this one
		var previous models.Visit
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("group_id = ? AND stop_nr < ?", groupId, *visit.Stopnr).
			Order("stop_nr DESC").First(&previous).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("cannot join: this is the first stop of the route")
			}
			return err
		}

		// a join is only legal at the first stop of a segment. Anywhere else it
		// would cut the segment in two and break contiguity.
		if previous.SegmentIndex != nil && *previous.SegmentIndex == *visit.SegmentIndex {
			return errors.New("can only join at the first stop of a segment")
		}

		var affected []models.Visit
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("group_id = ? AND stop_nr >= ?", groupId, *visit.Stopnr).
			Order("stop_nr ASC").Find(&affected).Error; err != nil {
			return err
		}

		for _, v := range affected {
			// guard above proves *v.SegmentIndex >= 1 for every affected row
			newIndex := *v.SegmentIndex - 1
			if err := internal.UpdateVisitValue(tx, v.ID, fmt.Sprintf("%d", newIndex), user.ID, "segment_index"); err != nil {
				return err
			}
		}

		// MAX keeps segment_index at 0 or higher
		if err := tx.Model(&models.Visit{}).
			Where("group_id = ? AND stop_nr >= ?", groupId, *visit.Stopnr).
			Update("segment_index", gorm.Expr("segment_index - 1")).Error; err != nil {
			return err
		} // IF DATABASE CHANGE TO SOMETHING ELSE THEN SQLITE THEN USE GREATEST() INSTEAD OF MAX

		// verify the invariant on the whole group, not just the shifted tail.
		// a return here rolls the transaction back.
		var all []models.Visit
		if err := tx.Where("group_id = ?", groupId).Find(&all).Error; err != nil {
			return err
		}
		if err := assertSegmentsValid(all); err != nil {
			return err
		}
		return clearGroupRoute(tx, uint(groupId))
	})

	if err != nil {
		logger.Errorf("JoinSegment: %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Segment joined successfully"})
}

// ponytail: asserts contiguity + density, the two invariants the optimizer needs
func assertSegmentsValid(visits []models.Visit) error {
	sorted := append([]models.Visit(nil), visits...)
	sort.Slice(sorted, func(i, j int) bool {
		return derefUint(sorted[i].Stopnr) < derefUint(sorted[j].Stopnr)
	})
	want := uint(0)
	for i, v := range sorted {
		got := derefUint(v.SegmentIndex)
		if i > 0 && got != derefUint(sorted[i-1].SegmentIndex) {
			want++
		}
		if got != want {
			return fmt.Errorf("assert failed, visit %d: segment_index %d, want %d", v.ID, got, want)
		}
	}
	return nil
}

/*
	apiv1.POST("/visits/group/:groupId/reorder",api.ReorderVisit) //
	apiv1.POST("/visits/group/:groupId/split",  api.SplitSegment)
	apiv1.POST("/visits/group/:groupId/join", api.JoinSegment)
*/

type groupSegment struct {
	visits []models.Visit
	isLast bool
}

// groupSegments splits visits (sorted by stop_nr) into contiguous segments by
// segment_index. When borrowAnchors is true, each non-last segment also
// carries the first stop of the next segment appended as its fixed end anchor.
// The free-endpoint solver does NOT use borrowed anchors; it receives real
// segment boundaries only.
func groupSegments(visits []models.Visit, borrowAnchors bool) []groupSegment {
	sorted := append([]models.Visit(nil), visits...)
	sort.Slice(sorted, func(i, j int) bool {
		return derefUint(sorted[i].Stopnr) < derefUint(sorted[j].Stopnr)
	})

	// group by segment_index, not by adjacency in stop_nr order —
	// stop_nr can interleave segments (see bug: a segment-1 stop
	// sitting between segment-0 stops).
	byIdx := map[uint][]models.Visit{}
	var order []uint
	for _, v := range sorted {
		idx := derefUint(v.SegmentIndex)
		if _, ok := byIdx[idx]; !ok {
			order = append(order, idx)
		}
		byIdx[idx] = append(byIdx[idx], v)
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })

	segs := make([]groupSegment, len(order))
	for i, idx := range order {
		segs[i].visits = byIdx[idx]
	}
	for i := range segs {
		segs[i].isLast = i == len(segs)-1
	}
	if borrowAnchors {
		for i := range segs {
			if !segs[i].isLast {
				nextFirst := segs[i+1].visits[0]
				segs[i].visits = append(segs[i].visits, nextFirst)
			}
		}
	}

	if strings.ToLower(os.Getenv("DEBUG")) == "true" {
		for i, s := range segs {
			ids := make([]string, len(s.visits))
			for j, v := range s.visits {
				ids[j] = fmt.Sprintf("id=%d/stop=%d/seg=%d", v.ID, derefUint(v.Stopnr), derefUint(v.SegmentIndex))
			}
			logger.Infof("segment %d (isLast=%v, n=%d): %s", i, s.isLast, len(s.visits), strings.Join(ids, ", "))
		}
	}

	return segs
}

// OptimizeGroup runs one TSP per segment inside a group, persists the new
// stop_nr order, and returns the optimized route with road geometry.
// body: {"costing": "auto", "mode": "distance", "freeEndpoints": false}
func OptimizeGroup(c *gin.Context) {
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
		Costing       string `json:"costing"`
		Mode          string `json:"mode"`
		FreeEndpoints bool   `json:"freeEndpoints"`
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

	var visits []models.Visit
	if err := initializers.DB.Where("group_id = ?", groupId).
		Order("stop_nr ASC").Find(&visits).Error; err != nil {
		logger.Errorf("OptimizeGroup: %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if len(visits) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Need at least 2 visits to optimize"})
		return
	}

	segs := groupSegments(visits, !input.FreeEndpoints)

	var orderedVisits []models.Visit

	if input.FreeEndpoints {
		orderedVisits, err = optimizeFree(visits, segs, costing, mode)
		if err != nil {
			logger.Errorf("OptimizeGroup: %s", err.Error())
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	} else {
		orderedVisits = optimizeSegmented(visits, segs, costing, mode)
	}

	if strings.ToLower(os.Getenv("DEBUG")) == "true" {
		ids := make([]string, len(orderedVisits))
		for i, v := range orderedVisits {
			ids[i] = fmt.Sprintf("id=%d", v.ID)
		}
		logger.Infof("final ordered visits: %v", ids)
	}

	// persist the new stop_nr (0-based, dense) in a transaction
	err = initializers.DB.Transaction(func(tx *gorm.DB) error {
		// only visits whose position actually changed
		changed := make([]int, 0, len(orderedVisits))
		for i := range orderedVisits {
			if derefUint(orderedVisits[i].Stopnr) != uint(i) {
				changed = append(changed, i)
			}
		}

		// park them out of the way first so the unique (group_id, stop_nr)
		// index cannot collide mid-swap, then write the final values
		maxStop := uint(0)
		for i := range orderedVisits {
			if derefUint(orderedVisits[i].Stopnr) > maxStop {
				maxStop = derefUint(orderedVisits[i].Stopnr)
			}
		}
		parkBase := maxStop + 1
		for _, i := range changed {
			park := parkBase + uint(i)
			if err := tx.Model(&models.Visit{}).
				Where("id = ?", orderedVisits[i].ID).
				Update("stop_nr", park).Error; err != nil {
				return err
			}
		}
		for _, i := range changed {
			stop := uint(i)
			if err := internal.UpdateVisitValue(tx, orderedVisits[i].ID, fmt.Sprintf("%d", stop), user.ID, "stop_nr"); err != nil {
				return err
			}
			if err := tx.Model(&models.Visit{}).
				Where("id = ?", orderedVisits[i].ID).
				Update("stop_nr", stop).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		logger.Errorf("OptimizeGroup: %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// route geometry over the full optimized order, plus arrival times and
	// stored route (visit_time is persisted and logged per visit)
	waypoints := make([]tsp.Waypoint, 0, len(orderedVisits))
	for i, v := range orderedVisits {
		lat, _ := strconv.ParseFloat(v.Latitude, 64)
		lon, _ := strconv.ParseFloat(v.Longitude, 64)
		waypoints = append(waypoints, tsp.Waypoint{
			ID:    strconv.FormatUint(uint64(v.ID), 10),
			Label: fmt.Sprintf("%d", i),
			Lat:   lat,
			Lon:   lon,
		})
	}
	geometry, distance, travelTime, overrun, err := computeAndStoreRoute(user.ID, uint(groupId), costing, mode)
	if err != nil {
		logger.Errorf("OptimizeGroup: %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// reflect the persisted order on the returned visits
	for i := range orderedVisits {
		stop := uint(i)
		orderedVisits[i].Stopnr = &stop
	}

	c.JSON(http.StatusOK, tsp.OptimizeResponse{
		Waypoints: waypoints,
		Distance:  distance,
		Time:      travelTime,
		Geometry:  geometry,
		Optimal:   len(waypoints) <= 25,
		Overrun:   overrun,
	})
}

// optimizeSegmented runs the classic per-segment TSP (fixed start, fixed end
// for non-last segments). Used for all modes except "free".
func optimizeSegmented(visits []models.Visit, segs []groupSegment, costing, mode string) []models.Visit {
	orderedVisits := make([]models.Visit, 0, len(visits))
	for _, s := range segs {
		if len(s.visits) < 2 {
			orderedVisits = append(orderedVisits, s.visits...)
			continue
		}

		waypoints := make([]tsp.Waypoint, 0, len(s.visits))
		for _, v := range s.visits {
			lat, _ := strconv.ParseFloat(v.Latitude, 64)
			lon, _ := strconv.ParseFloat(v.Longitude, 64)
			waypoints = append(waypoints, tsp.Waypoint{
				ID:    strconv.FormatUint(uint64(v.ID), 10),
				Label: fmt.Sprintf("%d", derefUint(v.Stopnr)),
				Lat:   lat,
				Lon:   lon,
			})
		}

		solved, err := tsp.SolveWaypoints(waypoints, costing, mode, true, !s.isLast)
		if err != nil {
			// fallback: keep original order for this segment
			for _, v := range s.visits {
				orderedVisits = append(orderedVisits, v)
			}
			continue
		}

		byID := make(map[string]models.Visit, len(s.visits))
		for _, v := range s.visits {
			byID[strconv.FormatUint(uint64(v.ID), 10)] = v
		}
		end := len(solved)
		if !s.isLast {
			end--
		}
		for _, w := range solved[:end] {
			orderedVisits = append(orderedVisits, byID[w.ID])
		}
	}
	return orderedVisits
}

// optimizeFree solves a segmented TSP with free start for segment 0 and free
// end for the last segment. It builds one cost matrix for all visits and
// runs the segment-level DP solver.
func optimizeFree(visits []models.Visit, segs []groupSegment, costing, mode string) ([]models.Visit, error) {
	// Flatten all visits into one waypoint list, track segment sizes
	var allWaypoints []tsp.Waypoint
	segmentSizes := make([]int, len(segs))
	for i, s := range segs {
		for _, v := range s.visits {
			lat, latErr := strconv.ParseFloat(v.Latitude, 64)
			lon, lonErr := strconv.ParseFloat(v.Longitude, 64)
			if latErr != nil || lonErr != nil {
				return nil, fmt.Errorf("visit %d has invalid coordinates", v.ID)
			}
			allWaypoints = append(allWaypoints, tsp.Waypoint{
				ID:    strconv.FormatUint(uint64(v.ID), 10),
				Label: fmt.Sprintf("%d", derefUint(v.Stopnr)),
				Lat:   lat,
				Lon:   lon,
			})
		}
		segmentSizes[i] = len(s.visits)
	}

	solved, err := tsp.SolveSegmentedWaypoints(allWaypoints, segmentSizes, costing, mode)
	if err != nil {
		return nil, err
	}

	byID := make(map[string]models.Visit, len(visits))
	for _, v := range visits {
		byID[strconv.FormatUint(uint64(v.ID), 10)] = v
	}

	orderedVisits := make([]models.Visit, 0, len(visits))
	for _, w := range solved {
		orderedVisits = append(orderedVisits, byID[w.ID])
	}

	if strings.ToLower(os.Getenv("DEBUG")) == "true" {
		assertSameVisits(orderedVisits, visits)
	}
	return orderedVisits, nil
}

// assertSameVisits panics if the optimized visit list has a different set or
// count of visits than the input. DEBUG-only safety net.
func assertSameVisits(got, want []models.Visit) {
	if len(got) != len(want) {
		panic(fmt.Sprintf("optimized route changed visit count: got %d, want %d", len(got), len(want)))
	}
	seen := make(map[uint]bool, len(got))
	for _, v := range got {
		if seen[v.ID] {
			panic(fmt.Sprintf("optimized route contains duplicate visit %d", v.ID))
		}
		seen[v.ID] = true
	}
	for _, v := range want {
		if !seen[v.ID] {
			panic(fmt.Sprintf("optimized route lost visit %d", v.ID))
		}
	}
}
