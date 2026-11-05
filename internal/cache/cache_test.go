package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mahbodhastam/nearby-business-data/internal/business"
)

func one(name string) []business.Business { return []business.Business{{Name: name}} }

func TestHitAndExpiry(t *testing.T) {
	c := New(time.Minute, 10)
	now := time.Now()
	c.now = func() time.Time { return now }

	var runs int
	fn := func(context.Context) ([]business.Business, error) { runs++; return one("a"), nil }

	for i, want := range []Status{Miss, Hit, Hit} {
		_, st, err := c.Do(context.Background(), "k", fn)
		if err != nil || st != want {
			t.Fatalf("call %d: status %q, err %v; want %q", i, st, err, want)
		}
	}
	now = now.Add(2 * time.Minute)
	if _, st, _ := c.Do(context.Background(), "k", fn); st != Miss || runs != 2 {
		t.Errorf("after expiry: status %q, runs %d", st, runs)
	}
}

func TestErrorsNotCached(t *testing.T) {
	c := New(time.Minute, 10)
	boom := errors.New("boom")
	if _, _, err := c.Do(context.Background(), "k", func(context.Context) ([]business.Business, error) {
		return one("partial"), boom
	}); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if c.Len() != 0 {
		t.Error("failed run was cached")
	}
}

func TestNoTTLStillCoalesces(t *testing.T) {
	c := New(0, 10)
	release := make(chan struct{})
	var runs atomic.Int32
	fn := func(context.Context) ([]business.Business, error) {
		runs.Add(1)
		<-release
		return one("a"), nil
	}

	var wg sync.WaitGroup
	statuses := make(chan Status, 5)
	for range 5 {
		wg.Go(func() {
			_, st, _ := c.Do(context.Background(), "k", fn)
			statuses <- st
		})
	}
	// Wait until every caller has joined before letting the scrape finish.
	for {
		c.mu.Lock()
		cl := c.calls["k"]
		n := 0
		if cl != nil {
			n = cl.waiters
		}
		c.mu.Unlock()
		if n == 5 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	wg.Wait()
	close(statuses)

	counts := map[Status]int{}
	for st := range statuses {
		counts[st]++
	}
	if runs.Load() != 1 || counts[Miss] != 1 || counts[Coalesced] != 4 {
		t.Errorf("runs %d, statuses %v", runs.Load(), counts)
	}
	if c.Len() != 0 {
		t.Error("ttl 0 must not store")
	}
}

func TestLeaderLeavingKeepsRunForOthers(t *testing.T) {
	c := New(time.Minute, 10)
	started, release := make(chan struct{}), make(chan struct{})
	fn := func(ctx context.Context) ([]business.Business, error) {
		close(started)
		select {
		case <-release:
			return one("a"), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	leaderCtx, leave := context.WithCancel(context.Background())
	leaderDone := make(chan error, 1)
	go func() { _, _, err := c.Do(leaderCtx, "k", fn); leaderDone <- err }()
	<-started

	followerDone := make(chan error, 1)
	go func() { _, _, err := c.Do(context.Background(), "k", fn); followerDone <- err }()
	for {
		c.mu.Lock()
		n := c.calls["k"].waiters
		c.mu.Unlock()
		if n == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}

	leave()
	if err := <-leaderDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("leader err = %v", err)
	}
	close(release)
	if err := <-followerDone; err != nil {
		t.Fatalf("follower err = %v; scrape should have survived the leader leaving", err)
	}
}

func TestAllWaitersLeavingCancelsRun(t *testing.T) {
	c := New(time.Minute, 10)
	started, stopped := make(chan struct{}), make(chan struct{})
	fn := func(ctx context.Context) ([]business.Business, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return nil, ctx.Err()
	}
	ctx, leave := context.WithCancel(context.Background())
	go c.Do(ctx, "k", fn)
	<-started
	leave()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("run not cancelled after all callers left")
	}
}

func TestEvictsOldest(t *testing.T) {
	c := New(time.Minute, 2)
	now := time.Now()
	c.now = func() time.Time { return now }
	fn := func(context.Context) ([]business.Business, error) { return one("a"), nil }
	for _, k := range []string{"a", "b", "c"} {
		c.Do(context.Background(), k, fn)
		now = now.Add(time.Second)
	}
	if c.Len() != 2 {
		t.Fatalf("len = %d", c.Len())
	}
	if _, st, _ := c.Do(context.Background(), "a", fn); st != Miss {
		t.Errorf("oldest entry should have been evicted, got %q", st)
	}
}
