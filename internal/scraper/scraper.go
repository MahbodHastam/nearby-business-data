package scraper

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/chromedp"

	"github.com/mahbodhastam/nearby-business-data/internal/business"
	"github.com/mahbodhastam/nearby-business-data/internal/proxy"
)

const (
	userAgent   = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"
	pageTimeout = 30 * time.Second
)

type Options struct {
	Concurrency int
	Headless    bool
	Timeout     time.Duration
	Proxies     *proxy.Pool
	// OnSearch, if set, is called when a search ends, whether it worked or not.
	OnSearch func(Stats)
}

// Stats describes one finished search. A phase the search never reached is 0.
type Stats struct {
	ChromeStart  time.Duration
	CollectLinks time.Duration
	ScrapePlaces time.Duration
	Total        time.Duration
	Places       int // places found in the results list
	Scraped      int // places read successfully
	Err          error
}

type Query struct {
	Subject string
	City    string // used when Near is nil
	Limit   int
	// Near centers the search on a point instead of a city. RadiusM sets how
	// far the map view reaches (0 = about 1.5 km). It only sizes the view;
	// it does not drop results.
	Near    *business.Point
	RadiusM float64
}

type Scraper struct {
	opts Options
}

func New(opts Options) *Scraper {
	if opts.Concurrency < 1 {
		opts.Concurrency = 4
	}
	return &Scraper{opts: opts}
}

func (s *Scraper) Search(ctx context.Context, q Query) (results []business.Business, err error) {
	start := time.Now()
	var st Stats
	defer func() {
		st.Total = time.Since(start)
		st.Scraped, st.Err = len(results), err
		slog.Info("search timing", "query", q.Subject, "city", q.City,
			"places", st.Places, "scraped", st.Scraped, "failed", st.Places-st.Scraped,
			"chrome_start", st.ChromeStart.Round(time.Millisecond),
			"collect_links", st.CollectLinks.Round(time.Millisecond),
			"scrape_places", st.ScrapePlaces.Round(time.Millisecond),
			"total", st.Total.Round(time.Millisecond))
		if s.opts.OnSearch != nil {
			s.opts.OnSearch(st)
		}
	}()

	if s.opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.opts.Timeout)
		defer cancel()
	}

	px := s.opts.Proxies.Next()
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", s.opts.Headless),
		chromedp.Flag("lang", "en-US"),
		chromedp.Flag("blink-settings", "imagesEnabled=false"),
		chromedp.UserAgent(userAgent),
		chromedp.WindowSize(1280, 900),
	)
	if px != nil {
		opts = append(opts, chromedp.ProxyServer(px.Scheme+"://"+px.Host))
	}
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	defer cancelAlloc()

	browser, cancel, err := newTab(allocCtx, px)
	if err != nil {
		return nil, fmt.Errorf("start chrome: %w", err)
	}
	defer cancel()
	st.ChromeStart = time.Since(start)

	t := time.Now()
	links, err := collectLinks(browser, q)
	st.CollectLinks = time.Since(t)
	if err != nil {
		return nil, err
	}
	st.Places = len(links)
	slog.Info("found places", "query", q.Subject, "city", q.City, "count", len(links))

	t = time.Now()
	results = s.scrapePlaces(browser, px, links)
	st.ScrapePlaces = time.Since(t)
	return results, ctx.Err()
}

// searchURL builds the Maps search page. With a point it searches the map
// view around it, otherwise "subject in city".
func searchURL(q Query) string {
	const base = "https://www.google.com/maps/search/"
	if q.Near == nil {
		return base + url.PathEscape(q.Subject+" in "+q.City) + "?hl=en"
	}
	return fmt.Sprintf("%s%s/@%.6f,%.6f,%dz?hl=en", base, url.PathEscape(q.Subject), q.Near.Lat, q.Near.Lng, zoomFor(q.Near.Lat, q.RadiusM))
}

// zoomFor picks the Maps zoom level at which about radiusM meters fit in half
// of the map view (~400px). Web Mercator is 156543 m/px at zoom 0 on the equator.
func zoomFor(lat, radiusM float64) int {
	if radiusM <= 0 {
		return 14
	}
	z := math.Log2(156543.03392 * math.Cos(lat*math.Pi/180) * 400 / radiusM)
	return min(max(int(math.Round(z)), 3), 18)
}

func collectLinks(tab context.Context, q Query) ([]string, error) {
	if err := chromedp.Do(tab, chromedp.Navigate(searchURL(q))); err != nil {
		return nil, fmt.Errorf("open search: %w", err)
	}

	state, err := waitForResults(tab)
	if err != nil {
		return nil, err
	}
	if state == "place" {
		// Maps skipped the list and opened the only match.
		u, err := chromedp.Run(tab, chromedp.Location())
		return []string{stripQuery(u)}, err
	}

	last, stale := 0, 0
	for stale < 6 {
		st, err := chromedp.Run(tab, chromedp.Evaluate[scrollState](scrollJS))
		if err != nil {
			return nil, fmt.Errorf("scroll results: %w", err)
		}
		if st.End || (q.Limit > 0 && st.Count >= q.Limit) {
			break
		}
		if st.Count == last {
			stale++
		} else {
			last, stale = st.Count, 0
		}
		if err := sleep(tab, time.Second); err != nil {
			return nil, err
		}
	}

	links, err := chromedp.Run(tab, chromedp.Evaluate[[]string](linksJS))
	if err != nil {
		return nil, fmt.Errorf("read results: %w", err)
	}
	for i := range links {
		links[i] = stripQuery(links[i])
	}
	links = dedupe(links)
	if q.Limit > 0 && len(links) > q.Limit {
		links = links[:q.Limit]
	}
	return links, nil
}

func waitForResults(tab context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(tab, pageTimeout)
	defer cancel()
	for {
		// Evaluate fails while the page is navigating, so just retry.
		state, err := chromedp.Run(ctx, chromedp.Evaluate[string](stateJS))
		if err == nil {
			switch state {
			case "feed", "place":
				return state, nil
			case "consent":
				chromedp.Run(ctx, chromedp.Evaluate[bool](consentJS))
			}
		}
		if err := sleep(ctx, 500*time.Millisecond); err != nil {
			return "", fmt.Errorf("no results page (blocked or nothing found): %w", err)
		}
	}
}

func (s *Scraper) scrapePlaces(browser context.Context, px *url.URL, links []string) []business.Business {
	found := make([]*business.Business, len(links))
	// Buffered and pre-filled so a worker that can't get a tab never blocks a producer.
	jobs := make(chan int, len(links))
	for i := range links {
		jobs <- i
	}
	close(jobs)

	// Each worker owns one tab and reuses it for every place it scrapes,
	// instead of paying tab + proxy-auth setup per place.
	var wg sync.WaitGroup
	for range min(s.opts.Concurrency, len(links)) {
		wg.Go(func() {
			var tab context.Context
			var cancel context.CancelFunc = func() {}
			defer func() { cancel() }()
			for i := range jobs {
				if browser.Err() != nil {
					return
				}
				if tab == nil || tab.Err() != nil {
					cancel()
					var err error
					if tab, cancel, err = newTab(browser, px); err != nil {
						slog.Warn("open tab", "err", err)
						tab, cancel = nil, func() {}
						continue
					}
				}
				b, err := scrapePlace(tab, links[i])
				if err != nil {
					slog.Warn("skipping place", "url", links[i], "err", err)
					continue
				}
				found[i] = &b
			}
		})
	}
	wg.Wait()

	var out []business.Business
	for _, b := range found {
		if b != nil {
			out = append(out, *b)
		}
	}
	return out
}

func scrapePlace(tab context.Context, link string) (business.Business, error) {
	ctx, cancelTimeout := context.WithTimeout(tab, pageTimeout)
	defer cancelTimeout()
	err := chromedp.Do(ctx, chromedp.Navigate(link+"?hl=en"), chromedp.WaitVisible(chromedp.CSS("h1")))
	if err != nil {
		return business.Business{}, err
	}
	raw, err := chromedp.Run(ctx, chromedp.Evaluate[rawPlace](placeJS, chromedp.EvalAwaitPromise))
	if err != nil {
		return business.Business{}, err
	}
	if raw.Name == "" {
		return business.Business{}, errors.New("empty place page")
	}
	return toBusiness(raw, link), nil
}

func newTab(parent context.Context, px *url.URL) (context.Context, context.CancelFunc, error) {
	tab, cancel := chromedp.NewContext(parent)
	err := chromedp.Do(tab)
	if err == nil {
		err = proxyAuth(tab, px)
	}
	if err != nil {
		cancel()
		return nil, nil, err
	}
	return tab, cancel, nil
}

// proxyAuth answers proxy auth challenges with the credentials in px.
func proxyAuth(tab context.Context, px *url.URL) error {
	if px == nil || px.User == nil {
		return nil
	}
	user := px.User.Username()
	pass, _ := px.User.Password()

	// With Fetch enabled every request is paused, so each one has to be resumed.
	paused := chromedp.Events(tab, fetch.RequestPaused)
	challenges := chromedp.Events(tab, fetch.AuthRequired)
	go func() {
		for ev, err := range paused {
			if err != nil {
				return
			}
			chromedp.Call(tab, fetch.ContinueRequest, fetch.ContinueRequestParams{RequestID: ev.RequestID})
		}
	}()
	go func() {
		for ev, err := range challenges {
			if err != nil {
				return
			}
			chromedp.Call(tab, fetch.ContinueWithAuth, fetch.ContinueWithAuthParams{
				RequestID: ev.RequestID,
				AuthChallengeResponse: &fetch.AuthChallengeResponse{
					Response: fetch.AuthChallengeResponseResponseProvideCredentials,
					Username: user,
					Password: pass,
				},
			})
		}
	}()

	handleAuth := true
	_, err := chromedp.Call(tab, fetch.Enable, fetch.EnableParams{HandleAuthRequests: &handleAuth})
	return err
}

func sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func stripQuery(u string) string {
	u, _, _ = strings.Cut(u, "?")
	return u
}

func dedupe(ss []string) []string {
	seen := make(map[string]bool, len(ss))
	out := ss[:0]
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
