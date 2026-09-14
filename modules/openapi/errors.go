package openapi

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"

	"apistock.dev/httpx"
	"apistock.dev/requestid"
)

// InstallErrors makes every Huma error an [httpx.Problem]:
//
//   - errors matched by mapper (domain sentinels, *httpx.Problem) use their mapping;
//   - validation and other client errors keep their status, get the default
//     code (for example "validation_failed") and list field errors without
//     echoing submitted values;
//   - server errors become a generic 500 and are logged once by mapper.
//
// Huma stores these hooks in package-level variables, so call InstallErrors
// once, from the composition root, before registering operations. It is the
// only package-level state an apistock app changes (ADR-0027).
func InstallErrors(mapper *httpx.Mapper) {
	huma.NewErrorWithContext = func(hctx huma.Context, status int, msg string, errs ...error) huma.StatusError {
		ctx := context.Background()
		if hctx != nil {
			ctx = hctx.Context()
		}
		p := problemFor(ctx, mapper, status, msg, errs)
		p.RequestID = requestid.From(ctx)
		return p
	}
	huma.NewError = func(status int, msg string, errs ...error) huma.StatusError {
		return problemFor(context.Background(), mapper, status, msg, errs)
	}
}

func problemFor(ctx context.Context, mapper *httpx.Mapper, status int, msg string, errs []error) *httpx.Problem {
	if status == 0 {
		// Huma calls NewError(0, "") at registration to discover the error schema.
		return &httpx.Problem{}
	}
	for _, err := range errs {
		if err == nil {
			continue
		}
		if p, ok := mapper.Match(err); ok {
			return p
		}
	}
	if status >= 500 {
		return mapper.Problem(ctx, errors.Join(errs...))
	}

	p := httpx.NewProblem(status, httpx.DefaultCode(status), msg)
	for _, err := range errs {
		var d huma.ErrorDetailer
		if errors.As(err, &d) {
			if detail := d.ErrorDetail(); detail != nil {
				p.Errors = append(p.Errors, httpx.FieldError{Location: detail.Location, Message: detail.Message})
			}
		}
	}
	return p
}
