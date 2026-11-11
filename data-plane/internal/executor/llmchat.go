package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// LLMChatExecutor runs "llm.chat" jobs: a chat completion with messages, an optional
// JSON output format and token usage in the result. It never logs prompts or answers.
//
// Input (version 1):
//
//	{
//	  "model":    "qwen2.5:14b",                       // optional; LLM_DEFAULT_MODEL
//	  "messages": [{"role": "user", "content": "…"}],  // or "prompt": "…"
//	  "system":   "…",                                 // optional, sent first
//	  "format":   "json" | {JSON schema},              // optional structured output
//	  "options":  {"temperature": 0.2, "max_tokens": 512, "top_p": 0.9, "seed": 1},
//	  "keep_alive": "10m"                              // optional, Ollama only
//	}
//
// Result:
//
//	{"status": "completed", "backend": "ollama", "model": "…",
//	 "message": {"role": "assistant", "content": "…"}, "content": "…",
//	 "done_reason": "stop", "usage": {"prompt_tokens": 12, "completion_tokens": 40},
//	 "duration_seconds": 1.2}
//
// Backend "ollama" calls {base}/api/chat; "openai" calls {base}/v1/chat/completions on
// any OpenAI-compatible server (llama.cpp, vLLM, LM Studio, hosted APIs).
type LLMChatExecutor struct {
	Backend      string // "ollama" (default) or "openai"
	BaseURL      string
	APIKey       string // sent as a Bearer token (openai backend)
	DefaultModel string
	Client       *http.Client
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model     string
	Messages  []chatMessage
	Format    interface{}
	Options   map[string]interface{}
	KeepAlive interface{}
}

func parseChatInput(input map[string]interface{}, defaultModel string) (*chatRequest, error) {
	req := &chatRequest{Model: defaultModel}
	if m, ok := input["model"].(string); ok && m != "" {
		req.Model = m
	}
	if req.Model == "" {
		return nil, fmt.Errorf("model is required (no LLM_DEFAULT_MODEL)")
	}
	if sys, ok := input["system"].(string); ok && sys != "" {
		req.Messages = append(req.Messages, chatMessage{"system", sys})
	}
	switch msgs := input["messages"].(type) {
	case []interface{}:
		for i, raw := range msgs {
			m, ok := raw.(map[string]interface{})
			role, _ := m["role"].(string)
			content, cok := m["content"].(string)
			if !ok || !cok || (role != "system" && role != "user" && role != "assistant") {
				return nil, fmt.Errorf("messages[%d] needs role (system, user, assistant) and content", i)
			}
			req.Messages = append(req.Messages, chatMessage{role, content})
		}
	case nil:
		if p, ok := input["prompt"].(string); ok && p != "" {
			req.Messages = append(req.Messages, chatMessage{"user", p})
		}
	default:
		return nil, fmt.Errorf("messages must be a list")
	}
	if len(req.Messages) == 0 || req.Messages[len(req.Messages)-1].Role == "system" {
		return nil, fmt.Errorf("messages (or prompt) are required")
	}
	switch f := input["format"].(type) {
	case nil:
	case string:
		if f != "json" {
			return nil, fmt.Errorf(`format must be "json" or a JSON schema object`)
		}
		req.Format = f
	case map[string]interface{}:
		req.Format = f
	default:
		return nil, fmt.Errorf(`format must be "json" or a JSON schema object`)
	}
	if o, ok := input["options"].(map[string]interface{}); ok {
		req.Options = o
	}
	req.KeepAlive = input["keep_alive"]
	return req, nil
}

func (e *LLMChatExecutor) Execute(ctx context.Context, jobType string, input map[string]interface{}) (map[string]interface{}, error) {
	req, err := parseChatInput(input, e.DefaultModel)
	if err != nil {
		return nil, err
	}
	client := e.Client
	if client == nil {
		client = &http.Client{} // the job context bounds the call
	}
	start := time.Now()
	var out map[string]interface{}
	if e.Backend == "openai" {
		out, err = e.openai(ctx, client, req)
	} else {
		out, err = e.ollama(ctx, client, req)
	}
	if err != nil {
		return nil, err
	}
	out["status"] = "completed"
	out["duration_seconds"] = time.Since(start).Seconds()
	return out, nil
}

func (e *LLMChatExecutor) ollama(ctx context.Context, client *http.Client, req *chatRequest) (map[string]interface{}, error) {
	body := map[string]interface{}{"model": req.Model, "messages": req.Messages, "stream": false}
	if req.Format != nil {
		body["format"] = req.Format
	}
	if len(req.Options) > 0 {
		opts := map[string]interface{}{}
		for k, v := range req.Options {
			if k == "max_tokens" {
				k = "num_predict"
			}
			opts[k] = v
		}
		body["options"] = opts
	}
	if req.KeepAlive != nil {
		body["keep_alive"] = req.KeepAlive
	}
	var resp struct {
		Model           string      `json:"model"`
		Message         chatMessage `json:"message"`
		DoneReason      string      `json:"done_reason"`
		PromptEvalCount int         `json:"prompt_eval_count"`
		EvalCount       int         `json:"eval_count"`
	}
	if err := postJSON(ctx, client, strings.TrimRight(e.BaseURL, "/")+"/api/chat", "", body, &resp); err != nil {
		return nil, err
	}
	return chatResult("ollama", resp.Model, resp.Message.Content, resp.DoneReason, resp.PromptEvalCount, resp.EvalCount), nil
}

func (e *LLMChatExecutor) openai(ctx context.Context, client *http.Client, req *chatRequest) (map[string]interface{}, error) {
	body := map[string]interface{}{"model": req.Model, "messages": req.Messages, "stream": false}
	for k, v := range req.Options {
		body[k] = v // temperature, max_tokens, top_p, seed have the same names
	}
	switch f := req.Format.(type) {
	case string:
		body["response_format"] = map[string]interface{}{"type": "json_object"}
	case map[string]interface{}:
		body["response_format"] = map[string]interface{}{
			"type":        "json_schema",
			"json_schema": map[string]interface{}{"name": "output", "schema": f, "strict": true},
		}
	}
	base := strings.TrimRight(e.BaseURL, "/")
	url := base + "/v1/chat/completions"
	if strings.HasSuffix(base, "/v1") {
		url = base + "/chat/completions"
	}
	var resp struct {
		Model   string `json:"model"`
		Choices []struct {
			Message      chatMessage `json:"message"`
			FinishReason string      `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := postJSON(ctx, client, url, e.APIKey, body, &resp); err != nil {
		return nil, err
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("LLM returned no choices")
	}
	c := resp.Choices[0]
	return chatResult("openai", resp.Model, c.Message.Content, c.FinishReason, resp.Usage.PromptTokens, resp.Usage.CompletionTokens), nil
}

func chatResult(backend, model, content, done string, in, out int) map[string]interface{} {
	return map[string]interface{}{
		"backend":     backend,
		"model":       model,
		"message":     map[string]interface{}{"role": "assistant", "content": content},
		"content":     content,
		"done_reason": done,
		"usage":       map[string]interface{}{"prompt_tokens": in, "completion_tokens": out},
	}
}

// postJSON posts body and decodes the answer. Error messages carry the status and at
// most 300 bytes of the answer (never the request).
func postJSON(ctx context.Context, client *http.Client, url, bearer string, body, out interface{}) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("LLM call failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("LLM returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(out); err != nil {
		return fmt.Errorf("LLM answer is not JSON: %w", err)
	}
	return nil
}
