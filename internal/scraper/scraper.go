package scraper

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
}

type Query struct {
	Subject string
	City    string
	Limit   int
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

func (s *Scraper) Search(ctx context.Context, q Query) ([]business.Business, error) {
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

	links, err := collectLinks(browser, q)
	if err != nil {
		return nil, err
	}
	slog.Info("found places", "query", q.Subject, "city", q.City, "count", len(links))

	results := s.scrapePlaces(browser, px, links)
	return results, ctx.Err()
}

func collectLinks(tab context.Context, q Query) ([]string, error) {
	u := "https://www.google.com/maps/search/" + url.PathEscape(q.Subject+" in "+q.City) + "?hl=en"
	if err := chromedp.Do(tab, chromedp.Navigate(u)); err != nil {
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
	sem := make(chan struct{}, s.opts.Concurrency)
	var wg sync.WaitGroup
	for i, link := range links {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			if browser.Err() != nil {
				return
			}
			b, err := scrapePlace(browser, px, link)
			if err != nil {
				slog.Warn("skipping place", "url", link, "err", err)
				return
			}
			found[i] = &b
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

func scrapePlace(browser context.Context, px *url.URL, link string) (business.Business, error) {
	tab, cancel, err := newTab(browser, px)
	if err != nil {
		return business.Business{}, err
	}
	defer cancel()

	ctx, cancelTimeout := context.WithTimeout(tab, pageTimeout)
	defer cancelTimeout()
	err = chromedp.Do(ctx, chromedp.Navigate(link+"?hl=en"), chromedp.WaitVisible(chromedp.CSS("h1")))
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
