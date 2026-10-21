// Package metrics exposes Prometheus-format metrics without pulling in the
// Prometheus client library. Counters and histograms are guarded by one
// mutex; backend gauges are read from the pool at scrape time.
package metrics

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/pool"
)

// Histogram bucket upper bounds, in seconds.
var buckets = []float64{0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}

type histogram struct {
	counts []uint64 // per bucket, not cumulative
	sum    float64
	count  uint64
}

type reqKey struct{ backend, result string }

// Metrics collects request counts and latencies.
type Metrics struct {
	pool *pool.Pool

	mu        sync.Mutex
	requests  map[reqKey]uint64
	durations map[string]*histogram
	noBackend uint64
}

// New creates a collector. p may be nil, in which case no gauges are written.
func New(p *pool.Pool) *Metrics {
	return &Metrics{
		pool:      p,
		requests:  make(map[reqKey]uint64),
		durations: make(map[string]*histogram),
	}
}

// Observe records one request (L7) or connection (L4). It is safe to call
// on a nil *Metrics, which keeps tests short.
func (m *Metrics) Observe(backendName, result string, d time.Duration) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests[reqKey{backendName, result}]++
	h := m.durations[backendName]
	if h == nil {
		h = &histogram{counts: make([]uint64, len(buckets))}
		m.durations[backendName] = h
	}
	s := d.Seconds()
	for i, upper := range buckets {
		if s <= upper {
			h.counts[i]++
			break
		}
	}
	h.sum += s
	h.count++
}

// NoBackend records a request that was rejected because nothing was healthy.
func (m *Metrics) NoBackend() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.noBackend++
	m.mu.Unlock()
}

// ServeHTTP writes all metrics in the Prometheus text format.
func (m *Metrics) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	var buf bytes.Buffer
	m.write(&buf)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
}

func (m *Metrics) write(w io.Writer) {
	m.mu.Lock()
	defer m.mu.Unlock()

	fmt.Fprintln(w, "# HELP lb_requests_total Requests (L7) or connections (L4) handled, by backend and result.")
	fmt.Fprintln(w, "# TYPE lb_requests_total counter")
	keys := make([]reqKey, 0, len(m.requests))
	for k := range m.requests {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].backend != keys[j].backend {
			return keys[i].backend < keys[j].backend
		}
		return keys[i].result < keys[j].result
	})
	for _, k := range keys {
		fmt.Fprintf(w, "lb_requests_total{backend=%q,result=%q} %d\n", k.backend, k.result, m.requests[k])
	}

	fmt.Fprintln(w, "# HELP lb_no_healthy_backend_total Requests rejected because no backend was healthy.")
	fmt.Fprintln(w, "# TYPE lb_no_healthy_backend_total counter")
	fmt.Fprintf(w, "lb_no_healthy_backend_total %d\n", m.noBackend)

	fmt.Fprintln(w, "# HELP lb_request_duration_seconds Request (L7) or connection (L4) duration.")
	fmt.Fprintln(w, "# TYPE lb_request_duration_seconds histogram")
	names := make([]string, 0, len(m.durations))
	for n := range m.durations {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		h := m.durations[n]
		var cumulative uint64
		for i, upper := range buckets {
			cumulative += h.counts[i]
			fmt.Fprintf(w, "lb_request_duration_seconds_bucket{backend=%q,le=%q} %d\n",
				n, strconv.FormatFloat(upper, 'g', -1, 64), cumulative)
		}
		fmt.Fprintf(w, "lb_request_duration_seconds_bucket{backend=%q,le=\"+Inf\"} %d\n", n, h.count)
		fmt.Fprintf(w, "lb_request_duration_seconds_sum{backend=%q} %g\n", n, h.sum)
		fmt.Fprintf(w, "lb_request_duration_seconds_count{backend=%q} %d\n", n, h.count)
	}

	if m.pool == nil {
		return
	}
	fmt.Fprintln(w, "# HELP lb_backend_up Whether the backend is in rotation (1) or not (0).")
	fmt.Fprintln(w, "# TYPE lb_backend_up gauge")
	for _, b := range m.pool.Backends() {
		up := 0
		if b.Alive() {
			up = 1
		}
		fmt.Fprintf(w, "lb_backend_up{backend=%q} %d\n", b.Name, up)
	}
	fmt.Fprintln(w, "# HELP lb_backend_active_connections In-flight requests or open connections per backend.")
	fmt.Fprintln(w, "# TYPE lb_backend_active_connections gauge")
	for _, b := range m.pool.Backends() {
		fmt.Fprintf(w, "lb_backend_active_connections{backend=%q} %d\n", b.Name, b.ActiveConns())
	}
}
