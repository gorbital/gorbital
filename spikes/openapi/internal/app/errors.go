package app

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	projectdomain "gorbital.dev/spikes/openapi/internal/modules/projects/domain"
)

// Problem is the RFC 9457 problem+json body with a stable machine code.
type Problem struct {
	Type      string              `json:"type,omitempty" doc:"URI identifying the problem type"`
	Title     string              `json:"title" doc:"Short summary" example:"Conflict"`
	Status    int                 `json:"status" doc:"HTTP status code" example:"409"`
	Code      string              `json:"code" doc:"Stable machine-readable code" example:"project_name_taken"`
	Detail    string              `json:"detail,omitempty" doc:"Human-readable explanation"`
	RequestID string              `json:"request_id,omitempty" doc:"Correlates with logs and traces"`
	Errors    []*huma.ErrorDetail `json:"errors,omitempty" doc:"Field-level validation errors"`
}

func (p *Problem) Error() string  { return p.Detail }
func (p *Problem) GetStatus() int { return p.Status }

// ContentType makes JSON problems use application/problem+json.
func (p *Problem) ContentType(ct string) string {
	if ct == "application/json" {
		return "application/problem+json"
	}
	return ct
}

type errorMapping struct {
	target error
	status int
	code   string
}

// errorTable maps domain errors to HTTP. In a generated app each module's
// wiring file contributes its rows.
var errorTable = []errorMapping{
	{projectdomain.ErrNotFound, http.StatusNotFound, "project_not_found"},
	{projectdomain.ErrNameTaken, http.StatusConflict, "project_name_taken"},
	{projectdomain.ErrAlreadyArchived, http.StatusConflict, "project_already_archived"},
	{projectdomain.ErrNameRequired, http.StatusUnprocessableEntity, "project_name_required"},
	{projectdomain.ErrNameTooLong, http.StatusUnprocessableEntity, "project_name_too_long"},
}

var defaultCodes = map[int]string{
	http.StatusBadRequest:          "bad_request",
	http.StatusUnauthorized:        "unauthorized",
	http.StatusForbidden:           "forbidden",
	http.StatusNotFound:            "not_found",
	http.StatusUnprocessableEntity: "validation_failed",
	http.StatusInternalServerError: "internal_error",
}

// installErrorHandling replaces Huma's package-level error constructors. It is
// called only from the composition root, before any operation is registered.
func installErrorHandling(logger *slog.Logger) {
	huma.NewError = func(status int, msg string, errs ...error) huma.StatusError {
		return newProblem(status, msg, errs...)
	}
	huma.NewErrorWithContext = func(hctx huma.Context, status int, msg string, errs ...error) huma.StatusError {
		p := newProblem(status, msg, errs...)
		if hctx != nil {
			ctx := hctx.Context()
			p.RequestID = RequestIDFrom(ctx)
			if p.Status >= http.StatusInternalServerError {
				logger.ErrorContext(ctx, "request failed", "err", errors.Join(errs...), "request_id", p.RequestID)
			}
		}
		return p
	}
}

func newProblem(status int, msg string, errs ...error) *Problem {
	for _, err := range errs {
		for _, m := range errorTable {
			if errors.Is(err, m.target) {
				return &Problem{Title: http.StatusText(m.status), Status: m.status, Code: m.code, Detail: m.target.Error()}
			}
		}
	}

	p := &Problem{Title: http.StatusText(status), Status: status, Code: defaultCodes[status], Detail: msg}
	if p.Code == "" {
		p.Code = "error"
	}
	if status >= http.StatusInternalServerError {
		p.Detail = "an internal error occurred"
		return p
	}
	for _, err := range errs {
		var d huma.ErrorDetailer
		if errors.As(err, &d) {
			p.Errors = append(p.Errors, d.ErrorDetail())
		}
	}
	return p
}
