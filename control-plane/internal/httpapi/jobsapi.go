package httpapi

import (
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/processor/control-plane/internal/jobs"
	"github.com/processor/control-plane/internal/middleware"
)

// MaxWait caps ?wait= on GET /api/v1/jobs/{id}; it stays below the server's write timeout.
const MaxWait = 10 * time.Second

// OnlineWindow is how recently a worker must have polled to count as online.
const OnlineWindow = 2 * time.Minute

var traceParentRE = regexp.MustCompile(`^[0-9a-f]{2}-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}$`)

func validTraceParent(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	if traceParentRE.MatchString(v) {
		return v
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// waitForJob returns the job; with ?wait=<seconds> it first waits (up to MaxWait) for
// the job to reach a final status, so callers need not poll in a tight loop.
func (api *API) waitForJob(r *http.Request, jobID string) (*jobs.Job, error) {
	job, err := api.jobsService.Get(jobID)
	if err != nil || r.URL.Query().Get("wait") == "" {
		return job, err
	}
	secs, perr := strconv.ParseFloat(r.URL.Query().Get("wait"), 64)
	if perr != nil || secs <= 0 {
		return job, nil
	}
	wait := time.Duration(secs * float64(time.Second))
	if wait > MaxWait {
		wait = MaxWait
	}
	deadline := time.Now().Add(wait)
	for !jobs.IsTerminal(job.Status) && time.Now().Before(deadline) {
		select {
		case <-r.Context().Done():
			return job, nil
		case <-time.After(250 * time.Millisecond):
		}
		if job, err = api.jobsService.Get(jobID); err != nil {
			return nil, err
		}
	}
	return job, nil
}

// JobAction routes POST /api/v1/jobs/{id}/cancel; everything else under /api/v1/jobs/ is
// GetJob.
func (api *API) JobAction(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/cancel") {
		api.CancelJob(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	api.GetJob(w, r)
}

// CancelJob cancels a job (queued: at once; running: at the worker's next heartbeat).
func (api *API) CancelJob(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
		return
	}
	jobID := strings.TrimSuffix(extractID(r.URL.Path, "/api/v1/jobs/"), "/cancel")
	job, err := api.jobsService.Get(jobID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Job not found")
		return
	}
	if job.UserID != user.ID && !user.IsAdmin {
		writeError(w, http.StatusForbidden, "forbidden", "Access denied")
		return
	}
	job, err = api.jobsService.Cancel(jobID)
	if errors.Is(err, jobs.ErrNotCancelable) {
		writeError(w, http.StatusConflict, "not_cancelable", "Job already finished as "+job.Status)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to cancel job")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "data": formatJob(job)})
}

// Stats reports queue depth and online workers (any authenticated user).
func (api *API) Stats(w http.ResponseWriter, r *http.Request) {
	if middleware.GetUserFromContext(r.Context()) == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
		return
	}
	st, err := api.jobsService.Stats(time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to read stats")
		return
	}
	online, err := api.workerService.Online(OnlineWindow)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to read workers")
		return
	}
	workers := make([]map[string]interface{}, 0, len(online))
	for id, caps := range online {
		workers = append(workers, map[string]interface{}{"id": id, "capabilities": caps})
	}
	sort.Slice(workers, func(i, j int) bool { return workers[i]["id"].(string) < workers[j]["id"].(string) })
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "data": map[string]interface{}{
		"jobs":            st.ByStatus,
		"oldest_queued_s": st.OldestQueuedAgeSecond,
		"workers_online":  workers,
	}})
}

// JobTypes lists the job types some online worker can run.
func (api *API) JobTypes(w http.ResponseWriter, r *http.Request) {
	if middleware.GetUserFromContext(r.Context()) == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
		return
	}
	online, err := api.workerService.Online(OnlineWindow)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to read workers")
		return
	}
	seen := map[string]bool{}
	for _, caps := range online {
		for _, c := range caps {
			seen[c] = true
		}
	}
	types := make([]string, 0, len(seen))
	for t := range seen {
		types = append(types, t)
	}
	sort.Strings(types)
	// J3: the registered contracts; "online" says whether a worker can run the type now
	registry := []map[string]interface{}{}
	for _, t := range api.jobTypes.Types() {
		registry = append(registry, map[string]interface{}{
			"name":          t.Name,
			"version":       t.Version,
			"description":   t.Description,
			"input_schema":  t.InputSchema,
			"result_schema": t.ResultSchema,
			"online":        seen[t.Name],
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "data": map[string]interface{}{
		"job_types": types,
		"registry":  registry,
		"strict":    api.jobTypes.Strict(),
	}})
}
