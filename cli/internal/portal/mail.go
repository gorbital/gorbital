package portal

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"gorbital.dev/cli/internal/devmail"
)

// MailConfig connects the portal to the mail catcher (ADR-0074).
type MailConfig struct {
	// Store is the inbox; nil when orb dev runs no catcher (the app sends
	// to Mailpit or a provider), which answers 404 no_mail_catcher.
	Store *devmail.Store
	// SMTPAddr is where the catcher listens, for the screen to show.
	SMTPAddr string
}

// Mail endpoints, under /_portal/api/mail:
//
//	GET    mail?q=&limit=      the inbox, newest first
//	GET    mail/stream         new messages as Server-Sent Events
//	GET    mail/{id}           one message: bodies, links, codes, headers
//	GET    mail/{id}/html      the HTML body, sandboxed, for an iframe
//	GET    mail/{id}/source    the message as received
//	DELETE mail/{id}, DELETE mail
func (s *Server) mailRoutes(mux *http.ServeMux) {
	guard := func(fn func(w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if s.cfg.Mail.Store == nil {
				writeProblem(w, http.StatusNotFound, "no_mail_catcher", "this orb dev runs no mail catcher: the app sends email to Mailpit or a provider (MAIL_DELIVERY)")
				return
			}
			fn(w, r)
		}
	}
	mux.HandleFunc("GET "+APIPrefix+"mail", guard(s.serveMailList))
	mux.HandleFunc("GET "+APIPrefix+"mail/stream", guard(s.serveMailStream))
	mux.HandleFunc("GET "+APIPrefix+"mail/{id}", guard(s.serveMailGet))
	mux.HandleFunc("GET "+APIPrefix+"mail/{id}/html", guard(s.serveMailHTML))
	mux.HandleFunc("GET "+APIPrefix+"mail/{id}/source", guard(s.serveMailSource))
	mux.HandleFunc("DELETE "+APIPrefix+"mail/{id}", guard(s.serveMailDelete))
	mux.HandleFunc("DELETE "+APIPrefix+"mail", guard(s.serveMailClear))
}

func (s *Server) serveMailList(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, total := s.cfg.Mail.Store.List(r.URL.Query().Get("q"), limit)
	_ = writeJSON(w, http.StatusOK, map[string]any{"messages": list, "total": total, "count": s.cfg.Mail.Store.Count(), "smtp_addr": s.cfg.Mail.SMTPAddr, "max": devmail.MaxMessages})
}

func (s *Server) serveMailGet(w http.ResponseWriter, r *http.Request) {
	d, err := s.cfg.Mail.Store.Get(r.PathValue("id"))
	if err != nil {
		writeMailError(w, err)
		return
	}
	_ = writeJSON(w, http.StatusOK, d)
}

// serveMailHTML serves the HTML body for an iframe: sandboxed, no
// scripts, no requests except images, so a message can't reach the
// portal's API.
func (s *Server) serveMailHTML(w http.ResponseWriter, r *http.Request) {
	d, err := s.cfg.Mail.Store.Get(r.PathValue("id"))
	if err != nil {
		writeMailError(w, err)
		return
	}
	body := d.HTML
	if body == "" {
		body = "<pre style=\"font-family: ui-monospace, monospace; white-space: pre-wrap\">" + htmlEscape(d.Text) + "</pre>"
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src data: http: https:; style-src 'unsafe-inline'; font-src data: https:")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, body)
}

func (s *Server) serveMailSource(w http.ResponseWriter, r *http.Request) {
	raw, err := s.cfg.Mail.Store.Source(r.PathValue("id"))
	if err != nil {
		writeMailError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(raw)
}

func (s *Server) serveMailDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.cfg.Mail.Store.Delete(r.PathValue("id")); err != nil {
		writeMailError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) serveMailClear(w http.ResponseWriter, _ *http.Request) {
	if err := s.cfg.Mail.Store.Clear(); err != nil {
		writeProblem(w, http.StatusInternalServerError, "mail_store_error", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeMailError(w http.ResponseWriter, err error) {
	if errors.Is(err, devmail.ErrNotFound) {
		writeProblem(w, http.StatusNotFound, "message_not_found", "no message has this ID")
		return
	}
	writeProblem(w, http.StatusInternalServerError, "mail_store_error", err.Error())
}

// serveMailStream sends each new message's summary as a "message" event.
func (s *Server) serveMailStream(w http.ResponseWriter, r *http.Request) {
	if int(s.streams.Add(1)) > s.cfg.MaxStreams {
		s.streams.Add(-1)
		writeProblem(w, http.StatusTooManyRequests, "rate_limited", fmt.Sprintf("at most %d event streams at once; close one", s.cfg.MaxStreams))
		return
	}
	defer s.streams.Add(-1)
	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	write := func(event string, data any) error {
		payload, err := json.Marshal(data)
		if err != nil {
			return err
		}
		_ = rc.SetWriteDeadline(time.Now().Add(writeTimeout))
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload); err != nil {
			return err
		}
		return rc.Flush()
	}
	_ = rc.SetWriteDeadline(time.Now().Add(writeTimeout))
	if _, err := io.WriteString(w, "retry: 3000\n\n"); err != nil {
		return
	}
	messages, stop := s.cfg.Mail.Store.Subscribe()
	defer stop()
	keepAlive := time.NewTicker(keepAliveInterval)
	defer keepAlive.Stop()
	deadline := time.NewTimer(s.cfg.StreamDuration)
	defer deadline.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.closed:
			_ = write("end", StreamEnd{Reason: "shutdown"})
			return
		case <-deadline.C:
			_ = write("end", StreamEnd{Reason: "duration"})
			return
		case <-keepAlive.C:
			_ = rc.SetWriteDeadline(time.Now().Add(writeTimeout))
			if _, err := io.WriteString(w, ": keep-alive\n\n"); err != nil {
				return
			}
			_ = rc.Flush()
		case m := <-messages:
			if err := write("message", m); err != nil {
				return
			}
		}
	}
}

func htmlEscape(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<':
			b = append(b, "&lt;"...)
		case '>':
			b = append(b, "&gt;"...)
		case '&':
			b = append(b, "&amp;"...)
		default:
			b = append(b, s[i])
		}
	}
	return string(b)
}
