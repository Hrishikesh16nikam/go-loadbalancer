package proxy

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/backend"
	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/balancer"
	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/metrics"
	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/pool"
)

func startHTTPBackend(t *testing.T, name string) *backend.Backend {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Seen-Forwarded-For", r.Header.Get("X-Forwarded-For"))
		fmt.Fprint(w, name)
	}))
	t.Cleanup(srv.Close)
	b, err := backend.New(name, strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func deadAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

func get(t *testing.T, url string) (int, string, http.Header) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(body), resp.Header
}

func TestHTTPProxyRoundRobin(t *testing.T) {
	one, two := startHTTPBackend(t, "one"), startHTTPBackend(t, "two")
	p := pool.New([]*backend.Backend{one, two}, &balancer.RoundRobin{}, 3, nil)
	lb := httptest.NewServer(NewHTTPProxy(p, metrics.New(p), time.Second, nil))
	defer lb.Close()

	counts := map[string]int{}
	for i := 0; i < 10; i++ {
		code, body, hdr := get(t, lb.URL)
		if code != http.StatusOK {
			t.Fatalf("status %d", code)
		}
		if hdr.Get("X-Served-By") != body {
			t.Errorf("X-Served-By = %q, body = %q", hdr.Get("X-Served-By"), body)
		}
		if hdr.Get("X-Seen-Forwarded-For") == "" {
			t.Error("backend did not receive X-Forwarded-For")
		}
		counts[body]++
	}
	if counts["one"] != 5 || counts["two"] != 5 {
		t.Fatalf("uneven distribution: %v", counts)
	}
}

func TestHTTPProxyNoHealthyBackend(t *testing.T) {
	one := startHTTPBackend(t, "one")
	one.SetAlive(false)
	p := pool.New([]*backend.Backend{one}, &balancer.RoundRobin{}, 3, nil)
	lb := httptest.NewServer(NewHTTPProxy(p, nil, time.Second, nil))
	defer lb.Close()

	if code, _, _ := get(t, lb.URL); code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503", code)
	}
}

func TestHTTPProxyTakesDeadBackendOutOfRotation(t *testing.T) {
	dead, err := backend.New("dead", deadAddr(t))
	if err != nil {
		t.Fatal(err)
	}
	live := startHTTPBackend(t, "live")
	p := pool.New([]*backend.Backend{dead, live}, &balancer.RoundRobin{}, 1, nil)
	lb := httptest.NewServer(NewHTTPProxy(p, nil, time.Second, nil))
	defer lb.Close()

	// Round robin sends the first request to "dead": it fails with 502 and
	// the passive check (threshold 1) takes the backend out of rotation.
	if code, _, _ := get(t, lb.URL); code != http.StatusBadGateway {
		t.Fatalf("first request: status %d, want 502", code)
	}
	if dead.Alive() {
		t.Fatal("dead backend should have been marked down")
	}
	for i := 0; i < 5; i++ {
		code, body, _ := get(t, lb.URL)
		if code != http.StatusOK || body != "live" {
			t.Fatalf("request %d: %d %q, want 200 from live", i, code, body)
		}
	}
}
