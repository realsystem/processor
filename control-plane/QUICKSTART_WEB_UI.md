# Quick Start Guide - Processor Control Plane Web UI

## Prerequisites

- Go 1.21 or later
- SQLite3

## First Time Setup

### 1. Navigate to the project directory

```bash
cd /Users/segorov/Projects/processor/control-plane
```

### 2. Run database migrations

```bash
go run ./cmd/server/main.go -migrate
```

### 3. Start the server

```bash
go run ./cmd/server/main.go
```

Or with environment variables:

```bash
ADMIN_EMAIL=admin@example.com ADMIN_PASSWORD=admin123 \
go run ./cmd/server/main.go
```

The server will start on `http://localhost:8080`

## Using the Web UI

### Create an Account

1. Open your browser to `http://localhost:8080`
2. You'll be redirected to the login page
3. Click "Sign up" to create a new account
4. Fill in your email and password (minimum 8 characters)
5. Click "Create Account"

### Login

1. Navigate to `http://localhost:8080/login`
2. Enter your email and password
3. Click "Sign In"

### Dashboard

After login, you'll see your dashboard with:
- Statistics (total jobs, queued, running, succeeded)
- Recent jobs list
- Quick actions panel
- Account information

### Create a Job

1. Click "New Job" or navigate to `/jobs/new`
2. Select a job type:
   - `noop` - No-operation (for testing)
   - `sleep` - Sleep for a duration
   - `echo_metadata` - Echo back the input
   - `ollama_chat` - Chat with Ollama
3. Enter input metadata as JSON (optional)
4. Set priority weight (-100 to 100, default 0)
5. Set max retry attempts (1-10, default 3)
6. Choose notification preferences
7. Click "Create Job"

### Manage API Keys

1. Navigate to `/api-keys`
2. Click "Create API Key"
3. Enter a descriptive name
4. Click "Create Key"
5. **Important**: Copy the key immediately - you won't see it again!
6. Use the key in API requests:
   ```bash
   curl -H "Authorization: Bearer pk_xxxxx_yyyyy" \
     http://localhost:8080/api/v1/jobs
   ```

### Configure Webhooks

1. Navigate to `/webhooks`
2. Click "Create Webhook"
3. Enter the target URL where events will be sent
4. Enter a secret for HMAC signing
5. Select which events to subscribe to:
   - `job.succeeded`
   - `job.failed`
   - `job.dead_letter`
6. Check "Active" to enable immediately
7. Click "Create Webhook"

### View Job Details

1. Go to `/jobs` to see all your jobs
2. Click "View" on any job
3. See detailed information:
   - Status and timeline
   - Input/output metadata
   - Error messages (if failed)
   - Execution attempts

## Admin Features

If you created an admin user (via `ADMIN_EMAIL` env var), you can access:

- **Admin Dashboard** (`/admin`) - System-wide statistics
- **All Jobs** (`/admin/jobs`) - View jobs from all users
- **Workers** (`/admin/workers`) - Monitor connected workers
- **Users** (`/admin/users`) - User management (coming soon)

## Environment Variables

Configure the server using environment variables:

```bash
# Database
DATABASE_PATH=/path/to/database.db

# HTTP Server
HTTP_PORT=8080

# Admin User (bootstrapped on first run)
ADMIN_EMAIL=admin@example.com
ADMIN_PASSWORD=securepassword

# Worker Authentication
WORKER_TOKEN=your-worker-token

# Email (optional)
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_USER=your-email@gmail.com
SMTP_PASS=your-app-password
SMTP_FROM=noreply@example.com

# Job Settings
LEASE_DURATION_SECONDS=300
```

## Example Workflow

### Complete Job Lifecycle

1. **Create a job** via web UI:
   - Job Type: `echo_metadata`
   - Input: `{"message": "Hello, world!"}`
   - Priority: 0
   - Max Attempts: 3

2. **Worker polls** and picks up the job

3. **Worker processes** the job:
   ```bash
   # Worker sees the job
   Job ID: job_abc123
   Type: echo_metadata
   Input: {"message": "Hello, world!"}
   ```

4. **Worker completes** the job with result:
   ```json
   {
     "echoed": {"message": "Hello, world!"},
     "processed_at": "2026-04-15T12:00:00Z"
   }
   ```

5. **View results** in web UI:
   - Navigate to job detail page
   - See status: "Succeeded"
   - View result metadata
   - Check execution timeline

## API Access

The web UI and API work together:

### Using Session (from browser)
```javascript
// JavaScript fetch from logged-in browser
fetch('/api/v1/jobs', {
  credentials: 'include'  // Include session cookie
})
.then(r => r.json())
.then(data => console.log(data));
```

### Using API Key (from scripts)
```bash
# Create a job via API
curl -X POST http://localhost:8080/api/v1/jobs \
  -H "Authorization: Bearer pk_xxxxx_yyyyy" \
  -H "Content-Type: application/json" \
  -d '{
    "job_type": "noop",
    "priority_weight": 0,
    "max_attempts": 3
  }'
```

## Troubleshooting

### Can't access web UI
- Check that server is running: `ps aux | grep server`
- Verify port 8080 is not in use: `lsof -i :8080`
- Check logs for errors

### Login not working
- Verify user exists in database: 
  ```bash
  sqlite3 processor.db "SELECT email FROM users;"
  ```
- Check password (minimum 8 characters)
- Clear browser cookies and try again

### Templates not loading
- Verify templates directory exists: `ls -la templates/`
- Check file permissions
- Look for errors in server logs

### Session expired
- Sessions last 7 days
- Clear cookie and login again
- Check server time is correct

## Next Steps

1. **Set up a worker** to process jobs
2. **Configure webhooks** to receive notifications
3. **Generate API keys** for programmatic access
4. **Explore admin features** if you're an admin
5. **Monitor jobs** in real-time

## Documentation

- **Full Implementation**: See `WEB_UI_IMPLEMENTATION.md`
- **API Documentation**: See API docs
- **Worker Setup**: See worker documentation

## Support

For issues or questions:
1. Check the implementation docs
2. Review server logs
3. Verify configuration
4. Check database connectivity
