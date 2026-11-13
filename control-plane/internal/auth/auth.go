package auth

import (
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/processor/control-plane/internal/db"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrUserNotFound       = errors.New("user not found")
	ErrUserDisabled       = errors.New("user account is disabled")
	ErrDuplicateEmail     = errors.New("email already registered")
	ErrInvalidSession     = errors.New("invalid or expired session")
	ErrInvalidAPIKey      = errors.New("invalid or revoked API key")
)

const (
	bcryptCost = 10
)

// User represents a user account
type User struct {
	ID           string
	Email        string
	PasswordHash string
	PlanTier     string
	IsAdmin      bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DisabledAt   sql.NullTime
}

// Session represents a user session
type Session struct {
	ID         string
	UserID     string
	TokenHash  string
	ExpiresAt  time.Time
	CreatedAt  time.Time
	LastSeenAt time.Time
}

// APIKey represents an API key
type APIKey struct {
	ID        string
	UserID    string
	Name      string
	KeyPrefix string
	KeyHash   string
	CreatedAt time.Time
	RevokedAt sql.NullTime
}

// Service handles authentication operations
type Service struct {
	db *sql.DB
}

// NewService creates a new auth service
func NewService(database *sql.DB) *Service {
	return &Service{db: database}
}

// Register creates a new user account
func (s *Service) Register(email, password string, isAdmin bool) (*User, error) {
	// Hash password
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	user := &User{
		ID:           db.GenerateID("usr"),
		Email:        email,
		PasswordHash: string(passwordHash),
		PlanTier:     "free",
		IsAdmin:      isAdmin,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	query := `
		INSERT INTO users (id, email, password_hash, plan_tier, is_admin, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`

	_, err = s.db.Exec(query, user.ID, user.Email, user.PasswordHash, user.PlanTier, user.IsAdmin, user.CreatedAt, user.UpdatedAt)
	if err != nil {
		if contains(err.Error(), "UNIQUE constraint failed") {
			return nil, ErrDuplicateEmail
		}
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return user, nil
}

// Login authenticates a user and creates a session
func (s *Service) Login(email, password string, sessionDuration time.Duration) (string, *User, error) {
	user, err := s.getUserByEmail(email)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil, ErrInvalidCredentials
		}
		return "", nil, err
	}

	// Check if user is disabled
	if user.DisabledAt.Valid {
		return "", nil, ErrUserDisabled
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return "", nil, ErrInvalidCredentials
	}

	// Create session
	sessionToken := db.GenerateToken(32)
	sessionTokenHash, err := bcrypt.GenerateFromPassword([]byte(sessionToken), bcryptCost)
	if err != nil {
		return "", nil, fmt.Errorf("failed to hash session token: %w", err)
	}

	session := &Session{
		ID:         db.GenerateID("sess"),
		UserID:     user.ID,
		TokenHash:  string(sessionTokenHash),
		ExpiresAt:  time.Now().Add(sessionDuration),
		CreatedAt:  time.Now(),
		LastSeenAt: time.Now(),
	}

	query := `
		INSERT INTO sessions (id, user_id, token_hash, expires_at, created_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`

	_, err = s.db.Exec(query, session.ID, session.UserID, session.TokenHash, session.ExpiresAt, session.CreatedAt, session.LastSeenAt)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create session: %w", err)
	}

	return sessionToken, user, nil
}

// ValidateSession validates a session token and returns the user
func (s *Service) ValidateSession(sessionToken string) (*User, error) {
	// Find all active sessions (we need to check the hash)
	query := `
		SELECT s.id, s.user_id, s.token_hash, s.expires_at, u.id, u.email, u.password_hash, u.plan_tier, u.is_admin, u.created_at, u.updated_at, u.disabled_at
		FROM sessions s
		JOIN users u ON s.user_id = u.id
		WHERE s.expires_at > ?
	`

	rows, err := s.db.Query(query, time.Now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var session Session
		var user User

		err := rows.Scan(
			&session.ID, &session.UserID, &session.TokenHash, &session.ExpiresAt,
			&user.ID, &user.Email, &user.PasswordHash, &user.PlanTier, &user.IsAdmin,
			&user.CreatedAt, &user.UpdatedAt, &user.DisabledAt,
		)
		if err != nil {
			return nil, err
		}

		// Check if token matches
		if err := bcrypt.CompareHashAndPassword([]byte(session.TokenHash), []byte(sessionToken)); err == nil {
			// Token matches, check if user is disabled
			if user.DisabledAt.Valid {
				return nil, ErrUserDisabled
			}

			// Update last seen
			s.db.Exec("UPDATE sessions SET last_seen_at = ? WHERE id = ?", time.Now(), session.ID)

			return &user, nil
		}
	}

	return nil, ErrInvalidSession
}

// Logout invalidates a session
func (s *Service) Logout(sessionToken string) error {
	// Find and delete the session
	query := `DELETE FROM sessions WHERE token_hash = ?`
	_, err := s.db.Exec(query, sessionToken)
	return err
}

// CreateAPIKey creates a new API key for a user
func (s *Service) CreateAPIKey(userID, name string) (string, *APIKey, error) {
	fullKey, prefix := db.GenerateAPIKey()

	keyHash, err := bcrypt.GenerateFromPassword([]byte(fullKey), bcryptCost)
	if err != nil {
		return "", nil, fmt.Errorf("failed to hash API key: %w", err)
	}

	apiKey := &APIKey{
		ID:        db.GenerateID("key"),
		UserID:    userID,
		Name:      name,
		KeyPrefix: prefix,
		KeyHash:   string(keyHash),
		CreatedAt: time.Now(),
	}

	query := `
		INSERT INTO api_keys (id, user_id, name, key_prefix, key_hash, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`

	_, err = s.db.Exec(query, apiKey.ID, apiKey.UserID, apiKey.Name, apiKey.KeyPrefix, apiKey.KeyHash, apiKey.CreatedAt)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create API key: %w", err)
	}

	return fullKey, apiKey, nil
}

// ValidateAPIKey validates an API key and returns the user
func (s *Service) ValidateAPIKey(apiKey string) (*User, error) {
	// Extract prefix (format: pk_xxxxxxxxxxxxxxxx_yyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyy)
	parts := splitN(apiKey, "_", 3)
	if len(parts) != 3 || parts[0] != "pk" {
		return nil, ErrInvalidAPIKey
	}
	prefix := parts[0] + "_" + parts[1]

	// Find API key by prefix
	query := `
		SELECT ak.id, ak.user_id, ak.key_hash, ak.revoked_at,
		       u.id, u.email, u.password_hash, u.plan_tier, u.is_admin, u.created_at, u.updated_at, u.disabled_at
		FROM api_keys ak
		JOIN users u ON ak.user_id = u.id
		WHERE ak.key_prefix = ?
	`

	var key APIKey
	var user User

	err := s.db.QueryRow(query, prefix).Scan(
		&key.ID, &key.UserID, &key.KeyHash, &key.RevokedAt,
		&user.ID, &user.Email, &user.PasswordHash, &user.PlanTier, &user.IsAdmin,
		&user.CreatedAt, &user.UpdatedAt, &user.DisabledAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrInvalidAPIKey
		}
		return nil, err
	}

	// Check if revoked
	if key.RevokedAt.Valid {
		return nil, ErrInvalidAPIKey
	}

	// Check if user is disabled
	if user.DisabledAt.Valid {
		return nil, ErrUserDisabled
	}

	// Verify API key
	if err := bcrypt.CompareHashAndPassword([]byte(key.KeyHash), []byte(apiKey)); err != nil {
		return nil, ErrInvalidAPIKey
	}

	return &user, nil
}

// RevokeAPIKey revokes an API key
func (s *Service) RevokeAPIKey(keyID, userID string) error {
	query := `UPDATE api_keys SET revoked_at = ? WHERE id = ? AND user_id = ?`
	result, err := s.db.Exec(query, time.Now(), keyID, userID)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rows == 0 {
		return errors.New("API key not found")
	}

	return nil
}

// ListAPIKeys returns all API keys for a user
func (s *Service) ListAPIKeys(userID string) ([]APIKey, error) {
	query := `
		SELECT id, user_id, name, key_prefix, created_at, revoked_at
		FROM api_keys
		WHERE user_id = ?
		ORDER BY created_at DESC
	`

	rows, err := s.db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []APIKey
	for rows.Next() {
		var key APIKey
		err := rows.Scan(&key.ID, &key.UserID, &key.Name, &key.KeyPrefix, &key.CreatedAt, &key.RevokedAt)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}

	return keys, nil
}

// GetUser returns a user by ID
func (s *Service) GetUser(userID string) (*User, error) {
	query := `
		SELECT id, email, password_hash, plan_tier, is_admin, created_at, updated_at, disabled_at
		FROM users
		WHERE id = ?
	`

	var user User
	err := s.db.QueryRow(query, userID).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.PlanTier, &user.IsAdmin,
		&user.CreatedAt, &user.UpdatedAt, &user.DisabledAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	return &user, nil
}

// GetUserByEmail returns the user with this email address
func (s *Service) GetUserByEmail(email string) (*User, error) {
	return s.getUserByEmail(email)
}

func (s *Service) getUserByEmail(email string) (*User, error) {
	query := `
		SELECT id, email, password_hash, plan_tier, is_admin, created_at, updated_at, disabled_at
		FROM users
		WHERE email = ?
	`

	var user User
	err := s.db.QueryRow(query, email).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.PlanTier, &user.IsAdmin,
		&user.CreatedAt, &user.UpdatedAt, &user.DisabledAt,
	)

	return &user, err
}

// Helper functions
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsMiddle(s, substr)))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func splitN(s, sep string, n int) []string {
	result := []string{}
	current := ""
	sepCount := 0

	for i := 0; i < len(s); i++ {
		if sepCount < n-1 && i+len(sep) <= len(s) && s[i:i+len(sep)] == sep {
			result = append(result, current)
			current = ""
			i += len(sep) - 1
			sepCount++
		} else {
			current += string(s[i])
		}
	}

	if current != "" || sepCount > 0 {
		result = append(result, current)
	}

	return result
}

// SecureCompare performs a timing-safe comparison
func SecureCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
