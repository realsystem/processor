package auth

import (
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/processor/control-plane/internal/db"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()

	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	// Run migrations
	if err := db.Migrate(database); err != nil {
		t.Fatalf("Failed to run migrations: %v", err)
	}

	return database
}

func TestRegister(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	service := NewService(database)

	t.Run("successful registration", func(t *testing.T) {
		user, err := service.Register("test@example.com", "password123", false)
		if err != nil {
			t.Fatalf("Register failed: %v", err)
		}

		if user.Email != "test@example.com" {
			t.Errorf("Expected email test@example.com, got %s", user.Email)
		}

		if user.PlanTier != "free" {
			t.Errorf("Expected plan tier free, got %s", user.PlanTier)
		}

		if user.IsAdmin {
			t.Error("Expected non-admin user")
		}

		if len(user.PasswordHash) == 0 {
			t.Error("Expected password hash to be set")
		}
	})

	t.Run("duplicate email", func(t *testing.T) {
		_, err := service.Register("test@example.com", "password456", false)
		if err != ErrDuplicateEmail {
			t.Errorf("Expected ErrDuplicateEmail, got %v", err)
		}
	})

	t.Run("admin registration", func(t *testing.T) {
		user, err := service.Register("admin@example.com", "adminpass", true)
		if err != nil {
			t.Fatalf("Admin register failed: %v", err)
		}

		if !user.IsAdmin {
			t.Error("Expected admin user")
		}
	})
}

func TestLogin(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	service := NewService(database)

	// Create test user
	_, err := service.Register("test@example.com", "password123", false)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	t.Run("successful login", func(t *testing.T) {
		sessionToken, user, err := service.Login("test@example.com", "password123", 24*time.Hour)
		if err != nil {
			t.Fatalf("Login failed: %v", err)
		}

		if sessionToken == "" {
			t.Error("Expected session token")
		}

		if user.Email != "test@example.com" {
			t.Errorf("Expected email test@example.com, got %s", user.Email)
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		_, _, err := service.Login("test@example.com", "wrongpassword", 24*time.Hour)
		if err != ErrInvalidCredentials {
			t.Errorf("Expected ErrInvalidCredentials, got %v", err)
		}
	})

	t.Run("nonexistent user", func(t *testing.T) {
		_, _, err := service.Login("nonexistent@example.com", "password", 24*time.Hour)
		if err != ErrInvalidCredentials {
			t.Errorf("Expected ErrInvalidCredentials, got %v", err)
		}
	})
}

func TestValidateSession(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	service := NewService(database)

	// Create and login test user
	_, err := service.Register("test@example.com", "password123", false)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	sessionToken, _, err := service.Login("test@example.com", "password123", 24*time.Hour)
	if err != nil {
		t.Fatalf("Failed to login: %v", err)
	}

	t.Run("valid session", func(t *testing.T) {
		user, err := service.ValidateSession(sessionToken)
		if err != nil {
			t.Fatalf("ValidateSession failed: %v", err)
		}

		if user.Email != "test@example.com" {
			t.Errorf("Expected email test@example.com, got %s", user.Email)
		}
	})

	t.Run("invalid session token", func(t *testing.T) {
		_, err := service.ValidateSession("invalid-token")
		if err != ErrInvalidSession {
			t.Errorf("Expected ErrInvalidSession, got %v", err)
		}
	})
}

func TestCreateAPIKey(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	service := NewService(database)

	// Create test user
	user, err := service.Register("test@example.com", "password123", false)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	t.Run("successful creation", func(t *testing.T) {
		fullKey, apiKey, err := service.CreateAPIKey(user.ID, "Test Key")
		if err != nil {
			t.Fatalf("CreateAPIKey failed: %v", err)
		}

		if fullKey == "" {
			t.Error("Expected full API key")
		}

		if apiKey.Name != "Test Key" {
			t.Errorf("Expected name 'Test Key', got %s", apiKey.Name)
		}

		if apiKey.UserID != user.ID {
			t.Errorf("Expected user ID %s, got %s", user.ID, apiKey.UserID)
		}

		// Verify key format (pk_xxxxxxxx_yyyyyyyy)
		if len(fullKey) < 20 {
			t.Error("API key too short")
		}

		if fullKey[:3] != "pk_" {
			t.Errorf("Expected key to start with 'pk_', got %s", fullKey[:3])
		}
	})
}

func TestValidateAPIKey(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	service := NewService(database)

	// Create test user and API key
	user, err := service.Register("test@example.com", "password123", false)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	fullKey, _, err := service.CreateAPIKey(user.ID, "Test Key")
	if err != nil {
		t.Fatalf("Failed to create API key: %v", err)
	}

	t.Run("valid API key", func(t *testing.T) {
		validatedUser, err := service.ValidateAPIKey(fullKey)
		if err != nil {
			t.Fatalf("ValidateAPIKey failed: %v", err)
		}

		if validatedUser.ID != user.ID {
			t.Errorf("Expected user ID %s, got %s", user.ID, validatedUser.ID)
		}
	})

	t.Run("invalid API key", func(t *testing.T) {
		_, err := service.ValidateAPIKey("invalid-key")
		if err != ErrInvalidAPIKey {
			t.Errorf("Expected ErrInvalidAPIKey, got %v", err)
		}
	})

	t.Run("malformed API key", func(t *testing.T) {
		_, err := service.ValidateAPIKey("not-a-valid-format")
		if err != ErrInvalidAPIKey {
			t.Errorf("Expected ErrInvalidAPIKey, got %v", err)
		}
	})
}

func TestRevokeAPIKey(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	service := NewService(database)

	// Create test user and API key
	user, err := service.Register("test@example.com", "password123", false)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	fullKey, apiKey, err := service.CreateAPIKey(user.ID, "Test Key")
	if err != nil {
		t.Fatalf("Failed to create API key: %v", err)
	}

	t.Run("successful revocation", func(t *testing.T) {
		err := service.RevokeAPIKey(apiKey.ID, user.ID)
		if err != nil {
			t.Fatalf("RevokeAPIKey failed: %v", err)
		}

		// Verify key no longer works
		_, err = service.ValidateAPIKey(fullKey)
		if err != ErrInvalidAPIKey {
			t.Errorf("Expected ErrInvalidAPIKey after revocation, got %v", err)
		}
	})

	t.Run("nonexistent key", func(t *testing.T) {
		err := service.RevokeAPIKey("nonexistent", user.ID)
		if err == nil {
			t.Error("Expected error revoking nonexistent key")
		}
	})
}

func TestListAPIKeys(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	service := NewService(database)

	// Create test user
	user, err := service.Register("test@example.com", "password123", false)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Create multiple API keys
	_, _, err = service.CreateAPIKey(user.ID, "Key 1")
	if err != nil {
		t.Fatalf("Failed to create API key: %v", err)
	}

	_, _, err = service.CreateAPIKey(user.ID, "Key 2")
	if err != nil {
		t.Fatalf("Failed to create API key: %v", err)
	}

	t.Run("list all keys", func(t *testing.T) {
		keys, err := service.ListAPIKeys(user.ID)
		if err != nil {
			t.Fatalf("ListAPIKeys failed: %v", err)
		}

		if len(keys) != 2 {
			t.Errorf("Expected 2 keys, got %d", len(keys))
		}
	})
}

func TestDisabledUser(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	service := NewService(database)

	// Create test user
	_, err := service.Register("test@example.com", "password123", false)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Disable user
	_, err = database.Exec("UPDATE users SET disabled_at = ? WHERE email = ?", time.Now(), "test@example.com")
	if err != nil {
		t.Fatalf("Failed to disable user: %v", err)
	}

	t.Run("disabled user cannot login", func(t *testing.T) {
		_, _, err := service.Login("test@example.com", "password123", 24*time.Hour)
		if err != ErrUserDisabled {
			t.Errorf("Expected ErrUserDisabled, got %v", err)
		}
	})
}

func TestPasswordHashing(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	service := NewService(database)

	user, err := service.Register("test@example.com", "password123", false)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	t.Run("password is hashed", func(t *testing.T) {
		if user.PasswordHash == "password123" {
			t.Error("Password was not hashed")
		}
	})

	t.Run("password hash is bcrypt", func(t *testing.T) {
		// Bcrypt hashes start with $2a$ or $2b$
		if len(user.PasswordHash) < 60 {
			t.Error("Password hash too short for bcrypt")
		}
	})
}

func TestSecureCompare(t *testing.T) {
	tests := []struct {
		name     string
		a        string
		b        string
		expected bool
	}{
		{"equal strings", "hello", "hello", true},
		{"different strings", "hello", "world", false},
		{"empty strings", "", "", true},
		{"one empty", "hello", "", false},
		{"different lengths", "hello", "hello!", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SecureCompare(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("SecureCompare(%q, %q) = %v, want %v", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}
