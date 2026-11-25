package web

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewJobFormListsConfiguredModels(t *testing.T) {
	r, _ := NewTemplateRenderer("../../templates")
	h := &WebHandlers{renderer: r}
	h.SetModels([]string{"llama3.2", "gemma2:2b", "qwen2.5:7b"}, "gemma2:2b")

	rec := httptest.NewRecorder()
	r.Render(rec, httptest.NewRequest("GET", "/jobs/new", nil), "jobs/new.html", h.newJobForm(nil))
	body := rec.Body.String()

	for _, m := range []string{"llama3.2", "gemma2:2b", "qwen2.5:7b"} {
		if !strings.Contains(body, `<option value="`+m+`"`) {
			t.Errorf("model %q is not in the dropdown", m)
		}
	}
	if !strings.Contains(body, `<option value="gemma2:2b" selected>`) {
		t.Error("the default model is not preselected")
	}
	if !strings.Contains(body, `<option value="llm.chat"`) {
		t.Error("llm.chat is not offered as a job type")
	}
}

func TestBuildLLMInput(t *testing.T) {
	models := []string{"llama3.2", "qwen2.5:7b"}

	in, err := buildLLMInput(models, "  hi  ", "qwen2.5:7b", " be brief ")
	if err != nil || in["prompt"] != "hi" || in["model"] != "qwen2.5:7b" || in["system"] != "be brief" {
		t.Fatalf("got %v, %v", in, err)
	}
	if in, _ := buildLLMInput(models, "hi", "", ""); in["model"] != nil || in["system"] != nil {
		t.Errorf("empty model/system must be left out so the worker default applies: %v", in)
	}
	if _, err := buildLLMInput(models, "hi", "not-offered:70b", ""); err == nil {
		t.Error("a model that is not in the list must be refused")
	}
	if _, err := buildLLMInput(models, "   ", "llama3.2", ""); err == nil {
		t.Error("an empty prompt must be refused")
	}
}
