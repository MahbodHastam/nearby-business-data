// Package refine filters, sorts and truncates scraped results. It works on
// results after scraping, so one cached scrape can serve many different
// filter combinations.
package refine

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/mahbodhastam/nearby-business-data/internal/business"
)

const (
	SortRating   = "rating"
	SortReviews  = "reviews"
	SortDistance = "distance"
	SortName     = "name"
)

type Options struct {
	Near       *business.Point // enables distance_m and distance filtering/sorting
	RadiusM    float64         // drop places farther than this from Near, 0 = off
	MinRating  float64
	MinReviews int
	Category   string // case-insensitive substring
	OpenNow    bool
	Sort       string // rating, reviews (both high to low), distance (near to far) or name
	Limit      int    // 0 = no cap
}

func (o Options) Validate() error {
	switch o.Sort {
	case "", SortRating, SortReviews, SortName:
	case SortDistance:
		if o.Near == nil {
			return fmt.Errorf("sort=distance needs a search point")
		}
	default:
		return fmt.Errorf("unknown sort %q, use rating, reviews, distance or name", o.Sort)
	}
	if o.RadiusM < 0 {
		return fmt.Errorf("radius must not be negative")
	}
	if o.RadiusM > 0 && o.Near == nil {
		return fmt.Errorf("radius needs a search point")
	}
	if o.MinRating < 0 || o.MinRating > 5 {
		return fmt.Errorf("min rating must be between 0 and 5")
	}
	if o.MinReviews < 0 {
		return fmt.Errorf("min reviews must not be negative")
	}
	return nil
}

// Active reports whether Apply can drop or reorder results. If so, scraping
// has to fetch more than Limit results for the final list to be right.
func (o Options) Active() bool {
	return o.RadiusM > 0 || o.MinRating > 0 || o.MinReviews > 0 || o.Category != "" || o.OpenNow || o.Sort != ""
}

// Apply returns a new slice; bs and the businesses in it are not modified,
// so it is safe to call on cached results.
func Apply(bs []business.Business, o Options) []business.Business {
	cat := strings.ToLower(strings.TrimSpace(o.Category))
	out := make([]business.Business, 0, len(bs))
	for _, b := range bs {
		located := b.Lat != 0 || b.Lng != 0
		if o.Near != nil && located {
			b.DistanceM = int(math.Round(Distance(*o.Near, business.Point{Lat: b.Lat, Lng: b.Lng})))
		}
		switch {
		case b.Rating < o.MinRating, b.Reviews < o.MinReviews:
			continue
		case cat != "" && !strings.Contains(strings.ToLower(b.Category), cat):
			continue
		case o.OpenNow && (b.OpenNow == nil || !*b.OpenNow):
			continue
		case o.RadiusM > 0 && (!located || float64(b.DistanceM) > o.RadiusM):
			continue
		}
		out = append(out, b)
	}

	switch o.Sort {
	case SortRating:
		slices.SortStableFunc(out, func(a, b business.Business) int {
			return cmp.Or(cmp.Compare(b.Rating, a.Rating), cmp.Compare(b.Reviews, a.Reviews))
		})
	case SortReviews:
		slices.SortStableFunc(out, func(a, b business.Business) int { return cmp.Compare(b.Reviews, a.Reviews) })
	case SortDistance:
		// Places without coordinates have no distance and go last.
		slices.SortStableFunc(out, func(a, b business.Business) int {
			return cmp.Or(
				cmp.Compare(noDistance(a), noDistance(b)),
				cmp.Compare(a.DistanceM, b.DistanceM),
			)
		})
	case SortName:
		slices.SortStableFunc(out, func(a, b business.Business) int {
			return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		})
	}

	if o.Limit > 0 && len(out) > o.Limit {
		out = out[:o.Limit]
	}
	return out
}

func noDistance(b business.Business) int {
	if b.Lat == 0 && b.Lng == 0 {
		return 1
	}
	return 0
}

// Distance is the great-circle distance in meters.
func Distance(a, b business.Point) float64 {
	const earthRadiusM = 6371000
	rad := math.Pi / 180
	dLat := (b.Lat - a.Lat) * rad
	dLng := (b.Lng - a.Lng) * rad
	h := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(a.Lat*rad)*math.Cos(b.Lat*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * earthRadiusM * math.Asin(math.Sqrt(h))
}
