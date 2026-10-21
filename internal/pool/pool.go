// Package pool owns the set of backends, decides which ones are healthy, and
// asks the configured balancer to pick among them.
package pool

import (
	"errors"
	"log/slog"

	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/backend"
	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/balancer"
)

// ErrNoHealthyBackend is returned when every backend is out of rotation.
var ErrNoHealthyBackend = errors.New("no healthy backend available")

// Pool combines backends, a balancer and passive health tracking.
type Pool struct {
	backends      []*backend.Backend
	balancer      balancer.Balancer
	failThreshold int64
	logger        *slog.Logger
}

// New builds a pool. passiveFailThreshold is the number of consecutive proxy
// failures after which a backend is taken out of rotation; zero or a
// negative value disables passive checks.
func New(backends []*backend.Backend, b balancer.Balancer, passiveFailThreshold int, logger *slog.Logger) *Pool {
	if logger == nil {
		logger = slog.Default()
	}
	return &Pool{
		backends:      backends,
		balancer:      b,
		failThreshold: int64(passiveFailThreshold),
		logger:        logger,
	}
}

// Backends returns every backend, healthy or not.
func (p *Pool) Backends() []*backend.Backend { return p.backends }

// Balancer returns the routing algorithm in use.
func (p *Pool) Balancer() balancer.Balancer { return p.balancer }

// Healthy returns the backends currently in rotation.
func (p *Pool) Healthy() []*backend.Backend {
	out := make([]*backend.Backend, 0, len(p.backends))
	for _, b := range p.backends {
		if b.Alive() {
			out = append(out, b)
		}
	}
	return out
}

// Next picks a healthy backend.
func (p *Pool) Next() (*backend.Backend, error) {
	b := p.balancer.Next(p.Healthy())
	if b == nil {
		return nil, ErrNoHealthyBackend
	}
	return b, nil
}

// ReportFailure records a failed proxy attempt (passive health check).
func (p *Pool) ReportFailure(b *backend.Backend, err error) {
	n := b.RecordFailure()
	if p.failThreshold > 0 && n >= p.failThreshold && b.SetAlive(false) {
		p.logger.Warn("backend marked down", "backend", b.Name, "check", "passive",
			"consecutive_failures", n, "error", err)
	}
}

// ReportSuccess clears the passive failure streak.
func (p *Pool) ReportSuccess(b *backend.Backend) { b.ResetFailures() }

// SetHealth is called by the active health checker.
func (p *Pool) SetHealth(b *backend.Backend, alive bool, reason string) {
	if !b.SetAlive(alive) {
		return
	}
	if alive {
		b.ResetFailures()
		p.logger.Info("backend marked up", "backend", b.Name, "check", "active", "reason", reason)
		return
	}
	p.logger.Warn("backend marked down", "backend", b.Name, "check", "active", "reason", reason)
}
