package metrics

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/backend"
	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/balancer"
	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/pool"
)

func TestExposition(t *testing.T) {
	b, err := backend.New("a", "127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	m := New(pool.New([]*backend.Backend{b}, &balancer.RoundRobin{}, 0, nil))
	m.Observe("a", "ok", 3*time.Millisecond)
	m.Observe("a", "ok", 30*time.Millisecond)
	m.Observe("a", "error", time.Millisecond)
	m.NoBackend()

	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	out := rec.Body.String()

	for _, want := range []string{
		`lb_requests_total{backend="a",result="ok"} 2`,
		`lb_requests_total{backend="a",result="error"} 1`,
		`lb_no_healthy_backend_total 1`,
		`lb_request_duration_seconds_bucket{backend="a",le="0.005"} 2`,
		`lb_request_duration_seconds_bucket{backend="a",le="+Inf"} 3`,
		`lb_request_duration_seconds_count{backend="a"} 3`,
		`lb_backend_up{backend="a"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
}

func TestNilMetricsIsSafe(t *testing.T) {
	var m *Metrics
	m.Observe("a", "ok", time.Millisecond)
	m.NoBackend()
}
