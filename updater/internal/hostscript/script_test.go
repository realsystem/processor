package hostscript

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	reqID   = "0123456789abcdef"
	fakeDkr = `#!/usr/bin/env bash
echo "docker $* TAG=$PROCESSOR_TAG" >> "$FAKE_DIR/calls"
case " $* " in
  *" pull "*) [ "$PROCESSOR_TAG" = "${FAKE_PULL_FAIL:-none}" ] && exit 1; exit 0 ;;
  *" up "*) echo "$PROCESSOR_TAG" > "$FAKE_DIR/running"; exit 0 ;;
esac
`
	fakeCurl = `#!/usr/bin/env bash
running=$(cat "$FAKE_DIR/running" 2>/dev/null)
case ",${FAKE_UNHEALTHY:-}," in *",$running,"*) exit 22 ;; esac
exit 0
`
)

type run struct {
	exit    int
	result  map[string]string
	calls   string
	env     string
	state   string
	process string
}

type opts struct {
	request    string // content of update.request ("" = none)
	asSymlink  bool
	envFile    string // content of .env ("" = none)
	running    string // tag the fake stack runs before the update
	unhealthy  string // comma separated tags the fake health check rejects
	pullFail   string
	preResult  string
	holdLock   bool
	skipResult bool
}

func scriptPath(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs("../../../scripts/processor-update.sh")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("script not found: %v", err)
	}
	return p
}

func execute(t *testing.T, o opts) run {
	t.Helper()
	root := t.TempDir()
	state, proc, fake := filepath.Join(root, "state"), filepath.Join(root, "proc"), filepath.Join(root, "fake")
	for _, d := range []string{filepath.Join(state, "requests"), filepath.Join(state, "results"), proc, fake} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, content string, mode os.FileMode) {
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(fake, "docker"), fakeDkr, 0o755)
	write(filepath.Join(fake, "curl"), fakeCurl, 0o755)
	write(filepath.Join(fake, "running"), o.running+"\n", 0o644)
	write(filepath.Join(proc, "docker-compose.deploy.yml"), "services: {}\n", 0o644)
	if o.envFile != "" {
		write(filepath.Join(proc, ".env"), o.envFile, 0o600)
	}
	if o.preResult != "" {
		write(filepath.Join(state, "results", "update-result.json"), o.preResult, 0o644)
	}
	req := filepath.Join(state, "requests", "update.request")
	switch {
	case o.asSymlink:
		write(filepath.Join(root, "secret"), "v9.9.9\n"+reqID+"\n", 0o644)
		if err := os.Symlink(filepath.Join(root, "secret"), req); err != nil {
			t.Fatal(err)
		}
	case o.request != "":
		write(req, o.request, 0o644)
	}
	if o.holdLock {
		if _, err := exec.LookPath("flock"); err != nil {
			t.Skip("flock not installed")
		}
		holder := exec.Command("flock", "-x", filepath.Join(state, "update.lock"), "sleep", "5")
		if err := holder.Start(); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = holder.Process.Kill(); _ = holder.Wait() }()
		time.Sleep(300 * time.Millisecond)
	}

	cmd := exec.Command("bash", scriptPath(t))
	cmd.Env = append(os.Environ(),
		"STATE_DIR="+state, "PROCESSOR_DIR="+proc, "DOCKER_BIN="+filepath.Join(fake, "docker"),
		"CURL_BIN="+filepath.Join(fake, "curl"), "FAKE_DIR="+fake, "HEALTH_TIMEOUT=0", "HEALTH_INTERVAL=0",
		"FAKE_UNHEALTHY="+o.unhealthy, "FAKE_PULL_FAIL="+o.pullFail)
	out, err := cmd.CombinedOutput()
	r := run{state: state, process: string(out)}
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		r.exit = ee.ExitCode()
	default:
		t.Fatalf("running script: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(fake, "calls")); err == nil {
		r.calls = string(b)
	}
	if b, err := os.ReadFile(filepath.Join(proc, ".env")); err == nil {
		r.env = string(b)
	}
	if b, err := os.ReadFile(filepath.Join(state, "results", "update-result.json")); err == nil {
		r.result = map[string]string{}
		if err := json.Unmarshal(b, &r.result); err != nil {
			t.Fatalf("result is not valid JSON: %v\n%s", err, b)
		}
	}
	if _, err := os.Lstat(req); err == nil && !o.holdLock {
		t.Error("the request file was not consumed")
	}
	return r
}

func (r run) current() string {
	b, _ := os.ReadFile(filepath.Join(r.state, "results", "current-tag"))
	return strings.TrimSpace(string(b))
}

func TestUpdateOutcomes(t *testing.T) {
	const envWithSecret = "SESSION_SECRET=keep-me\nPROCESSOR_TAG=v1.2.0\nADMIN_EMAIL=owner@example.invalid\n"
	tests := []struct {
		name         string
		o            opts
		wantExit     int
		wantStatus   string
		wantEnvTag   string
		wantCurrent  string
		wantCalls    []string // substrings that must appear
		noCalls      []string // substrings that must not appear
		detailSubstr string
	}{
		{
			name: "success", o: opts{request: "v1.3.0\n" + reqID + "\n", envFile: envWithSecret, running: "v1.2.0"},
			wantExit: 0, wantStatus: "success", wantEnvTag: "v1.3.0", wantCurrent: "v1.3.0",
			wantCalls: []string{"pull TAG=v1.3.0", "up -d TAG=v1.3.0"},
		},
		{
			name:     "health check fails on the new tag: rolled back",
			o:        opts{request: "v1.3.0\n" + reqID + "\n", envFile: envWithSecret, running: "v1.2.0", unhealthy: "v1.3.0"},
			wantExit: 1, wantStatus: "rolled_back", wantEnvTag: "v1.2.0", wantCurrent: "v1.2.0",
			wantCalls: []string{"up -d TAG=v1.3.0", "up -d TAG=v1.2.0"},
		},
		{
			name:     "update and rollback both unhealthy",
			o:        opts{request: "v1.3.0\n" + reqID + "\n", envFile: envWithSecret, running: "v1.2.0", unhealthy: "v1.3.0,v1.2.0"},
			wantExit: 1, wantStatus: "failed", wantEnvTag: "v1.2.0", detailSubstr: "recover by hand",
		},
		{
			name:     "image cannot be pulled: nothing restarted",
			o:        opts{request: "v1.3.0\n" + reqID + "\n", envFile: envWithSecret, running: "v1.2.0", pullFail: "v1.3.0"},
			wantExit: 1, wantStatus: "failed", wantEnvTag: "v1.2.0", noCalls: []string{" up "}, detailSubstr: "could not pull",
		},
		{
			name:     "already on this tag and healthy: no-op",
			o:        opts{request: "v1.3.0\n" + reqID + "\n", envFile: "PROCESSOR_TAG=v1.3.0\n", running: "v1.3.0"},
			wantExit: 0, wantStatus: "success", wantEnvTag: "v1.3.0", wantCurrent: "v1.3.0",
			noCalls: []string{"pull", " up "}, detailSubstr: "already running",
		},
		{
			name:     "same tag but unhealthy: redeploy attempted",
			o:        opts{request: "v1.3.0\n" + reqID + "\n", envFile: "PROCESSOR_TAG=v1.3.0\n", running: "v1.3.0", unhealthy: "v1.3.0"},
			wantExit: 1, wantStatus: "failed", wantCalls: []string{"pull TAG=v1.3.0", " up "},
		},
		{
			name:     "no settings file yet: current defaults to main",
			o:        opts{request: "v1.0\n" + reqID + "\n", running: "main"},
			wantExit: 0, wantStatus: "success", wantEnvTag: "v1.0", wantCurrent: "v1.0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := execute(t, tt.o)
			if r.exit != tt.wantExit {
				t.Errorf("exit %d, want %d\n%s", r.exit, tt.wantExit, r.process)
			}
			if r.result["status"] != tt.wantStatus || r.result["request_id"] != reqID {
				t.Errorf("result %v, want status %s id %s", r.result, tt.wantStatus, reqID)
			}
			if tt.wantEnvTag != "" && !strings.Contains(r.env, "PROCESSOR_TAG="+tt.wantEnvTag+"\n") {
				t.Errorf(".env = %q, want tag %s", r.env, tt.wantEnvTag)
			}
			if strings.Contains(tt.o.envFile, "keep-me") && !strings.Contains(r.env, "SESSION_SECRET=keep-me") {
				t.Errorf("other settings lost: %q", r.env)
			}
			if tt.wantCurrent != "" && r.current() != tt.wantCurrent {
				t.Errorf("current-tag %q, want %q", r.current(), tt.wantCurrent)
			}
			for _, c := range tt.wantCalls {
				if !strings.Contains(r.calls, c) {
					t.Errorf("docker calls lack %q:\n%s", c, r.calls)
				}
			}
			for _, c := range tt.noCalls {
				if strings.Contains(r.calls, c) {
					t.Errorf("docker calls contain %q:\n%s", c, r.calls)
				}
			}
			if tt.detailSubstr != "" && !strings.Contains(r.result["detail"], tt.detailSubstr) {
				t.Errorf("detail %q lacks %q", r.result["detail"], tt.detailSubstr)
			}
		})
	}
}

func TestHostileRequestsAreRefusedWithoutTouchingDocker(t *testing.T) {
	tests := []struct {
		name    string
		request string
		wantRes bool // a result is written (request id was valid)
	}{
		{"a branch", "main\n" + reqID + "\n", true},
		{"command injection in the tag", "v1.2.3; touch /tmp/pwned\n" + reqID + "\n", true},
		{"path traversal", "../v1.0\n" + reqID + "\n", true},
		{"prerelease", "v1.2.3-rc1\n" + reqID + "\n", true},
		{"sha pin", "sha-abc1234\n" + reqID + "\n", true},
		{"empty", "\n\n", false},
		{"bad request id", "v1.3.0\n$(id)\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := execute(t, opts{request: tt.request, envFile: "PROCESSOR_TAG=v1.2.0\n", running: "v1.2.0"})
			if r.calls != "" {
				t.Errorf("docker was called:\n%s", r.calls)
			}
			if strings.Contains(r.env, "v1.3.0") || !strings.Contains(r.env, "PROCESSOR_TAG=v1.2.0") {
				t.Errorf(".env changed: %q", r.env)
			}
			if tt.wantRes && r.result["status"] != "refused" {
				t.Errorf("result %v, want refused", r.result)
			}
			if !tt.wantRes && r.result != nil {
				t.Errorf("a result was written for an invalid request id: %v", r.result)
			}
		})
	}
}

func TestSymlinkedRequestIsDroppedUnread(t *testing.T) {
	r := execute(t, opts{asSymlink: true, envFile: "PROCESSOR_TAG=v1.2.0\n", running: "v1.2.0"})
	if r.calls != "" || r.result != nil {
		t.Errorf("symlink request acted on: calls=%q result=%v", r.calls, r.result)
	}
}

func TestNoRequestIsANoOp(t *testing.T) {
	r := execute(t, opts{envFile: "PROCESSOR_TAG=v1.2.0\n", running: "v1.2.0"})
	if r.exit != 0 || r.calls != "" || r.result != nil {
		t.Errorf("exit=%d calls=%q result=%v", r.exit, r.calls, r.result)
	}
}

func TestSecondUpdateWhileOneRunsIsRefusedAndKeepsTheirResult(t *testing.T) {
	const running = `{"request_id":"aaaaaaaaaaaaaaaa","status":"success","from":"v1.0","to":"v1.1","detail":"x","finished_at":"t"}`
	r := execute(t, opts{
		request: "v1.3.0\n" + reqID + "\n", envFile: "PROCESSOR_TAG=v1.2.0\n", running: "v1.2.0",
		holdLock: true, preResult: running,
	})
	if r.exit != 0 || r.calls != "" {
		t.Errorf("exit=%d, docker calls=%q\n%s", r.exit, r.calls, r.process)
	}
	if r.result["request_id"] != "aaaaaaaaaaaaaaaa" {
		t.Errorf("the running update's result was overwritten: %v", r.result)
	}
	if !strings.Contains(r.process, "already in progress") {
		t.Errorf("no log line about the refusal:\n%s", r.process)
	}
}
