// Package e2e runs the whole flow in one process: a fake Telegram and a fake GitHub served over
// HTTP, the real bot, and the real host script (with a fake docker and curl).
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/realsystem/processor/updater/internal/bot"
	"github.com/realsystem/processor/updater/internal/release"
	"github.com/realsystem/processor/updater/internal/totp"
)

const (
	secretB32 = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP" // test-only secret
	sha       = "3333333333333333333333333333333333333333"
	owner     = int64(42)
)

type fakeTelegram struct {
	mu      sync.Mutex
	pending []string // texts waiting to be delivered as messages from the owner
	sent    []string
	nextID  int64
}

func (f *fakeTelegram) push(text string) {
	f.mu.Lock()
	f.pending = append(f.pending, text)
	f.mu.Unlock()
}
func (f *fakeTelegram) messages() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.sent...)
}

func (f *fakeTelegram) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case strings.HasSuffix(r.URL.Path, "/getUpdates"):
		var ups []map[string]any
		for _, text := range f.pending {
			f.nextID++
			ups = append(ups, map[string]any{"update_id": f.nextID, "message": map[string]any{
				"message_id": f.nextID, "from": map[string]any{"id": owner},
				"chat": map[string]any{"id": owner, "type": "private"}, "date": time.Now().Unix(), "text": text}})
		}
		f.pending = nil
		if len(ups) == 0 {
			f.mu.Unlock()
			time.Sleep(20 * time.Millisecond)
			f.mu.Lock()
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": ups})
	case strings.HasSuffix(r.URL.Path, "/sendMessage"):
		f.sent = append(f.sent, r.PostForm.Get("text"))
		fmt.Fprint(w, `{"ok":true,"result":{}}`)
	default:
		fmt.Fprint(w, `{"ok":true,"result":true}`)
	}
}

func fakeGitHub(ciConclusion string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/tags"):
			fmt.Fprintf(w, `[{"name":"v1.2.0","commit":{"sha":%q}},{"name":"v1.3.0","commit":{"sha":%q}},{"name":"nightly","commit":{"sha":%q}}]`, sha, sha, sha)
		case strings.HasSuffix(r.URL.Path, "/check-runs"):
			fmt.Fprintf(w, `{"total_count":1,"check_runs":[{"name":"go","status":"completed","conclusion":%q}]}`, ci(ciConclusion))
		default:
			http.NotFound(w, r)
		}
	})
}

func ci(c string) string { return c }

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func setup(t *testing.T, ciConclusion string, unhealthy string) (tg *fakeTelegram, state, proc string, key []byte, run func()) {
	t.Helper()
	root := t.TempDir()
	state, proc = filepath.Join(root, "state"), filepath.Join(root, "proc")
	fake := filepath.Join(root, "fake")
	for _, d := range []string{"requests", "results", "bot"} {
		if err := os.MkdirAll(filepath.Join(state, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{proc, fake} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(state, "bot", "totp.secret"):       secretB32 + "\n",
		filepath.Join(proc, ".env"):                      "PROCESSOR_TAG=v1.2.0\n",
		filepath.Join(proc, "docker-compose.deploy.yml"): "services: {}\n",
		filepath.Join(fake, "running"):                   "v1.2.0\n",
		filepath.Join(state, "results", "current-tag"):   "v1.2.0\n",
		filepath.Join(fake, "docker"):                    "#!/usr/bin/env bash\ncase \" $* \" in *\" up \"*) echo \"$PROCESSOR_TAG\" > \"$FAKE_DIR/running\";; esac\nexit 0\n",
		filepath.Join(fake, "curl"):                      "#!/usr/bin/env bash\nrunning=$(cat \"$FAKE_DIR/running\")\ncase \",$FAKE_UNHEALTHY,\" in *\",$running,\"*) exit 22;; esac\nexit 0\n",
	}
	for p, c := range files {
		mode := os.FileMode(0o644)
		if strings.HasSuffix(p, "/docker") || strings.HasSuffix(p, "/curl") {
			mode = 0o755
		}
		if err := os.WriteFile(p, []byte(c), mode); err != nil {
			t.Fatal(err)
		}
	}
	key, _ = totp.DecodeSecret(secretB32)

	tg = &fakeTelegram{}
	tgSrv := httptest.NewServer(tg)
	ghSrv := httptest.NewServer(fakeGitHub(ciConclusion))
	t.Cleanup(tgSrv.Close)
	t.Cleanup(ghSrv.Close)

	cfg := bot.Config{
		AllowedIDs: map[int64]bool{owner: true}, Repo: "owner/repo", RequireCI: true, Version: "e2e",
		BotDir: filepath.Join(state, "bot"), RequestsDir: filepath.Join(state, "requests"), ResultsDir: filepath.Join(state, "results"),
		ResultPoll: 10 * time.Millisecond,
	}
	b := bot.New(cfg, &bot.HTTPTelegram{Base: tgSrv.URL, Token: "TOKEN-PLACEHOLDER"},
		&release.Client{BaseURL: ghSrv.URL, Repo: "owner/repo"}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = b.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	script, _ := filepath.Abs("../../../scripts/processor-update.sh")
	run = func() { // what processor-update.path / .service do on the host
		cmd := exec.Command("bash", script)
		cmd.Env = append(os.Environ(), "STATE_DIR="+state, "PROCESSOR_DIR="+proc,
			"DOCKER_BIN="+filepath.Join(fake, "docker"), "CURL_BIN="+filepath.Join(fake, "curl"), "FAKE_DIR="+fake,
			"FAKE_UNHEALTHY="+unhealthy, "HEALTH_TIMEOUT=0", "HEALTH_INTERVAL=0")
		_ = cmd.Run() // a failed update exits 1; the result file says what happened
	}
	return tg, state, proc, key, run
}

func hasMessage(tg *fakeTelegram, sub string) bool {
	for _, m := range tg.messages() {
		if strings.Contains(m, sub) {
			return true
		}
	}
	return false
}

func TestEndToEndSuccess(t *testing.T) {
	tg, state, proc, key, run := setup(t, "success", "")
	tg.push("/update " + totp.Code(key, totp.Step(time.Now())))
	waitFor(t, "the request", func() bool { _, err := os.Stat(filepath.Join(state, "requests", "update.request")); return err == nil })
	run()
	waitFor(t, "the result message", func() bool { return hasMessage(tg, "Update finished: v1.2.0 -> v1.3.0") })
	if b, _ := os.ReadFile(filepath.Join(proc, ".env")); !strings.Contains(string(b), "PROCESSOR_TAG=v1.3.0") {
		t.Errorf(".env = %q", b)
	}
	tg.push("/status")
	waitFor(t, "/status", func() bool { return hasMessage(tg, "Deployed: v1.3.0") })
	if b, _ := os.ReadFile(filepath.Join(state, "bot", "audit.log")); !strings.Contains(string(b), `"outcome":"requested"`) {
		t.Errorf("audit log: %s", b)
	}
}

func TestEndToEndRollback(t *testing.T) {
	tg, state, proc, key, run := setup(t, "success", "v1.3.0")
	tg.push("/update " + totp.Code(key, totp.Step(time.Now())))
	waitFor(t, "the request", func() bool { _, err := os.Stat(filepath.Join(state, "requests", "update.request")); return err == nil })
	run()
	waitFor(t, "the rollback message", func() bool { return hasMessage(tg, "rolled back to v1.2.0") })
	if b, _ := os.ReadFile(filepath.Join(proc, ".env")); !strings.Contains(string(b), "PROCESSOR_TAG=v1.2.0") {
		t.Errorf(".env = %q", b)
	}
}

func TestEndToEndRefusedWhenCIFailed(t *testing.T) {
	tg, state, _, key, _ := setup(t, "failure", "")
	tg.push("/update " + totp.Code(key, totp.Step(time.Now())))
	waitFor(t, "the refusal", func() bool { return hasMessage(tg, "CI failed on v1.3.0") })
	if _, err := os.Stat(filepath.Join(state, "requests", "update.request")); err == nil {
		t.Fatal("a request was dropped although CI failed")
	}
}
