# Mobile backend with an external identity provider

The API behind a travel app. People sign in on their phone with an identity provider — Auth0, Clerk, Supabase, Firebase or Amazon Cognito — and the app sends the access token it gets back with every request. The API stores no passwords, sends no email, issues no sessions and has no accounts table: it verifies the provider's tokens, turns each one into an actor, and keeps that traveller's trips.

The app is in `examples/apps/mobile-backend/`. It has one module, `trips`, laid out like Shelfie's books module ([1. A books module](../shelfie/01-books-module.md)): `domain/`, `usecase/`, `repository/` and `delivery/`, one file per operation.

| Route | Needs | Does |
|---|---|---|
| `POST /v1/trips` | `trips.trip.write` | Adds a trip for the caller |
| `GET /v1/trips` | `trips.trip.read` | Lists the caller's trips, newest first |
| `GET /v1/trips/{id}` | `trips.trip.read` | Reads one of the caller's trips |
| `DELETE /v1/trips/{id}` | `trips.trip.write` | Deletes one of the caller's trips |

## Choosing a provider over gorbital's own sign-in

Both work in an app on `gorbital.Main`, and both end in the same place: an actor in the request context, which the guards, the rate limiters and the audit log read. They differ in what you own.

| | External provider (this recipe) | [gorbital's sign-in](../../guides/authentication.md) |
|---|---|---|
| Accounts, passwords, MFA, password resets | The provider's | Yours, in your database |
| Who your users are | Subjects (`sub`) of the provider's tokens | Rows you can join, list and delete |
| Sign-in screens | The provider's SDK in the mobile app | Your API's endpoints, your screens |
| Revoking access now | Not possible: a token is valid until `exp`, so keep them short | Delete the session |
| Cost of an outage at the provider | Nobody signs in; requests with a valid, cached-key token still work | Yours to keep up |
| `guard.RecentReauth`, API keys, organisations | Not available: they need gorbital's own principal | Available |

Choose the provider when you already run one for other apps, when the mobile app wants social or enterprise sign-in without your writing it, or when a platform team owns identity. Choose gorbital's sign-in when the accounts are part of the product.

The two can share an app: the JWT middleware passes a request that already carries an actor straight on, and leaves a bearer token that isn't shaped like a JWT for another authenticator. `gorbital.WithAuth` takes one authenticator, so an app that wants both composes their middleware itself, with `gorbital.WithStack` in place of the stack's `Auth` step.

## main.go

<!-- include examples/apps/mobile-backend/cmd/api/main.go#main -->

`gorbital.WithAuth` takes anything with a `Middleware(*slog.Logger) func(http.Handler) http.Handler` method, which `*jwt.Authenticator` has. It can't be passed straight to `WithAuth` here, though: `jwt.New` takes a context and reaches the provider over the network, and `main` has neither a context nor anywhere to report an error. The app passes a small type of its own instead, which does the work in gorbital's two optional hooks:

<!-- include examples/apps/mobile-backend/cmd/api/idp.go#authenticator -->

`CheckConfig` runs before the app connects to anything, so a missing variable exits with status 2 alongside the other configuration errors; `Setup` runs with a context while the app is built, so an unreachable provider or a mistyped JWKS URL stops a deployment instead of turning into 503s later. Neither runs while the OpenAPI document is exported, which needs no configuration.

## Configuring the authenticator

Everything the authenticator needs is public — there is no client secret, because the API only verifies signatures — so it all lives in environment variables:

<!-- include examples/apps/mobile-backend/.env.example#idp -->

`cmd/api/idp.go` turns them into a `jwt.Config`, reporting every problem at once as `gorbital.LoadConfig` does:

<!-- include examples/apps/mobile-backend/cmd/api/idp.go#idp-config -->

| Setting | What it is | Unset |
|---|---|---|
| `Issuer` | The `iss` claim, compared exactly, trailing slash and all | Required |
| `Audiences` | What identifies this API; a token issued for another of the provider's APIs is refused | Required |
| `JWKSURL` | Where the provider publishes its public keys: https, or http on a loopback address | Required for asymmetric algorithms |
| `AudienceClaim` | The claim holding the audience | `aud` |
| `Algorithms` | The accepted `alg` values | RS256, ES256 and EdDSA |
| `ClockSkew` | How far token times may be from this server's clock, at most 5 minutes | 30 seconds |
| `PermissionsClaim` | The claim the caller's permissions are read from | `permissions` |

List **exactly the algorithms your provider uses** once you know them, and keep several audiences only while you migrate from one to the next.

## What is verified, and what isn't

Every request with an `Authorization: Bearer <JWT>` header is checked before any route sees it: the signature against the provider's published key for the token's `kid`, the algorithm against the allowed list, then `iss`, the audience, `sub`, `exp`, `nbf` and `iat` within the clock skew.

| Request | Answer |
|---|---|
| A valid token | The handler runs, with the caller as the actor |
| No `Authorization` header, another scheme, or a bearer token that isn't shaped like a JWT (an API key) | The request continues with no actor, so these routes answer `401 unauthenticated` |
| Expired, not yet valid, tampered with, for another issuer or audience, or signed with a key or algorithm that isn't allowed | `401 invalid_token`, with `WWW-Authenticate: Bearer error="invalid_token"` |
| A token whose key the cache hasn't got, while the provider can't be reached | `503 auth_unavailable` |

What it does **not** do: it doesn't sign anyone in, refresh or revoke tokens, and it can't tell an ID token from an access token unless their audiences differ or you check a claim yourself. A stolen token works until it expires, which is the argument for short-lived access tokens at the provider. `guard.RecentReauth` and everything else that needs a gorbital session refuses these callers.

## From claims to actor to permissions

The authenticator's default mapping is the whole of this app's identity model:

| Token | Actor |
|---|---|
| `sub` | The actor's ID, a user |
| The `permissions` claim (`PermissionsClaim`), a list of strings or one space-separated string | The actor's permissions |

So the provider must issue tokens carrying this app's permission names, `trips.trip.read` and `trips.trip.write`. The module declares them, without roles, because nothing in this app grants them — the provider does:

<!-- include examples/apps/mobile-backend/internal/modules/trips/module.go#permissions -->

When the provider's names are its own (`read:trips`, a Cognito group, a Clerk session-token claim), map them with `Config.ActorFrom` instead, which replaces the default mapping entirely and can also recognise machine-to-machine tokens as service actors or set an organisation ([mapping claims to the actor](../../guides/security-layers.md#mapping-claims-to-the-actor)). Keep the mapping in one place: use either the provider's claim or `ActorFrom`, never a guard that reads claims of its own.

The use cases then read the actor, never the request body:

<!-- include examples/apps/mobile-backend/internal/modules/trips/usecase/service.go#traveller -->

## The trips module

The route table asks for a permission on each route, and nothing else:

<!-- include examples/apps/mobile-backend/internal/modules/trips/delivery/routes.go#routes -->

A permission says what a traveller may do; it never says whose rows they may do it to. That second question is the module's, and it is answered in one place: the `Store` port takes the owner on every method, so a query can't be written that forgets it.

<!-- include examples/apps/mobile-backend/internal/modules/trips/usecase/ports.go#store -->

The owner comes from the actor, so the API has no field a client could set:

<!-- include examples/apps/mobile-backend/internal/modules/trips/usecase/add_trip.go#add -->

<!-- include examples/apps/mobile-backend/internal/modules/trips/delivery/add_trip.go#add-handler -->

Reading and deleting filter by owner in the SQL, which makes another traveller's trip indistinguishable from an ID that was never used:

<!-- include examples/apps/mobile-backend/internal/modules/trips/repository/select_trip.go#select-trip -->

That is a deliberate choice of answer, recorded in the error:

<!-- include examples/apps/mobile-backend/internal/modules/trips/domain/errors.go#errors -->

The table has no accounts to reference: the provider owns the people, this app owns their trips.

<!-- include examples/apps/mobile-backend/db/migrations/20260921000001_trips.sql#table -->

## Keys, rotation and outages

The app holds only public keys, and fetches them itself; nothing runs in the background.

| Situation | Behaviour |
|---|---|
| Start | `jwt.New` fetches the key set once. No keys, or none usable, is an error, so a mistyped URL fails at deploy |
| Normal traffic | Keys come from memory, cached as long as the provider's `Cache-Control: max-age` says, between 5 minutes and 24 hours |
| The provider rotates its signing key | The first token naming a `kid` the cache hasn't got fetches the key set again, at most once every 30 seconds, shared by every waiting request |
| The provider is unreachable | Cached keys keep working for up to 24 hours past their cache age. A token needing a key that isn't cached gets `503 auth_unavailable` |
| A key that is too weak (RSA under 2048 bits), `alg: none`, or a key embedded in the token | Never used |

The 503 matters to the mobile app: a client that treats every refusal as "signed out" will log people out during an outage at the provider. `401 invalid_token` means *get a new token*; `503 auth_unavailable` means *retry shortly*.

The test moves a clock rather than sleeping, so it can walk through a rotation and an outage in milliseconds:

<!-- include examples/apps/mobile-backend/internal/modules/trips/trips_test.go#rotation-and-outage -->

## Testing with a local JWKS

The tests need no provider account and no network: an `httptest` server publishes a key set, and the test signs its own tokens with the matching private key. That is exactly what a provider does, so the app under test is wired as it is in production.

<!-- include examples/apps/mobile-backend/internal/modules/trips/idp_test.go#test-idp -->

<!-- include examples/apps/mobile-backend/internal/modules/trips/trips_test.go#test-app -->

A valid token reaches the routes, and what it writes belongs to its subject:

<!-- include examples/apps/mobile-backend/internal/modules/trips/trips_test.go#valid-token -->

Everything else is refused, and the table says which refusal each mistake earns:

<!-- include examples/apps/mobile-backend/internal/modules/trips/trips_test.go#refused-tokens -->

Permissions are the token's, so a token without one is refused before the use case runs:

<!-- include examples/apps/mobile-backend/internal/modules/trips/trips_test.go#permissions-from-claims -->

And a second traveller, holding every permission the routes ask for, still reaches nothing of the first's:

<!-- include examples/apps/mobile-backend/internal/modules/trips/trips_test.go#other-travellers -->

Run them with PostgreSQL up (`orb dev`, or `docker compose up -d --wait`):

```bash
export GORBITAL_TEST_DATABASE_URL='postgres://mobile_backend:mobile_backend@127.0.0.1:5432/mobile_backend?sslmode=disable'
export GORBITAL_REQUIRE_DB=1
go test ./...
```

| Test | Checks |
|---|---|
| `TestATokenReachesOnlyItsOwnTrips` | A valid token works end to end; the row and the audit event carry the token's subject |
| `TestTokensTheAPIRefuses` | No token, expired, wrong audience, wrong issuer, forged signature, and a bearer token that isn't a JWT |
| `TestPermissionsComeFromTheToken` | A token without the permission gets 403, with it 200 |
| `TestAnotherTravellersTripIsNotFound` | One traveller can't read or delete another's trip, and doesn't see it in their list |
| `TestKeyRotationAndOutage` | Rotation is followed without a restart; cached keys survive an outage; an unknown key while the provider is down is 503 |
| `TestIDPConfig`, `TestIDPConfigReportsEveryProblem` (`cmd/api/idp_test.go`) | `IDP_*` becomes a `jwt.Config`; a deployment missing variables is told all of them at once |
| `TestOpenAPIIsCurrent` (`cmd/api/main_test.go`) | `api/openapi.json` matches the code |
| `internal/modules/architecture_test.go` | The layers import only what they may |

More: [Testing with gorbitaltest](../../guides/testing-with-gorbitaltest.md).

## Settings per provider

The shapes below are the usual ones; **check each value in your provider's documentation and dashboard**, because they differ by tenant, region and token type, and providers change them.

| Provider | `IDP_ISSUER` | `IDP_JWKS_URL` | Audience | Permissions |
|---|---|---|---|---|
| Auth0 | `https://<tenant>.<region>.auth0.com/`, with the trailing slash | The issuer followed by `.well-known/jwks.json` | The API identifier | `permissions`, once RBAC and "Add Permissions in the Access Token" are on |
| Clerk | Your instance's Frontend API URL | The issuer followed by `/.well-known/jwks.json` | `IDP_AUDIENCE_CLAIM=azp` with your app's origins | A custom claim in a session-token template, mapped with `ActorFrom` |
| Supabase | `https://<ref>.supabase.co/auth/v1` | The issuer followed by `/.well-known/jwks.json` | `authenticated` | `app_metadata`, read with `Claims.Decode` in `ActorFrom` |
| Firebase | `https://securetoken.google.com/<project-id>` | `https://www.googleapis.com/service_accounts/v1/jwk/securetoken@system.gserviceaccount.com`, not under the issuer | The project ID | Custom claims, mapped with `ActorFrom` |
| Amazon Cognito (access tokens) | `https://cognito-idp.<region>.amazonaws.com/<user-pool-id>` | The issuer followed by `/.well-known/jwks.json` | `IDP_AUDIENCE_CLAIM=client_id` with the app client IDs | `IDP_PERMISSIONS_CLAIM=scope`, or `cognito:groups` with `ActorFrom` |

A Supabase project still on the legacy shared JWT secret is the one case without a JWKS URL: `Algorithms: []string{"HS256"}` and `jwt.WithHMACSecret(secret)`, with the secret read as a `config.Secret` like any other. Prefer the project's asymmetric keys — anyone holding that secret can issue tokens your API will believe.

## Related

- [Security layers](../../guides/security-layers.md#external-identity-providers-jwt): the other layers, and this one in an app that isn't on `gorbital.Main`
- [Authentication](../../guides/authentication.md): gorbital's own sign-in, and what it adds that tokens don't
- [modules/jwt](../../methods/modules-jwt.md): every option of `jwt.Config`, `Claims` and `Verify`
- [gorbital/guard](../../methods/gorbital-guard.md): the guards on the routes, and what each refusal says
