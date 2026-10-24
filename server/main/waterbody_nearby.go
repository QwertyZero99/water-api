
package main

import (
	"container/heap"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	nearbyEarthMi   = 3958.7613
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
	Paths        []nearbyPath
	Polygons     []nearbyPolygon
	MinLon       float64
	MaxLon       float64
	MinLat       float64
	MaxLat       float64
}

type nearbyCell struct {
	X int
	Y int
}

type nearbySpatialIndex struct {
	Items []nearbyGeometry
	Cells map[nearbyCell][]int
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
	return !math.IsNaN(lon) && !math.IsNaN(lat) &&
		!math.IsInf(lon, 0) && !math.IsInf(lat, 0) &&
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

	for _, v := range coords {
		pair, ok := v.([]any)
		if !ok || len(pair) < 2 {
			continue
		}

		lon, lonOK := pair[0].(float64)
		lat, latOK := pair[1].(float64)
		if !lonOK || !latOK || !validCoordinate(lon, lat) {
			continue
		}

		path.Points = append(path.Points, nearbyPoint{Lon: lon, Lat: lat})
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

// Decode paths without connecting independent MultiPoint coordinates.
func decodeGeometry(raw json.RawMessage, item *nearbyGeometry) {
	var g rawGeometry
	if err := json.Unmarshal(raw, &g); err != nil {
		return
	}

	switch g.Type {
	case "Point":
		var coords []any
		if json.Unmarshal(g.Coordinates, &coords) == nil {
			if p, ok := makeNearbyPath([]any{coords}); ok {
				item.Paths = append(item.Paths, p)
			}
		}

	case "MultiPoint":
		var coords [][]any
		if json.Unmarshal(g.Coordinates, &coords) == nil {
			for _, coord := range coords {
				if p, ok := makeNearbyPath([]any{coord}); ok {
					item.Paths = append(item.Paths, p)
				}
			}
		}

	case "LineString":
		var coords []any
		if json.Unmarshal(g.Coordinates, &coords) == nil {
			if p, ok := makeNearbyPath(coords); ok {
				item.Paths = append(item.Paths, p)
			}
		}

	case "MultiLineString":
		var lines [][]any
		if json.Unmarshal(g.Coordinates, &lines) == nil {
			for _, line := range lines {
				if p, ok := makeNearbyPath(line); ok {
					item.Paths = append(item.Paths, p)
				}
			}
		}

	case "Polygon":
		var rings [][]any
		if json.Unmarshal(g.Coordinates, &rings) == nil {
			poly := nearbyPolygon{}
			for _, ring := range rings {
				if p, ok := makeNearbyPath(ring); ok {
					poly.Rings = append(poly.Rings, p)
					item.Paths = append(item.Paths, p)
				}
			}
			if len(poly.Rings) > 0 {
				item.Polygons = append(item.Polygons, poly)
			}
		}

	case "MultiPolygon":
		var polygons [][][]any
		if json.Unmarshal(g.Coordinates, &polygons) == nil {
			for _, rings := range polygons {
				poly := nearbyPolygon{}
				for _, ring := range rings {
					if p, ok := makeNearbyPath(ring); ok {
						poly.Rings = append(poly.Rings, p)
						item.Paths = append(item.Paths, p)
					}
				}
				if len(poly.Rings) > 0 {
					item.Polygons = append(item.Polygons, poly)
				}
			}
		}
	}
}

func initGeometryBounds(item *nearbyGeometry) {
	item.MinLon, item.MinLat = math.Inf(1), math.Inf(1)
	item.MaxLon, item.MaxLat = math.Inf(-1), math.Inf(-1)

	for _, p := range item.Paths {
		item.MinLon = math.Min(item.MinLon, p.MinLon)
		item.MaxLon = math.Max(item.MaxLon, p.MaxLon)
		item.MinLat = math.Min(item.MinLat, p.MinLat)
		item.MaxLat = math.Max(item.MaxLat, p.MaxLat)
	}
}

func cellFor(lat, lon float64) nearbyCell {
	// A latitude/longitude grid with approximately 5-mile spacing.
	// Longitude cells are narrower in New York; this remains a conservative
	// candidate index because each geometry is inserted into every covered cell.
	latStep := nearbyCellMiles / 69.0
	lonStep := nearbyCellMiles / (69.172 * math.Cos(lat*math.Pi/180))
	if lonStep < 0.01 {
		lonStep = 0.01
	}
	return nearbyCell{
		X: int(math.Floor(lon / lonStep)),
		Y: int(math.Floor(lat / latStep)),
	}
}

func buildNearbyIndex(items []nearbyGeometry) *nearbySpatialIndex {
	idx := &nearbySpatialIndex{
		Items: items,
		Cells: make(map[nearbyCell][]int),
	}

	// Use a fixed longitude grid sized for New York rather than changing cell
	// width by latitude. This keeps query and insertion keys consistent.
	lonStep := nearbyCellMiles / (69.172 * math.Cos(43*math.Pi/180))
	latStep := nearbyCellMiles / 69.0

	for i := range idx.Items {
		g := &idx.Items[i]
		minX := int(math.Floor(g.MinLon / lonStep))
		maxX := int(math.Floor(g.MaxLon / lonStep))
		minY := int(math.Floor(g.MinLat / latStep))
		maxY := int(math.Floor(g.MaxLat / latStep))

		// Avoid exploding the index for exceptionally large geometries.
		if maxX-minX > 1000 || maxY-minY > 1000 {
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
		return nil, errors.New("database unavailable")
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
		var item nearbyGeometry
		var raw []byte
		if err := rows.Scan(&item.ID, &item.Name, &item.Type,
			&item.Grouped, &item.SegmentCount, &raw); err != nil {
			return nil, err
		}

		var segments []rawSegment
		if err := json.Unmarshal(raw, &segments); err != nil {
			continue
		}

		for _, seg := range segments {
			decodeGeometry(seg.Geometry.Coordinates, &item)
		}
		if len(item.Paths) == 0 {
			continue
		}

		initGeometryBounds(&item)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, errors.New("no waterbody geometry available")
	}

	nearbyCache.index = buildNearbyIndex(items)
	return nearbyCache.index, nil
}

func distanceToPathMiles(lat, lon float64, path nearbyPath) float64 {
	if len(path.Points) == 0 {
		return math.Inf(1)
	}

	scaleX := 69.172 * math.Cos(lat*math.Pi/180)
	scaleY := 69.0
	nearest := math.Inf(1)

	var lastX, lastY float64
	for i, p := range path.Points {
		x := (p.Lon - lon) * scaleX
		y := (p.Lat - lat) * scaleY

		nearest = math.Min(nearest, math.Hypot(x, y))

		if i > 0 {
			dx, dy := x-lastX, y-lastY
			den := dx*dx + dy*dy
			if den > 0 {
				t := math.Max(0, math.Min(1,
					-(lastX*dx+lastY*dy)/den))
				nearest = math.Min(nearest,
					math.Hypot(lastX+t*dx, lastY+t*dy))
			}
		}
		lastX, lastY = x, y
	}
	return nearest
}

func pointInRing(lat, lon float64, ring nearbyPath) bool {
	if !ring.Closed || len(ring.Points) < 4 {
		return false
	}

	inside := false
	j := len(ring.Points) - 1
	for i, p := range ring.Points {
		prev := ring.Points[j]
		if (p.Lat > lat) != (prev.Lat > lat) &&
			lon < (prev.Lon-p.Lon)*(lat-p.Lat)/(prev.Lat-p.Lat)+p.Lon {
			inside = !inside
		}
		j = i
	}
	return inside
}

func pointInPolygon(lat, lon float64, poly nearbyPolygon) bool {
	if len(poly.Rings) == 0 || !pointInRing(lat, lon, poly.Rings[0]) {
		return false
	}
	for _, hole := range poly.Rings[1:] {
		if pointInRing(lat, lon, hole) {
			return false
		}
	}
	return true
}

func geometryDistance(lat, lon float64, g *nearbyGeometry) float64 {
	// A bounding-box lower bound avoids expensive path scans for distant items.
	dx := 0.0
	if lon < g.MinLon {
		dx = (g.MinLon - lon) * 69.172 * math.Cos(lat*math.Pi/180)
	} else if lon > g.MaxLon {
		dx = (lon - g.MaxLon) * 69.172 * math.Cos(lat*math.Pi/180)
	}
	dy := 0.0
	if lat < g.MinLat {
		dy = (g.MinLat - lat) * 69.0
	} else if lat > g.MaxLat {
		dy = (lat - g.MaxLat) * 69.0
	}
	best := math.Hypot(dx, dy)

	if best > 25 {
		return best
	}
	for _, poly := range g.Polygons {
		if pointInPolygon(lat, lon, poly) {
			return 0
		}
	}
	for _, path := range g.Paths {
		if lon < path.MinLon-1 || lon > path.MaxLon+1 ||
			lat < path.MinLat-1 || lat > path.MaxLat+1 {
			continue
		}
		d := distanceToPathMiles(lat, lon, path)
		if d < best {
			best = d
		}
		if best == 0 {
			break
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
func (h nearbyMaxHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *nearbyMaxHeap) Push(v any)   { *h = append(*h, v.(nearbyResult)) }
func (h *nearbyMaxHeap) Pop() any {
	old := *h
	n := len(old)
	v := old[n-1]
	*h = old[:n-1]
	return v
}

func queryNearby(ctx context.Context, lat, lon, radius float64) ([]nearbyItem, error) {
	idx, err := loadNearbyIndex(ctx)
	if err != nil {
		return nil, err
	}

	lonStep := nearbyCellMiles / (69.172 * math.Cos(43*math.Pi/180))
	latStep := nearbyCellMiles / 69.0

	latPad := radius / 69.0
	lonPad := radius / (69.172 * math.Cos(lat*math.Pi/180))
	minX := int(math.Floor((lon - lonPad) / lonStep))
	maxX := int(math.Floor((lon + lonPad) / lonStep))
	minY := int(math.Floor((lat - latPad) / latStep))
	maxY := int(math.Floor((lat + latPad) / latStep))

	seen := make(map[int]struct{}, 256)
	results := make(nearbyMaxHeap, 0, nearbyMaxItems)
	heap.Init(&results)

	for x := minX; x <= maxX; x++ {
		for y := minY; y <= maxY; y++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}

			for _, i := range idx.Cells[nearbyCell{X: x, Y: y}] {
				if _, ok := seen[i]; ok {
					continue
				}
				seen[i] = struct{}{}

				g := &idx.Items[i]
				d := geometryDistance(lat, lon, g)
				if d > radius {
					continue
				}

				item := nearbyItem{
					ID: g.ID, Name: g.Name, Type: g.Type,
					Grouped: g.Grouped, SegmentCount: g.SegmentCount,
					DistanceMiles: math.Round(d*10) / 10,
				}
				candidate := nearbyResult{item: item}
				if results.Len() < nearbyMaxItems {
					heap.Push(&results, candidate)
				} else if candidate.item.DistanceMiles < results[0].item.DistanceMiles ||
					(candidate.item.DistanceMiles == results[0].item.DistanceMiles &&
						candidate.item.Name < results[0].item.Name) {
					heap.Pop(&results)
					heap.Push(&results, candidate)
				}
			}
		}
	}

	found := make([]nearbyItem, 0, results.Len())
	for results.Len() > 0 {
		found = append(found, heap.Pop(&results).(nearbyResult).item)
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].DistanceMiles == found[j].DistanceMiles {
			return found[i].Name < found[j].Name
		}
		return found[i].DistanceMiles < found[j].DistanceMiles
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
		lat < 40 || lat > 46 || lon < -80.5 || lon > -71 ||
		math.IsNaN(radius) || math.IsInf(radius, 0) ||
		radius < 1 || radius > 25 {
		http.Error(w, "invalid NY coordinates or radius (1-25 miles)",
			http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	start := time.Now()
	found, err := queryNearby(ctx, lat, lon, radius)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			http.Error(w, "nearby search timed out", http.StatusGatewayTimeout)
			return
		}
		if errors.Is(err, context.Canceled) {
			return
		}
		http.Error(w, "nearby waterbody index unavailable",
			http.StatusServiceUnavailable)
		log.Printf("nearby index/search error: %v", err)
		return
	}

	log.Printf("nearby search: %.2fms; results=%d",
		float64(time.Since(start).Microseconds())/1000, len(found))

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=600")
	if err := json.NewEncoder(w).Encode(found); err != nil {
		log.Printf("nearby response error: %v", err)
	}
}

// Keep strconv referenced in builds that share validation helpers.
var _ = fmt.Sprintf
