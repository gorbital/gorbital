package portal

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"runtime"
	"strings"

	"gorbital.dev/cli/internal/tunnel"
)

// Tunnel (ADR-0086), under /_portal/api/tunnel:
//
//	GET  tunnel           cloudflared, the configuration (never the token) and the status
//	POST tunnel/start     {"mode": "quick"|"named", "hostname": "..."}
//	POST tunnel/stop
//	POST tunnel/restart
//	POST tunnel/check     request the public URL's /livez now
//	GET  tunnel/setup     the .env changes and provider addresses for the running tunnel
//	PUT  tunnel/settings  {"hostname": "..."}: a named tunnel's hostname for later starts
//
// Status changes also arrive on the event stream as "tunnel" events. The
// .env changes are applied with PUT env, like every other .env edit.

// TunnelConfig connects the Tunnel screen.
type TunnelConfig struct {
	// Manager runs cloudflared; nil answers 404 no_tunnel.
	Manager *tunnel.Manager
	// Setup builds the configuration help for a connected tunnel; nil
	// answers 404.
	Setup func(ctx context.Context, status tunnel.Status) (tunnel.Setup, error)
}

// TunnelSettings is PUT tunnel/settings.
type TunnelSettings struct {
	Hostname string `json:"hostname"`
}

func (s *Server) tunnelRoutes(mux *http.ServeMux) {
	with := func(fn func(m *tunnel.Manager, w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			m := s.cfg.Tunnel.Manager
			if m == nil {
				writeProblem(w, http.StatusNotFound, "no_tunnel", "this orb dev runs no tunnels")
				return
			}
			fn(m, w, r)
		}
	}
	mux.HandleFunc("GET "+APIPrefix+"tunnel", with(func(m *tunnel.Manager, w http.ResponseWriter, r *http.Request) {
		_ = writeJSON(w, http.StatusOK, m.Info(r.Context(), runtime.GOOS))
	}))
	mux.HandleFunc("POST "+APIPrefix+"tunnel/start", with(func(m *tunnel.Manager, w http.ResponseWriter, r *http.Request) {
		var opts tunnel.StartOptions
		if !decodeBody(w, r, &opts) {
			return
		}
		// The process outlives the request.
		if err := m.Start(context.WithoutCancel(r.Context()), opts); err != nil {
			writeTunnelError(w, err)
			return
		}
		_ = writeJSON(w, http.StatusAccepted, m.Info(r.Context(), runtime.GOOS))
	}))
	mux.HandleFunc("POST "+APIPrefix+"tunnel/stop", with(func(m *tunnel.Manager, w http.ResponseWriter, r *http.Request) {
		if err := m.Stop(); err != nil {
			writeTunnelError(w, err)
			return
		}
		_ = writeJSON(w, http.StatusAccepted, m.Info(r.Context(), runtime.GOOS))
	}))
	mux.HandleFunc("POST "+APIPrefix+"tunnel/restart", with(func(m *tunnel.Manager, w http.ResponseWriter, r *http.Request) {
		if err := m.Restart(context.WithoutCancel(r.Context())); err != nil {
			writeTunnelError(w, err)
			return
		}
		_ = writeJSON(w, http.StatusAccepted, m.Info(r.Context(), runtime.GOOS))
	}))
	mux.HandleFunc("POST "+APIPrefix+"tunnel/check", with(func(m *tunnel.Manager, w http.ResponseWriter, r *http.Request) {
		result, err := m.Check(r.Context())
		if err != nil {
			writeTunnelError(w, err)
			return
		}
		_ = writeJSON(w, http.StatusOK, result)
	}))
	mux.HandleFunc("GET "+APIPrefix+"tunnel/setup", with(func(m *tunnel.Manager, w http.ResponseWriter, r *http.Request) {
		if s.cfg.Tunnel.Setup == nil {
			writeProblem(w, http.StatusNotFound, "no_tunnel_setup", "this orb dev proposes no configuration for tunnels")
			return
		}
		status := m.Status()
		if status.PublicURL == "" || (status.State != tunnel.StateConnected && status.State != tunnel.StateStarting) {
			writeProblem(w, http.StatusConflict, tunnel.CodeNotConnected, "start a tunnel first: the configuration depends on its public URL")
			return
		}
		setup, err := s.cfg.Tunnel.Setup(r.Context(), status)
		if err != nil {
			writeTunnelError(w, err)
			return
		}
		_ = writeJSON(w, http.StatusOK, setup)
	}))
	mux.HandleFunc("PUT "+APIPrefix+"tunnel/settings", with(func(m *tunnel.Manager, w http.ResponseWriter, r *http.Request) {
		var in TunnelSettings
		if !decodeBody(w, r, &in) {
			return
		}
		hostname, err := m.SaveHostname(in.Hostname)
		if err != nil {
			writeTunnelError(w, err)
			return
		}
		_ = writeJSON(w, http.StatusOK, TunnelSettings{Hostname: hostname})
	}))
}

// decodeBody reads a small JSON body into v, refusing unknown fields. An
// empty body leaves v as it is.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil {
		writeProblem(w, http.StatusRequestEntityTooLarge, "body_too_large", "the request body is too large")
		return false
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return true
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_json", "the body must be JSON: "+err.Error())
		return false
	}
	return true
}

// writeTunnelError answers a tunnel refusal with its code.
func writeTunnelError(w http.ResponseWriter, err error) {
	var e *tunnel.Error
	if !errors.As(err, &e) {
		writeProblem(w, http.StatusInternalServerError, "tunnel_failed", err.Error())
		return
	}
	status := http.StatusUnprocessableEntity
	switch e.Code {
	case tunnel.CodeInvalidMode:
		status = http.StatusBadRequest
	case tunnel.CodeNotConnected:
		status = http.StatusConflict
	}
	writeProblem(w, status, e.Code, e.Detail)
}
