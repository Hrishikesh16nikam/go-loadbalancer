package health

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/backend"
	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/balancer"
	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/config"
	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/pool"
)

func hcConfig(typ string, healthy, unhealthy int) config.HealthCheckConfig {
	return config.HealthCheckConfig{
		Type:               typ,
		Path:               "/health",
		Interval:           config.Duration{Duration: time.Second},
		Timeout:            config.Duration{Duration: time.Second},
		HealthyThreshold:   healthy,
		UnhealthyThreshold: unhealthy,
	}
}

func TestHTTPCheckUsesThresholds(t *testing.T) {
	var healthy atomic.Bool
	healthy.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !healthy.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	b, err := backend.New("a", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	p := pool.New([]*backend.Backend{b}, &balancer.RoundRobin{}, 0, nil)
	c := NewChecker(p, hcConfig("http", 2, 2), nil)
	ctx := context.Background()

	healthy.Store(false)
	c.CheckOnce(ctx)
	if !b.Alive() {
		t.Fatal("marked down after 1 failure; unhealthy threshold is 2")
	}
	c.CheckOnce(ctx)
	if b.Alive() {
		t.Fatal("should be down after 2 failed checks")
	}

	healthy.Store(true)
	c.CheckOnce(ctx)
	if b.Alive() {
		t.Fatal("marked up after 1 success; healthy threshold is 2")
	}
	c.CheckOnce(ctx)
	if !b.Alive() {
		t.Fatal("should be back up after 2 passing checks")
	}
}

func TestTCPCheckDetectsClosedPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close() // nothing is listening on addr any more

	b, err := backend.New("a", addr)
	if err != nil {
		t.Fatal(err)
	}
	p := pool.New([]*backend.Backend{b}, &balancer.RoundRobin{}, 0, nil)
	NewChecker(p, hcConfig("tcp", 1, 1), nil).CheckOnce(context.Background())
	if b.Alive() {
		t.Fatal("backend with a closed port should be marked down")
	}
}
