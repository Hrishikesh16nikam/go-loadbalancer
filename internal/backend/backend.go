// Package backend models a single upstream server that the load balancer can route to.
package backend

import (
	"fmt"
	"net/url"
	"sync/atomic"
)

// Backend is one upstream server. All mutable state is atomic so the proxy,
// the balancer and the health checker can read and update it concurrently
// without a lock on the hot path.
type Backend struct {
	Name string
	Addr string   // host:port, used for L4 proxying and TCP health checks
	URL  *url.URL // http://host:port, used for L7 proxying and HTTP health checks

	alive  atomic.Bool
	active atomic.Int64 // in-flight requests (L7) or open connections (L4)
	fails  atomic.Int64 // consecutive passive failures
}

// New creates a backend that starts out healthy.
func New(name, addr string) (*Backend, error) {
	u, err := url.Parse("http://" + addr)
	if err != nil {
		return nil, fmt.Errorf("backend %q: invalid address %q: %w", name, addr, err)
	}
	b := &Backend{Name: name, Addr: addr, URL: u}
	b.alive.Store(true)
	return b, nil
}

// Alive reports whether the backend is currently in rotation.
func (b *Backend) Alive() bool { return b.alive.Load() }

// SetAlive updates health and reports whether the value actually changed.
func (b *Backend) SetAlive(v bool) bool { return b.alive.Swap(v) != v }

// ActiveConns returns the number of in-flight requests or connections.
func (b *Backend) ActiveConns() int64 { return b.active.Load() }

// Acquire marks the start of a request or connection.
func (b *Backend) Acquire() { b.active.Add(1) }

// Release marks the end of a request or connection.
func (b *Backend) Release() { b.active.Add(-1) }

// RecordFailure increments the consecutive failure streak and returns it.
func (b *Backend) RecordFailure() int64 { return b.fails.Add(1) }

// ResetFailures clears the consecutive failure streak.
func (b *Backend) ResetFailures() { b.fails.Store(0) }
