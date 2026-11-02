package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mahbodhastam/nearby-business-data/internal/output"
	"github.com/mahbodhastam/nearby-business-data/internal/proxy"
	"github.com/mahbodhastam/nearby-business-data/internal/scraper"
	"github.com/mahbodhastam/nearby-business-data/internal/server"
)

const usage = `usage:
  nearby scrape -q <subject> -city <city> [flags]
  nearby serve [flags]

run "nearby <command> -h" to see the flags
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch os.Args[1] {
	case "scrape":
		err = scrape(ctx, os.Args[2:])
	case "serve":
		err = serve(ctx, os.Args[2:])
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}

type scraperFlags struct {
	concurrency int
	proxies     string
	headful     bool
	timeout     time.Duration
}

func (f *scraperFlags) register(fs *flag.FlagSet) {
	fs.IntVar(&f.concurrency, "concurrency", 4, "number of place pages to scrape in parallel")
	fs.StringVar(&f.proxies, "proxies", "", "comma separated proxy URLs, or @file")
	fs.BoolVar(&f.headful, "headful", false, "show the browser window")
	fs.DurationVar(&f.timeout, "timeout", 5*time.Minute, "timeout per search")
}

func (f *scraperFlags) scraper() (*scraper.Scraper, error) {
	pool, err := proxy.Load(f.proxies)
	if err != nil {
		return nil, err
	}
	return scraper.New(scraper.Options{
		Concurrency: f.concurrency,
		Headless:    !f.headful,
		Timeout:     f.timeout,
		Proxies:     pool,
	}), nil
}

func scrape(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("scrape", flag.ExitOnError)
	subject := fs.String("q", "", "what to search for, e.g. coffee")
	city := fs.String("city", "", "city to search in, e.g. Berlin")
	limit := fs.Int("limit", 20, "max number of results, 0 for all")
	out := fs.String("o", "", "output file (.json or .csv), stdout if empty")
	format := fs.String("format", "", "json or csv (default: from -o extension)")
	var sf scraperFlags
	sf.register(fs)
	fs.Parse(args)

	if *subject == "" || *city == "" {
		return errors.New("-q and -city are required")
	}
	f, err := output.Format(*format, *out)
	if err != nil {
		return err
	}
	s, err := sf.scraper()
	if err != nil {
		return err
	}

	start := time.Now()
	results, err := s.Search(ctx, scraper.Query{Subject: *subject, City: *city, Limit: *limit})
	if err != nil {
		if len(results) == 0 {
			return err
		}
		slog.Warn("search stopped early, saving partial results", "err", err)
	}

	var w io.Writer = os.Stdout
	if *out != "" {
		file, err := os.Create(*out)
		if err != nil {
			return err
		}
		defer file.Close()
		w = file
	}
	if err := output.Write(w, f, results); err != nil {
		return err
	}
	slog.Info("done", "results", len(results), "took", time.Since(start).Round(time.Second), "output", cmp.Or(*out, "stdout"))
	return nil
}

func serve(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "address to listen on")
	maxJobs := fs.Int("max-jobs", 2, "max searches running at once")
	var sf scraperFlags
	sf.register(fs)
	fs.Parse(args)

	s, err := sf.scraper()
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              *addr,
		Handler:           server.New(s.Search, *maxJobs),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("listening", "addr", *addr)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
