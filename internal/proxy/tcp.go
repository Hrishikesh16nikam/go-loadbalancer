// Package proxy contains the L4 (TCP) and L7 (HTTP) proxies.
package proxy

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/backend"
	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/metrics"
	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/pool"
)

// TCPProxy forwards raw TCP connections. It never looks at the bytes, so it
// works for any protocol (HTTP, Postgres, Redis, ...), but it can only make
// one routing decision per connection.
type TCPProxy struct {
	Pool        *pool.Pool
	Metrics     *metrics.Metrics // optional
	DialTimeout time.Duration
	Logger      *slog.Logger // optional

	mu    sync.Mutex
	conns map[net.Conn]struct{}
	wg    sync.WaitGroup
}

// Serve accepts connections until ln is closed, then returns nil.
func (p *TCPProxy) Serve(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return err
		}
		p.track(conn, true)
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			defer p.track(conn, false)
			p.handle(conn)
		}()
	}
}

// Drain waits for open connections to finish. If ctx expires first, every
// remaining connection is closed. Call it after Serve has returned.
func (p *TCPProxy) Drain(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		p.mu.Lock()
		for c := range p.conns {
			_ = c.Close()
		}
		p.mu.Unlock()
		<-done
		return ctx.Err()
	}
}

func (p *TCPProxy) handle(client net.Conn) {
	defer client.Close()
	start := time.Now()

	// Try each backend at most once: if a dial fails we report it (which
	// feeds passive health checks) and move on to the next pick.
	var (
		b        *backend.Backend
		upstream net.Conn
	)
	for attempt := 0; attempt < len(p.Pool.Backends()); attempt++ {
		cand, err := p.Pool.Next()
		if err != nil {
			break
		}
		conn, err := net.DialTimeout("tcp", cand.Addr, p.dialTimeout())
		if err != nil {
			p.Pool.ReportFailure(cand, err)
			p.Metrics.Observe(cand.Name, "error", time.Since(start))
			continue
		}
		p.Pool.ReportSuccess(cand)
		b, upstream = cand, conn
		break
	}
	if upstream == nil {
		p.Metrics.NoBackend()
		p.logger().Warn("dropping connection: no backend reachable", "client", client.RemoteAddr().String())
		return
	}
	p.track(upstream, true)
	defer p.track(upstream, false)
	defer upstream.Close()

	b.Acquire()
	defer b.Release()

	done := make(chan struct{}, 2)
	go pipe(upstream, client, done)
	go pipe(client, upstream, done)
	<-done
	<-done
	p.Metrics.Observe(b.Name, "ok", time.Since(start))
}

// pipe copies src to dst, then half-closes dst so the other side sees EOF
// while data can still flow in the opposite direction.
func pipe(dst, src net.Conn, done chan<- struct{}) {
	_, _ = io.Copy(dst, src)
	if cw, ok := dst.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
	} else {
		_ = dst.Close()
	}
	done <- struct{}{}
}

func (p *TCPProxy) track(c net.Conn, add bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conns == nil {
		p.conns = make(map[net.Conn]struct{})
	}
	if add {
		p.conns[c] = struct{}{}
	} else {
		delete(p.conns, c)
	}
}

func (p *TCPProxy) dialTimeout() time.Duration {
	if p.DialTimeout > 0 {
		return p.DialTimeout
	}
	return 2 * time.Second
}

func (p *TCPProxy) logger() *slog.Logger {
	if p.Logger != nil {
		return p.Logger
	}
	return slog.Default()
}
