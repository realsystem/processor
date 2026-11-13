package config

import (
	"strings"
	"testing"
)

func prod() *Config {
	return &Config{
		DatabasePath:  "x.db",
		WorkerAuth:    "token",
		Env:           "production",
		SessionSecret: strings.Repeat("s", 40),
		WorkerToken:   strings.Repeat("w", 40),
		AdminPassword: "a-real-password-1f3c",
	}
}

func TestProductionAcceptsRealValues(t *testing.T) {
	if err := prod().Validate(); err != nil {
		t.Fatal(err)
	}
}

// The development defaults are published in the repository, so production must refuse them.
func TestProductionRefusesPublishedDefaults(t *testing.T) {
	for name, mutate := range map[string]func(*Config){
		"session secret": func(c *Config) { c.SessionSecret = "docker-dev-session-secret-padding-padding" },
		"worker token":   func(c *Config) { c.WorkerToken = "docker-dev-worker-token-padding-padding-x" },
		"admin password": func(c *Config) { c.AdminPassword = "admin123" },
	} {
		c := prod()
		mutate(c)
		if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "development default") {
			t.Errorf("%s: got %v, want a development-default error", name, err)
		}
	}
}

func TestDevelopmentKeepsItsDefaults(t *testing.T) {
	c := &Config{DatabasePath: "x.db", WorkerAuth: "token", Env: "development", SessionSecret: "docker-dev-session-secret", AdminPassword: "admin123"}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}
