# Security Model

## Overview

This document describes the security architecture, threat model, and security controls for the Processor platform.

## Threat Model

### Assets

1. **User Data**: Email addresses, password hashes, API keys, job metadata
2. **Job Queue**: Pending work, execution results
3. **Webhook Secrets**: HMAC signing keys
4. **Worker Tokens**: Authentication credentials for data-plane workers
5. **Session Tokens**: Browser session identifiers

### Actors

**Legitimate**:
- End users (job submitters)
- System administrators
- Data-plane workers

**Threat Actors**:
- Unauthenticated attackers (internet)
- Authenticated malicious users
- Compromised user accounts
- Compromised API keys
- Compromised worker tokens

### Threats In Scope

1. **Unauthorized Access**
   - Unauthenticated access to protected resources
   - Privilege escalation (user → admin)
   - Horizontal privilege escalation (user A accessing user B's jobs)

2. **Injection Attacks**
   - SQL injection
   - Cross-Site Scripting (XSS)
   - Command injection (if executing shell commands)
   - Template injection

3. **Cross-Site Request Forgery (CSRF)**
   - State-changing operations via forged requests

4. **Authentication & Session Attacks**
   - Password guessing / brute force
   - Session hijacking
   - Session fixation
   - API key theft

5. **Webhook Attacks**
   - Webhook URL hijacking
   - Payload tampering
   - Replay attacks

6. **Data Exposure**
   - Accidental exposure of sensitive metadata
   - Leaking of API keys or tokens
   - Directory traversal

### Threats Out of Scope

1. **Infrastructure Level**
   - OS-level vulnerabilities (assumes hardened Ubuntu)
   - TLS/SSL configuration (assumes nginx properly configured)
   - DDoS attacks (assumes Cloudflare or similar)
   - Physical access to servers

2. **Social Engineering**
   - Phishing attacks on users
   - Social engineering of administrators

3. **Advanced Persistent Threats**
   - State-sponsored attacks
   - Zero-day exploits
   - Supply chain attacks (assumes trusted Go dependencies)

## Security Controls

### 1. Authentication

#### Password-Based Authentication

**Hashing**:
- Algorithm: bcrypt
- Cost factor: 10 (2^10 iterations)
- Salt: automatically generated per password

**Registration**:
- Email validation (format check)
- Password minimum length: 8 characters
- No password complexity requirements (length is more important)
- Rate limiting: 5 attempts per 15 minutes per IP

**Login**:
- Email + password verification
- Rate limiting: 5 attempts per 15 minutes per IP
- Timing-safe comparison for password hashes
- Account lockout after 10 failed attempts in 1 hour (optional, not implemented initially)

#### Session Management

**Session Creation**:
- Cryptographically random session ID (32 bytes)
- Stored in database with expiration
- Associated with user ID

**Session Cookies**:
```
Set-Cookie: session=<token>; HttpOnly; Secure; SameSite=Lax; Max-Age=86400; Path=/
```

Flags explained:
- `HttpOnly`: Prevents JavaScript access (XSS mitigation)
- `Secure`: Only sent over HTTPS (production)
- `SameSite=Lax`: CSRF protection
- `Max-Age=86400`: 24 hour expiration
- `Path=/`: Available to entire application

**Session Validation**:
- Every request checks session token
- Validates expiration
- Updates `last_seen_at` timestamp
- Extends expiration on activity

#### API Key Authentication

**Key Generation**:
- Format: `pk_<16 hex chars>_<32 hex chars>`
- Prefix (`pk_<16 hex>`) for identification
- Full key hashed with bcrypt before storage
- Only shown once at creation

**Key Validation**:
- Extracted from `Authorization: Bearer <key>` header
- Prefix used to find key record
- Full key compared against hash
- Timing-safe comparison

**Key Revocation**:
- Soft delete (set `revoked_at` timestamp)
- Revoked keys fail authentication

#### Worker Authentication

**Worker Tokens**:
- Pre-shared secret or generated at worker registration
- Format: `wk_<32 hex chars>`
- Hashed with bcrypt before storage
- Sent in `X-Worker-Token` header

**Worker Registration** (optional future feature):
- Admin-only endpoint to create workers
- Returns token once
- Worker must store token securely

### 2. Authorization

#### Role-Based Access Control (RBAC)

**Roles**:
- `user`: Regular user (default)
- `admin`: Administrator

**Permission Model**:

| Resource | User | Admin |
|----------|------|-------|
| Own jobs | CRUD | CRUD |
| Other users' jobs | - | Read |
| Own API keys | CRUD | CRUD |
| Other users' API keys | - | Read, Revoke |
| Own webhooks | CRUD | CRUD |
| Workers | - | CRUD |
| Audit log | - | Read |

**Enforcement**:
- Every protected endpoint checks authentication
- Ownership checks for user-scoped resources
- Admin checks for admin-scoped resources

### 3. Input Validation

**Validation Strategy**:
- Whitelist approach (validate expected inputs)
- Reject unexpected fields
- Type checking
- Length limits
- Format validation (email, URLs)

**Specific Validations**:

```go
// Email
- Must match regex: ^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$
- Max length: 255 characters

// Job Type
- Must be one of: noop, sleep, echo_metadata, ollama_chat
- Max length: 64 characters

// Priority Weight
- Integer range: -100 to 100

// Input Metadata
- Valid JSON object
- Max size: 1MB

// Webhook URL
- Must be HTTPS (production)
- Must be valid URL format
- Max length: 2048 characters
```

**SQL Injection Prevention**:
- Always use parameterized queries
- Never concatenate user input into SQL
- Example:
  ```go
  // GOOD
  db.QueryRow("SELECT * FROM users WHERE email = ?", email)
  
  // BAD (never do this)
  db.QueryRow("SELECT * FROM users WHERE email = '" + email + "'")
  ```

### 4. Output Encoding

**HTML Templates**:
- Use Go's `html/template` package (auto-escapes by default)
- Never use `text/template` for HTML
- Explicitly mark trusted content with `template.HTML` only when necessary

**JSON Responses**:
- Use `encoding/json` standard library
- Automatic escaping of special characters

**Example**:
```go
// Auto-escaped in template
<p>User email: {{.Email}}</p>

// Renders as: <p>User email: user@example.com</p>
// Even if Email contains: <script>alert('xss')</script>
// Renders as: <p>User email: &lt;script&gt;alert(&#39;xss&#39;)&lt;/script&gt;</p>
```

### 5. CSRF Protection

**Implementation**:
- CSRF token in session
- Token included in all state-changing forms
- Token validated on POST/PUT/DELETE requests
- Double-submit cookie pattern (alternative)

**Form Example**:
```html
<form method="POST" action="/jobs">
  <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
  <!-- other fields -->
</form>
```

**Middleware**:
```go
func CSRFMiddleware(next http.Handler) http.Handler {
  return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if r.Method == "POST" || r.Method == "PUT" || r.Method == "DELETE" {
      sessionToken := getSessionToken(r)
      formToken := r.FormValue("csrf_token")
      if !hmac.Equal([]byte(sessionToken), []byte(formToken)) {
        http.Error(w, "CSRF validation failed", http.StatusForbidden)
        return
      }
    }
    next.ServeHTTP(w, r)
  })
}
```

**Exemptions**:
- JSON API endpoints (use CORS instead)
- Worker API endpoints (use worker token auth)

### 6. Webhook Security

**Request Signing**:
- Algorithm: HMAC-SHA256
- Key: User-provided secret (hashed in database)
- Message: Full request body (JSON)
- Signature sent in `X-Processor-Signature` header

**Signature Generation**:
```go
func signWebhook(secret string, payload []byte) string {
  h := hmac.New(sha256.New, []byte(secret))
  h.Write(payload)
  return "sha256=" + hex.EncodeToString(h.Sum(nil))
}
```

**Recipient Verification**:
```go
func verifyWebhook(secret string, payload []byte, signature string) bool {
  expected := signWebhook(secret, payload)
  return hmac.Equal([]byte(expected), []byte(signature))
}
```

**Replay Protection**:
- Include timestamp in payload
- Recipients should reject old requests (e.g., >5 minutes old)

### 7. Rate Limiting

**Endpoints**:

| Endpoint | Limit | Window | Scope |
|----------|-------|--------|-------|
| /register | 5 | 15 min | IP |
| /login | 5 | 15 min | IP |
| /api/v1/* | 100 | 1 min | User |
| /api/v1/worker/* | 1000 | 1 min | Worker |

**Implementation**:
- In-memory sliding window counter
- Simple map with mutex for low-scale deployment
- Redis-backed for multi-instance deployment (future)

**Response**:
```
HTTP/1.1 429 Too Many Requests
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 0
X-RateLimit-Reset: 1713178200
Retry-After: 60

{"success": false, "error": {"code": "rate_limited", "message": "Too many requests"}}
```

### 8. Audit Logging

**Logged Events**:
- User registration
- User login (success and failure)
- API key creation and revocation
- Webhook creation and deletion
- Job submission
- Admin actions (user list, job requeue, etc.)
- Worker registration

**Log Format**:
```go
type AuditLogEntry struct {
  ID          string    `json:"id"`
  Timestamp   time.Time `json:"timestamp"`
  ActorType   string    `json:"actor_type"`   // user, worker, system
  ActorID     string    `json:"actor_id"`
  EventType   string    `json:"event_type"`   // user.login, job.created, etc.
  ObjectType  string    `json:"object_type"`  // job, api_key, webhook
  ObjectID    string    `json:"object_id"`
  Metadata    JSON      `json:"metadata"`     // Additional context
  IPAddress   string    `json:"ip_address"`
  UserAgent   string    `json:"user_agent"`
}
```

**Storage**:
- Stored in SQLite `audit_log` table
- Indexed by timestamp, actor_id, event_type
- Retention: 90 days (configurable)

### 9. Secure Configuration

**Environment Variables**:
```bash
# Required
DATABASE_PATH=/var/lib/processor/processor.db
SESSION_SECRET=<random 32+ chars>  # for session encryption
WORKER_TOKEN=<random 32+ chars>    # for worker auth

# Optional
ADMIN_EMAIL=admin@example.com      # bootstrap admin user
BASE_URL=https://processor.example.com
SMTP_HOST=smtp.example.com
SMTP_PORT=587
SMTP_USER=notifications@example.com
SMTP_PASS=<password>
```

**Secret Generation**:
```bash
# Generate random secret
openssl rand -hex 32

# Or with Go
go run -c 'package main; import ("crypto/rand"; "encoding/hex"; "fmt"); func main() { b := make([]byte, 32); rand.Read(b); fmt.Println(hex.EncodeToString(b)) }'
```

**File Permissions**:
```bash
# Database file
chmod 600 /var/lib/processor/processor.db
chown processor:processor /var/lib/processor/processor.db

# Environment file
chmod 600 /etc/processor/.env
chown root:root /etc/processor/.env
```

### 10. Transport Security

**HTTPS Only** (Production):
- nginx handles TLS termination
- Redirect HTTP to HTTPS
- HSTS header: `Strict-Transport-Security: max-age=31536000; includeSubDomains`
- Modern TLS only (TLS 1.2+)

**nginx Configuration**:
```nginx
server {
  listen 443 ssl http2;
  server_name processor.example.com;

  ssl_certificate /etc/letsencrypt/live/processor.example.com/fullchain.pem;
  ssl_certificate_key /etc/letsencrypt/live/processor.example.com/privkey.pem;
  ssl_protocols TLSv1.2 TLSv1.3;
  ssl_ciphers HIGH:!aNULL:!MD5;

  add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;
  add_header X-Content-Type-Options "nosniff" always;
  add_header X-Frame-Options "DENY" always;
  add_header X-XSS-Protection "1; mode=block" always;

  location / {
    proxy_pass http://localhost:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
  }
}
```

### 11. Database Security

**SQLite Configuration**:
```go
db, err := sql.Open("sqlite3", "file:processor.db?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on")
```

**Prepared Statements**:
- Always use for user input
- Prevents SQL injection

**Sensitive Data**:
- Passwords: bcrypt hashed, never plaintext
- API keys: bcrypt hashed, prefix + hash stored
- Worker tokens: bcrypt hashed
- Webhook secrets: bcrypt hashed
- Session tokens: hashed (optional, can store plaintext if encrypted at rest)

## Security Checklist

**Deployment**:
- [ ] HTTPS enabled with valid certificate
- [ ] SESSION_SECRET is random and secret
- [ ] WORKER_TOKEN is random and secret
- [ ] Database file has correct permissions (600)
- [ ] Environment file has correct permissions (600)
- [ ] nginx security headers configured
- [ ] Rate limiting enabled
- [ ] Audit logging enabled
- [ ] Regular database backups configured

**Code Review**:
- [ ] All SQL queries use parameterized statements
- [ ] All HTML templates use `html/template`
- [ ] All password comparisons use timing-safe functions
- [ ] CSRF protection on all state-changing forms
- [ ] Input validation on all endpoints
- [ ] Authorization checks on all protected endpoints
- [ ] Webhook signatures verified correctly
- [ ] API keys never logged or returned in responses

## Incident Response

**Suspected Breach**:
1. **Immediate**: Revoke all API keys and worker tokens
2. **Investigate**: Review audit logs for suspicious activity
3. **Rotate**: Change SESSION_SECRET and WORKER_TOKEN
4. **Notify**: Email affected users
5. **Patch**: Fix vulnerability
6. **Monitor**: Watch for continued suspicious activity

**Compromised User Account**:
1. Disable user account
2. Revoke all API keys
3. Invalidate all sessions
4. Contact user via backup email
5. Require password reset

**Compromised Worker**:
1. Revoke worker token
2. Review all jobs executed by worker
3. Check for malicious job results
4. Generate new worker token
5. Update worker configuration

## Future Enhancements

**Planned**:
- Two-factor authentication (TOTP)
- IP whitelisting for API keys
- OAuth2 support (Google, GitHub)
- Automated security scanning (Dependabot, Snyk)
- Web Application Firewall (WAF) rules
- Content Security Policy (CSP) headers

**Considered**:
- End-to-end encryption of job metadata
- Hardware security module (HSM) for key storage
- Database encryption at rest
- Anomaly detection (unusual job patterns)
