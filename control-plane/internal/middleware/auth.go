package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/processor/control-plane/internal/auth"
)

type contextKey string

const (
	UserContextKey contextKey = "user"
)

// AuthMiddleware provides authentication middleware
type AuthMiddleware struct {
	authService *auth.Service
}

// NewAuthMiddleware creates a new auth middleware
func NewAuthMiddleware(authService *auth.Service) *AuthMiddleware {
	return &AuthMiddleware{authService: authService}
}

// Session requires a valid session cookie
func (m *AuthMiddleware) Session(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session")
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		user, err := m.authService.ValidateSession(cookie.Value)
		if err != nil {
			http.SetCookie(w, &http.Cookie{
				Name:   "session",
				Value:  "",
				Path:   "/",
				MaxAge: -1,
			})
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		ctx := context.WithValue(r.Context(), UserContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// APIAuth requires either session or API key authentication
func (m *AuthMiddleware) APIAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var user *auth.User
		var err error

		// Try API key first
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			apiKey := strings.TrimPrefix(authHeader, "Bearer ")
			user, err = m.authService.ValidateAPIKey(apiKey)
		}

		// Try session cookie if API key failed
		if err != nil || user == nil {
			cookie, cookieErr := r.Cookie("session")
			if cookieErr == nil {
				user, err = m.authService.ValidateSession(cookie.Value)
			}
		}

		if err != nil || user == nil {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
			return
		}

		ctx := context.WithValue(r.Context(), UserContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// WorkerAuth requires worker token authentication
func (m *AuthMiddleware) WorkerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Worker authentication is handled in the httpapi package
		// This middleware is a placeholder for future worker-specific logic
		next.ServeHTTP(w, r)
	})
}

// AdminOnly requires the user to be an admin
func (m *AuthMiddleware) AdminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := getUserFromContext(r.Context())
		if user == nil || !user.IsAdmin {
			writeJSONError(w, http.StatusForbidden, "forbidden", "Admin access required")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// GetUserFromContext retrieves the user from the request context
func GetUserFromContext(ctx context.Context) *auth.User {
	user, _ := ctx.Value(UserContextKey).(*auth.User)
	return user
}

func getUserFromContext(ctx context.Context) *auth.User {
	return GetUserFromContext(ctx)
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json := `{"success":false,"error":{"code":"` + code + `","message":"` + message + `"}}`
	w.Write([]byte(json))
}
