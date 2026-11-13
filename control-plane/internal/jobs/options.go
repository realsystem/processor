package jobs

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/processor/control-plane/internal/db"
)

// Optional job features. All are off unless the caller asks for them, so existing
// clients behave exactly as before.
//
//   - IdempotencyKey: a second submission with the same key (per user) returns the
//     first job instead of creating another one.
//   - TraceParent / CorrelationID: W3C trace context and a correlation id, stored with
//     the job and handed to the worker, so a job shows up in the caller's trace.
//   - Sensitive: input and result are never sent in webhooks or e-mails and the worker
//     does not log them. Sensitive jobs are purged after RetentionSeconds (default
//     DefaultSensitiveRetention).
//   - RetentionSeconds: after the job finishes, its input and result are erased
//     (purged_at is set); the job row and its status stay.
//   - Cancel: queued jobs are cancelled at once; running jobs at the worker's next
//     heartbeat.

var (
	ErrIdempotencyConflict = errors.New("idempotency key already used for a different job")
	ErrNotCancelable       = errors.New("job already finished")
	ErrCanceled            = errors.New("job was canceled")
)

// DefaultSensitiveRetention applies to sensitive jobs submitted without a retention.
const DefaultSensitiveRetention = 24 * time.Hour

// CreateOptions are the per-job settings for CreateWithOptions.
type CreateOptions struct {
	PriorityWeight   int
	MaxAttempts      int
	NotifyEmail      bool
	NotifyWebhook    bool
	IdempotencyKey   string
	TraceParent      string
	CorrelationID    string
	Sensitive        bool
	RetentionSeconds int // 0: keep forever (unless Sensitive)

	// JobTypeVersion is the registered version the job was submitted under (see package
	// jobtypes); empty for the untyped contract.
	JobTypeVersion string
}

// CreateWithOptions creates a job. With an idempotency key that was already used by this
// user, it returns the existing job and existing=true (or ErrIdempotencyConflict if the
// job type differs).
func (s *Service) CreateWithOptions(userID, jobType string, input map[string]interface{}, o CreateOptions) (job *Job, existing bool, err error) {
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = 3
	}
	if o.Sensitive && o.RetentionSeconds <= 0 {
		o.RetentionSeconds = int(DefaultSensitiveRetention / time.Second)
	}
	if o.IdempotencyKey != "" {
		if prev, err := s.byIdempotencyKey(userID, o.IdempotencyKey); err == nil {
			if prev.JobType != jobType || versionOf(prev) != versionOrUntyped(o.JobTypeVersion) {
				return nil, true, ErrIdempotencyConflict
			}
			return prev, true, nil
		} else if !errors.Is(err, ErrJobNotFound) {
			return nil, false, err
		}
	}
	if input == nil {
		input = map[string]interface{}{}
	}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, false, fmt.Errorf("failed to marshal input metadata: %w", err)
	}
	now := time.Now()
	job = &Job{
		ID:               db.GenerateID("job"),
		UserID:           userID,
		JobType:          jobType,
		Status:           StatusQueued,
		PriorityWeight:   o.PriorityWeight,
		InputMetadata:    input,
		CreatedAt:        now,
		UpdatedAt:        now,
		QueuedAt:         sql.NullTime{Time: now, Valid: true},
		MaxAttempts:      o.MaxAttempts,
		NotifyEmail:      o.NotifyEmail,
		NotifyWebhook:    o.NotifyWebhook,
		JobTypeVersion:   nullString(o.JobTypeVersion),
		IdempotencyKey:   nullString(o.IdempotencyKey),
		TraceParent:      nullString(o.TraceParent),
		CorrelationID:    nullString(o.CorrelationID),
		Sensitive:        o.Sensitive,
		RetentionSeconds: sql.NullInt64{Int64: int64(o.RetentionSeconds), Valid: o.RetentionSeconds > 0},
	}
	_, err = s.db.Exec(`
		INSERT INTO jobs (id, user_id, job_type, status, priority_weight, input_metadata_json,
			created_at, updated_at, queued_at, attempt_count, max_attempts, notify_email, notify_webhook,
			idempotency_key, trace_parent, correlation_id, sensitive, retention_seconds, job_type_version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.UserID, job.JobType, job.Status, job.PriorityWeight, string(inputJSON),
		job.CreatedAt, job.UpdatedAt, job.QueuedAt, job.MaxAttempts, job.NotifyEmail, job.NotifyWebhook,
		job.IdempotencyKey, job.TraceParent, job.CorrelationID, job.Sensitive, job.RetentionSeconds, job.JobTypeVersion,
	)
	if err != nil {
		if o.IdempotencyKey != "" {
			// lost a race with an identical submission: return the winner
			if prev, perr := s.byIdempotencyKey(userID, o.IdempotencyKey); perr == nil && prev.JobType == jobType && versionOf(prev) == versionOrUntyped(o.JobTypeVersion) {
				return prev, true, nil
			}
		}
		return nil, false, fmt.Errorf("failed to create job: %w", err)
	}
	return job, false, nil
}

func (s *Service) byIdempotencyKey(userID, key string) (*Job, error) {
	var id string
	err := s.db.QueryRow("SELECT id FROM jobs WHERE user_id = ? AND idempotency_key = ?", userID, key).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, ErrJobNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.Get(id)
}

// Cancel cancels a job: a queued or leased job at once, a running job at its worker's
// next heartbeat (the worker then stops it). Finished jobs return ErrNotCancelable.
func (s *Service) Cancel(jobID string) (*Job, error) {
	job, err := s.Get(jobID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	switch job.Status {
	case StatusQueued, StatusLeased:
		res, err := s.db.Exec(`
			UPDATE jobs SET status = ?, completed_at = ?, updated_at = ?, cancel_requested_at = ?,
				leased_at = NULL, lease_expires_at = NULL, worker_id = NULL
			WHERE id = ? AND status = ?`, StatusCanceled, now, now, now, jobID, job.Status)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return s.Cancel(jobID) // the status moved on; decide again
		}
	case StatusRunning:
		if _, err := s.db.Exec("UPDATE jobs SET cancel_requested_at = ?, updated_at = ? WHERE id = ? AND cancel_requested_at IS NULL",
			now, now, jobID); err != nil {
			return nil, err
		}
	default:
		return job, ErrNotCancelable
	}
	return s.Get(jobID)
}

// settleCancel finishes a running job whose cancellation was requested; called from
// Heartbeat. Returns ErrCanceled when the job is (now) canceled.
func (s *Service) settleCancel(jobID, workerID string) error {
	var requested sql.NullTime
	var status string
	err := s.db.QueryRow("SELECT status, cancel_requested_at FROM jobs WHERE id = ?", jobID).Scan(&status, &requested)
	if err != nil {
		return err
	}
	if status == StatusCanceled {
		return ErrCanceled
	}
	if !requested.Valid {
		return nil
	}
	now := time.Now()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`
		UPDATE jobs SET status = ?, completed_at = ?, updated_at = ?,
			leased_at = NULL, lease_expires_at = NULL, worker_id = NULL
		WHERE id = ? AND worker_id = ?`, StatusCanceled, now, now, jobID, workerID); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE job_attempts SET status = ?, finished_at = ?, error_text = ? WHERE job_id = ? AND status = ?",
		StatusCanceled, now, "canceled", jobID, StatusRunning); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return ErrCanceled
}

// PurgeExpired erases input, result and error text of finished jobs whose retention has
// passed. Returns how many jobs were purged.
func (s *Service) PurgeExpired(now time.Time) (int, error) {
	rows, err := s.db.Query(`
		SELECT id, completed_at, retention_seconds FROM jobs
		WHERE retention_seconds IS NOT NULL AND purged_at IS NULL AND completed_at IS NOT NULL`)
	if err != nil {
		return 0, err
	}
	var due []string
	for rows.Next() {
		var id string
		var completed time.Time
		var keep int64
		if err := rows.Scan(&id, &completed, &keep); err != nil {
			rows.Close()
			return 0, err
		}
		if !completed.Add(time.Duration(keep) * time.Second).After(now) {
			due = append(due, id)
		}
	}
	rows.Close()
	for _, id := range due {
		if _, err := s.db.Exec(`
			UPDATE jobs SET input_metadata_json = '{}', result_metadata_json = NULL, error_text = NULL,
				purged_at = ?, updated_at = ?
			WHERE id = ?`, now, now, id); err != nil {
			return 0, err
		}
		if _, err := s.db.Exec("UPDATE job_attempts SET error_text = NULL WHERE job_id = ?", id); err != nil {
			return 0, err
		}
	}
	return len(due), nil
}

// Stats is a snapshot of the queue.
type Stats struct {
	ByStatus              map[string]int `json:"by_status"`
	OldestQueuedAgeSecond int            `json:"oldest_queued_age_seconds"`
}

// Stats counts jobs by status and reports how long the oldest queued job has waited.
func (s *Service) Stats(now time.Time) (*Stats, error) {
	st := &Stats{ByStatus: map[string]int{}}
	rows, err := s.db.Query("SELECT status, COUNT(*) FROM jobs GROUP BY status")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			rows.Close()
			return nil, err
		}
		st.ByStatus[status] = n
	}
	rows.Close()
	var oldest sql.NullTime
	if err := s.db.QueryRow("SELECT MIN(queued_at) FROM jobs WHERE status = ?", StatusQueued).Scan(&oldest); err == nil && oldest.Valid {
		st.OldestQueuedAgeSecond = int(now.Sub(oldest.Time).Seconds())
	} else {
		// MIN() over DATETIME text comes back as a string in go-sqlite3
		var text sql.NullString
		if err := s.db.QueryRow("SELECT MIN(queued_at) FROM jobs WHERE status = ?", StatusQueued).Scan(&text); err == nil && text.Valid {
			if t, perr := parseSQLiteTime(text.String); perr == nil {
				st.OldestQueuedAgeSecond = int(now.Sub(t).Seconds())
			}
		}
	}
	return st, nil
}

// IsTerminal reports whether a status is final.
func IsTerminal(status string) bool {
	switch status {
	case StatusSucceeded, StatusFailed, StatusDeadLetter, StatusCanceled:
		return true
	}
	return false
}

func nullString(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }

func parseSQLiteTime(s string) (time.Time, error) {
	for _, layout := range []string{
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02T15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02T15:04:05Z",
		time.RFC3339Nano,
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized time %q", s)
}

// VersionOf is the job type version a job runs under: "0" for the untyped contract.
func VersionOf(j *Job) string { return versionOf(j) }

func versionOf(j *Job) string { return versionOrUntyped(j.JobTypeVersion.String) }

func versionOrUntyped(v string) string {
	if v == "" {
		return "0"
	}
	return v
}
