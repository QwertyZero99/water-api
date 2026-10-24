package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Nearby search uses the stored DEC GeoJSON coordinates. Distances are approximate
// straight-line miles from the ZIP centroid to the waterbody, not road distances.
type nearbyItem struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Type          string  `json:"type"`
	Grouped       bool    `json:"grouped"`
	SegmentCount  int     `json:"segmentCount"`
	DistanceMiles float64 `json:"distanceMiles"`
}
type nearbyGeometry struct {
	ID, Name, Type string
	Grouped        bool
	SegmentCount   int
	Paths          [][][2]float64 // lon, lat
}

var nearbyCache struct {
	sync.Mutex
	items []nearbyGeometry
}

// JSON coordinates may be Point, MultiPoint, LineString, MultiLineString,
// Polygon or MultiPolygon. Retain each path independently (never join rings).
func collectCoordinatePaths(v any, paths *[][][2]float64) {
	a, ok := v.([]any)
	if !ok || len(a) == 0 {
		return
	}
	if len(a) >= 2 {
		if _, lonOK := a[0].(float64); lonOK {
			if _, latOK := a[1].(float64); latOK {
				*paths = append(*paths, [][2]float64{{a[0].(float64), a[1].(float64)}})
				return
			}
		}
	}
	points := make([][2]float64, 0)
	for _, child := range a {
		pair, pairOK := child.([]any)
		if !pairOK || len(pair) < 2 {
			continue
		}
		lon, lonOK := pair[0].(float64)
		lat, latOK := pair[1].(float64)
		if lonOK && latOK && lon >= -180 && lon <= 180 && lat >= -90 && lat <= 90 {
			points = append(points, [2]float64{lon, lat})
		}
	}
	if len(points) > 0 {
		*paths = append(*paths, points)
		return
	}
	for _, child := range a {
		collectCoordinatePaths(child, paths)
	}
}

func loadNearbyIndex(ctx context.Context) ([]nearbyGeometry, error) {
	nearbyCache.Lock()
	defer nearbyCache.Unlock()
	if nearbyCache.items != nil {
		return nearbyCache.items, nil
	}
	if stationStore.db == nil {
		return nil, errors.New("database unavailable")
	}
	rows, err := stationStore.db.QueryContext(ctx,
		`SELECT id, name, waterbody_type, grouped, segment_count, segments
         FROM waterbodies_v2`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]nearbyGeometry, 0, 5000)
	for rows.Next() {
		var item nearbyGeometry
		var raw []byte
		if err := rows.Scan(&item.ID, &item.Name, &item.Type, &item.Grouped,
			&item.SegmentCount, &raw); err != nil {
			return nil, err
		}
		var segments []struct {
			Geometry struct {
				Coordinates any `json:"coordinates"`
			} `json:"geometry"`
		}
		if err := json.Unmarshal(raw, &segments); err != nil {
			continue
		}
		for _, seg := range segments {
			collectCoordinatePaths(seg.Geometry.Coordinates, &item.Paths)
		}
		if len(item.Paths) > 0 {
			items = append(items, item)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// A failed load is not cached; a later search may retry.
	if len(items) == 0 {
		return nil, errors.New("no waterbody geometry available")
	}
	nearbyCache.items = items
	return nearbyCache.items, nil
}

// Point-to-line-segment distance in a local equirectangular projection.
// At NY distances (<=25 mi), distortion is small. All returned distances
// are labeled approximate; this is NOT a flood or access measurement.
func distanceToPathMiles(lat, lon float64, path [][2]float64) float64 {
	if len(path) == 0 {
		return math.Inf(1)
	}
	scaleX := 69.172 * math.Cos(lat*math.Pi/180)
	scaleY := 69.0
	nearest := math.Inf(1)
	px, py := 0.0, 0.0
	var lastX, lastY float64
	for i, point := range path {
		x, y := (point[0]-lon)*scaleX, (point[1]-lat)*scaleY
		d := math.Hypot(x-px, y-py)
		if d < nearest {
			nearest = d
		}
		if i > 0 {
			dx, dy := x-lastX, y-lastY
			denominator := dx*dx + dy*dy
			if denominator > 0 {
				t := math.Max(0, math.Min(1, (-lastX*dx-lastY*dy)/denominator))
				sd := math.Hypot(lastX+t*dx, lastY+t*dy)
				if sd < nearest {
					nearest = sd
				}
			}
		}
		lastX, lastY = x, y
	}
	// An interior point is zero miles from a polygon's water area. For closed
	// polygon rings, test containment via the even-odd ray crossing rule.
	if len(path) >= 4 && path[0] == path[len(path)-1] {
		inside := false
		j := len(path) - 1
		for i := range path {
			xi, yi := path[i][0], path[i][1]
			xj, yj := path[j][0], path[j][1]
			if (yi > lat) != (yj > lat) && lon < (xj-xi)*(lat-yi)/(yj-yi)+xi {
				inside = !inside
			}
			j = i
		}
		if inside {
			return 0
		}
	}
	return nearest
}

func WaterbodiesNearby(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lat, latErr := strconv.ParseFloat(q.Get("lat"), 64)
	lon, lonErr := strconv.ParseFloat(q.Get("lon"), 64)
	radius, radiusErr := strconv.ParseFloat(q.Get("radius"), 64)
	if latErr != nil || lonErr != nil || radiusErr != nil ||
		lat < 40 || lat > 46 || lon < -80.5 || lon > -71 ||
		radius < 1 || radius > 25 || math.IsNaN(radius) {
		http.Error(w, "invalid NY coordinates or radius (1-25 miles)", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 180*time.Second)
	defer cancel()
	candidates, err := loadNearbyIndex(ctx)
	if err != nil {
		http.Error(w, "nearby waterbody index unavailable: "+err.Error(), 503)
		return
	}
	found := make([]nearbyItem, 0)
	for _, item := range candidates {
		nearest := math.Inf(1)
		for _, path := range item.Paths {
			d := distanceToPathMiles(lat, lon, path)
			if d < nearest {
				nearest = d
			}
		}
		if nearest <= radius {
			found = append(found, nearbyItem{ID: item.ID, Name: item.Name, Type: item.Type,
				Grouped: item.Grouped, SegmentCount: item.SegmentCount,
				DistanceMiles: math.Round(nearest*10) / 10})
		}
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].DistanceMiles == found[j].DistanceMiles {
			return found[i].Name < found[j].Name
		}
		return found[i].DistanceMiles < found[j].DistanceMiles
	})
	if len(found) > 25 {
		found = found[:25]
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=600")
	if err := json.NewEncoder(w).Encode(found); err != nil {
		fmt.Printf("nearby response error: %v\n", err)
	}
}
