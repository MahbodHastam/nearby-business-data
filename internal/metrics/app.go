package metrics

import (
	"github.com/mahbodhastam/nearby-business-data/internal/scraper"
)

var (
	// Searches and HTTP requests take seconds to minutes.
	slowBuckets = []float64{0.1, 0.5, 1, 2.5, 5, 10, 20, 40, 80, 160, 300}
	// Starting Chrome is well under a few seconds unless the machine is starved.
	startBuckets = []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10}
)

// Metrics holds every metric the app exports.
type Metrics struct {
	Registry *Registry

	HTTPRequests  *Counter   // route, code
	HTTPDuration  *Histogram // route
	CacheRequests *Counter   // status: hit, miss, coalesced
	BusyRejected  *Counter
	InFlight      *Gauge

	Searches       *Counter   // result: ok, partial, error
	SearchDuration *Histogram // seconds, whole search
	ChromeStart    *Histogram
	CollectLinks   *Histogram
	ScrapePlaces   *Histogram
	PlacesFound    *Counter
	PlacesScraped  *Counter
	PlacesFailed   *Counter
}

func New() *Metrics {
	r := &Registry{}
	return &Metrics{
		Registry: r,

		HTTPRequests:  r.NewCounter("nearby_http_requests_total", "HTTP requests by route and status code.", "route", "code"),
		HTTPDuration:  r.NewHistogram("nearby_http_request_duration_seconds", "HTTP request duration by route.", slowBuckets, "route"),
		CacheRequests: r.NewCounter("nearby_cache_requests_total", "Search lookups by cache status: hit, miss or coalesced.", "status"),
		BusyRejected:  r.NewCounter("nearby_busy_rejections_total", "Searches turned away because max-jobs were already running."),
		InFlight:      r.NewGauge("nearby_searches_in_flight", "Scrapes running right now."),

		Searches:       r.NewCounter("nearby_searches_total", "Finished scrapes: ok, partial (stopped early with some results) or error.", "result"),
		SearchDuration: r.NewHistogram("nearby_search_duration_seconds", "Time of a whole scrape, including starting Chrome.", slowBuckets),
		ChromeStart:    r.NewHistogram("nearby_chrome_start_duration_seconds", "Time to start Chrome and open the first tab.", startBuckets),
		CollectLinks:   r.NewHistogram("nearby_collect_links_duration_seconds", "Time to load the Maps results list and scroll it.", slowBuckets),
		ScrapePlaces:   r.NewHistogram("nearby_scrape_places_duration_seconds", "Time to read all place pages of a search.", slowBuckets),
		PlacesFound:    r.NewCounter("nearby_places_found_total", "Places found in results lists."),
		PlacesScraped:  r.NewCounter("nearby_places_scraped_total", "Place pages read successfully."),
		PlacesFailed:   r.NewCounter("nearby_places_failed_total", "Place pages that failed and were skipped."),
	}
}

// ObserveSearch is meant for scraper.Options.OnSearch.
func (m *Metrics) ObserveSearch(st scraper.Stats) {
	result := "ok"
	switch {
	case st.Err != nil && st.Scraped > 0:
		result = "partial"
	case st.Err != nil:
		result = "error"
	}
	m.Searches.Inc(result)
	m.SearchDuration.Observe(st.Total.Seconds())

	// Skip phases the search never reached so they don't pull the averages to 0.
	if st.ChromeStart > 0 {
		m.ChromeStart.Observe(st.ChromeStart.Seconds())
	}
	if st.CollectLinks > 0 {
		m.CollectLinks.Observe(st.CollectLinks.Seconds())
	}
	if st.ScrapePlaces > 0 {
		m.ScrapePlaces.Observe(st.ScrapePlaces.Seconds())
	}
	m.PlacesFound.Add(float64(st.Places))
	m.PlacesScraped.Add(float64(st.Scraped))
	m.PlacesFailed.Add(float64(max(st.Places-st.Scraped, 0)))
}
