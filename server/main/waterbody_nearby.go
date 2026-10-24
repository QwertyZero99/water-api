package main

import (
	"container/heap"
	"context"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Fast nearest-waterbody search. Run Go from the server/ directory.
// Delete the generated index file after updating waterbodies_v2.
const (
	nearbyCellMiles      = 5.0
	nearbyMaxItems       = 25
	nearbyLatStep        = nearbyCellMiles / 69.0
	nearbyLonStep        = nearbyCellMiles / (69.172 * 0.69)
	nearbyMaxCellSpan    = 1000
	nearbyRoundingMargin = 0.051
	nearbyIndexVersion   = 3
	nearbyIndexFile      = "nearby-waterbodies-index-v3.gob"
)

type nearbyItem struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Type          string  `json:"type"`
	Grouped       bool    `json:"grouped"`
	SegmentCount  int     `json:"segmentCount"`
	DistanceMiles float64 `json:"distanceMiles"`
}
type nearbyPoint struct{ Lon, Lat float64 }
type nearbyPath struct {
	Points                         []nearbyPoint
	MinLon, MaxLon, MinLat, MaxLat float64
	Closed                         bool
}
type nearbyPolygon struct{ Rings []nearbyPath }
type nearbyGeometry struct {
	ID, Name, Type                 string
	Grouped                        bool
	SegmentCount                   int
	Paths                          []nearbyPath
	Polygons                       []nearbyPolygon
	MinLon, MaxLon, MinLat, MaxLat float64
}
type nearbyCell struct{ X, Y int }
type nearbySpatialIndex struct {
	Items     []nearbyGeometry
	Cells     map[nearbyCell][]int
	Oversized []int
}
type nearbyDiskIndex struct {
	Version int
	Index   nearbySpatialIndex
}

var nearbyCache struct {
	sync.RWMutex
	index *nearbySpatialIndex
	// done is closed after an initialization attempt; loading happens only once at a time.
	done    chan struct{}
	lastErr error
}

type rawGeometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
	Geometries  []rawGeometry   `json:"geometries"`
}
type rawSegment struct {
	Geometry rawGeometry `json:"geometry"`
}

func validCoordinate(lon, lat float64) bool {
	return !math.IsNaN(lon) && !math.IsNaN(lat) && !math.IsInf(lon, 0) && !math.IsInf(lat, 0) && lon >= -180 && lon <= 180 && lat >= -90 && lat <= 90
}
func nearbyBounds(points []nearbyPoint) nearbyPath {
	p := nearbyPath{Points: points, MinLon: math.Inf(1), MaxLon: math.Inf(-1), MinLat: math.Inf(1), MaxLat: math.Inf(-1)}
	for _, v := range points {
		p.MinLon = math.Min(p.MinLon, v.Lon)
		p.MaxLon = math.Max(p.MaxLon, v.Lon)
		p.MinLat = math.Min(p.MinLat, v.Lat)
		p.MaxLat = math.Max(p.MaxLat, v.Lat)
	}
	p.Closed = len(points) >= 4 && points[0] == points[len(points)-1]
	return p
}
func makeNearbyPath(coords []any) (nearbyPath, bool) {
	points := make([]nearbyPoint, 0, len(coords))
	for _, value := range coords {
		pair, ok := value.([]any)
		if !ok || len(pair) < 2 {
			continue
		}
		lon, ok1 := pair[0].(float64)
		lat, ok2 := pair[1].(float64)
		if ok1 && ok2 && validCoordinate(lon, lat) {
			points = append(points, nearbyPoint{lon, lat})
		}
	}
	if len(points) == 0 {
		return nearbyPath{}, false
	}
	return nearbyBounds(points), true
}
func asJSONArray(raw json.RawMessage) ([]any, bool) {
	var a []any
	if json.Unmarshal(raw, &a) != nil {
		return nil, false
	}
	return a, true
}
func newNearbyGeometry() nearbyGeometry {
	return nearbyGeometry{MinLon: math.Inf(1), MaxLon: math.Inf(-1), MinLat: math.Inf(1), MaxLat: math.Inf(-1)}
}
func addNearbyPath(item *nearbyGeometry, path nearbyPath) {
	item.Paths = append(item.Paths, path)
	item.MinLon = math.Min(item.MinLon, path.MinLon)
	item.MaxLon = math.Max(item.MaxLon, path.MaxLon)
	item.MinLat = math.Min(item.MinLat, path.MinLat)
	item.MaxLat = math.Max(item.MaxLat, path.MaxLat)
}
func decodeGeometryValue(g rawGeometry, item *nearbyGeometry) {
	if g.Type == "GeometryCollection" {
		for _, child := range g.Geometries {
			decodeGeometryValue(child, item)
		}
		return
	}
	a, ok := asJSONArray(g.Coordinates)
	if !ok {
		return
	}
	switch g.Type {
	case "Point":
		lon, ok1 := coordNumber(a, 0)
		lat, ok2 := coordNumber(a, 1)
		if ok1 && ok2 && validCoordinate(lon, lat) {
			addNearbyPath(item, nearbyBounds([]nearbyPoint{{lon, lat}}))
		}
	case "MultiPoint", "LineString":
		if p, ok := makeNearbyPath(a); ok {
			addNearbyPath(item, p)
		}
	case "MultiLineString", "Polygon":
		polygon := nearbyPolygon{}
		for _, v := range a {
			ring, ok := v.([]any)
			if !ok {
				continue
			}
			if p, ok := makeNearbyPath(ring); ok {
				addNearbyPath(item, p)
				if g.Type == "Polygon" {
					polygon.Rings = append(polygon.Rings, p)
				}
			}
		}
		if len(polygon.Rings) > 0 {
			item.Polygons = append(item.Polygons, polygon)
		}
	case "MultiPolygon":
		for _, poly := range a {
			rings, ok := poly.([]any)
			if !ok {
				continue
			}
			polygon := nearbyPolygon{}
			for _, rv := range rings {
				ring, ok := rv.([]any)
				if !ok {
					continue
				}
				if p, ok := makeNearbyPath(ring); ok {
					addNearbyPath(item, p)
					polygon.Rings = append(polygon.Rings, p)
				}
			}
			if len(polygon.Rings) > 0 {
				item.Polygons = append(item.Polygons, polygon)
			}
		}
	}
}
func coordNumber(a []any, i int) (float64, bool) {
	if i >= len(a) {
		return 0, false
	}
	n, ok := a[i].(float64)
	return n, ok
}
func nearbyCellRange(g *nearbyGeometry) (int, int, int, int) {
	return int(math.Floor(g.MinLon / nearbyLonStep)), int(math.Floor(g.MaxLon / nearbyLonStep)), int(math.Floor(g.MinLat / nearbyLatStep)), int(math.Floor(g.MaxLat / nearbyLatStep))
}
func buildNearbyGrid(items []nearbyGeometry) *nearbySpatialIndex {
	idx := &nearbySpatialIndex{Items: items, Cells: make(map[nearbyCell][]int)}
	for i := range items {
		minX, maxX, minY, maxY := nearbyCellRange(&items[i])
		count := int64(maxX-minX+1) * int64(maxY-minY+1)
		if count > nearbyMaxCellSpan {
			idx.Oversized = append(idx.Oversized, i)
			continue
		}
		for x := minX; x <= maxX; x++ {
			for y := minY; y <= maxY; y++ {
				cell := nearbyCell{x, y}
				idx.Cells[cell] = append(idx.Cells[cell], i)
			}
		}
	}
	return idx
}
func readNearbyDiskIndex() (*nearbySpatialIndex, error) {
	f, err := os.Open(nearbyIndexFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var disk nearbyDiskIndex
	if err = gob.NewDecoder(f).Decode(&disk); err != nil {
		return nil, err
	}
	if disk.Version != nearbyIndexVersion || len(disk.Index.Items) == 0 || disk.Index.Cells == nil {
		return nil, errors.New("incompatible nearby index")
	}
	return &disk.Index, nil
}
func writeNearbyDiskIndex(idx *nearbySpatialIndex) error {
	tmp, err := os.CreateTemp(filepath.Dir(nearbyIndexFile), ".nearby-v3-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = gob.NewEncoder(tmp).Encode(nearbyDiskIndex{Version: nearbyIndexVersion, Index: *idx}); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	// On Windows replacing a present cache file via Rename fails. A cache is
	// immutable once written; don't remove a good file to replace it.
	if _, err = os.Stat(nearbyIndexFile); err == nil {
		return nil
	}
	return os.Rename(name, nearbyIndexFile)
}
func buildNearbyIndex() (*nearbySpatialIndex, error) {
	start := time.Now()
	if cached, err := readNearbyDiskIndex(); err == nil {
		log.Printf("nearby index loaded from disk in %v (%d geometries)", time.Since(start), len(cached.Items))
		return cached, nil
	}
	if stationStore.db == nil {
		return nil, errors.New("database unavailable")
	}
	// Do not link the one-time index build to the first HTTP request's deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	rows, err := stationStore.db.QueryContext(ctx, `SELECT id, name, waterbody_type, grouped, segment_count, segments FROM waterbodies_v2`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]nearbyGeometry, 0, 5000)
	for rows.Next() {
		var item nearbyGeometry
		item = newNearbyGeometry()
		var raw []byte
		if err = rows.Scan(&item.ID, &item.Name, &item.Type, &item.Grouped, &item.SegmentCount, &raw); err != nil {
			return nil, err
		}
		var segments []rawSegment
		if json.Unmarshal(raw, &segments) != nil {
			continue
		}
		for _, s := range segments {
			decodeGeometryValue(s.Geometry, &item)
		}
		if len(item.Paths) > 0 {
			items = append(items, item)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, errors.New("no waterbody geometry available")
	}
	idx := buildNearbyGrid(items)
	log.Printf("nearby index built from DB in %v: %d geometries, %d cells", time.Since(start), len(items), len(idx.Cells))
	if err = writeNearbyDiskIndex(idx); err != nil {
		log.Printf("nearby index disk cache save failed: %v", err)
	}
	return idx, nil
}

// A single shared initialization continues even if the initiating request times out.
func loadNearbyIndex(ctx context.Context) (*nearbySpatialIndex, error) {
	nearbyCache.RLock()
	idx := nearbyCache.index
	done := nearbyCache.done
	nearbyCache.RUnlock()
	if idx != nil {
		return idx, nil
	}
	if done == nil {
		nearbyCache.Lock()
		if nearbyCache.index != nil {
			idx = nearbyCache.index
			nearbyCache.Unlock()
			return idx, nil
		}
		if nearbyCache.done == nil {
			nearbyCache.done = make(chan struct{})
			go func() {
				built, err := buildNearbyIndex()
				nearbyCache.Lock()
				nearbyCache.index = built
				nearbyCache.lastErr = err
				close(nearbyCache.done)
				nearbyCache.Unlock()
			}()
		}
		done = nearbyCache.done
		nearbyCache.Unlock()
	}
	select {
	case <-done:
		nearbyCache.RLock()
		idx = nearbyCache.index
		err := nearbyCache.lastErr
		nearbyCache.RUnlock()
		if err != nil {
			return nil, err
		}
		return idx, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func pointInRing(lat, lon float64, p nearbyPath) bool {
	inside := false
	n := len(p.Points)
	if n < 3 {
		return false
	}
	j := n - 1
	for i := 0; i < n; i++ {
		a, b := p.Points[i], p.Points[j]
		if (a.Lat > lat) != (b.Lat > lat) && lon < (b.Lon-a.Lon)*(lat-a.Lat)/(b.Lat-a.Lat)+a.Lon {
			inside = !inside
		}
		j = i
	}
	return inside
}
func pointInPolygon(lat, lon float64, p nearbyPolygon) bool {
	if len(p.Rings) == 0 {
		return false
	}
	outer := p.Rings[0]
	if lon < outer.MinLon || lon > outer.MaxLon || lat < outer.MinLat || lat > outer.MaxLat || !pointInRing(lat, lon, outer) {
		return false
	}
	for _, hole := range p.Rings[1:] {
		if lon >= hole.MinLon && lon <= hole.MaxLon && lat >= hole.MinLat && lat <= hole.MaxLat && pointInRing(lat, lon, hole) {
			return false
		}
	}
	return true
}
func pathLowerBoundMiles(lat, lon, minLon, maxLon, minLat, maxLat float64) float64 {
	sx := 69.172 * math.Cos(lat*math.Pi/180)
	dx := 0.0
	dy := 0.0
	if lon < minLon {
		dx = (minLon - lon) * sx
	} else if lon > maxLon {
		dx = (lon - maxLon) * sx
	}
	if lat < minLat {
		dy = (minLat - lat) * 69
	} else if lat > maxLat {
		dy = (lat - maxLat) * 69
	}
	return math.Hypot(dx, dy)
}
func geometryLowerBoundMiles(lat, lon float64, g *nearbyGeometry) float64 {
	return pathLowerBoundMiles(lat, lon, g.MinLon, g.MaxLon, g.MinLat, g.MaxLat)
}
func distanceToPathMiles(lat, lon float64, p nearbyPath) float64 {
	if len(p.Points) == 0 {
		return math.Inf(1)
	}
	sx := 69.172 * math.Cos(lat*math.Pi/180)
	best2 := math.Inf(1)
	lx, ly := 0.0, 0.0
	for i, v := range p.Points {
		x, y := (v.Lon-lon)*sx, (v.Lat-lat)*69
		d2 := x*x + y*y
		if d2 < best2 {
			best2 = d2
		}
		if i > 0 {
			dx, dy := x-lx, y-ly
			den := dx*dx + dy*dy
			if den > 0 {
				t := math.Max(0, math.Min(1, -(lx*dx+ly*dy)/den))
				px, py := lx+t*dx, ly+t*dy
				d2 = px*px + py*py
				if d2 < best2 {
					best2 = d2
				}
			}
		}
		lx, ly = x, y
	}
	return math.Sqrt(best2)
}
func geometryDistance(lat, lon float64, g *nearbyGeometry, limit float64) float64 {
	if geometryLowerBoundMiles(lat, lon, g) > limit {
		return math.Inf(1)
	}
	for _, poly := range g.Polygons {
		if pointInPolygon(lat, lon, poly) {
			return 0
		}
	}
	best := limit
	found := false
	for _, path := range g.Paths {
		if pathLowerBoundMiles(lat, lon, path.MinLon, path.MaxLon, path.MinLat, path.MaxLat) > best {
			continue
		}
		d := distanceToPathMiles(lat, lon, path)
		if d <= best {
			best = d
			found = true
		}
		if best == 0 {
			return 0
		}
	}
	if !found {
		return math.Inf(1)
	}
	return best
}

type nearbyResult struct{ item nearbyItem }
type nearbyMaxHeap []nearbyResult

func (h nearbyMaxHeap) Len() int { return len(h) }
func (h nearbyMaxHeap) Less(i, j int) bool {
	if h[i].item.DistanceMiles == h[j].item.DistanceMiles {
		return h[i].item.Name > h[j].item.Name
	}
	return h[i].item.DistanceMiles > h[j].item.DistanceMiles
}
func (h nearbyMaxHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *nearbyMaxHeap) Push(x any)   { *h = append(*h, x.(nearbyResult)) }
func (h *nearbyMaxHeap) Pop() any     { old := *h; n := len(old); v := old[n-1]; *h = old[:n-1]; return v }
func nearbyItemLess(a, b nearbyItem) bool {
	if a.DistanceMiles == b.DistanceMiles {
		return a.Name < b.Name
	}
	return a.DistanceMiles < b.DistanceMiles
}
func queryNearby(ctx context.Context, lat, lon, radius float64) ([]nearbyItem, error) {
	idx, err := loadNearbyIndex(ctx)
	if err != nil {
		return nil, err
	}
	latPad := radius / 69.0
	lonPad := radius / (69.172 * math.Cos(lat*math.Pi/180))
	minX, maxX := int(math.Floor((lon-lonPad)/nearbyLonStep)), int(math.Floor((lon+lonPad)/nearbyLonStep))
	minY, maxY := int(math.Floor((lat-latPad)/nearbyLatStep)), int(math.Floor((lat+latPad)/nearbyLatStep))
	seen := make(map[int]struct{}, 256)
	results := make(nearbyMaxHeap, 0, nearbyMaxItems)
	heap.Init(&results)
	consider := func(i int) {
		if _, ok := seen[i]; ok {
			return
		}
		seen[i] = struct{}{}
		g := &idx.Items[i]
		bound := geometryLowerBoundMiles(lat, lon, g)
		if bound > radius {
			return
		}
		cutoff := radius
		if len(results) == nearbyMaxItems {
			cutoff = math.Min(radius, results[0].item.DistanceMiles+nearbyRoundingMargin)
			if bound > cutoff {
				return
			}
		}
		d := geometryDistance(lat, lon, g, cutoff)
		if d > radius || math.IsInf(d, 1) {
			return
		}
		item := nearbyItem{ID: g.ID, Name: g.Name, Type: g.Type, Grouped: g.Grouped, SegmentCount: g.SegmentCount, DistanceMiles: math.Round(d*10) / 10}
		candidate := nearbyResult{item: item}
		if len(results) < nearbyMaxItems {
			heap.Push(&results, candidate)
		} else if nearbyItemLess(item, results[0].item) {
			heap.Pop(&results)
			heap.Push(&results, candidate)
		}
	}
	for x := minX; x <= maxX; x++ {
		for y := minY; y <= maxY; y++ {
			if err = ctx.Err(); err != nil {
				return nil, err
			}
			for _, i := range idx.Cells[nearbyCell{x, y}] {
				consider(i)
			}
		}
	}
	for _, i := range idx.Oversized {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		consider(i)
	}
	found := make([]nearbyItem, 0, len(results))
	for len(results) > 0 {
		found = append(found, heap.Pop(&results).(nearbyResult).item)
	}
	sort.Slice(found, func(i, j int) bool { return nearbyItemLess(found[i], found[j]) })
	return found, nil
}
func WaterbodiesNearby(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lat, e1 := strconv.ParseFloat(q.Get("lat"), 64)
	lon, e2 := strconv.ParseFloat(q.Get("lon"), 64)
	radius, e3 := strconv.ParseFloat(q.Get("radius"), 64)
	if e1 != nil || e2 != nil || e3 != nil || !validCoordinate(lon, lat) || lat < 40 || lat > 46 || lon < -80.5 || lon > -71 || math.IsNaN(radius) || math.IsInf(radius, 0) || radius < 1 || radius > 25 {
		http.Error(w, "invalid NY coordinates or radius (1-25 miles)", http.StatusBadRequest)
		return
	}
	// Allows the first DB build to finish. Warm queries run independently in milliseconds.
	ctx, cancel := context.WithTimeout(r.Context(), 180*time.Second)
	defer cancel()
	start := time.Now()
	found, err := queryNearby(ctx, lat, lon, radius)
	if err != nil {
		code := http.StatusServiceUnavailable
		if errors.Is(err, context.DeadlineExceeded) {
			code = http.StatusGatewayTimeout
		}
		if errors.Is(err, context.Canceled) {
			return
		}
		log.Printf("nearby query error: %v", err)
		http.Error(w, "nearby waterbody search unavailable", code)
		return
	}
	log.Printf("nearby search completed in %v, %d results", time.Since(start), len(found))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=600")
	if err = json.NewEncoder(w).Encode(found); err != nil {
		log.Print(fmt.Errorf("nearby response: %w", err))
	}
}
