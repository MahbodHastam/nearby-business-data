//go:build integration

package scraper

import (
	"context"
	"testing"
	"time"
)

// Hits the real Google Maps. Run with: go test -tags integration ./internal/scraper/
func TestSearchLive(t *testing.T) {
	s := New(Options{Headless: true, Timeout: 2 * time.Minute})
	got, err := s.Search(context.Background(), Query{Subject: "coffee", City: "Berlin", Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("no results")
	}
	for _, b := range got {
		if b.Name == "" || b.Address == "" || b.Lat == 0 {
			t.Errorf("incomplete result: %+v", b)
		}
	}
}
