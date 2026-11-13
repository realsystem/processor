package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"testing"
)

var semver = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

func TestHealthContract(t *testing.T) {
	e := newEnv(t, "tok", "token")
	e.api.SetCell("home-hub")
	for path, handler := range map[string]http.HandlerFunc{"/v1/health": e.api.HealthV1, "/v1/ready": e.api.ReadyV1} {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest(http.MethodGet, path, nil))
		var doc map[string]interface{}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &doc) != nil {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
		if doc["status"] != "ok" || doc["component_id"] != "svc:job-service@home-hub" || !semver.MatchString(doc["version"].(string)) {
			t.Fatalf("%s: %v", path, doc)
		}
		if _, isNumber := doc["uptime_s"].(float64); !isNumber {
			t.Fatalf("%s: uptime_s: %v", path, doc)
		}
	}
}

func TestReadinessSaysDownWhenTheDatabaseIsGone(t *testing.T) {
	e := newEnv(t, "tok", "token")
	// close the database underneath the service
	e.closeDB()
	w := httptest.NewRecorder()
	e.api.ReadyV1(w, httptest.NewRequest(http.MethodGet, "/v1/ready", nil))
	var doc struct {
		Status string                   `json:"status"`
		Checks []map[string]interface{} `json:"checks"`
	}
	json.Unmarshal(w.Body.Bytes(), &doc)
	if w.Code != http.StatusServiceUnavailable || doc.Status != "down" || len(doc.Checks) != 1 ||
		doc.Checks[0]["ok"] != false || doc.Checks[0]["impact"] == "" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

// The manifest the repository publishes must describe this code.
func TestManifestMatchesTheCode(t *testing.T) {
	raw, err := os.ReadFile("../../../autonomy.manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		ComponentID string `json:"component_id"`
		Version     string `json:"version"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.ComponentID != ComponentID || m.Version != ComponentVersion {
		t.Fatalf("manifest %s %s, code %s %s", m.ComponentID, m.Version, ComponentID, ComponentVersion)
	}
}

func (e *env) closeDB() { e.db.Close() }
