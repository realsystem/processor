package httpapi

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/processor/control-plane/internal/auth"
	"github.com/processor/control-plane/internal/db"
	"github.com/processor/control-plane/internal/email"
	"github.com/processor/control-plane/internal/jobs"
	"github.com/processor/control-plane/internal/middleware"
	"github.com/processor/control-plane/internal/webhooks"
	"github.com/processor/control-plane/internal/workers"
)

type env struct {
	api  *API
	jobs *jobs.Service
	wrk  *workers.Service
	user *auth.User
	db   *sql.DB
}

func newEnv(t *testing.T, token, mode string) *env {
	t.Helper()
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}
	authSvc := auth.NewService(database)
	user, err := authSvc.Register("owner@example.com", "a-long-password", false)
	if err != nil {
		t.Fatal(err)
	}
	e := &env{
		jobs: jobs.NewService(database, 5*time.Minute),
		wrk:  workers.NewService(database),
		user: user,
		db:   database,
	}
	e.api = NewAPI(authSvc, e.jobs, webhooks.NewService(database), email.NewService(database, email.NewLogProvider()), e.wrk, token)
	e.api.SetWorkerAuth(mode)
	return e
}

func (e *env) userReq(method, path string, body interface{}, headers map[string]string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	r = r.WithContext(context.WithValue(r.Context(), middleware.UserContextKey, e.user))
	w := httptest.NewRecorder()
	switch {
	case path == "/api/v1/jobs":
		e.api.CreateJob(w, r)
	case path == "/api/v1/stats":
		e.api.Stats(w, r)
	case path == "/api/v1/job-types":
		e.api.JobTypes(w, r)
	default:
		e.api.JobAction(w, r)
	}
	return w
}

func data(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var out struct {
		Success bool                   `json:"success"`
		Data    map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("bad JSON %q", w.Body.String())
	}
	return out.Data
}

type signer struct {
	id   string
	priv ed25519.PrivateKey
}

func (s signer) poll(e *env, bodyID string, skew time.Duration) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]interface{}{"worker_id": bodyID, "capabilities": []string{"noop"}})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/worker/poll", bytes.NewReader(body))
	ts := strconv.FormatInt(time.Now().Add(skew).Unix(), 10)
	r.Header.Set("X-Worker-Id", s.id)
	r.Header.Set("X-Worker-Timestamp", ts)
	r.Header.Set("X-Worker-Signature", base64.RawURLEncoding.EncodeToString(
		ed25519.Sign(s.priv, SignaturePayload(http.MethodPost, "/api/v1/worker/poll", ts, body))))
	w := httptest.NewRecorder()
	e.api.WorkerPoll(w, r)
	return w
}

func tokenPoll(e *env, token string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]interface{}{"worker_id": "wrk_tok", "capabilities": []string{"noop"}})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/worker/poll", bytes.NewReader(body))
	if token != "" {
		r.Header.Set("X-Worker-Token", token)
	}
	w := httptest.NewRecorder()
	e.api.WorkerPoll(w, r)
	return w
}

func newSigner(t *testing.T, e *env, id string) signer {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	if err := e.wrk.SetPublicKey(id, base64.RawURLEncoding.EncodeToString(pub), []string{"noop"}); err != nil {
		t.Fatal(err)
	}
	return signer{id, priv}
}

func TestWorkerTokenAuth(t *testing.T) {
	e := newEnv(t, "the-shared-token", "token")
	if w := tokenPoll(e, "the-shared-token"); w.Code != 200 {
		t.Fatalf("valid token: %d %s", w.Code, w.Body)
	}
	if w := tokenPoll(e, "wrong"); w.Code != 401 {
		t.Fatalf("wrong token: %d", w.Code)
	}
	// signatures are ignored in token mode
	if w := newSigner(t, e, "wrk_sig").poll(e, "wrk_sig", 0); w.Code != 401 {
		t.Fatalf("signature in token mode: %d", w.Code)
	}
}

func TestEmptyTokenNeverAuthenticates(t *testing.T) {
	e := newEnv(t, "", "token")
	if w := tokenPoll(e, ""); w.Code != 401 {
		t.Fatalf("no token configured, none sent: %d (must be refused)", w.Code)
	}
}

func TestWorkerSignatureAuth(t *testing.T) {
	e := newEnv(t, "the-shared-token", "signature")
	s := newSigner(t, e, "wrk_sig")
	if w := s.poll(e, "wrk_sig", 0); w.Code != 200 {
		t.Fatalf("signed poll: %d %s", w.Code, w.Body)
	}
	if w := s.poll(e, "wrk_other", 0); w.Code != 403 {
		t.Fatalf("signing worker claims another id: %d", w.Code)
	}
	if w := s.poll(e, "wrk_sig", -5*time.Minute); w.Code != 401 {
		t.Fatalf("stale timestamp: %d", w.Code)
	}
	impostor := signer{"wrk_sig", newSigner(t, e, "wrk_x").priv}
	if w := impostor.poll(e, "wrk_sig", 0); w.Code != 401 {
		t.Fatalf("wrong key: %d", w.Code)
	}
	if w := tokenPoll(e, "the-shared-token"); w.Code != 401 {
		t.Fatalf("token in signature mode: %d", w.Code)
	}
	both := newEnv(t, "the-shared-token", "any")
	if tokenPoll(both, "the-shared-token").Code != 200 || newSigner(t, both, "wrk_a").poll(both, "wrk_a", 0).Code != 200 {
		t.Fatal("mode any accepts both")
	}
}

func TestCreateWithHeadersAndReplay(t *testing.T) {
	e := newEnv(t, "tok", "token")
	tp := "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	h := map[string]string{"Idempotency-Key": "abc", "traceparent": tp, "X-Correlation-Id": "corr_9"}
	body := map[string]interface{}{"job_type": "llm.chat", "input_metadata": map[string]interface{}{"x": 1}, "sensitive": true}
	first := data(t, e.userReq(http.MethodPost, "/api/v1/jobs", body, h))
	if first["trace_parent"] != tp || first["correlation_id"] != "corr_9" || first["sensitive"] != true || first["idempotent_replay"] != false {
		t.Fatalf("created: %v", first)
	}
	again := data(t, e.userReq(http.MethodPost, "/api/v1/jobs", body, h))
	if again["id"] != first["id"] || again["idempotent_replay"] != true {
		t.Fatalf("replay: %v", again)
	}
	body["job_type"] = "noop"
	if w := e.userReq(http.MethodPost, "/api/v1/jobs", body, h); w.Code != http.StatusConflict {
		t.Fatalf("same key, other type: %d", w.Code)
	}
	bad := data(t, e.userReq(http.MethodPost, "/api/v1/jobs", map[string]interface{}{"job_type": "noop"},
		map[string]string{"traceparent": "garbage"}))
	if bad["trace_parent"] != nil {
		t.Fatal("malformed traceparent is dropped")
	}
}

func TestCancelAndWait(t *testing.T) {
	e := newEnv(t, "tok", "token")
	job := data(t, e.userReq(http.MethodPost, "/api/v1/jobs", map[string]interface{}{"job_type": "noop"}, nil))
	id := job["id"].(string)
	start := time.Now()
	got := data(t, e.userReq(http.MethodGet, "/api/v1/jobs/"+id+"?wait=0.6", nil, nil))
	if got["status"] != "queued" || time.Since(start) < 500*time.Millisecond {
		t.Fatalf("wait returns after the timeout while the job is queued: %v", got["status"])
	}
	w := e.userReq(http.MethodPost, "/api/v1/jobs/"+id+"/cancel", nil, nil)
	if w.Code != 200 || data(t, w)["status"] != "canceled" {
		t.Fatalf("cancel: %d %s", w.Code, w.Body)
	}
	start = time.Now()
	if data(t, e.userReq(http.MethodGet, "/api/v1/jobs/"+id+"?wait=5", nil, nil))["status"] != "canceled" || time.Since(start) > time.Second {
		t.Fatal("a finished job returns at once")
	}
	if w := e.userReq(http.MethodPost, "/api/v1/jobs/"+id+"/cancel", nil, nil); w.Code != http.StatusConflict {
		t.Fatalf("second cancel: %d", w.Code)
	}
}

func TestStatsAndJobTypes(t *testing.T) {
	e := newEnv(t, "tok", "token")
	tokenPoll(e, "tok") // wrk_tok comes online with capability noop
	e.userReq(http.MethodPost, "/api/v1/jobs", map[string]interface{}{"job_type": "llm.chat"}, nil)
	st := data(t, e.userReq(http.MethodGet, "/api/v1/stats", nil, nil))
	if st["jobs"].(map[string]interface{})["queued"].(float64) != 1 || len(st["workers_online"].([]interface{})) != 1 {
		t.Fatalf("stats: %v", st)
	}
	types := data(t, e.userReq(http.MethodGet, "/api/v1/job-types", nil, nil))["job_types"].([]interface{})
	if len(types) != 1 || types[0] != "noop" {
		t.Fatalf("job types: %v", types)
	}
}
