// Package health runs active health checks against every backend.
//
// A backend changes state only after a streak of results (see the
// thresholds in config), so a single slow response does not flap it in and
// out of rotation.
package health

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/backend"
	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/config"
	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/pool"
)

type streak struct{ ok, fail int }

// Checker probes backends on a fixed interval.
type Checker struct {
	pool    *pool.Pool
	cfg     config.HealthCheckConfig
	client  *http.Client
	logger  *slog.Logger
	streaks map[*backend.Backend]*streak // only touched by the goroutine calling CheckOnce
}

// NewChecker builds a checker for the pool.
func NewChecker(p *pool.Pool, cfg config.HealthCheckConfig, logger *slog.Logger) *Checker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Checker{
		pool:   p,
		cfg:    cfg,
		logger: logger,
		client: &http.Client{
			Timeout:   cfg.Timeout.Duration,
			Transport: &http.Transport{DisableKeepAlives: true},
		},
		streaks: make(map[*backend.Backend]*streak),
	}
}

// Run checks immediately, then on every interval until ctx is cancelled.
func (c *Checker) Run(ctx context.Context) {
	c.CheckOnce(ctx)
	t := time.NewTicker(c.cfg.Interval.Duration)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.CheckOnce(ctx)
		}
	}
}

// CheckOnce probes every backend in parallel and then applies the results.
func (c *Checker) CheckOnce(ctx context.Context) {
	backends := c.pool.Backends()
	results := make([]error, len(backends))
	var wg sync.WaitGroup
	for i, b := range backends {
		wg.Add(1)
		go func(i int, b *backend.Backend) {
			defer wg.Done()
			results[i] = c.probe(ctx, b)
		}(i, b)
	}
	wg.Wait()
	for i, b := range backends {
		c.record(b, results[i])
	}
}

func (c *Checker) probe(ctx context.Context, b *backend.Backend) error {
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout.Duration)
	defer cancel()

	if c.cfg.Type == "http" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.URL.String()+c.cfg.Path, nil)
		if err != nil {
			return err
		}
		resp, err := c.client.Do(req)
		if err != nil {
			return err
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return fmt.Errorf("status %d", resp.StatusCode)
		}
		return nil
	}

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", b.Addr)
	if err != nil {
		return err
	}
	return conn.Close()
}

func (c *Checker) record(b *backend.Backend, err error) {
	s := c.streaks[b]
	if s == nil {
		s = &streak{}
		c.streaks[b] = s
	}
	if err == nil {
		s.ok++
		s.fail = 0
		if !b.Alive() && s.ok >= c.cfg.HealthyThreshold {
			c.pool.SetHealth(b, true, fmt.Sprintf("%d checks passed", s.ok))
		}
		return
	}
	s.fail++
	s.ok = 0
	c.logger.Debug("health check failed", "backend", b.Name, "error", err)
	if b.Alive() && s.fail >= c.cfg.UnhealthyThreshold {
		c.pool.SetHealth(b, false, fmt.Sprintf("%d checks failed, last error: %v", s.fail, err))
	}
}
