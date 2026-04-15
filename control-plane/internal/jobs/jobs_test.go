package jobs

import (
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/processor/control-plane/internal/db"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()

	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	// Run migrations
	if err := db.Migrate(database); err != nil {
		t.Fatalf("Failed to run migrations: %v", err)
	}

	return database
}

func createTestUser(t *testing.T, database *sql.DB) string {
	t.Helper()

	userID := db.GenerateID("usr")
	_, err := database.Exec(`
		INSERT INTO users (id, email, password_hash, plan_tier, is_admin, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, userID, "test@example.com", "hash", "free", false, time.Now(), time.Now())
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	return userID
}

func createTestWorker(t *testing.T, database *sql.DB, workerID string) {
	t.Helper()

	_, err := database.Exec(`
		INSERT INTO workers (id, name, auth_token_hash, status, capabilities_json, version, last_seen_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, workerID, "test-worker", "hash", "active", "[\"noop\",\"echo_metadata\"]", "1.0.0", time.Now(), time.Now(), time.Now())
	if err != nil {
		t.Fatalf("Failed to create test worker: %v", err)
	}
}

func TestCreate(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	userID := createTestUser(t, database)
	service := NewService(database, 5*time.Minute)

	t.Run("successful creation", func(t *testing.T) {
		input := map[string]interface{}{
			"message": "test",
		}

		job, err := service.Create(userID, "noop", input, 10, 3, false, false)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		if job.UserID != userID {
			t.Errorf("Expected user ID %s, got %s", userID, job.UserID)
		}

		if job.JobType != "noop" {
			t.Errorf("Expected job type noop, got %s", job.JobType)
		}

		if job.Status != StatusQueued {
			t.Errorf("Expected status queued, got %s", job.Status)
		}

		if job.PriorityWeight != 10 {
			t.Errorf("Expected priority 10, got %d", job.PriorityWeight)
		}

		if job.MaxAttempts != 3 {
			t.Errorf("Expected max attempts 3, got %d", job.MaxAttempts)
		}
	})
}

func TestPoll(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	userID := createTestUser(t, database)
	workerID := "worker_test"
	createTestWorker(t, database, workerID)
	service := NewService(database, 5*time.Minute)

	t.Run("poll with no jobs", func(t *testing.T) {
		job, err := service.Poll(workerID, []string{"noop"})
		if err != nil {
			t.Fatalf("Poll failed: %v", err)
		}

		if job != nil {
			t.Error("Expected no job, got one")
		}
	})

	t.Run("poll with available job", func(t *testing.T) {
		// Create a job
		input := map[string]interface{}{"message": "test"}
		created, err := service.Create(userID, "noop", input, 10, 3, false, false)
		if err != nil {
			t.Fatalf("Failed to create job: %v", err)
		}

		// Poll for job
		job, err := service.Poll(workerID, []string{"noop"})
		if err != nil {
			t.Fatalf("Poll failed: %v", err)
		}

		if job == nil {
			t.Fatal("Expected job, got nil")
		}

		if job.ID != created.ID {
			t.Errorf("Expected job ID %s, got %s", created.ID, job.ID)
		}

		if job.Status != StatusLeased {
			t.Errorf("Expected status leased, got %s", job.Status)
		}

		if !job.WorkerID.Valid || job.WorkerID.String != workerID {
			t.Errorf("Expected worker ID %s, got %s", workerID, job.WorkerID.String)
		}
	})

	t.Run("poll filters by capability", func(t *testing.T) {
		// Create a job with type not in capabilities
		input := map[string]interface{}{"message": "test"}
		_, err := service.Create(userID, "ollama_chat", input, 10, 3, false, false)
		if err != nil {
			t.Fatalf("Failed to create job: %v", err)
		}

		// Poll with different capability should not find it
		job, err := service.Poll(workerID, []string{"noop"})
		if err != nil {
			t.Fatalf("Poll failed: %v", err)
		}

		if job != nil {
			t.Error("Expected no job due to capability mismatch")
		}
	})

	t.Run("poll prioritizes by weight", func(t *testing.T) {
		// Create jobs with different priorities
		input := map[string]interface{}{"message": "test"}
		lowPriority, _ := service.Create(userID, "noop", input, 5, 3, false, false)
		highPriority, _ := service.Create(userID, "noop", input, 20, 3, false, false)

		// Poll should get high priority first
		job, err := service.Poll(workerID, []string{"noop"})
		if err != nil {
			t.Fatalf("Poll failed: %v", err)
		}

		if job.ID != highPriority.ID {
			t.Error("Expected high priority job to be polled first")
		}

		// Poll again should get low priority
		job, err = service.Poll(workerID, []string{"noop"})
		if err != nil {
			t.Fatalf("Poll failed: %v", err)
		}

		if job.ID != lowPriority.ID {
			t.Error("Expected low priority job to be polled second")
		}
	})
}

func TestHeartbeat(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	userID := createTestUser(t, database)
	workerID := "worker_test"
	createTestWorker(t, database, workerID)
	service := NewService(database, 5*time.Minute)

	// Create and lease a job
	input := map[string]interface{}{"message": "test"}
	_, err := service.Create(userID, "noop", input, 10, 3, false, false)
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	job, err := service.Poll(workerID, []string{"noop"})
	if err != nil {
		t.Fatalf("Failed to poll job: %v", err)
	}

	t.Run("successful heartbeat", func(t *testing.T) {
		newExpiry, err := service.Heartbeat(job.ID, workerID)
		if err != nil {
			t.Fatalf("Heartbeat failed: %v", err)
		}

		if newExpiry.Before(time.Now()) {
			t.Error("New expiry should be in the future")
		}
	})

	t.Run("heartbeat with wrong worker", func(t *testing.T) {
		_, err := service.Heartbeat(job.ID, "wrong_worker")
		if err != ErrNotLeased {
			t.Errorf("Expected ErrNotLeased, got %v", err)
		}
	})
}

func TestMarkRunning(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	userID := createTestUser(t, database)
	workerID := "worker_test"
	createTestWorker(t, database, workerID)
	service := NewService(database, 5*time.Minute)

	// Create and lease a job
	input := map[string]interface{}{"message": "test"}
	_, err := service.Create(userID, "noop", input, 10, 3, false, false)
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	job, err := service.Poll(workerID, []string{"noop"})
	if err != nil {
		t.Fatalf("Failed to poll job: %v", err)
	}

	t.Run("successful mark running", func(t *testing.T) {
		err := service.MarkRunning(job.ID, workerID)
		if err != nil {
			t.Fatalf("MarkRunning failed: %v", err)
		}

		// Verify status changed
		updated, err := service.Get(job.ID)
		if err != nil {
			t.Fatalf("Failed to get job: %v", err)
		}

		if updated.Status != StatusRunning {
			t.Errorf("Expected status running, got %s", updated.Status)
		}
	})

	t.Run("attempt record created", func(t *testing.T) {
		attempts, err := service.GetAttempts(job.ID)
		if err != nil {
			t.Fatalf("Failed to get attempts: %v", err)
		}

		if len(attempts) != 1 {
			t.Errorf("Expected 1 attempt, got %d", len(attempts))
		}

		if attempts[0].Status != StatusRunning {
			t.Errorf("Expected attempt status running, got %s", attempts[0].Status)
		}
	})
}

func TestComplete(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	userID := createTestUser(t, database)
	workerID := "worker_test"
	createTestWorker(t, database, workerID)
	service := NewService(database, 5*time.Minute)

	// Create, lease, and start a job
	input := map[string]interface{}{"message": "test"}
	_, err := service.Create(userID, "noop", input, 10, 3, false, false)
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	job, err := service.Poll(workerID, []string{"noop"})
	if err != nil {
		t.Fatalf("Failed to poll job: %v", err)
	}

	err = service.MarkRunning(job.ID, workerID)
	if err != nil {
		t.Fatalf("Failed to mark running: %v", err)
	}

	t.Run("successful completion", func(t *testing.T) {
		result := map[string]interface{}{
			"status":  "ok",
			"message": "completed successfully",
		}

		err := service.Complete(job.ID, workerID, result)
		if err != nil {
			t.Fatalf("Complete failed: %v", err)
		}

		// Verify status changed
		updated, err := service.Get(job.ID)
		if err != nil {
			t.Fatalf("Failed to get job: %v", err)
		}

		if updated.Status != StatusSucceeded {
			t.Errorf("Expected status succeeded, got %s", updated.Status)
		}

		if !updated.CompletedAt.Valid {
			t.Error("Expected completed_at to be set")
		}

		if updated.ResultMetadata["status"] != "ok" {
			t.Error("Expected result metadata to be preserved")
		}
	})
}

func TestFail(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	userID := createTestUser(t, database)
	workerID := "worker_test"
	createTestWorker(t, database, workerID)
	service := NewService(database, 5*time.Minute)

	t.Run("fail with retry", func(t *testing.T) {
		// Create, lease, and start a job
		input := map[string]interface{}{"message": "test"}
		_, err := service.Create(userID, "noop", input, 10, 3, false, false)
		if err != nil {
			t.Fatalf("Failed to create job: %v", err)
		}

		job, err := service.Poll(workerID, []string{"noop"})
		if err != nil {
			t.Fatalf("Failed to poll job: %v", err)
		}

		err = service.MarkRunning(job.ID, workerID)
		if err != nil {
			t.Fatalf("Failed to mark running: %v", err)
		}

		// Fail with retry
		err = service.Fail(job.ID, workerID, "test error", true)
		if err != nil {
			t.Fatalf("Fail failed: %v", err)
		}

		// Should be requeued
		updated, err := service.Get(job.ID)
		if err != nil {
			t.Fatalf("Failed to get job: %v", err)
		}

		if updated.Status != StatusQueued {
			t.Errorf("Expected status queued for retry, got %s", updated.Status)
		}

		if updated.AttemptCount != 1 {
			t.Errorf("Expected attempt count 1, got %d", updated.AttemptCount)
		}
	})

	t.Run("fail exceeding max attempts", func(t *testing.T) {
		// Create, lease, and start a job with max_attempts=1
		input := map[string]interface{}{"message": "test"}
		_, err := service.Create(userID, "noop", input, 10, 1, false, false)
		if err != nil {
			t.Fatalf("Failed to create job: %v", err)
		}

		job, err := service.Poll(workerID, []string{"noop"})
		if err != nil {
			t.Fatalf("Failed to poll job: %v", err)
		}

		err = service.MarkRunning(job.ID, workerID)
		if err != nil {
			t.Fatalf("Failed to mark running: %v", err)
		}

		// Fail - should go to dead letter
		err = service.Fail(job.ID, workerID, "test error", true)
		if err != nil {
			t.Fatalf("Fail failed: %v", err)
		}

		updated, err := service.Get(job.ID)
		if err != nil {
			t.Fatalf("Failed to get job: %v", err)
		}

		if updated.Status != StatusDeadLetter {
			t.Errorf("Expected status dead_letter, got %s", updated.Status)
		}
	})
}

func TestReleaseExpiredLeases(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	userID := createTestUser(t, database)
	workerID := "worker_test"
	createTestWorker(t, database, workerID)
	service := NewService(database, 1*time.Second) // Short lease for testing

	t.Run("releases expired leases", func(t *testing.T) {
		// Create and lease a job
		input := map[string]interface{}{"message": "test"}
		created, err := service.Create(userID, "noop", input, 10, 3, false, false)
		if err != nil {
			t.Fatalf("Failed to create job: %v", err)
		}

		_, err = service.Poll(workerID, []string{"noop"})
		if err != nil {
			t.Fatalf("Failed to poll job: %v", err)
		}

		// Wait for lease to expire
		time.Sleep(2 * time.Second)

		// Release expired leases
		count, err := service.ReleaseExpiredLeases()
		if err != nil {
			t.Fatalf("ReleaseExpiredLeases failed: %v", err)
		}

		if count != 1 {
			t.Errorf("Expected 1 released lease, got %d", count)
		}

		// Verify job is back to queued
		job, err := service.Get(created.ID)
		if err != nil {
			t.Fatalf("Failed to get job: %v", err)
		}

		if job.Status != StatusQueued {
			t.Errorf("Expected status queued after release, got %s", job.Status)
		}

		if job.WorkerID.Valid {
			t.Error("Expected worker ID to be cleared")
		}
	})
}

func TestList(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	userID := createTestUser(t, database)
	service := NewService(database, 5*time.Minute)

	// Create multiple jobs
	for i := 0; i < 5; i++ {
		input := map[string]interface{}{"index": i}
		_, err := service.Create(userID, "noop", input, 10, 3, false, false)
		if err != nil {
			t.Fatalf("Failed to create job %d: %v", i, err)
		}
	}

	t.Run("list all jobs", func(t *testing.T) {
		jobs, total, err := service.List(userID, "", 10, 0)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		if len(jobs) != 5 {
			t.Errorf("Expected 5 jobs, got %d", len(jobs))
		}

		if total != 5 {
			t.Errorf("Expected total 5, got %d", total)
		}
	})

	t.Run("list with pagination", func(t *testing.T) {
		jobs, total, err := service.List(userID, "", 2, 0)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		if len(jobs) != 2 {
			t.Errorf("Expected 2 jobs, got %d", len(jobs))
		}

		if total != 5 {
			t.Errorf("Expected total 5, got %d", total)
		}
	})

	t.Run("list with status filter", func(t *testing.T) {
		jobs, total, err := service.List(userID, StatusQueued, 10, 0)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		if len(jobs) != 5 {
			t.Errorf("Expected 5 queued jobs, got %d", len(jobs))
		}

		if total != 5 {
			t.Errorf("Expected total 5, got %d", total)
		}
	})
}
