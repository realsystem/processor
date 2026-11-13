package executor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNoopExecutor(t *testing.T) {
	executor := &NoopExecutor{}
	ctx := context.Background()

	t.Run("executes successfully", func(t *testing.T) {
		result, err := executor.Execute(ctx, "noop", map[string]interface{}{})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if result["status"] != "completed" {
			t.Errorf("Expected status completed, got %v", result["status"])
		}
	})
}

func TestSleepExecutor(t *testing.T) {
	executor := &SleepExecutor{}
	ctx := context.Background()

	t.Run("sleeps for specified duration", func(t *testing.T) {
		start := time.Now()
		input := map[string]interface{}{
			"duration_seconds": 0.1, // 100ms
		}

		result, err := executor.Execute(ctx, "sleep", input)
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if elapsed < 100*time.Millisecond {
			t.Errorf("Expected to sleep at least 100ms, slept %v", elapsed)
		}

		if result["status"] != "completed" {
			t.Errorf("Expected status completed, got %v", result["status"])
		}

		if result["slept_for_seconds"] != 0.1 {
			t.Errorf("Expected slept_for_seconds 0.1, got %v", result["slept_for_seconds"])
		}
	})

	t.Run("uses default duration when not specified", func(t *testing.T) {
		result, err := executor.Execute(ctx, "sleep", map[string]interface{}{})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if result["status"] != "completed" {
			t.Errorf("Expected status completed, got %v", result["status"])
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		// Cancel immediately
		cancel()

		input := map[string]interface{}{
			"duration_seconds": 10.0,
		}

		_, err := executor.Execute(ctx, "sleep", input)
		if err == nil {
			t.Error("Expected error due to context cancellation")
		}
	})

	t.Run("respects context timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		input := map[string]interface{}{
			"duration_seconds": 10.0, // 10 seconds
		}

		_, err := executor.Execute(ctx, "sleep", input)
		if err == nil {
			t.Error("Expected error due to context timeout")
		}
	})
}

func TestEchoExecutor(t *testing.T) {
	executor := &EchoExecutor{}
	ctx := context.Background()

	t.Run("echoes input metadata", func(t *testing.T) {
		input := map[string]interface{}{
			"message": "hello",
			"number":  42,
			"nested": map[string]interface{}{
				"key": "value",
			},
		}

		result, err := executor.Execute(ctx, "echo_metadata", input)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if result["status"] != "completed" {
			t.Errorf("Expected status completed, got %v", result["status"])
		}

		echo, ok := result["echo"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected echo field to be present")
		}

		if echo["message"] != "hello" {
			t.Errorf("Expected message to be echoed, got %v", echo["message"])
		}

		if echo["number"] != 42 {
			t.Errorf("Expected number to be echoed, got %v", echo["number"])
		}
	})

	t.Run("includes processed_at timestamp", func(t *testing.T) {
		result, err := executor.Execute(ctx, "echo_metadata", map[string]interface{}{})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		processedAt, ok := result["processed_at"].(string)
		if !ok {
			t.Fatal("Expected processed_at to be a string")
		}

		if len(processedAt) == 0 {
			t.Error("Expected processed_at to be set")
		}
	})
}

// fakeOllama answers /api/generate like Ollama and records the requests.
func fakeOllama(t *testing.T) (*httptest.Server, *[]map[string]interface{}) {
	t.Helper()
	var got []map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		got = append(got, body)
		if r.URL.Path != "/api/generate" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"model": body["model"], "created_at": "2026-01-01T00:00:00Z",
			"response": "42", "done": true,
		})
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestOllamaExecutor(t *testing.T) {
	srv, got := fakeOllama(t)
	executor := &OllamaExecutor{BaseURL: srv.URL}
	ctx := context.Background()

	t.Run("calls Ollama and returns its response", func(t *testing.T) {
		input := map[string]interface{}{
			"model":  "llama2",
			"prompt": "What is the meaning of life?",
		}

		result, err := executor.Execute(ctx, "ollama_chat", input)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		if result["status"] != "completed" || result["model"] != "llama2" || result["response"] != "42" {
			t.Errorf("unexpected result %v", result)
		}
		if result["prompt"] != "What is the meaning of life?" {
			t.Errorf("Expected prompt to be preserved, got %v", result["prompt"])
		}
		last := (*got)[len(*got)-1]
		if last["prompt"] != "What is the meaning of life?" || last["stream"] != false {
			t.Errorf("unexpected request %v", last)
		}
	})

	t.Run("uses default model when not specified", func(t *testing.T) {
		result, err := executor.Execute(ctx, "ollama_chat", map[string]interface{}{"prompt": "test prompt"})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		if result["model"] != "llama3.2" {
			t.Errorf("Expected default model llama3.2, got %v", result["model"])
		}
	})

	t.Run("prompt is required", func(t *testing.T) {
		if _, err := executor.Execute(ctx, "ollama_chat", map[string]interface{}{}); err == nil {
			t.Error("expected an error without a prompt")
		}
	})

	t.Run("Ollama errors are returned", func(t *testing.T) {
		bad := &OllamaExecutor{BaseURL: srv.URL + "/nope"}
		if _, err := bad.Execute(ctx, "ollama_chat", map[string]interface{}{"prompt": "x"}); err == nil {
			t.Error("expected an error from a 404")
		}
	})
}

func TestRegistry(t *testing.T) {
	registry := NewRegistry()

	t.Run("register and execute", func(t *testing.T) {
		executor := &NoopExecutor{}
		registry.Register("noop", executor)

		result, err := registry.Execute(context.Background(), "noop", map[string]interface{}{})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if result["status"] != "completed" {
			t.Errorf("Expected status completed, got %v", result["status"])
		}
	})

	t.Run("error on unknown job type", func(t *testing.T) {
		_, err := registry.Execute(context.Background(), "unknown", map[string]interface{}{})
		if err == nil {
			t.Error("Expected error for unknown job type")
		}
	})

	t.Run("can replace registered executor", func(t *testing.T) {
		registry.Register("test", &NoopExecutor{})
		registry.Register("test", &EchoExecutor{}) // Replace

		result, err := registry.Execute(context.Background(), "test", map[string]interface{}{"key": "value"})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		// Should use EchoExecutor
		if _, ok := result["echo"]; !ok {
			t.Error("Expected echo field from EchoExecutor")
		}
	})
}

func TestNewDefaultRegistry(t *testing.T) {
	registry := NewDefaultRegistry("http://localhost:11434")

	t.Run("has all default executors", func(t *testing.T) {
		jobTypes := []string{"noop", "sleep", "echo_metadata", "ollama_chat"}

		for _, jobType := range jobTypes {
			if !registry.Has(jobType) {
				t.Errorf("Expected executor for %s to be registered", jobType)
			}
		}
	})

	t.Run("noop executor works", func(t *testing.T) {
		result, err := registry.Execute(context.Background(), "noop", map[string]interface{}{})
		if err != nil {
			t.Fatalf("Noop failed: %v", err)
		}

		if result["status"] != "completed" {
			t.Error("Noop executor not working correctly")
		}
	})

	t.Run("echo executor works", func(t *testing.T) {
		input := map[string]interface{}{"test": "value"}
		result, err := registry.Execute(context.Background(), "echo_metadata", input)
		if err != nil {
			t.Fatalf("Echo failed: %v", err)
		}

		echo, ok := result["echo"].(map[string]interface{})
		if !ok || echo["test"] != "value" {
			t.Error("Echo executor not working correctly")
		}
	})
}

func TestExecutorContextCancellation(t *testing.T) {
	t.Run("all executors respect context", func(t *testing.T) {
		registry := NewDefaultRegistry("http://localhost:11434")
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		// Sleep should respect cancellation
		input := map[string]interface{}{"duration_seconds": 10.0}
		_, err := registry.Execute(ctx, "sleep", input)
		if err == nil {
			t.Error("Sleep executor should respect context cancellation")
		}
	})
}

func TestExecutorConcurrency(t *testing.T) {
	t.Run("registry is safe for concurrent use", func(t *testing.T) {
		registry := NewDefaultRegistry("http://localhost:11434")

		// Run multiple executions concurrently
		done := make(chan bool, 10)
		for i := 0; i < 10; i++ {
			go func() {
				_, err := registry.Execute(context.Background(), "noop", map[string]interface{}{})
				if err != nil {
					t.Errorf("Concurrent execution failed: %v", err)
				}
				done <- true
			}()
		}

		// Wait for all to complete
		for i := 0; i < 10; i++ {
			<-done
		}
	})
}
