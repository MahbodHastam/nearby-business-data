package business

import (
	"fmt"
	"strconv"
	"strings"
)

// Point is a WGS84 coordinate.
type Point struct {
	Lat float64
	Lng float64
}

// NewPoint validates the coordinate ranges.
func NewPoint(lat, lng float64) (*Point, error) {
	if lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return nil, fmt.Errorf("coordinates out of range: %g,%g", lat, lng)
	}
	return &Point{lat, lng}, nil
}

// ParsePoint parses "lat,lng".
func ParsePoint(s string) (*Point, error) {
	a, b, ok := strings.Cut(s, ",")
	lat, err1 := strconv.ParseFloat(strings.TrimSpace(a), 64)
	lng, err2 := strconv.ParseFloat(strings.TrimSpace(b), 64)
	if !ok || err1 != nil || err2 != nil {
		return nil, fmt.Errorf("invalid coordinates %q, want lat,lng", s)
	}
	return NewPoint(lat, lng)
}

type Business struct {
	Name     string   `json:"name"`
	Category string   `json:"category,omitempty"`
	Rating   float64  `json:"rating,omitempty"`
	Reviews  int      `json:"reviews,omitempty"`
	Address  string   `json:"address,omitempty"`
	Phone    string   `json:"phone,omitempty"`
	Website  string   `json:"website,omitempty"`
	Hours    []string `json:"hours,omitempty"`
	// OpenNow is read from the place page at scrape time, so cached results
	// can be stale. Nil when Maps doesn't say.
	OpenNow *bool   `json:"open_now,omitempty"`
	Lat     float64 `json:"lat,omitempty"`
	Lng     float64 `json:"lng,omitempty"`
	PlaceID string  `json:"place_id,omitempty"`
	URL     string  `json:"url"`
	// DistanceM is meters from the search point, set only for searches with one.
	DistanceM int `json:"distance_m,omitempty"`
}

var CSVHeader = []string{"name", "category", "rating", "reviews", "address", "phone", "website", "hours", "open_now", "lat", "lng", "distance_m", "place_id", "url"}

func (b Business) CSVRow() []string {
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	openNow := ""
	if b.OpenNow != nil {
		openNow = strconv.FormatBool(*b.OpenNow)
	}
	distance := ""
	if b.DistanceM > 0 {
		distance = strconv.Itoa(b.DistanceM)
	}
	return []string{
		b.Name,
		b.Category,
		f(b.Rating),
		strconv.Itoa(b.Reviews),
		b.Address,
		b.Phone,
		b.Website,
		strings.Join(b.Hours, "; "),
		openNow,
		f(b.Lat),
		f(b.Lng),
		distance,
		b.PlaceID,
		b.URL,
	}
}
