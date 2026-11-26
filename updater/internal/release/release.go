// Package release asks the GitHub API which release tag is newest and whether CI is green on it.
// Only tags named vMAJOR.MINOR[.PATCH] count, so a branch or arbitrary ref can never be deployed.
package release

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TagRegex is the only shape of tag that can be deployed.
var TagRegex = regexp.MustCompile(`^v(\d+)\.(\d+)(?:\.(\d+))?$`)

// Errors a caller tells apart to give the owner a clear message.
var (
	ErrNoTag = errors.New("no release tag vMAJOR.MINOR[.PATCH] found")
)

// RateLimitError means GitHub refused because the quota is used up.
type RateLimitError struct{ Reset time.Time }

func (e *RateLimitError) Error() string {
	if e.Reset.IsZero() {
		return "github api rate limit reached"
	}
	return "github api rate limit reached, resets at " + e.Reset.UTC().Format("15:04 UTC")
}

// APIError is any other unexpected answer from GitHub.
type APIError struct {
	Status int
	Path   string
}

func (e *APIError) Error() string { return fmt.Sprintf("github api %s answered %d", e.Path, e.Status) }

// Tag is a release tag and the commit it points at.
type Tag struct {
	Name                string
	SHA                 string
	Major, Minor, Patch int
}

// Parse fills the version numbers of a tag name; ok is false for names that are not releases.
func Parse(name string) (Tag, bool) {
	m := TagRegex.FindStringSubmatch(name)
	if m == nil {
		return Tag{}, false
	}
	t := Tag{Name: name}
	t.Major, _ = strconv.Atoi(m[1])
	t.Minor, _ = strconv.Atoi(m[2])
	if m[3] != "" {
		t.Patch, _ = strconv.Atoi(m[3])
	}
	return t, true
}

// Newer reports whether a is a higher version than b.
func (a Tag) Newer(b Tag) bool {
	if a.Major != b.Major {
		return a.Major > b.Major
	}
	if a.Minor != b.Minor {
		return a.Minor > b.Minor
	}
	return a.Patch > b.Patch
}

// CIState is the verdict on the check runs of a commit.
type CIState int

// The possible verdicts.
const (
	CIPassed CIState = iota
	CIPending
	CIFailed
	CINone
)

// CIResult is the verdict plus a short reason for the owner.
type CIResult struct {
	State  CIState
	Detail string
}

// Client talks to the GitHub REST API.
type Client struct {
	BaseURL string // https://api.github.com
	Repo    string // owner/name
	Token   string // optional; raises the rate limit
	HTTP    *http.Client

	mu        sync.Mutex
	tagsETag  string
	tagsCache Tag
	tagsHave  bool
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (c *Client) get(ctx context.Context, path, etag string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.BaseURL, "/")+path, nil)
	if err != nil {
		return nil, fmt.Errorf("build github request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "processor-updater")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("github request %s: %w", path, err)
	}
	return resp, nil
}

// checkStatus turns a rate limit or any non-success status into an error.
func checkStatus(resp *http.Response, path string, okCodes ...int) error {
	for _, c := range okCodes {
		if resp.StatusCode == c {
			return nil
		}
	}
	if (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests) &&
		(resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != "") {
		e := &RateLimitError{}
		if v, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			e.Reset = time.Unix(v, 0)
		}
		return e
	}
	return &APIError{Status: resp.StatusCode, Path: path}
}

type tagItem struct {
	Name   string `json:"name"`
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

var nextLink = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// LatestTag returns the highest release tag. The first page is fetched with a conditional
// request (If-None-Match), so an unchanged answer costs no rate limit.
func (c *Client) LatestTag(ctx context.Context) (Tag, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	path := "/repos/" + c.Repo + "/tags?per_page=100"
	var best Tag
	found := false
	for page := 0; page < 5; page++ {
		etag := ""
		if page == 0 {
			etag = c.tagsETag
		}
		resp, err := c.get(ctx, path, etag)
		if err != nil {
			return Tag{}, err
		}
		if page == 0 && resp.StatusCode == http.StatusNotModified && c.tagsHave {
			_ = resp.Body.Close()
			return c.tagsCache, nil
		}
		if err := checkStatus(resp, path, http.StatusOK); err != nil {
			_ = resp.Body.Close()
			return Tag{}, err
		}
		var items []tagItem
		err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&items)
		link := resp.Header.Get("Link")
		newETag := resp.Header.Get("ETag")
		_ = resp.Body.Close()
		if err != nil {
			return Tag{}, fmt.Errorf("decode github tags: %w", err)
		}
		if page == 0 {
			c.tagsETag = newETag
		}
		for _, it := range items {
			t, ok := Parse(it.Name)
			if !ok || it.Commit.SHA == "" {
				continue
			}
			t.SHA = it.Commit.SHA
			if !found || t.Newer(best) {
				best, found = t, true
			}
		}
		m := nextLink.FindStringSubmatch(link)
		if m == nil {
			break
		}
		next := m[1]
		// follow only links on the same API host
		if i := strings.Index(next, "/repos/"); i >= 0 {
			path = next[i:]
		} else {
			break
		}
	}
	if !found {
		c.tagsETag, c.tagsHave = "", false
		return Tag{}, ErrNoTag
	}
	c.tagsCache, c.tagsHave = best, true
	return best, nil
}

type checkRuns struct {
	TotalCount int `json:"total_count"`
	CheckRuns  []struct {
		Name       string  `json:"name"`
		Status     string  `json:"status"`
		Conclusion *string `json:"conclusion"`
	} `json:"check_runs"`
}

var shaRegex = regexp.MustCompile(`^[0-9a-f]{40}$`)

// CI evaluates the check runs of a commit: failed beats pending beats passed; no runs is its
// own verdict, because "nothing ran" is not "everything passed".
func (c *Client) CI(ctx context.Context, sha string) (CIResult, error) {
	if !shaRegex.MatchString(sha) {
		return CIResult{}, fmt.Errorf("refusing to query CI for malformed commit id")
	}
	path := "/repos/" + c.Repo + "/commits/" + sha + "/check-runs?per_page=100"
	resp, err := c.get(ctx, path, "")
	if err != nil {
		return CIResult{}, err
	}
	defer resp.Body.Close()
	if err := checkStatus(resp, path, http.StatusOK); err != nil {
		return CIResult{}, err
	}
	var cr checkRuns
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&cr); err != nil {
		return CIResult{}, fmt.Errorf("decode github check runs: %w", err)
	}
	if cr.TotalCount == 0 || len(cr.CheckRuns) == 0 {
		return CIResult{State: CINone, Detail: "no CI runs on this commit"}, nil
	}
	if cr.TotalCount > len(cr.CheckRuns) {
		return CIResult{}, fmt.Errorf("github returned %d of %d check runs", len(cr.CheckRuns), cr.TotalCount)
	}
	var failed, pending []string
	for _, r := range cr.CheckRuns {
		if r.Status != "completed" {
			pending = append(pending, r.Name)
			continue
		}
		concl := ""
		if r.Conclusion != nil {
			concl = *r.Conclusion
		}
		switch concl {
		case "success", "neutral", "skipped":
		default:
			failed = append(failed, r.Name)
		}
	}
	switch {
	case len(failed) > 0:
		return CIResult{State: CIFailed, Detail: "failed: " + strings.Join(failed, ", ")}, nil
	case len(pending) > 0:
		return CIResult{State: CIPending, Detail: "still running: " + strings.Join(pending, ", ")}, nil
	}
	return CIResult{State: CIPassed, Detail: fmt.Sprintf("%d checks passed", len(cr.CheckRuns))}, nil
}
