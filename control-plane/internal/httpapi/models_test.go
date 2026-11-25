package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestModelsEndpoint(t *testing.T) {
	api := &API{}
	api.SetModels([]string{"llama3.2", "qwen2.5:7b"}, "llama3.2")

	rec := httptest.NewRecorder()
	api.Models(rec, httptest.NewRequest(http.MethodGet, "/api/v1/models", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var resp struct {
		Data struct {
			Models  []string `json:"models"`
			Default string   `json:"default"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data.Models) != 2 || resp.Data.Default != "llama3.2" {
		t.Fatalf("got %+v", resp.Data)
	}

	rec = httptest.NewRecorder()
	api.Models(rec, httptest.NewRequest(http.MethodPost, "/api/v1/models", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: status %d", rec.Code)
	}
}

// With no models configured the endpoint returns an empty list, not null.
func TestModelsEndpointEmpty(t *testing.T) {
	rec := httptest.NewRecorder()
	(&API{}).Models(rec, httptest.NewRequest(http.MethodGet, "/api/v1/models", nil))
	var resp struct {
		Data struct {
			Models []string `json:"models"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Data.Models == nil {
		t.Fatal("models must be [], not null")
	}
}
