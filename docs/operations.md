# Operations Guide

## Monitoring

### Health Checks

**Endpoint**: `GET /api/v1/health`

```bash
curl http://localhost:8080/api/v1/health
```

Expected response:
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

**Monitor**:
- Response time < 100ms
- HTTP status 200
- Check every 30 seconds

### Key Metrics

**Job Queue Depth**:
```sql
SELECT status, COUNT(*) FROM jobs GROUP BY status;
```

Watch for:
- Growing `queued` count (workers may be down)
- High `running` count (jobs may be stuck)
- High `failed` or `dead_letter` count (systematic failures)

**Worker Health**:
```sql
SELECT id, name, status, last_seen_at 
FROM workers 
WHERE last_seen_at < datetime('now', '-5 minutes');
```

Watch for:
- Workers with old `last_seen_at` (indicates offline worker)
- Workers in `error` status

**Failed Notifications**:
```sql
SELECT COUNT(*) FROM webhook_deliveries WHERE status = 'failed';
SELECT COUNT(*) FROM email_notifications WHERE state = 'failed';
```

Watch for:
- Growing failed counts (delivery issues)

**Database Size**:
```bash
ls -lh /var/lib/processor/processor.db
```

Watch for:
- Disk space consumption
- WAL file size (should checkpoint periodically)

### Logging

**Application Logs**:
```bash
# systemd journal
journalctl -u processor-control-plane -f

# or direct file if configured
tail -f /var/log/processor/control-plane.log
```

**Log Levels**:
- `ERROR`: Errors requiring attention
- `WARN`: Potential issues, degraded state
- `INFO`: Normal operations, important events
- `DEBUG`: Detailed diagnostic information

**Important Log Messages**:
- `user.login.success` / `user.login.failed`
- `job.created`
- `job.failed`
- `worker.offline`
- `webhook.delivery.failed`
- `database.error`

## Common Tasks

### Create Admin User

**Method 1: Bootstrap on first run**

Set environment variable:
```bash
export ADMIN_EMAIL=admin@example.com
export ADMIN_PASSWORD=ChangeMe123
```

Start the control plane. It will create the admin user if it doesn't exist.

**Method 2: Direct SQL**

```bash
sqlite3 /var/lib/processor/processor.db

-- Generate password hash first
-- Use: go run -c 'package main; import ("fmt"; "golang.org/x/crypto/bcrypt"); func main() { h, _ := bcrypt.GenerateFromPassword([]byte("YourPassword"), 10); fmt.Println(string(h)) }'

INSERT INTO users (id, email, password_hash, is_admin, created_at, updated_at)
VALUES (
  'usr_admin',
  'admin@example.com',
  '$2a$10$...',  -- bcrypt hash
  1,
  datetime('now'),
  datetime('now')
);
```

### Reset User Password

```bash
# Generate new hash
go run -c 'package main; import ("fmt"; "golang.org/x/crypto/bcrypt"); func main() { h, _ := bcrypt.GenerateFromPassword([]byte("NewPassword123"), 10); fmt.Println(string(h)) }'

# Update in database
sqlite3 /var/lib/processor/processor.db
UPDATE users SET password_hash = '$2a$10$...' WHERE email = 'user@example.com';
```

### Requeue Failed Job

```bash
sqlite3 /var/lib/processor/processor.db

UPDATE jobs 
SET status = 'queued',
    queued_at = datetime('now'),
    leased_at = NULL,
    lease_expires_at = NULL,
    worker_id = NULL,
    error_text = NULL
WHERE id = 'job_abc123';
```

### Cancel Stuck Job

```bash
sqlite3 /var/lib/processor/processor.db

UPDATE jobs 
SET status = 'canceled',
    completed_at = datetime('now')
WHERE id = 'job_abc123';
```

### Revoke API Key

```bash
sqlite3 /var/lib/processor/processor.db

UPDATE api_keys 
SET revoked_at = datetime('now')
WHERE key_prefix = 'pk_1234567890abcdef';
```

### List Active Jobs

```bash
sqlite3 /var/lib/processor/processor.db

SELECT 
  id,
  user_id,
  job_type,
  status,
  created_at,
  started_at
FROM jobs
WHERE status IN ('queued', 'leased', 'running')
ORDER BY created_at;
```

### Inspect Job Details

```bash
sqlite3 /var/lib/processor/processor.db

.mode column
.headers on

SELECT 
  id,
  job_type,
  status,
  priority_weight,
  attempt_count,
  max_attempts,
  created_at,
  queued_at,
  started_at,
  completed_at,
  error_text
FROM jobs
WHERE id = 'job_abc123';

-- View attempts
SELECT * FROM job_attempts WHERE job_id = 'job_abc123' ORDER BY attempt_number;
```

### Check Webhook Delivery Status

```bash
sqlite3 /var/lib/processor/processor.db

SELECT 
  id,
  webhook_id,
  job_id,
  event_type,
  status,
  status_code,
  attempt_count,
  delivered_at,
  failed_at
FROM webhook_deliveries
WHERE webhook_id = 'hook_456'
ORDER BY created_at DESC
LIMIT 10;
```

### View Audit Log

```bash
sqlite3 /var/lib/processor/processor.db

SELECT 
  created_at,
  actor_type,
  actor_id,
  event_type,
  object_type,
  object_id
FROM audit_log
ORDER BY created_at DESC
LIMIT 50;
```

## Backup & Restore

### Backup

**Online Backup** (while service is running):
```bash
sqlite3 /var/lib/processor/processor.db ".backup /var/backups/processor/processor-$(date +%Y%m%d-%H%M%S).db"
```

**File Copy** (requires service stop):
```bash
systemctl stop processor-control-plane
cp /var/lib/processor/processor.db /var/backups/processor/processor-$(date +%Y%m%d-%H%M%S).db
systemctl start processor-control-plane
```

**Automated Backup Script**:
```bash
#!/bin/bash
# /usr/local/bin/backup-processor.sh

BACKUP_DIR="/var/backups/processor"
RETENTION_DAYS=30

# Create backup
sqlite3 /var/lib/processor/processor.db ".backup ${BACKUP_DIR}/processor-$(date +%Y%m%d-%H%M%S).db"

# Delete old backups
find ${BACKUP_DIR} -name "processor-*.db" -mtime +${RETENTION_DAYS} -delete

# Upload to remote storage (optional)
# rclone copy ${BACKUP_DIR} remote:processor-backups/
```

**Cron Job**:
```bash
# /etc/cron.d/processor-backup
0 2 * * * processor /usr/local/bin/backup-processor.sh
```

### Restore

```bash
# Stop service
systemctl stop processor-control-plane

# Restore database
cp /var/backups/processor/processor-20260415-020000.db /var/lib/processor/processor.db

# Set permissions
chown processor:processor /var/lib/processor/processor.db
chmod 600 /var/lib/processor/processor.db

# Start service
systemctl start processor-control-plane

# Verify
journalctl -u processor-control-plane -n 50
curl http://localhost:8080/api/v1/health
```

## Database Maintenance

### Vacuum Database

Reclaim unused space and optimize:
```bash
sqlite3 /var/lib/processor/processor.db "VACUUM;"
```

**Note**: VACUUM requires temporary disk space equal to the database size.

### Checkpoint WAL

Force WAL checkpoint to merge into main database:
```bash
sqlite3 /var/lib/processor/processor.db "PRAGMA wal_checkpoint(TRUNCATE);"
```

### Analyze Database

Update query planner statistics:
```bash
sqlite3 /var/lib/processor/processor.db "ANALYZE;"
```

### Check Integrity

```bash
sqlite3 /var/lib/processor/processor.db "PRAGMA integrity_check;"
```

Expected output: `ok`

## Scaling

### Vertical Scaling

**VPS Upgrade**:
- 1GB → 2GB RAM allows more concurrent connections
- Faster CPU improves request throughput
- More disk space for database growth

**Configuration Tuning**:
```bash
# Increase SQLite busy timeout
DATABASE_DSN=file:processor.db?_busy_timeout=10000

# Increase Go's GOMAXPROCS (default: number of CPUs)
export GOMAXPROCS=2
```

### Horizontal Scaling (Future)

**Multiple Control Plane Instances**:
- Run multiple Go processes behind nginx
- Use shared SQLite file (NFS/GlusterFS) or migrate to PostgreSQL
- Session state in database or Redis

**Multiple Workers**:
- Each worker polls independently
- Lease mechanism prevents duplicate work
- Add workers to increase throughput

**Load Balancing**:
```nginx
upstream processor {
  server 127.0.0.1:8080;
  server 127.0.0.1:8081;
}

server {
  location / {
    proxy_pass http://processor;
  }
}
```

## Troubleshooting

### Service Won't Start

**Check logs**:
```bash
journalctl -u processor-control-plane -n 100
```

**Common issues**:
- Database file permissions
- Missing environment variables
- Port already in use
- Database migration failed

**Solutions**:
```bash
# Check database permissions
ls -la /var/lib/processor/processor.db

# Check port
netstat -tlnp | grep 8080

# Run migration manually
cd /opt/processor/control-plane
./server -migrate

# Check environment
systemctl cat processor-control-plane | grep Environment
```

### Database Locked Errors

**Symptoms**:
```
Error: database is locked
```

**Causes**:
- Long-running transaction
- WAL file not checkpointing
- Multiple processes accessing database incorrectly

**Solutions**:
```bash
# Check for other processes
lsof /var/lib/processor/processor.db

# Force WAL checkpoint
sqlite3 /var/lib/processor/processor.db "PRAGMA wal_checkpoint(RESTART);"

# Increase busy timeout (in code)
db.SetMaxOpenConns(1)  // Serialize writes
```

### Worker Not Polling

**Check worker status**:
```bash
journalctl -u processor-worker -n 100
```

**Common issues**:
- Invalid worker token
- Network connectivity
- Control plane down
- Worker crashed

**Solutions**:
```bash
# Test connectivity
curl -H "X-Worker-Token: $WORKER_TOKEN" https://processor.example.com/api/v1/worker/poll

# Restart worker
systemctl restart processor-worker

# Check token
echo $WORKER_TOKEN
```

### Jobs Stuck in Running

**Identify stuck jobs**:
```sql
SELECT id, job_type, started_at, worker_id
FROM jobs
WHERE status = 'running'
  AND started_at < datetime('now', '-1 hour');
```

**Check lease expiration**:
```sql
SELECT id, job_type, lease_expires_at
FROM jobs
WHERE status = 'leased'
  AND lease_expires_at < datetime('now');
```

**Solutions**:
- Wait for lease expiration (auto-requeue)
- Manually requeue (see "Requeue Failed Job")
- Check worker health
- Investigate worker logs for execution issues

### High Memory Usage

**Monitor**:
```bash
# Control plane
ps aux | grep processor-server

# System
free -h
```

**Causes**:
- Large job metadata
- Memory leak
- Too many concurrent requests

**Solutions**:
- Limit job metadata size (validation)
- Restart service (temporary)
- Investigate with pprof (Go profiling)
- Upgrade VPS

### Webhook Delivery Failures

**Check deliveries**:
```sql
SELECT 
  webhook_id,
  target_url,
  status,
  status_code,
  response_snippet,
  attempt_count
FROM webhook_deliveries wd
JOIN webhooks w ON wd.webhook_id = w.id
WHERE status = 'failed';
```

**Common issues**:
- Target server down
- Invalid URL
- TLS certificate issues
- Timeout

**Solutions**:
- Verify target URL manually: `curl -X POST <url>`
- Check webhook secret matches
- Review target server logs
- Disable webhook temporarily if target is down

### Email Not Sending

**Check email queue**:
```sql
SELECT 
  id,
  user_id,
  job_id,
  state,
  attempt_count,
  failed_at
FROM email_notifications
WHERE state IN ('pending', 'failed');
```

**Common issues**:
- SMTP credentials wrong
- SMTP server down
- Rate limiting by email provider

**Solutions**:
- Test SMTP manually: `telnet smtp.example.com 587`
- Check SMTP environment variables
- Review email provider logs
- Implement exponential backoff (already in retry logic)

## Performance Optimization

### Database Indexes

Check query performance:
```bash
sqlite3 /var/lib/processor/processor.db

.timer on
EXPLAIN QUERY PLAN SELECT * FROM jobs WHERE status = 'queued' ORDER BY priority_weight DESC, created_at LIMIT 1;
```

Ensure indexes exist:
```sql
CREATE INDEX IF NOT EXISTS idx_jobs_queue_poll ON jobs(status, priority_weight, created_at);
CREATE INDEX IF NOT EXISTS idx_jobs_user_id ON jobs(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_token_hash ON sessions(token_hash);
```

### Query Optimization

**Avoid full table scans**:
- Use indexes for WHERE clauses
- Use LIMIT on large result sets
- Use pagination for list endpoints

**Batch operations**:
- Use transactions for multiple inserts
- Use prepared statements

### Connection Pooling

```go
db.SetMaxOpenConns(10)      // Max concurrent connections
db.SetMaxIdleConns(5)       // Idle connection pool
db.SetConnMaxLifetime(time.Hour)
```

### Caching

**Future optimization**:
- Cache frequently accessed data (user profiles, API keys)
- Use in-memory cache (sync.Map) for low-scale
- Use Redis for multi-instance deployments

## Security Incidents

### Suspected Unauthorized Access

1. **Review audit log**:
```sql
SELECT * FROM audit_log
WHERE actor_id = 'usr_suspect'
  AND created_at > datetime('now', '-24 hours')
ORDER BY created_at DESC;
```

2. **Disable user**:
```sql
UPDATE users SET disabled_at = datetime('now') WHERE id = 'usr_suspect';
```

3. **Revoke API keys**:
```sql
UPDATE api_keys SET revoked_at = datetime('now') WHERE user_id = 'usr_suspect';
```

4. **Invalidate sessions**:
```sql
DELETE FROM sessions WHERE user_id = 'usr_suspect';
```

### Rotate Secrets

```bash
# Generate new secrets
NEW_SESSION_SECRET=$(openssl rand -hex 32)
NEW_WORKER_TOKEN=$(openssl rand -hex 32)

# Update environment
vim /etc/processor/.env

# Restart service
systemctl restart processor-control-plane

# Workers need new token
systemctl restart processor-worker
```

**Note**: Rotating SESSION_SECRET invalidates all active sessions (users must log in again).

## Disaster Recovery

### Total System Failure

1. **Provision new VPS**
2. **Install dependencies** (nginx, systemd)
3. **Restore database** from backup
4. **Deploy application** (see deployment.md)
5. **Update DNS** (if IP changed)
6. **Verify functionality** (health check, login, job submission)

**RTO** (Recovery Time Objective): 1-2 hours  
**RPO** (Recovery Point Objective): Last backup (daily = 24 hours max data loss)

### Data Corruption

1. **Stop service**
2. **Restore from last known good backup**
3. **Verify integrity**: `PRAGMA integrity_check`
4. **Restart service**
5. **Notify affected users** if data loss occurred

## Capacity Planning

### Current Capacity Estimates

**VPS (1GB RAM)**:
- ~100 requests/second
- ~10,000 jobs in database
- ~10 active users
- ~1-2 workers

**When to scale**:
- Job queue depth consistently > 100
- Response time > 500ms
- Memory usage > 80%
- Database size > 1GB

### Growth Projections

| Metric | Current | 6 months | 12 months |
|--------|---------|----------|-----------|
| Users | 10 | 50 | 200 |
| Jobs/day | 100 | 1,000 | 5,000 |
| Database | 100MB | 500MB | 2GB |
| RAM needed | 1GB | 2GB | 4GB |

## On-Call Runbook

### Alerts

**Critical** (page immediately):
- Control plane down (health check fails for 5 minutes)
- Database corruption
- Zero workers active for 10 minutes

**Warning** (investigate within 1 hour):
- High job queue depth (>100 for 30 minutes)
- Webhook delivery failure rate >50%
- Worker offline >15 minutes

**Info** (review next business day):
- Email delivery failures
- Individual job failures
- Slow queries

### Response Procedures

**Control Plane Down**:
1. Check systemd status: `systemctl status processor-control-plane`
2. Review logs: `journalctl -u processor-control-plane -n 200`
3. Common fixes:
   - Restart: `systemctl restart processor-control-plane`
   - Database issue: Check locks, checkpoint WAL
   - Disk full: Clean up old logs, backups
4. If unrecoverable: Restore from backup

**Worker Down**:
1. Check systemd status: `systemctl status processor-worker`
2. Review logs: `journalctl -u processor-worker -n 200`
3. Common fixes:
   - Restart: `systemctl restart processor-worker`
   - Network issue: Check connectivity to control plane
   - Auth issue: Verify worker token
4. Jobs will auto-requeue after lease expiration

**High Queue Depth**:
1. Check worker status: Query `workers` table
2. Add temporary worker if needed
3. Investigate job failure patterns
4. Consider increasing worker capacity
