package scraper

import (
	"slices"
	"testing"
)

const sampleURL = "https://www.google.com/maps/place/Grandaire/data=!4m7!3m6!1s0x47a84f5a22092c49:0x42134cfee294f7cd!8m2!3d52.5178121!4d13.416528!16s%2Fg%2F11y30_5kzw!19sChIJSSwJIlpPqEcRzfeU4v5ME0I"

func TestParseCoords(t *testing.T) {
	tests := []struct {
		url      string
		lat, lng float64
	}{
		{sampleURL, 52.5178121, 13.416528},
		{"https://www.google.com/maps/place/X/data=!3d-33.86!4d-151.2", -33.86, -151.2},
		{"https://www.google.com/maps/place/X", 0, 0},
	}
	for _, tt := range tests {
		lat, lng := parseCoords(tt.url)
		if lat != tt.lat || lng != tt.lng {
			t.Errorf("parseCoords(%q) = %v, %v; want %v, %v", tt.url, lat, lng, tt.lat, tt.lng)
		}
	}
}

func TestParsePlaceID(t *testing.T) {
	if got, want := parsePlaceID(sampleURL), "ChIJSSwJIlpPqEcRzfeU4v5ME0I"; got != want {
		t.Errorf("parsePlaceID = %q; want %q", got, want)
	}
	if got := parsePlaceID("https://www.google.com/maps/place/X"); got != "" {
		t.Errorf("parsePlaceID without id = %q; want empty", got)
	}
}

func TestParseRating(t *testing.T) {
	tests := map[string]float64{
		"4.8 stars ": 4.8,
		"5 stars":    5,
		"4,6 stars":  4.6,
		"":           0,
		"no rating":  0,
	}
	for in, want := range tests {
		if got := parseRating(in); got != want {
			t.Errorf("parseRating(%q) = %v; want %v", in, got, want)
		}
	}
}

func TestParseCount(t *testing.T) {
	tests := map[string]int{
		"567 reviews":   567,
		"1,234 reviews": 1234,
		"1 review":      1,
		"":              0,
	}
	for in, want := range tests {
		if got := parseCount(in); got != want {
			t.Errorf("parseCount(%q) = %v; want %v", in, got, want)
		}
	}
}

func TestToBusiness(t *testing.T) {
	b := toBusiness(rawPlace{
		Name:    " Grandaire ",
		Rating:  "4.8 stars ",
		Reviews: "567 reviews",
		Address: "Address: Voltairestraße 11, 10179 Berlin ",
		Phone:   "phone:tel:015563807676",
		Hours:   []string{"Monday: 8 am–11 pm"},
	}, sampleURL)

	if b.Name != "Grandaire" || b.Address != "Voltairestraße 11, 10179 Berlin" || b.Phone != "015563807676" {
		t.Errorf("text fields not cleaned: %+v", b)
	}
	if b.Rating != 4.8 || b.Reviews != 567 || b.Lat != 52.5178121 || b.PlaceID == "" {
		t.Errorf("parsed fields wrong: %+v", b)
	}
}

func TestDedupeAndStripQuery(t *testing.T) {
	in := []string{"a?x=1", "b", "a?x=2", "b"}
	for i := range in {
		in[i] = stripQuery(in[i])
	}
	if got := dedupe(in); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("dedupe = %v", got)
	}
}
