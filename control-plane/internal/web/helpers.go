package web

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"time"

	"github.com/processor/control-plane/internal/jobs"
	"github.com/processor/control-plane/internal/middleware"
)

// TemplateRenderer handles template rendering
type TemplateRenderer struct {
	templates *template.Template
}

// NewTemplateRenderer creates a new template renderer
func NewTemplateRenderer(templatesDir string) (*TemplateRenderer, error) {
	funcMap := template.FuncMap{
		"formatTime":   formatTime,
		"formatJSON":   formatJSON,
		"statusBadge":  statusBadge,
		"truncate":     truncate,
		"add":          add,
		"sub":          sub,
		"eq":           eq,
		"gt":           gt,
		"lt":           lt,
	}

	// Parse all templates
	templates, err := template.New("").Funcs(funcMap).ParseGlob(filepath.Join(templatesDir, "*.html"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse base templates: %w", err)
	}

	// Parse subdirectories
	subdirs := []string{"jobs", "api-keys", "webhooks", "admin", "partials"}
	for _, subdir := range subdirs {
		pattern := filepath.Join(templatesDir, subdir, "*.html")
		templates, err = templates.ParseGlob(pattern)
		if err != nil {
			// Continue if subdir doesn't exist or has no templates
			continue
		}
	}

	return &TemplateRenderer{
		templates: templates,
	}, nil
}

// Render renders a template with data
func (tr *TemplateRenderer) Render(w http.ResponseWriter, r *http.Request, templateName string, data map[string]interface{}) {
	// Add common data
	if data == nil {
		data = make(map[string]interface{})
	}

	// Add user to data
	if user := middleware.GetUserFromContext(r.Context()); user != nil {
		data["User"] = user
		data["IsAuthenticated"] = true
	} else {
		data["IsAuthenticated"] = false
	}

	// Add flash messages
	if flashes := GetFlashes(r); len(flashes) > 0 {
		data["Flashes"] = flashes
	}

	// Add CSRF token
	data["CSRFToken"] = GenerateCSRF(w, r)

	// Set content type
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Render template
	if err := tr.templates.ExecuteTemplate(w, "base.html", data); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// Template functions

func formatTime(t time.Time) string {
	now := time.Now()
	diff := now.Sub(t)

	if diff < time.Minute {
		return "just now"
	} else if diff < time.Hour {
		mins := int(diff.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	} else if diff < 24*time.Hour {
		hours := int(diff.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	} else if diff < 7*24*time.Hour {
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}

	return t.Format("Jan 2, 2006 3:04 PM")
}

func formatJSON(data interface{}) string {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(b)
}

func statusBadge(status string) template.HTML {
	var class string
	var text string

	switch status {
	case jobs.StatusQueued:
		class = "badge-queued"
		text = "Queued"
	case jobs.StatusLeased:
		class = "badge-leased"
		text = "Leased"
	case jobs.StatusRunning:
		class = "badge-running"
		text = "Running"
	case jobs.StatusSucceeded:
		class = "badge-succeeded"
		text = "Succeeded"
	case jobs.StatusFailed:
		class = "badge-failed"
		text = "Failed"
	case jobs.StatusDeadLetter:
		class = "badge-dead_letter"
		text = "Dead Letter"
	case jobs.StatusCanceled:
		class = "badge-canceled"
		text = "Canceled"
	default:
		class = "badge-inactive"
		text = status
	}

	return template.HTML(fmt.Sprintf(`<span class="badge %s">%s</span>`, class, text))
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func add(a, b int) int {
	return a + b
}

func sub(a, b int) int {
	return a - b
}

func eq(a, b interface{}) bool {
	return a == b
}

func gt(a, b int) bool {
	return a > b
}

func lt(a, b int) bool {
	return a < b
}

// Flash message management

type FlashMessage struct {
	Type    string
	Message string
}

const flashCookieName = "flash"

// SetFlash sets a flash message
func SetFlash(w http.ResponseWriter, typ, message string) {
	flash := FlashMessage{
		Type:    typ,
		Message: message,
	}

	data, _ := json.Marshal(flash)
	encoded := base64.URLEncoding.EncodeToString(data)

	http.SetCookie(w, &http.Cookie{
		Name:     flashCookieName,
		Value:    encoded,
		Path:     "/",
		MaxAge:   0, // Session cookie
		HttpOnly: true,
		Secure:   false, // Set to true in production with HTTPS
		SameSite: http.SameSiteLaxMode,
	})
}

// GetFlashes retrieves and clears flash messages
func GetFlashes(r *http.Request) []FlashMessage {
	cookie, err := r.Cookie(flashCookieName)
	if err != nil {
		return nil
	}

	data, err := base64.URLEncoding.DecodeString(cookie.Value)
	if err != nil {
		return nil
	}

	var flash FlashMessage
	if err := json.Unmarshal(data, &flash); err != nil {
		return nil
	}

	return []FlashMessage{flash}
}

// CSRF protection

const csrfCookieName = "csrf_token"

// GenerateCSRF generates a CSRF token
func GenerateCSRF(w http.ResponseWriter, r *http.Request) string {
	// Check if we already have a token
	if cookie, err := r.Cookie(csrfCookieName); err == nil && cookie.Value != "" {
		return cookie.Value
	}

	// Generate new token
	token := generateRandomToken(32)

	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   3600 * 24, // 24 hours
		HttpOnly: true,
		Secure:   false, // Set to true in production with HTTPS
		SameSite: http.SameSiteLaxMode,
	})

	return token
}

// ValidateCSRF validates a CSRF token
func ValidateCSRF(r *http.Request, token string) bool {
	cookie, err := r.Cookie(csrfCookieName)
	if err != nil {
		return false
	}

	return cookie.Value == token && token != ""
}

// Session management

const sessionCookieName = "session"

// SetSessionCookie sets the session cookie
func SetSessionCookie(w http.ResponseWriter, sessionToken string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionToken,
		Path:     "/",
		MaxAge:   3600 * 24 * 7, // 7 days
		HttpOnly: true,
		Secure:   false, // Set to true in production with HTTPS
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie clears the session cookie
func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
	})
}

// GetSessionToken gets the session token from cookie
func GetSessionToken(r *http.Request) string {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// SetSessionValue sets a value in session (using cookies for now)
func SetSessionValue(w http.ResponseWriter, key, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "sess_" + key,
		Value:    value,
		Path:     "/",
		MaxAge:   3600,
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
	})
}

// Helper to generate random tokens
func generateRandomToken(length int) string {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based token
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return base64.URLEncoding.EncodeToString(b)[:length]
}
