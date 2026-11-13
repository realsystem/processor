# API Reference

## Authentication

The API supports two authentication methods:

### 1. Session Cookies (Web UI)
After logging in via `/login`, a session cookie is set. All subsequent requests include this cookie.

### 2. API Keys (Programmatic Access)
Include API key in the `Authorization` header:
```
Authorization: Bearer pk_1234567890abcdef_hash
```

## Base URL

**Local Development**: `http://localhost:8080`  
**Production**: `https://your-domain.com`

## Response Format

All API responses are JSON:

```json
{
  "success": true,
  "data": { ... }
}
```

Error responses:

```json
{
  "success": false,
  "error": {
    "code": "invalid_input",
    "message": "Job type is required"
  }
}
```

## API Endpoints

### Health

#### GET /api/v1/health

Check service health.

**Response**:
```json
{
  "success": true,
  "data": {
    "status": "healthy",
    "timestamp": "2026-04-15T10:30:00Z",
    "version": "1.0.0"
  }
}
```

### User

#### GET /api/v1/me

Get current user information.

**Authentication**: Required

**Response**:
```json
{
  "success": true,
  "data": {
    "id": "usr_123",
    "email": "user@example.com",
    "plan_tier": "free",
    "is_admin": false,
    "created_at": "2026-04-01T12:00:00Z"
  }
}
```

### Jobs

#### POST /api/v1/jobs

Create a new job.

**Authentication**: Required

**Request Body**:
```json
{
  "job_type": "echo_metadata",
  "input_metadata": {
    "message": "Hello, world!"
  },
  "priority_weight": 10,
  "max_attempts": 3,
  "notify_email": true,
  "notify_webhook": false
}
```

**Fields**:
- `job_type` (string, required): Type of job to execute
- `input_metadata` (object, required): Job-specific input data
- `priority_weight` (integer, optional): Priority (higher = more urgent), default: 0
- `max_attempts` (integer, optional): Maximum retry attempts, default: 3
- `notify_email` (boolean, optional): Send email on completion, default: false
- `notify_webhook` (boolean, optional): Trigger webhook on completion, default: false

**Response**:
```json
{
  "success": true,
  "data": {
    "id": "job_abc123",
    "user_id": "usr_123",
    "job_type": "echo_metadata",
    "status": "queued",
    "priority_weight": 10,
    "input_metadata": {
      "message": "Hello, world!"
    },
    "created_at": "2026-04-15T10:30:00Z",
    "max_attempts": 3,
    "attempt_count": 0
  }
}
```

#### GET /api/v1/jobs

List jobs for the current user.

**Authentication**: Required

**Query Parameters**:
- `status` (string, optional): Filter by status (queued, running, succeeded, failed, dead_letter)
- `limit` (integer, optional): Max results, default: 50, max: 100
- `offset` (integer, optional): Pagination offset, default: 0

**Response**:
```json
{
  "success": true,
  "data": {
    "jobs": [
      {
        "id": "job_abc123",
        "job_type": "echo_metadata",
        "status": "succeeded",
        "priority_weight": 10,
        "created_at": "2026-04-15T10:30:00Z",
        "completed_at": "2026-04-15T10:31:00Z",
        "attempt_count": 1
      }
    ],
    "total": 1,
    "limit": 50,
    "offset": 0
  }
}
```

#### GET /api/v1/jobs/:id

Get job details.

**Authentication**: Required (must own job or be admin)

**Response**:
```json
{
  "success": true,
  "data": {
    "id": "job_abc123",
    "user_id": "usr_123",
    "job_type": "echo_metadata",
    "status": "succeeded",
    "priority_weight": 10,
    "input_metadata": {
      "message": "Hello, world!"
    },
    "result_metadata": {
      "echo": "Hello, world!",
      "processed_at": "2026-04-15T10:31:00Z"
    },
    "error_text": null,
    "created_at": "2026-04-15T10:30:00Z",
    "queued_at": "2026-04-15T10:30:00Z",
    "started_at": "2026-04-15T10:30:30Z",
    "completed_at": "2026-04-15T10:31:00Z",
    "worker_id": "worker_1",
    "attempt_count": 1,
    "max_attempts": 3,
    "attempts": [
      {
        "attempt_number": 1,
        "worker_id": "worker_1",
        "started_at": "2026-04-15T10:30:30Z",
        "finished_at": "2026-04-15T10:31:00Z",
        "status": "succeeded"
      }
    ]
  }
}
```

### API Keys

#### POST /api/v1/api-keys

Create a new API key.

**Authentication**: Required (session cookie only, not API key)

**Request Body**:
```json
{
  "name": "Production API Key"
}
```

**Response**:
```json
{
  "success": true,
  "data": {
    "id": "key_xyz789",
    "name": "Production API Key",
    "key": "pk_1234567890abcdef_9876543210fedcba",
    "key_prefix": "pk_1234567890abcdef",
    "created_at": "2026-04-15T10:30:00Z"
  }
}
```

**Important**: The `key` field is only returned once. Store it securely.

#### GET /api/v1/api-keys

List API keys for the current user.

**Authentication**: Required (session cookie only)

**Response**:
```json
{
  "success": true,
  "data": {
    "api_keys": [
      {
        "id": "key_xyz789",
        "name": "Production API Key",
        "key_prefix": "pk_1234567890abcdef",
        "created_at": "2026-04-15T10:30:00Z",
        "revoked_at": null
      }
    ]
  }
}
```

#### POST /api/v1/api-keys/:id/revoke

Revoke an API key.

**Authentication**: Required (session cookie only)

**Response**:
```json
{
  "success": true,
  "data": {
    "revoked": true
  }
}
```

### Webhooks

#### POST /api/v1/webhooks

Create a webhook.

**Authentication**: Required

**Request Body**:
```json
{
  "target_url": "https://example.com/webhooks/processor",
  "secret": "your-webhook-secret",
  "subscribed_events": ["job.succeeded", "job.failed", "job.dead_letter"],
  "is_active": true
}
```

**Response**:
```json
{
  "success": true,
  "data": {
    "id": "hook_456",
    "user_id": "usr_123",
    "target_url": "https://example.com/webhooks/processor",
    "is_active": true,
    "subscribed_events": ["job.succeeded", "job.failed", "job.dead_letter"],
    "created_at": "2026-04-15T10:30:00Z"
  }
}
```

**Note**: The `secret` is stored hashed and not returned.

#### GET /api/v1/webhooks

List webhooks for the current user.

**Authentication**: Required

**Response**:
```json
{
  "success": true,
  "data": {
    "webhooks": [
      {
        "id": "hook_456",
        "target_url": "https://example.com/webhooks/processor",
        "is_active": true,
        "subscribed_events": ["job.succeeded", "job.failed"],
        "created_at": "2026-04-15T10:30:00Z"
      }
    ]
  }
}
```

#### DELETE /api/v1/webhooks/:id

Delete a webhook.

**Authentication**: Required (must own webhook)

**Response**:
```json
{
  "success": true,
  "data": {
    "deleted": true
  }
}
```

## Worker API

These endpoints are used by data-plane workers. Authentication requires a worker token in the `X-Worker-Token` header.

### POST /api/v1/worker/poll

Poll for the next available job.

**Authentication**: Worker token required

**Request Body**:
```json
{
  "worker_id": "worker_1",
  "capabilities": ["noop", "sleep", "echo_metadata", "ollama_chat"]
}
```

**Response** (job available):
```json
{
  "success": true,
  "data": {
    "job": {
      "id": "job_abc123",
      "job_type": "echo_metadata",
      "input_metadata": {
        "message": "Hello, world!"
      },
      "max_attempts": 3,
      "attempt_count": 0,
      "lease_expires_at": "2026-04-15T10:35:00Z"
    }
  }
}
```

**Response** (no job available):
```json
{
  "success": true,
  "data": {
    "job": null
  }
}
```

### POST /api/v1/worker/heartbeat

Send heartbeat to extend lease.

**Authentication**: Worker token required

**Request Body**:
```json
{
  "worker_id": "worker_1",
  "job_id": "job_abc123"
}
```

**Response**:
```json
{
  "success": true,
  "data": {
    "lease_extended": true,
    "lease_expires_at": "2026-04-15T10:40:00Z"
  }
}
```

### POST /api/v1/worker/jobs/:id/start

Mark job as running.

**Authentication**: Worker token required

**Request Body**:
```json
{
  "worker_id": "worker_1"
}
```

**Response**:
```json
{
  "success": true,
  "data": {
    "status": "running",
    "started_at": "2026-04-15T10:30:30Z"
  }
}
```

### POST /api/v1/worker/jobs/:id/complete

Mark job as completed successfully.

**Authentication**: Worker token required

**Request Body**:
```json
{
  "worker_id": "worker_1",
  "result_metadata": {
    "echo": "Hello, world!",
    "processed_at": "2026-04-15T10:31:00Z"
  }
}
```

**Response**:
```json
{
  "success": true,
  "data": {
    "status": "succeeded",
    "completed_at": "2026-04-15T10:31:00Z"
  }
}
```

### POST /api/v1/worker/jobs/:id/fail

Mark job as failed.

**Authentication**: Worker token required

**Request Body**:
```json
{
  "worker_id": "worker_1",
  "error_text": "Connection timeout",
  "should_retry": true
}
```

**Response**:
```json
{
  "success": true,
  "data": {
    "status": "failed",
    "will_retry": true,
    "attempt_count": 1,
    "max_attempts": 3
  }
}
```

## Webhook Payload Format

When a webhook is triggered, the control plane sends an HTTP POST to the configured `target_url`.

**Headers**:
```
Content-Type: application/json
X-Processor-Signature: sha256=a1b2c3d4e5f6...
X-Processor-Event: job.succeeded
X-Processor-Delivery-ID: del_123
```

**Body**:
```json
{
  "event": "job.succeeded",
  "timestamp": "2026-04-15T10:31:00Z",
  "data": {
    "job": {
      "id": "job_abc123",
      "user_id": "usr_123",
      "job_type": "echo_metadata",
      "status": "succeeded",
      "created_at": "2026-04-15T10:30:00Z",
      "completed_at": "2026-04-15T10:31:00Z",
      "result_metadata": {
        "echo": "Hello, world!"
      }
    }
  }
}
```

### Verifying Webhook Signatures

The `X-Processor-Signature` header contains an HMAC-SHA256 signature of the request body.

**Verification (Python example)**:
```python
import hmac
import hashlib

def verify_webhook(secret, body, signature_header):
    expected = hmac.new(
        secret.encode('utf-8'),
        body.encode('utf-8'),
        hashlib.sha256
    ).hexdigest()
    expected_sig = f"sha256={expected}"
    return hmac.compare_digest(expected_sig, signature_header)
```

**Verification (Node.js example)**:
```javascript
const crypto = require('crypto');

function verifyWebhook(secret, body, signatureHeader) {
  const expected = crypto
    .createHmac('sha256', secret)
    .update(body)
    .digest('hex');
  const expectedSig = `sha256=${expected}`;
  return crypto.timingSafeEqual(
    Buffer.from(expectedSig),
    Buffer.from(signatureHeader)
  );
}
```

## Error Codes

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `unauthorized` | 401 | Missing or invalid authentication |
| `forbidden` | 403 | Insufficient permissions |
| `not_found` | 404 | Resource not found |
| `invalid_input` | 400 | Invalid request parameters |
| `conflict` | 409 | Resource conflict (e.g., duplicate email) |
| `rate_limited` | 429 | Too many requests |
| `internal_error` | 500 | Internal server error |

## Rate Limits

- **Authentication endpoints** (`/register`, `/login`): 5 requests per minute per IP
- **API endpoints**: 100 requests per minute per user
- **Worker endpoints**: 1000 requests per minute per worker

Rate limit headers:
```
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 95
X-RateLimit-Reset: 1713178200
```

## Pagination

List endpoints support pagination via `limit` and `offset` query parameters.

**Example**:
```
GET /api/v1/jobs?limit=20&offset=40
```

**Response includes**:
```json
{
  "jobs": [...],
  "total": 150,
  "limit": 20,
  "offset": 40
}
```

## Example Usage

### Create a job with curl

```bash
curl -X POST https://your-domain.com/api/v1/jobs \
  -H "Authorization: Bearer pk_your_api_key_here" \
  -H "Content-Type: application/json" \
  -d '{
    "job_type": "echo_metadata",
    "input_metadata": {
      "message": "Hello from API"
    },
    "priority_weight": 5,
    "notify_email": true
  }'
```

### Check job status

```bash
curl https://your-domain.com/api/v1/jobs/job_abc123 \
  -H "Authorization: Bearer pk_your_api_key_here"
```

### List all jobs

```bash
curl https://your-domain.com/api/v1/jobs \
  -H "Authorization: Bearer pk_your_api_key_here"
```

### Create an API key (requires session cookie)

```bash
curl -X POST https://your-domain.com/api/v1/api-keys \
  -H "Cookie: session=your_session_cookie" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "My API Key"
  }'
```
