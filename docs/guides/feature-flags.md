# Feature flags

A feature flag lets operators turn a feature on for some organisations, some users, a stable share of everyone, or nobody, without a redeploy. `gorbital.dev/modules/flags` declares flags in Go, stores what operators change in PostgreSQL, and applies changes on every instance within moments. Decision: [ADR-0057](../adr/0057-feature-flags.md). Endpoints: [ops API reference](ops-api.md#feature-flags).

## Flag, setting or permission?

| Use | When | Example |
|---|---|---|
| **Feature flag** (`internal/app/flags.go`) | Who gets a feature is decided per organisation, per user or by percentage, usually for a while | A new checkout for 10% of users, a beta for three customers, a kill switch |
| **Runtime setting** (`internal/app/settings.go`) | A value applies to everyone (or per organisation), with bounds | A code expiry, a rate limit, a sender name |
| **Permission** (`internal/app/permissions.go`) | Access control: who may do something | Changing an organisation's members |

Flags are not access control. A client can see which client flags are on for it, and a flag's lists are edited by operators: never protect data or actions with a flag alone.

## Declaring flags

```go
reg := flags.NewRegistry()

newCheckout := flags.Bool(reg, "checkout.new_flow",
	flags.Describe("The redesigned checkout."),
	flags.Client(), // listed by GET /v1/flags
)
searchKill := flags.Bool(reg, "search.enabled", flags.DefaultOn())
```

Keys are dotted lowercase, namespaced by feature. Keys are public API (recorded in `api/surface.json`): remove the code that reads a flag before its key, and never reuse a key for another feature.

| Option | Effect |
|---|---|
| `Describe(text)` | Help text shown to operators |
| `Group(name)` | Listing group (default: the key's first segment) |
| `Client()` | Signed-in clients read it from `GET /v1/flags`. Other flags stay server-side, so their keys don't reveal unreleased features |
| `DefaultOn()` | Declared enabled with a default of true: on for everyone until an operator changes it. Without it, a flag is off until an operator turns it on |

A bad key, a duplicate or a declaration after `NewStore` panics at startup.

## Wiring the store

```go
store, err := flags.NewStore(ctx, pool, reg, recorder, flags.WithLogger(logger))
if err != nil {
	return err
}
runners := []app.Runner{server, store} // so changes from other instances arrive
```

The Full apps do this in `internal/app/app.go` and list the store in `Workers()`. The tables come from `flags.Migrations`, copied as `db/migrations/20260918000010_flags.sql`. `Store.Run` listens on the `gorbital_flags` channel, reloads after every reconnect, and reloads everything every 5 minutes.

## Checking a flag

```go
if newCheckout.Enabled(ctx) {
	return s.newCheckout(ctx, cart)
}
```

`Enabled` reads memory (about 140 ns, no allocation) and the actor in `ctx`. A `*flags.Flag` is also a `config.Value[bool]`, so a module can take it as a live option without importing the flags module; the Full apps pass `example.ping_time` to the ping module that way. `Evaluate(ctx)` also returns the rule that decided (`disabled`, `org_denied`, `org_allowed`, `user_denied`, `user_allowed`, `rollout`, `default`), handy in tests and logs.

## How a flag decides

A flag's state has five parts, applied in order; the first that applies decides:

| # | Rule | Applies when | Answer |
|---|---|---|---|
| 1 | `enabled` is false | Always | false: a kill switch that keeps the rest for later |
| 2 | `orgs.deny`, then `orgs.allow` | The caller acts in an organisation (`orgs.RequireMember` put it in the context) and it is listed | false, or true |
| 3 | `users.deny`, then `users.allow` | The caller is signed in (a user, service account or other authenticated actor) and its ID is listed | false, or true |
| 4 | `percentage` | A percentage is set | true when the subject's bucket is below it |
| 5 | `default` | Otherwise | the default |

- **Organisation before user.** A member of an allowed organisation gets the feature there even if their user ID is denied, and a denied organisation beats a user allow. Outside the organisation, their user rule applies.
- **Subject.** In an organisation the organisation is the subject, so every member gets the same answer; elsewhere the caller's ID is.
- **Buckets.** `flags.Bucket(key, subject)` is the first eight bytes of SHA-256 of the key, a zero byte and the subject, as a big-endian number, modulo 100. The same subject always gets the same answer on every instance; raising the percentage only adds subjects; and each flag picks its own subjects, so one group of users doesn't get every experiment. A client or data pipeline can compute the same buckets.
- **Anonymous callers** have no subject: `percentage: 0` and `100` still apply, any other percentage leaves them to `default`.

Bounds: each list holds at most 1,000 IDs of 1 to 100 visible ASCII characters, an ID can't be in both lists of a rule, and `percentage` is 0 to 100. Lists are stored sorted without duplicates. For more targets, use a percentage, or your own data.

## Changing flags

Operators with `ops.flags.write` (`platform_admin`) use `PUT /ops/flags/{key}` with the whole state, the `version` they read and a `reason`; `DELETE /ops/flags/{key}` returns to the declared state. `ops.flags.read` (`ops_viewer` too) lists flags and their history. Typical rollout:

```bash
flag() {
  curl -X PUT http://127.0.0.1:8080/ops/flags/checkout.new_flow \
    -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d "$1"
}
# 1. Internal testers only.
flag '{"version":0,"reason":"internal testing","state":{"enabled":true,"default":false,"users":{"allow":["usr_a","usr_b"]}}}'
# 2. 10% of everyone, testers still in.
flag '{"version":1,"reason":"10% rollout","state":{"enabled":true,"default":false,"users":{"allow":["usr_a","usr_b"]},"percentage":10}}'
# 3. Everyone.
flag '{"version":2,"reason":"released","state":{"enabled":true,"default":true}}'
# Something broke: switch it off, keeping the default for when it's fixed.
flag '{"version":3,"reason":"incident 42","state":{"enabled":false,"default":true}}'
```

| Behaviour | Detail |
|---|---|
| Actor and reason | Required for every change and reset (`flag_reason_required`) |
| Versions | A stale `version` answers 409 `flag_version_conflict`; read again |
| No-op | The same state again (lists in any order) changes nothing |
| History | Old and new states, reason, actor and request ID per change (`GET /ops/flags/{key}/history`), deleted after `ops.history_retention` by the `retention` job |
| Audit | `flags.flag.changed` (summary: enabled, default, percentage, how many organisations and users are targeted) and `flags.flag.reset`; the IDs themselves are only in the history |
| Propagation | This instance immediately, others through `NOTIFY`, and every 5 minutes at the latest |
| Unknown and invalid rows | Rows of flags no longer declared are kept and ignored; a stored state that fails validation is ignored (the declared state applies), logged, and shown as `invalid_stored_value` |

## Clients

In an app on `gorbital.Main`, a module declares its flags in `Module.Flags` ([modules and routes](modules-and-routes.md)) and the endpoint comes with `gorbital.WithModules(flagshttp.Module())` ([Methods](../methods/gorbital-flagshttp.md)); a v0.1 app has it in `internal/modules/flags`.

`GET /v1/flags` (signed in, permission `flags.flag.read`, which the `user` role gives every user; an API key needs it in its scopes) returns the client flags evaluated for the caller, with `Cache-Control: private, no-store`:

```json
{"flags": {"example.ping_time": false}}
```

In multi-tenant apps, `GET /v1/orgs/{orgId}/flags` returns them as the organisation, for any member; non-members get 404 `org_not_found` like every organisation route. Read them after sign-in and when switching organisation, and treat a missing key as off.

## Testing

Unit-test code behind a flag by passing `config.Static(true)` or `config.Static(false)` where a module takes a `config.Value[bool]`. `internal/app/flags_test.go` changes the example flag over HTTP and checks `GET /v1/ping` and `GET /v1/flags` for allowed users, rollouts and anonymous callers, and that a second instance applies changes; `org_flags_test.go` (multi-tenant) checks organisation targeting and cross-organisation denial. The library's tests pin bucket values, check precedence and the distribution over 10,000 subjects, and run the store against Docker PostgreSQL.

## Storage

| Table | Holds |
|---|---|
| `flags_states` | One row per changed flag: `key`, `state` (jsonb, NULL = declared state), `version`, `updated_at`, `updated_by` |
| `flags_history` | Every change with old and new states, reason, actor and request ID |

Organisation and user IDs in lists are identifiers, not personal details, but they stay in states and history after an account or organisation is deleted, until an operator edits the flag and the history ages out.

## Limitations

- Flags are on/off; no string or number variants, no targeting by attributes such as country or plan.
- No scheduled changes, and no per-flag expiry: remove stale flags from code.
- Every flag's state is kept in memory on every instance (up to 4,000 IDs per flag).
