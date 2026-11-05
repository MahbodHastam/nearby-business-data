// Package metrics is a small Prometheus text-format exporter: counters,
// gauges and histograms, with labels, and no dependencies.
package metrics

import (
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
)

const sep = "\xff"

type renderer interface {
	render(w io.Writer)
}

type Registry struct {
	mu    sync.Mutex
	items []renderer
}

func (r *Registry) add(m renderer) {
	r.mu.Lock()
	r.items = append(r.items, m)
	r.mu.Unlock()
}

func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		r.mu.Lock()
		items := slices.Clone(r.items)
		r.mu.Unlock()
		for _, m := range items {
			m.render(w)
		}
	})
}

type meta struct {
	name, help, typ string
	labels          []string
}

func (m meta) header(w io.Writer) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", m.name, m.help, m.name, m.typ)
}

// labelString renders {a="x",b="y"}, with extra pairs ("le", "0.5") appended.
func (m meta) labelString(key string, extra ...string) string {
	var values []string
	if len(m.labels) > 0 {
		values = strings.Split(key, sep)
	}
	var parts []string
	for i, name := range m.labels {
		parts = append(parts, fmt.Sprintf(`%s="%s"`, name, escape(values[i])))
	}
	for i := 0; i+1 < len(extra); i += 2 {
		parts = append(parts, fmt.Sprintf(`%s="%s"`, extra[i], extra[i+1]))
	}
	if len(parts) == 0 {
		return ""
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func escape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(s)
}

func (m meta) key(values []string) string {
	if len(values) != len(m.labels) {
		panic(fmt.Sprintf("metric %s: got %d label values, want %d", m.name, len(values), len(m.labels)))
	}
	return strings.Join(values, sep)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

type Counter struct {
	meta
	mu   sync.Mutex
	vals map[string]float64
}

func (r *Registry) NewCounter(name, help string, labels ...string) *Counter {
	c := &Counter{meta: meta{name, help, "counter", labels}, vals: map[string]float64{}}
	r.add(c)
	return c
}

func (c *Counter) Add(v float64, labelValues ...string) {
	k := c.key(labelValues)
	c.mu.Lock()
	c.vals[k] += v
	c.mu.Unlock()
}

func (c *Counter) Inc(labelValues ...string) { c.Add(1, labelValues...) }

func (c *Counter) render(w io.Writer) {
	c.header(w)
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, k := range sortedKeys(c.vals) {
		fmt.Fprintf(w, "%s%s %v\n", c.name, c.labelString(k), c.vals[k])
	}
}

type Gauge struct {
	meta
	mu  sync.Mutex
	val float64
}

func (r *Registry) NewGauge(name, help string) *Gauge {
	g := &Gauge{meta: meta{name: name, help: help, typ: "gauge"}}
	r.add(g)
	return g
}

func (g *Gauge) Add(delta float64) {
	g.mu.Lock()
	g.val += delta
	g.mu.Unlock()
}

func (g *Gauge) render(w io.Writer) {
	g.header(w)
	g.mu.Lock()
	defer g.mu.Unlock()
	fmt.Fprintf(w, "%s %v\n", g.name, g.val)
}

type series struct {
	counts []uint64 // per bucket, not cumulative; the last one is +Inf
	sum    float64
}

type Histogram struct {
	meta
	buckets []float64
	mu      sync.Mutex
	vals    map[string]*series
}

// NewHistogram takes ascending upper bounds; +Inf is added.
func (r *Registry) NewHistogram(name, help string, buckets []float64, labels ...string) *Histogram {
	h := &Histogram{meta: meta{name, help, "histogram", labels}, buckets: buckets, vals: map[string]*series{}}
	r.add(h)
	return h
}

func (h *Histogram) Observe(v float64, labelValues ...string) {
	k := h.key(labelValues)
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.vals[k]
	if s == nil {
		s = &series{counts: make([]uint64, len(h.buckets)+1)}
		h.vals[k] = s
	}
	i, _ := slices.BinarySearch(h.buckets, v) // first bucket with bound >= v
	s.counts[i]++
	s.sum += v
}

func (h *Histogram) render(w io.Writer) {
	h.header(w)
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, k := range sortedKeys(h.vals) {
		s := h.vals[k]
		var cum uint64
		for i, bound := range h.buckets {
			cum += s.counts[i]
			fmt.Fprintf(w, "%s_bucket%s %d\n", h.name, h.labelString(k, "le", fmt.Sprint(bound)), cum)
		}
		cum += s.counts[len(h.buckets)]
		fmt.Fprintf(w, "%s_bucket%s %d\n", h.name, h.labelString(k, "le", "+Inf"), cum)
		fmt.Fprintf(w, "%s_sum%s %v\n", h.name, h.labelString(k), s.sum)
		fmt.Fprintf(w, "%s_count%s %d\n", h.name, h.labelString(k), cum)
	}
}
