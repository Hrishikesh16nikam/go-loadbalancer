package config

import (
	"strings"
	"testing"
	"time"
)

func TestDefaults(t *testing.T) {
	c, err := Parse([]byte(`{"backends":[{"name":"a","addr":"localhost:9001"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Mode != "l7" || c.Algorithm != "round_robin" || c.Listen != ":8080" {
		t.Errorf("unexpected defaults: %+v", c)
	}
	if c.HealthCheck.Type != "http" || c.HealthCheck.Interval.Duration != 2*time.Second {
		t.Errorf("unexpected health check defaults: %+v", c.HealthCheck)
	}
	if c.PassiveFailureThreshold != 3 {
		t.Errorf("PassiveFailureThreshold = %d, want 3", c.PassiveFailureThreshold)
	}
}

func TestL4DefaultsToTCPHealthChecks(t *testing.T) {
	c, err := Parse([]byte(`{"mode":"l4","backends":[{"name":"a","addr":"localhost:9001"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.HealthCheck.Type != "tcp" {
		t.Errorf("health check type = %q, want tcp", c.HealthCheck.Type)
	}
}

func TestDurations(t *testing.T) {
	c, err := Parse([]byte(`{
		"backends":[{"name":"a","addr":"localhost:9001"}],
		"health_check":{"interval":"500ms","timeout":"250ms"},
		"dial_timeout":"3s"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.HealthCheck.Interval.Duration != 500*time.Millisecond || c.DialTimeout.Duration != 3*time.Second {
		t.Errorf("durations not parsed: %+v", c)
	}
}

func TestErrors(t *testing.T) {
	cases := map[string]string{
		"no backends":   `{}`,
		"bad mode":      `{"mode":"l5","backends":[{"name":"a","addr":"x:1"}]}`,
		"unknown field": `{"algoritm":"round_robin","backends":[{"name":"a","addr":"x:1"}]}`,
		"bad duration":  `{"dial_timeout":"soon","backends":[{"name":"a","addr":"x:1"}]}`,
		"duplicate":     `{"backends":[{"name":"a","addr":"x:1"},{"name":"a","addr":"x:2"}]}`,
	}
	for name, in := range cases {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestValidateReportsAllProblems(t *testing.T) {
	_, err := Parse([]byte(`{"mode":"l5"}`))
	if err == nil || !strings.Contains(err.Error(), "mode") || !strings.Contains(err.Error(), "backend") {
		t.Fatalf("expected both mode and backend errors, got %v", err)
	}
}
