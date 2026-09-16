# Idempotency keys

Retrying a request that creates something is only safe when the server can tell a retry from a new request. Full apps accept an `Idempotency-Key` header on POST and PATCH requests: the first request runs, its response is kept, and a retry with the same key gets the same response instead of creating a second project, invitation or job run. Decision: [ADR-0060](../adr/0060-idempotency-keys.md). Library: `gorbital.dev/modules/idempotency`.

## Sending a key

Generate a new random key (a UUID is fine) for each operation the user starts, and send the same key with every retry of it:

```bash
KEY=$(uuidgen)
curl -X POST http://127.0.0.1:8080/v1/projects \
  -H "Authorization: Bearer $TOKEN" \
  -H "Idempotency-Key: $KEY" \
  -H "Content-Type: application/json" \
  -d '{"name":"Website"}'
```

A retry with the same key and body answers with the first response, marked `Idempotent-Replayed: true`, and nothing is created again.

| Rule | |
|---|---|
| Methods | POST and PATCH. GET, PUT and DELETE are idempotent already and ignore the header |
| Key | 1 to 255 visible ASCII characters, one header. Otherwise 400 `invalid_idempotency_key` |
| Who | Signed-in users (and service accounts). Keys belong to the caller: two users sending the same key never see each other's responses. Requests without a session ignore the header |
| Where | Every endpoint except `/v1/auth/*`, whose responses carry session tokens and cookies |
| Same request | The same method, path, query and body. Change any of them and the key is refused: 422 `idempotency_key_reused`. Use a new key for a new request |
| How long | `idempotency.retention`, 24 hours by default. After that, the key can be used again |

## Responses

| Situation | Response |
|---|---|
| First request with the key | The request runs normally |
| Retry after it finished | The stored status, `Content-Type`, `Location`, `ETag` and body, plus `Idempotent-Replayed: true` |
| Retry while the first request is still running | 409 `idempotency_in_progress` with `Retry-After: 1`. Retry after a moment |
| Same key, different request | 422 `idempotency_key_reused` |
| The database can't be reached | 503 `unavailable`; the request doesn't run |

Client errors are final outcomes and are replayed too: a retry of a request that failed with 409 `project_name_taken` gets the same 409. Some responses are **not** kept, so a retry runs the request again:

- server errors (5xx) and crashes (panics);
- 401, 403, 408 and 429, which the caller can resolve without changing the request (sign in, get a permission or a fresh second factor, wait);
- responses that set a cookie;
- responses marked `Cache-Control: no-store`, such as the ones that show a new API key once ([API keys](api-keys.md));
- response bodies larger than 1 MiB;
- responses whose handler calls `idempotency.DontStore(ctx)`.

If an instance dies while a request holds a key, the key answers 409 for up to 5 minutes, then the same request can run again.

In multi-tenant apps the path includes the organisation, so a key used in one organisation and sent again to another is refused with 422 rather than replayed.

## Operations

| | |
|---|---|
| Setting | `idempotency.retention`: 24 hours, between 1 hour and 7 days, reason required. Shortening it applies to stored responses at once |
| Job | `idempotency_cleanup`, hourly: deletes expired keys and their responses in batches of 1 000 |
| Table | `idempotency_keys`. Keys and caller IDs are stored as SHA-256 hashes; responses as sent |
| Retention report | `GET /ops/retention` lists `idempotency_keys` with its oldest key |
| Metric | `idempotency.requests`, by `outcome`: `stored`, `released`, `replayed`, `in_progress`, `key_reused`, `unavailable` |

Stored responses can contain personal data (whatever the endpoint returned), so keep the retention short. The table isn't readable through `/ops`.

## In your own endpoints

Every POST and PATCH operation gets the header automatically and documents it in `api/openapi.json`. Nothing to add for resources generated with `orb gen resource`.

If a response shows a secret only once, such as a new API key, don't let it be stored:

```go
func (h *handler) createKey(ctx context.Context, in *createKeyInput) (*keyOutput, error) {
	idempotency.DontStore(ctx) // a retry creates another key instead of replaying this one
	...
}
```

Browsers calling the API from `APP_CORS_ORIGINS` may send `Idempotency-Key` and read `Idempotent-Replayed`.

The wiring is in `internal/app/idempotency.go`: the store, the middleware (last in the chain, after authentication) and the OpenAPI header. To exclude more paths, extend its skip function and `idempotencySkipped`.

## Existing apps

`orb upgrade` adds the module, the migration `20260918000040_idempotency_keys.sql`, `internal/app/idempotency.go`, the setting, the job and the middleware. Run `go run ./cmd/migrate` before deploying. Clients that don't send the header see no change.

## Testing

Tests in `internal/app/idempotency_test.go` retry a project creation, reuse a key with another body, race ten retries and check that another user's key is independent. The library's tests in `modules/idempotency` cover concurrency, replay, released responses, stale locks, retention and the body cap against Docker PostgreSQL.
