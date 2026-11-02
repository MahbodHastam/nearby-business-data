package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mahbodhastam/nearby-business-data/internal/business"
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
	rec := get(New(fakeSearch, 1), "/search?q=coffee&city=Berlin&limit=3")
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
	rec := get(New(fakeSearch, 1), "/search?q=coffee&city=Berlin&limit=2&format=csv")
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("status = %d, type = %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if lines := strings.Count(rec.Body.String(), "\n"); lines != 3 {
		t.Errorf("want header + 2 rows, got %d lines", lines)
	}
}

func TestSearchBadRequest(t *testing.T) {
	h := New(fakeSearch, 1)
	for _, target := range []string{
		"/search?city=Berlin",
		"/search?q=coffee",
		"/search?q=coffee&city=Berlin&limit=0",
		"/search?q=coffee&city=Berlin&limit=101",
		"/search?q=coffee&city=Berlin&limit=abc",
		"/search?q=coffee&city=Berlin&format=xml",
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
	h := New(blocking, 1)

	done := make(chan struct{})
	go func() { get(h, "/search?q=a&city=b"); close(done) }()
	<-started

	if rec := get(h, "/search?q=a&city=b"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("second request status = %d; want 503", rec.Code)
	}
	close(release)
	<-done
}

func TestSearchFailure(t *testing.T) {
	failing := func(context.Context, scraper.Query) ([]business.Business, error) {
		return nil, errors.New("blocked")
	}
	if rec := get(New(failing, 1), "/search?q=a&city=b"); rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d; want 502", rec.Code)
	}
}

func TestHealthz(t *testing.T) {
	if rec := get(New(fakeSearch, 1), "/healthz"); rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
}
