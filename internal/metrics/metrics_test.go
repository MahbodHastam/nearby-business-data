package metrics

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mahbodhastam/nearby-business-data/internal/scraper"
)

func scrapeText(m *Metrics) string {
	rec := httptest.NewRecorder()
	m.Registry.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	return rec.Body.String()
}

func TestRender(t *testing.T) {
	r := &Registry{}
	c := r.NewCounter("req_total", "Requests.", "route", "code")
	c.Inc("/a", "200")
	c.Add(2, "/a", "200")
	c.Inc("/b", `5"00`)
	g := r.NewGauge("running", "Running.")
	g.Add(2)
	g.Add(-1)
	h := r.NewHistogram("dur_seconds", "Duration.", []float64{1, 5}, "route")
	h.Observe(0.5, "/a")
	h.Observe(1, "/a") // bounds are inclusive
	h.Observe(3, "/a")
	h.Observe(9, "/a")

	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	got := rec.Body.String()

	for _, want := range []string{
		"# TYPE req_total counter\n",
		`req_total{route="/a",code="200"} 3` + "\n",
		`req_total{route="/b",code="5\"00"} 1` + "\n",
		"# TYPE running gauge\nrunning 1\n",
		`dur_seconds_bucket{route="/a",le="1"} 2` + "\n",
		`dur_seconds_bucket{route="/a",le="5"} 3` + "\n",
		`dur_seconds_bucket{route="/a",le="+Inf"} 4` + "\n",
		`dur_seconds_sum{route="/a"} 13.5` + "\n",
		`dur_seconds_count{route="/a"} 4` + "\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain; version=0.0.4") {
		t.Errorf("content type = %q", ct)
	}
}

func TestUnlabeledCounterHasNoBraces(t *testing.T) {
	r := &Registry{}
	r.NewCounter("plain_total", "Plain.").Inc()
	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(rec.Body.String(), "plain_total 1\n") {
		t.Errorf("got:\n%s", rec.Body)
	}
}

func TestWrongLabelCountPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("want panic")
		}
	}()
	(&Registry{}).NewCounter("x", "x", "a").Inc()
}

func TestObserveSearch(t *testing.T) {
	m := New()
	m.ObserveSearch(scraper.Stats{
		ChromeStart: 400 * time.Millisecond, CollectLinks: 8 * time.Second, ScrapePlaces: 4 * time.Second,
		Total: 13 * time.Second, Places: 12, Scraped: 10,
	})
	m.ObserveSearch(scraper.Stats{Total: time.Second, ChromeStart: time.Second, Err: errors.New("blocked")})
	m.ObserveSearch(scraper.Stats{Total: time.Minute, Places: 5, Scraped: 2, Err: errors.New("timeout")})

	got := scrapeText(m)
	for _, want := range []string{
		`nearby_searches_total{result="ok"} 1`,
		`nearby_searches_total{result="error"} 1`,
		`nearby_searches_total{result="partial"} 1`,
		"nearby_places_found_total 17",
		"nearby_places_scraped_total 12",
		"nearby_places_failed_total 5",
		"nearby_chrome_start_duration_seconds_count 2",
		"nearby_collect_links_duration_seconds_count 1", // only the search that got that far
		"nearby_search_duration_seconds_count 3",
	} {
		if !strings.Contains(got, want+"\n") {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}
