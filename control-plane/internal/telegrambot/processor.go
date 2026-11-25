package telegrambot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Job is the part of a control-plane job the bot reads.
type Job struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	ErrorText string `json:"error_text"`
	Result    struct {
		Content         string  `json:"content"`
		Model           string  `json:"model"`
		DurationSeconds float64 `json:"duration_seconds"`
	} `json:"result_metadata"`
}

// Done reports whether the job will not change any more.
func (j Job) Done() bool {
	switch j.Status {
	case "succeeded", "failed", "dead_letter", "cancelled":
		return true
	}
	return false
}

// Backend is the part of the control-plane API the bot uses.
type Backend interface {
	Models(ctx context.Context) (models []string, def string, err error)
	Submit(ctx context.Context, input map[string]interface{}, sensitive bool) (string, error)
	// Wait blocks until the job is finished or ctx ends.
	Wait(ctx context.Context, id string) (Job, error)
}

// Processor is the HTTP client for the control plane (API-key authenticated).
type Processor struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewProcessor(baseURL, apiKey string) *Processor {
	return &Processor{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, client: &http.Client{Timeout: 40 * time.Second}}
}

func (p *Processor) do(ctx context.Context, method, path string, body interface{}, out interface{}) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.baseURL+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	var env struct {
		Data  json.RawMessage `json:"data"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &env)
	if resp.StatusCode >= 300 {
		msg := http.StatusText(resp.StatusCode)
		if env.Error != nil && env.Error.Message != "" {
			msg = env.Error.Message
		}
		return fmt.Errorf("control plane %s %s: %s (status %d)", method, path, msg, resp.StatusCode)
	}
	if out != nil {
		return json.Unmarshal(env.Data, out)
	}
	return nil
}

func (p *Processor) Models(ctx context.Context) ([]string, string, error) {
	var d struct {
		Models  []string `json:"models"`
		Default string   `json:"default"`
	}
	if err := p.do(ctx, http.MethodGet, "/api/v1/models", nil, &d); err != nil {
		return nil, "", err
	}
	return d.Models, d.Default, nil
}

func (p *Processor) Submit(ctx context.Context, input map[string]interface{}, sensitive bool) (string, error) {
	var j Job
	err := p.do(ctx, http.MethodPost, "/api/v1/jobs", map[string]interface{}{
		"job_type": "llm.chat", "input_metadata": input, "sensitive": sensitive,
	}, &j)
	return j.ID, err
}

func (p *Processor) Wait(ctx context.Context, id string) (Job, error) {
	for {
		var j Job
		// the control plane holds the request for up to ?wait seconds while the job runs
		if err := p.do(ctx, http.MethodGet, "/api/v1/jobs/"+url.PathEscape(id)+"?wait=20", nil, &j); err != nil {
			if ctx.Err() != nil {
				return j, ctx.Err()
			}
			// a transient error must not lose a job that is still running
			select {
			case <-time.After(3 * time.Second):
				continue
			case <-ctx.Done():
				return j, ctx.Err()
			}
		}
		if j.Done() {
			return j, nil
		}
		select {
		case <-ctx.Done():
			return j, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}
