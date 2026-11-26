package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/realsystem/processor/updater/internal/release"
	"github.com/realsystem/processor/updater/internal/totp"
)

const (
	ownerID   = int64(42)
	secretB32 = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP" // test-only secret
	sha       = "2222222222222222222222222222222222222222"
)

type fakeTG struct {
	mu      sync.Mutex
	sent    []string
	deleted []int
}

func (f *fakeTG) GetUpdates(context.Context, int64, int) ([]Update, error) { return nil, nil }
func (f *fakeTG) Send(_ context.Context, _ int64, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, text)
	return nil
}
func (f *fakeTG) Delete(_ context.Context, _ int64, id int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, id)
	return nil
}
func (f *fakeTG) last() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		return ""
	}
	return f.sent[len(f.sent)-1]
}
func (f *fakeTG) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

type fakeRel struct {
	mu      sync.Mutex
	tag     release.Tag
	tagErr  error
	ci      release.CIResult
	ciErr   error
	ciCalls int
}

func (f *fakeRel) LatestTag(context.Context) (release.Tag, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tag, f.tagErr
}
func (f *fakeRel) CI(context.Context, string) (release.CIResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ciCalls++
	return f.ci, f.ciErr
}

type testEnv struct {
	t      *testing.T
	b      *Bot
	tg     *fakeTG
	rel    *fakeRel
	cfg    Config
	now    time.Time
	key    []byte
	msg    int
	ctx    context.Context
	cancel context.CancelFunc
}

func newEnv(t *testing.T, mod func(*Config)) *testEnv {
	t.Helper()
	dir := t.TempDir()
	cfg := Config{
		AllowedIDs: map[int64]bool{ownerID: true}, Repo: "owner/repo", RequireCI: true, Version: "test",
		BotDir: filepath.Join(dir, "bot"), RequestsDir: filepath.Join(dir, "requests"), ResultsDir: filepath.Join(dir, "results"),
		ResultPoll: time.Millisecond,
	}
	for _, d := range []string{cfg.BotDir, cfg.RequestsDir, cfg.ResultsDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if mod != nil {
		mod(&cfg)
	}
	if err := os.WriteFile(filepath.Join(cfg.BotDir, "totp.secret"), []byte(secretB32+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	key, _ := totp.DecodeSecret(secretB32)
	e := &testEnv{t: t, tg: &fakeTG{}, cfg: cfg, key: key, now: time.Unix(1_700_000_000, 0)}
	e.ctx, e.cancel = context.WithCancel(context.Background())
	t.Cleanup(e.cancel)
	e.rel = &fakeRel{tag: release.Tag{Name: "v1.3.0", SHA: sha}, ci: release.CIResult{State: release.CIPassed}}
	e.setCurrent("v1.2.0")
	e.b = New(cfg, e.tg, e.rel, func() time.Time { return e.now })
	return e
}

func (e *testEnv) setCurrent(tag string) {
	if err := os.WriteFile(filepath.Join(e.cfg.ResultsDir, "current-tag"), []byte(tag+"\n"), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func (e *testEnv) code(stepOffset int64) string { return totp.Code(e.key, totp.Step(e.now)+stepOffset) }

// send delivers a message from uid in a chat of chatType, sent ageSec seconds ago.
func (e *testEnv) send(uid int64, chatType, text string, ageSec int) {
	e.msg++
	e.b.HandleMessage(e.ctx, Message{
		MessageID: e.msg, From: &User{ID: uid}, Chat: Chat{ID: uid, Type: chatType},
		Date: e.now.Add(-time.Duration(ageSec) * time.Second).Unix(), Text: text,
	})
}

func (e *testEnv) requestExists() bool {
	_, err := os.Lstat(filepath.Join(e.cfg.RequestsDir, "update.request"))
	return err == nil
}

// clearRequest plays the host consuming the request and the update ending.
func (e *testEnv) clearRequest() {
	e.clearRequestOnly()
	_ = os.Remove(filepath.Join(e.cfg.BotDir, "notify.json"))
}

func (e *testEnv) clearRequestOnly() {
	_ = os.Remove(filepath.Join(e.cfg.RequestsDir, "update.request"))
}

func (e *testEnv) audit() string {
	b, _ := os.ReadFile(filepath.Join(e.cfg.BotDir, "audit.log"))
	return string(b)
}

func (e *testEnv) lastStep() string {
	b, _ := os.ReadFile(filepath.Join(e.cfg.BotDir, "totp-last-step"))
	return strings.TrimSpace(string(b))
}

func TestUpdateRefusals(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(e *testEnv)
		uid         int64
		chat        string
		text        func(e *testEnv) string
		age         int
		wantReply   string // substring; "" means no reply expected
		wantOutcome string
	}{
		{name: "wrong code", uid: ownerID, chat: "private", age: 1,
			text:      func(e *testEnv) string { return "/update 000000" },
			wantReply: "Wrong code", wantOutcome: "refused_wrong_code"},
		{name: "code from two steps ago", uid: ownerID, chat: "private", age: 1,
			text:      func(e *testEnv) string { return "/update " + e.code(-2) },
			wantReply: "Wrong code", wantOutcome: "refused_wrong_code"},
		{name: "expired message", uid: ownerID, chat: "private", age: 300,
			text:      func(e *testEnv) string { return "/update " + e.code(0) },
			wantReply: "too old", wantOutcome: "refused_expired"},
		{name: "group chat", uid: ownerID, chat: "supergroup", age: 1,
			text:      func(e *testEnv) string { return "/update " + e.code(0) },
			wantReply: "private chat", wantOutcome: "refused_not_private"},
		{name: "not on the allowlist", uid: 7, chat: "private", age: 1,
			text:      func(e *testEnv) string { return "/update " + e.code(0) },
			wantReply: "", wantOutcome: "denied"},
		{name: "five digits", uid: ownerID, chat: "private", age: 1,
			text: func(e *testEnv) string { return "/update 12345" }, wantReply: "Usage", wantOutcome: "refused_bad_format"},
		{name: "seven digits", uid: ownerID, chat: "private", age: 1,
			text: func(e *testEnv) string { return "/update 1234567" }, wantReply: "Usage", wantOutcome: "refused_bad_format"},
		{name: "letters", uid: ownerID, chat: "private", age: 1,
			text: func(e *testEnv) string { return "/update abcdef" }, wantReply: "Usage", wantOutcome: "refused_bad_format"},
		{name: "no code", uid: ownerID, chat: "private", age: 1,
			text: func(e *testEnv) string { return "/update" }, wantReply: "Usage", wantOutcome: "refused_bad_format"},
		{name: "extra arguments", uid: ownerID, chat: "private", age: 1,
			text:      func(e *testEnv) string { return "/update " + e.code(0) + " now" },
			wantReply: "Usage", wantOutcome: "refused_bad_format"},
		{name: "no release tag", uid: ownerID, chat: "private", age: 1,
			setup:     func(e *testEnv) { e.rel.tagErr = release.ErrNoTag },
			text:      func(e *testEnv) string { return "/update " + e.code(0) },
			wantReply: "Cut a tag first", wantOutcome: "refused_no_tag"},
		{name: "github rate limited", uid: ownerID, chat: "private", age: 1,
			setup:     func(e *testEnv) { e.rel.tagErr = &release.RateLimitError{Reset: time.Unix(1_900_000_000, 0)} },
			text:      func(e *testEnv) string { return "/update " + e.code(0) },
			wantReply: "rate limit", wantOutcome: "refused_rate_limited"},
		{name: "github unreachable", uid: ownerID, chat: "private", age: 1,
			setup:     func(e *testEnv) { e.rel.tagErr = errors.New("dial tcp: connection refused") },
			text:      func(e *testEnv) string { return "/update " + e.code(0) },
			wantReply: "Could not ask GitHub", wantOutcome: "refused_github_error"},
		{name: "already on the newest tag", uid: ownerID, chat: "private", age: 1,
			setup:     func(e *testEnv) { e.setCurrent("v1.3.0") },
			text:      func(e *testEnv) string { return "/update " + e.code(0) },
			wantReply: "Already on the newest release", wantOutcome: "already_current"},
		{name: "deployed version newer than newest tag", uid: ownerID, chat: "private", age: 1,
			setup:     func(e *testEnv) { e.setCurrent("v1.4.0") },
			text:      func(e *testEnv) string { return "/update " + e.code(0) },
			wantReply: "Not downgrading", wantOutcome: "refused_downgrade"},
		{name: "CI pending", uid: ownerID, chat: "private", age: 1,
			setup: func(e *testEnv) {
				e.rel.ci = release.CIResult{State: release.CIPending, Detail: "still running: lab"}
			},
			text:      func(e *testEnv) string { return "/update " + e.code(0) },
			wantReply: "CI is still running", wantOutcome: "refused_ci_pending"},
		{name: "CI failed", uid: ownerID, chat: "private", age: 1,
			setup: func(e *testEnv) {
				e.rel.ci = release.CIResult{State: release.CIFailed, Detail: "failed: go control-plane"}
			},
			text:      func(e *testEnv) string { return "/update " + e.code(0) },
			wantReply: "CI failed", wantOutcome: "refused_ci_failed"},
		{name: "no CI runs", uid: ownerID, chat: "private", age: 1,
			setup:     func(e *testEnv) { e.rel.ci = release.CIResult{State: release.CINone} },
			text:      func(e *testEnv) string { return "/update " + e.code(0) },
			wantReply: "no CI runs", wantOutcome: "refused_ci_none"},
		{name: "CI api error", uid: ownerID, chat: "private", age: 1,
			setup:     func(e *testEnv) { e.rel.ciErr = errors.New("boom") },
			text:      func(e *testEnv) string { return "/update " + e.code(0) },
			wantReply: "Could not read CI", wantOutcome: "refused_ci_error"},
		{name: "update already in progress", uid: ownerID, chat: "private", age: 1,
			setup: func(e *testEnv) {
				_ = os.WriteFile(filepath.Join(e.cfg.RequestsDir, "update.request"), []byte("v1.2.5\nabcdefabcdefabcd\n"), 0o644)
			},
			text:      func(e *testEnv) string { return "/update " + e.code(0) },
			wantReply: "already in progress", wantOutcome: "refused_in_progress"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, nil)
			if tt.setup != nil {
				tt.setup(e)
			}
			preexisting := e.requestExists()
			text := tt.text(e)
			e.send(tt.uid, tt.chat, text, tt.age)

			if tt.wantReply == "" {
				if e.tg.count() != 0 {
					t.Errorf("replied to an ignored user: %q", e.tg.last())
				}
			} else if !strings.Contains(e.tg.last(), tt.wantReply) {
				t.Errorf("reply %q lacks %q", e.tg.last(), tt.wantReply)
			}
			if !strings.Contains(e.audit(), `"outcome":"`+tt.wantOutcome) {
				t.Errorf("audit lacks outcome %s:\n%s", tt.wantOutcome, e.audit())
			}
			if !preexisting && e.requestExists() {
				t.Error("a request was dropped on a refusal")
			}
			if e.lastStep() != "" {
				t.Errorf("a code was spent on a refusal (last step %s)", e.lastStep())
			}
			if f := strings.Fields(text); len(f) > 1 && codeRegex.MatchString(f[1]) && strings.Contains(e.audit(), f[1]) {
				t.Error("the audit log contains the code")
			}
		})
	}
}

func TestUpdateSuccessDropsRequestAndSpendsCode(t *testing.T) {
	e := newEnv(t, nil)
	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(os.Stderr)

	code := e.code(0)
	e.send(ownerID, "private", "/update "+code, 1)

	raw, err := os.ReadFile(filepath.Join(e.cfg.RequestsDir, "update.request"))
	if err != nil {
		t.Fatalf("no request dropped: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 || lines[0] != "v1.3.0" || len(lines[1]) < 16 {
		t.Fatalf("request content %q", raw)
	}
	if !strings.Contains(e.tg.last(), "Updating v1.2.0 -> v1.3.0") {
		t.Errorf("reply %q", e.tg.last())
	}
	if len(e.tg.deleted) != 1 {
		t.Error("the message carrying the code was not deleted")
	}
	if e.lastStep() == "" {
		t.Error("the code was not recorded as used")
	}
	if !strings.Contains(e.audit(), `"outcome":"requested"`) {
		t.Errorf("audit: %s", e.audit())
	}
	for name, content := range map[string]string{"audit": e.audit(), "log": logs.String(), "reply": strings.Join(e.tg.sent, "\n")} {
		if strings.Contains(content, code) {
			t.Errorf("the code appears in the %s", name)
		}
	}
	if fi, _ := os.Stat(filepath.Join(e.cfg.BotDir, "notify.json")); fi == nil {
		t.Error("no notify marker")
	}
}

func TestReusedCodeIsRefused(t *testing.T) {
	e := newEnv(t, nil)
	code := e.code(0)
	e.send(ownerID, "private", "/update "+code, 1)
	if !e.requestExists() {
		t.Fatal("first use refused")
	}
	e.clearRequest() // the host consumed it and the update ended
	e.rel.tag = release.Tag{Name: "v1.4.0", SHA: sha}
	e.send(ownerID, "private", "/update "+code, 1)
	if !strings.Contains(e.tg.last(), "already used") {
		t.Fatalf("reply %q", e.tg.last())
	}
	if e.requestExists() {
		t.Fatal("a reused code dropped a request")
	}
}

func TestPreviousStepCodeAcceptedOnceThenNextStepWorks(t *testing.T) {
	e := newEnv(t, nil)
	e.send(ownerID, "private", "/update "+e.code(-1), 1) // clock skew: previous step
	if !e.requestExists() {
		t.Fatalf("skewed code refused: %q", e.tg.last())
	}
	e.clearRequest()
	e.rel.tag = release.Tag{Name: "v1.4.0", SHA: sha}
	e.send(ownerID, "private", "/update "+e.code(-1), 1) // same step again
	if e.requestExists() {
		t.Fatal("same step accepted twice")
	}
	e.send(ownerID, "private", "/update "+e.code(0), 1) // next step is fine
	if !e.requestExists() {
		t.Fatalf("next step refused: %q", e.tg.last())
	}
}

func TestLockoutAfterRepeatedWrongCodesSurvivesRestart(t *testing.T) {
	e := newEnv(t, nil)
	for i := 0; i < 5; i++ {
		e.send(ownerID, "private", "/update 000000", 1)
	}
	if !strings.Contains(e.tg.last(), "locked") {
		t.Fatalf("5th reply %q", e.tg.last())
	}
	e.send(ownerID, "private", "/update "+e.code(0), 1) // even the right code is refused now
	if !strings.Contains(e.tg.last(), "Locked") || e.requestExists() {
		t.Fatalf("right code during lockout: %q, request=%v", e.tg.last(), e.requestExists())
	}
	restarted := New(e.cfg, e.tg, e.rel, func() time.Time { return e.now })
	restarted.HandleMessage(e.ctx, Message{MessageID: 99, From: &User{ID: ownerID},
		Chat: Chat{ID: ownerID, Type: "private"}, Date: e.now.Unix(), Text: "/update " + e.code(0)})
	if e.requestExists() {
		t.Fatal("lockout did not survive a restart")
	}
	e.now = e.now.Add(16 * time.Minute) // lock over
	e.send(ownerID, "private", "/update "+e.code(0), 1)
	if !e.requestExists() {
		t.Fatalf("still refused after the lock ended: %q", e.tg.last())
	}
}

func TestRequireCIOffSkipsTheCheck(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.RequireCI = false })
	e.rel.ci = release.CIResult{State: release.CIFailed}
	e.send(ownerID, "private", "/update "+e.code(0), 1)
	if !e.requestExists() || e.rel.ciCalls != 0 {
		t.Fatalf("request=%v ciCalls=%d", e.requestExists(), e.rel.ciCalls)
	}
}

func TestConcurrentUpdatesDropOnlyOneRequest(t *testing.T) {
	e := newEnv(t, nil)
	code := e.code(0)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.b.HandleMessage(e.ctx, Message{MessageID: 1, From: &User{ID: ownerID},
				Chat: Chat{ID: ownerID, Type: "private"}, Date: e.now.Unix(), Text: "/update " + code})
		}()
	}
	wg.Wait()
	accepted := 0
	for _, s := range e.tg.sent {
		if strings.Contains(s, "Code accepted") {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("%d requests accepted, want 1: %q", accepted, e.tg.sent)
	}
}

func TestResultIsReportedAndMarkerCleared(t *testing.T) {
	tests := []struct {
		status, want string
	}{
		{"success", "Update finished: v1.2.0 -> v1.3.0"},
		{"rolled_back", "rolled back to v1.2.0"},
		{"failed", "Update to v1.3.0 failed"},
		{"refused", "refused by the host"},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			e := newEnv(t, nil)
			e.send(ownerID, "private", "/update "+e.code(0), 1)
			raw, _ := os.ReadFile(filepath.Join(e.cfg.RequestsDir, "update.request"))
			id := strings.Split(strings.TrimSpace(string(raw)), "\n")[1]
			res, _ := json.Marshal(Result{RequestID: id, Status: tt.status, From: "v1.2.0", To: "v1.3.0", Detail: "detail text"})
			// a result for another request must be ignored
			other, _ := json.Marshal(Result{RequestID: "ffffffffffffffff", Status: "success"})
			_ = os.WriteFile(filepath.Join(e.cfg.ResultsDir, "update-result.json"), other, 0o644)
			time.Sleep(10 * time.Millisecond)
			_ = os.WriteFile(filepath.Join(e.cfg.ResultsDir, "update-result.json"), res, 0o644)
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) && !strings.Contains(e.tg.last(), tt.want) {
				time.Sleep(5 * time.Millisecond)
			}
			if !strings.Contains(e.tg.last(), tt.want) {
				t.Fatalf("last message %q, want %q", e.tg.last(), tt.want)
			}
			if _, err := os.Stat(filepath.Join(e.cfg.BotDir, "notify.json")); !os.IsNotExist(err) {
				t.Error("notify marker not cleared")
			}
		})
	}
}

// The bot restarts (or is itself replaced by the update) before the result exists: it still reports.
func TestResultReportedAfterBotRestart(t *testing.T) {
	e := newEnv(t, nil)
	e.send(ownerID, "private", "/update "+e.code(0), 1)
	raw, _ := os.ReadFile(filepath.Join(e.cfg.RequestsDir, "update.request"))
	id := strings.Split(strings.TrimSpace(string(raw)), "\n")[1]
	e.cancel() // the first bot goes away before the update ends
	e.b.Wait()
	e.clearRequestOnly()
	res, _ := json.Marshal(Result{RequestID: id, Status: "success", From: "v1.2.0", To: "v1.3.0", Detail: "healthy"})
	_ = os.WriteFile(filepath.Join(e.cfg.ResultsDir, "update-result.json"), res, 0o644)

	tg2 := &fakeTG{}
	b2 := New(e.cfg, tg2, e.rel, func() time.Time { return e.now })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = b2.Run(ctx); close(done) }()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(tg2.last(), "Update finished") {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if !strings.Contains(tg2.last(), "Update finished: v1.2.0 -> v1.3.0") {
		t.Fatalf("restarted bot said %q", tg2.last())
	}
}

func TestIDStatusHelpAndUnknownUsers(t *testing.T) {
	e := newEnv(t, nil)
	e.send(7, "private", "/id", 1)
	if !strings.Contains(e.tg.last(), "user id is 7") {
		t.Errorf("/id reply %q", e.tg.last())
	}
	n := e.tg.count()
	e.send(7, "private", "/status", 1)
	e.send(7, "private", "/help", 1)
	e.send(7, "private", "hello", 1)
	if e.tg.count() != n {
		t.Error("a user off the allowlist got an answer")
	}
	e.send(ownerID, "private", "/status", 1)
	if s := e.tg.last(); !strings.Contains(s, "Deployed: v1.2.0") || !strings.Contains(s, "Last update: none") {
		t.Errorf("/status reply %q", s)
	}
	e.send(ownerID, "private", "/help@SomeBot", 1)
	if !strings.Contains(e.tg.last(), "/update <6-digit code>") {
		t.Errorf("/help reply %q", e.tg.last())
	}
}
