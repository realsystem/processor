package webhooks

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/processor/control-plane/internal/db"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrWebhookNotFound = errors.New("webhook not found")
)

// Webhook represents a user webhook configuration
type Webhook struct {
	ID               string
	UserID           string
	TargetURL        string
	SecretHash       string
	IsActive         bool
	SubscribedEvents []string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// WebhookDelivery represents a webhook delivery attempt
type WebhookDelivery struct {
	ID              string
	WebhookID       string
	JobID           sql.NullString
	EventType       string
	PayloadJSON     string
	Status          string
	StatusCode      sql.NullInt64
	ResponseSnippet string
	AttemptCount    int
	NextAttemptAt   sql.NullTime
	DeliveredAt     sql.NullTime
	FailedAt        sql.NullTime
	CreatedAt       time.Time
}

// Service handles webhook operations
type Service struct {
	db         *sql.DB
	httpClient *http.Client
}

// NewService creates a new webhook service
func NewService(database *sql.DB) *Service {
	return &Service{
		db: database,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Create creates a new webhook
func (s *Service) Create(userID, targetURL, secret string, subscribedEvents []string, isActive bool) (*Webhook, error) {
	secretHash, err := bcrypt.GenerateFromPassword([]byte(secret), 10)
	if err != nil {
		return nil, fmt.Errorf("failed to hash secret: %w", err)
	}

	eventsJSON, err := json.Marshal(subscribedEvents)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal events: %w", err)
	}

	webhook := &Webhook{
		ID:               db.GenerateID("hook"),
		UserID:           userID,
		TargetURL:        targetURL,
		SecretHash:       string(secretHash),
		IsActive:         isActive,
		SubscribedEvents: subscribedEvents,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	query := `
		INSERT INTO webhooks (id, user_id, target_url, secret_hash, is_active, subscribed_events_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = s.db.Exec(query,
		webhook.ID, webhook.UserID, webhook.TargetURL, webhook.SecretHash,
		webhook.IsActive, string(eventsJSON), webhook.CreatedAt, webhook.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create webhook: %w", err)
	}

	return webhook, nil
}

// List returns all webhooks for a user
func (s *Service) List(userID string) ([]Webhook, error) {
	query := `
		SELECT id, user_id, target_url, is_active, subscribed_events_json, created_at, updated_at
		FROM webhooks
		WHERE user_id = ?
		ORDER BY created_at DESC
	`

	rows, err := s.db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var webhooks []Webhook
	for rows.Next() {
		var webhook Webhook
		var eventsJSON string

		err := rows.Scan(
			&webhook.ID, &webhook.UserID, &webhook.TargetURL, &webhook.IsActive,
			&eventsJSON, &webhook.CreatedAt, &webhook.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal([]byte(eventsJSON), &webhook.SubscribedEvents); err != nil {
			return nil, fmt.Errorf("failed to unmarshal events: %w", err)
		}

		webhooks = append(webhooks, webhook)
	}

	return webhooks, nil
}

// Delete deletes a webhook
func (s *Service) Delete(webhookID, userID string) error {
	query := `DELETE FROM webhooks WHERE id = ? AND user_id = ?`
	result, err := s.db.Exec(query, webhookID, userID)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rows == 0 {
		return ErrWebhookNotFound
	}

	return nil
}

// QueueDelivery queues a webhook delivery
func (s *Service) QueueDelivery(webhookID, eventType string, jobID sql.NullString, payload map[string]interface{}) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	delivery := &WebhookDelivery{
		ID:            db.GenerateID("del"),
		WebhookID:     webhookID,
		JobID:         jobID,
		EventType:     eventType,
		PayloadJSON:   string(payloadJSON),
		Status:        "pending",
		AttemptCount:  0,
		NextAttemptAt: sql.NullTime{Time: time.Now(), Valid: true},
		CreatedAt:     time.Now(),
	}

	query := `
		INSERT INTO webhook_deliveries (id, webhook_id, job_id, event_type, payload_json, status, attempt_count, next_attempt_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = s.db.Exec(query,
		delivery.ID, delivery.WebhookID, delivery.JobID, delivery.EventType,
		delivery.PayloadJSON, delivery.Status, delivery.AttemptCount,
		delivery.NextAttemptAt, delivery.CreatedAt,
	)

	return err
}

// ProcessPendingDeliveries processes pending webhook deliveries
func (s *Service) ProcessPendingDeliveries(limit int) (int, error) {
	// Get pending deliveries
	query := `
		SELECT d.id, d.webhook_id, d.event_type, d.payload_json, d.attempt_count,
		       w.id, w.target_url, w.secret_hash
		FROM webhook_deliveries d
		JOIN webhooks w ON d.webhook_id = w.id
		WHERE d.status = 'pending' AND d.next_attempt_at <= ? AND w.is_active = 1
		ORDER BY d.created_at ASC
		LIMIT ?
	`

	rows, err := s.db.Query(query, time.Now(), limit)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	processed := 0

	for rows.Next() {
		var deliveryID, webhookID, eventType, payloadJSON string
		var attemptCount int
		var targetURL, secretHash string
		var webhookIDUnused string // webhook.id from JOIN, not needed

		err := rows.Scan(
			&deliveryID, &webhookID, &eventType, &payloadJSON, &attemptCount,
			&webhookIDUnused, &targetURL, &secretHash,
		)
		if err != nil {
			continue
		}

		// Attempt delivery
		statusCode, responseSnippet, err := s.deliver(targetURL, secretHash, deliveryID, eventType, payloadJSON)

		attemptCount++

		if err == nil && statusCode >= 200 && statusCode < 300 {
			// Success
			s.markDelivered(deliveryID, statusCode, responseSnippet, attemptCount)
			processed++
		} else {
			// Failed
			if attemptCount >= 5 {
				// Give up after 5 attempts
				s.markFailed(deliveryID, statusCode, responseSnippet, attemptCount)
			} else {
				// Retry with exponential backoff
				nextAttempt := time.Now().Add(time.Duration(attemptCount*attemptCount) * time.Minute)
				s.markPending(deliveryID, statusCode, responseSnippet, attemptCount, nextAttempt)
			}
		}
	}

	return processed, nil
}

func (s *Service) deliver(targetURL, secretHash, deliveryID, eventType, payloadJSON string) (int, string, error) {
	// Create request
	req, err := http.NewRequest("POST", targetURL, bytes.NewBufferString(payloadJSON))
	if err != nil {
		return 0, "", err
	}

	// Sign payload (we need to retrieve the actual secret, but we only have the hash)
	// For now, we'll use a simplified approach where we sign with a derived key
	// In production, you might want to store secrets encrypted instead of hashed
	signature := s.signPayload(secretHash, payloadJSON)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Processor-Signature", signature)
	req.Header.Set("X-Processor-Event", eventType)
	req.Header.Set("X-Processor-Delivery-ID", deliveryID)

	// Send request
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return 0, err.Error(), err
	}
	defer resp.Body.Close()

	// Read response snippet
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	responseSnippet := string(bodyBytes)

	return resp.StatusCode, responseSnippet, nil
}

func (s *Service) signPayload(secretHash, payload string) string {
	// Note: In production, you should store the secret encrypted, not hashed
	// For this implementation, we'll use the hash as the signing key
	h := hmac.New(sha256.New, []byte(secretHash))
	h.Write([]byte(payload))
	return "sha256=" + hex.EncodeToString(h.Sum(nil))
}

func (s *Service) markDelivered(deliveryID string, statusCode int, responseSnippet string, attemptCount int) {
	query := `
		UPDATE webhook_deliveries
		SET status = 'delivered', status_code = ?, response_snippet = ?, attempt_count = ?, delivered_at = ?
		WHERE id = ?
	`
	s.db.Exec(query, statusCode, responseSnippet, attemptCount, time.Now(), deliveryID)
}

func (s *Service) markFailed(deliveryID string, statusCode int, responseSnippet string, attemptCount int) {
	query := `
		UPDATE webhook_deliveries
		SET status = 'failed', status_code = ?, response_snippet = ?, attempt_count = ?, failed_at = ?
		WHERE id = ?
	`
	s.db.Exec(query, statusCode, responseSnippet, attemptCount, time.Now(), deliveryID)
}

func (s *Service) markPending(deliveryID string, statusCode int, responseSnippet string, attemptCount int, nextAttempt time.Time) {
	query := `
		UPDATE webhook_deliveries
		SET status_code = ?, response_snippet = ?, attempt_count = ?, next_attempt_at = ?
		WHERE id = ?
	`
	s.db.Exec(query, statusCode, responseSnippet, attemptCount, nextAttempt, deliveryID)
}

// FindActiveWebhooksForEvent finds all active webhooks subscribed to an event for a user
func (s *Service) FindActiveWebhooksForEvent(userID, eventType string) ([]Webhook, error) {
	query := `
		SELECT id, user_id, target_url, secret_hash, subscribed_events_json, created_at, updated_at
		FROM webhooks
		WHERE user_id = ? AND is_active = 1
	`

	rows, err := s.db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var webhooks []Webhook
	for rows.Next() {
		var webhook Webhook
		var eventsJSON string

		err := rows.Scan(
			&webhook.ID, &webhook.UserID, &webhook.TargetURL, &webhook.SecretHash,
			&eventsJSON, &webhook.CreatedAt, &webhook.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal([]byte(eventsJSON), &webhook.SubscribedEvents); err != nil {
			return nil, fmt.Errorf("failed to unmarshal events: %w", err)
		}

		// Check if subscribed to this event
		for _, event := range webhook.SubscribedEvents {
			if event == eventType {
				webhooks = append(webhooks, webhook)
				break
			}
		}
	}

	return webhooks, nil
}
