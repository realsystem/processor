package release

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const sha1 = "1111111111111111111111111111111111111111"

func tagsJSON(names ...string) string {
	var parts []string
	for _, n := range names {
		parts = append(parts, fmt.Sprintf(`{"name":%q,"commit":{"sha":%q}}`, n, sha1))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func client(srv *httptest.Server) *Client {
	return &Client{BaseURL: srv.URL, Repo: "owner/repo", HTTP: srv.Client()}
}

func TestLatestTag(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    string
		wantErr error
	}{
		{"highest release wins", tagsJSON("v1.2", "v1.10.1", "v1.9.9", "v0.99"), "v1.10.1", nil},
		{"branches and prereleases are never picked", tagsJSON("main", "v2.0-rc1", "release-3", "v1.4.0.1", "1.5", "v1.0"), "v1.0", nil},
		{"two-part tag", tagsJSON("v3.1"), "v3.1", nil},
		{"none", tagsJSON("nightly", "latest"), "", ErrNoTag},
		{"empty list", "[]", "", ErrNoTag},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/repos/owner/repo/tags" {
					t.Errorf("unexpected path %s", r.URL.Path)
				}
				fmt.Fprint(w, tt.body)
			}))
			defer srv.Close()
			got, err := client(srv).LatestTag(context.Background())
			if !errors.Is(err, tt.wantErr) || got.Name != tt.want {
				t.Fatalf("LatestTag = %q, %v; want %q, %v", got.Name, err, tt.want, tt.wantErr)
			}
			if tt.want != "" && got.SHA != sha1 {
				t.Errorf("sha = %q", got.SHA)
			}
		})
	}
}

func TestLatestTagUsesConditionalRequest(t *testing.T) {
	var calls, conditional int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.Header.Get("If-None-Match") == `"abc"` {
			atomic.AddInt32(&conditional, 1)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"abc"`)
		fmt.Fprint(w, tagsJSON("v1.0", "v1.1"))
	}))
	defer srv.Close()
	c := client(srv)
	for i := 0; i < 3; i++ {
		got, err := c.LatestTag(context.Background())
		if err != nil || got.Name != "v1.1" {
			t.Fatalf("call %d: %q, %v", i, got.Name, err)
		}
	}
	if calls != 3 || conditional != 2 {
		t.Fatalf("calls=%d conditional=%d, want 3 and 2", calls, conditional)
	}
}

func TestLatestTagFollowsPages(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			fmt.Fprint(w, tagsJSON("v9.0"))
			return
		}
		w.Header().Set("Link", fmt.Sprintf(`<%s/repos/owner/repo/tags?per_page=100&page=2>; rel="next"`, srv.URL))
		fmt.Fprint(w, tagsJSON("v1.0"))
	}))
	defer srv.Close()
	got, err := client(srv).LatestTag(context.Background())
	if err != nil || got.Name != "v9.0" {
		t.Fatalf("got %q, %v", got.Name, err)
	}
}

func TestAPIFailures(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		check   func(error) bool
	}{
		{"rate limit", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", "1900000000")
			w.WriteHeader(http.StatusForbidden)
		}, func(err error) bool { var e *RateLimitError; return errors.As(err, &e) && !e.Reset.IsZero() }},
		{"plain 403", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) },
			func(err error) bool { var e *APIError; return errors.As(err, &e) && e.Status == 403 }},
		{"server error", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) },
			func(err error) bool { var e *APIError; return errors.As(err, &e) && e.Status == 502 }},
		{"not json", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<html>") },
			func(err error) bool { return err != nil && !errors.Is(err, ErrNoTag) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()
			if _, err := client(srv).LatestTag(context.Background()); !tt.check(err) {
				t.Fatalf("unexpected error %v", err)
			}
			if _, err := client(srv).CI(context.Background(), sha1); err == nil {
				t.Fatal("CI: expected an error")
			}
		})
	}
}

func TestCI(t *testing.T) {
	run := func(status, concl string) string {
		if concl == "" {
			return fmt.Sprintf(`{"name":"job","status":%q,"conclusion":null}`, status)
		}
		return fmt.Sprintf(`{"name":"job","status":%q,"conclusion":%q}`, status, concl)
	}
	body := func(runs ...string) string {
		return fmt.Sprintf(`{"total_count":%d,"check_runs":[%s]}`, len(runs), strings.Join(runs, ","))
	}
	tests := []struct {
		name string
		body string
		want CIState
	}{
		{"all green", body(run("completed", "success"), run("completed", "skipped"), run("completed", "neutral")), CIPassed},
		{"pending", body(run("completed", "success"), run("in_progress", "")), CIPending},
		{"queued", body(run("queued", "")), CIPending},
		{"failure", body(run("completed", "success"), run("completed", "failure")), CIFailed},
		{"cancelled", body(run("completed", "cancelled")), CIFailed},
		{"failure beats pending", body(run("in_progress", ""), run("completed", "timed_out")), CIFailed},
		{"none", `{"total_count":0,"check_runs":[]}`, CINone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "/commits/"+sha1+"/check-runs") {
					t.Errorf("unexpected path %s", r.URL.Path)
				}
				fmt.Fprint(w, tt.body)
			}))
			defer srv.Close()
			got, err := client(srv).CI(context.Background(), sha1)
			if err != nil || got.State != tt.want {
				t.Fatalf("CI = %v, %v; want %v", got.State, err, tt.want)
			}
		})
	}
}

func TestCIRejectsMalformedSHA(t *testing.T) {
	c := &Client{BaseURL: "http://unused", Repo: "o/r"}
	for _, sha := range []string{"", "main", "../x", sha1 + "0", strings.ToUpper(sha1)} {
		if _, err := c.CI(context.Background(), sha); err == nil {
			t.Errorf("sha %q accepted", sha)
		}
	}
}

func TestParseAndCompare(t *testing.T) {
	a, _ := Parse("v1.10")
	b, _ := Parse("v1.9.9")
	if !a.Newer(b) || b.Newer(a) {
		t.Error("v1.10 should be newer than v1.9.9")
	}
	for _, bad := range []string{"1.0", "v1", "v1.0.0.0", "v1.0-rc1", "main", "sha-abc1234", ""} {
		if _, ok := Parse(bad); ok {
			t.Errorf("%q parsed as a release", bad)
		}
	}
}
