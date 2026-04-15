# Web UI Implementation

This document describes the complete web UI implementation for the Processor Control Plane.

## Overview

The web UI provides a full-featured browser interface for managing jobs, API keys, webhooks, and user accounts. It runs alongside the existing JSON API and shares the same backend services.

## Architecture

### Components

1. **Templates** (`/templates/`)
   - Server-rendered HTML using Go's `html/template`
   - Base layout with header and flash messages
   - Page-specific templates for all features

2. **Web Handlers** (`/internal/web/handlers.go`)
   - Handles HTTP requests for the web UI
   - Delegates business logic to existing services
   - Manages form validation and user feedback

3. **Template Helpers** (`/internal/web/helpers.go`)
   - Template rendering utilities
   - Flash message management
   - CSRF protection
   - Template functions (formatTime, statusBadge, etc.)

4. **Static Assets** (`/static/`)
   - CSS styling (`/static/css/main.css`)
   - JavaScript enhancements (`/static/js/main.js`)

## Features Implemented

### Authentication
- **Login** (`/login`) - Email/password authentication with session cookies
- **Register** (`/register`) - New user registration
- **Logout** (`/logout`) - Session termination

### User Dashboard
- **Dashboard** (`/`) - Overview with stats and recent jobs
- **Profile** (`/profile`) - User account information

### Jobs Management
- **List Jobs** (`/jobs`) - Paginated job listing with status filter
- **Create Job** (`/jobs/new`) - Form to submit new jobs
- **Job Details** (`/jobs/:id`) - Detailed job view with attempts history

### API Keys
- **List API Keys** (`/api-keys`) - View and manage API keys
- **Create API Key** - Generate new API key (shown once)
- **Revoke API Key** - Revoke existing API key

### Webhooks
- **List Webhooks** (`/webhooks`) - View configured webhooks
- **Create Webhook** - Add new webhook endpoint
- **Delete Webhook** - Remove webhook

### Admin Features
- **Admin Dashboard** (`/admin`) - System-wide statistics
- **All Jobs** (`/admin/jobs`) - View jobs across all users
- **Workers** (`/admin/workers`) - Monitor data-plane workers
- **Users** (`/admin/users`) - User management (placeholder)

## Session Management

- Sessions are stored in the database
- Session tokens are stored in HTTP-only cookies
- Sessions expire after 7 days of inactivity
- CSRF protection on all forms

## Security Features

1. **CSRF Protection**: All forms include CSRF tokens
2. **Session Cookies**: HTTP-only, secure in production
3. **Input Validation**: Server-side validation on all forms
4. **Admin-Only Routes**: Middleware checks for admin privileges
5. **XSS Protection**: Templates auto-escape HTML

## Template Functions

Custom template functions available in all templates:

- `formatTime` - Human-readable relative time (e.g., "2 hours ago")
- `formatJSON` - Pretty-print JSON with indentation
- `statusBadge` - Colored badge for job status
- `truncate` - Truncate strings with ellipsis
- `add`, `sub` - Math operations
- `eq`, `gt`, `lt` - Comparison operations

## Flash Messages

Flash messages provide user feedback:

- **Success** - Green banner for successful operations
- **Error** - Red banner for errors
- **Info** - Blue banner for informational messages
- **Warning** - Yellow banner for warnings

Messages auto-dismiss after 5 seconds (except errors).

## Styling

The UI uses a custom CSS design system with:

- Responsive layout (mobile-friendly)
- Consistent color palette
- Reusable components (cards, buttons, forms, tables)
- Status badges with semantic colors
- Empty states with friendly messages

## Running the Server

```bash
# Build and run
cd control-plane
go run ./cmd/server/main.go

# With migrations
go run ./cmd/server/main.go -migrate
```

The web UI is available at `http://localhost:8080/`

## Development

### Adding a New Page

1. Create template in `/templates/`
2. Add handler in `/internal/web/handlers.go`
3. Register route in `/cmd/server/main.go`
4. Add navigation link in `/templates/partials/header.html`

### Template Structure

All pages extend `base.html`:

```html
{{define "content"}}
<div class="page-header">
    <h1 class="page-title">Page Title</h1>
</div>

<div class="card">
    <!-- Your content here -->
</div>
{{end}}
```

### Form Example

```html
<form method="POST" action="/endpoint">
    <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
    
    <div class="form-group">
        <label for="field" class="form-label form-label-required">Field</label>
        <input type="text" id="field" name="field" class="form-control" required>
        {{if .Errors.Field}}<span class="form-error">{{.Errors.Field}}</span>{{end}}
    </div>
    
    <button type="submit" class="btn btn-primary">Submit</button>
</form>
```

## Integration with API

The web UI and JSON API coexist:

- **Web UI**: Uses session cookies, renders HTML
- **JSON API**: Uses API keys or session cookies, returns JSON
- **Shared Services**: Both use the same auth/jobs/webhooks services
- **Middleware**: `AuthMiddleware.Session()` for web, `AuthMiddleware.APIAuth()` for API

## Browser Support

- Modern browsers (Chrome, Firefox, Safari, Edge)
- JavaScript progressive enhancement (works without JS)
- CSS Grid and Flexbox for layout

## Future Enhancements

Potential improvements:

1. **User Management** - Complete admin user CRUD operations
2. **Job Cancellation** - Allow users to cancel pending jobs
3. **Real-time Updates** - WebSocket support for live job status
4. **Bulk Operations** - Select and operate on multiple jobs
5. **Advanced Filtering** - Date ranges, job types, search
6. **Email Preferences** - Configure notification settings
7. **Usage Metrics** - Charts and graphs for job statistics
8. **Dark Mode** - Theme switcher

## Files Created

### Templates (16 files)
- `templates/base.html` - Base layout
- `templates/login.html` - Login page
- `templates/register.html` - Registration page
- `templates/dashboard.html` - User dashboard
- `templates/profile.html` - User profile
- `templates/jobs/list.html` - Jobs listing
- `templates/jobs/new.html` - Create job form
- `templates/jobs/detail.html` - Job details
- `templates/api-keys/list.html` - API keys management
- `templates/webhooks/list.html` - Webhooks management
- `templates/admin/dashboard.html` - Admin overview
- `templates/admin/jobs.html` - All jobs (admin)
- `templates/admin/workers.html` - Workers list
- `templates/admin/users.html` - Users list
- `templates/partials/header.html` - Navigation header
- `templates/partials/flash.html` - Flash messages

### Go Packages (2 files)
- `internal/web/handlers.go` - Web request handlers
- `internal/web/helpers.go` - Template rendering and utilities

### Modified Files (2 files)
- `cmd/server/main.go` - Added web routes and template renderer
- `internal/jobs/jobs.go` - Updated List() to support admin view

## Testing

To test the implementation:

1. **Start the server**: `go run ./cmd/server/main.go`
2. **Create an account**: Navigate to http://localhost:8080/register
3. **Explore features**:
   - Create jobs
   - Generate API keys
   - Configure webhooks
   - View job details
   - Check admin panel (if admin user)

## Error Handling

- Form validation errors shown inline
- Flash messages for operation feedback
- Empty states for lists with no data
- Graceful fallbacks for missing data
- 404 handling for not found resources
- Permission checks for admin routes
