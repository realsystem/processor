package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/processor/control-plane/internal/jobtypes"
)

func typedEnv(t *testing.T, strict bool) *env {
	t.Helper()
	e := newEnv(t, "tok", "token")
	reg, err := jobtypes.New([]jobtypes.Type{
		{Name: "video.frames", Version: "1", InputSchema: map[string]interface{}{"type": "object", "required": []interface{}{"source"}}},
		{
			Name: "video.frames", Version: "2", Description: "adds fps",
			InputSchema: map[string]interface{}{
				"type": "object", "required": []interface{}{"source", "fps"},
				"properties": map[string]interface{}{"fps": map[string]interface{}{"type": "integer", "minimum": float64(1)}},
			},
			ResultSchema: map[string]interface{}{"type": "object", "required": []interface{}{"frames"}},
		},
	}, strict)
	if err != nil {
		t.Fatal(err)
	}
	e.api.SetJobTypes(reg)
	return e
}

func (e *env) submit(job map[string]interface{}) *httptest.ResponseRecorder {
	return e.userReq(http.MethodPost, "/api/v1/jobs", job, nil)
}

func errCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var out struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	return out.Error.Code
}

func TestTypedSubmission(t *testing.T) {
	e := typedEnv(t, false)

	// the newest version applies when none is named, and the input is checked against it
	w := e.submit(map[string]interface{}{"job_type": "video.frames", "input_metadata": map[string]interface{}{"source": "x"}})
	if w.Code != 400 || errCode(t, w) != "invalid_input" || !strings.Contains(w.Body.String(), `\"fps\" is required`) {
		t.Fatalf("newest version must require fps: %d %s", w.Code, w.Body)
	}
	// an older version is still available, by name@version or by a separate field
	for _, body := range []map[string]interface{}{
		{"job_type": "video.frames@1", "input_metadata": map[string]interface{}{"source": "x"}},
		{"job_type": "video.frames", "job_type_version": "1", "input_metadata": map[string]interface{}{"source": "x"}},
	} {
		w := e.submit(body)
		job := data(t, w)
		if w.Code != 200 || job["job_type"] != "video.frames" || job["job_type_version"] != "1" {
			t.Fatalf("%v: %d %s", body, w.Code, w.Body)
		}
	}
	// version 2 accepts a good input and rejects a bad one
	good := map[string]interface{}{"source": "x", "fps": 2}
	if job := data(t, e.submit(map[string]interface{}{"job_type": "video.frames@2", "input_metadata": good})); job["job_type_version"] != "2" {
		t.Fatalf("version 2: %v", job)
	}
	w = e.submit(map[string]interface{}{"job_type": "video.frames@2", "input_metadata": map[string]interface{}{"source": "x", "fps": 0}})
	if w.Code != 400 || !strings.Contains(w.Body.String(), "at least 1") {
		t.Fatalf("fps 0: %d %s", w.Code, w.Body)
	}
	if w := e.submit(map[string]interface{}{"job_type": "video.frames@9"}); w.Code != 400 || errCode(t, w) != "unknown_job_type_version" {
		t.Fatalf("unknown version: %d %s", w.Code, w.Body)
	}
}

func TestUntypedJobsAreUnchanged(t *testing.T) {
	e := typedEnv(t, false)
	job := data(t, e.submit(map[string]interface{}{"job_type": "llm.chat", "input_metadata": map[string]interface{}{"anything": true}}))
	if job["job_type"] != "llm.chat" || job["job_type_version"] != "0" {
		t.Fatalf("an unlisted type is accepted as version 0: %v", job)
	}
	// and so is a server with no registry at all
	plain := newEnv(t, "tok", "token")
	if job := data(t, plain.submit(map[string]interface{}{"job_type": "video.frames", "input_metadata": map[string]interface{}{}})); job["job_type_version"] != "0" {
		t.Fatalf("no registry: %v", job)
	}
}

func TestStrictRegistryRefusesUnlistedTypes(t *testing.T) {
	e := typedEnv(t, true)
	if w := e.submit(map[string]interface{}{"job_type": "llm.chat"}); w.Code != 400 || errCode(t, w) != "unknown_job_type" {
		t.Fatalf("strict: %d %s", w.Code, w.Body)
	}
}

func TestIdempotencyKeyIsPerTypeVersion(t *testing.T) {
	e := typedEnv(t, false)
	h := map[string]string{"Idempotency-Key": "k1"}
	post := func(jt string, in map[string]interface{}) *httptest.ResponseRecorder {
		return e.userReq(http.MethodPost, "/api/v1/jobs", map[string]interface{}{"job_type": jt, "input_metadata": in}, h)
	}
	first := data(t, post("video.frames@1", map[string]interface{}{"source": "x"}))
	again := data(t, post("video.frames@1", map[string]interface{}{"source": "x"}))
	if first["id"] != again["id"] || again["idempotent_replay"] != true {
		t.Fatalf("same key and version must replay: %v %v", first, again)
	}
	if w := post("video.frames@2", map[string]interface{}{"source": "x", "fps": 2}); w.Code != http.StatusConflict {
		t.Fatalf("same key, another version: %d %s", w.Code, w.Body)
	}
}

func TestResultMustMatchTheContract(t *testing.T) {
	e := typedEnv(t, false)
	job := data(t, e.submit(map[string]interface{}{
		"job_type": "video.frames@2", "max_attempts": 3, "input_metadata": map[string]interface{}{"source": "x", "fps": 2},
	}))
	id := job["id"].(string)

	call := func(path string, body map[string]interface{}) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
		r.Header.Set("X-Worker-Token", "tok")
		w := httptest.NewRecorder()
		if strings.HasSuffix(path, "/poll") {
			e.api.WorkerPoll(w, r)
		} else {
			e.api.WorkerCompleteJob(w, r)
		}
		return w
	}
	polled := data(t, call("/api/v1/worker/poll", map[string]interface{}{"worker_id": "wrk_tok", "capabilities": []string{"video.frames"}}))
	if polled["job"].(map[string]interface{})["job_type_version"] != "2" {
		t.Fatalf("the worker is told the version: %v", polled)
	}
	if err := e.jobs.MarkRunning(id, "wrk_tok"); err != nil {
		t.Fatal(err)
	}

	bad := call("/api/v1/worker/jobs/"+id+"/complete", map[string]interface{}{"worker_id": "wrk_tok", "result_metadata": map[string]interface{}{"oops": 1}})
	if bad.Code != http.StatusUnprocessableEntity || errCode(t, bad) != "invalid_result" {
		t.Fatalf("a result that breaks the contract: %d %s", bad.Code, bad.Body)
	}
	got := data(t, e.userReq(http.MethodGet, "/api/v1/jobs/"+id, nil, nil))
	if got["status"] == "succeeded" || !strings.Contains(got["error_text"].(string), "result does not match") {
		t.Fatalf("the attempt failed, the job did not succeed: %v", got)
	}
}

func TestJobTypesListsTheRegistry(t *testing.T) {
	e := typedEnv(t, true)
	tokenPoll(e, "tok")
	d := data(t, e.userReq(http.MethodGet, "/api/v1/job-types", nil, nil))
	reg := d["registry"].([]interface{})
	if len(reg) != 2 || d["strict"] != true {
		t.Fatalf("registry: %v", d)
	}
	newest := reg[0].(map[string]interface{})
	if newest["name"] != "video.frames" || newest["version"] != "2" || newest["input_schema"] == nil || newest["online"] != false {
		t.Fatalf("newest first, with its schema, and not online (the worker runs noop): %v", newest)
	}
	// the old flat list is still there for existing clients
	if types := d["job_types"].([]interface{}); len(types) != 1 || types[0] != "noop" {
		t.Fatalf("job_types: %v", types)
	}
}
