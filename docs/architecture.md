# System Architecture

## Overview

Processor is a split-system batch job processing platform designed for resource-constrained public hosting combined with powerful private local execution.

## System Topology

```
┌─────────────────────────────────────────┐
│         Internet Users                  │
└────────────┬────────────────────────────┘
             │
             │ HTTPS
             ▼
┌─────────────────────────────────────────┐
│     Ubuntu VPS (1GB RAM)                │
│  ┌──────────────────────────────────┐   │
│  │         nginx                     │   │
│  │    (reverse proxy, TLS)           │   │
│  └────────────┬─────────────────────┘   │
│               │                          │
│               ▼                          │
│  ┌──────────────────────────────────┐   │
│  │   Control Plane (Go)             │   │
│  │                                  │   │
│  │  - HTTP API + Web UI             │   │
│  │  - Authentication                │   │
│  │  - Job Queue Management          │   │
│  │  - Worker Coordination           │   │
│  │  - Notification Dispatch         │   │
│  └────────────┬─────────────────────┘   │
│               │                          │
│               ▼                          │
│  ┌──────────────────────────────────┐   │
│  │      SQLite Database             │   │
│  │   (WAL mode, local file)         │   │
│  └──────────────────────────────────┘   │
└─────────────┬───────────────────────────┘
              │
              │ Polling (HTTPS outbound only)
              │
              ▼
┌─────────────────────────────────────────┐
│   Local PC (GPU, Ollama)                │
│  ┌──────────────────────────────────┐   │
│  │   Data Plane Worker (Go)         │   │
│  │                                  │   │
│  │  - Polls for jobs                │   │
│  │  - Executes tasks                │   │
│  │  - Reports status                │   │
│  │  - Heartbeat monitoring          │   │
│  └────────────┬─────────────────────┘   │
│               │                          │
│               ▼                          │
│  ┌──────────────────────────────────┐   │
│  │    Job Executor Adapters         │   │
│  │  - Generic tasks (noop, echo)    │   │
│  │  - Ollama adapter (future)       │   │
│  └──────────────────────────────────┘   │
└─────────────────────────────────────────┘
```

## Design Principles

### 1. Split System Architecture
- **Control Plane**: Publicly accessible, minimal resources, coordination only
- **Data Plane**: Private, powerful hardware, execution only
- **No inbound exposure**: Worker polls outbound only, firewall-friendly

### 2. Resource Constraints
- VPS has only 1GB RAM
- SQLite instead of PostgreSQL
- No Redis or message broker
- Efficient Go implementation
- Careful memory management

### 3. Asynchronous Only
- No synchronous inference endpoints
- All work is queued
- Users poll for results or use webhooks/email
- Worker processes at its own pace

### 4. Durability and Reliability
- SQLite with WAL mode for concurrent access
- Lease-based job acquisition prevents duplicate work
- Automatic retry with configurable attempts
- Lease expiration recovers from worker failures
- Heartbeat monitoring detects stuck jobs

### 5. Production-Ready Security
- Password hashing with bcrypt
- Session-based auth for web UI
- API key auth for programmatic access
- Worker token auth for data plane
- CSRF protection on forms
- Webhook request signing with HMAC
- Audit logging
- Rate limiting

## Core Components

### Control Plane

#### HTTP Server
- Server-rendered HTML with Go templates
- JSON API for programmatic access
- Middleware: auth, CSRF, logging, rate limiting
- Graceful shutdown

#### Authentication & Authorization
- Email/password registration and login
- Session cookies (HttpOnly, Secure, SameSite)
- API keys with prefix + hash storage
- Role-based access (regular user vs admin)

#### Job Queue
- SQLite-backed durable queue
- Priority-based ordering (weight + created_at)
- Lease-based acquisition (prevents duplicate processing)
- Automatic retry with exponential backoff
- Dead letter queue for exhausted retries
- Job lifecycle: queued → leased → running → succeeded/failed/dead_letter

#### Worker Coordination
- Worker registration/authentication
- Poll endpoint returns next available job
- Heartbeat endpoint prevents lease expiration
- Complete/fail endpoints update job status
- Worker status tracking

#### Notifications
- **Email**: Queued in DB, background dispatch, retry on failure
- **Webhooks**: User-configurable URLs, HMAC signing, retry with backoff
- **Events**: job.succeeded, job.failed, job.dead_letter
- Non-blocking: notifications queued and dispatched asynchronously

#### Admin Interface
- User management
- Job inspection and requeuing
- Worker monitoring
- Failed notification inspection
- Audit log review

### Data Plane

#### Polling Worker
- Configurable poll interval (default: 5s)
- Fetches next job from control plane
- Sends periodic heartbeats while working
- Reports results back to control plane
- Graceful shutdown (completes current job)

#### Job Executor
- Pluggable interface for different job types
- Generic tasks: noop, sleep, echo_metadata
- Ollama adapter (prepared for future use)
- Execution context with cancellation
- Local retry for transient failures

#### State Management
- Minimal local state (current job only)
- Control plane is source of truth
- Heartbeat prevents lease expiration
- Automatic recovery on restart

## Database Schema

### Core Tables

**users**
- User accounts
- Email/password credentials
- Plan tier and admin flag

**sessions**
- Browser session tracking
- Expiration management

**api_keys**
- Programmatic access tokens
- Prefix for identification, hash for verification

**jobs**
- Job queue entries
- Status tracking
- Lease management
- Retry tracking
- Timestamps for lifecycle events

**job_attempts**
- Historical record of execution attempts
- Diagnostic information for failures

**webhooks**
- User-configured webhook endpoints
- Event subscriptions
- Active/inactive status

**webhook_deliveries**
- Delivery attempts and status
- Response tracking
- Retry queue

**email_notifications**
- Email send queue
- Retry tracking

**workers**
- Worker registration
- Status and capability tracking
- Current job assignment

**audit_log**
- Security and operational events
- Actor tracking

### Concurrency Model

- SQLite with WAL mode enables concurrent reads
- Short write transactions prevent lock contention
- Busy timeout configured for retries
- Polling endpoints use `FOR UPDATE` or equivalent locking

## Job Lifecycle

1. **Submission**: User submits job via web UI or API
2. **Validation**: Control plane validates input and stores metadata
3. **Queued**: Job enters queue with priority weight
4. **Leasing**: Worker polls, control plane atomically leases highest priority job
5. **Running**: Worker sends start notification, executes job, sends heartbeats
6. **Completion**: Worker reports success/failure
7. **Notification**: Control plane queues email and webhook notifications
8. **Delivery**: Background workers dispatch notifications with retries

### Retry Logic

- Jobs have `max_attempts` (default: 3)
- Failed jobs automatically requeue if attempts remain
- Exponential backoff between attempts (optional)
- Jobs exceeding max attempts move to `dead_letter` status
- Admins can manually requeue dead letter jobs

### Lease Expiration

- Leases have timeout (e.g., 5 minutes + expected job duration)
- Heartbeats extend lease expiration
- Expired leases automatically release job back to queue
- Prevents stuck jobs from worker crashes

## Security Model

### Authentication Layers

1. **Web Users**: Session cookies after email/password login
2. **API Clients**: API keys (Bearer token)
3. **Workers**: Pre-shared worker tokens

### Threat Model

**In Scope**:
- Unauthorized access to jobs and data
- CSRF attacks on web forms
- API key theft and misuse
- Webhook URL hijacking
- SQL injection
- XSS in templates

**Mitigations**:
- Bcrypt password hashing (cost 10)
- Secure session cookie flags
- CSRF tokens on all mutation forms
- API key hashing (never store plaintext)
- Worker token hashing
- HMAC webhook signing
- Parameterized SQL queries only
- HTML template auto-escaping
- Rate limiting on auth endpoints
- Audit logging

**Out of Scope** (Assumptions):
- VPS OS is hardened and updated
- nginx handles TLS termination properly
- SQLite file permissions are correct (systemd user isolation)
- Local PC is physically secure
- Email provider is trusted
- No DDoS protection (use Cloudflare or similar if needed)

### Webhook Security

- User provides secret during webhook creation
- Control plane signs payload: `HMAC-SHA256(secret, payload)`
- Signature sent in `X-Processor-Signature` header
- Receiving service must verify signature
- Prevents webhook URL hijacking and replay attacks

## Scalability Considerations

### Current Scale (Initial Design Point)
- ~10 users
- ~100s of jobs per day
- 1-2 workers
- Single VPS, single worker machine

### Growth Path
- SQLite can handle 100k+ jobs with proper indexing
- Add more workers (each polls independently)
- Shard by user if needed (multi-tenant DB files)
- Eventually migrate to PostgreSQL if RAM increases
- Control plane is stateless (can run multiple instances behind nginx)

### Bottlenecks
- SQLite write throughput (~1000 writes/sec, plenty for this scale)
- VPS memory (1GB is tight, monitor carefully)
- Worker execution time (add more workers)

## Operational Concerns

### Monitoring
- Health endpoints: `/api/v1/health`
- Worker last-seen timestamps
- Job queue depth metrics
- Failed notification counts
- Audit log for security events

### Backup
- SQLite file backup (simple file copy with `.backup` command)
- Regular snapshots recommended
- WAL checkpoint before backup

### Failure Modes

| Failure | Impact | Recovery |
|---------|--------|----------|
| Control plane crash | Jobs queued, not dispatched | Restart; leases auto-expire and requeue |
| Worker crash | Current job lease expires | Restart; job auto-requeues after lease timeout |
| Database corruption | System inoperable | Restore from backup; WAL mode reduces risk |
| Webhook target down | Delivery retries accumulate | Automatic retry with backoff; manual inspection |
| Email provider down | Email queue grows | Automatic retry; monitor queue depth |
| Network partition | Worker can't poll | Worker retries polling; jobs wait in queue |

## Technology Choices Rationale

### Go
- Low memory footprint
- Fast compilation
- Excellent standard library (http, html/template, database/sql)
- Easy deployment (static binary)
- Great concurrency primitives

### SQLite
- No separate database server (saves RAM)
- ACID guarantees
- WAL mode enables concurrent readers
- Easy backups (just copy file)
- Sufficient for <1M jobs
- Embedded in process (no network overhead)

### No Redis/Queue Service
- Additional memory overhead
- Additional operational complexity
- SQLite with proper indexes is sufficient at this scale
- Reduces VPS costs

### Server-Rendered HTML
- No heavy JavaScript framework
- Faster page loads
- Better on low-end VPS
- Progressive enhancement possible
- SEO-friendly (if needed later)

### nginx
- Industry standard
- TLS termination
- Static file serving
- Rate limiting
- Request buffering (protects Go app)

## Future Enhancements

### Planned
- Real Ollama job types with prompt templates
- Job result storage (S3 or local file storage)
- User quotas and rate limiting per plan tier
- Scheduled jobs (cron-like)
- Job dependencies (wait for other jobs)

### Possible
- Multi-worker job distribution (map/reduce)
- Streaming job progress updates (SSE or WebSocket)
- Job output artifacts (file uploads)
- Advanced retry policies (jitter, circuit breaker)
- Metrics and observability (Prometheus)
- Job templates (reusable job definitions)

### Not Planned
- Synchronous inference API
- Real-time chat interface
- Client-side state management (React/Vue)
- Multi-region deployment
- Auto-scaling workers
