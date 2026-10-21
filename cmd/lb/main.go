// Command lb is a small L4/L7 load balancer.
//
//	go run ./cmd/lb -config configs/l7.json
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/backend"
	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/balancer"
	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/config"
	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/health"
	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/metrics"
	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/pool"
	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/proxy"
)

func main() {
	cfgPath := flag.String("config", "configs/l7.json", "path to the JSON config file")
	algorithm := flag.String("algorithm", "", "override the config's algorithm (round_robin or least_connections)")
	debug := flag.Bool("debug", false, "log every failed health check")
	flag.Parse()

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		logger.Error("invalid config", "path", *cfgPath, "error", err)
		os.Exit(1)
	}
	if *algorithm != "" {
		cfg.Algorithm = *algorithm
	}
	if err := run(cfg, logger); err != nil {
		logger.Error("load balancer stopped", "error", err)
		os.Exit(1)
	}
}

func run(cfg *config.Config, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	bal, err := balancer.New(cfg.Algorithm)
	if err != nil {
		return err
	}
	backends := make([]*backend.Backend, 0, len(cfg.Backends))
	for _, bc := range cfg.Backends {
		b, err := backend.New(bc.Name, bc.Addr)
		if err != nil {
			return err
		}
		backends = append(backends, b)
	}
	p := pool.New(backends, bal, cfg.PassiveFailureThreshold, logger)
	m := metrics.New(p)

	go health.NewChecker(p, cfg.HealthCheck, logger).Run(ctx)

	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", m)
	metricsSrv := &http.Server{Addr: cfg.MetricsListen, Handler: metricsMux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("metrics server failed", "error", err)
		}
	}()
	defer metricsSrv.Close()

	logger.Info("load balancer starting",
		"mode", cfg.Mode, "listen", cfg.Listen, "metrics", cfg.MetricsListen,
		"algorithm", bal.Name(), "backends", len(backends))

	switch cfg.Mode {
	case "l4":
		return runL4(ctx, cfg, p, m, logger)
	case "l7":
		return runL7(ctx, cfg, p, m, logger)
	default:
		return fmt.Errorf("unknown mode %q", cfg.Mode)
	}
}

func runL4(ctx context.Context, cfg *config.Config, p *pool.Pool, m *metrics.Metrics, logger *slog.Logger) error {
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	tp := &proxy.TCPProxy{Pool: p, Metrics: m, DialTimeout: cfg.DialTimeout.Duration, Logger: logger}
	serveErr := make(chan error, 1)
	go func() { serveErr <- tp.Serve(ln) }()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}
	logger.Info("shutting down, draining connections", "timeout", cfg.ShutdownTimeout.Duration)
	_ = ln.Close()
	<-serveErr
	drainCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout.Duration)
	defer cancel()
	if err := tp.Drain(drainCtx); err != nil {
		logger.Warn("closed connections that did not finish in time")
	}
	return nil
}

func runL7(ctx context.Context, cfg *config.Config, p *pool.Pool, m *metrics.Metrics, logger *slog.Logger) error {
	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           proxy.NewHTTPProxy(p, m, cfg.DialTimeout.Duration, logger),
		ReadHeaderTimeout: 10 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}
	logger.Info("shutting down, draining in-flight requests", "timeout", cfg.ShutdownTimeout.Duration)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout.Duration)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
