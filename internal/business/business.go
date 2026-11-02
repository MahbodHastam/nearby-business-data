package business

import (
	"strconv"
	"strings"
)

type Business struct {
	Name     string   `json:"name"`
	Category string   `json:"category,omitempty"`
	Rating   float64  `json:"rating,omitempty"`
	Reviews  int      `json:"reviews,omitempty"`
	Address  string   `json:"address,omitempty"`
	Phone    string   `json:"phone,omitempty"`
	Website  string   `json:"website,omitempty"`
	Hours    []string `json:"hours,omitempty"`
	Lat      float64  `json:"lat,omitempty"`
	Lng      float64  `json:"lng,omitempty"`
	PlaceID  string   `json:"place_id,omitempty"`
	URL      string   `json:"url"`
}

var CSVHeader = []string{"name", "category", "rating", "reviews", "address", "phone", "website", "hours", "lat", "lng", "place_id", "url"}

func (b Business) CSVRow() []string {
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	return []string{
		b.Name,
		b.Category,
		f(b.Rating),
		strconv.Itoa(b.Reviews),
		b.Address,
		b.Phone,
		b.Website,
		strings.Join(b.Hours, "; "),
		f(b.Lat),
		f(b.Lng),
		b.PlaceID,
		b.URL,
	}
}
