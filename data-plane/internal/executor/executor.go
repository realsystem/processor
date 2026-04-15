package executor

import (
	"context"
	"fmt"
	"log"
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

// OllamaExecutor executes Ollama chat jobs (stub for now)
type OllamaExecutor struct {
	BaseURL string
}

func (e *OllamaExecutor) Execute(ctx context.Context, jobType string, input map[string]interface{}) (map[string]interface{}, error) {
	log.Printf("OllamaExecutor: processing job (stub implementation)")

	// In a real implementation, this would call the Ollama API
	// For now, we'll just return a stub response

	prompt, _ := input["prompt"].(string)
	model, ok := input["model"].(string)
	if !ok {
		model = "llama2"
	}

	log.Printf("OllamaExecutor: would call Ollama with model=%s, prompt=%s", model, prompt)

	return map[string]interface{}{
		"status":  "completed",
		"model":   model,
		"prompt":  prompt,
		"response": "This is a stub response. Ollama integration not yet implemented.",
		"stub":    true,
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
