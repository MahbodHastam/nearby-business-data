package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/mahbodhastam/nearby-business-data/internal/business"
	"github.com/mahbodhastam/nearby-business-data/internal/output"
	"github.com/mahbodhastam/nearby-business-data/internal/scraper"
)

const maxLimit = 100

type SearchFunc func(context.Context, scraper.Query) ([]business.Business, error)

type Response struct {
	Query   string              `json:"query"`
	City    string              `json:"city"`
	Count   int                 `json:"count"`
	Results []business.Business `json:"results"`
}

// New returns the API handler. Every search starts its own Chrome, so only
// maxJobs run at once and the rest get a 503.
func New(search SearchFunc, maxJobs int) http.Handler {
	sem := make(chan struct{}, max(maxJobs, 1))
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok\n"))
	})

	mux.HandleFunc("GET /search", func(w http.ResponseWriter, r *http.Request) {
		params := r.URL.Query()
		q := scraper.Query{Subject: params.Get("q"), City: params.Get("city"), Limit: 20}
		if q.Subject == "" || q.City == "" {
			writeError(w, http.StatusBadRequest, "q and city are required")
			return
		}
		if s := params.Get("limit"); s != "" {
			n, err := strconv.Atoi(s)
			if err != nil || n < 1 || n > maxLimit {
				writeError(w, http.StatusBadRequest, "limit must be between 1 and "+strconv.Itoa(maxLimit))
				return
			}
			q.Limit = n
		}
		format, err := output.Format(params.Get("format"), "")
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		select {
		case sem <- struct{}{}:
			defer func() { <-sem }()
		default:
			writeError(w, http.StatusServiceUnavailable, "server is busy, try again later")
			return
		}

		results, err := search(r.Context(), q)
		if err != nil && len(results) == 0 {
			if errors.Is(err, context.Canceled) {
				return
			}
			slog.Error("search failed", "query", q.Subject, "city", q.City, "err", err)
			writeError(w, http.StatusBadGateway, "search failed")
			return
		}

		if format == "csv" {
			w.Header().Set("Content-Type", "text/csv; charset=utf-8")
			output.WriteCSV(w, results)
			return
		}
		if results == nil {
			results = []business.Business{}
		}
		writeJSON(w, http.StatusOK, Response{Query: q.Subject, City: q.City, Count: len(results), Results: results})
	})

	return mux
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
