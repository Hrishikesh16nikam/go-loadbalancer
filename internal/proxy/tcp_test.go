package proxy

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/backend"
	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/balancer"
	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/pool"
)

// startEcho runs a TCP server that writes back whatever it reads.
func startEcho(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}()
		}
	}()
	return ln.Addr().String()
}

func startTCPProxy(t *testing.T, p *pool.Pool) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tp := &TCPProxy{Pool: p, DialTimeout: time.Second}
	go func() { _ = tp.Serve(ln) }()
	t.Cleanup(func() { ln.Close() })
	return ln.Addr().String()
}

func roundTrip(t *testing.T, addr, msg string) string {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte(msg)); err != nil {
		t.Fatal(err)
	}
	if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(conn)
	if err != nil {
		t.Fatal(err)
	}
	return string(got)
}

func TestTCPProxyEcho(t *testing.T) {
	b, err := backend.New("echo", startEcho(t))
	if err != nil {
		t.Fatal(err)
	}
	addr := startTCPProxy(t, pool.New([]*backend.Backend{b}, &balancer.RoundRobin{}, 3, nil))

	const msg = "hello through the proxy"
	if got := roundTrip(t, addr, msg); got != msg {
		t.Fatalf("got %q, want %q", got, msg)
	}
}

func TestTCPProxySkipsDeadBackend(t *testing.T) {
	dead, err := backend.New("dead", deadAddr(t))
	if err != nil {
		t.Fatal(err)
	}
	live, err := backend.New("live", startEcho(t))
	if err != nil {
		t.Fatal(err)
	}
	addr := startTCPProxy(t, pool.New([]*backend.Backend{dead, live}, &balancer.RoundRobin{}, 1, nil))

	// The first pick is "dead"; the proxy should retry on "live" within the
	// same connection, so the client never notices.
	if got := roundTrip(t, addr, "ping"); got != "ping" {
		t.Fatalf("got %q, want ping", got)
	}
	if dead.Alive() {
		t.Fatal("dead backend should have been marked down")
	}
}
