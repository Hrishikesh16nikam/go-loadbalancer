// Package config loads and validates the load balancer's JSON config file.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// Duration lets durations be written as "2s" or "500ms" in JSON.
type Duration struct{ time.Duration }

// UnmarshalJSON implements json.Unmarshaler.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("duration must be a string such as \"2s\": %w", err)
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	d.Duration = v
	return nil
}

// MarshalJSON implements json.Marshaler.
func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

// BackendConfig describes one upstream server.
type BackendConfig struct {
	Name string `json:"name"`
	Addr string `json:"addr"`
}

// HealthCheckConfig controls active health checks.
type HealthCheckConfig struct {
	Type               string   `json:"type"` // "tcp" or "http"
	Path               string   `json:"path"` // used when Type is "http"
	Interval           Duration `json:"interval"`
	Timeout            Duration `json:"timeout"`
	HealthyThreshold   int      `json:"healthy_threshold"`
	UnhealthyThreshold int      `json:"unhealthy_threshold"`
}

// Config is the whole config file.
type Config struct {
	Mode          string          `json:"mode"` // "l4" or "l7"
	Listen        string          `json:"listen"`
	MetricsListen string          `json:"metrics_listen"`
	Algorithm     string          `json:"algorithm"`
	Backends      []BackendConfig `json:"backends"`

	HealthCheck HealthCheckConfig `json:"health_check"`

	// Consecutive proxy failures before a backend is taken out of rotation.
	// 0 means the default (3); a negative value disables passive checks.
	PassiveFailureThreshold int `json:"passive_failure_threshold"`

	DialTimeout     Duration `json:"dial_timeout"`
	ShutdownTimeout Duration `json:"shutdown_timeout"`
}

// Load reads and validates a config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse decodes, fills in defaults and validates a config.
func Parse(data []byte) (*Config, error) {
	var c Config
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields() // catch typos like "algoritm"
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	c.applyDefaults()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) applyDefaults() {
	if c.Mode == "" {
		c.Mode = "l7"
	}
	if c.Listen == "" {
		c.Listen = ":8080"
	}
	if c.MetricsListen == "" {
		c.MetricsListen = ":9090"
	}
	if c.Algorithm == "" {
		c.Algorithm = "round_robin"
	}
	hc := &c.HealthCheck
	if hc.Type == "" {
		if c.Mode == "l4" {
			hc.Type = "tcp"
		} else {
			hc.Type = "http"
		}
	}
	if hc.Path == "" {
		hc.Path = "/health"
	}
	if hc.Interval.Duration == 0 {
		hc.Interval.Duration = 2 * time.Second
	}
	if hc.Timeout.Duration == 0 {
		hc.Timeout.Duration = time.Second
	}
	if hc.HealthyThreshold == 0 {
		hc.HealthyThreshold = 2
	}
	if hc.UnhealthyThreshold == 0 {
		hc.UnhealthyThreshold = 3
	}
	if c.PassiveFailureThreshold == 0 {
		c.PassiveFailureThreshold = 3
	}
	if c.DialTimeout.Duration == 0 {
		c.DialTimeout.Duration = 2 * time.Second
	}
	if c.ShutdownTimeout.Duration == 0 {
		c.ShutdownTimeout.Duration = 10 * time.Second
	}
}

// Validate reports every problem in the config at once.
func (c *Config) Validate() error {
	var errs []error
	if c.Mode != "l4" && c.Mode != "l7" {
		errs = append(errs, fmt.Errorf("mode must be \"l4\" or \"l7\", got %q", c.Mode))
	}
	if len(c.Backends) == 0 {
		errs = append(errs, errors.New("at least one backend is required"))
	}
	seen := map[string]bool{}
	for i, b := range c.Backends {
		if b.Name == "" || b.Addr == "" {
			errs = append(errs, fmt.Errorf("backend %d needs both name and addr", i))
		}
		if seen[b.Name] {
			errs = append(errs, fmt.Errorf("duplicate backend name %q", b.Name))
		}
		seen[b.Name] = true
	}
	hc := c.HealthCheck
	if hc.Type != "tcp" && hc.Type != "http" {
		errs = append(errs, fmt.Errorf("health_check.type must be \"tcp\" or \"http\", got %q", hc.Type))
	}
	if hc.Interval.Duration <= 0 || hc.Timeout.Duration <= 0 {
		errs = append(errs, errors.New("health_check interval and timeout must be positive"))
	}
	if hc.HealthyThreshold < 1 || hc.UnhealthyThreshold < 1 {
		errs = append(errs, errors.New("health_check thresholds must be at least 1"))
	}
	return errors.Join(errs...)
}
