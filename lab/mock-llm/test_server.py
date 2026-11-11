import json
import os
import sys
import tempfile
import threading
import unittest
import urllib.error
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
os.environ["MOCK_LLM_LOG"] = os.path.join(tempfile.mkdtemp(), "log.jsonl")
os.environ["MOCK_LLM_RULES"] = os.path.join(HERE, "rules.json")
import server  # noqa: E402

SCHEMA = {"type": "object", "required": ["answer", "restart"],
          "properties": {"answer": {"type": "string"}, "restart": {"type": "string"}}}
REVIEW = {"type": "object", "properties": {"verdict": {"enum": ["ok", "escalate"]}, "note": {"type": "string"}}}


class MockLLM(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.httpd = server.ThreadingHTTPServer(("127.0.0.1", 0), server.Handler)
        cls.base = f"http://127.0.0.1:{cls.httpd.server_address[1]}"
        threading.Thread(target=cls.httpd.serve_forever, daemon=True).start()

    @classmethod
    def tearDownClass(cls):
        cls.httpd.shutdown()

    def setUp(self):
        self.post("/_lab/mode", {"mode": "normal"})

    def post(self, path, body):
        r = urllib.request.Request(self.base + path, json.dumps(body).encode(), {"Content-Type": "application/json"})
        return json.load(urllib.request.urlopen(r, timeout=10))

    def chat(self, text, fmt=None):
        body = {"messages": [{"role": "user", "content": text}]}
        if fmt:
            body["format"] = fmt
        return self.post("/api/chat", body)["message"]["content"]

    def test_structured_answer_matches_schema(self):
        data = json.loads(self.chat("the camera is stuck", SCHEMA))
        self.assertEqual(data["restart"], "frigate")
        self.assertEqual(set(data), {"answer", "restart"})

    def test_default_structured_answer_is_valid(self):
        data = json.loads(self.chat("something else entirely", SCHEMA))
        self.assertIsInstance(data["answer"], str)

    def test_reviewer_escalates_reboot_only(self):
        self.assertEqual(json.loads(self.chat("action host.reboot", REVIEW))["verdict"], "escalate")
        self.assertEqual(json.loads(self.chat("restart frigate", REVIEW))["verdict"], "ok")

    def test_words_not_substrings(self):
        self.assertNotIn("Hello from", self.chat("which one"))

    def test_openai_shape(self):
        out = self.post("/v1/chat/completions", {"messages": [{"role": "user", "content": "hello"}]})
        self.assertEqual(out["choices"][0]["message"]["role"], "assistant")
        self.assertIn("total_tokens", out["usage"])

    def test_modes(self):
        self.post("/_lab/mode", {"mode": "garbage"})
        with self.assertRaises(ValueError):
            json.loads(self.chat("x", SCHEMA))
        self.post("/_lab/mode", {"mode": "echo"})
        self.assertIn("[user] ping", self.chat("ping"))
        self.post("/_lab/mode", {"mode": "error"})
        with self.assertRaises(urllib.error.HTTPError) as e:
            self.chat("x")
        self.assertEqual(e.exception.code, 503)

    def test_bad_mode_rejected(self):
        with self.assertRaises(urllib.error.HTTPError):
            self.post("/_lab/mode", {"mode": "nope"})

    def test_requests_are_logged(self):
        self.chat("log me please")
        with urllib.request.urlopen(self.base + "/_lab/requests") as r:
            lines = json.load(r)["requests"]
        self.assertTrue(any("log me please" in json.dumps(x) for x in lines))


if __name__ == "__main__":
    unittest.main()
