package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mahbodhastam/nearby-business-data/internal/business"
	"github.com/mahbodhastam/nearby-business-data/internal/cache"
	"github.com/mahbodhastam/nearby-business-data/internal/metrics"
	"github.com/mahbodhastam/nearby-business-data/internal/output"
	"github.com/mahbodhastam/nearby-business-data/internal/refine"
	"github.com/mahbodhastam/nearby-business-data/internal/scraper"
)

const (
	defaultLimit = 20
	maxLimit     = 100
	maxRadiusM   = 50_000
	// With filters or a sort, the scrape has to look at more places than the
	// caller asked for. Maps lists about 120 at most.
	candidateLimit = 100
)

var errBusy = errors.New("server is busy")

type SearchFunc func(context.Context, scraper.Query) ([]business.Business, error)

type Near struct {
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	RadiusM float64 `json:"radius_m,omitempty"`
}

type Response struct {
	Query   string              `json:"query"`
	City    string              `json:"city,omitempty"`
	Near    *Near               `json:"near,omitempty"`
	Count   int                 `json:"count"`
	Results []business.Business `json:"results"`
}

type Config struct {
	Search SearchFunc
	// MaxJobs is how many scrapes run at once; every scrape starts its own
	// Chrome, so more get a 503. Cache hits don't count.
	MaxJobs int
	// Cache stores results and merges identical concurrent searches.
	// Nil means no storing, but merging still happens.
	Cache *cache.Cache
	// Metrics is served on /metrics. Nil makes the server keep its own.
	Metrics *metrics.Metrics
}

// New returns the API handler.
func New(cfg Config) http.Handler {
	if cfg.Cache == nil {
		cfg.Cache = cache.New(0, 1)
	}
	if cfg.Metrics == nil {
		cfg.Metrics = metrics.New()
	}
	m := cfg.Metrics
	sem := make(chan struct{}, max(cfg.MaxJobs, 1))
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok\n"))
	})
	mux.Handle("GET /metrics", m.Registry.Handler())

	mux.HandleFunc("GET /search", func(w http.ResponseWriter, r *http.Request) {
		req, err := parseSearch(r.URL.Query())
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		results, status, err := cfg.Cache.Do(r.Context(), req.cacheKey(), func(ctx context.Context) ([]business.Business, error) {
			select {
			case sem <- struct{}{}:
			default:
				m.BusyRejected.Inc()
				return nil, errBusy
			}
			defer func() { <-sem }()
			m.InFlight.Add(1)
			defer m.InFlight.Add(-1)
			return cfg.Search(ctx, req.scrape)
		})
		m.CacheRequests.Inc(string(status))
		w.Header().Set("X-Cache", string(status))

		if err != nil && len(results) == 0 {
			switch {
			case errors.Is(err, errBusy):
				writeError(w, http.StatusServiceUnavailable, "server is busy, try again later")
			case r.Context().Err() != nil:
				w.WriteHeader(499) // client went away
			default:
				slog.Error("search failed", "query", req.scrape.Subject, "city", req.scrape.City, "err", err)
				writeError(w, http.StatusBadGateway, "search failed")
			}
			return
		}

		results = refine.Apply(results, req.refine)
		if req.format == "csv" {
			w.Header().Set("Content-Type", "text/csv; charset=utf-8")
			output.WriteCSV(w, results)
			return
		}
		resp := Response{Query: req.scrape.Subject, City: req.city, Count: len(results), Results: results}
		if p := req.refine.Near; p != nil {
			resp.Near = &Near{Lat: p.Lat, Lng: p.Lng, RadiusM: req.refine.RadiusM}
		}
		writeJSON(w, http.StatusOK, resp)
	})

	return instrument(mux, m)
}

type searchRequest struct {
	scrape scraper.Query
	refine refine.Options
	city   string // as given, even when a point takes over
	format string
}

// cacheKey is the part of the request that decides what gets scraped. Filters
// and sorting are not in it: they run on the scraped results, so requests that
// differ only in those share one cache entry.
func (r searchRequest) cacheKey() string {
	q := r.scrape
	key := fmt.Sprintf("%s|%s|%d|%.0f", strings.ToLower(q.Subject), strings.ToLower(q.City), q.Limit, q.RadiusM)
	if q.Near != nil {
		// 4 decimals is about 11 m.
		key += fmt.Sprintf("|%.4f,%.4f", q.Near.Lat, q.Near.Lng)
	}
	return key
}

func parseSearch(p url.Values) (searchRequest, error) {
	var req searchRequest
	subject := strings.TrimSpace(p.Get("q"))
	req.city = strings.TrimSpace(p.Get("city"))

	var err error
	if p.Has("lat") != p.Has("lng") {
		return req, errors.New("lat and lng must be given together")
	}
	if p.Has("lat") {
		lat, err1 := strconv.ParseFloat(p.Get("lat"), 64)
		lng, err2 := strconv.ParseFloat(p.Get("lng"), 64)
		if err1 != nil || err2 != nil {
			return req, errors.New("lat and lng must be numbers")
		}
		if req.refine.Near, err = business.NewPoint(lat, lng); err != nil {
			return req, err
		}
	}
	if subject == "" || (req.city == "" && req.refine.Near == nil) {
		return req, errors.New("q and either city or lat and lng are required")
	}

	limit := defaultLimit
	if s := p.Get("limit"); s != "" {
		limit, err = strconv.Atoi(s)
		if err != nil || limit < 1 || limit > maxLimit {
			return req, errors.New("limit must be between 1 and " + strconv.Itoa(maxLimit))
		}
	}

	o := &req.refine
	o.Limit = limit
	o.Category = p.Get("category")
	o.Sort = strings.ToLower(p.Get("sort"))
	if s := p.Get("radius"); s != "" {
		o.RadiusM, err = strconv.ParseFloat(s, 64)
		if err != nil || o.RadiusM <= 0 || o.RadiusM > maxRadiusM {
			return req, fmt.Errorf("radius must be a number of meters between 1 and %d", maxRadiusM)
		}
	}
	if s := p.Get("min_rating"); s != "" {
		if o.MinRating, err = strconv.ParseFloat(s, 64); err != nil {
			return req, errors.New("min_rating must be a number")
		}
	}
	if s := p.Get("min_reviews"); s != "" {
		if o.MinReviews, err = strconv.Atoi(s); err != nil {
			return req, errors.New("min_reviews must be a whole number")
		}
	}
	if s := p.Get("open_now"); s != "" {
		if o.OpenNow, err = strconv.ParseBool(s); err != nil {
			return req, errors.New("open_now must be true or false")
		}
	}
	if err := o.Validate(); err != nil {
		return req, err
	}
	if req.format, err = output.Format(p.Get("format"), ""); err != nil {
		return req, err
	}

	req.scrape = scraper.Query{Subject: subject, City: req.city, Limit: limit, Near: o.Near, RadiusM: o.RadiusM}
	if o.Near != nil {
		req.scrape.City = ""
	}
	if o.Active() {
		req.scrape.Limit = candidateLimit
	}
	return req, nil
}

// instrument counts requests and times them, per route pattern so that
// unknown URLs can't create unbounded label values.
func instrument(next http.Handler, m *metrics.Metrics) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(sw, r)

		route := "unmatched"
		if r.Pattern != "" { // set by the mux while serving
			route = r.Pattern
		}
		m.HTTPRequests.Inc(route, strconv.Itoa(sw.code))
		m.HTTPDuration.Observe(time.Since(start).Seconds(), route)
	})
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(code int) {
	w.code = code
	w.ResponseWriter.WriteHeader(code)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}
