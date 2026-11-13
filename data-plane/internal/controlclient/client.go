package controlclient

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// ErrCanceled is returned by Heartbeat when the job was canceled: stop working on it.
var ErrCanceled = errors.New("job was canceled")

// Job represents a job from the control plane
type Job struct {
	ID             string                 `json:"id"`
	JobType        string                 `json:"job_type"`
	InputMetadata  map[string]interface{} `json:"input_metadata"`
	MaxAttempts    int                    `json:"max_attempts"`
	AttemptCount   int                    `json:"attempt_count"`
	LeaseExpiresAt string                 `json:"lease_expires_at"`
	TraceParent    string                 `json:"trace_parent"`
	CorrelationID  string                 `json:"correlation_id"`
	Sensitive      bool                   `json:"sensitive"`
}

// Client communicates with the control plane
type Client struct {
	baseURL     string
	workerToken string
	httpClient  *http.Client

	// signing (optional): when set, every request carries an Ed25519 signature
	workerID string
	key      ed25519.PrivateKey
}

// WithSigningKey makes the client sign its requests as workerID (WORKER_AUTH=signature
// or any on the control plane). The shared token is still sent if one is configured.
func (c *Client) WithSigningKey(workerID string, key ed25519.PrivateKey) *Client {
	c.workerID = workerID
	c.key = key
	return c
}

// SignaturePayload must match the control plane's: method, path, unix timestamp and the
// hex SHA-256 of the body, separated by newlines.
func SignaturePayload(method, path, timestamp string, body []byte) []byte {
	sum := sha256.Sum256(body)
	return []byte(method + "\n" + path + "\n" + timestamp + "\n" + hex.EncodeToString(sum[:]))
}

// NewClient creates a new control plane client
func NewClient(baseURL, workerToken string) *Client {
	return &Client{
		baseURL:     baseURL,
		workerToken: workerToken,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Poll polls for the next available job
func (c *Client) Poll(workerID string, capabilities []string) (*Job, error) {
	reqBody := map[string]interface{}{
		"worker_id":    workerID,
		"capabilities": capabilities,
	}

	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			Job *Job `json:"job"`
		} `json:"data"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	err := c.request("POST", "/api/v1/worker/poll", reqBody, &resp)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("poll failed: %s", resp.Error.Message)
	}

	return resp.Data.Job, nil
}

// Heartbeat sends a heartbeat for a job
func (c *Client) Heartbeat(workerID, jobID string) error {
	reqBody := map[string]interface{}{
		"worker_id": workerID,
		"job_id":    jobID,
	}

	var resp struct {
		Success bool `json:"success"`
	}

	err := c.request("POST", "/api/v1/worker/heartbeat", reqBody, &resp)
	var se *StatusError
	if errors.As(err, &se) && se.Status == http.StatusConflict && se.Code == "canceled" {
		return ErrCanceled
	}
	return err
}

// MarkRunning marks a job as running
func (c *Client) MarkRunning(workerID, jobID string) error {
	reqBody := map[string]interface{}{
		"worker_id": workerID,
	}

	var resp struct {
		Success bool `json:"success"`
	}

	path := fmt.Sprintf("/api/v1/worker/jobs/%s/start", jobID)
	return c.request("POST", path, reqBody, &resp)
}

// Complete marks a job as completed
func (c *Client) Complete(workerID, jobID string, resultMetadata map[string]interface{}) error {
	reqBody := map[string]interface{}{
		"worker_id":       workerID,
		"result_metadata": resultMetadata,
	}

	var resp struct {
		Success bool `json:"success"`
	}

	path := fmt.Sprintf("/api/v1/worker/jobs/%s/complete", jobID)
	return c.request("POST", path, reqBody, &resp)
}

// Fail marks a job as failed
func (c *Client) Fail(workerID, jobID, errorText string, shouldRetry bool) error {
	reqBody := map[string]interface{}{
		"worker_id":    workerID,
		"error_text":   errorText,
		"should_retry": shouldRetry,
	}

	var resp struct {
		Success bool `json:"success"`
	}

	path := fmt.Sprintf("/api/v1/worker/jobs/%s/fail", jobID)
	return c.request("POST", path, reqBody, &resp)
}

// StatusError is a non-2xx answer from the control plane.
type StatusError struct {
	Status  int
	Code    string
	Message string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("request failed with status %d: %s %s", e.Status, e.Code, e.Message)
}

func (c *Client) request(method, path string, reqBody interface{}, respBody interface{}) error {
	var payload []byte
	if reqBody != nil {
		var err error
		if payload, err = json.Marshal(reqBody); err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}
	}

	req, err := http.NewRequest(method, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.workerToken != "" {
		req.Header.Set("X-Worker-Token", c.workerToken)
	}
	if c.key != nil {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		req.Header.Set("X-Worker-Id", c.workerID)
		req.Header.Set("X-Worker-Timestamp", ts)
		req.Header.Set("X-Worker-Signature", base64.RawURLEncoding.EncodeToString(
			ed25519.Sign(c.key, SignaturePayload(method, req.URL.Path, ts, payload))))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		se := &StatusError{Status: resp.StatusCode, Message: string(bodyBytes)}
		var e struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(bodyBytes, &e) == nil && e.Error.Code != "" {
			se.Code, se.Message = e.Error.Code, e.Error.Message
		}
		return se
	}

	if respBody != nil {
		if err := json.NewDecoder(resp.Body).Decode(respBody); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}
