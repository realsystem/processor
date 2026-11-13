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

	// WorkerKeyFile: Ed25519 private key (PEM, PKCS#8) used to sign requests; created
	// on first start if missing. Empty: requests are not signed.
	WorkerKeyFile string
	// JobTimeout bounds one job's execution.
	JobTimeout time.Duration

	// llm.chat executor: LLMBackend "ollama" (native /api/chat) or "openai" (any
	// OpenAI-compatible /v1/chat/completions: llama.cpp, vLLM, LM Studio, clouds).
	LLMBackend      string
	LLMBaseURL      string
	LLMAPIKey       string
	LLMDefaultModel string
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	cfg := &Config{
		ControlPlaneURL: getEnv("CONTROL_PLANE_URL", "http://localhost:8080"),
		WorkerToken:     getEnv("WORKER_TOKEN", ""),
		WorkerID:        getEnv("WORKER_ID", "worker_1"),
		WorkerName:      getEnv("WORKER_NAME", "default-worker"),
		PollInterval:    parseDuration(getEnv("POLL_INTERVAL", "5s"), 5*time.Second),
		Capabilities:    parseCapabilities(getEnv("WORKER_CAPABILITIES", "noop,sleep,echo_metadata,ollama_chat,llm.chat")),
		OllamaBaseURL:   getEnv("OLLAMA_BASE_URL", "http://localhost:11434"),
		LogLevel:        getEnv("LOG_LEVEL", "info"),
		WorkerKeyFile:   getEnv("WORKER_KEY_FILE", ""),
		JobTimeout:      parseDuration(getEnv("JOB_TIMEOUT", "5m"), 5*time.Minute),
		LLMBackend:      getEnv("LLM_BACKEND", "ollama"),
		LLMAPIKey:       getEnv("LLM_API_KEY", ""),
		LLMDefaultModel: getEnv("LLM_DEFAULT_MODEL", "llama3.2"),
	}
	cfg.LLMBaseURL = getEnv("LLM_BASE_URL", cfg.OllamaBaseURL)
	if f := getEnv("LLM_API_KEY_FILE", ""); f != "" && cfg.LLMAPIKey == "" {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("LLM_API_KEY_FILE: %w", err)
		}
		cfg.LLMAPIKey = strings.TrimSpace(string(b))
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
	if c.WorkerToken == "" && c.WorkerKeyFile == "" {
		return fmt.Errorf("WORKER_TOKEN or WORKER_KEY_FILE is required")
	}
	if c.LLMBackend != "ollama" && c.LLMBackend != "openai" {
		return fmt.Errorf("LLM_BACKEND must be ollama or openai")
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
