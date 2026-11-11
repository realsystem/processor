#!/usr/bin/env python3
"""mock-llm: a stand-in for a language model, for tests and labs. No model, no downloads.

Speaks Ollama  POST /api/chat  and OpenAI  POST /v1/chat/completions  (plus /api/tags,
/v1/models, /health) and answers from rules.json. Standard library only.

Admin (not part of any model API):
  GET  /_lab/mode            -> {"mode": ..., "delay": ...}
  POST /_lab/mode            {"mode": "normal|slow|garbage|echo|error", "delay": 5}
  GET  /_lab/requests        -> the request log (the same lines as the log file)

Modes: normal (rules), slow (answer after `delay` seconds), garbage (not JSON, to test retry
and escalation), echo (repeat the prompt exactly), error (HTTP 503). "down" is done by
stopping the container, not here.

Every request is logged as one JSON line to $MOCK_LLM_LOG (default /data/mock-llm.jsonl):
that is what a real model would have been sent.
"""
import json
import os
import re
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

RULES_PATH = os.environ.get("MOCK_LLM_RULES", "/app/rules.json")
LOG_PATH = os.environ.get("MOCK_LLM_LOG", "/data/mock-llm.jsonl")
PORT = int(os.environ.get("MOCK_LLM_PORT", "11434"))
MODEL = "mock-llm"

_lock = threading.Lock()
_state = {"mode": "normal", "delay": 5.0}


def load_rules(path=RULES_PATH):
    try:
        with open(path, encoding="utf-8") as f:
            return json.load(f)
    except (OSError, ValueError):
        return {"rules": [], "text": "This is the mock model."}


def from_schema(schema):
    """The simplest value that is valid for a JSON schema."""
    if not isinstance(schema, dict):
        return "mock answer"
    if "enum" in schema and schema["enum"]:
        return schema["enum"][0]
    t = schema.get("type")
    if t == "object" or "properties" in schema:
        props = schema.get("properties", {})
        return {k: from_schema(v) for k, v in props.items()}
    if t == "array":
        return []
    if t == "integer":
        return 0
    if t == "number":
        return 0.0
    if t == "boolean":
        return False
    return "mock answer"


def pick(messages, rules):
    """The first rule whose words appear in the last user message (or the system prompt)."""
    user = next((m["content"] for m in reversed(messages) if m["role"] == "user"), "").lower()
    system = " ".join(m["content"] for m in messages if m["role"] == "system").lower()
    for rule in rules.get("rules", []):
        words = [w.lower() for w in rule.get("match", [])]
        hay = system if rule.get("where") == "system" else user
        if words and any(re.search(r"(?<!\w)" + re.escape(w) + r"(?!\w)", hay) for w in words):
            return rule
    return {}


def answer(messages, fmt, rules):
    """(content, structured) for the request, by mode and rules."""
    with _lock:
        mode = _state["mode"]
    if mode == "echo":
        return "\n".join(f"[{m['role']}] {m['content']}" for m in messages), None
    if mode == "garbage":
        return "<<not json>> mock-llm is in garbage mode", None
    rule = pick(messages, rules)
    if fmt not in (None, "", "json") or fmt == "json":
        data = from_schema(fmt) if isinstance(fmt, dict) else {}
        data = dict(data) if isinstance(data, dict) else {}
        data.update(rule.get("json", {}))
        return json.dumps(data), data
    return rule.get("text", rules.get("text", "This is the mock model.")), None


def log(entry):
    entry["at"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
    try:
        os.makedirs(os.path.dirname(LOG_PATH), exist_ok=True)
        with _lock, open(LOG_PATH, "a", encoding="utf-8") as f:
            f.write(json.dumps(entry) + "\n")
    except OSError:
        pass


class Handler(BaseHTTPRequestHandler):
    server_version = "mock-llm"

    def log_message(self, *a):  # quiet; the JSON-lines log is the record
        pass

    def _send(self, code, body):
        raw = json.dumps(body).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def _body(self):
        n = int(self.headers.get("Content-Length") or 0)
        try:
            return json.loads(self.rfile.read(n) or b"{}")
        except ValueError:
            return None

    def do_GET(self):
        if self.path == "/health":
            return self._send(200, {"status": "ok", "model": MODEL})
        if self.path == "/api/tags":
            return self._send(200, {"models": [{"name": MODEL, "model": MODEL}]})
        if self.path == "/v1/models":
            return self._send(200, {"object": "list", "data": [{"id": MODEL, "object": "model"}]})
        if self.path == "/_lab/mode":
            with _lock:
                return self._send(200, dict(_state))
        if self.path == "/_lab/requests":
            try:
                with open(LOG_PATH, encoding="utf-8") as f:
                    lines = [json.loads(x) for x in f if x.strip()]
            except OSError:
                lines = []
            return self._send(200, {"requests": lines})
        self._send(404, {"error": "not found"})

    def do_POST(self):
        body = self._body()
        if body is None:
            return self._send(400, {"error": "bad json"})
        if self.path == "/_lab/mode":
            mode = body.get("mode", "normal")
            if mode not in ("normal", "slow", "garbage", "echo", "error"):
                return self._send(400, {"error": f"unknown mode {mode}"})
            with _lock:
                _state["mode"] = mode
                if "delay" in body:
                    _state["delay"] = float(body["delay"])
                return self._send(200, dict(_state))
        if self.path not in ("/api/chat", "/v1/chat/completions"):
            return self._send(404, {"error": "not found"})

        openai = self.path.startswith("/v1/")
        messages = [
            {"role": m.get("role", "user"), "content": str(m.get("content", ""))}
            for m in body.get("messages", [])
        ]
        if openai:
            rf = body.get("response_format") or {}
            fmt = (rf.get("json_schema") or {}).get("schema") or (
                "json" if rf.get("type") == "json_object" else None
            )
        else:
            fmt = body.get("format")
        log({"api": "openai" if openai else "ollama", "model": body.get("model"),
             "format": fmt, "messages": messages})

        with _lock:
            mode, delay = _state["mode"], _state["delay"]
        if mode == "error":
            return self._send(503, {"error": "mock-llm is in error mode"})
        if mode == "slow":
            time.sleep(delay)

        content, _ = answer(messages, fmt, load_rules())
        tokens_in = sum(len(m["content"].split()) for m in messages)
        tokens_out = len(content.split())
        if openai:
            return self._send(200, {
                "id": "mock", "object": "chat.completion", "model": MODEL,
                "choices": [{"index": 0, "finish_reason": "stop",
                             "message": {"role": "assistant", "content": content}}],
                "usage": {"prompt_tokens": tokens_in, "completion_tokens": tokens_out,
                          "total_tokens": tokens_in + tokens_out},
            })
        return self._send(200, {
            "model": MODEL, "created_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
            "message": {"role": "assistant", "content": content},
            "done": True, "done_reason": "stop",
            "prompt_eval_count": tokens_in, "eval_count": tokens_out,
        })


def main():
    ThreadingHTTPServer(("0.0.0.0", PORT), Handler).serve_forever()


if __name__ == "__main__":
    main()
