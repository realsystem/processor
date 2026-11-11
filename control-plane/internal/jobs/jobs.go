package jobs

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/processor/control-plane/internal/db"
)

const (
	StatusQueued     = "queued"
	StatusLeased     = "leased"
	StatusRunning    = "running"
	StatusSucceeded  = "succeeded"
	StatusFailed     = "failed"
	StatusDeadLetter = "dead_letter"
	StatusCanceled   = "canceled"
)

var (
	ErrJobNotFound = errors.New("job not found")
	ErrNotLeased   = errors.New("job is not leased by this worker")
)

// Job represents a job in the queue
type Job struct {
	ID             string
	UserID         string
	JobType        string
	Status         string
	PriorityWeight int
	InputMetadata  map[string]interface{}
	ResultMetadata map[string]interface{}
	ErrorText      sql.NullString
	CreatedAt      time.Time
	UpdatedAt      time.Time
	QueuedAt       sql.NullTime
	LeasedAt       sql.NullTime
	LeaseExpiresAt sql.NullTime
	StartedAt      sql.NullTime
	CompletedAt    sql.NullTime
	WorkerID       sql.NullString
	AttemptCount   int
	MaxAttempts    int
	NotifyEmail    bool
	NotifyWebhook  bool

	// optional features (see options.go)
	IdempotencyKey    sql.NullString
	TraceParent       sql.NullString
	CorrelationID     sql.NullString
	Sensitive         bool
	RetentionSeconds  sql.NullInt64
	PurgedAt          sql.NullTime
	CancelRequestedAt sql.NullTime
}

// JobAttempt represents an execution attempt
type JobAttempt struct {
	ID            string
	JobID         string
	WorkerID      sql.NullString
	AttemptNumber int
	StartedAt     time.Time
	FinishedAt    sql.NullTime
	Status        string
	ErrorText     sql.NullString
}

// Service handles job operations
type Service struct {
	db            *sql.DB
	leaseDuration time.Duration
}

// NewService creates a new job service
func NewService(database *sql.DB, leaseDuration time.Duration) *Service {
	return &Service{
		db:            database,
		leaseDuration: leaseDuration,
	}
}

// Create creates a new job (see CreateWithOptions for idempotency, tracing and retention)
func (s *Service) Create(userID, jobType string, inputMetadata map[string]interface{}, priorityWeight, maxAttempts int, notifyEmail, notifyWebhook bool) (*Job, error) {
	job, _, err := s.CreateWithOptions(userID, jobType, inputMetadata, CreateOptions{
		PriorityWeight: priorityWeight,
		MaxAttempts:    maxAttempts,
		NotifyEmail:    notifyEmail,
		NotifyWebhook:  notifyWebhook,
	})
	return job, err
}

// Poll polls for the next available job and leases it
func (s *Service) Poll(workerID string, capabilities []string) (*Job, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Build capabilities filter
	capFilter := ""
	if len(capabilities) > 0 {
		capFilter = " AND job_type IN ("
		for i := range capabilities {
			if i > 0 {
				capFilter += ", "
			}
			capFilter += "?"
		}
		capFilter += ")"
	}

	// Find next available job
	query := `
		SELECT id, user_id, job_type, status, priority_weight, input_metadata_json,
			   created_at, updated_at, queued_at, attempt_count, max_attempts,
			   trace_parent, correlation_id, sensitive
		FROM jobs
		WHERE (status = ? OR (status = ? AND lease_expires_at < ?))` + capFilter + `
		ORDER BY priority_weight DESC, created_at ASC
		LIMIT 1
	`

	args := []interface{}{StatusQueued, StatusLeased, time.Now()}
	for _, cap := range capabilities {
		args = append(args, cap)
	}

	var job Job
	var inputJSON string

	err = tx.QueryRow(query, args...).Scan(
		&job.ID, &job.UserID, &job.JobType, &job.Status, &job.PriorityWeight, &inputJSON,
		&job.CreatedAt, &job.UpdatedAt, &job.QueuedAt, &job.AttemptCount, &job.MaxAttempts,
		&job.TraceParent, &job.CorrelationID, &job.Sensitive,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // No jobs available
		}
		return nil, err
	}

	// Parse input metadata
	if err := json.Unmarshal([]byte(inputJSON), &job.InputMetadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal input metadata: %w", err)
	}

	// Lease the job
	leaseExpiresAt := time.Now().Add(s.leaseDuration)
	updateQuery := `
		UPDATE jobs
		SET status = ?, leased_at = ?, lease_expires_at = ?, worker_id = ?, updated_at = ?
		WHERE id = ?
	`

	_, err = tx.Exec(updateQuery, StatusLeased, time.Now(), leaseExpiresAt, workerID, time.Now(), job.ID)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	job.Status = StatusLeased
	job.LeasedAt = sql.NullTime{Time: time.Now(), Valid: true}
	job.LeaseExpiresAt = sql.NullTime{Time: leaseExpiresAt, Valid: true}
	job.WorkerID = sql.NullString{String: workerID, Valid: true}

	return &job, nil
}

// Heartbeat extends the lease on a job
func (s *Service) Heartbeat(jobID, workerID string) (time.Time, error) {
	if err := s.settleCancel(jobID, workerID); err != nil {
		return time.Time{}, err
	}
	leaseExpiresAt := time.Now().Add(s.leaseDuration)

	query := `
		UPDATE jobs
		SET lease_expires_at = ?, updated_at = ?
		WHERE id = ? AND worker_id = ? AND status IN (?, ?)
	`

	result, err := s.db.Exec(query, leaseExpiresAt, time.Now(), jobID, workerID, StatusLeased, StatusRunning)
	if err != nil {
		return time.Time{}, err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return time.Time{}, err
	}

	if rows == 0 {
		return time.Time{}, ErrNotLeased
	}

	return leaseExpiresAt, nil
}

// MarkRunning marks a job as running
func (s *Service) MarkRunning(jobID, workerID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Update job status
	query := `
		UPDATE jobs
		SET status = ?, started_at = ?, updated_at = ?
		WHERE id = ? AND worker_id = ? AND status = ?
	`

	result, err := tx.Exec(query, StatusRunning, time.Now(), time.Now(), jobID, workerID, StatusLeased)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rows == 0 {
		return ErrNotLeased
	}

	// Get attempt count
	var attemptCount int
	err = tx.QueryRow("SELECT attempt_count FROM jobs WHERE id = ?", jobID).Scan(&attemptCount)
	if err != nil {
		return err
	}

	// Create attempt record
	attemptQuery := `
		INSERT INTO job_attempts (id, job_id, worker_id, attempt_number, started_at, status)
		VALUES (?, ?, ?, ?, ?, ?)
	`

	_, err = tx.Exec(attemptQuery,
		db.GenerateID("att"), jobID, workerID, attemptCount+1, time.Now(), StatusRunning,
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// Complete marks a job as successfully completed
func (s *Service) Complete(jobID, workerID string, resultMetadata map[string]interface{}) error {
	resultJSON, err := json.Marshal(resultMetadata)
	if err != nil {
		return fmt.Errorf("failed to marshal result metadata: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Update job
	query := `
		UPDATE jobs
		SET status = ?, completed_at = ?, updated_at = ?, result_metadata_json = ?
		WHERE id = ? AND worker_id = ? AND status = ?
	`

	result, err := tx.Exec(query, StatusSucceeded, time.Now(), time.Now(), string(resultJSON), jobID, workerID, StatusRunning)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rows == 0 {
		return ErrNotLeased
	}

	// Update attempt record
	attemptQuery := `
		UPDATE job_attempts
		SET status = ?, finished_at = ?
		WHERE job_id = ? AND status = ?
	`

	_, err = tx.Exec(attemptQuery, StatusSucceeded, time.Now(), jobID, StatusRunning)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// Fail marks a job as failed and potentially requeues it
func (s *Service) Fail(jobID, workerID, errorText string, shouldRetry bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Get current job state
	var attemptCount, maxAttempts int
	err = tx.QueryRow("SELECT attempt_count, max_attempts FROM jobs WHERE id = ?", jobID).Scan(&attemptCount, &maxAttempts)
	if err != nil {
		return err
	}

	// Increment attempt count
	newAttemptCount := attemptCount + 1

	// Determine next status
	var nextStatus string
	var completedAt sql.NullTime

	if shouldRetry && newAttemptCount < maxAttempts {
		// Requeue for retry
		nextStatus = StatusQueued
	} else if newAttemptCount >= maxAttempts {
		// Move to dead letter
		nextStatus = StatusDeadLetter
		completedAt = sql.NullTime{Time: time.Now(), Valid: true}
	} else {
		// Failed, no retry
		nextStatus = StatusFailed
		completedAt = sql.NullTime{Time: time.Now(), Valid: true}
	}

	// Update job
	query := `
		UPDATE jobs
		SET status = ?, error_text = ?, attempt_count = ?, completed_at = ?, updated_at = ?,
		    leased_at = NULL, lease_expires_at = NULL, worker_id = NULL
		WHERE id = ? AND worker_id = ?
	`

	errorTextNull := sql.NullString{String: errorText, Valid: errorText != ""}
	result, err := tx.Exec(query, nextStatus, errorTextNull, newAttemptCount, completedAt, time.Now(), jobID, workerID)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rows == 0 {
		return ErrNotLeased
	}

	// Update attempt record
	attemptQuery := `
		UPDATE job_attempts
		SET status = ?, error_text = ?, finished_at = ?
		WHERE job_id = ? AND status = ?
	`

	errorTextNullAttempt := sql.NullString{String: errorText, Valid: errorText != ""}
	_, err = tx.Exec(attemptQuery, StatusFailed, errorTextNullAttempt, time.Now(), jobID, StatusRunning)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// Get retrieves a job by ID
func (s *Service) Get(jobID string) (*Job, error) {
	query := `
		SELECT id, user_id, job_type, status, priority_weight, input_metadata_json,
		       result_metadata_json, error_text, created_at, updated_at, queued_at,
		       leased_at, lease_expires_at, started_at, completed_at, worker_id,
		       attempt_count, max_attempts, notify_email, notify_webhook,
		       idempotency_key, trace_parent, correlation_id, sensitive, retention_seconds,
		       purged_at, cancel_requested_at
		FROM jobs
		WHERE id = ?
	`

	var job Job
	var inputJSON, resultJSON sql.NullString

	err := s.db.QueryRow(query, jobID).Scan(
		&job.ID, &job.UserID, &job.JobType, &job.Status, &job.PriorityWeight, &inputJSON,
		&resultJSON, &job.ErrorText, &job.CreatedAt, &job.UpdatedAt, &job.QueuedAt,
		&job.LeasedAt, &job.LeaseExpiresAt, &job.StartedAt, &job.CompletedAt, &job.WorkerID,
		&job.AttemptCount, &job.MaxAttempts, &job.NotifyEmail, &job.NotifyWebhook,
		&job.IdempotencyKey, &job.TraceParent, &job.CorrelationID, &job.Sensitive, &job.RetentionSeconds,
		&job.PurgedAt, &job.CancelRequestedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrJobNotFound
		}
		return nil, err
	}

	// Parse JSON fields
	if inputJSON.Valid {
		if err := json.Unmarshal([]byte(inputJSON.String), &job.InputMetadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal input metadata: %w", err)
		}
	}

	if resultJSON.Valid {
		if err := json.Unmarshal([]byte(resultJSON.String), &job.ResultMetadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal result metadata: %w", err)
		}
	}

	return &job, nil
}

// List retrieves jobs for a user with filters
func (s *Service) List(userID string, status string, limit, offset int) ([]Job, int, error) {
	// Build query
	query := `
		SELECT id, user_id, job_type, status, priority_weight, created_at,
		       updated_at, started_at, completed_at, attempt_count, worker_id
		FROM jobs
	`
	countQuery := "SELECT COUNT(*) FROM jobs"
	args := []interface{}{}
	countArgs := []interface{}{}

	// Add user filter if provided (empty userID means all jobs for admin)
	if userID != "" {
		query += " WHERE user_id = ?"
		countQuery += " WHERE user_id = ?"
		args = append(args, userID)
		countArgs = append(countArgs, userID)
	} else {
		query += " WHERE 1=1"
		countQuery += " WHERE 1=1"
	}

	if status != "" {
		query += " AND status = ?"
		countQuery += " AND status = ?"
		args = append(args, status)
		countArgs = append(countArgs, status)
	}

	// Get total count
	var total int
	err := s.db.QueryRow(countQuery, countArgs...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// Get jobs
	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var jobs []Job
	for rows.Next() {
		var job Job
		err := rows.Scan(
			&job.ID, &job.UserID, &job.JobType, &job.Status, &job.PriorityWeight,
			&job.CreatedAt, &job.UpdatedAt, &job.StartedAt, &job.CompletedAt, &job.AttemptCount, &job.WorkerID,
		)
		if err != nil {
			return nil, 0, err
		}
		jobs = append(jobs, job)
	}

	return jobs, total, nil
}

// GetAttempts retrieves all attempts for a job
func (s *Service) GetAttempts(jobID string) ([]JobAttempt, error) {
	query := `
		SELECT id, job_id, worker_id, attempt_number, started_at, finished_at, status, error_text
		FROM job_attempts
		WHERE job_id = ?
		ORDER BY attempt_number ASC
	`

	rows, err := s.db.Query(query, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attempts []JobAttempt
	for rows.Next() {
		var attempt JobAttempt
		err := rows.Scan(
			&attempt.ID, &attempt.JobID, &attempt.WorkerID, &attempt.AttemptNumber,
			&attempt.StartedAt, &attempt.FinishedAt, &attempt.Status, &attempt.ErrorText,
		)
		if err != nil {
			return nil, err
		}
		attempts = append(attempts, attempt)
	}

	return attempts, nil
}

// ReleaseExpiredLeases releases jobs with expired leases: leased jobs go back to the
// queue; running jobs whose worker stopped heartbeating count as a failed attempt and are
// requeued, or dead-lettered when they have no attempts left.
func (s *Service) ReleaseExpiredLeases() (int, error) {
	now := time.Now()
	result, err := s.db.Exec(`
		UPDATE jobs
		SET status = ?, leased_at = NULL, lease_expires_at = NULL, worker_id = NULL, updated_at = ?
		WHERE status = ? AND lease_expires_at < ?
	`, StatusQueued, now, StatusLeased, now)
	if err != nil {
		return 0, err
	}
	released, _ := result.RowsAffected()

	rows, err := s.db.Query("SELECT id, attempt_count, max_attempts FROM jobs WHERE status = ? AND lease_expires_at < ?",
		StatusRunning, now)
	if err != nil {
		return int(released), err
	}
	type lost struct {
		id            string
		attempts, max int
	}
	var stuck []lost
	for rows.Next() {
		var l lost
		if err := rows.Scan(&l.id, &l.attempts, &l.max); err != nil {
			rows.Close()
			return int(released), err
		}
		stuck = append(stuck, l)
	}
	rows.Close()
	for _, l := range stuck {
		next := StatusQueued
		var completed sql.NullTime
		if l.attempts+1 >= l.max {
			next = StatusDeadLetter
			completed = sql.NullTime{Time: now, Valid: true}
		}
		res, err := s.db.Exec(`
			UPDATE jobs SET status = ?, attempt_count = attempt_count + 1, error_text = ?, completed_at = ?,
				leased_at = NULL, lease_expires_at = NULL, worker_id = NULL, updated_at = ?
			WHERE id = ? AND status = ? AND lease_expires_at < ?`,
			next, "worker stopped sending heartbeats", completed, now, l.id, StatusRunning, now)
		if err != nil {
			return int(released), err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			released++
			s.db.Exec("UPDATE job_attempts SET status = ?, finished_at = ?, error_text = ? WHERE job_id = ? AND status = ?",
				StatusFailed, now, "lease expired", l.id, StatusRunning)
		}
	}
	return int(released), nil
}
