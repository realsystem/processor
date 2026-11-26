package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newStore(t *testing.T) (*Store, *time.Time) {
	t.Helper()
	now := time.Unix(1_700_000_000, 0)
	return &Store{Dir: t.TempDir(), Now: func() time.Time { return now }}, &now
}

func TestLastStepSurvivesRestartAndOnlyMovesForward(t *testing.T) {
	s, _ := newStore(t)
	if got, err := s.LastStep(); err != nil || got != 0 {
		t.Fatalf("fresh store: %d, %v", got, err)
	}
	if err := s.SetLastStep(100); err != nil {
		t.Fatal(err)
	}
	for _, step := range []int64{100, 99} {
		if err := s.SetLastStep(step); err == nil {
			t.Errorf("step %d accepted after 100", step)
		}
	}
	again := &Store{Dir: s.Dir} // "restart"
	if got, _ := again.LastStep(); got != 100 {
		t.Fatalf("after restart: %d, want 100", got)
	}
	fi, err := os.Stat(filepath.Join(s.Dir, "totp-last-step"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("last step file mode: %v, %v", fi, err)
	}
}

func TestLockout(t *testing.T) {
	s, now := newStore(t)
	for i := 0; i < 4; i++ {
		if locked, _ := s.RecordFailure(5, 15*time.Minute, 15*time.Minute); locked {
			t.Fatalf("locked after %d failures", i+1)
		}
	}
	if locked, _ := s.RecordFailure(5, 15*time.Minute, 15*time.Minute); !locked {
		t.Fatal("not locked after 5 failures")
	}
	if _, locked := s.LockedUntil(); !locked {
		t.Fatal("LockedUntil says unlocked")
	}
	restarted := &Store{Dir: s.Dir, Now: s.Now}
	if _, locked := restarted.LockedUntil(); !locked {
		t.Fatal("lock did not survive a restart")
	}
	*now = now.Add(16 * time.Minute)
	if _, locked := s.LockedUntil(); locked {
		t.Fatal("still locked after the lock time")
	}
}

func TestFailuresExpireOutsideWindow(t *testing.T) {
	s, now := newStore(t)
	for i := 0; i < 4; i++ {
		_, _ = s.RecordFailure(5, 15*time.Minute, 15*time.Minute)
	}
	*now = now.Add(20 * time.Minute)
	if locked, _ := s.RecordFailure(5, 15*time.Minute, 15*time.Minute); locked {
		t.Fatal("old failures still counted")
	}
}

func TestAuditHasNoCodeField(t *testing.T) {
	s, _ := newStore(t)
	if err := s.AppendAudit(42, "/update", "refused_wrong_code", "to v1.2"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(s.Dir, "audit.log"))
	line := string(b)
	for _, want := range []string{`"user_id":42`, `"command":"/update"`, `"outcome":"refused_wrong_code"`} {
		if !strings.Contains(line, want) {
			t.Errorf("audit line %q lacks %s", line, want)
		}
	}
	if strings.Contains(strings.ToLower(line), "code\":\"") {
		t.Errorf("audit line carries a code field: %s", line)
	}
}
