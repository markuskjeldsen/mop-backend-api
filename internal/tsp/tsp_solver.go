package tsp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"

	"github.com/MOPDev/mop-backend-api/internal/logger"
	"github.com/gin-gonic/gin"
)

// ========== Types ==========

type Location struct {
	Lat    float64 `json:"lat"`
	Lon    float64 `json:"lon"`
	Radius int     `json:"radius,omitempty"` // important!
}

type Waypoint struct {
	ID    string  `json:"id"`
	Label string  `json:"label"`
	Lat   float64 `json:"lat"`
	Lon   float64 `json:"lon"`
}

type OptimizeRequest struct {
	Waypoints  []Waypoint `json:"waypoints"`
	Costing    string     `json:"costing"`
	Mode       string     `json:"mode"`
	FixedStart bool       `json:"fixed_start"` // Lock waypoints[0] as start
	FixedEnd   bool       `json:"fixed_end"`   // Lock waypoints[last] as end
}

type OptimizeResponse struct {
	Waypoints []Waypoint `json:"waypoints"`
	Distance  float64    `json:"distance"`
	Time      float64    `json:"time"`
	Geometry  []string   `json:"geometry"` // Encoded polyline string
	Optimal   bool       `json:"optimal"`
	Overrun   bool       `json:"overrun"`
}

type ValhallaMatrixRequest struct {
	Sources        []Location               `json:"sources"`
	Targets        []Location               `json:"targets"`
	Costing        string                   `json:"costing"`
	CostingOptions map[string]CostingOption `json:"costing_options,omitempty"`
}

type ValhallaRouteRequest struct {
	Locations         []Location               `json:"locations"`
	Costing           string                   `json:"costing"`
	CostingOptions    map[string]CostingOption `json:"costing_options,omitempty"`
	DirectionsType    string                   `json:"directions_type"`
	DirectionsOptions map[string]string        `json:"directions_options"`
}

type CostingOption struct {
	Shortest bool `json:"shortest,omitempty"`
}

type ValhallaMatrixResponse struct {
	SourcesToTargets [][]struct {
		FromIndex int     `json:"from_index"`
		ToIndex   int     `json:"to_index"`
		Distance  float64 `json:"distance"`
		Time      float64 `json:"time"`
	} `json:"sources_to_targets"`
}

type ValhallaRouteResponse struct {
	Trip struct {
		Summary struct {
			Time     float64 `json:"time"`
			Distance float64 `json:"length"`
		} `json:"summary"`
		Legs []struct {
			Shape   string `json:"shape"`
			Summary struct {
				Time float64 `json:"time"`
			} `json:"summary"`
		} `json:"legs"`
	} `json:"trip"`
}

// ========== TSP Solver ==========

func heldKarp(matrix [][]float64, startIdx int, fixedEndIdx int) ([]int, float64, int) {
	n := len(matrix)
	hasFixedEnd := fixedEndIdx >= 0

	if n <= 2 {
		if n == 1 {
			return []int{0}, 0, 0
		}
		return []int{0, 1}, matrix[0][1], 1
	}

	numSubsets := 1 << n
	dp := make([][]float64, numSubsets)
	parent := make([][]int, numSubsets)

	for mask := 0; mask < numSubsets; mask++ {
		dp[mask] = make([]float64, n)
		parent[mask] = make([]int, n)
		for i := 0; i < n; i++ {
			dp[mask][i] = math.Inf(1)
			parent[mask][i] = -1
		}
	}

	startMask := 1 << startIdx
	dp[startMask][startIdx] = 0

	for mask := 0; mask < numSubsets; mask++ {
		if (mask & startMask) == 0 {
			continue
		}
		for end := 0; end < n; end++ {
			if end == startIdx {
				continue
			}
			if (mask & (1 << end)) == 0 {
				continue
			}
			// removed: `if dp[mask][end] == math.Inf(1) { continue }`
			// that guard skipped computing dp[mask][end] before ever trying,
			// so every cell stayed +Inf.

			prevMask := mask ^ (1 << end)
			for prev := 0; prev < n; prev++ {
				if (prevMask & (1 << prev)) == 0 {
					continue
				}
				if dp[prevMask][prev] == math.Inf(1) {
					continue // correct here: prevMask/prev is the *source*, skip if unreached
				}
				if matrix[prev][end] < 0 {
					continue
				}
				newDist := dp[prevMask][prev] + matrix[prev][end]
				if newDist < dp[mask][end] {
					dp[mask][end] = newDist
					parent[mask][end] = prev
				}
			}
		}
	}

	fullMask := (1 << n) - 1
	minCost := math.Inf(1)
	bestEnd := -1

	if hasFixedEnd {
		// Must end at fixedEndIdx
		bestEnd = fixedEndIdx
		minCost = dp[fullMask][fixedEndIdx]
	} else {
		// Find the best endpoint (any point except start)
		for end := 0; end < n; end++ {
			if end == startIdx {
				continue
			}
			if dp[fullMask][end] < minCost {
				minCost = dp[fullMask][end]
				bestEnd = end
			}
		}
	}

	if bestEnd == -1 || math.IsInf(minCost, 1) {
		// Fallback: just return original order
		order := make([]int, n)
		for i := 0; i < n; i++ {
			order[i] = i
		}

		return order, matrix[0][n-1], n - 1
	}

	// Reconstruct path
	order := make([]int, n)
	order[n-1] = bestEnd
	currentMask := fullMask
	currentCity := bestEnd

	for i := n - 2; i >= 1; i-- {
		prevCity := parent[currentMask][currentCity]
		order[i] = prevCity
		currentMask = currentMask ^ (1 << currentCity)
		currentCity = prevCity
	}

	order[0] = startIdx

	return order, minCost, bestEnd
}

func twoOpt(order []int, matrix [][]float64, fixedStart, fixedEnd int, hasFixedEnd bool) ([]int, float64) {
	n := len(order)
	improved := true
	currentDist := calculateTourDistance(order, matrix)

	// Determine the range we're allowed to modify
	// Index 0 is always fixed (start)
	// If hasFixedEnd, index n-1 is also fixed
	lastModifiableIdx := n - 1
	if hasFixedEnd {
		lastModifiableIdx = n - 2 // Don't touch the last element
	}

	for improved {
		improved = false
		for i := 1; i <= lastModifiableIdx; i++ {
			for j := i + 1; j <= lastModifiableIdx; j++ {
				newOrder := make([]int, n)
				copy(newOrder, order)

				// Reverse segment [i, j]
				for k := 0; k <= j-i; k++ {
					newOrder[i+k] = order[j-k]
				}

				newDist := calculateTourDistance(newOrder, matrix)
				if newDist < currentDist {
					order = newOrder
					currentDist = newDist
					improved = true
				}
			}
		}
	}

	return order, currentDist
}

func nearestNeighbor(matrix [][]float64, startIdx int, fixedEndIdx int) []int {
	n := len(matrix)
	hasFixedEnd := fixedEndIdx >= 0
	visited := make([]bool, n)
	order := make([]int, n)

	order[0] = startIdx
	visited[startIdx] = true
	if hasFixedEnd {
		visited[fixedEndIdx] = true // Reserve it for last position
	}

	current := startIdx
	// Fill all positions except the last (if fixed)
	endLimit := n
	if hasFixedEnd {
		endLimit = n - 1
	}

	for i := 1; i < endLimit; i++ {
		bestNext := -1
		bestDist := math.Inf(1)

		for j := 0; j < n; j++ {
			if !visited[j] && matrix[current][j] < bestDist {
				bestDist = matrix[current][j]
				bestNext = j
			}
		}

		if bestNext == -1 {
			// No unvisited node found (shouldn't happen normally)
			break
		}

		order[i] = bestNext
		visited[bestNext] = true
		current = bestNext
	}

	if hasFixedEnd {
		order[n-1] = fixedEndIdx
	}

	return order
}

func calculateTourDistance(order []int, matrix [][]float64) float64 {
	dist := 0.0
	for i := 0; i < len(order)-1; i++ {
		dist += matrix[order[i]][order[i+1]]
	}
	return dist
}

// ========== Valhalla Client ==========

const (
	valhallaBaseURL = "http://192.168.2.14:8002" // Your LAN IP where Valhalla runs
	dupeEps         = 1e-5                       // ~1m at most latitudes
)

// coLocated reports whether two locations are within dupeEps of each other.
func coLocated(a, b Location) bool {
	return math.Abs(a.Lat-b.Lat) < dupeEps && math.Abs(a.Lon-b.Lon) < dupeEps
}

// collapseConsecutive removes waypoints that sit on top of their predecessor,
// returning the unique list plus, for each original waypoint, the index of
// its representative in the unique list.
func collapseConsecutive(waypoints []Waypoint) (unique []Waypoint, reps []int) {
	reps = make([]int, len(waypoints))
	for i, w := range waypoints {
		if len(unique) > 0 && coLocated(Location{Lat: unique[len(unique)-1].Lat, Lon: unique[len(unique)-1].Lon}, Location{Lat: w.Lat, Lon: w.Lon}) {
			reps[i] = len(unique) - 1
			continue
		}
		reps[i] = len(unique)
		unique = append(unique, w)
	}
	return unique, reps
}

// expandLegs maps legs of the collapsed route back to one leg per original
// waypoint pair. Pairs that share a representative are co-located and get a
// zero-length leg (empty geometry, 0 travel time), while every stop still
// receives its own service time in the caller's schedule.
func expandLegs(reps []int, geometry []string, legTimes []float64) ([]string, []float64) {
	outG := make([]string, len(reps)-1)
	outT := make([]float64, len(reps)-1)
	for i := 0; i < len(reps)-1; i++ {
		if reps[i] == reps[i+1] {
			continue // co-located pair: zero leg
		}
		u := reps[i] // collapsed leg u runs between unique[u] and unique[u+1]
		outG[i] = geometry[u]
		outT[i] = legTimes[u]
	}
	return outG, outT
}

func getMatrixFromValhalla(locations []Location, costing, mode string) ([][]float64, error) {
	req := ValhallaMatrixRequest{
		Sources: locations,
		Targets: locations,
		Costing: costing,
	}

	if mode == "distance" {
		req.CostingOptions = map[string]CostingOption{
			costing: {Shortest: true},
		}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal matrix request: %w", err)
	}

	resp, err := http.Post(
		valhallaBaseURL+"/sources_to_targets",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("valhalla matrix request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("valhalla returned %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Read and parse the raw JSON
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read matrix response: %w", err)
	}

	var matrixResp ValhallaMatrixResponse
	if err := json.Unmarshal(bodyBytes, &matrixResp); err != nil {
		return nil, fmt.Errorf("failed to decode matrix response: %w", err)
	}

	n := len(locations)
	matrix := make([][]float64, n)
	for i := range matrix {
		matrix[i] = make([]float64, n)
	}

	// Track which connections are unreachable
	unreachable := 0

	for _, row := range matrixResp.SourcesToTargets {
		for _, cell := range row {
			fromIdx := cell.FromIndex
			toIdx := cell.ToIndex

			if fromIdx >= n || toIdx >= n {
				continue
			}

			// co-located pair: zero cost, not unreachable
			if coLocated(locations[fromIdx], locations[toIdx]) {
				matrix[fromIdx][toIdx] = 0
				continue
			}

			if mode == "time" {
				if cell.Time > 0 {
					matrix[fromIdx][toIdx] = cell.Time
				} else {
					// UNREACHABLE - use a very large number
					matrix[fromIdx][toIdx] = cell.Distance * -1
					unreachable++
				}
			} else {
				if cell.Distance > 0 {
					matrix[fromIdx][toIdx] = cell.Distance
				} else {
					// UNREACHABLE - use a very large number
					matrix[fromIdx][toIdx] = cell.Distance * -1
					unreachable++
				}
			}
		}
	}

	if strings.ToLower(os.Getenv("DEBUG")) == "true" {
		// ADD THIS DEBUG LOGGING
		fmt.Printf("=== MATRIX DEBUG ===\n")
		// Print column headers
		fmt.Printf("      ")
		for j := 0; j < len(locations); j++ {
			fmt.Printf(" %6d", j)
		}
		fmt.Printf("\n")

		// Print rows with row headers
		for i := 0; i < len(locations); i++ {
			fmt.Printf(" %4d ", i)
			for j := 0; j < len(locations); j++ {
				val := matrix[i][j]
				// Truncate large numbers for display
				if val < -999999 {
					fmt.Printf(" %6s", "<-999k")
				} else {
					fmt.Printf(" %6.0f", val)
				}
			}
			fmt.Printf("\n")
		}
		fmt.Printf("===================\n")

		if unreachable > len(locations) {
			fmt.Printf("[WARNING] %d location pairs are unreachable\n", unreachable)
		}

	}

	return matrix, nil
}

func getRouteFromValhalla(waypoints []Waypoint, costing, mode string) (*ValhallaRouteResponse, error) {
	locations := make([]Location, len(waypoints))
	for i, w := range waypoints {
		locations[i] = Location{Lat: w.Lat, Lon: w.Lon, Radius: 100} // perhaps limit to 50
	}

	req := ValhallaRouteRequest{
		Locations:      locations,
		Costing:        costing,
		DirectionsType: "maneuvers",
		DirectionsOptions: map[string]string{
			"units": "km",
		},
	}

	if mode == "distance" {
		req.CostingOptions = map[string]CostingOption{
			costing: {Shortest: true},
		}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal route request: %w", err)
	}

	resp, err := http.Post(
		valhallaBaseURL+"/route",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("valhalla route request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("valhalla returned %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var routeResp ValhallaRouteResponse
	if err := json.NewDecoder(resp.Body).Decode(&routeResp); err != nil {
		return nil, fmt.Errorf("failed to decode route response: %w", err)
	}

	return &routeResp, nil
}

// Add brute force solver for comparison
func bruteForceTSP(matrix [][]float64, startIdx int) ([]int, float64) {
	n := len(matrix)
	if n > 10 {
		return nil, math.Inf(1)
	}

	// Generate all permutations excluding startIdx
	others := make([]int, n-1)
	j := 0
	for i := 0; i < n; i++ {
		if i != startIdx {
			others[j] = i
			j++
		}
	}

	bestOrder := make([]int, n)
	bestCost := math.Inf(1)

	permute(others, 0, func(perm []int) {
		order := make([]int, n)
		order[0] = startIdx
		copy(order[1:], perm)

		cost := calculateTourDistance(order, matrix)
		if cost < bestCost {
			bestCost = cost
			copy(bestOrder, order)
		}
	})

	return bestOrder, bestCost
}

func permute(arr []int, start int, callback func([]int)) {
	if start == len(arr)-1 {
		callback(arr)
		return
	}

	for i := start; i < len(arr); i++ {
		arr[start], arr[i] = arr[i], arr[start]
		permute(arr, start+1, callback)
		arr[start], arr[i] = arr[i], arr[start]
	}
}

// ========== Handlers ==========

// BuildMatrix builds the NxN cost matrix for the given waypoints, retrying
// with escalating snap radii on failure.
func BuildMatrix(waypoints []Waypoint, costing, mode string) ([][]float64, error) {
	radii := []int{50, 200, 400, 800}
	for _, radius := range radii {
		locations := make([]Location, len(waypoints))
		for i, w := range waypoints {
			locations[i] = Location{Lat: w.Lat, Lon: w.Lon, Radius: radius}
		}
		matrix, err := getMatrixFromValhalla(locations, costing, mode)
		if err != nil {
			return nil, err
		}
		if !hasUnreachablePair(matrix) {
			return matrix, nil
		}
	}
	return nil, errors.New("some waypoints are unreachable at the maximum snap radius")
}

// SolveWaypoints reorders the waypoints by solving an open TSP with the given
// endpoint constraints. The road geometry is not fetched here.
func SolveWaypoints(waypoints []Waypoint, costing, mode string, fixedStart, fixedEnd bool) ([]Waypoint, error) {
	if len(waypoints) < 2 {
		return nil, errors.New("need at least 2 waypoints")
	}

	matrix, err := BuildMatrix(waypoints, costing, mode)
	if err != nil {
		return nil, err
	}

	order := solveOrder(matrix, fixedStart, fixedEnd)

	ordered := make([]Waypoint, len(waypoints))
	for i, idx := range order {
		ordered[i] = waypoints[idx]
	}
	return ordered, nil
}

// hasUnreachablePair reports true if any off-diagonal cell is unreachable.
// Valhalla marks unreachable pairs with a negative value or -0 (IEEE 754
// negative zero). Co-located stops get positive zero, which is valid.
func hasUnreachablePair(matrix [][]float64) bool {
	for i := range matrix {
		for j := range matrix[i] {
			if i == j {
				continue
			}
			if matrix[i][j] < 0 || (matrix[i][j] == 0 && math.Signbit(matrix[i][j])) {
				return true
			}
		}
	}
	return false
}

// RouteResult is the geometry + travel data for an ordered route.
type RouteResult struct {
	Geometry []string  // one encoded polyline per leg
	LegTimes []float64 // travel time per leg, seconds
	Distance float64
	Time     float64
}

// RouteGeometry fetches the road geometry for the waypoints in the given order.
// Consecutive waypoints on top of each other are collapsed before the Valhalla
// call and expanded back into zero-length legs afterwards.
func RouteGeometry(waypoints []Waypoint, costing, mode string) (*RouteResult, error) {
	unique, reps := collapseConsecutive(waypoints)

	// all waypoints on top of each other: every leg is zero-length
	if len(unique) < 2 {
		outG, outT := expandLegs(reps, nil, nil)
		return &RouteResult{Geometry: outG, LegTimes: outT}, nil
	}

	routeResp, err := getRouteFromValhalla(unique, costing, mode)
	if err != nil {
		return nil, err
	}

	geometry := make([]string, 0, len(routeResp.Trip.Legs))
	legTimes := make([]float64, 0, len(routeResp.Trip.Legs))
	for _, leg := range routeResp.Trip.Legs {
		geometry = append(geometry, leg.Shape)
		legTimes = append(legTimes, leg.Summary.Time)
	}

	outG, outT := expandLegs(reps, geometry, legTimes)

	result := &RouteResult{
		Geometry: outG,
		LegTimes: outT,
		Distance: routeResp.Trip.Summary.Distance,
		Time:     routeResp.Trip.Summary.Time,
	}
	return result, nil
}

// solveOrder maps the fixed start/end flags onto the solver.
func solveOrder(matrix [][]float64, fixedStart, fixedEnd bool) []int {
	n := len(matrix)
	startIdx := 0
	fixedEndIdx := -1 // -1 means "not fixed"

	switch {
	case fixedStart && fixedEnd:
		// waypoints[0] is start, waypoints[n-1] is end
		startIdx, fixedEndIdx = 0, n-1
	case fixedStart && !fixedEnd:
		// waypoints[0] is start, end is free
		startIdx, fixedEndIdx = 0, -1
	case !fixedStart && fixedEnd:
		// end fixed, start free: run with waypoints[n-1] as "start", reverse at the end
		startIdx, fixedEndIdx = -1, n-1
	default:
		// fully free (classic open TSP)
		startIdx, fixedEndIdx = -1, -1
	}

	if n <= 25 {
		switch {
		case startIdx == -1 && fixedEndIdx == -1:
			order, _, _ := heldKarp(matrix, 0, -1)
			return order
		case startIdx == -1 && fixedEndIdx >= 0:
			revOrder, _, _ := heldKarp(reverseMatrix(matrix), fixedEndIdx, -1)
			return reverseOrder(revOrder)
		default:
			order, _, _ := heldKarp(matrix, startIdx, fixedEndIdx)
			return order
		}
	}

	// Larger problem: nearest neighbor + 2-opt
	effectiveStart := startIdx
	if effectiveStart == -1 {
		effectiveStart = 0 // arbitrary pick for NN
	}

	nnOrder := nearestNeighbor(matrix, effectiveStart, fixedEndIdx)
	hasFixedEnd := fixedEndIdx >= 0
	order, _ := twoOpt(nnOrder, matrix, effectiveStart, fixedEndIdx, hasFixedEnd)
	return order
}

func OptimizeHandler(c *gin.Context) {
	var req OptimizeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if len(req.Waypoints) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Need at least 2 waypoints"})
		return
	}

	ordered, err := SolveWaypoints(req.Waypoints, req.Costing, req.Mode, req.FixedStart, req.FixedEnd)
	if err != nil {
		logger.Errorf("TSP optimize: %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	result, err := RouteGeometry(ordered, req.Costing, req.Mode)
	if err != nil {
		logger.Errorf("RouteGeometry: %s", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, OptimizeResponse{
		Waypoints: ordered,
		Distance:  result.Distance,
		Time:      result.Time,
		Geometry:  result.Geometry,
		Optimal:   len(req.Waypoints) <= 25,
	})
}

// Helper: reverse a matrix (swap from/to) for "fixed end, free start" case
func reverseMatrix(matrix [][]float64) [][]float64 {
	n := len(matrix)
	reversed := make([][]float64, n)
	for i := range reversed {
		reversed[i] = make([]float64, n)
		for j := range reversed[i] {
			reversed[i][j] = matrix[j][i] // transpose
		}
	}
	return reversed
}

// Helper: reverse an order slice
func reverseOrder(order []int) []int {
	n := len(order)
	reversed := make([]int, n)
	for i, v := range order {
		reversed[n-1-i] = v
	}
	return reversed
}

// Health check endpoint
func HealthHandler(c *gin.Context) {
	// Quick check if Valhalla is reachable
	resp, err := http.Get(valhallaBaseURL + "/status")
	if err != nil || resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":   "unhealthy",
			"valhalla": "unreachable",
		})
		return
	}
	resp.Body.Close()

	c.JSON(http.StatusOK, gin.H{
		"status":   "healthy",
		"valhalla": "connected",
	})
}
