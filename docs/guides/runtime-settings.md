# Runtime settings guide

`gorbital.dev/modules/settings` stores non-secret tunables in PostgreSQL so operators change them without a redeploy. Decision: [ADR-0031](../adr/0031-runtime-settings.md). Admin endpoints and every setting the Full apps declare, with defaults, bounds and whether a reason is required: [runtime settings reference](../reference/settings.md), generated from the golden apps; the endpoints: [ops API reference](ops-api.md#runtime-settings).

## Environment or runtime setting?

| Put it in | When | Examples |
|---|---|---|
| **Environment** (`internal/app/config.go`, `.env.example`) | It is a secret, infrastructure, or needed before the database connects | `DATABASE_URL`, API keys such as `RESEND_API_KEY`, listen address, pool size |
| **Runtime setting** (`internal/app/settings.go`) | An operator should change it without a deploy | Code expiry, rate limits, sender name, frontend URL, maintenance mode |

A value is never in both, and secrets are never runtime settings.

## Declaring settings

Declare every setting before building the store, usually in `internal/app/settings.go`:

```go
reg := settings.NewRegistry()

codeTTL := settings.Duration(reg, "auth.verification_code_ttl", 15*time.Minute,
	settings.Describe("How long email verification codes stay valid."),
	settings.Range(5*time.Minute, time.Hour),
	settings.ReasonRequired(),
)
origins := settings.StringList(reg, "http.cors_origins", nil, settings.URL(), settings.MaxItems(20))
delivery := settings.Enum(reg, "mail.delivery", "async", []string{"sync", "async"})
```

Keys are dotted lowercase, namespaced by feature (`module.name`). Keys are public API: never rename one.

| Declaration | Go type | Stored and edited as |
|---|---|---|
| `Bool` | `bool` | `true` |
| `Int` | `int` | `42` |
| `Float` | `float64` | `0.5` |
| `String` | `string` | `"text"` |
| `Enum` | `string` | `"async"`, one of the allowed values |
| `Duration` | `time.Duration` | `"15m"`, `"1h30m"` |
| `StringList` | `[]string` | `["a", "b"]` |

| Option | Applies to | Effect |
|---|---|---|
| `Describe(text)` | all | Help text shown to operators |
| `Group(name)` | all | Listing group (default: the key's first segment) |
| `ReasonRequired()` | all | Every change needs a reason. Use it for every security-relevant setting: lifetimes of sessions and codes, rate limits, retention, maintenance, and anything that decides where emails and links go or who emails come from |
| `RestartRequired()` | all | `Get` keeps the startup value; changes apply after restart |
| `Range(lo, hi)` | Int, Float, Duration | Bounds; types must match (`Range(0.0, 1.0)` for Float) |
| `OneOf(values...)` | String, StringList items | Allowed values |
| `MaxLen(n)` | String, StringList items | Maximum characters |
| `MaxItems(n)` | StringList | Maximum items; without it, `DefaultMaxItems` (100) |
| `URL()` | String, StringList items | Absolute http(s) URL |
| `Email()` | String, StringList items | Bare email address |
| `Validate(func(T) error)` | all | Custom check; messages must not include the value |

An invalid declaration (bad key, duplicate, mismatched option, default outside its bounds) panics at startup.

## Wiring the store

```go
store, err := settings.NewStore(ctx, pool, reg, recorder, settings.WithLogger(logger))
if err != nil {
	return err
}
// Run it with the app's runners so changes from other instances arrive.
runners := []app.Runner{server, store}
```

- `NewStore` loads every stored value and freezes the registry.
- The tables come from `settings.Migrations`, copied into `db/migrations`.
- `Store.Run` listens with PostgreSQL `LISTEN/NOTIFY` on a dedicated connection, reloads after every reconnect, and reloads everything every 5 minutes (`WithResyncInterval`).

## Reading settings

Every handle implements `config.Value[T]`:

```go
ttl := codeTTL.Get(ctx) // in-memory read, never a database call
```

Pass handles to use cases and library modules as `config.Value[T]`, and call `Get` each time the value is needed. For a fixed value (tests, spec export), use `config.Static(v)`.

`Get` returns the default when the setting was never changed, was reset, has a stored value that fails current validation, or before the store has loaded.

## Changing settings

Operators use `PUT /ops/settings/{key}`. In Go:

```go
view, err := store.Set(ctx, "auth.verification_code_ttl", json.RawMessage(`"30m"`),
	settings.Change{Version: view.Version, Reason: "support backlog"})
```

| Behaviour | Detail |
|---|---|
| Actor | The context must carry an authenticated actor (`ErrActorRequired`) |
| Versions | `Change.Version` must equal the current version (`ErrVersionConflict`); a never-changed setting is version 0 |
| Validation | `*InvalidValueError` with a reason that never includes the value |
| No-op | Setting the current value again, or resetting a default, changes nothing |
| History | Old value, new value, reason, actor and request ID per change (`Store.History`) |
| Audit | One `settings.value.changed` event per change, with `version`, `reset` and `reason` metadata, and the client's IP address and user agent for changes made over HTTP |
| Propagation | This instance immediately; others through `NOTIFY` within moments |
| Reset | `Store.Reset` stores NULL, so versions never repeat |

## Views for admin panels

`Store.List()` and `Store.Get(key)` return a `View` with the effective and default values, `Modified`, `InvalidStoredValue`, `Version`, `UpdatedAt`, `UpdatedBy`, `ReasonRequired`, `RestartRequired`, `RestartPending` and `Constraints` (min, max, one_of, max_len, max_items, format). `Store.UnknownKeys()` lists stored keys no longer declared.

## Storage

| Table | Holds |
|---|---|
| `settings_values` | One row per changed setting: `key`, `value` (jsonb, NULL = default), `version`, `updated_at`, `updated_by`, `org_id` (reserved) |
| `settings_history` | Every change with old and new values, reason, actor and request ID |

## Limitations

- No per-organisation settings yet (the `org_id` column is reserved).
- No feature flags or percentage rollouts (v1.1).
- A setting cannot be locked from the environment.
