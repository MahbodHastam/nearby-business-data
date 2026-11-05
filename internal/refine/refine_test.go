package refine

import (
	"math"
	"slices"
	"testing"

	"github.com/mahbodhastam/nearby-business-data/internal/business"
)

func ptr[T any](v T) *T { return &v }

// Berlin Alexanderplatz.
var center = &business.Point{Lat: 52.5219, Lng: 13.4132}

var places = []business.Business{
	{Name: "Beta Cafe", Category: "Coffee shop", Rating: 4.5, Reviews: 100, Lat: 52.5225, Lng: 13.4140, OpenNow: ptr(true)},
	{Name: "alpha Bakery", Category: "Bakery", Rating: 4.8, Reviews: 20, Lat: 52.5300, Lng: 13.4200, OpenNow: ptr(false)},
	{Name: "Gamma Cafe", Category: "Cafe", Rating: 4.5, Reviews: 900, Lat: 52.6000, Lng: 13.5000},
	{Name: "No Coords", Category: "Cafe", Rating: 3.0, Reviews: 5},
}

func names(bs []business.Business) []string {
	var out []string
	for _, b := range bs {
		out = append(out, b.Name)
	}
	return out
}

func TestApplyFilters(t *testing.T) {
	tests := []struct {
		name string
		o    Options
		want []string
	}{
		{"none", Options{}, []string{"Beta Cafe", "alpha Bakery", "Gamma Cafe", "No Coords"}},
		{"min rating", Options{MinRating: 4.5}, []string{"Beta Cafe", "alpha Bakery", "Gamma Cafe"}},
		{"min reviews", Options{MinReviews: 100}, []string{"Beta Cafe", "Gamma Cafe"}},
		{"category", Options{Category: " CAFE"}, []string{"Gamma Cafe", "No Coords"}},
		{"open now", Options{OpenNow: true}, []string{"Beta Cafe"}},
		{"radius", Options{Near: center, RadiusM: 2000}, []string{"Beta Cafe", "alpha Bakery"}},
		{"limit", Options{Limit: 2}, []string{"Beta Cafe", "alpha Bakery"}},
	}
	for _, tt := range tests {
		if got := names(Apply(places, tt.o)); !slices.Equal(got, tt.want) {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestApplySort(t *testing.T) {
	tests := []struct {
		sort string
		want []string
	}{
		{SortRating, []string{"alpha Bakery", "Gamma Cafe", "Beta Cafe", "No Coords"}}, // ties on rating go to more reviews
		{SortReviews, []string{"Gamma Cafe", "Beta Cafe", "alpha Bakery", "No Coords"}},
		{SortDistance, []string{"Beta Cafe", "alpha Bakery", "Gamma Cafe", "No Coords"}},
		{SortName, []string{"alpha Bakery", "Beta Cafe", "Gamma Cafe", "No Coords"}},
	}
	for _, tt := range tests {
		if got := names(Apply(places, Options{Near: center, Sort: tt.sort})); !slices.Equal(got, tt.want) {
			t.Errorf("sort %s: got %v, want %v", tt.sort, got, tt.want)
		}
	}
}

func TestApplyDistanceAndNoMutation(t *testing.T) {
	in := slices.Clone(places)
	got := Apply(in, Options{Near: center, Sort: SortDistance})
	if got[0].DistanceM < 50 || got[0].DistanceM > 100 {
		t.Errorf("distance = %d, want ~70m", got[0].DistanceM)
	}
	if got[len(got)-1].DistanceM != 0 {
		t.Errorf("place without coordinates got distance %d", got[len(got)-1].DistanceM)
	}
	for i := range in {
		if in[i].Name != places[i].Name || in[i].DistanceM != 0 {
			t.Fatalf("input modified at %d: %+v", i, in[i])
		}
	}
}

func TestDistance(t *testing.T) {
	// Berlin to Paris is about 878 km.
	got := Distance(business.Point{Lat: 52.52, Lng: 13.405}, business.Point{Lat: 48.8566, Lng: 2.3522})
	if math.Abs(got-878000) > 5000 {
		t.Errorf("distance = %.0f m", got)
	}
}

func TestValidateAndActive(t *testing.T) {
	bad := []Options{
		{Sort: "price"},
		{Sort: SortDistance},
		{RadiusM: 100},
		{RadiusM: -1, Near: center},
		{MinRating: 6},
		{MinReviews: -1},
	}
	for _, o := range bad {
		if o.Validate() == nil {
			t.Errorf("%+v: want error", o)
		}
	}
	if err := (Options{Near: center, RadiusM: 5, Sort: SortDistance, MinRating: 4}).Validate(); err != nil {
		t.Error(err)
	}
	if (Options{Limit: 5, Near: center}).Active() {
		t.Error("limit and point alone must not be active")
	}
	if !(Options{Category: "x"}).Active() || !(Options{Sort: SortName}).Active() {
		t.Error("filter or sort must be active")
	}
}
