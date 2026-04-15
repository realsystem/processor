package controlclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Job represents a job from the control plane
type Job struct {
	ID              string                 `json:"id"`
	JobType         string                 `json:"job_type"`
	InputMetadata   map[string]interface{} `json:"input_metadata"`
	MaxAttempts     int                    `json:"max_attempts"`
	AttemptCount    int                    `json:"attempt_count"`
	LeaseExpiresAt  string                 `json:"lease_expires_at"`
}

// Client communicates with the control plane
type Client struct {
	baseURL     string
	workerToken string
	httpClient  *http.Client
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

	return c.request("POST", "/api/v1/worker/heartbeat", reqBody, &resp)
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

func (c *Client) request(method, path string, reqBody interface{}, respBody interface{}) error {
	var body io.Reader
	if reqBody != nil {
		jsonData, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}
		body = bytes.NewBuffer(jsonData)
	}

	req, err := http.NewRequest(method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Worker-Token", c.workerToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	if respBody != nil {
		if err := json.NewDecoder(resp.Body).Decode(respBody); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}
