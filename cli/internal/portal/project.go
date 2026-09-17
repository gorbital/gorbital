package portal

import (
	"context"
	"net/http"
	"time"
)

// ProjectSettings is the Project Settings screen's view of the app
// (ADR-0077): what the manifest, go.mod and .env say, with the
// environment key behind each value so the screen edits it through the
// env editor. Secrets are never included.
type ProjectSettings struct {
	Project
	// Git reports the app directory is a git repository.
	Git bool `json:"git"`
	// Ports and addresses.
	App struct {
		Addr string `json:"addr"`
		URL  string `json:"url"`
		Key  string `json:"key"` // APP_ADDR
	} `json:"app"`
	Portal struct {
		Port string `json:"port"`
		Key  string `json:"key"` // DEV_PORTAL_PORT
	} `json:"portal"`
	DatabaseSettings struct {
		Configured bool   `json:"configured"`
		Host       string `json:"host,omitempty"` // host:port/name, never the password
		Key        string `json:"key"`            // DATABASE_URL
		PortKey    string `json:"port_key"`       // POSTGRES_PORT
	} `json:"database_settings"`
	Mail struct {
		Delivery    string `json:"delivery"`
		CatcherAddr string `json:"catcher_addr,omitempty"`
		Key         string `json:"key"` // MAIL_DELIVERY
	} `json:"mail"`
	Storage struct {
		Driver   string `json:"driver"`
		Bucket   string `json:"bucket,omitempty"`
		LocalDir string `json:"local_dir,omitempty"`
		Key      string `json:"key"` // STORAGE_DRIVER
	} `json:"storage"`
	CORS struct {
		Origins []string `json:"origins"`
		Key     string   `json:"key"` // APP_CORS_ORIGINS
	} `json:"cors"`
	Logging struct {
		Level  string   `json:"level"`
		Format string   `json:"format"`
		Keys   []string `json:"keys"` // APP_LOG_LEVEL, APP_LOG_FORMAT
	} `json:"logging"`
	Docs struct {
		Enabled bool   `json:"enabled"`
		Key     string `json:"key"` // APP_DOCS_ENABLED
	} `json:"docs"`
	// Danger zone: what the portal can clear, with the endpoints.
	Danger []DangerAction `json:"danger"`
}

// DangerAction is a destructive action the Project Settings screen
// offers, confirmed with Loses.
type DangerAction struct {
	Name      string `json:"name"`
	Method    string `json:"method"`
	Path      string `json:"path"`
	Loses     string `json:"loses"`
	Available bool   `json:"available"`
}

// ProjectConfig connects the Project Settings screen.
type ProjectConfig struct {
	// Settings describes the app; nil answers 404.
	Settings func(ctx context.Context) (ProjectSettings, error)
	// ResetDatabase drops the app's schema and applies migrations and seed
	// data again; nil means the action isn't available.
	ResetDatabase func(ctx context.Context) error
}

func (s *Server) projectRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET "+APIPrefix+"project", func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.ProjectSettings.Settings == nil {
			writeProblem(w, http.StatusNotFound, "no_project_settings", "this orb dev describes no project")
			return
		}
		settings, err := s.cfg.ProjectSettings.Settings(r.Context())
		if err != nil {
			writeProblem(w, http.StatusInternalServerError, "project_error", err.Error())
			return
		}
		_ = writeJSON(w, http.StatusOK, settings)
	})
	mux.HandleFunc("POST "+APIPrefix+"project/reset-database", func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.ProjectSettings.ResetDatabase == nil {
			writeProblem(w, http.StatusNotFound, "no_database", "this app has no database to reset")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()
		if err := s.cfg.ProjectSettings.ResetDatabase(ctx); err != nil {
			writeProblem(w, http.StatusConflict, "reset_failed", err.Error())
			return
		}
		_ = writeJSON(w, http.StatusAccepted, map[string]any{"status": "accepted", "detail": "the schema was dropped; migrations and seed data are being applied"})
	})
}
