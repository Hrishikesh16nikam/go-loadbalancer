package pool

import (
	"errors"
	"testing"

	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/backend"
	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/balancer"
)

func mustBackend(t *testing.T, name string) *backend.Backend {
	t.Helper()
	b, err := backend.New(name, "127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPassiveFailuresMarkBackendDown(t *testing.T) {
	b := mustBackend(t, "a")
	p := New([]*backend.Backend{b}, &balancer.RoundRobin{}, 2, nil)

	p.ReportFailure(b, errors.New("boom"))
	if !b.Alive() {
		t.Fatal("backend went down after 1 failure; threshold is 2")
	}
	p.ReportFailure(b, errors.New("boom"))
	if b.Alive() {
		t.Fatal("backend should be down after 2 consecutive failures")
	}
	if _, err := p.Next(); !errors.Is(err, ErrNoHealthyBackend) {
		t.Fatalf("Next() error = %v, want ErrNoHealthyBackend", err)
	}
}

func TestSuccessResetsFailureStreak(t *testing.T) {
	b := mustBackend(t, "a")
	p := New([]*backend.Backend{b}, &balancer.RoundRobin{}, 2, nil)

	p.ReportFailure(b, errors.New("boom"))
	p.ReportSuccess(b)
	p.ReportFailure(b, errors.New("boom"))
	if !b.Alive() {
		t.Fatal("failures were not consecutive, backend should still be up")
	}
}

func TestPassiveChecksCanBeDisabled(t *testing.T) {
	b := mustBackend(t, "a")
	p := New([]*backend.Backend{b}, &balancer.RoundRobin{}, -1, nil)
	for i := 0; i < 10; i++ {
		p.ReportFailure(b, errors.New("boom"))
	}
	if !b.Alive() {
		t.Fatal("passive checks are disabled, backend should stay up")
	}
}

func TestHealthyFiltersDownBackends(t *testing.T) {
	a, b := mustBackend(t, "a"), mustBackend(t, "b")
	p := New([]*backend.Backend{a, b}, &balancer.RoundRobin{}, 0, nil)
	p.SetHealth(a, false, "test")
	h := p.Healthy()
	if len(h) != 1 || h[0] != b {
		t.Fatalf("Healthy() = %v, want only b", h)
	}
	for i := 0; i < 5; i++ {
		got, err := p.Next()
		if err != nil || got != b {
			t.Fatalf("Next() = %v, %v; want b", got, err)
		}
	}
}
