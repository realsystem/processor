package bot

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestHTTPTelegram(t *testing.T) {
	var gotPaths []string
	var form url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		_ = r.ParseForm()
		form = r.PostForm
		switch {
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			fmt.Fprint(w, `{"ok":true,"result":[{"update_id":7,"message":{"message_id":3,"from":{"id":42},"chat":{"id":42,"type":"private"},"date":1,"text":"/id"}}]}`)
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			fmt.Fprint(w, `{"ok":true,"result":{}}`)
		default:
			fmt.Fprint(w, `{"ok":false,"error_code":400,"description":"Bad Request: message can't be deleted"}`)
		}
	}))
	defer srv.Close()
	tg := &HTTPTelegram{Base: srv.URL, Token: "TOKEN-PLACEHOLDER"}

	ups, err := tg.GetUpdates(context.Background(), 5, 1)
	if err != nil || len(ups) != 1 || ups[0].Message.From.ID != 42 || ups[0].Message.Chat.Type != "private" {
		t.Fatalf("GetUpdates = %+v, %v", ups, err)
	}
	if form.Get("offset") != "5" || form.Get("timeout") != "1" {
		t.Errorf("form %v", form)
	}
	if err := tg.Send(context.Background(), 42, "hi"); err != nil || form.Get("text") != "hi" || form.Get("parse_mode") != "" {
		t.Errorf("Send: %v form=%v", err, form)
	}
	if err := tg.Delete(context.Background(), 42, 3); err == nil || !strings.Contains(err.Error(), "can't be deleted") {
		t.Errorf("Delete error = %v", err)
	}
	for _, p := range gotPaths {
		if !strings.HasPrefix(p, "/botTOKEN-PLACEHOLDER/") {
			t.Errorf("unexpected path %s", p)
		}
	}
}

func TestTransportErrorsNeverContainTheToken(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close() // connection refused
	tg := &HTTPTelegram{Base: base, Token: "TOKEN-PLACEHOLDER"}
	_, err := tg.GetUpdates(context.Background(), 0, 1)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "TOKEN-PLACEHOLDER") {
		t.Fatalf("error leaks the token: %v", err)
	}
}
