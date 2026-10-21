// Package balancer contains the routing algorithms. Each algorithm only picks
// from the candidates it is given; filtering out unhealthy backends is the
// pool's job, which keeps the algorithms small and easy to test.
package balancer

import (
	"fmt"
	"sync/atomic"

	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/backend"
)

// Names accepted in the config file.
const (
	RoundRobinName       = "round_robin"
	LeastConnectionsName = "least_connections"
)

// Balancer chooses one backend from a list of healthy candidates.
// It returns nil when the list is empty.
type Balancer interface {
	Next(candidates []*backend.Backend) *backend.Backend
	Name() string
}

// New returns the balancer registered under name.
func New(name string) (Balancer, error) {
	switch name {
	case RoundRobinName:
		return &RoundRobin{}, nil
	case LeastConnectionsName:
		return &LeastConnections{}, nil
	default:
		return nil, fmt.Errorf("unknown algorithm %q (want %q or %q)", name, RoundRobinName, LeastConnectionsName)
	}
}

// RoundRobin hands out backends in turn. The counter is atomic, so it is
// safe to call Next from many goroutines at once.
type RoundRobin struct {
	next atomic.Uint64
}

// Name implements Balancer.
func (r *RoundRobin) Name() string { return RoundRobinName }

// Next implements Balancer.
func (r *RoundRobin) Next(candidates []*backend.Backend) *backend.Backend {
	if len(candidates) == 0 {
		return nil
	}
	i := r.next.Add(1) - 1
	return candidates[i%uint64(len(candidates))]
}

// LeastConnections picks the backend with the fewest in-flight requests,
// which adapts automatically when one backend is slower than the others.
type LeastConnections struct{}

// Name implements Balancer.
func (LeastConnections) Name() string { return LeastConnectionsName }

// Next implements Balancer. Ties go to the earliest backend in the list.
func (LeastConnections) Next(candidates []*backend.Backend) *backend.Backend {
	var best *backend.Backend
	var bestConns int64
	for _, b := range candidates {
		c := b.ActiveConns()
		if best == nil || c < bestConns {
			best, bestConns = b, c
		}
	}
	return best
}
