-- Optional job features: idempotent submission, trace context, cancellation, retention
-- for sensitive inputs; and Ed25519 public keys for workers (signed worker requests).
ALTER TABLE jobs ADD COLUMN idempotency_key TEXT;
ALTER TABLE jobs ADD COLUMN trace_parent TEXT;
ALTER TABLE jobs ADD COLUMN correlation_id TEXT;
ALTER TABLE jobs ADD COLUMN sensitive INTEGER NOT NULL DEFAULT 0;
ALTER TABLE jobs ADD COLUMN retention_seconds INTEGER;
ALTER TABLE jobs ADD COLUMN purged_at DATETIME;
ALTER TABLE jobs ADD COLUMN cancel_requested_at DATETIME;

CREATE UNIQUE INDEX IF NOT EXISTS idx_jobs_user_idempotency
    ON jobs(user_id, idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_jobs_purge
    ON jobs(completed_at) WHERE retention_seconds IS NOT NULL AND purged_at IS NULL;

ALTER TABLE workers ADD COLUMN public_key TEXT;
