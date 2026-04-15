package email

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/smtp"
	"time"

	"github.com/processor/control-plane/internal/db"
)

// EmailNotification represents an email to be sent
type EmailNotification struct {
	ID            string
	UserID        string
	JobID         sql.NullString
	TemplateName  string
	PayloadJSON   string
	State         string
	AttemptCount  int
	NextAttemptAt sql.NullTime
	SentAt        sql.NullTime
	FailedAt      sql.NullTime
	CreatedAt     time.Time
}

// Provider defines the email sending interface
type Provider interface {
	Send(to, subject, body string) error
}

// SMTPProvider implements email sending via SMTP
type SMTPProvider struct {
	Host     string
	Port     int
	User     string
	Password string
	From     string
}

// LogProvider logs emails instead of sending (for development)
type LogProvider struct{}

// Service handles email notifications
type Service struct {
	database *sql.DB
	provider Provider
}

// NewService creates a new email service
func NewService(database *sql.DB, provider Provider) *Service {
	return &Service{
		database: database,
		provider: provider,
	}
}

// Queue queues an email notification
func (s *Service) Queue(userID string, jobID sql.NullString, templateName string, payload map[string]interface{}) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	notification := &EmailNotification{
		ID:            db.GenerateID("email"),
		UserID:        userID,
		JobID:         jobID,
		TemplateName:  templateName,
		PayloadJSON:   string(payloadJSON),
		State:         "pending",
		AttemptCount:  0,
		NextAttemptAt: sql.NullTime{Time: time.Now(), Valid: true},
		CreatedAt:     time.Now(),
	}

	query := `
		INSERT INTO email_notifications (id, user_id, job_id, template_name, payload_json, state, attempt_count, next_attempt_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = s.database.Exec(query,
		notification.ID, notification.UserID, notification.JobID, notification.TemplateName,
		notification.PayloadJSON, notification.State, notification.AttemptCount,
		notification.NextAttemptAt, notification.CreatedAt,
	)

	return err
}

// ProcessPending processes pending email notifications
func (s *Service) ProcessPending(limit int) (int, error) {
	// Get pending notifications
	query := `
		SELECT en.id, en.user_id, en.template_name, en.payload_json, en.attempt_count,
		       u.email
		FROM email_notifications en
		JOIN users u ON en.user_id = u.id
		WHERE en.state = 'pending' AND en.next_attempt_at <= ?
		ORDER BY en.created_at ASC
		LIMIT ?
	`

	rows, err := s.database.Query(query, time.Now(), limit)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	processed := 0

	for rows.Next() {
		var notificationID, userID, templateName, payloadJSON, userEmail string
		var attemptCount int

		err := rows.Scan(
			&notificationID, &userID, &templateName, &payloadJSON, &attemptCount, &userEmail,
		)
		if err != nil {
			continue
		}

		// Parse payload
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
			log.Printf("Failed to unmarshal email payload: %v", err)
			continue
		}

		// Render email
		subject, body := s.renderEmail(templateName, payload)

		// Send email
		err = s.provider.Send(userEmail, subject, body)

		attemptCount++

		if err == nil {
			// Success
			s.markSent(notificationID, attemptCount)
			processed++
		} else {
			// Failed
			if attemptCount >= 5 {
				// Give up after 5 attempts
				s.markFailed(notificationID, attemptCount)
			} else {
				// Retry with exponential backoff
				nextAttempt := time.Now().Add(time.Duration(attemptCount*attemptCount) * time.Minute)
				s.markPending(notificationID, attemptCount, nextAttempt)
			}
		}
	}

	return processed, nil
}

func (s *Service) renderEmail(templateName string, payload map[string]interface{}) (subject, body string) {
	switch templateName {
	case "job_succeeded":
		subject = "Job Completed Successfully"
		jobID, _ := payload["job_id"].(string)
		jobType, _ := payload["job_type"].(string)
		body = fmt.Sprintf("Your job %s (%s) has completed successfully.\n\n", jobID, jobType)
	case "job_failed":
		subject = "Job Failed"
		jobID, _ := payload["job_id"].(string)
		jobType, _ := payload["job_type"].(string)
		errorText, _ := payload["error"].(string)
		body = fmt.Sprintf("Your job %s (%s) has failed.\n\nError: %s\n\n", jobID, jobType, errorText)
	case "job_dead_letter":
		subject = "Job Moved to Dead Letter Queue"
		jobID, _ := payload["job_id"].(string)
		jobType, _ := payload["job_type"].(string)
		body = fmt.Sprintf("Your job %s (%s) has exhausted all retry attempts and has been moved to the dead letter queue.\n\n", jobID, jobType)
	default:
		subject = "Notification"
		body = fmt.Sprintf("Payload: %v\n", payload)
	}

	return subject, body
}

func (s *Service) markSent(notificationID string, attemptCount int) {
	query := `
		UPDATE email_notifications
		SET state = 'sent', attempt_count = ?, sent_at = ?
		WHERE id = ?
	`
	s.database.Exec(query, attemptCount, time.Now(), notificationID)
}

func (s *Service) markFailed(notificationID string, attemptCount int) {
	query := `
		UPDATE email_notifications
		SET state = 'failed', attempt_count = ?, failed_at = ?
		WHERE id = ?
	`
	s.database.Exec(query, attemptCount, time.Now(), notificationID)
}

func (s *Service) markPending(notificationID string, attemptCount int, nextAttempt time.Time) {
	query := `
		UPDATE email_notifications
		SET attempt_count = ?, next_attempt_at = ?
		WHERE id = ?
	`
	s.database.Exec(query, attemptCount, nextAttempt, notificationID)
}

// Send implements the Provider interface for SMTPProvider
func (p *SMTPProvider) Send(to, subject, body string) error {
	if p.Host == "" {
		return fmt.Errorf("SMTP host not configured")
	}

	auth := smtp.PlainAuth("", p.User, p.Password, p.Host)

	msg := fmt.Sprintf("From: %s\r\n"+
		"To: %s\r\n"+
		"Subject: %s\r\n"+
		"\r\n"+
		"%s\r\n", p.From, to, subject, body)

	addr := fmt.Sprintf("%s:%d", p.Host, p.Port)
	return smtp.SendMail(addr, auth, p.From, []string{to}, []byte(msg))
}

// Send implements the Provider interface for LogProvider
func (p *LogProvider) Send(to, subject, body string) error {
	log.Printf("[EMAIL] To: %s, Subject: %s\n%s\n", to, subject, body)
	return nil
}

// NewSMTPProvider creates a new SMTP email provider
func NewSMTPProvider(host string, port int, user, password, from string) *SMTPProvider {
	return &SMTPProvider{
		Host:     host,
		Port:     port,
		User:     user,
		Password: password,
		From:     from,
	}
}

// NewLogProvider creates a new log-based email provider (for development)
func NewLogProvider() *LogProvider {
	return &LogProvider{}
}
