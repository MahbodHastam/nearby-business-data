package scraper

import (
	"testing"

	"github.com/mahbodhastam/nearby-business-data/internal/business"
)

func TestSearchURL(t *testing.T) {
	city := searchURL(Query{Subject: "coffee shop", City: "Berlin"})
	if want := "https://www.google.com/maps/search/coffee%20shop%20in%20Berlin?hl=en"; city != want {
		t.Errorf("city url = %q; want %q", city, want)
	}

	near := searchURL(Query{Subject: "coffee", City: "ignored", Near: &business.Point{Lat: 52.52, Lng: 13.405}, RadiusM: 1000})
	if want := "https://www.google.com/maps/search/coffee/@52.520000,13.405000,15z?hl=en"; near != want {
		t.Errorf("point url = %q; want %q", near, want)
	}
}

func TestZoomFor(t *testing.T) {
	tests := []struct {
		lat, radius float64
		want        int
	}{
		{52.52, 0, 14},      // default
		{52.52, 1000, 15},   // ~1 km
		{52.52, 5000, 13},   // ~5 km
		{0, 50, 18},         // clamped to max zoom
		{0, 100_000_000, 3}, // clamped to min zoom
		{52.52, 100_000, 9}, // wide area
		{-33.87, 1000, 16},  // southern hemisphere
	}
	for _, tt := range tests {
		if got := zoomFor(tt.lat, tt.radius); got != tt.want {
			t.Errorf("zoomFor(%v, %v) = %d; want %d", tt.lat, tt.radius, got, tt.want)
		}
	}
}
