package proxy

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
)

type Pool struct {
	proxies []*url.URL
	next    atomic.Uint64
}

// Load parses a comma separated list of proxy URLs, or reads them from a
// file when spec starts with "@" (one per line, # for comments).
func Load(spec string) (*Pool, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, nil
	}

	var lines []string
	if path, ok := strings.CutPrefix(spec, "@"); ok {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		lines = strings.Split(string(data), "\n")
	} else {
		lines = strings.Split(spec, ",")
	}

	p := &Pool{}
	for _, line := range lines {
		line, _, _ = strings.Cut(line, "#")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		u, err := url.Parse(line)
		if err != nil || u.Host == "" {
			return nil, fmt.Errorf("invalid proxy %q", line)
		}
		p.proxies = append(p.proxies, u)
	}
	if len(p.proxies) == 0 {
		return nil, nil
	}
	return p, nil
}

// Next returns proxies round-robin. A nil pool returns nil.
func (p *Pool) Next() *url.URL {
	if p == nil || len(p.proxies) == 0 {
		return nil
	}
	n := p.next.Add(1) - 1
	return p.proxies[n%uint64(len(p.proxies))]
}

func (p *Pool) Len() int {
	if p == nil {
		return 0
	}
	return len(p.proxies)
}
