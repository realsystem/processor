package controlclient

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSignedRequestsAndCancel(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	var verified int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		sig, _ := base64.RawURLEncoding.DecodeString(r.Header.Get("X-Worker-Signature"))
		if r.Header.Get("X-Worker-Id") != "wrk_1" ||
			!ed25519.Verify(pub, SignaturePayload(r.Method, r.URL.Path, r.Header.Get("X-Worker-Timestamp"), body), sig) {
			w.WriteHeader(401)
			return
		}
		verified++
		if r.URL.Path == "/api/v1/worker/heartbeat" {
			w.WriteHeader(409)
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": map[string]string{"code": "canceled", "message": "stop"}})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "data": map[string]interface{}{"job": map[string]interface{}{
			"id": "job_1", "job_type": "llm.chat", "sensitive": true, "trace_parent": "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01",
		}}})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "").WithSigningKey("wrk_1", priv)
	job, err := c.Poll("wrk_1", []string{"llm.chat"})
	if err != nil || job.ID != "job_1" || !job.Sensitive || job.TraceParent == "" {
		t.Fatalf("poll: %v %+v", err, job)
	}
	if err := c.Heartbeat("wrk_1", "job_1"); !errors.Is(err, ErrCanceled) {
		t.Fatalf("heartbeat must report the cancel: %v", err)
	}
	if verified != 2 {
		t.Fatalf("verified %d signed requests", verified)
	}
	unsigned := NewClient(srv.URL, "tok")
	var se *StatusError
	if _, err := unsigned.Poll("wrk_1", nil); !errors.As(err, &se) || se.Status != 401 {
		t.Fatalf("unsigned: %v", err)
	}
}
