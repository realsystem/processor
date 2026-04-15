package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/processor/data-plane/internal/config"
	"github.com/processor/data-plane/internal/controlclient"
	"github.com/processor/data-plane/internal/executor"
)

func main() {
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

	// Create executor registry
	executorRegistry := executor.NewDefaultRegistry(cfg.OllamaBaseURL)

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

	log.Printf("Received job: %s (type: %s)", job.ID, job.JobType)

	// Mark job as running
	if err := w.client.MarkRunning(w.cfg.WorkerID, job.ID); err != nil {
		log.Printf("Failed to mark job as running: %v", err)
		return err
	}

	// Start heartbeat goroutine
	heartbeatCtx, cancelHeartbeat := context.WithCancel(ctx)
	defer cancelHeartbeat()

	go w.sendHeartbeats(heartbeatCtx, job.ID)

	// Execute job
	result, err := w.executeJob(ctx, job)

	if err != nil {
		log.Printf("Job %s failed: %v", job.ID, err)
		failErr := w.client.Fail(w.cfg.WorkerID, job.ID, err.Error(), true)
		if failErr != nil {
			log.Printf("Failed to report job failure: %v", failErr)
		}
		return err
	}

	log.Printf("Job %s completed successfully", job.ID)
	if err := w.client.Complete(w.cfg.WorkerID, job.ID, result); err != nil {
		log.Printf("Failed to report job completion: %v", err)
		return err
	}

	return nil
}

func (w *Worker) executeJob(ctx context.Context, job *controlclient.Job) (map[string]interface{}, error) {
	log.Printf("Executing job %s of type %s", job.ID, job.JobType)

	// Create execution context with timeout
	execCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// Execute job
	result, err := w.executor.Execute(execCtx, job.JobType, job.InputMetadata)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (w *Worker) sendHeartbeats(ctx context.Context, jobID string) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.client.Heartbeat(w.cfg.WorkerID, jobID); err != nil {
				log.Printf("Failed to send heartbeat: %v", err)
			}
		}
	}
}
