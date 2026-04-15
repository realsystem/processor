package workers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/processor/control-plane/internal/db"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrWorkerNotFound   = errors.New("worker not found")
	ErrInvalidToken     = errors.New("invalid worker token")
)

// Worker represents a data-plane worker
type Worker struct {
	ID            string
	Name          string
	AuthTokenHash string
	Status        string
	Capabilities  []string
	Version       string
	LastSeenAt    time.Time
	CurrentJobID  sql.NullString
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Service handles worker operations
type Service struct {
	db *sql.DB
}

// NewService creates a new worker service
func NewService(database *sql.DB) *Service {
	return &Service{db: database}
}

// ValidateToken validates a worker auth token
func (s *Service) ValidateToken(token string) (*Worker, error) {
	// For simplicity in this version, we use a single shared worker token
	// In production, you might want individual worker tokens
	query := `
		SELECT id, name, auth_token_hash, status, capabilities_json, version, last_seen_at, current_job_id, created_at, updated_at
		FROM workers
		ORDER BY created_at ASC
		LIMIT 1
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var worker Worker
		var capJSON string

		err := rows.Scan(
			&worker.ID, &worker.Name, &worker.AuthTokenHash, &worker.Status, &capJSON,
			&worker.Version, &worker.LastSeenAt, &worker.CurrentJobID, &worker.CreatedAt, &worker.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		// Verify token
		if err := bcrypt.CompareHashAndPassword([]byte(worker.AuthTokenHash), []byte(token)); err == nil {
			// Parse capabilities
			if err := json.Unmarshal([]byte(capJSON), &worker.Capabilities); err != nil {
				return nil, fmt.Errorf("failed to unmarshal capabilities: %w", err)
			}

			return &worker, nil
		}
	}

	return nil, ErrInvalidToken
}

// UpdateLastSeen updates the worker's last seen timestamp
func (s *Service) UpdateLastSeen(workerID string) error {
	query := `UPDATE workers SET last_seen_at = ?, updated_at = ? WHERE id = ?`
	_, err := s.db.Exec(query, time.Now(), time.Now(), workerID)
	return err
}

// UpdateCurrentJob updates the worker's current job
func (s *Service) UpdateCurrentJob(workerID string, jobID sql.NullString) error {
	query := `UPDATE workers SET current_job_id = ?, updated_at = ? WHERE id = ?`
	_, err := s.db.Exec(query, jobID, time.Now(), workerID)
	return err
}

// EnsureWorker creates a worker if it doesn't exist
func (s *Service) EnsureWorker(workerID string, capabilities []string) error {
	// Check if worker exists
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM workers WHERE id = ?`, workerID).Scan(&count)
	if err != nil {
		return err
	}

	if count > 0 {
		// Worker exists, update capabilities
		capJSON, _ := json.Marshal(capabilities)
		_, err = s.db.Exec(`UPDATE workers SET capabilities_json = ?, updated_at = ? WHERE id = ?`,
			string(capJSON), time.Now(), workerID)
		return err
	}

	// Worker doesn't exist, create it
	capJSON, err := json.Marshal(capabilities)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO workers (id, name, auth_token_hash, status, capabilities_json, version, last_seen_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err = s.db.Exec(query,
		workerID,
		workerID, // Use ID as name for auto-registered workers
		"",       // No token hash for auto-registered workers
		"active",
		string(capJSON),
		"auto-registered",
		time.Now(),
		time.Now(),
		time.Now(),
	)
	return err
}

// Register registers a new worker or updates existing
func (s *Service) Register(name, token string, capabilities []string, version string) (*Worker, error) {
	tokenHash, err := bcrypt.GenerateFromPassword([]byte(token), 10)
	if err != nil {
		return nil, fmt.Errorf("failed to hash token: %w", err)
	}

	capJSON, err := json.Marshal(capabilities)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal capabilities: %w", err)
	}

	worker := &Worker{
		ID:            db.GenerateID("wrk"),
		Name:          name,
		AuthTokenHash: string(tokenHash),
		Status:        "active",
		Capabilities:  capabilities,
		Version:       version,
		LastSeenAt:    time.Now(),
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	query := `
		INSERT INTO workers (id, name, auth_token_hash, status, capabilities_json, version, last_seen_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = s.db.Exec(query,
		worker.ID, worker.Name, worker.AuthTokenHash, worker.Status, string(capJSON),
		worker.Version, worker.LastSeenAt, worker.CreatedAt, worker.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to register worker: %w", err)
	}

	return worker, nil
}

// List returns all workers
func (s *Service) List() ([]Worker, error) {
	query := `
		SELECT id, name, status, capabilities_json, version, last_seen_at, current_job_id, created_at, updated_at
		FROM workers
		ORDER BY created_at DESC
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var workers []Worker
	for rows.Next() {
		var worker Worker
		var capJSON string

		err := rows.Scan(
			&worker.ID, &worker.Name, &worker.Status, &capJSON, &worker.Version,
			&worker.LastSeenAt, &worker.CurrentJobID, &worker.CreatedAt, &worker.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal([]byte(capJSON), &worker.Capabilities); err != nil {
			return nil, fmt.Errorf("failed to unmarshal capabilities: %w", err)
		}

		workers = append(workers, worker)
	}

	return workers, nil
}

// Get retrieves a worker by ID
func (s *Service) Get(workerID string) (*Worker, error) {
	query := `
		SELECT id, name, status, capabilities_json, version, last_seen_at, current_job_id, created_at, updated_at
		FROM workers
		WHERE id = ?
	`

	var worker Worker
	var capJSON string

	err := s.db.QueryRow(query, workerID).Scan(
		&worker.ID, &worker.Name, &worker.Status, &capJSON, &worker.Version,
		&worker.LastSeenAt, &worker.CurrentJobID, &worker.CreatedAt, &worker.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrWorkerNotFound
		}
		return nil, err
	}

	if err := json.Unmarshal([]byte(capJSON), &worker.Capabilities); err != nil {
		return nil, fmt.Errorf("failed to unmarshal capabilities: %w", err)
	}

	return &worker, nil
}

// EnsureBootstrapWorker ensures a bootstrap worker exists for the worker token
func (s *Service) EnsureBootstrapWorker(token string) error {
	// Check if any worker exists
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM workers").Scan(&count)
	if err != nil {
		return err
	}

	if count > 0 {
		return nil // Workers already exist
	}

	// Create bootstrap worker
	_, err = s.Register("bootstrap-worker", token, []string{"noop", "sleep", "echo_metadata", "ollama_chat"}, "1.0.0")
	return err
}
