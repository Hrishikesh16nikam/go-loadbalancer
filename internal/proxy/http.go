package proxy

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"time"

	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/backend"
	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/metrics"
	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/pool"
)

type ctxKey struct{}

// reqState travels with each request through httputil.ReverseProxy so the
// rewrite, response and error hooks know which backend was picked.
type reqState struct {
	backend  *backend.Backend
	failed   bool
	canceled bool
}

func stateFrom(ctx context.Context) *reqState {
	st, _ := ctx.Value(ctxKey{}).(*reqState)
	return st
}

// HTTPProxy is the L7 proxy. Because it parses HTTP it makes a routing
// decision per request (not per connection), reuses keep-alive connections
// to backends, and can add headers such as X-Forwarded-For.
type HTTPProxy struct {
	pool    *pool.Pool
	metrics *metrics.Metrics
	logger  *slog.Logger
	rp      *httputil.ReverseProxy
}

// NewHTTPProxy builds the L7 proxy.
func NewHTTPProxy(p *pool.Pool, m *metrics.Metrics, dialTimeout time.Duration, logger *slog.Logger) *HTTPProxy {
	if logger == nil {
		logger = slog.Default()
	}
	h := &HTTPProxy{pool: p, metrics: m, logger: logger}
	transport := &http.Transport{
		DialContext:         (&net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConns:        1000,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
	}
	h.rp = &httputil.ReverseProxy{
		Transport: transport,
		Rewrite: func(pr *httputil.ProxyRequest) {
			st := stateFrom(pr.In.Context())
			pr.SetURL(st.backend.URL)
			pr.SetXForwarded()       // X-Forwarded-For, -Host, -Proto
			pr.Out.Host = pr.In.Host // keep the client's Host header
		},
		ModifyResponse: func(resp *http.Response) error {
			if st := stateFrom(resp.Request.Context()); st != nil {
				resp.Header.Set("X-Served-By", st.backend.Name)
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			st := stateFrom(r.Context())
			if errors.Is(err, context.Canceled) {
				// The client went away; that says nothing about backend health.
				if st != nil {
					st.canceled = true
				}
				return
			}
			if st != nil {
				st.failed = true
				h.pool.ReportFailure(st.backend, err)
				h.logger.Warn("proxy error", "backend", st.backend.Name, "error", err)
			}
			http.Error(w, "bad gateway", http.StatusBadGateway)
		},
	}
	return h
}

// ServeHTTP implements http.Handler.
func (h *HTTPProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	b, err := h.pool.Next()
	if err != nil {
		h.metrics.NoBackend()
		http.Error(w, "no healthy backend available", http.StatusServiceUnavailable)
		return
	}
	b.Acquire()
	defer b.Release()

	st := &reqState{backend: b}
	h.rp.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, st)))

	result := "ok"
	switch {
	case st.canceled:
		result = "canceled"
	case st.failed:
		result = "error"
	default:
		h.pool.ReportSuccess(b)
	}
	h.metrics.Observe(b.Name, result, time.Since(start))
}
