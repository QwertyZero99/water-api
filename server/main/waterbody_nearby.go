
package main

import (
	"container/heap"
	"context"
	"encoding/json"
	"errors"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"
)

const (
	nearbyCellMiles = 5.0
	nearbyMaxItems  = 25

	// Fixed grid spacing. The longitude cell size is conservative for
	// the supported New York latitude range.
	nearbyLatStep = nearbyCellMiles / 69.0
	nearbyLonStep = nearbyCellMiles / (69.172 * 0.69)

	nearbyMaxCellSpan = 1000

	// A result is returned with one decimal place. Include a margin
	// when pruning against the rounded heap distance.
	nearbyRoundingMargin = 0.051
)

var (
	errNearbyDBUnavailable = errors.New("database unavailable")
	errNearbyNoGeometry    = errors.New("no waterbody geometry available")
)

type nearbyItem struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Type          string  `json:"type"`
	Grouped       bool    `json:"grouped"`
	SegmentCount  int     `json:"segmentCount"`
	DistanceMiles float64 `json:"distanceMiles"`
}

type nearbyPoint struct {
	Lon float64
	Lat float64
}

type nearbyPath struct {
	Points []nearbyPoint

	MinLon float64
	MaxLon float64
	MinLat float64
	MaxLat float64

	Closed bool
}

type nearbyPolygon struct {
	Rings []nearbyPath
}

type nearbyGeometry struct {
	ID           string
	Name         string
	Type         string
	Grouped      bool
	SegmentCount int

	Paths    []nearbyPath
	Polygons []nearbyPolygon

	MinLon float64
	MaxLon float64
	MinLat float64
	MaxLat float64
}

type nearbyCell struct {
	X int
	Y int
}

type nearbySpatialIndex struct {
	Items []nearbyGeometry
	Cells map[nearbyCell][]int

	// Very large geometries are checked separately instead of
	// generating an excessive number of grid references.
	Oversized []int
}

var nearbyCache struct {
	sync.RWMutex
	index *nearbySpatialIndex
}

type rawGeometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
}

type rawSegment struct {
	Geometry rawGeometry `json:"geometry"`
}

func validCoordinate(lon, lat float64) bool {
	return !math.IsNaN(lon) &&
		!math.IsNaN(lat) &&
		!math.IsInf(lon, 0) &&
		!math.IsInf(lat, 0) &&
		lon >= -180 && lon <= 180 &&
		lat >= -90 && lat <= 90
}

func makeNearbyPath(coords []any) (nearbyPath, bool) {
	path := nearbyPath{
		MinLon: math.Inf(1),
		MaxLon: math.Inf(-1),
		MinLat: math.Inf(1),
		MaxLat: math.Inf(-1),
	}

	for _, value := range coords {
		pair, ok := value.([]any)
		if !ok || len(pair) < 2 {
			continue
		}

		lon, lonOK := pair[0].(float64)
		lat, latOK := pair[1].(float64)
		if !lonOK || !latOK || !validCoordinate(lon, lat) {
			continue
		}

		point := nearbyPoint{Lon: lon, Lat: lat}
		path.Points = append(path.Points, point)

		path.MinLon = math.Min(path.MinLon, lon)
		path.MaxLon = math.Max(path.MaxLon, lon)
		path.MinLat = math.Min(path.MinLat, lat)
		path.MaxLat = math.Max(path.MaxLat, lat)
	}

	if len(path.Points) == 0 {
		return nearbyPath{}, false
	}

	path.Closed = len(path.Points) >= 4 &&
		path.Points[0] == path.Points[len(path.Points)-1]

	return path, true
}

// decodeGeometryValue receives a complete GeoJSON geometry object.
// MultiPoint coordinates remain independent points.
func decodeGeometryValue(g rawGeometry, item *nearbyGeometry) {
	switch g.Type {
	case "Point":
		var coords []any
		if json.Unmarshal(g.Coordinates, &coords) == nil {
			if path, ok := makeNearbyPath([]any{coords}); ok {
				item.Paths = append(item.Paths, path)
			}
		}

	case "MultiPoint":
		var coords [][]any
		if json.Unmarshal(g.Coordinates, &coords) == nil {
			for _, coord := range coords {
				if path, ok := makeNearbyPath([]any{coord}); ok {
					item.Paths = append(item.Paths, path)
				}
			}
		}

	case "LineString":
		var coords []any
		if json.Unmarshal(g.Coordinates, &coords) == nil {
			if path, ok := makeNearbyPath(coords); ok {
				item.Paths = append(item.Paths, path)
			}
		}

	case "MultiLineString":
		var lines [][]any
		if json.Unmarshal(g.Coordinates, &lines) == nil {
			for _, line := range lines {
				if path, ok := makeNearbyPath(line); ok {
					item.Paths = append(item.Paths, path)
				}
			}
		}

	case "Polygon":
		var rings [][]any
		if json.Unmarshal(g.Coordinates, &rings) != nil {
			return
		}

		polygon := nearbyPolygon{}
		for _, ring := range rings {
			if path, ok := makeNearbyPath(ring); ok {
				polygon.Rings = append(polygon.Rings, path)
				item.Paths = append(item.Paths, path)
			}
		}

		if len(polygon.Rings) > 0 {
			item.Polygons = append(item.Polygons, polygon)
		}

	case "MultiPolygon":
		var polygons [][][]any
		if json.Unmarshal(g.Coordinates, &polygons) != nil {
			return
		}

		for _, rings := range polygons {
			polygon := nearbyPolygon{}

			for _, ring := range rings {
				if path, ok := makeNearbyPath(ring); ok {
					polygon.Rings = append(polygon.Rings, path)
					item.Paths = append(item.Paths, path)
				}
			}

			if len(polygon.Rings) > 0 {
				item.Polygons = append(item.Polygons, polygon)
			}
		}
	}
}

// Retained for callers that have an encoded, complete geometry object.
func decodeGeometry(raw json.RawMessage, item *nearbyGeometry) {
	var g rawGeometry
	if json.Unmarshal(raw, &g) == nil {
		decodeGeometryValue(g, item)
	}
}

func initGeometryBounds(item *nearbyGeometry) bool {
	item.MinLon = math.Inf(1)
	item.MaxLon = math.Inf(-1)
	item.MinLat = math.Inf(1)
	item.MaxLat = math.Inf(-1)

	for _, path := range item.Paths {
		item.MinLon = math.Min(item.MinLon, path.MinLon)
		item.MaxLon = math.Max(item.MaxLon, path.MaxLon)
		item.MinLat = math.Min(item.MinLat, path.MinLat)
		item.MaxLat = math.Max(item.MaxLat, path.MaxLat)
	}

	return len(item.Paths) > 0
}

func cellFor(lat, lon float64) nearbyCell {
	return nearbyCell{
		X: int(math.Floor(lon / nearbyLonStep)),
		Y: int(math.Floor(lat / nearbyLatStep)),
	}
}

func buildNearbyIndex(items []nearbyGeometry) *nearbySpatialIndex {
	idx := &nearbySpatialIndex{
		Items: make([]nearbyGeometry, len(items)),
		Cells: make(map[nearbyCell][]int, len(items)*2),
	}

	copy(idx.Items, items)

	for i := range idx.Items {
		g := &idx.Items[i]

		minX := int(math.Floor(g.MinLon / nearbyLonStep))
		maxX := int(math.Floor(g.MaxLon / nearbyLonStep))
		minY := int(math.Floor(g.MinLat / nearbyLatStep))
		maxY := int(math.Floor(g.MaxLat / nearbyLatStep))

		if maxX-minX > nearbyMaxCellSpan ||
			maxY-minY > nearbyMaxCellSpan {
			idx.Oversized = append(idx.Oversized, i)
			continue
		}

		for x := minX; x <= maxX; x++ {
			for y := minY; y <= maxY; y++ {
				key := nearbyCell{X: x, Y: y}
				idx.Cells[key] = append(idx.Cells[key], i)
			}
		}
	}

	return idx
}

func loadNearbyIndex(ctx context.Context) (*nearbySpatialIndex, error) {
	nearbyCache.RLock()
	idx := nearbyCache.index
	nearbyCache.RUnlock()

	if idx != nil {
		return idx, nil
	}

	nearbyCache.Lock()
	defer nearbyCache.Unlock()

	if nearbyCache.index != nil {
		return nearbyCache.index, nil
	}

	if stationStore.db == nil {
		return nil, errNearbyDBUnavailable
	}

	rows, err := stationStore.db.QueryContext(ctx, `
		SELECT id, name, waterbody_type, grouped, segment_count, segments
		FROM waterbodies_v2`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]nearbyGeometry, 0, 5000)

	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		var item nearbyGeometry
		var raw []byte

		if err := rows.Scan(
			&item.ID,
			&item.Name,
			&item.Type,
			&item.Grouped,
			&item.SegmentCount,
			&raw,
		); err != nil {
			return nil, err
		}

		var segments []rawSegment
		if err := json.Unmarshal(raw, &segments); err != nil {
			continue
		}

		for _, segment := range segments {
			// Pass the complete geometry, including its "type".
			decodeGeometryValue(segment.Geometry, &item)
		}

		if !initGeometryBounds(&item) {
			continue
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(items) == 0 {
		return nil, errNearbyNoGeometry
	}

	// The index is published only after it is fully built.
	built := buildNearbyIndex(items)
	nearbyCache.index = built

	return built, nil
}

// Local planar distance in miles. Suitable as an approximation for
// short distances in New York; see deployment notes below.
func distanceToPathMiles(lat, lon float64, path nearbyPath) float64 {
	if len(path.Points) == 0 {
		return math.Inf(1)
	}

	scaleX := 69.172 * math.Cos(lat*math.Pi/180)
	scaleY := 69.0

	bestSquared := math.Inf(1)
	lastX, lastY := 0.0, 0.0

	for i, point := range path.Points {
		x := (point.Lon - lon) * scaleX
		y := (point.Lat - lat) * scaleY

		d2 := x*x + y*y
		if d2 < bestSquared {
			bestSquared = d2
		}

		if i > 0 {
			dx := x - lastX
			dy := y - lastY
			den := dx*dx + dy*dy

			if den > 0 {
				t := -(lastX*dx + lastY*dy) / den
				t = math.Max(0, math.Min(1, t))

				px := lastX + t*dx
				py := lastY + t*dy

				d2 = px*px + py*py
				if d2 < bestSquared {
					bestSquared = d2
				}
			}
		}

		lastX, lastY = x, y
	}

	return math.Sqrt(bestSquared)
}

func pointInRing(lat, lon float64, ring nearbyPath) bool {
	if !ring.Closed || len(ring.Points) < 4 {
		return false
	}

	inside := false
	j := len(ring.Points) - 1

	for i, point := range ring.Points {
		prev := ring.Points[j]

		if (point.Lat > lat) != (prev.Lat > lat) {
			crossingLon := (prev.Lon-point.Lon)*
				(lat-point.Lat)/(prev.Lat-point.Lat) + point.Lon

			if lon < crossingLon {
				inside = !inside
			}
		}

		j = i
	}

	return inside
}

func pointInPolygon(lat, lon float64, polygon nearbyPolygon) bool {
	if len(polygon.Rings) == 0 {
		return false
	}

	outer := polygon.Rings[0]
	if lon < outer.MinLon || lon > outer.MaxLon ||
		lat < outer.MinLat || lat > outer.MaxLat {
		return false
	}

	if !pointInRing(lat, lon, outer) {
		return false
	}

	// Subsequent rings represent holes.
	for _, hole := range polygon.Rings[1:] {
		if lon < hole.MinLon || lon > hole.MaxLon ||
			lat < hole.MinLat || lat > hole.MaxLat {
			continue
		}

		if pointInRing(lat, lon, hole) {
			return false
		}
	}

	return true
}

// Lower bound to the bounding box in the same local planar metric used
// by the final path-distance calculation.
func geometryLowerBoundMiles(lat, lon float64, g *nearbyGeometry) float64 {
	scaleX := 69.172 * math.Cos(lat*math.Pi/180)

	dx := 0.0
	if lon < g.MinLon {
		dx = (g.MinLon - lon) * scaleX
	} else if lon > g.MaxLon {
		dx = (lon - g.MaxLon) * scaleX
	}

	dy := 0.0
	if lat < g.MinLat {
		dy = (g.MinLat - lat) * 69.0
	} else if lat > g.MaxLat {
		dy = (lat - g.MaxLat) * 69.0
	}

	return math.Hypot(dx, dy)
}

func geometryDistance(lat, lon float64, g *nearbyGeometry) float64 {
	lowerBound := geometryLowerBoundMiles(lat, lon, g)

	// The API radius is at most 25 miles.
	if lowerBound > 25 {
		return lowerBound
	}

	for _, polygon := range g.Polygons {
		if pointInPolygon(lat, lon, polygon) {
			return 0
		}
	}

	best := math.Inf(1)

	// Restrict scans to paths whose bounding boxes are near enough
	// to improve the current best distance.
	for _, path := range g.Paths {
		if best < math.Inf(1) {
			latPad := best/69.0 + 0.01
			lonScale := 69.172 * math.Cos(lat*math.Pi/180)
			lonPad := 180.0

			if math.Abs(lonScale) > 1e-9 {
				lonPad = best/math.Abs(lonScale) + 0.01
			}

			if path.MinLon > lon+lonPad ||
				path.MaxLon < lon-lonPad ||
				path.MinLat > lat+latPad ||
				path.MaxLat < lat-latPad {
				continue
			}
		}

		d := distanceToPathMiles(lat, lon, path)
		if d < best {
			best = d
		}

		if best == 0 {
			return 0
		}
	}

	return best
}

type nearbyResult struct {
	item nearbyItem
}

type nearbyMaxHeap []nearbyResult

func (h nearbyMaxHeap) Len() int { return len(h) }

func (h nearbyMaxHeap) Less(i, j int) bool {
	if h[i].item.DistanceMiles == h[j].item.DistanceMiles {
		return h[i].item.Name > h[j].item.Name
	}
	return h[i].item.DistanceMiles > h[j].item.DistanceMiles
}

func (h nearbyMaxHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *nearbyMaxHeap) Push(value any) {
	*h = append(*h, value.(nearbyResult))
}

func (h *nearbyMaxHeap) Pop() any {
	old := *h
	n := len(old)
	value := old[n-1]
	*h = old[:n-1]
	return value
}

func nearbyItemLess(a, b nearbyItem) bool {
	if a.DistanceMiles == b.DistanceMiles {
		return a.Name < b.Name
	}
	return a.DistanceMiles < b.DistanceMiles
}

func queryNearby(
	ctx context.Context,
	lat, lon, radius float64,
) ([]nearbyItem, error) {
	idx, err := loadNearbyIndex(ctx)
	if err != nil {
		return nil, err
	}

	latPad := radius / 69.0
	cosLat := math.Cos(lat * math.Pi / 180)

	lonPad := 180.0
	if math.Abs(cosLat) > 1e-9 {
		lonPad = radius / (69.172 * math.Abs(cosLat))
	}

	minX := int(math.Floor((lon - lonPad) / nearbyLonStep))
	maxX := int(math.Floor((lon + lonPad) / nearbyLonStep))
	minY := int(math.Floor((lat - latPad) / nearbyLatStep))
	maxY := int(math.Floor((lat + latPad) / nearbyLatStep))

	seen := make(map[int]struct{}, 256)
	results := make(nearbyMaxHeap, 0, nearbyMaxItems)
	heap.Init(&results)

	consider := func(i int) {
		if _, exists := seen[i]; exists {
			return
		}
		seen[i] = struct{}{}

		g := &idx.Items[i]

		lowerBound := geometryLowerBoundMiles(lat, lon, g)
		if lowerBound > radius {
			return
		}

		// Because heap distances are rounded to one decimal place,
		// keep a margin to avoid dropping candidates that could tie
		// after rounding and win the name-based tie-break.
		if results.Len() == nearbyMaxItems {
			cutoff := results[0].item.DistanceMiles +
				nearbyRoundingMargin

			if lowerBound > cutoff {
				return
			}
		}

		d := geometryDistance(lat, lon, g)
		if d > radius {
			return
		}

		item := nearbyItem{
			ID:            g.ID,
			Name:          g.Name,
			Type:          g.Type,
			Grouped:       g.Grouped,
			SegmentCount:  g.SegmentCount,
			DistanceMiles: math.Round(d*10) / 10,
		}

		candidate := nearbyResult{item: item}

		if results.Len() < nearbyMaxItems {
			heap.Push(&results, candidate)
			return
		}

		if nearbyItemLess(candidate.item, results[0].item) {
			heap.Pop(&results)
			heap.Push(&results, candidate)
		}
	}

	for x := minX; x <= maxX; x++ {
		for y := minY; y <= maxY; y++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}

			for _, i := range idx.Cells[nearbyCell{X: x, Y: y}] {
				consider(i)
			}
		}
	}

	for _, i := range idx.Oversized {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		consider(i)
	}

	found := make([]nearbyItem, 0, results.Len())
	for results.Len() > 0 {
		found = append(found, heap.Pop(&results).(nearbyResult).item)
	}

	sort.Slice(found, func(i, j int) bool {
		return nearbyItemLess(found[i], found[j])
	})

	return found, nil
}

func WaterbodiesNearby(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	lat, latErr := strconv.ParseFloat(q.Get("lat"), 64)
	lon, lonErr := strconv.ParseFloat(q.Get("lon"), 64)
	radius, radiusErr := strconv.ParseFloat(q.Get("radius"), 64)

	if latErr != nil || lonErr != nil || radiusErr != nil ||
		!validCoordinate(lon, lat) ||
		lat < 40 || lat > 46 ||
		lon < -80.5 || lon > -71 ||
		math.IsNaN(radius) || math.IsInf(radius, 0) ||
		radius < 1 || radius > 25 {
		http.Error(
			w,
			"invalid NY coordinates or radius (1-25 miles)",
			http.StatusBadRequest,
		)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	start := time.Now()

	found, err := queryNearby(ctx, lat, lon, radius)
	if err != nil {
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			http.Error(
				w,
				"nearby search timed out",
				http.StatusGatewayTimeout,
			)

		case errors.Is(err, context.Canceled):
			return

		default:
			http.Error(
				w,
				"nearby waterbody index unavailable",
				http.StatusServiceUnavailable,
			)
			log.Printf("nearby index/search error: %v", err)
		}
		return
	}

	log.Printf(
		"nearby search: %.2fms; results=%d",
		float64(time.Since(start).Microseconds())/1000,
		len(found),
	)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=600")

	if err := json.NewEncoder(w).Encode(found); err != nil {
		log.Printf("nearby response error: %v", err)
	}
}
