package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sync"

	"apistock.dev/requestid"
)

// ProblemContentType is the media type of problem responses.
const ProblemContentType = "application/problem+json"

// Problem is an RFC 9457 problem details response extended with a stable
// machine-readable code and the request ID. Codes are public API; titles and
// details are not (ADR-0015).
type Problem struct {
	Type      string       `json:"type,omitempty" doc:"URI identifying the problem type"`
	Title     string       `json:"title" doc:"Short summary of the problem type" example:"Conflict"`
	Status    int          `json:"status" doc:"HTTP status code" example:"409"`
	Code      string       `json:"code" doc:"Stable machine-readable error code" example:"project_name_taken"`
	Detail    string       `json:"detail,omitempty" doc:"Human-readable explanation" example:"project name is already taken"`
	RequestID string       `json:"request_id,omitempty" doc:"Correlates with server logs and traces" example:"req_9f86d081884c7d65"`
	Errors    []FieldError `json:"errors,omitempty" doc:"Field-level validation errors"`
}

// FieldError describes one invalid input field. It never echoes the
// submitted value, which may be a password or personal data.
type FieldError struct {
	Location string `json:"location,omitempty" doc:"Where the error occurred" example:"body.name"`
	Message  string `json:"message" doc:"What is wrong" example:"expected length >= 1"`
}

// NewProblem returns a problem with the standard title for status.
func NewProblem(status int, code, detail string) *Problem {
	return &Problem{Title: http.StatusText(status), Status: status, Code: code, Detail: detail}
}

// Error returns the detail, or the title when detail is empty.
func (p *Problem) Error() string {
	if p.Detail != "" {
		return p.Detail
	}
	return p.Title
}

// GetStatus returns the HTTP status. It lets frameworks such as Huma use a
// Problem as a status error.
func (p *Problem) GetStatus() int { return p.Status }

// ContentType returns application/problem+json for JSON responses.
func (p *Problem) ContentType(ct string) string {
	if ct == "application/json" {
		return ProblemContentType
	}
	return ct
}

// WriteProblem writes p as application/problem+json, filling the request ID
// from the request context when empty.
func WriteProblem(w http.ResponseWriter, r *http.Request, p *Problem) {
	if p.RequestID == "" {
		p.RequestID = requestid.From(r.Context())
	}
	w.Header().Set("Content-Type", ProblemContentType)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

var codePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

// A Mapping maps a sentinel error (matched with [errors.Is]) to an HTTP
// status and stable code. Detail defaults to the error's message.
type Mapping struct {
	Err    error
	Status int
	Code   string
	Detail string
}

// Mapper turns errors into problems using application-owned mappings. Errors
// without a mapping become a generic 500 and are logged once with the
// request ID. It is safe for concurrent use.
type Mapper struct {
	logger *slog.Logger

	mu       sync.RWMutex
	mappings []Mapping
}

// NewMapper returns a mapper with the given mappings.
func NewMapper(logger *slog.Logger, mappings ...Mapping) (*Mapper, error) {
	if logger == nil {
		return nil, errors.New("httpx: mapper logger must not be nil")
	}
	m := &Mapper{logger: logger}
	if err := m.Add(mappings...); err != nil {
		return nil, err
	}
	return m, nil
}

// Add registers mappings. Each needs a non-nil error, a 4xx or 5xx status and
// a snake_case code; errors and codes must be unique.
func (m *Mapper) Add(mappings ...Mapping) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, mp := range mappings {
		switch {
		case mp.Err == nil:
			return errors.New("httpx: mapping error must not be nil")
		case mp.Status < 400 || mp.Status > 599:
			return fmt.Errorf("httpx: mapping %q: status %d is not an error status", mp.Code, mp.Status)
		case !codePattern.MatchString(mp.Code):
			return fmt.Errorf("httpx: mapping code %q must be snake_case", mp.Code)
		}
		for _, existing := range m.mappings {
			if errors.Is(existing.Err, mp.Err) || existing.Code == mp.Code {
				return fmt.Errorf("httpx: duplicate mapping for %q: %w", mp.Code, mp.Err)
			}
		}
		m.mappings = append(m.mappings, mp)
	}
	return nil
}

// Match returns the problem for err if err is (or wraps) a *[Problem] or a
// mapped error. It never logs.
func (m *Mapper) Match(err error) (*Problem, bool) {
	var p *Problem
	if errors.As(err, &p) {
		cp := *p
		return &cp, true
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, mp := range m.mappings {
		if errors.Is(err, mp.Err) {
			detail := mp.Detail
			if detail == "" {
				detail = mp.Err.Error()
			}
			return NewProblem(mp.Status, mp.Code, detail), true
		}
	}
	return nil, false
}

// Problem returns the problem for err. Unmapped errors become 500
// "internal_error" with a generic detail and are logged.
func (m *Mapper) Problem(ctx context.Context, err error) *Problem {
	if p, ok := m.Match(err); ok {
		return p
	}
	m.logger.ErrorContext(ctx, "unhandled error", "err", err, "request_id", requestid.From(ctx))
	return NewProblem(http.StatusInternalServerError, "internal_error", "an internal error occurred")
}

// Write maps err and writes the problem response.
func (m *Mapper) Write(w http.ResponseWriter, r *http.Request, err error) {
	WriteProblem(w, r, m.Problem(r.Context(), err))
}

// DefaultCode returns the generic code for a status without a mapping, such
// as "not_found" for 404.
func DefaultCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "bad_request"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusMethodNotAllowed:
		return "method_not_allowed"
	case http.StatusConflict:
		return "conflict"
	case http.StatusRequestEntityTooLarge:
		return "request_too_large"
	case http.StatusUnprocessableEntity:
		return "validation_failed"
	case http.StatusTooManyRequests:
		return "rate_limited"
	case http.StatusServiceUnavailable:
		return "unavailable"
	}
	if status >= 500 {
		return "internal_error"
	}
	return "error"
}
