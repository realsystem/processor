package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/processor/data-plane/internal/config"
	"github.com/processor/data-plane/internal/controlclient"
	"github.com/processor/data-plane/internal/executor"
	"github.com/processor/data-plane/internal/identity"
)

func main() {
	printKey := flag.Bool("print-public-key", false, "Print this worker's public key (creating WORKER_KEY_FILE if needed) and exit")
	flag.Parse()

	if *printKey {
		path := os.Getenv("WORKER_KEY_FILE")
		if path == "" {
			log.Fatal("WORKER_KEY_FILE is not set")
		}
		key, _, err := identity.LoadOrCreate(path)
		if err != nil {
			log.Fatalf("Worker key: %v", err)
		}
		fmt.Println(identity.PublicKeyB64(key))
		return
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Printf("Starting Processor Data Plane Worker")
	log.Printf("Worker ID: %s", cfg.WorkerID)
	log.Printf("Control Plane: %s", cfg.ControlPlaneURL)
	log.Printf("Poll Interval: %v", cfg.PollInterval)
	log.Printf("Capabilities: %v", cfg.Capabilities)

	// Create control plane client
	client := controlclient.NewClient(cfg.ControlPlaneURL, cfg.WorkerToken)
	if cfg.WorkerKeyFile != "" {
		key, created, err := identity.LoadOrCreate(cfg.WorkerKeyFile)
		if err != nil {
			log.Fatalf("Worker key: %v", err)
		}
		if created {
			log.Printf("Created worker key %s", cfg.WorkerKeyFile)
		}
		log.Printf("Signing requests as %s; public key %s (register it with: server -register-worker-key %s=%s)",
			cfg.WorkerID, identity.PublicKeyB64(key), cfg.WorkerID, identity.PublicKeyB64(key))
		client.WithSigningKey(cfg.WorkerID, key)
	}

	// Create executor registry
	executorRegistry := executor.NewDefaultRegistry(cfg.OllamaBaseURL)
	executorRegistry.Register("llm.chat", &executor.LLMChatExecutor{
		Backend:      cfg.LLMBackend,
		BaseURL:      cfg.LLMBaseURL,
		APIKey:       cfg.LLMAPIKey,
		DefaultModel: cfg.LLMDefaultModel,
	})
	log.Printf("llm.chat: %s backend at %s (default model %s)", cfg.LLMBackend, cfg.LLMBaseURL, cfg.LLMDefaultModel)

	// Create worker
	worker := &Worker{
		cfg:      cfg,
		client:   client,
		executor: executorRegistry,
	}

	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigint := make(chan os.Signal, 1)
	signal.Notify(sigint, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigint
		log.Println("Received shutdown signal, stopping worker...")
		cancel()
	}()

	// Run worker
	if err := worker.Run(ctx); err != nil {
		log.Fatalf("Worker error: %v", err)
	}

	log.Println("Worker stopped")
}

// Worker represents the data plane worker
type Worker struct {
	cfg      *config.Config
	client   *controlclient.Client
	executor *executor.Registry
}

// Run starts the worker polling loop
func (w *Worker) Run(ctx context.Context) error {
	log.Println("Worker started, polling for jobs...")

	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Worker stopping...")
			return nil

		case <-ticker.C:
			if err := w.pollAndExecute(ctx); err != nil {
				log.Printf("Error in poll cycle: %v", err)
			}
		}
	}
}

func (w *Worker) pollAndExecute(ctx context.Context) error {
	// Poll for next job
	job, err := w.client.Poll(w.cfg.WorkerID, w.cfg.Capabilities)
	if err != nil {
		return err
	}

	if job == nil {
		// No jobs available
		return nil
	}

	// only ids and types are logged; never inputs or results
	log.Printf("Received job: %s (type: %s%s)", job.ID, job.JobType, traceNote(job))

	// Mark job as running
	if err := w.client.MarkRunning(w.cfg.WorkerID, job.ID); err != nil {
		log.Printf("Failed to mark job as running: %v", err)
		return err
	}

	// The job runs until it finishes, times out or is canceled (heartbeat answer)
	execCtx, cancelExec := context.WithTimeout(ctx, w.cfg.JobTimeout)
	defer cancelExec()
	canceled := make(chan struct{})
	go w.sendHeartbeats(execCtx, job.ID, func() { close(canceled); cancelExec() })

	result, err := w.executor.Execute(execCtx, job.JobType, job.InputMetadata)

	select {
	case <-canceled:
		log.Printf("Job %s was canceled; dropped its result", job.ID)
		return nil
	default:
	}

	if err != nil {
		if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
			err = fmt.Errorf("job timed out after %v", w.cfg.JobTimeout)
		}
		msg := err.Error()
		if job.Sensitive {
			log.Printf("Job %s failed (details withheld: sensitive job)", job.ID)
		} else {
			log.Printf("Job %s failed: %v", job.ID, err)
		}
		if failErr := w.client.Fail(w.cfg.WorkerID, job.ID, msg, true); failErr != nil {
			log.Printf("Failed to report job failure: %v", failErr)
		}
		return nil
	}

	log.Printf("Job %s completed successfully", job.ID)
	if err := w.client.Complete(w.cfg.WorkerID, job.ID, result); err != nil {
		log.Printf("Failed to report job completion: %v", err)
		return err
	}

	return nil
}

func traceNote(job *controlclient.Job) string {
	if job.TraceParent == "" {
		return ""
	}
	return ", trace " + job.TraceParent
}

func (w *Worker) sendHeartbeats(ctx context.Context, jobID string, onCancel func()) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			err := w.client.Heartbeat(w.cfg.WorkerID, jobID)
			if errors.Is(err, controlclient.ErrCanceled) {
				onCancel()
				return
			}
			if err != nil {
				log.Printf("Failed to send heartbeat: %v", err)
			}
		}
	}
}
