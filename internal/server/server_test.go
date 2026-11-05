package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mahbodhastam/nearby-business-data/internal/business"
	"github.com/mahbodhastam/nearby-business-data/internal/cache"
	"github.com/mahbodhastam/nearby-business-data/internal/metrics"
	"github.com/mahbodhastam/nearby-business-data/internal/scraper"
)

func fakeSearch(_ context.Context, q scraper.Query) ([]business.Business, error) {
	out := make([]business.Business, q.Limit)
	for i := range out {
		out[i] = business.Business{Name: q.Subject + " place", URL: "u"}
	}
	return out, nil
}

func get(h http.Handler, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestSearchJSON(t *testing.T) {
	rec := get(New(Config{Search: fakeSearch, MaxJobs: 1}), "/search?q=coffee&city=Berlin&limit=3")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body %s", rec.Code, rec.Body)
	}
	var resp Response
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Query != "coffee" || resp.City != "Berlin" || resp.Count != 3 || len(resp.Results) != 3 {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestSearchCSV(t *testing.T) {
	rec := get(New(Config{Search: fakeSearch, MaxJobs: 1}), "/search?q=coffee&city=Berlin&limit=2&format=csv")
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("status = %d, type = %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if lines := strings.Count(rec.Body.String(), "\n"); lines != 3 {
		t.Errorf("want header + 2 rows, got %d lines", lines)
	}
}

func TestSearchBadRequest(t *testing.T) {
	h := New(Config{Search: fakeSearch, MaxJobs: 1})
	for _, target := range []string{
		"/search?city=Berlin",
		"/search?q=coffee",
		"/search?q=coffee&city=Berlin&limit=0",
		"/search?q=coffee&city=Berlin&limit=101",
		"/search?q=coffee&city=Berlin&limit=abc",
		"/search?q=coffee&city=Berlin&format=xml",
		"/search?q=coffee",                             // neither city nor point
		"/search?q=coffee&lat=52.5",                    // lat without lng
		"/search?q=coffee&lat=abc&lng=1",               // not a number
		"/search?q=coffee&lat=91&lng=1",                // out of range
		"/search?q=coffee&city=Berlin&radius=500",      // radius without point
		"/search?q=coffee&lat=1&lng=1&radius=-5",       // negative radius
		"/search?q=coffee&lat=1&lng=1&radius=60000",    // over the cap
		"/search?q=coffee&city=Berlin&sort=distance",   // distance sort without point
		"/search?q=coffee&city=Berlin&sort=price",      // unknown sort
		"/search?q=coffee&city=Berlin&min_rating=6",    // over 5
		"/search?q=coffee&city=Berlin&min_rating=x",    // not a number
		"/search?q=coffee&city=Berlin&min_reviews=1.5", // not whole
		"/search?q=coffee&city=Berlin&open_now=maybe",  // not a bool
	} {
		if rec := get(h, target); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d; want 400", target, rec.Code)
		}
	}
}

func TestSearchBusy(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	blocking := func(ctx context.Context, q scraper.Query) ([]business.Business, error) {
		close(started)
		<-release
		return nil, nil
	}
	h := New(Config{Search: blocking, MaxJobs: 1})

	done := make(chan struct{})
	go func() { get(h, "/search?q=a&city=b"); close(done) }()
	<-started

	// A different search can't share the running scrape, and there is no free slot.
	if rec := get(h, "/search?q=other&city=b"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("second request status = %d; want 503", rec.Code)
	}
	close(release)
	<-done
}

func TestSearchFailure(t *testing.T) {
	failing := func(context.Context, scraper.Query) ([]business.Business, error) {
		return nil, errors.New("blocked")
	}
	if rec := get(New(Config{Search: failing, MaxJobs: 1}), "/search?q=a&city=b"); rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d; want 502", rec.Code)
	}
}

func TestHealthz(t *testing.T) {
	if rec := get(New(Config{Search: fakeSearch, MaxJobs: 1}), "/healthz"); rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
}

func ptr[T any](v T) *T { return &v }

var catalog = []business.Business{
	{Name: "Far Cafe", Category: "Cafe", Rating: 4.9, Reviews: 800, Lat: 52.60, Lng: 13.50, OpenNow: ptr(true)},
	{Name: "Near Cafe", Category: "Cafe", Rating: 4.2, Reviews: 50, Lat: 52.5225, Lng: 13.4140, OpenNow: ptr(true)},
	{Name: "Mid Bakery", Category: "Bakery", Rating: 4.6, Reviews: 300, Lat: 52.5300, Lng: 13.4200, OpenNow: ptr(false)},
}

// recorder returns a search over catalog that remembers what it was asked.
func recorder() (SearchFunc, *[]scraper.Query) {
	var seen []scraper.Query
	return func(_ context.Context, q scraper.Query) ([]business.Business, error) {
		seen = append(seen, q)
		return catalog, nil
	}, &seen
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) Response {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body %s", rec.Code, rec.Body)
	}
	var resp Response
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

func resultNames(r Response) []string {
	var out []string
	for _, b := range r.Results {
		out = append(out, b.Name)
	}
	return out
}

func TestFiltersAndSort(t *testing.T) {
	search, seen := recorder()
	h := New(Config{Search: search, MaxJobs: 1})

	tests := []struct {
		query string
		want  string
	}{
		{"min_rating=4.5&sort=rating", "Far Cafe,Mid Bakery"},
		{"min_reviews=100&sort=reviews", "Far Cafe,Mid Bakery"},
		{"category=cafe&sort=name", "Far Cafe,Near Cafe"},
		{"open_now=true&sort=name", "Far Cafe,Near Cafe"},
		{"category=cafe&sort=rating&limit=1", "Far Cafe"},
	}
	for _, tt := range tests {
		got := strings.Join(resultNames(decode(t, get(h, "/search?q=x&city=Berlin&"+tt.query))), ",")
		if got != tt.want {
			t.Errorf("%s: got %s; want %s", tt.query, got, tt.want)
		}
	}

	// Filters need more candidates than the limit to pick from.
	for _, q := range *seen {
		if q.Limit != candidateLimit {
			t.Errorf("scrape limit = %d; want %d when filtering", q.Limit, candidateLimit)
		}
	}
}

func TestPlainSearchScrapesOnlyLimit(t *testing.T) {
	search, seen := recorder()
	decode(t, get(New(Config{Search: search, MaxJobs: 1}), "/search?q=x&city=Berlin&limit=7"))
	if len(*seen) != 1 || (*seen)[0].Limit != 7 {
		t.Errorf("scrape queries = %+v; want one with limit 7", *seen)
	}
}

func TestLocationSearch(t *testing.T) {
	search, seen := recorder()
	h := New(Config{Search: search, MaxJobs: 1})

	resp := decode(t, get(h, "/search?q=cafe&lat=52.5219&lng=13.4132&radius=2000&sort=distance"))
	if got := strings.Join(resultNames(resp), ","); got != "Near Cafe,Mid Bakery" {
		t.Errorf("results = %s; Far Cafe is >2 km away and must be dropped", got)
	}
	if resp.Results[0].DistanceM == 0 || resp.Results[0].DistanceM > resp.Results[1].DistanceM {
		t.Errorf("distances = %d, %d", resp.Results[0].DistanceM, resp.Results[1].DistanceM)
	}
	if resp.City != "" || resp.Near == nil || resp.Near.Lat != 52.5219 || resp.Near.RadiusM != 2000 {
		t.Errorf("response = %+v", resp)
	}
	q := (*seen)[0]
	if q.Near == nil || q.Near.Lng != 13.4132 || q.RadiusM != 2000 || q.City != "" {
		t.Errorf("scrape query = %+v", q)
	}
}

func TestCacheServesRepeatsAndFilterVariants(t *testing.T) {
	calls := 0
	search := func(context.Context, scraper.Query) ([]business.Business, error) {
		calls++
		return catalog, nil
	}
	h := New(Config{Search: search, MaxJobs: 1, Cache: cache.New(time.Minute, 10)})

	if got := get(h, "/search?q=Cafe&city=Berlin").Header().Get("X-Cache"); got != "miss" {
		t.Errorf("first X-Cache = %q", got)
	}
	// Same scrape, different case/filters/sort: served from cache.
	for _, target := range []string{
		"/search?q=cafe&city=berlin",
		"/search?q=cafe&city=berlin&format=csv",
	} {
		if got := get(h, target).Header().Get("X-Cache"); got != "hit" {
			t.Errorf("%s: X-Cache = %q; want hit", target, got)
		}
	}
	if got := get(h, "/search?q=cafe&city=berlin&sort=name").Header().Get("X-Cache"); got != "miss" {
		t.Errorf("sorted request needs more candidates, so it is its own scrape; X-Cache = %q", got)
	}
	if got := get(h, "/search?q=cafe&city=berlin&sort=rating&min_rating=4").Header().Get("X-Cache"); got != "hit" {
		t.Errorf("filter variants share one scrape; X-Cache = %q", got)
	}
	if calls != 2 {
		t.Errorf("scrapes = %d; want 2", calls)
	}
}

func TestIdenticalRequestsShareOneScrape(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	search := func(context.Context, scraper.Query) ([]business.Business, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return catalog, nil
	}
	h := New(Config{Search: search, MaxJobs: 1}) // one slot: a second scrape would get a 503

	var wg sync.WaitGroup
	codes := make(chan int, 2)
	statuses := make(chan string, 2)
	run := func() {
		defer wg.Done()
		rec := get(h, "/search?q=cafe&city=Berlin")
		codes <- rec.Code
		statuses <- rec.Header().Get("X-Cache")
	}
	wg.Add(1)
	go run()
	<-started
	wg.Add(1)
	go run()
	time.Sleep(50 * time.Millisecond) // let the second request join
	close(release)
	wg.Wait()

	if c1, c2 := <-codes, <-codes; c1 != 200 || c2 != 200 {
		t.Errorf("codes = %d, %d", c1, c2)
	}
	if s1, s2 := <-statuses, <-statuses; !(s1 == "miss" && s2 == "coalesced" || s1 == "coalesced" && s2 == "miss") {
		t.Errorf("X-Cache = %s, %s", s1, s2)
	}
	if calls.Load() != 1 {
		t.Errorf("scrapes = %d; want 1", calls.Load())
	}
}

func TestMetricsEndpoint(t *testing.T) {
	m := metrics.New()
	h := New(Config{Search: fakeSearch, MaxJobs: 1, Metrics: m})
	get(h, "/search?q=a&city=b&limit=1")
	get(h, "/search?q=a") // 400
	get(h, "/nope")       // unmatched
	got := get(h, "/metrics").Body.String()

	for _, want := range []string{
		`nearby_http_requests_total{route="GET /search",code="200"} 1`,
		`nearby_http_requests_total{route="GET /search",code="400"} 1`,
		`nearby_http_requests_total{route="unmatched",code="404"} 1`,
		`nearby_cache_requests_total{status="miss"} 1`,
		`nearby_http_request_duration_seconds_count{route="GET /search"} 2`,
		"nearby_searches_in_flight 0",
	} {
		if !strings.Contains(got, want+"\n") {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestBusyCounted(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	blocking := func(context.Context, scraper.Query) ([]business.Business, error) {
		close(started)
		<-release
		return nil, nil
	}
	m := metrics.New()
	h := New(Config{Search: blocking, MaxJobs: 1, Metrics: m})
	done := make(chan struct{})
	go func() { get(h, "/search?q=a&city=b"); close(done) }()
	<-started
	get(h, "/search?q=other&city=b")
	close(release)
	<-done

	if got := get(h, "/metrics").Body.String(); !strings.Contains(got, "nearby_busy_rejections_total 1\n") {
		t.Errorf("busy rejection not counted:\n%s", got)
	}
}
