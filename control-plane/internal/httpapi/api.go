package httpapi

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/processor/control-plane/internal/auth"
	"github.com/processor/control-plane/internal/email"
	"github.com/processor/control-plane/internal/jobs"
	"github.com/processor/control-plane/internal/middleware"
	"github.com/processor/control-plane/internal/webhooks"
	"github.com/processor/control-plane/internal/workers"
)

// API handles HTTP API requests
type API struct {
	authService    *auth.Service
	jobsService    *jobs.Service
	webhookService *webhooks.Service
	emailService   *email.Service
	workerService  *workers.Service
	workerToken    string
}

// NewAPI creates a new API handler
func NewAPI(
	authService *auth.Service,
	jobsService *jobs.Service,
	webhookService *webhooks.Service,
	emailService *email.Service,
	workerService *workers.Service,
	workerToken string,
) *API {
	return &API{
		authService:    authService,
		jobsService:    jobsService,
		webhookService: webhookService,
		emailService:   emailService,
		workerService:  workerService,
		workerToken:    workerToken,
	}
}

// Health handles health check requests
func (api *API) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"status":    "healthy",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
			"version":   "1.0.0",
		},
	})
}

// Me returns current user information
func (api *API) Me(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"id":         user.ID,
			"email":      user.Email,
			"plan_tier":  user.PlanTier,
			"is_admin":   user.IsAdmin,
			"created_at": user.CreatedAt.Format(time.RFC3339),
		},
	})
}

// CreateJob creates a new job
func (api *API) CreateJob(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
		return
	}

	var req struct {
		JobType        string                 `json:"job_type"`
		InputMetadata  map[string]interface{} `json:"input_metadata"`
		PriorityWeight int                    `json:"priority_weight"`
		MaxAttempts    int                    `json:"max_attempts"`
		NotifyEmail    bool                   `json:"notify_email"`
		NotifyWebhook  bool                   `json:"notify_webhook"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "Invalid JSON")
		return
	}

	if req.JobType == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "Job type is required")
		return
	}

	if req.InputMetadata == nil {
		req.InputMetadata = make(map[string]interface{})
	}

	if req.MaxAttempts == 0 {
		req.MaxAttempts = 3
	}

	job, err := api.jobsService.Create(
		user.ID,
		req.JobType,
		req.InputMetadata,
		req.PriorityWeight,
		req.MaxAttempts,
		req.NotifyEmail,
		req.NotifyWebhook,
	)

	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to create job")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    formatJob(job),
	})
}

// ListJobs lists jobs for the current user
func (api *API) ListJobs(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
		return
	}

	status := r.URL.Query().Get("status")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	if limit == 0 || limit > 100 {
		limit = 50
	}

	jobsList, total, err := api.jobsService.List(user.ID, status, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to list jobs")
		return
	}

	jobs := make([]map[string]interface{}, len(jobsList))
	for i, job := range jobsList {
		jobs[i] = formatJob(&job)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"jobs":   jobs,
			"total":  total,
			"limit":  limit,
			"offset": offset,
		},
	})
}

// GetJob gets a specific job
func (api *API) GetJob(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
		return
	}

	jobID := extractID(r.URL.Path, "/api/v1/jobs/")

	job, err := api.jobsService.Get(jobID)
	if err != nil {
		if err == jobs.ErrJobNotFound {
			writeError(w, http.StatusNotFound, "not_found", "Job not found")
		} else {
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to get job")
		}
		return
	}

	if job.UserID != user.ID && !user.IsAdmin {
		writeError(w, http.StatusForbidden, "forbidden", "Access denied")
		return
	}

	attempts, _ := api.jobsService.GetAttempts(jobID)
	attemptsList := make([]map[string]interface{}, len(attempts))
	for i, att := range attempts {
		attemptsList[i] = map[string]interface{}{
			"attempt_number": att.AttemptNumber,
			"worker_id":      att.WorkerID,
			"started_at":     att.StartedAt.Format(time.RFC3339),
			"finished_at":    formatNullTime(att.FinishedAt),
			"status":         att.Status,
			"error_text":     att.ErrorText,
		}
	}

	data := formatJob(job)
	data["attempts"] = attemptsList

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    data,
	})
}

// CreateAPIKey creates a new API key
func (api *API) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
		return
	}

	var req struct {
		Name string `json:"name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "Invalid JSON")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "Name is required")
		return
	}

	fullKey, apiKey, err := api.authService.CreateAPIKey(user.ID, req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to create API key")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"id":         apiKey.ID,
			"name":       apiKey.Name,
			"key":        fullKey,
			"key_prefix": apiKey.KeyPrefix,
			"created_at": apiKey.CreatedAt.Format(time.RFC3339),
		},
	})
}

// ListAPIKeys lists API keys for the current user
func (api *API) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
		return
	}

	keys, err := api.authService.ListAPIKeys(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to list API keys")
		return
	}

	apiKeys := make([]map[string]interface{}, len(keys))
	for i, key := range keys {
		apiKeys[i] = map[string]interface{}{
			"id":         key.ID,
			"name":       key.Name,
			"key_prefix": key.KeyPrefix,
			"created_at": key.CreatedAt.Format(time.RFC3339),
			"revoked_at": formatNullTime(key.RevokedAt),
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"api_keys": apiKeys,
		},
	})
}

// RevokeAPIKey revokes an API key
func (api *API) RevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
		return
	}

	keyID := extractID(r.URL.Path, "/api/v1/api-keys/")
	keyID = strings.TrimSuffix(keyID, "/revoke")

	err := api.authService.RevokeAPIKey(keyID, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to revoke API key")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"revoked": true,
		},
	})
}

// CreateWebhook creates a new webhook
func (api *API) CreateWebhook(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
		return
	}

	var req struct {
		TargetURL        string   `json:"target_url"`
		Secret           string   `json:"secret"`
		SubscribedEvents []string `json:"subscribed_events"`
		IsActive         bool     `json:"is_active"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "Invalid JSON")
		return
	}

	if req.TargetURL == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "Target URL is required")
		return
	}

	if req.Secret == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "Secret is required")
		return
	}

	if len(req.SubscribedEvents) == 0 {
		req.SubscribedEvents = []string{"job.succeeded", "job.failed", "job.dead_letter"}
	}

	webhook, err := api.webhookService.Create(user.ID, req.TargetURL, req.Secret, req.SubscribedEvents, req.IsActive)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to create webhook")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"id":                webhook.ID,
			"user_id":           webhook.UserID,
			"target_url":        webhook.TargetURL,
			"is_active":         webhook.IsActive,
			"subscribed_events": webhook.SubscribedEvents,
			"created_at":        webhook.CreatedAt.Format(time.RFC3339),
		},
	})
}

// ListWebhooks lists webhooks for the current user
func (api *API) ListWebhooks(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
		return
	}

	webhooksList, err := api.webhookService.List(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to list webhooks")
		return
	}

	webhooks := make([]map[string]interface{}, len(webhooksList))
	for i, wh := range webhooksList {
		webhooks[i] = map[string]interface{}{
			"id":                wh.ID,
			"target_url":        wh.TargetURL,
			"is_active":         wh.IsActive,
			"subscribed_events": wh.SubscribedEvents,
			"created_at":        wh.CreatedAt.Format(time.RFC3339),
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"webhooks": webhooks,
		},
	})
}

// DeleteWebhook deletes a webhook
func (api *API) DeleteWebhook(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
		return
	}

	webhookID := extractID(r.URL.Path, "/api/v1/webhooks/")

	err := api.webhookService.Delete(webhookID, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to delete webhook")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"deleted": true,
		},
	})
}

// Worker API Endpoints

// WorkerPoll handles worker polling for jobs
func (api *API) WorkerPoll(w http.ResponseWriter, r *http.Request) {
	if !api.validateWorkerToken(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Invalid worker token")
		return
	}

	var req struct {
		WorkerID     string   `json:"worker_id"`
		Capabilities []string `json:"capabilities"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "Invalid JSON")
		return
	}

	// Auto-register worker if it doesn't exist
	if err := api.workerService.EnsureWorker(req.WorkerID, req.Capabilities); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to register worker")
		return
	}

	api.workerService.UpdateLastSeen(req.WorkerID)

	job, err := api.jobsService.Poll(req.WorkerID, req.Capabilities)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to poll for jobs")
		return
	}

	if job == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"data": map[string]interface{}{
				"job": nil,
			},
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"job": map[string]interface{}{
				"id":                job.ID,
				"job_type":          job.JobType,
				"input_metadata":    job.InputMetadata,
				"max_attempts":      job.MaxAttempts,
				"attempt_count":     job.AttemptCount,
				"lease_expires_at":  formatNullTime(job.LeaseExpiresAt),
			},
		},
	})
}

// WorkerHeartbeat handles worker heartbeats
func (api *API) WorkerHeartbeat(w http.ResponseWriter, r *http.Request) {
	if !api.validateWorkerToken(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Invalid worker token")
		return
	}

	var req struct {
		WorkerID string `json:"worker_id"`
		JobID    string `json:"job_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "Invalid JSON")
		return
	}

	api.workerService.UpdateLastSeen(req.WorkerID)

	leaseExpiresAt, err := api.jobsService.Heartbeat(req.JobID, req.WorkerID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"lease_extended":   true,
			"lease_expires_at": leaseExpiresAt.Format(time.RFC3339),
		},
	})
}

// WorkerStartJob marks a job as running
func (api *API) WorkerStartJob(w http.ResponseWriter, r *http.Request) {
	if !api.validateWorkerToken(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Invalid worker token")
		return
	}

	jobID := extractID(r.URL.Path, "/api/v1/worker/jobs/")
	jobID = strings.TrimSuffix(jobID, "/start")

	var req struct {
		WorkerID string `json:"worker_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "Invalid JSON")
		return
	}

	api.workerService.UpdateCurrentJob(req.WorkerID, sql.NullString{String: jobID, Valid: true})

	err := api.jobsService.MarkRunning(jobID, req.WorkerID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"status":     jobs.StatusRunning,
			"started_at": time.Now().Format(time.RFC3339),
		},
	})
}

// WorkerCompleteJob marks a job as completed
func (api *API) WorkerCompleteJob(w http.ResponseWriter, r *http.Request) {
	if !api.validateWorkerToken(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Invalid worker token")
		return
	}

	jobID := extractID(r.URL.Path, "/api/v1/worker/jobs/")
	jobID = strings.TrimSuffix(jobID, "/complete")

	var req struct {
		WorkerID       string                 `json:"worker_id"`
		ResultMetadata map[string]interface{} `json:"result_metadata"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "Invalid JSON")
		return
	}

	err := api.jobsService.Complete(jobID, req.WorkerID, req.ResultMetadata)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	api.workerService.UpdateCurrentJob(req.WorkerID, sql.NullString{})

	// Trigger notifications
	go api.sendNotifications(jobID, "job.succeeded")

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"status":       jobs.StatusSucceeded,
			"completed_at": time.Now().Format(time.RFC3339),
		},
	})
}

// WorkerFailJob marks a job as failed
func (api *API) WorkerFailJob(w http.ResponseWriter, r *http.Request) {
	if !api.validateWorkerToken(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Invalid worker token")
		return
	}

	jobID := extractID(r.URL.Path, "/api/v1/worker/jobs/")
	jobID = strings.TrimSuffix(jobID, "/fail")

	var req struct {
		WorkerID    string `json:"worker_id"`
		ErrorText   string `json:"error_text"`
		ShouldRetry bool   `json:"should_retry"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "Invalid JSON")
		return
	}

	err := api.jobsService.Fail(jobID, req.WorkerID, req.ErrorText, req.ShouldRetry)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	api.workerService.UpdateCurrentJob(req.WorkerID, sql.NullString{})

	// Get job to check final status
	job, _ := api.jobsService.Get(jobID)
	if job != nil {
		if job.Status == jobs.StatusDeadLetter {
			go api.sendNotifications(jobID, "job.dead_letter")
		} else if job.Status == jobs.StatusFailed {
			go api.sendNotifications(jobID, "job.failed")
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"status": "failed",
		},
	})
}

// Helper functions

func (api *API) validateWorkerToken(r *http.Request) bool {
	token := r.Header.Get("X-Worker-Token")
	return token == api.workerToken
}

func (api *API) sendNotifications(jobID, eventType string) {
	job, err := api.jobsService.Get(jobID)
	if err != nil {
		return
	}

	// Send email if requested
	if job.NotifyEmail {
		templateName := "job_succeeded"
		if eventType == "job.failed" {
			templateName = "job_failed"
		} else if eventType == "job.dead_letter" {
			templateName = "job_dead_letter"
		}

		payload := map[string]interface{}{
			"job_id":   job.ID,
			"job_type": job.JobType,
			"status":   job.Status,
		}
		if job.ErrorText.Valid && job.ErrorText.String != "" {
			payload["error"] = job.ErrorText.String
		}

		api.emailService.Queue(job.UserID, sql.NullString{String: job.ID, Valid: true}, templateName, payload)
	}

	// Send webhooks if requested
	if job.NotifyWebhook {
		webhooks, err := api.webhookService.FindActiveWebhooksForEvent(job.UserID, eventType)
		if err == nil {
			payload := map[string]interface{}{
				"event":     eventType,
				"timestamp": time.Now().UTC().Format(time.RFC3339),
				"data": map[string]interface{}{
					"job": formatJob(job),
				},
			}

			for _, webhook := range webhooks {
				api.webhookService.QueueDelivery(webhook.ID, eventType, sql.NullString{String: job.ID, Valid: true}, payload)
			}
		}
	}
}

func formatJob(job *jobs.Job) map[string]interface{} {
	return map[string]interface{}{
		"id":                job.ID,
		"user_id":           job.UserID,
		"job_type":          job.JobType,
		"status":            job.Status,
		"priority_weight":   job.PriorityWeight,
		"input_metadata":    job.InputMetadata,
		"result_metadata":   job.ResultMetadata,
		"error_text":        formatNullString(job.ErrorText),
		"created_at":        job.CreatedAt.Format(time.RFC3339),
		"updated_at":        job.UpdatedAt.Format(time.RFC3339),
		"queued_at":         formatNullTime(job.QueuedAt),
		"started_at":        formatNullTime(job.StartedAt),
		"completed_at":      formatNullTime(job.CompletedAt),
		"worker_id":         formatNullString(job.WorkerID),
		"attempt_count":     job.AttemptCount,
		"max_attempts":      job.MaxAttempts,
	}
}

func formatNullTime(nt sql.NullTime) interface{} {
	if nt.Valid {
		return nt.Time.Format(time.RFC3339)
	}
	return nil
}

func formatNullString(ns sql.NullString) interface{} {
	if ns.Valid {
		return ns.String
	}
	return nil
}

func extractID(path, prefix string) string {
	id := strings.TrimPrefix(path, prefix)
	parts := strings.Split(id, "/")
	return parts[0]
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]interface{}{
		"success": false,
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}
