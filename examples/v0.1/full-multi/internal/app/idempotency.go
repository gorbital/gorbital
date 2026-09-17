package app

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/httpx"
	"gorbital.dev/modules/idempotency"
)

// idempotencySkipped is the path prefix whose requests ignore the
// Idempotency-Key header: sign-in and account endpoints answer with session
// tokens and cookies, which are never stored (ADR-0060).
const idempotencySkipped = "/v1/auth/"

// newIdempotency builds the store of idempotency keys on pool. Responses are
// kept for idempotency.retention, read on every request.
func newIdempotency(pool *pgxpool.Pool, s appSettings, logger *slog.Logger) (*idempotency.Store, error) {
	return idempotency.NewStore(pool,
		idempotency.WithRetention(s.idempotencyRetention.Get),
		idempotency.WithLogger(logger),
	)
}

// idempotencyMiddleware replays the stored response of a POST or PATCH
// request retried with the same Idempotency-Key by the same signed-in caller.
// It runs after authentication, which identifies the caller.
func idempotencyMiddleware(store *idempotency.Store) httpx.Middleware {
	return idempotency.Middleware(store, idempotency.WithSkip(func(r *http.Request) bool {
		return strings.HasPrefix(r.URL.Path, idempotencySkipped)
	}))
}

// documentIdempotencyKey adds the optional Idempotency-Key header to every
// POST and PATCH operation the middleware applies to. Call it after
// registering the operations.
func documentIdempotencyKey(api huma.API) {
	minLength, maxLength := 1, idempotency.MaxKeyLength
	for path, item := range api.OpenAPI().Paths {
		if strings.HasPrefix(path, idempotencySkipped) {
			continue
		}
		for _, op := range []*huma.Operation{item.Post, item.Patch} {
			if op == nil {
				continue
			}
			op.Parameters = append(op.Parameters, &huma.Param{
				Name: idempotency.Header,
				In:   "header",
				Description: "A unique value, such as a UUID, that makes retrying this request safe: a retry with the same key and " +
					"request gets the first response again, with `Idempotent-Replayed: true`. The same key with another request " +
					"fails with 422 `idempotency_key_reused`, and while the first request runs with 409 `idempotency_in_progress`. " +
					"Only for signed-in callers; keys and responses are kept for 24 hours by default.",
				Example: "7f1e9c4a-4f5b-4c1e-9a55-2d1d7c2b8e61",
				Schema:  &huma.Schema{Type: "string", MinLength: &minLength, MaxLength: &maxLength, Pattern: "^[!-~]+$"},
			})
		}
	}
}
