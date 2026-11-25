package telegrambot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTelegramClientCallsTheBotAPI(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte(`{"ok":true,"result":[{"update_id":5,"message":{"message_id":1,"from":{"id":42},"chat":{"id":42},"text":"hi"}}]}`))
	}))
	defer srv.Close()

	tg := NewTelegram(srv.URL, "SECRET")
	ups, err := tg.GetUpdates(context.Background(), 5, 30)
	if err != nil || len(ups) != 1 || ups[0].UpdateID != 5 || ups[0].Message.Text != "hi" {
		t.Fatalf("%v %v", ups, err)
	}
	if gotPath != "/botSECRET/getUpdates" || gotBody["offset"] != float64(5) {
		t.Errorf("path %q body %v", gotPath, gotBody)
	}
}

func TestTelegramErrorsNeverLeakTheToken(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // connection refused: net/http puts the full URL, token included, in the error

	err := NewTelegram(url, "SECRET-TOKEN").SendMessage(context.Background(), 1, "x", nil)
	if err == nil || strings.Contains(err.Error(), "SECRET-TOKEN") {
		t.Fatalf("error %v must exist and must not contain the token", err)
	}

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"ok":false,"description":"Unauthorized"}`))
	}))
	defer srv2.Close()
	err = NewTelegram(srv2.URL, "SECRET-TOKEN").SendMessage(context.Background(), 1, "x", nil)
	if err == nil || !strings.Contains(err.Error(), "Unauthorized") || strings.Contains(err.Error(), "SECRET-TOKEN") {
		t.Fatalf("error %v", err)
	}
}

func TestProcessorClient(t *testing.T) {
	var polls int32
	var submitted map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer pk_test" {
			w.WriteHeader(401)
			w.Write([]byte(`{"success":false,"error":{"message":"bad key"}}`))
			return
		}
		switch {
		case r.URL.Path == "/api/v1/models":
			w.Write([]byte(`{"success":true,"data":{"models":["a","b"],"default":"a"}}`))
		case r.URL.Path == "/api/v1/jobs" && r.Method == http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&submitted)
			w.WriteHeader(201)
			w.Write([]byte(`{"success":true,"data":{"id":"job_9","status":"queued"}}`))
		case r.URL.Path == "/api/v1/jobs/job_9":
			if atomic.AddInt32(&polls, 1) < 3 {
				w.Write([]byte(`{"success":true,"data":{"id":"job_9","status":"running"}}`))
				return
			}
			w.Write([]byte(`{"success":true,"data":{"id":"job_9","status":"succeeded","result_metadata":{"content":"hello","model":"a","duration_seconds":2.5}}}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	p := NewProcessor(srv.URL, "pk_test")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	models, def, err := p.Models(ctx)
	if err != nil || len(models) != 2 || def != "a" {
		t.Fatalf("%v %q %v", models, def, err)
	}
	id, err := p.Submit(ctx, map[string]interface{}{"prompt": "hi", "model": "a"}, true)
	if err != nil || id != "job_9" {
		t.Fatalf("%q %v", id, err)
	}
	if submitted["job_type"] != "llm.chat" || submitted["sensitive"] != true {
		t.Errorf("submitted %v", submitted)
	}
	job, err := p.Wait(ctx, id)
	if err != nil || job.Status != "succeeded" || job.Result.Content != "hello" || job.Result.DurationSeconds != 2.5 {
		t.Fatalf("%+v %v", job, err)
	}
	if polls != 3 {
		t.Errorf("polled %d times, want 3", polls)
	}

	if _, _, err := NewProcessor(srv.URL, "wrong").Models(ctx); err == nil || !strings.Contains(err.Error(), "bad key") {
		t.Errorf("error %v", err)
	}
}
