package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/processor/control-plane/internal/auth"
	"github.com/processor/control-plane/internal/jobs"
	"github.com/processor/control-plane/internal/middleware"
	"github.com/processor/control-plane/internal/webhooks"
	"github.com/processor/control-plane/internal/workers"
)

// WebHandlers handles web UI requests
type WebHandlers struct {
	authService    *auth.Service
	jobsService    *jobs.Service
	webhookService *webhooks.Service
	workerService  *workers.Service
	renderer       *TemplateRenderer
}

// NewWebHandlers creates a new web handlers instance
func NewWebHandlers(
	authService *auth.Service,
	jobsService *jobs.Service,
	webhookService *webhooks.Service,
	workerService *workers.Service,
	renderer *TemplateRenderer,
) *WebHandlers {
	return &WebHandlers{
		authService:    authService,
		jobsService:    jobsService,
		webhookService: webhookService,
		workerService:  workerService,
		renderer:       renderer,
	}
}

// ShowLogin shows the login page
func (h *WebHandlers) ShowLogin(w http.ResponseWriter, r *http.Request) {
	// Redirect if already logged in
	if user := middleware.GetUserFromContext(r.Context()); user != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	h.renderer.Render(w, r, "login.html", map[string]interface{}{
		"Title": "Sign In",
	})
}

// Login handles login form submission
func (h *WebHandlers) Login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")

	// Validate CSRF
	if !ValidateCSRF(r, r.FormValue("csrf_token")) {
		SetFlash(w, "error", "Invalid request. Please try again.")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Validate input
	errors := make(map[string]string)
	if email == "" {
		errors["Email"] = "Email is required"
	}
	if password == "" {
		errors["Password"] = "Password is required"
	}

	if len(errors) > 0 {
		h.renderer.Render(w, r, "login.html", map[string]interface{}{
			"Title":  "Sign In",
			"Email":  email,
			"Errors": errors,
		})
		return
	}

	// Authenticate
	sessionToken, user, err := h.authService.Login(email, password, 7*24*time.Hour)
	if err != nil {
		if err == auth.ErrInvalidCredentials || err == auth.ErrUserDisabled {
			SetFlash(w, "error", "Invalid email or password")
		} else {
			SetFlash(w, "error", "Login failed. Please try again.")
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Set session cookie
	SetSessionCookie(w, sessionToken)

	// Store user ID in session for flash messages
	SetSessionValue(w, "user_id", user.ID)

	SetFlash(w, "success", "Welcome back!")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// ShowRegister shows the registration page
func (h *WebHandlers) ShowRegister(w http.ResponseWriter, r *http.Request) {
	// Redirect if already logged in
	if user := middleware.GetUserFromContext(r.Context()); user != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	h.renderer.Render(w, r, "register.html", map[string]interface{}{
		"Title": "Create Account",
	})
}

// Register handles registration form submission
func (h *WebHandlers) Register(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	passwordConfirm := r.FormValue("password_confirm")

	// Validate CSRF
	if !ValidateCSRF(r, r.FormValue("csrf_token")) {
		SetFlash(w, "error", "Invalid request. Please try again.")
		http.Redirect(w, r, "/register", http.StatusSeeOther)
		return
	}

	// Validate input
	errors := make(map[string]string)
	if email == "" {
		errors["Email"] = "Email is required"
	}
	if password == "" {
		errors["Password"] = "Password is required"
	} else if len(password) < 8 {
		errors["Password"] = "Password must be at least 8 characters"
	}
	if password != passwordConfirm {
		errors["PasswordConfirm"] = "Passwords do not match"
	}

	if len(errors) > 0 {
		h.renderer.Render(w, r, "register.html", map[string]interface{}{
			"Title":  "Create Account",
			"Email":  email,
			"Errors": errors,
		})
		return
	}

	// Register user
	_, err := h.authService.Register(email, password, false)
	if err != nil {
		if err == auth.ErrDuplicateEmail {
			errors["Email"] = "Email already registered"
			h.renderer.Render(w, r, "register.html", map[string]interface{}{
				"Title":  "Create Account",
				"Email":  email,
				"Errors": errors,
			})
		} else {
			SetFlash(w, "error", "Registration failed. Please try again.")
			http.Redirect(w, r, "/register", http.StatusSeeOther)
		}
		return
	}

	SetFlash(w, "success", "Account created! Please sign in.")
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// Logout handles logout
func (h *WebHandlers) Logout(w http.ResponseWriter, r *http.Request) {
	// Validate CSRF for POST requests
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err == nil {
			if !ValidateCSRF(r, r.FormValue("csrf_token")) {
				SetFlash(w, "error", "Invalid request")
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
		}
	}

	// Clear session cookie
	ClearSessionCookie(w)

	SetFlash(w, "success", "You have been signed out")
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// Dashboard shows the user dashboard
func (h *WebHandlers) Dashboard(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Get stats
	stats := h.getUserStats(user.ID)

	// Get recent jobs
	recentJobs, _, _ := h.jobsService.List(user.ID, "", 5, 0)

	h.renderer.Render(w, r, "dashboard.html", map[string]interface{}{
		"Title":      "Dashboard",
		"Stats":      stats,
		"RecentJobs": recentJobs,
	})
}

// ListJobs shows the jobs list
func (h *WebHandlers) ListJobs(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	status := r.URL.Query().Get("status")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	limit := 25
	offset := (page - 1) * limit

	jobsList, total, err := h.jobsService.List(user.ID, status, limit, offset)
	if err != nil {
		SetFlash(w, "error", "Failed to load jobs")
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	totalPages := (total + limit - 1) / limit
	start := offset + 1
	end := offset + len(jobsList)
	if end > total {
		end = total
	}

	h.renderer.Render(w, r, "jobs/list.html", map[string]interface{}{
		"Title": "Jobs",
		"Jobs":  jobsList,
		"Filter": map[string]string{
			"Status": status,
		},
		"Pagination": map[string]interface{}{
			"Page":       page,
			"Total":      total,
			"TotalPages": totalPages,
			"Start":      start,
			"End":        end,
		},
	})
}

// ShowNewJob shows the create job form
func (h *WebHandlers) ShowNewJob(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	h.renderer.Render(w, r, "jobs/new.html", map[string]interface{}{
		"Title": "Create Job",
	})
}

// CreateJob handles job creation
func (h *WebHandlers) CreateJob(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Validate CSRF
	if !ValidateCSRF(r, r.FormValue("csrf_token")) {
		SetFlash(w, "error", "Invalid request. Please try again.")
		http.Redirect(w, r, "/jobs/new", http.StatusSeeOther)
		return
	}

	jobType := r.FormValue("job_type")
	inputMetadataStr := strings.TrimSpace(r.FormValue("input_metadata"))
	priorityWeight, _ := strconv.Atoi(r.FormValue("priority_weight"))
	maxAttempts, _ := strconv.Atoi(r.FormValue("max_attempts"))
	notifyEmail := r.FormValue("notify_email") == "true"
	notifyWebhook := r.FormValue("notify_webhook") == "true"

	// Validate
	errors := make(map[string]string)
	if jobType == "" {
		errors["JobType"] = "Job type is required"
	}

	var inputMetadata map[string]interface{}
	if inputMetadataStr == "" {
		inputMetadata = make(map[string]interface{})
	} else {
		if err := json.Unmarshal([]byte(inputMetadataStr), &inputMetadata); err != nil {
			errors["InputMetadata"] = "Invalid JSON format"
		}
	}

	if maxAttempts < 1 {
		maxAttempts = 3
	}
	if maxAttempts > 10 {
		errors["MaxAttempts"] = "Maximum 10 attempts allowed"
	}

	if len(errors) > 0 {
		h.renderer.Render(w, r, "jobs/new.html", map[string]interface{}{
			"Title":  "Create Job",
			"Errors": errors,
			"FormData": map[string]interface{}{
				"JobType":        jobType,
				"InputMetadata":  inputMetadataStr,
				"PriorityWeight": priorityWeight,
				"MaxAttempts":    maxAttempts,
				"NotifyEmail":    notifyEmail,
				"NotifyWebhook":  notifyWebhook,
			},
		})
		return
	}

	// Create job
	job, err := h.jobsService.Create(user.ID, jobType, inputMetadata, priorityWeight, maxAttempts, notifyEmail, notifyWebhook)
	if err != nil {
		SetFlash(w, "error", "Failed to create job")
		http.Redirect(w, r, "/jobs/new", http.StatusSeeOther)
		return
	}

	SetFlash(w, "success", "Job created successfully")
	http.Redirect(w, r, "/jobs/"+job.ID, http.StatusSeeOther)
}

// ShowJob shows job details
func (h *WebHandlers) ShowJob(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	jobID := extractIDFromPath(r.URL.Path, "/jobs/")

	job, err := h.jobsService.Get(jobID)
	if err != nil {
		if err == jobs.ErrJobNotFound {
			SetFlash(w, "error", "Job not found")
		} else {
			SetFlash(w, "error", "Failed to load job")
		}
		http.Redirect(w, r, "/jobs", http.StatusSeeOther)
		return
	}

	// Check permissions
	if job.UserID != user.ID && !user.IsAdmin {
		SetFlash(w, "error", "Access denied")
		http.Redirect(w, r, "/jobs", http.StatusSeeOther)
		return
	}

	// Get attempts
	attempts, _ := h.jobsService.GetAttempts(jobID)

	h.renderer.Render(w, r, "jobs/detail.html", map[string]interface{}{
		"Title":    "Job Details",
		"Job":      job,
		"Attempts": attempts,
	})
}

// ListAPIKeys shows API keys list
func (h *WebHandlers) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	keys, err := h.authService.ListAPIKeys(user.ID)
	if err != nil {
		SetFlash(w, "error", "Failed to load API keys")
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	h.renderer.Render(w, r, "api-keys/list.html", map[string]interface{}{
		"Title":   "API Keys",
		"APIKeys": keys,
	})
}

// CreateAPIKey handles API key creation
func (h *WebHandlers) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Validate CSRF
	if !ValidateCSRF(r, r.FormValue("csrf_token")) {
		SetFlash(w, "error", "Invalid request. Please try again.")
		http.Redirect(w, r, "/api-keys", http.StatusSeeOther)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		SetFlash(w, "error", "Name is required")
		http.Redirect(w, r, "/api-keys", http.StatusSeeOther)
		return
	}

	fullKey, _, err := h.authService.CreateAPIKey(user.ID, name)
	if err != nil {
		SetFlash(w, "error", "Failed to create API key")
		http.Redirect(w, r, "/api-keys", http.StatusSeeOther)
		return
	}

	// Show the key once
	keys, _ := h.authService.ListAPIKeys(user.ID)
	h.renderer.Render(w, r, "api-keys/list.html", map[string]interface{}{
		"Title":   "API Keys",
		"APIKeys": keys,
		"NewKey":  fullKey,
	})
}

// RevokeAPIKey handles API key revocation
func (h *WebHandlers) RevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Validate CSRF
	if !ValidateCSRF(r, r.FormValue("csrf_token")) {
		SetFlash(w, "error", "Invalid request. Please try again.")
		http.Redirect(w, r, "/api-keys", http.StatusSeeOther)
		return
	}

	keyID := extractIDFromPath(r.URL.Path, "/api-keys/")

	err := h.authService.RevokeAPIKey(keyID, user.ID)
	if err != nil {
		SetFlash(w, "error", "Failed to revoke API key")
	} else {
		SetFlash(w, "success", "API key revoked")
	}

	http.Redirect(w, r, "/api-keys", http.StatusSeeOther)
}

// ListWebhooks shows webhooks list
func (h *WebHandlers) ListWebhooks(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	webhooksList, err := h.webhookService.List(user.ID)
	if err != nil {
		SetFlash(w, "error", "Failed to load webhooks")
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	h.renderer.Render(w, r, "webhooks/list.html", map[string]interface{}{
		"Title":    "Webhooks",
		"Webhooks": webhooksList,
	})
}

// CreateWebhook handles webhook creation
func (h *WebHandlers) CreateWebhook(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Validate CSRF
	if !ValidateCSRF(r, r.FormValue("csrf_token")) {
		SetFlash(w, "error", "Invalid request. Please try again.")
		http.Redirect(w, r, "/webhooks", http.StatusSeeOther)
		return
	}

	targetURL := strings.TrimSpace(r.FormValue("target_url"))
	secret := r.FormValue("secret")
	events := r.Form["events"]
	isActive := r.FormValue("is_active") == "true"

	if targetURL == "" {
		SetFlash(w, "error", "Target URL is required")
		http.Redirect(w, r, "/webhooks", http.StatusSeeOther)
		return
	}

	if secret == "" {
		SetFlash(w, "error", "Secret is required")
		http.Redirect(w, r, "/webhooks", http.StatusSeeOther)
		return
	}

	if len(events) == 0 {
		events = []string{"job.succeeded", "job.failed", "job.dead_letter"}
	}

	_, err := h.webhookService.Create(user.ID, targetURL, secret, events, isActive)
	if err != nil {
		SetFlash(w, "error", "Failed to create webhook")
	} else {
		SetFlash(w, "success", "Webhook created successfully")
	}

	http.Redirect(w, r, "/webhooks", http.StatusSeeOther)
}

// DeleteWebhook handles webhook deletion
func (h *WebHandlers) DeleteWebhook(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Validate CSRF
	if !ValidateCSRF(r, r.FormValue("csrf_token")) {
		SetFlash(w, "error", "Invalid request. Please try again.")
		http.Redirect(w, r, "/webhooks", http.StatusSeeOther)
		return
	}

	webhookID := extractIDFromPath(r.URL.Path, "/webhooks/")

	err := h.webhookService.Delete(webhookID, user.ID)
	if err != nil {
		SetFlash(w, "error", "Failed to delete webhook")
	} else {
		SetFlash(w, "success", "Webhook deleted")
	}

	http.Redirect(w, r, "/webhooks", http.StatusSeeOther)
}

// ShowProfile shows user profile
func (h *WebHandlers) ShowProfile(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	h.renderer.Render(w, r, "profile.html", map[string]interface{}{
		"Title": "Profile",
	})
}

// Admin handlers

// AdminDashboard shows admin dashboard
func (h *WebHandlers) AdminDashboard(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil || !user.IsAdmin {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	stats := h.getAdminStats()

	h.renderer.Render(w, r, "admin/dashboard.html", map[string]interface{}{
		"Title": "Admin Dashboard",
		"Stats": stats,
	})
}

// AdminJobs shows all jobs (admin)
func (h *WebHandlers) AdminJobs(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil || !user.IsAdmin {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	status := r.URL.Query().Get("status")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	limit := 50
	offset := (page - 1) * limit

	// List all jobs (admin view)
	jobsList, total, err := h.jobsService.List("", status, limit, offset)
	if err != nil {
		SetFlash(w, "error", "Failed to load jobs")
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	totalPages := (total + limit - 1) / limit
	start := offset + 1
	end := offset + len(jobsList)
	if end > total {
		end = total
	}

	h.renderer.Render(w, r, "admin/jobs.html", map[string]interface{}{
		"Title": "All Jobs",
		"Jobs":  jobsList,
		"Filter": map[string]string{
			"Status": status,
		},
		"Pagination": map[string]interface{}{
			"Page":       page,
			"Total":      total,
			"TotalPages": totalPages,
			"Start":      start,
			"End":        end,
		},
	})
}

// AdminWorkers shows all workers
func (h *WebHandlers) AdminWorkers(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil || !user.IsAdmin {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	workersList, err := h.workerService.List()
	if err != nil {
		SetFlash(w, "error", "Failed to load workers")
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	h.renderer.Render(w, r, "admin/workers.html", map[string]interface{}{
		"Title":   "Workers",
		"Workers": workersList,
	})
}

// AdminUsers shows all users
func (h *WebHandlers) AdminUsers(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil || !user.IsAdmin {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// Get all users with job counts
	type UserWithStats struct {
		auth.User
		JobCount int
	}

	// For now, just show empty list - will need DB access to implement properly
	// TODO: Implement proper user listing with job counts
	h.renderer.Render(w, r, "admin/users.html", map[string]interface{}{
		"Title": "Users",
		"Users": []UserWithStats{},
	})
}

// Helper functions

func (h *WebHandlers) getUserStats(userID string) map[string]interface{} {
	stats := make(map[string]interface{})

	// Total jobs
	_, total, _ := h.jobsService.List(userID, "", 1, 0)
	stats["TotalJobs"] = total

	// Queued jobs
	_, queued, _ := h.jobsService.List(userID, jobs.StatusQueued, 1, 0)
	stats["QueuedJobs"] = queued

	// Running jobs
	_, running, _ := h.jobsService.List(userID, jobs.StatusRunning, 1, 0)
	stats["RunningJobs"] = running

	// Succeeded jobs
	_, succeeded, _ := h.jobsService.List(userID, jobs.StatusSucceeded, 1, 0)
	stats["SucceededJobs"] = succeeded

	return stats
}

func (h *WebHandlers) getAdminStats() map[string]interface{} {
	stats := make(map[string]interface{})

	// Total jobs across all users (pass empty userID)
	_, total, _ := h.jobsService.List("", "", 1, 0)
	stats["TotalJobs"] = total

	// Jobs by status
	_, queued, _ := h.jobsService.List("", jobs.StatusQueued, 1, 0)
	stats["QueuedJobs"] = queued

	_, running, _ := h.jobsService.List("", jobs.StatusRunning, 1, 0)
	stats["RunningJobs"] = running

	_, succeeded, _ := h.jobsService.List("", jobs.StatusSucceeded, 1, 0)
	stats["SucceededJobs"] = succeeded

	_, failed, _ := h.jobsService.List("", jobs.StatusFailed, 1, 0)
	stats["FailedJobs"] = failed

	_, deadLetter, _ := h.jobsService.List("", jobs.StatusDeadLetter, 1, 0)
	stats["DeadLetterJobs"] = deadLetter

	// Worker stats
	workersList, _ := h.workerService.List()
	stats["ActiveWorkers"] = len(workersList)

	// Default values
	stats["TotalUsers"] = 0
	stats["JobsToday"] = 0

	return stats
}

func extractIDFromPath(path, prefix string) string {
	id := strings.TrimPrefix(path, prefix)
	parts := strings.Split(id, "/")
	return parts[0]
}
