package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all application configuration
type Config struct {
	// Database
	DatabasePath string

	// HTTP Server
	HTTPPort int
	BaseURL  string

	// Security
	SessionSecret string
	WorkerToken   string
	// WorkerAuth: "token" (shared WORKER_TOKEN, default), "signature" (per-worker
	// Ed25519 keys only) or "any" (either)
	WorkerAuth string

	// Typed job contracts (optional): a JSON file of {"job_types": [{name, version, input_schema,
	// result_schema}]}. With JobTypesStrict, a job type that is not listed is refused.
	JobTypesFile   string
	JobTypesStrict bool

	// Environment
	Env      string
	LogLevel string

	// Admin Bootstrap
	AdminEmail    string
	AdminPassword string

	// SMTP (optional)
	SMTPHost string
	SMTPPort int
	SMTPUser string
	SMTPPass string
	SMTPFrom string
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	cfg := &Config{
		DatabasePath:   getEnv("DATABASE_PATH", "./processor.db"),
		HTTPPort:       getEnvInt("HTTP_PORT", 8080),
		BaseURL:        getEnv("BASE_URL", "http://localhost:8080"),
		SessionSecret:  getEnv("SESSION_SECRET", ""),
		WorkerToken:    getEnv("WORKER_TOKEN", ""),
		WorkerAuth:     getEnv("WORKER_AUTH", "token"),
		JobTypesFile:   getEnv("JOB_TYPES_FILE", ""),
		JobTypesStrict: getEnv("JOB_TYPES_STRICT", "") == "true",
		Env:            getEnv("ENV", "development"),
		LogLevel:       getEnv("LOG_LEVEL", "info"),
		AdminEmail:     getEnv("ADMIN_EMAIL", ""),
		AdminPassword:  getEnv("ADMIN_PASSWORD", ""),
		SMTPHost:       getEnv("SMTP_HOST", ""),
		SMTPPort:       getEnvInt("SMTP_PORT", 587),
		SMTPUser:       getEnv("SMTP_USER", ""),
		SMTPPass:       getEnv("SMTP_PASS", ""),
		SMTPFrom:       getEnv("SMTP_FROM", "noreply@processor.local"),
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate checks that required configuration is present
func (c *Config) Validate() error {
	if c.DatabasePath == "" {
		return fmt.Errorf("DATABASE_PATH is required")
	}
	switch c.WorkerAuth {
	case "token", "signature", "any":
	default:
		return fmt.Errorf("WORKER_AUTH must be token, signature or any")
	}

	if c.Env == "production" {
		if c.SessionSecret == "" {
			return fmt.Errorf("SESSION_SECRET is required in production")
		}
		if c.WorkerToken == "" && c.WorkerAuth != "signature" {
			return fmt.Errorf("WORKER_TOKEN is required in production (or WORKER_AUTH=signature)")
		}
		if len(c.SessionSecret) < 32 {
			return fmt.Errorf("SESSION_SECRET must be at least 32 characters")
		}
		if c.WorkerToken != "" && len(c.WorkerToken) < 32 {
			return fmt.Errorf("WORKER_TOKEN must be at least 32 characters")
		}
	}

	return nil
}

// IsDevelopment returns true if running in development mode
func (c *Config) IsDevelopment() bool {
	return c.Env == "development"
}

// IsProduction returns true if running in production mode
func (c *Config) IsProduction() bool {
	return c.Env == "production"
}

// SessionDuration returns the session duration
func (c *Config) SessionDuration() time.Duration {
	return 24 * time.Hour
}

// LeaseDuration returns the default job lease duration
func (c *Config) LeaseDuration() time.Duration {
	return 10 * time.Minute
}

// HeartbeatInterval returns the expected heartbeat interval
func (c *Config) HeartbeatInterval() time.Duration {
	return 30 * time.Second
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}
