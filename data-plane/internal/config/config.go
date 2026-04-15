package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Config holds worker configuration
type Config struct {
	ControlPlaneURL string
	WorkerToken     string
	WorkerID        string
	WorkerName      string
	PollInterval    time.Duration
	Capabilities    []string
	OllamaBaseURL   string
	LogLevel        string
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	cfg := &Config{
		ControlPlaneURL: getEnv("CONTROL_PLANE_URL", "http://localhost:8080"),
		WorkerToken:     getEnv("WORKER_TOKEN", ""),
		WorkerID:        getEnv("WORKER_ID", "worker_1"),
		WorkerName:      getEnv("WORKER_NAME", "default-worker"),
		PollInterval:    parseDuration(getEnv("POLL_INTERVAL", "5s"), 5*time.Second),
		Capabilities:    parseCapabilities(getEnv("WORKER_CAPABILITIES", "noop,sleep,echo_metadata,ollama_chat")),
		OllamaBaseURL:   getEnv("OLLAMA_BASE_URL", "http://localhost:11434"),
		LogLevel:        getEnv("LOG_LEVEL", "info"),
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate checks that required configuration is present
func (c *Config) Validate() error {
	if c.ControlPlaneURL == "" {
		return fmt.Errorf("CONTROL_PLANE_URL is required")
	}
	if c.WorkerToken == "" {
		return fmt.Errorf("WORKER_TOKEN is required")
	}
	if c.WorkerID == "" {
		return fmt.Errorf("WORKER_ID is required")
	}
	return nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func parseDuration(s string, defaultValue time.Duration) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return defaultValue
	}
	return d
}

func parseCapabilities(s string) []string {
	parts := strings.Split(s, ",")
	var result []string
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
