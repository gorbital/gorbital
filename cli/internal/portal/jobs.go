package portal

import (
	"encoding/json"
	"net/http"
)

// JobSource is a job's code in the app (ADR-0071): where its definition
// and worker live and, for a job orb gen job wrote, the form that produced
// it. Once the worker is edited by hand the job is ejected: the portal
// shows it as a custom job edited in code.
type JobSource struct {
	Name       string `json:"name"`
	Ident      string `json:"ident"`
	Package    string `json:"package"`
	Definition string `json:"definition"`
	Worker     string `json:"worker"`
	// Generated reports an //orb:job marker in the definition file.
	Generated bool `json:"generated"`
	// Ejected reports a generated job whose worker no longer matches the
	// marker's hash.
	Ejected bool `json:"ejected"`
	// Kind is the marker's kind for a generated job that isn't ejected,
	// custom otherwise.
	Kind string `json:"kind"`
	// Form is the marker's fields, for the portal to fill the job form.
	Form json.RawMessage `json:"form,omitempty"`
}

// serveJobs lists the app's jobs as they are in code.
func (s *Server) serveJobs(w http.ResponseWriter, r *http.Request) {
	jobs := []JobSource{}
	if s.cfg.Jobs != nil {
		found, err := s.cfg.Jobs()
		if err != nil {
			writeProblem(w, http.StatusInternalServerError, "jobs_unavailable", err.Error())
			return
		}
		if found != nil {
			jobs = found
		}
	}
	_ = writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}
