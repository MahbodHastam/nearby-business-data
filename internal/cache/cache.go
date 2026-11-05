// Package cache keeps recent search results and makes identical in-flight
// searches share one scrape.
package cache

import (
	"context"
	"sync"
	"time"

	"github.com/mahbodhastam/nearby-business-data/internal/business"
)

type Status string

const (
	Hit       Status = "hit"       // served from the cache
	Miss      Status = "miss"      // this request ran the scrape
	Coalesced Status = "coalesced" // joined a scrape another request started
)

type Func func(context.Context) ([]business.Business, error)

type entry struct {
	results []business.Business
	expires time.Time
}

type call struct {
	done    chan struct{}
	cancel  context.CancelFunc
	waiters int
	results []business.Business
	err     error
}

type Cache struct {
	ttl time.Duration
	max int
	now func() time.Time

	mu      sync.Mutex
	entries map[string]entry
	calls   map[string]*call
}

// New returns a cache that keeps up to maxEntries results for ttl. With
// ttl <= 0 nothing is stored but identical concurrent calls are still coalesced.
func New(ttl time.Duration, maxEntries int) *Cache {
	return &Cache{
		ttl:     ttl,
		max:     max(maxEntries, 1),
		now:     time.Now,
		entries: map[string]entry{},
		calls:   map[string]*call{},
	}
}

// Do returns the cached results for key, or runs fn. Concurrent calls with the
// same key share one fn run. That run only stops when every caller has given
// up, so one caller leaving doesn't fail the others. Only complete runs (fn
// returned no error) are stored. The returned slice is shared and must not be
// modified.
func (c *Cache) Do(ctx context.Context, key string, fn Func) ([]business.Business, Status, error) {
	c.mu.Lock()
	if e, ok := c.entries[key]; ok {
		if c.now().Before(e.expires) {
			c.mu.Unlock()
			return e.results, Hit, nil
		}
		delete(c.entries, key)
	}

	status := Coalesced
	cl, ok := c.calls[key]
	if ok {
		cl.waiters++
	} else {
		status = Miss
		runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
		cl = &call{done: make(chan struct{}), cancel: cancel, waiters: 1}
		c.calls[key] = cl
		go c.run(runCtx, key, cl, fn)
	}
	c.mu.Unlock()

	select {
	case <-cl.done:
		return cl.results, status, cl.err
	case <-ctx.Done():
		c.mu.Lock()
		if cl.waiters--; cl.waiters == 0 {
			cl.cancel()
			// New callers must not join a call that is being cancelled.
			if c.calls[key] == cl {
				delete(c.calls, key)
			}
		}
		c.mu.Unlock()
		return nil, status, ctx.Err()
	}
}

func (c *Cache) run(ctx context.Context, key string, cl *call, fn Func) {
	results, err := fn(ctx)

	c.mu.Lock()
	cl.results, cl.err = results, err
	if c.calls[key] == cl {
		delete(c.calls, key)
	}
	if err == nil && c.ttl > 0 {
		c.store(key, results)
	}
	c.mu.Unlock()

	cl.cancel()
	close(cl.done)
}

// store must be called with c.mu held.
func (c *Cache) store(key string, results []business.Business) {
	now := c.now()
	if _, exists := c.entries[key]; !exists && len(c.entries) >= c.max {
		var oldest string
		var oldestExp time.Time
		for k, e := range c.entries {
			if !now.Before(e.expires) {
				delete(c.entries, k)
			} else if oldest == "" || e.expires.Before(oldestExp) {
				oldest, oldestExp = k, e.expires
			}
		}
		if len(c.entries) >= c.max {
			delete(c.entries, oldest)
		}
	}
	c.entries[key] = entry{results: results, expires: now.Add(c.ttl)}
}

// Len is the number of stored results, including expired ones not yet evicted.
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}
