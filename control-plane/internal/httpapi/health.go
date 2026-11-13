package httpapi

import (
	"encoding/json"
	"net/http"
	"time"
)

// Autonomy health contract (spec health.schema.json): GET /v1/health says the process is up,
// GET /v1/ready says whether it can do its job (the database answers). Both are unauthenticated
// and carry no job data. The older /api/v1/health is unchanged.

// ComponentID and ComponentVersion are what autonomy.manifest.json declares; a test keeps them equal.
const (
	ComponentID      = "svc:job-service"
	ComponentVersion = "0.2.0"
)

var started = time.Now()

// SetCell names the cell this instance runs on (the @cell part of its component reference).
func (api *API) SetCell(cell string) { api.cell = cell }

func (api *API) componentRef() string {
	if api.cell == "" {
		return ComponentID
	}
	return ComponentID + "@" + api.cell
}

func (api *API) healthDoc(status string, checks []map[string]interface{}) map[string]interface{} {
	doc := map[string]interface{}{
		"status":       status,
		"component_id": api.componentRef(),
		"version":      ComponentVersion,
		"uptime_s":     int64(time.Since(started).Seconds()),
	}
	if len(checks) > 0 {
		doc["checks"] = checks
	}
	return doc
}

func writeHealth(w http.ResponseWriter, code int, doc map[string]interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(doc)
}

// HealthV1 is liveness: the process answers.
func (api *API) HealthV1(w http.ResponseWriter, r *http.Request) {
	writeHealth(w, http.StatusOK, api.healthDoc("ok", nil))
}

// ReadyV1 is readiness: 200 when the queue's database answers, 503 (status "down") when it does not.
func (api *API) ReadyV1(w http.ResponseWriter, r *http.Request) {
	check := map[string]interface{}{"name": "database", "ok": true}
	status, code := "ok", http.StatusOK
	if _, err := api.jobsService.Stats(time.Now()); err != nil {
		check["ok"] = false
		check["impact"] = "jobs cannot be submitted, leased or completed"
		check["safety"] = false
		status, code = "down", http.StatusServiceUnavailable
	}
	writeHealth(w, code, api.healthDoc(status, []map[string]interface{}{check}))
}
