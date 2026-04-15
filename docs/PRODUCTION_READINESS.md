# Production Readiness Checklist

This checklist ensures the Processor platform is ready for production deployment.

**Last Review**: 2026-04-15  
**Reviewer**: Principal Engineer / SRE / QA Lead  
**Status**: ✅ **APPROVED with limitations**

## Pre-Deployment Checklist

### 1. Code Quality ✅

- [x] All unit tests passing (70%+ coverage)
- [x] Integration tests passing
- [x] Smoke tests passing
- [x] No critical linter warnings
- [x] Code reviewed by at least one other engineer
- [x] No hardcoded secrets or credentials
- [x] Error handling comprehensive
- [x] Logging appropriate (not excessive, not sparse)

### 2. Security ✅

- [x] Passwords hashed with bcrypt (cost 10)
- [x] Session tokens hashed
- [x] API keys hashed with prefixes
- [x] Worker authentication in place
- [x] Webhook HMAC signing implemented
- [x] Rate limiting enabled
- [x] No secrets in environment examples
- [x] Database file permissions correct (0600)
- [x] Running as non-root in Docker
- [ ] CSRF protection enabled (⚠️ See limitations)
- [ ] HTTPS/TLS configured (manual setup required)

### 3. Database ✅

- [x] Migrations tested
- [x] Migrations are idempotent
- [x] Indexes optimized for queue polling
- [x] SQLite WAL mode enabled
- [x] Backup strategy documented
- [x] Connection pooling configured
- [ ] Tested with production-scale data (⚠️ Recommend load testing)

### 4. Reliability ✅

- [x] Graceful shutdown implemented
- [x] Background workers stop cleanly
- [x] Lease expiration handling
- [x] Job retry logic implemented
- [x] Webhook retry with backoff
- [x] Email queue with retry
- [x] Health check endpoint
- [x] Timeouts on all HTTP operations
- [x] Context cancellation respected

### 5. Operations ✅

- [x] Systemd service files provided
- [x] Nginx configuration example provided
- [x] Deployment documentation complete
- [x] Operations guide available
- [x] Troubleshooting guide available
- [x] Log files rotation strategy documented
- [x] Database backup procedure documented
- [x] Secret rotation procedure documented
- [ ] Monitoring/alerting configured (⚠️ Manual setup required)

### 6. Testing ✅

- [x] Local Docker testing works
- [x] Smoke tests automated
- [x] Integration tests automated
- [x] End-to-end workflow validated
- [x] Worker registration tested
- [x] Job lifecycle tested
- [x] API authentication tested
- [ ] Load/stress testing performed (⚠️ Recommended)

### 7. Documentation ✅

- [x] README.md complete
- [x] QUICKSTART.md complete
- [x] Architecture documentation
- [x] API documentation
- [x] Deployment guide
- [x] Security documentation
- [x] Operations guide
- [x] Testing guide
- [x] Development guide
- [x] Docker local guide
- [x] Review report complete

### 8. Resource Efficiency ✅

- [x] Suitable for 1GB RAM VPS
- [x] No memory leaks identified
- [x] Connection pooling limits set
- [x] Background worker intervals appropriate
- [x] No unnecessary buffering
- [x] Binary sizes optimized

## Deployment Steps

### Pre-Deployment (1 week before)

- [ ] Review all documentation
- [ ] Test deployment procedure on staging VPS
- [ ] Verify backup/restore procedures
- [ ] Test rollback procedure
- [ ] Prepare monitoring/alerting
- [ ] Review security settings
- [ ] Load test with expected traffic

### Deployment Day

#### Control Plane Deployment (VPS)

- [ ] **1. Prepare VPS**
  ```bash
  # Update system
  sudo apt update && sudo apt upgrade -y
  
  # Install dependencies
  sudo apt install -y sqlite3 nginx
  ```

- [ ] **2. Build binary**
  ```bash
  # On development machine
  cd control-plane
  CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o server ./cmd/server
  scp server user@vps:/opt/processor/
  ```

- [ ] **3. Setup database**
  ```bash
  # On VPS
  mkdir -p /opt/processor/data
  chmod 700 /opt/processor/data
  /opt/processor/server -migrate
  ```

- [ ] **4. Configure environment**
  ```bash
  # Create /opt/processor/.env
  DATABASE_PATH=/opt/processor/data/processor.db
  SESSION_SECRET=<GENERATE_SECURE_SECRET>
  WORKER_TOKEN=<GENERATE_SECURE_TOKEN>
  HTTP_PORT=8080
  ENV=production
  LOG_LEVEL=info
  ```

- [ ] **5. Setup systemd**
  ```bash
  sudo cp control-plane/systemd/processor-control-plane.service /etc/systemd/system/
  sudo systemctl daemon-reload
  sudo systemctl enable processor-control-plane
  sudo systemctl start processor-control-plane
  ```

- [ ] **6. Configure nginx**
  ```bash
  sudo cp control-plane/nginx.conf.example /etc/nginx/sites-available/processor
  # Edit for your domain
  sudo ln -s /etc/nginx/sites-available/processor /etc/nginx/sites-enabled/
  sudo nginx -t
  sudo systemctl reload nginx
  ```

- [ ] **7. Setup TLS**
  ```bash
  sudo apt install certbot python3-certbot-nginx
  sudo certbot --nginx -d your-domain.com
  ```

#### Worker Deployment (Local PC)

- [ ] **1. Build binary**
  ```bash
  cd data-plane
  go build -o worker ./cmd/worker
  ```

- [ ] **2. Configure environment**
  ```bash
  # Create .env
  CONTROL_PLANE_URL=https://your-domain.com
  WORKER_TOKEN=<SAME_AS_CONTROL_PLANE>
  WORKER_ID=worker_local_1
  WORKER_NAME=local-worker-1
  POLL_INTERVAL=5s
  WORKER_CAPABILITIES=noop,sleep,echo_metadata,ollama_chat
  LOG_LEVEL=info
  ```

- [ ] **3. Register worker**
  ```bash
  # On VPS or via API
  cd control-plane
  go run scripts/register-worker.go \
    -id worker_local_1 \
    -name local-worker-1 \
    -token <WORKER_TOKEN>
  ```

- [ ] **4. Setup systemd (Linux) or launchd (macOS)**
  ```bash
  # Linux
  sudo cp data-plane/systemd/processor-worker.service /etc/systemd/system/
  sudo systemctl daemon-reload
  sudo systemctl enable processor-worker
  sudo systemctl start processor-worker
  
  # macOS - manual or use launchd
  ```

### Post-Deployment Validation

- [ ] **1. Smoke test**
  ```bash
  curl https://your-domain.com/api/v1/health
  # Expected: {"success":true,"data":{"status":"healthy",...}}
  ```

- [ ] **2. Create admin user**
  ```bash
  # On VPS
  cd /opt/processor
  ./server # Will auto-create if ADMIN_EMAIL set
  # Or manually via scripts
  ```

- [ ] **3. Create API key**
  ```bash
  cd /opt/processor
  # Use scripts/create-api-key.go
  ```

- [ ] **4. Submit test job**
  ```bash
  curl -X POST https://your-domain.com/api/v1/jobs \
    -H "Authorization: Bearer $API_KEY" \
    -H "Content-Type: application/json" \
    -d '{"job_type":"noop","input_metadata":{},"priority_weight":10}'
  ```

- [ ] **5. Verify worker processes job**
  ```bash
  # Check worker logs
  journalctl -u processor-worker -f
  
  # Check job status via API
  curl https://your-domain.com/api/v1/jobs/$JOB_ID \
    -H "Authorization: Bearer $API_KEY"
  ```

- [ ] **6. Setup monitoring**
  - Configure health check monitoring (e.g., UptimeRobot)
  - Set up log aggregation (optional)
  - Configure alerting (e.g., email on failures)

- [ ] **7. Verify backup**
  ```bash
  # Create first backup
  sqlite3 /opt/processor/data/processor.db ".backup /opt/processor/backups/processor-$(date +%Y%m%d).db"
  
  # Setup cron for daily backups
  crontab -e
  # Add: 0 2 * * * /opt/processor/scripts/backup.sh
  ```

## Production Monitoring

### Health Checks

Monitor these endpoints/metrics:

- **Health endpoint**: `GET /api/v1/health` (should return 200)
- **Disk space**: SQLite database size and available space
- **Memory usage**: Should stay under 500MB for 1GB VPS
- **CPU usage**: Should be low except during job processing
- **Worker connectivity**: Check last_seen_at in workers table

### Log Monitoring

Watch for these in logs:

- **Errors**: Any ERROR or FATAL level logs
- **Failed jobs**: Jobs moving to dead_letter status
- **Webhook failures**: Repeated webhook delivery failures
- **Database locks**: SQLite lock errors
- **Worker disconnects**: Worker polling failures

### Database Queries

Regular health checks:

```sql
-- Check job queue depth
SELECT status, COUNT(*) FROM jobs GROUP BY status;

-- Check old leases
SELECT COUNT(*) FROM jobs WHERE status = 'leased' AND lease_expires_at < datetime('now');

-- Check dead letter queue
SELECT COUNT(*) FROM jobs WHERE status = 'dead_letter';

-- Check worker status
SELECT id, name, last_seen_at FROM workers WHERE status = 'active';
```

## Rollback Procedure

If issues occur post-deployment:

1. **Stop services**:
   ```bash
   sudo systemctl stop processor-control-plane
   sudo systemctl stop processor-worker  # On worker machine
   ```

2. **Restore database**:
   ```bash
   cp /opt/processor/backups/processor-YYYYMMDD.db /opt/processor/data/processor.db
   ```

3. **Deploy previous version**:
   ```bash
   cp /opt/processor/server.backup /opt/processor/server
   ```

4. **Restart services**:
   ```bash
   sudo systemctl start processor-control-plane
   sudo systemctl start processor-worker  # On worker machine
   ```

5. **Verify**:
   ```bash
   curl https://your-domain.com/api/v1/health
   ```

## Known Limitations

### Critical Limitations (Address before production)

None - system is production-ready for initial deployment.

### Important Limitations (Address within 1-3 months)

1. **SQLite Scalability**: Single-server database limits horizontal scaling
   - Mitigation: Monitor database size and query performance
   - Future: Consider PostgreSQL migration if needed

2. **No Web UI**: API-only interface
   - Mitigation: Provide good API documentation
   - Future: Build admin dashboard

3. **Limited Observability**: No built-in metrics endpoint
   - Mitigation: Monitor via logs and health checks
   - Future: Add Prometheus metrics

### Nice-to-Have Improvements

1. CSRF protection for browser-based API access
2. More comprehensive webhook/email tests
3. Performance/load testing results
4. Automated deployment scripts
5. Database migration rollback support

## Sign-Off

**Production Readiness**: ✅ **APPROVED**

**Conditions**:
1. Follow deployment checklist exactly
2. Set up monitoring within first week
3. Perform daily backups
4. Review logs daily for first week
5. Have rollback plan ready

**Approved For**:
- Production deployment on 1GB RAM Ubuntu VPS
- Local worker deployment on standard PC/laptop
- Initial user load (< 1000 jobs/day)

**Not Approved For**:
- High-scale production (> 10,000 jobs/day) without load testing
- Mission-critical workflows without redundancy
- Multi-region deployment without architecture review

**Next Review**: After 1 month of production operation

---

**Reviewer Sign-Off**:  
Name: [Principal Engineer / SRE / QA Lead]  
Date: 2026-04-15  
Signature: ___________________________
