package jobs

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

func optionsDB(t *testing.T) (*Service, *sql.DB, string) {
	t.Helper()
	database := setupTestDB(t)
	database.SetMaxOpenConns(1) // one in-memory database
	t.Cleanup(func() { database.Close() })
	userID := createTestUser(t, database)
	createTestWorker(t, database, "wrk_1")
	return NewService(database, 5*time.Minute), database, userID
}

func startJob(t *testing.T, s *Service, jobType string) *Job {
	t.Helper()
	job, err := s.Poll("wrk_1", []string{jobType})
	if err != nil || job == nil {
		t.Fatalf("poll: %v %v", job, err)
	}
	if err := s.MarkRunning(job.ID, "wrk_1"); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	return job
}

func TestIdempotentCreate(t *testing.T) {
	s, _, user := optionsDB(t)
	first, existing, err := s.CreateWithOptions(user, "noop", map[string]interface{}{"a": 1}, CreateOptions{IdempotencyKey: "k1"})
	if err != nil || existing {
		t.Fatalf("first create: %v existing=%v", err, existing)
	}
	again, existing, err := s.CreateWithOptions(user, "noop", map[string]interface{}{"a": 2}, CreateOptions{IdempotencyKey: "k1"})
	if err != nil || !existing || again.ID != first.ID {
		t.Fatalf("replay: %v existing=%v id=%s want %s", err, existing, again.ID, first.ID)
	}
	if _, _, err := s.CreateWithOptions(user, "sleep", nil, CreateOptions{IdempotencyKey: "k1"}); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("different type with the same key: %v", err)
	}
	other, existing, _ := s.CreateWithOptions(user, "noop", nil, CreateOptions{IdempotencyKey: "k2"})
	if existing || other.ID == first.ID {
		t.Fatal("a new key must create a new job")
	}
	plain1, _ := s.Create(user, "noop", nil, 0, 3, false, false)
	plain2, _ := s.Create(user, "noop", nil, 0, 3, false, false)
	if plain1.ID == plain2.ID {
		t.Fatal("jobs without a key are never deduplicated")
	}
}

func TestTraceContextReachesTheWorker(t *testing.T) {
	s, _, user := optionsDB(t)
	tp := "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	created, _, _ := s.CreateWithOptions(user, "noop", nil, CreateOptions{TraceParent: tp, CorrelationID: "corr_1", Sensitive: true})
	job, _ := s.Poll("wrk_1", []string{"noop"})
	if job.ID != created.ID || job.TraceParent.String != tp || job.CorrelationID.String != "corr_1" || !job.Sensitive {
		t.Fatalf("poll lost the options: %+v", job)
	}
	got, _ := s.Get(created.ID)
	if got.RetentionSeconds.Int64 != int64(DefaultSensitiveRetention/time.Second) {
		t.Fatalf("sensitive jobs get the default retention, got %v", got.RetentionSeconds)
	}
}

func TestCancel(t *testing.T) {
	s, _, user := optionsDB(t)

	queued, _ := s.Create(user, "noop", nil, 0, 3, false, false)
	job, err := s.Cancel(queued.ID)
	if err != nil || job.Status != StatusCanceled {
		t.Fatalf("queued job: %v %s", err, job.Status)
	}
	if next, _ := s.Poll("wrk_1", []string{"noop"}); next != nil {
		t.Fatal("a canceled job must not be polled")
	}

	running, _ := s.Create(user, "sleep", nil, 0, 3, false, false)
	startJob(t, s, "sleep")
	job, err = s.Cancel(running.ID)
	if err != nil || job.Status != StatusRunning || !job.CancelRequestedAt.Valid {
		t.Fatalf("running job: cancel is requested, not yet done: %v %+v", err, job)
	}
	if _, err := s.Heartbeat(running.ID, "wrk_1"); !errors.Is(err, ErrCanceled) {
		t.Fatalf("heartbeat must tell the worker: %v", err)
	}
	job, _ = s.Get(running.ID)
	if job.Status != StatusCanceled || !job.CompletedAt.Valid {
		t.Fatalf("after the heartbeat: %s", job.Status)
	}
	if err := s.Complete(running.ID, "wrk_1", map[string]interface{}{}); !errors.Is(err, ErrNotLeased) {
		t.Fatalf("a late completion is refused: %v", err)
	}
	if _, err := s.Cancel(running.ID); !errors.Is(err, ErrNotCancelable) {
		t.Fatalf("finished jobs cannot be canceled: %v", err)
	}
}

func TestPurgeAfterRetention(t *testing.T) {
	s, _, user := optionsDB(t)
	keep, _ := s.Create(user, "noop", map[string]interface{}{"x": 1}, 0, 3, false, false)
	created, _, _ := s.CreateWithOptions(user, "sleep", map[string]interface{}{"prompt": "secret"}, CreateOptions{RetentionSeconds: 60})
	startJob(t, s, "sleep")
	if err := s.Complete(created.ID, "wrk_1", map[string]interface{}{"answer": "secret too"}); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.PurgeExpired(time.Now()); n != 0 {
		t.Fatal("nothing is due yet")
	}
	n, err := s.PurgeExpired(time.Now().Add(61 * time.Second))
	if err != nil || n != 1 {
		t.Fatalf("purge: %d %v", n, err)
	}
	job, _ := s.Get(created.ID)
	if len(job.InputMetadata) != 0 || job.ResultMetadata != nil || !job.PurgedAt.Valid || job.Status != StatusSucceeded {
		t.Fatalf("purged job: %+v", job)
	}
	if k, _ := s.Get(keep.ID); k.InputMetadata["x"] == nil {
		t.Fatal("jobs without retention are kept")
	}
	if n, _ := s.PurgeExpired(time.Now().Add(time.Hour)); n != 0 {
		t.Fatal("purged once")
	}
}

func TestStuckRunningJobIsRecovered(t *testing.T) {
	s, database, user := optionsDB(t)
	retry, _ := s.Create(user, "sleep", nil, 0, 2, false, false)
	startJob(t, s, "sleep")
	expire := func(id string) {
		database.Exec("UPDATE jobs SET lease_expires_at = ? WHERE id = ?", time.Now().Add(-time.Minute), id)
	}
	expire(retry.ID)
	if n, err := s.ReleaseExpiredLeases(); err != nil || n != 1 {
		t.Fatalf("release: %d %v", n, err)
	}
	job, _ := s.Get(retry.ID)
	if job.Status != StatusQueued || job.AttemptCount != 1 {
		t.Fatalf("first loss requeues: %s attempts=%d", job.Status, job.AttemptCount)
	}
	startJob(t, s, "sleep")
	expire(retry.ID)
	s.ReleaseExpiredLeases()
	job, _ = s.Get(retry.ID)
	if job.Status != StatusDeadLetter || job.ErrorText.String != "worker stopped sending heartbeats" {
		t.Fatalf("out of attempts: %s %v", job.Status, job.ErrorText)
	}
}

func TestStats(t *testing.T) {
	s, _, user := optionsDB(t)
	s.Create(user, "noop", nil, 0, 3, false, false)
	s.Create(user, "noop", nil, 0, 3, false, false)
	s.Create(user, "sleep", nil, 0, 3, false, false)
	startJob(t, s, "sleep")
	st, err := s.Stats(time.Now().Add(30 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if st.ByStatus[StatusQueued] != 2 || st.ByStatus[StatusRunning] != 1 {
		t.Fatalf("counts: %v", st.ByStatus)
	}
	if st.OldestQueuedAgeSecond < 29 || st.OldestQueuedAgeSecond > 31 {
		t.Fatalf("oldest queued age: %d", st.OldestQueuedAgeSecond)
	}
}
