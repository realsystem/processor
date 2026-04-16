package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

// Executor defines the interface for job executors
type Executor interface {
	Execute(ctx context.Context, jobType string, input map[string]interface{}) (map[string]interface{}, error)
}

// Registry holds all available executors
type Registry struct {
	executors map[string]Executor
}

// NewRegistry creates a new executor registry
func NewRegistry() *Registry {
	return &Registry{
		executors: make(map[string]Executor),
	}
}

// Register registers an executor for a job type
func (r *Registry) Register(jobType string, executor Executor) {
	r.executors[jobType] = executor
}

// Execute executes a job
func (r *Registry) Execute(ctx context.Context, jobType string, input map[string]interface{}) (map[string]interface{}, error) {
	executor, ok := r.executors[jobType]
	if !ok {
		return nil, fmt.Errorf("no executor found for job type: %s", jobType)
	}

	return executor.Execute(ctx, jobType, input)
}

// NoopExecutor does nothing (for testing)
type NoopExecutor struct{}

func (e *NoopExecutor) Execute(ctx context.Context, jobType string, input map[string]interface{}) (map[string]interface{}, error) {
	log.Printf("NoopExecutor: executing job (no-op)")
	return map[string]interface{}{
		"status": "completed",
		"message": "No operation performed",
	}, nil
}

// SleepExecutor sleeps for a specified duration
type SleepExecutor struct{}

func (e *SleepExecutor) Execute(ctx context.Context, jobType string, input map[string]interface{}) (map[string]interface{}, error) {
	durationSec, ok := input["duration_seconds"].(float64)
	if !ok {
		durationSec = 1.0
	}

	duration := time.Duration(durationSec * float64(time.Second))
	log.Printf("SleepExecutor: sleeping for %v", duration)

	select {
	case <-time.After(duration):
		return map[string]interface{}{
			"status":           "completed",
			"slept_for_seconds": durationSec,
		}, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("sleep cancelled")
	}
}

// EchoExecutor echoes back the input metadata
type EchoExecutor struct{}

func (e *EchoExecutor) Execute(ctx context.Context, jobType string, input map[string]interface{}) (map[string]interface{}, error) {
	log.Printf("EchoExecutor: echoing input metadata")

	return map[string]interface{}{
		"status":       "completed",
		"echo":         input,
		"processed_at": time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// OllamaExecutor executes Ollama chat jobs
type OllamaExecutor struct {
	BaseURL string
	Client  *http.Client
}

// OllamaGenerateRequest represents the request to Ollama's generate API
type OllamaGenerateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

// OllamaGenerateResponse represents the response from Ollama's generate API
type OllamaGenerateResponse struct {
	Model     string `json:"model"`
	CreatedAt string `json:"created_at"`
	Response  string `json:"response"`
	Done      bool   `json:"done"`
}

func (e *OllamaExecutor) Execute(ctx context.Context, jobType string, input map[string]interface{}) (map[string]interface{}, error) {
	// Extract parameters
	prompt, ok := input["prompt"].(string)
	if !ok || prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}

	model, ok := input["model"].(string)
	if !ok || model == "" {
		model = "llama3.2" // Default to llama3.2 (small, fast)
	}

	// Determine Ollama base URL (check env var if not set or set to default localhost)
	baseURL := e.BaseURL
	if baseURL == "" || baseURL == "http://localhost:11434" {
		if envURL := os.Getenv("OLLAMA_BASE_URL"); envURL != "" {
			baseURL = envURL
		} else if baseURL == "" {
			baseURL = "http://localhost:11434"
		}
	}

	log.Printf("OllamaExecutor: calling Ollama at %s with model=%s, prompt=%s", baseURL, model, prompt)

	// Prepare request
	reqBody := OllamaGenerateRequest{
		Model:  model,
		Prompt: prompt,
		Stream: false, // Non-streaming for simplicity
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	url := fmt.Sprintf("%s/api/generate", baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Get or create HTTP client
	client := e.Client
	if client == nil {
		client = &http.Client{
			Timeout: 5 * time.Minute, // Ollama can take a while
		}
	}

	// Send request
	startTime := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call Ollama API: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Ollama API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var ollamaResp OllamaGenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	duration := time.Since(startTime)

	log.Printf("OllamaExecutor: completed in %v, got %d chars response", duration, len(ollamaResp.Response))

	// Return result
	return map[string]interface{}{
		"status":            "completed",
		"model":             ollamaResp.Model,
		"prompt":            prompt,
		"response":          ollamaResp.Response,
		"created_at":        ollamaResp.CreatedAt,
		"duration_seconds":  duration.Seconds(),
		"response_length":   len(ollamaResp.Response),
	}, nil
}

// NewDefaultRegistry creates a registry with all default executors
func NewDefaultRegistry(ollamaBaseURL string) *Registry {
	registry := NewRegistry()
	registry.Register("noop", &NoopExecutor{})
	registry.Register("sleep", &SleepExecutor{})
	registry.Register("echo_metadata", &EchoExecutor{})
	registry.Register("ollama_chat", &OllamaExecutor{BaseURL: ollamaBaseURL})
	return registry
}
