// Package state holds what must survive a restart: the last accepted TOTP step, the lockout
// counter and the audit log. Everything lives in one private directory.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Store is a directory of small files, written atomically with mode 0600.
type Store struct {
	Dir string
	Now func() time.Time

	mu sync.Mutex
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// WriteFileAtomic writes data to path through a temp file in the same directory.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", filepath.Base(path), err)
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod %s: %w", filepath.Base(path), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("replace %s: %w", filepath.Base(path), err)
	}
	return nil
}

func (s *Store) path(name string) string { return filepath.Join(s.Dir, name) }

// LastStep returns the last accepted TOTP step (0 when none).
func (s *Store) LastStep() (int64, error) {
	b, err := os.ReadFile(s.path("totp-last-step"))
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read last totp step: %w", err)
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse last totp step: %w", err)
	}
	return n, nil
}

// SetLastStep records an accepted step. It only ever moves forward.
func (s *Store) SetLastStep(step int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.LastStep()
	if err != nil {
		return err
	}
	if step <= cur {
		return fmt.Errorf("totp step %d is not newer than %d", step, cur)
	}
	return WriteFileAtomic(s.path("totp-last-step"), []byte(strconv.FormatInt(step, 10)+"\n"), 0o600)
}

type lockoutFile struct {
	Failures    []int64 `json:"failures"` // unix times of recent wrong codes
	LockedUntil int64   `json:"locked_until"`
}

func (s *Store) readLockout() lockoutFile {
	var lf lockoutFile
	if b, err := os.ReadFile(s.path("lockout.json")); err == nil {
		_ = json.Unmarshal(b, &lf) // a damaged file counts as no history
	}
	return lf
}

// LockedUntil reports whether codes are refused right now, and until when.
func (s *Store) LockedUntil() (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lf := s.readLockout()
	until := time.Unix(lf.LockedUntil, 0)
	return until, until.After(s.now())
}

// RecordFailure counts a wrong or reused code. After max failures inside window the bot locks
// for lockFor. It returns whether the lock is now on.
func (s *Store) RecordFailure(max int, window, lockFor time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	lf := s.readLockout()
	keep := lf.Failures[:0]
	for _, t := range lf.Failures {
		if now.Sub(time.Unix(t, 0)) < window {
			keep = append(keep, t)
		}
	}
	lf.Failures = append(keep, now.Unix())
	locked := false
	if len(lf.Failures) >= max {
		lf.LockedUntil = now.Add(lockFor).Unix()
		lf.Failures = nil
		locked = true
	}
	b, _ := json.Marshal(lf)
	if err := WriteFileAtomic(s.path("lockout.json"), b, 0o600); err != nil {
		return locked, err
	}
	return locked, nil
}

// ClearFailures forgets the wrong-code history after a good code.
func (s *Store) ClearFailures() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	lf := s.readLockout()
	if len(lf.Failures) == 0 {
		return nil
	}
	lf.Failures = nil
	b, _ := json.Marshal(lf)
	return WriteFileAtomic(s.path("lockout.json"), b, 0o600)
}

// Audit is one line of the audit log. There is deliberately no field for a code.
type Audit struct {
	Time    string `json:"time"`
	UserID  int64  `json:"user_id"`
	Command string `json:"command"`
	Outcome string `json:"outcome"`
	Detail  string `json:"detail,omitempty"`
}

// AppendAudit adds one line to audit.log.
func (s *Store) AppendAudit(userID int64, command, outcome, detail string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	line, _ := json.Marshal(Audit{
		Time: s.now().UTC().Format(time.RFC3339), UserID: userID,
		Command: command, Outcome: outcome, Detail: detail,
	})
	f, err := os.OpenFile(s.path("audit.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open audit log: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write audit log: %w", err)
	}
	return nil
}
