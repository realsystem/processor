package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/processor/control-plane/internal/auth"
	"github.com/processor/control-plane/internal/config"
	"github.com/processor/control-plane/internal/db"
	"github.com/processor/control-plane/internal/email"
	"github.com/processor/control-plane/internal/httpapi"
	"github.com/processor/control-plane/internal/jobs"
	"github.com/processor/control-plane/internal/middleware"
	"github.com/processor/control-plane/internal/webhooks"
	"github.com/processor/control-plane/internal/workers"
)

var (
	migrateFlag = flag.Bool("migrate", false, "Run database migrations and exit")
)

func main() {
	flag.Parse()

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Printf("Starting Processor Control Plane")
	log.Printf("Environment: %s", cfg.Env)
	log.Printf("Database: %s", cfg.DatabasePath)

	// Open database
	database, err := db.Open(cfg.DatabasePath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	// Run migrations if requested
	if *migrateFlag {
		if err := db.Migrate(database); err != nil {
			log.Fatalf("Failed to run migrations: %v", err)
		}
		log.Println("Migrations completed successfully")
		return
	}

	// Initialize services
	authService := auth.NewService(database)
	jobsService := jobs.NewService(database, cfg.LeaseDuration())
	webhookService := webhooks.NewService(database)
	workerService := workers.NewService(database)

	// Initialize email provider
	var emailProvider email.Provider
	if cfg.SMTPHost != "" {
		emailProvider = email.NewSMTPProvider(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUser, cfg.SMTPPass, cfg.SMTPFrom)
	} else {
		emailProvider = email.NewLogProvider()
	}
	emailService := email.NewService(database, emailProvider)

	// Bootstrap admin user if configured
	if cfg.AdminEmail != "" && cfg.AdminPassword != "" {
		_, err := authService.Register(cfg.AdminEmail, cfg.AdminPassword, true)
		if err != nil && err != auth.ErrDuplicateEmail {
			log.Printf("Warning: Failed to create admin user: %v", err)
		} else if err == nil {
			log.Printf("Created admin user: %s", cfg.AdminEmail)
		}
	}

	// Ensure bootstrap worker exists
	if err := workerService.EnsureBootstrapWorker(cfg.WorkerToken); err != nil {
		log.Printf("Warning: Failed to ensure bootstrap worker: %v", err)
	}

	// Initialize API
	api := httpapi.NewAPI(authService, jobsService, webhookService, emailService, workerService, cfg.WorkerToken)

	// Initialize middleware
	authMiddleware := middleware.NewAuthMiddleware(authService)

	// Setup router
	mux := http.NewServeMux()

	// Health endpoint (no auth)
	mux.HandleFunc("/api/v1/health", api.Health)

	// API endpoints (require auth)
	mux.Handle("/api/v1/me", authMiddleware.APIAuth(http.HandlerFunc(api.Me)))
	mux.Handle("/api/v1/jobs", handleMethodRouter(authMiddleware.APIAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			api.ListJobs(w, r)
		case http.MethodPost:
			api.CreateJob(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}))))

	// Job detail endpoint
	mux.Handle("/api/v1/jobs/", authMiddleware.APIAuth(http.HandlerFunc(api.GetJob)))

	// API Keys
	mux.Handle("/api/v1/api-keys", handleMethodRouter(authMiddleware.APIAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			api.ListAPIKeys(w, r)
		case http.MethodPost:
			api.CreateAPIKey(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}))))

	mux.Handle("/api/v1/api-keys/", authMiddleware.APIAuth(http.HandlerFunc(api.RevokeAPIKey)))

	// Webhooks
	mux.Handle("/api/v1/webhooks", handleMethodRouter(authMiddleware.APIAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			api.ListWebhooks(w, r)
		case http.MethodPost:
			api.CreateWebhook(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}))))

	mux.Handle("/api/v1/webhooks/", authMiddleware.APIAuth(http.HandlerFunc(api.DeleteWebhook)))

	// Worker endpoints
	mux.HandleFunc("/api/v1/worker/poll", api.WorkerPoll)
	mux.HandleFunc("/api/v1/worker/heartbeat", api.WorkerHeartbeat)
	mux.HandleFunc("/api/v1/worker/jobs/", func(w http.ResponseWriter, r *http.Request) {
		if contains(r.URL.Path, "/start") {
			api.WorkerStartJob(w, r)
		} else if contains(r.URL.Path, "/complete") {
			api.WorkerCompleteJob(w, r)
		} else if contains(r.URL.Path, "/fail") {
			api.WorkerFailJob(w, r)
		} else {
			http.Error(w, "Not found", http.StatusNotFound)
		}
	})

	// Apply logging middleware
	handler := middleware.Logging(mux)

	// Start background workers
	go startBackgroundWorkers(jobsService, webhookService, emailService)

	// Start HTTP server
	addr := fmt.Sprintf(":%d", cfg.HTTPPort)
	server := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt, syscall.SIGTERM)
		<-sigint

		log.Println("Shutting down server...")
		if err := server.Close(); err != nil {
			log.Printf("HTTP server Close error: %v", err)
		}
	}()

	log.Printf("Server listening on %s", addr)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("HTTP server error: %v", err)
	}

	log.Println("Server stopped")
}

func startBackgroundWorkers(jobsService *jobs.Service, webhookService *webhooks.Service, emailService *email.Service) {
	// Release expired leases every 30 seconds
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			count, err := jobsService.ReleaseExpiredLeases()
			if err != nil {
				log.Printf("Error releasing expired leases: %v", err)
			} else if count > 0 {
				log.Printf("Released %d expired leases", count)
			}
		}
	}()

	// Process webhook deliveries every 10 seconds
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			count, err := webhookService.ProcessPendingDeliveries(10)
			if err != nil {
				log.Printf("Error processing webhook deliveries: %v", err)
			} else if count > 0 {
				log.Printf("Processed %d webhook deliveries", count)
			}
		}
	}()

	// Process email notifications every 10 seconds
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			count, err := emailService.ProcessPending(10)
			if err != nil {
				log.Printf("Error processing email notifications: %v", err)
			} else if count > 0 {
				log.Printf("Processed %d email notifications", count)
			}
		}
	}()
}

func handleMethodRouter(handler http.Handler) http.Handler {
	return handler
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && indexOf(s, substr) >= 0
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
