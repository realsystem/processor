package executor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fakeLLM(t *testing.T, status int) (*httptest.Server, *[]map[string]interface{}, *[]*http.Request) {
	t.Helper()
	var bodies []map[string]interface{}
	var reqs []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&b)
		bodies = append(bodies, b)
		reqs = append(reqs, r)
		if status != 200 {
			w.WriteHeader(status)
			w.Write([]byte(`{"error":"model not found"}`))
			return
		}
		switch r.URL.Path {
		case "/api/chat":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"model": b["model"], "message": map[string]string{"role": "assistant", "content": `{"verdict":"ok"}`},
				"done_reason": "stop", "prompt_eval_count": 21, "eval_count": 7,
			})
		case "/v1/chat/completions":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"model":   b["model"],
				"choices": []map[string]interface{}{{"message": map[string]string{"role": "assistant", "content": "hi"}, "finish_reason": "stop"}},
				"usage":   map[string]int{"prompt_tokens": 5, "completion_tokens": 1},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &bodies, &reqs
}

func TestLLMChatOllama(t *testing.T) {
	srv, bodies, _ := fakeLLM(t, 200)
	e := &LLMChatExecutor{Backend: "ollama", BaseURL: srv.URL, DefaultModel: "llama3.2"}
	schema := map[string]interface{}{"type": "object", "properties": map[string]interface{}{"verdict": map[string]interface{}{"type": "string"}}}
	out, err := e.Execute(context.Background(), "llm.chat", map[string]interface{}{
		"system":     "You review proposals.",
		"messages":   []interface{}{map[string]interface{}{"role": "user", "content": "Is this ok?"}},
		"format":     schema,
		"options":    map[string]interface{}{"temperature": 0.1, "max_tokens": 64.0},
		"keep_alive": "10m",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out["content"] != `{"verdict":"ok"}` || out["model"] != "llama3.2" || out["backend"] != "ollama" || out["status"] != "completed" {
		t.Fatalf("result %v", out)
	}
	if u := out["usage"].(map[string]interface{}); u["prompt_tokens"] != 21 || u["completion_tokens"] != 7 {
		t.Fatalf("usage %v", u)
	}
	sent := (*bodies)[0]
	msgs := sent["messages"].([]interface{})
	if len(msgs) != 2 || msgs[0].(map[string]interface{})["role"] != "system" || sent["stream"] != false {
		t.Fatalf("messages %v", sent)
	}
	if sent["options"].(map[string]interface{})["num_predict"] != 64.0 || sent["format"] == nil || sent["keep_alive"] != "10m" {
		t.Fatalf("options/format %v", sent)
	}
}

func TestLLMChatOpenAICompatible(t *testing.T) {
	srv, bodies, reqs := fakeLLM(t, 200)
	for _, base := range []string{srv.URL, srv.URL + "/v1"} {
		e := &LLMChatExecutor{Backend: "openai", BaseURL: base, APIKey: "sk-test", DefaultModel: "m"}
		out, err := e.Execute(context.Background(), "llm.chat", map[string]interface{}{"prompt": "hello", "format": "json"})
		if err != nil {
			t.Fatal(err)
		}
		if out["content"] != "hi" || out["backend"] != "openai" {
			t.Fatalf("result %v", out)
		}
	}
	last := (*reqs)[len(*reqs)-1]
	if last.Header.Get("Authorization") != "Bearer sk-test" {
		t.Fatal("API key is sent as a bearer token")
	}
	if (*bodies)[0]["response_format"].(map[string]interface{})["type"] != "json_object" {
		t.Fatalf("json format %v", (*bodies)[0])
	}
}

func TestLLMChatErrorsDoNotEchoThePrompt(t *testing.T) {
	srv, _, _ := fakeLLM(t, 404)
	e := &LLMChatExecutor{BaseURL: srv.URL, DefaultModel: "m"}
	_, err := e.Execute(context.Background(), "llm.chat", map[string]interface{}{"prompt": "my secret farm data"})
	if err == nil || !strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("error %v", err)
	}
}

func TestLLMChatInputValidation(t *testing.T) {
	e := &LLMChatExecutor{BaseURL: "http://unused", DefaultModel: "m"}
	for name, in := range map[string]map[string]interface{}{
		"no messages":     {},
		"only system":     {"system": "x"},
		"bad role":        {"messages": []interface{}{map[string]interface{}{"role": "tool", "content": "x"}}},
		"messages object": {"messages": map[string]interface{}{}},
		"bad format":      {"prompt": "x", "format": "yaml"},
	} {
		if _, err := e.Execute(context.Background(), "llm.chat", in); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if _, err := (&LLMChatExecutor{}).Execute(context.Background(), "llm.chat", map[string]interface{}{"prompt": "x"}); err == nil {
		t.Error("no model anywhere must fail")
	}
}

func TestLLMChatRespectsContext(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-block }))
	defer srv.Close()
	defer close(block)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (&LLMChatExecutor{BaseURL: srv.URL, DefaultModel: "m"}).Execute(ctx, "llm.chat", map[string]interface{}{"prompt": "x"}); err == nil {
		t.Fatal("a canceled job must stop the call")
	}
}
