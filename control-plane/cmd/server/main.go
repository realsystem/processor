package main

import (
	"context"
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
	"github.com/processor/control-plane/internal/web"
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

	// Initialize template renderer
	templatesDir := "control-plane/templates"
	renderer, err := web.NewTemplateRenderer(templatesDir)
	if err != nil {
		log.Fatalf("Failed to initialize template renderer: %v", err)
	}

	// Initialize web handlers
	webHandlers := web.NewWebHandlers(authService, jobsService, webhookService, workerService, renderer)

	// Setup router
	mux := http.NewServeMux()

	// Static files
	fs := http.FileServer(http.Dir("control-plane/static"))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))

	// Web UI routes (public)
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			webHandlers.ShowLogin(w, r)
		} else if r.Method == http.MethodPost {
			webHandlers.Login(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			webHandlers.ShowRegister(w, r)
		} else if r.Method == http.MethodPost {
			webHandlers.Register(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/logout", webHandlers.Logout)

	// Web UI routes (require authentication)
	mux.Handle("/", authMiddleware.Session(http.HandlerFunc(webHandlers.Dashboard)))
	mux.Handle("/profile", authMiddleware.Session(http.HandlerFunc(webHandlers.ShowProfile)))

	// Jobs
	mux.Handle("/jobs", authMiddleware.Session(http.HandlerFunc(webHandlers.ListJobs)))
	mux.Handle("/jobs/new", authMiddleware.Session(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			webHandlers.ShowNewJob(w, r)
		} else if r.Method == http.MethodPost {
			webHandlers.CreateJob(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})))
	mux.Handle("/jobs/", authMiddleware.Session(http.HandlerFunc(webHandlers.ShowJob)))

	// API Keys
	mux.Handle("/api-keys", authMiddleware.Session(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			webHandlers.ListAPIKeys(w, r)
		} else if r.Method == http.MethodPost {
			webHandlers.CreateAPIKey(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})))
	mux.Handle("/api-keys/", authMiddleware.Session(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && contains(r.URL.Path, "/revoke") {
			webHandlers.RevokeAPIKey(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})))

	// Webhooks
	mux.Handle("/webhooks", authMiddleware.Session(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			webHandlers.ListWebhooks(w, r)
		} else if r.Method == http.MethodPost {
			webHandlers.CreateWebhook(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})))
	mux.Handle("/webhooks/", authMiddleware.Session(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && contains(r.URL.Path, "/delete") {
			webHandlers.DeleteWebhook(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})))

	// Admin routes
	mux.Handle("/admin", authMiddleware.Session(http.HandlerFunc(webHandlers.AdminDashboard)))
	mux.Handle("/admin/jobs", authMiddleware.Session(http.HandlerFunc(webHandlers.AdminJobs)))
	mux.Handle("/admin/workers", authMiddleware.Session(http.HandlerFunc(webHandlers.AdminWorkers)))
	mux.Handle("/admin/users", authMiddleware.Session(http.HandlerFunc(webHandlers.AdminUsers)))

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

	// Create context for background workers
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start background workers
	go startBackgroundWorkers(ctx, jobsService, webhookService, emailService)

	// Start HTTP server
	addr := fmt.Sprintf(":%d", cfg.HTTPPort)
	server := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Setup graceful shutdown
	shutdown := make(chan error, 1)
	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt, syscall.SIGTERM)
		<-sigint

		log.Println("Received shutdown signal, stopping server gracefully...")

		// Stop background workers
		cancel()

		// Give server 30 seconds to shut down gracefully
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			shutdown <- fmt.Errorf("HTTP server shutdown error: %w", err)
			return
		}

		log.Println("Server shutdown complete")
		shutdown <- nil
	}()

	log.Printf("Server listening on %s", addr)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("HTTP server error: %v", err)
	}

	// Wait for graceful shutdown to complete
	if err := <-shutdown; err != nil {
		log.Printf("Shutdown error: %v", err)
		os.Exit(1)
	}

	log.Println("Server stopped cleanly")
}

func startBackgroundWorkers(ctx context.Context, jobsService *jobs.Service, webhookService *webhooks.Service, emailService *email.Service) {
	// Release expired leases every 30 seconds
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				log.Println("Stopping lease expiration worker")
				return
			case <-ticker.C:
				count, err := jobsService.ReleaseExpiredLeases()
				if err != nil {
					log.Printf("Error releasing expired leases: %v", err)
				} else if count > 0 {
					log.Printf("Released %d expired leases", count)
				}
			}
		}
	}()

	// Process webhook deliveries every 10 seconds
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				log.Println("Stopping webhook delivery worker")
				return
			case <-ticker.C:
				count, err := webhookService.ProcessPendingDeliveries(10)
				if err != nil {
					log.Printf("Error processing webhook deliveries: %v", err)
				} else if count > 0 {
					log.Printf("Processed %d webhook deliveries", count)
				}
			}
		}
	}()

	// Process email notifications every 10 seconds
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				log.Println("Stopping email notification worker")
				return
			case <-ticker.C:
				count, err := emailService.ProcessPending(10)
				if err != nil {
					log.Printf("Error processing email notifications: %v", err)
				} else if count > 0 {
					log.Printf("Processed %d email notifications", count)
				}
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
