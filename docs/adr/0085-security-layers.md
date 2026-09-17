# ADR-0085: Security layers: timeouts, IP filters, signed webhooks and external identity providers

**Status:** Accepted (2026-09-17) · **Amends:** ADR-0029 (rows 50 to 53), ADR-0062 (the Resend verifier delegates to `webhook`) · **Builds on:** ADR-0052, ADR-0082

## Context

Phase 10 of the [v0.2 roadmap](../v0.2-roadmap.md#phase-10-security-layers) adds only layers the examples and the threat model ([ADR-0029](0029-threat-model.md)) need. Today:

| Need | Today | Evidence |
|---|---|---|
| A slow dependency ties up requests | No request deadline: `httpx.Server` sets read, write and idle timeouts on connections, not a context deadline for handlers; a handler waiting on a stuck query holds its connection and pool slot until the write timeout, then the client gets a reset instead of an error | `httpx/server.go`; roadmap item 89 |
| `/ops` reachable from the internet | Protected by sessions, 2FA-required roles and rate limits; nothing lets an operator also restrict it to an office or VPN range | Roadmap item 90; ADR-0029 row 18 |
| Receiving payment and other provider webhooks | Only Resend's verifier exists, inside `modules/mail/resend`; an app receiving Stripe-style, GitHub or Standard Webhooks events writes its own HMAC check, and the body is usually already parsed by the time it can check | `modules/mail/resend/webhook.go`; recipe *Receiving payment webhooks* |
| Mobile apps and SPAs signed in with Auth0, Clerk, Supabase, Firebase or Cognito | `modules/auth` authenticates only its own sessions and API keys; verifying a provider's JWT means choosing a library, caching JWKS and getting `alg`, audience and clock handling right | Recipe *Mobile backend with an external identity provider*; ADR-0083's `Authenticator` |

Constraints: core packages depend on the standard library, the OpenTelemetry API and `golang.org/x` only (ADR-0019, `internal/archtest`); modules don't import `gorbital.dev/gorbital`; guards run before input parsing (ADR-0082); error codes are public API (ADR-0015); the default stack and `gorbital.Timeout` belong to Phase 3, which is built in parallel, so this decision covers the library pieces and states how they are wired later.

## Options

### Request timeouts

| Option | Verdict |
|---|---|
| `http.TimeoutHandler` | Rejected: it buffers the whole response in memory, so streaming and `Flush` don't work, and it loses `http.ResponseController` |
| Deadline on the context only | Rejected: a handler that ignores its context still holds the request, and a handler that honours it answers whatever error its code maps a cancelled query to (often a 500) |
| Run the handler in a goroutine and answer from the request goroutine (what `TimeoutHandler` does, without the buffer) | Built first and measured: about 4 µs per request on an idle machine, from waking a thread for every request (`pthread_cond_signal` dominated the profile); a panic after the answer has no `Recover` left to reach; the handler keeps running after `Server.Shutdown` believes the request is over |
| **Handler on the request goroutine; a timer takes a lock shared with the handler's writer and writes the 503 if the response hasn't started** | **Chosen**: about 1 µs and 12 allocations per request; panics reach `Recover` with their stack; shutdown waits for handlers. The 503 is flushed with a `Content-Length` at the deadline, so HTTP/1.1 clients have the whole answer even if the handler ignores its context |

### IP filtering

| Option | Verdict |
|---|---|
| Leave it to the proxy or firewall | Kept as the recommendation for whole-app rules; not enough for "only `/ops`" when the app terminates HTTP behind a generic load balancer |
| Filter on `X-Forwarded-For` directly | Rejected: clients choose that header (ADR-0052) |
| **`httpx.IPFilter(allow, deny)` on `RemoteAddr` after `TrustedProxies`** | **Chosen**; moved to `ipfilter.New` in `gorbital.dev/httpx/ipfilter` (see [Package moves](#package-moves-2026-09-17)) |

### Signed webhooks

| Option | Verdict |
|---|---|
| A library such as `svix-webhooks` or `standard-webhooks` | Rejected: a dependency per scheme for one HMAC; core can't take it |
| Verify in the handler | Rejected: Huma has parsed and validated the body by then, so an unsigned request costs a JSON decode and learns validation errors; each handler repeats body limits and constant-time comparison |
| **A stdlib-only package `gorbital.dev/webhook` (a `Verifier` interface, an HMAC-SHA256 verifier, a Standard Webhooks constructor) and `guard.Webhook(v)` running it before parsing** | **Chosen**. A package of its own rather than `httpx`: it has no HTTP middleware, and non-gorbital code (a job re-verifying a stored delivery, `modules/mail/resend`) uses it without the HTTP helpers |

### External identity providers

| Option | Verdict |
|---|---|
| `github.com/golang-jwt/jwt` with `MicahParks/keyfunc` for JWKS | Rejected: two new dependencies; `keyfunc` refreshes in a background goroutine |
| `github.com/coreos/go-oidc` | Rejected: built for OpenID Connect ID tokens (discovery, nonce) rather than access tokens; Clerk, Supabase and Cognito access tokens don't all fit its checks |
| `github.com/lestrrat-go/jwx` | Rejected: a large new dependency tree |
| **`github.com/go-jose/go-jose/v4`, already in the dependency graph through `modules/auth`, with key caching and claim checks written here** | **Chosen**: no new module in `go.sum` of apps that use `modules/auth`; it refuses `alg` values not in the list given to `ParseSigned` and checks that the key type fits the algorithm |

## Decision

### `httpx.Timeout(d)`, now `timeout.New(d)` in `gorbital.dev/httpx/timeout`

- Each request gets `context.WithTimeout(r.Context(), d)`; handlers see `Deadline()` and `context.DeadlineExceeded`. `d <= 0` returns the handler unchanged.
- The response writer keeps the handler's headers in a copy until the response starts. At the deadline a timer takes the writer's lock: if the handler hasn't written a status, body, flush or hijack, it writes 503 **`request_timeout`** (a new code: `unavailable` doesn't tell a client the request itself was too slow) with the request ID, `Cache-Control: no-store` and `Content-Length`, and flushes. Afterwards the handler's writes return `http.ErrHandlerTimeout`, `WriteHeader` does nothing and `Unwrap` returns a writer that discards.
- A handler whose first write comes after the deadline is refused even if the timer hasn't run yet (the writer checks the context), so a handler reacting to its cancelled context never races the 503.
- Once the response has started, the deadline only cancels the context: no buffering, so streaming, `Flush`, trailers, 103 Early Hints and `http.ResponseController` (`Flush`, `Hijack`, `SetReadDeadline`, `SetWriteDeadline`, `EnableFullDuplex`, forwarded under the lock) behave as without the middleware.
- Streams meant to outlive `d` (server-sent events) go on routes without the timeout. **Integration (done with Phase 3, 2026-09-17):** the default stack's `Timeout` step after `AccessLog`, configured by `APP_REQUEST_TIMEOUT` (default 30s, `0` off, shorter than the server's 60s write timeout), and `gorbital.Timeout(d)`, a route option that **shortens** a route's deadline. Lengthening per route was considered (the stack applying the timeout per operation instead of around the router) and rejected for now: it moves a stack step into the router and routes that need longer are rare; they raise `APP_REQUEST_TIMEOUT`, or drop `Timeout` with `WithStack` and put `gorbital.Timeout` on the groups that need one. Found during integration: Huma panics when a write fails, so late writes after a timeout now report success and are discarded instead of returning `http.ErrHandlerTimeout` (flush, hijack and deadline calls still return it).

### `httpx.IPFilter(allow, deny)` and `httpx.ParsePrefixes`, now `ipfilter.New` and `ipfilter.ParsePrefixes` in `gorbital.dev/httpx/ipfilter`

- The address is `RemoteAddr` as `TrustedProxies` left it; IPv4-mapped IPv6 addresses compare as IPv4 and IPv6 zones are ignored. A `RemoteAddr` that isn't an address is refused.
- Deny wins; an empty allow list allows every address not denied; both empty is a no-op. Refusal: 403 **`ip_not_allowed`** (new), without echoing the address.
- Refused at construction: invalid ranges; a deny range covering a whole family (`0.0.0.0/0`, `::/0`, `ErrDenyAll`): it would refuse every request, and the intent is almost always "only these ranges", which is what `allow` says; an allow range inside a deny range, which could never match; IPv4-mapped ranges shorter than /96, which mix families. Allow `0.0.0.0/0` is accepted (IPv4 only).
- `ParsePrefixes` reads `OPS_ALLOWED_IPS`-style lists: CIDR or single addresses, masked, mapped addresses turned into IPv4. **Integration (done with Phase 4, 2026-09-17):** `LoadConfig` reads `OPS_ALLOWED_IPS` into `Config.OpsAllowedIPs`, reporting every problem with the others, and `opshttp` puts each `/ops/` route behind `IPFilter(allowed, nil)` as its first middleware, before the sign-in check (ADR-0083, Phase 4 notes).

### `gorbital.dev/webhook` and `guard.Webhook`

- `Verifier` is `Verify(ctx, header http.Header, body []byte) error`, returning an error wrapping `ErrInvalidSignature` for an inauthentic request.
- `NewHMAC(HMACConfig)`: signature header; entry prefix (`v1,`, `sha256=`); base64 or hex; optional signed ID header (non-empty, at most 255 bytes, no dots or whitespace) and timestamp header (Unix seconds, positive); tolerance (default 5 minutes, either way; `ErrTimestamp` wraps `ErrInvalidSignature`); several secrets for rotation, each at least 16 bytes and copied; the signed content (default `id.timestamp.body`, customisable for schemes such as Slack's); an injectable clock. Every entry is compared with every secret with `hmac.Equal`, without stopping at the first match.
- `NewStandard(StandardConfig)`: Standard Webhooks (`webhook-id`, `webhook-timestamp`, `webhook-signature`, `whsec_` base64 secrets), with `HeaderPrefix: "svix-"` for Svix senders (Resend, Clerk). `DecodeStandardSecret` lets apps refuse a mistyped secret at start without quoting it.
- `resend.VerifyWebhook` now builds an `HMAC` with Svix's headers and maps its errors to `ErrInvalidWebhook` and `ErrWebhookTimestamp`: exported API, errors, secret handling and behaviour unchanged; its existing tests (Svix's published vector, a Resend vector) and fuzz targets pass unmodified.
- `guard.Webhook(v, guard.WebhookBodyLimit(n))` reads the raw body once, up to `n` (default 1 MiB, Huma's default): a declared or actual larger body is 413 `request_too_large`, an unreadable body 400 `bad_request`; then `v.Verify`, refusing with 401 `invalid_webhook_signature` (the code the v0.1 Resend endpoint returns); then puts the same bytes back as the body. A verifier error that doesn't wrap `ErrInvalidSignature` (a key server down) is a 500 through the usual mapping. The guard documents 400, 401 and 413 and names itself `webhook` in `x-gorbital-guards`. Senders have no session: routes combine it with `guard.Public()`, explicitly, so public routes stay greppable.
- **Replay-ID storage is not built.** Within the tolerance a captured delivery can be replayed, and senders retry legitimately with the same ID. Handlers must be idempotent: store the delivery ID with the change it makes (`INSERT … ON CONFLICT DO NOTHING` in the same transaction, as `suppressionpg.AddOnce` does), or rely on the idempotency of the change itself. A generic store would need a table and retention for every app, for what one conflict clause does in the app's own transaction.

### `gorbital.dev/modules/jwt`

- `jwt.New(ctx, jwt.Config{Issuer, Audiences, AudienceClaim, JWKSURL, Algorithms, ClockSkew, PermissionsClaim, ActorFrom}, opts...)` with `WithHTTPClient`, `WithClock`, `WithHMACSecret`; `(*Authenticator).Verify(ctx, token) (Claims, error)`; `(*Authenticator).Middleware(logger *slog.Logger) func(http.Handler) http.Handler`, the signature of Phase 3's `gorbital.Authenticator`, so apps write `gorbital.WithAuth(auth)`. The module imports only core packages and go-jose.
- **Configuration refused at start:** no issuer; no or empty audience; an algorithm outside RS/PS/ES 256–512 and EdDSA, or HS256–512 without a 32-byte `WithHMACSecret` (`none` is never supported); a secret without an HS algorithm; asymmetric algorithms without a JWKS URL, or a URL with only HS; a URL that isn't https (http only on loopback hosts, for providers running locally), or has user information; clock skew outside 0–5 minutes; keys that can't be fetched or contain no usable key. Defaults: `RS256`, `ES256`, `EdDSA`; skew 30 s; permissions from `permissions`; audience from `aud` (`client_id` for Cognito access tokens).
- **Keys:** fetched in `New` within its context; `GET` with a 10-second timeout, at most 3 redirects, each to an allowed URL; response at most 1 MiB and status 200. Each key is parsed on its own, so an unknown key type doesn't hide the others; kept only if public, `use` empty or `sig`, RSA (at least 2048 bits), ECDSA or Ed25519; at most 100. Cached for the response's `max-age` bounded to 5 minutes–24 hours (1 hour without one, 5 minutes for `no-store`/`no-cache`). Lookups read an atomic pointer.
- **Refresh:** when the cache has expired or a token names an unknown `kid`: one fetch at a time in its own goroutine, detached from the request's cancellation and shared by every request waiting for it, and at most one every 30 seconds whatever the kids requested. No background loop. While the provider is down, cached keys stay usable for 24 hours past their cache age; a key that isn't cached is `ErrKeysUnavailable`. Failed fetches are logged at warn level, so at most every 30 seconds.
- **Key choice:** by `kid`, among keys whose `alg` (if set) equals the token's and whose type and curve fit it (RS/PS: RSA; ES256/384/512: P-256/384/521; EdDSA: Ed25519). A token without `kid` needs exactly one fitting key. Embedded `jwk`, `jku` and `x5u` headers are never used.
- **Claims:** at most 16 KiB; compact JWS with one signature; `iss` exact; the audience claim contains a configured audience; `sub` non-empty; `exp` required and `now - skew` not after it; `nbf` and `iat`, when present, not after `now + skew`. `Claims` exposes the registered claims and `String`, `Strings` (list or space-separated) and `Decode` for others.
- **Middleware:** no `Authorization: Bearer` header, a token that isn't shaped like a JWT (not three dot-separated parts: API keys, opaque tokens for another authenticator), or an actor already set → next handler unchanged, anonymous callers get the route's own 401. A JWT that fails → 401 **`invalid_token`** (new) with `WWW-Authenticate: Bearer error="invalid_token"` (RFC 6750); keys unavailable → 503 `auth_unavailable` (the code `modules/auth` uses). Success → `actor.With` (user by default, `ActorFrom` for services, permission mapping or organisations; kinds other than user or service, or an empty ID, refused), `actor.WithClient` unless set, and `user_id` or `service_id` on the access log line.
- **Why refuse instead of continuing anonymously** (unlike `modules/auth`, whose unknown session tokens continue): a provider's JWT is self-describing, so "invalid" means expired or forged, not "someone else's token". Continuing would turn an expired token into silent anonymous access on public routes that personalise, and into `unauthenticated` on protected ones, which tells a mobile client to sign the person in again instead of refreshing. Refusal also keeps rate limits and audit keyed by who the client claims to be from being quietly downgraded to per-IP.

## Threat model

| Threat | Layer | Mitigation | Residual risk |
|---|---|---|---|
| Slow dependencies or slow handlers exhaust connections and pool slots (slowloris on the application side) | Timeout | Context deadline cancels queries; 503 at the deadline; shutdown still waits for handlers | A handler that ignores its context keeps its goroutine and connection until it returns; on HTTP/2 the stream isn't ended until then. Server read/write timeouts (`httpx.Server`) still bound connections |
| Timeout response racing the handler: mixed or double responses, data races, writes after the request ended | Timeout | One lock around every write, flush, hijack and deadline change; the handler's headers in a private map until the response starts; refused writes after the deadline; `go test -race` with 200 concurrent requests crossing the deadline | Code holding the writer from `Unwrap` before the deadline bypasses the lock (documented) |
| Streaming responses cut or buffered | Timeout | Nothing buffered; started responses never replaced | Streams longer than `d` are cancelled through the context: put them on routes without the timeout (Phase 3 per-route setting) |
| `/ops` reached from untrusted networks | IP filter | Allow list on the resolved client address, deny wins, fail closed on unparseable addresses | Protects only as well as `APP_TRUSTED_PROXIES` is set: behind an untrusted-but-forwarding proxy every client has the proxy's address |
| Spoofing an allowed address through `X-Forwarded-For` | IP filter | Only `TrustedProxies` rewrites `RemoteAddr`, right to left, from configured peers (ADR-0052) | — |
| IPv4-mapped and zoned addresses evading ranges | IP filter | Clients unmapped and zones dropped; mapped ranges converted; fuzzed against a direct reading of the rules | — |
| Misconfiguration that locks everyone out or never matches | IP filter | Deny-everything and dead allow ranges refused at start | — |
| Forged webhooks | Webhook | HMAC-SHA256 over ID, timestamp and raw body; constant-time comparison of every entry with every secret; secrets at least 16 bytes | A leaked secret forges deliveries until rotated; rotation supported without downtime |
| Replayed webhooks | Webhook | Signed timestamp within 5 minutes either way; signed ID bound to the signature | Replays inside the window, and retries: handlers must be idempotent (not built into the guard, above) |
| Unsigned requests costing parsing, or learning validation messages | Webhook | Guard before input parsing; the body read once with a limit | — |
| Oversized bodies (memory exhaustion) before verification | Webhook | `Content-Length` checked first, then at most limit + 1 bytes read | Limit × concurrent requests of memory; the app stack's body limit and rate limits apply |
| Verifier leaking which check failed | Webhook | One refusal for missing headers, bad signature and old timestamp | — |
| Forged JWTs: `alg: none`, HS256 signed with the public key, an algorithm the key wasn't made for | JWT | Algorithm allowlist passed to the parser; `none` unsupported; HS only with a configured secret and never with JWKS keys; key type and curve must fit `alg`; the JWK's own `alg` must match | — |
| Keys smuggled through the token (`jwk`, `jku`, `x5u`, `kid` path tricks) | JWT | Keys come only from the configured JWKS; `kid` is a map key, never a path or URL | — |
| Token substitution: a token for another app of the same provider tenant, or an ID token used as an access token | JWT | Audience required and checked; issuer exact | An ID token whose audience equals the API's audience is accepted; configure the API's own audience (Auth0 API identifier, Firebase project ID for Firebase only) |
| Expired or not-yet-valid tokens | JWT | `exp` required; `exp`, `nbf`, `iat` with at most 5 minutes of skew | Tokens aren't revocable before `exp` (provider-side revocation isn't checked); keep access tokens short-lived |
| JWKS fetch storms from random `kid` values (denial of service on the app and the provider) | JWT | One fetch at a time, at most every 30 seconds per instance; lookups of cached keys lock-free | An attacker can make unknown-kid refusals slower by at most one shared fetch per 30 s |
| JWKS fetched from an attacker (downgrade to http, redirects, SSRF) | JWT | https (loopback http only); redirects only to allowed URLs, at most 3; 1 MiB limit; 10 s timeout | The URL is configuration: whoever sets it chooses the keys |
| Provider outage becoming an app outage | JWT | Cached keys usable 24 h past their cache age; unknown keys 503, not 401, so clients retry rather than sign out | A key the provider revoked stays usable during an outage for up to 24 h after its cache age |
| Weak or unusable keys in the JWKS | JWT | RSA under 2048 bits, private, symmetric and encryption keys skipped | — |
| Malformed tokens crashing or slowing verification | JWT | 16 KiB limit; go-jose parsing; fuzzed with mutated valid tokens (only the signed claims are ever accepted) | — |
| Invalid tokens silently treated as anonymous | JWT | 401 `invalid_token` for any JWT-shaped bearer token that fails | Non-JWT bearer tokens pass through for other authenticators by design |
| Tokens or claims in logs | JWT | Refusals logged at debug with the reason only; access log gets the subject ID | Subjects can be email-like at some providers (`auth0|…` is not); use `ActorFrom` to map to internal IDs if needed |

## Why

- A deadline with a real 503 turns stuck dependencies into fast, retryable errors, and the writer design keeps streaming working, which `TimeoutHandler` can't.
- An IP filter on the resolved address reuses the trusted-proxy work instead of reading forwarding headers again.
- One verifier interface serves every HMAC scheme, keeps core free of dependencies, and runs before parsing, where ADR-0082 put every refusal.
- JWT verification is easy to get subtly wrong; doing it once, with go-jose already in the graph, fixes the dangerous choices (algorithms, audiences, key refresh) for every app.

## Trade-offs

- The timeout costs about 1 µs and 12 allocations per request (below), and one timer; it cannot stop a handler that ignores its context.
- `guard.Webhook` holds the whole body in memory (up to its limit) twice briefly: once verified, once parsed by Huma.
- No replay-ID store; idempotent handlers are the rule.
- JWT refusal differs from `modules/auth`'s continue-anonymously for unknown session tokens (reasons above).
- `jwt.New` fails when the provider is unreachable at start, so an app can't start during an identity provider outage; a running app keeps working on cached keys.
- New public codes: `request_timeout`, `ip_not_allowed`, `invalid_token`, each in a package v0.1 apps don't link. `docs/reference/error-codes.md` is generated from the golden apps, which don't link those packages; the codes are documented in the [Security layers guide](../guides/security-layers.md#error-codes) until the golden apps move to `gorbital.Main` (Phase 9).

## Consequences

- New core packages `gorbital.dev/webhook`, `gorbital.dev/httpx/timeout` and `gorbital.dev/httpx/ipfilter`; new module `gorbital.dev/modules/jwt` (CI test, lint and govulncheck lists; release finds it); additive API in `gorbital/guard`.
- Integration left to later phases: recipes *Mobile backend with an external identity provider* and *Receiving payment webhooks*, Shelfie chapter 10.
- Guide: [Security layers](../guides/security-layers.md).

## Package moves (2026-09-17)

`httpx.Timeout`, `httpx.IPFilter`, `httpx.ParsePrefixes` and `httpx.ErrDenyAll` were first added to `httpx`. Integrating Phases 4 and 5 found that this broke apps generated by `orb` v0.1.0: their `TestPublicSurface` records the problem codes of every `gorbital.dev` package the app links (`api/surface.json`), and every v0.1 app links `httpx`, so `request_timeout` and `ip_not_allowed` appeared as unrecorded names and the apps' own tests failed after `go get`. Recording them in the golden apps' surfaces hid the failure in this repository but not in apps users already have.

| Option | Verdict |
|---|---|
| Keep them in `httpx` and ask v0.1 apps to run `-update` | Rejected: breaks D2 (v0.1.0 apps build and pass their tests unchanged) |
| Build the codes with `fmt.Sprintf` or a variable so the scan misses them | Rejected: hides public names from the inventory that exists to list them |
| Change the scan in the generated `surface_test.go` | Rejected: existing apps keep the v0.1.0 test until they upgrade |
| **New core packages that v0.1 apps don't link: `gorbital.dev/httpx/timeout` (`timeout.New(d)`) and `gorbital.dev/httpx/ipfilter` (`ipfilter.New(allow, deny)`, `ipfilter.ParsePrefixes`, `ipfilter.ErrDenyAll`)** | **Chosen**: behaviour, messages (prefixed `ipfilter:` instead of `httpx:`), tests, fuzz tests (`FuzzNew`, `FuzzParsePrefixes`), benchmarks (`BenchmarkTimeout`, `BenchmarkNew`) and examples moved unchanged. A v0.1 app that adopts one links it deliberately and records its code then |

The API was unreleased, so the move needs no deprecation. `gorbital`'s `Timeout` step and route option, `LoadConfig`'s `OPS_ALLOWED_IPS` and `opshttp` use the new packages; the commit that recorded the codes in the golden apps' `api/surface.json` and `docs/reference/error-codes.md` was reverted, and the v0.1.0 scaffold compatibility check (`ORB_COMPAT_FROM=v0.1.0 ORB_COMPAT_PUBLISHED=1`) passes again. The rule is written down in [stability](../guides/stability.md#adding-error-codes) and `CONTRIBUTING.md`: new problem codes and audit actions go in packages v0.1 apps don't link. `gorbital.dev/webhook` (linked through `modules/mail/resend`) has no problem codes; `invalid_webhook_signature` is `guard.Webhook`'s, in `gorbital.dev/gorbital`, and `invalid_token` is in `modules/jwt`, which v0.1 apps don't link.

## Implementation notes (2026-09-17)

The tests and benchmarks below were named `httpx` `TestIPFilter*`, `FuzzIPFilter` and `BenchmarkIPFilter` before the [package moves](#package-moves-2026-09-17); they run in `httpx/timeout` and `httpx/ipfilter` now.

| Check | Result |
|---|---|
| `httpx` `TestTimeout*` (race detector, `-count=30`, and with `GOMAXPROCS=2`) | Fast responses pass with headers set, deleted and set without writing; a handler ignoring its context: an HTTP/1.1 client reads the complete 503 with the request ID while the handler still runs, late writes return `ErrHandlerTimeout` and never reach the response; a handler reacting to the deadline loses deterministically; a started response is completed and its context cancelled; SSE through a real server: first event before the handler ends, trailers delivered, `SetWriteDeadline`, `EnableFullDuplex`, `Flush` supported; hijacked connections untouched; every `ResponseController` method after the timeout returns `ErrHandlerTimeout`; a client that went away gets no 503; 103 Early Hints; panics recovered with the handler's stack, before and after the 503; 200 concurrent requests crossing a 2 ms deadline each get exactly the handler's response or the 503 |
| `httpx` `TestIPFilter*`, `TestParsePrefixes`, `FuzzIPFilter`, `FuzzParsePrefixes` | Allow, deny, deny-over-allow, IPv6, mapped clients and ranges, zones, addresses without ports, non-addresses refused; behind `TrustedProxies` a proxied allowed client passes and a client claiming an allowed address doesn't; deny-all, dead allow and family-mixing ranges refused; caller's slices copied |
| `webhook` `TestStandard`, `TestStandardSvixVector`, `TestHMACGitHubVector`, `TestHMACCustomSignedContent`, `TestReorderedDeliveries`, `FuzzStandardVerify`, `FuzzDecodeStandardSecret` | Svix's and GitHub's published vectors; rotation both ways; 4 min 59 s either way accepted, 5 min 1 s refused with `ErrTimestamp`; tampered body, trailing newline, wrong secret, other ID, moved timestamp, other or missing version, other header prefix, missing, malformed or oversized headers refused; the fuzz target checks every acceptance against an independent computation |
| `modules/mail/resend` existing `TestVerifyWebhook`, `TestWebhookSecret`, `FuzzVerifyWebhook`, `FuzzVerifyWebhookSigned` | Pass unchanged after the refactor |
| `gorbital/guard` `TestWebhook*`, examples | Raw and parsed bodies reach the handler identical; tampered, wrong secret, expired, future, missing headers 401 `invalid_webhook_signature`; declared and undeclared oversize 413; cut-off body 400; a signed invalid body reaches validation (422) but an unsigned one never does; rotated secret with reordered deliveries; verifier failure 500; OpenAPI lists 400, 401, 413 and `public,webhook` |
| `modules/jwt` `TestVerify`, `TestVerifyReturnsClaims`, `TestAudienceClaim`, `TestHMAC`, `TestNewValidates`, `TestJWKSContents`, `TestKeyRotation`, `TestJWKSDown`, `TestSlowJWKSAndCancelledRequest`, `TestCacheAge`, `TestMiddleware*`, `FuzzVerify`, `FuzzBearerToken` | RS256, ES256, ES384, EdDSA with generated keys; expired, not yet valid, issued in the future, no `exp`, wrong or trailing-slash issuer, wrong or missing audience, no subject, non-string issuer, tampered payload, another key under a known `kid`, algorithm not fitting the key, algorithm not allowed, `alg: none`, HS256 keyed with the public key, JWE shape, oversize refused; unknown `kid` refused within 30 s of a fetch and accepted after one refetch; 50 concurrent unknown kids cause one fetch; removed keys fail after the cache age; provider down: cached keys work, unknown keys `ErrKeysUnavailable`, no refetch storm, stale keys stop after 24 h, recovery; a request cancelled during a slow fetch doesn't abort it; weak, private, encryption and symmetric keys skipped, unknown key types don't hide good ones; middleware: user and service actors, client info, access log `user_id`, anonymous pass-through for no token, other schemes and non-JWT tokens, 401 `invalid_token` with `WWW-Authenticate` for broken JWTs, earlier actor and client kept, 503 `auth_unavailable`; fuzzing only ever accepts the claims the provider signed |

Benchmarks (Apple M1 Max, Go 1.26.0, `-count 3`):

| Benchmark | Time | Memory | Allocations |
|---|---|---|---|
| `BenchmarkTimeout/without` (recorder, JSON write) | 645–692 ns | 1056 B | 11 |
| `BenchmarkTimeout/with` | 1.62–1.67 µs | 2272 B | 23 |
| `BenchmarkIPFilter` (4 allow, 1 deny range) | 66 ns | 0 B | 0 |
| `BenchmarkStandardVerify` (1.1 KiB body) | 1.66–1.71 µs | 1856 B | 16 |
| `BenchmarkWebhookGuard` (whole Huma request, 1.1 KiB body) | 5.83–5.84 µs | 9880 B | 52 |
| `BenchmarkVerify/RS256` (warm cache) | 51 µs | 10.9 KiB | 158 |
| `BenchmarkVerify/ES256` | 85 µs | 10.6 KiB | 170 |
| `BenchmarkVerify/EdDSA` | 68 µs | 9.2 KiB | 147 |
| `BenchmarkMiddleware` (RS256) | 52 µs | 11.7 KiB | 171 |

The goroutine design of the timeout measured 3.8–4.8 µs and 24 allocations on the same machine before it was replaced.
