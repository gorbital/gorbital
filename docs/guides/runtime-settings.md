# Runtime settings guide

`gorbital.dev/modules/settings` stores non-secret tunables in PostgreSQL so operators change them without a redeploy. Decision: [ADR-0031](../adr/0031-runtime-settings.md). Admin endpoints and every setting the Full apps declare, with defaults, bounds and whether a reason is required: [runtime settings reference](../reference/settings.md), generated from the golden apps; the endpoints: [ops API reference](ops-api.md#runtime-settings).

## Environment or runtime setting?

| Put it in | When | Examples |
|---|---|---|
| **Environment** (`.env.example`; read by `gorbital.LoadConfig`, or `internal/app/config.go` in a v0.1 app) | It is a secret, infrastructure, or needed before the database connects | `DATABASE_URL`, API keys such as `RESEND_API_KEY`, listen address, pool size |
| **Runtime setting** (a module's `Settings`, or `internal/app/settings.go` in a v0.1 app) | An operator should change it without a deploy | Code expiry, rate limits, sender name, frontend URL, maintenance mode |

A value is never in both, and secrets are never runtime settings.

## Declaring settings

Declare every setting before building the store — in the module that owns it in an app on [`gorbital.Main`](main-go.md) ([below](#in-an-app-on-gorbitalmain)), in `internal/app/settings.go` in a v0.1 app:

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
| `OrgOverridable()` | all but `RestartRequired` | Each organisation may have its own value ([below](#per-organisation-settings)) |
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

## In an app on gorbital.Main

`gorbital.New` does all of the wiring above: it creates the registry, calls each module's `Settings` func with it, builds the store, runs it and hands modules the result as `Deps.Settings`. A module declares its own settings and keeps the handles in the closure its `Module` function returns, so `Routes` uses them without a lookup by name:

```go
// internal/modules/books/module.go
func Module() gorbital.Module {
	var pageSize *settings.Setting[int]
	return gorbital.Module{
		Name: "books",
		Settings: func(r *settings.Registry) {
			pageSize = settings.Int(r, "books.page_size", 20,
				settings.Describe("How many books one page of GET /v1/books returns."),
				settings.Group("books"),
				settings.Range(1, 100),
			)
		},
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			svc := usecase.NewService(repository.NewStore(d.DB), pageSize)
			delivery.Register(r, svc)
		},
	}
}
```

- `New` calls `Settings` once, in module order, before the store exists. Declare there and nothing else: a handle's `Get` returns the default until the store has loaded.
- Keys are still global and still public API. Namespace them with the module's name, as the library's own modules do (`auth.*`, `orgs.*`, `mail.*`).
- An invalid declaration doesn't panic out of the app: `New` reports it as an error naming the module.
- `gorbital.Declare` is the same step on its own, for an app that composes the pieces by hand rather than through `Main`.

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

## Per-organisation settings

In a multi-tenant app, a setting declared with `OrgOverridable()` takes a value per organisation, within the same validation ([ADR-0056](../adr/0056-per-organisation-settings.md)). The Full multi-tenant app declares one, `orgs.invitation_ttl`:

```go
settings.Duration(reg, "orgs.invitation_ttl", 7*24*time.Hour,
	settings.Range(24*time.Hour, 30*24*time.Hour),
	settings.ReasonRequired(),
	settings.OrgOverridable(),
)
```

`Get(ctx)` returns the organisation's value when the actor in `ctx` acts in an organisation that has one, and the platform value otherwise. `orgs.RequireMember` returns such a context, so a use case that reads the setting after checking membership gets the organisation's value with no other change. Still no database call: every organisation value is kept in memory, about 350 bytes each.

| In Go | Over HTTP (multi-tenant Full app) | Who |
|---|---|---|
| `store.ListForOrg(orgID)`, `store.GetForOrg(orgID, key)` | `GET /v1/orgs/{orgId}/settings`, `GET /v1/orgs/{orgId}/settings/{key}` | Members (`orgs.settings.read`) |
| `store.SetForOrg(ctx, orgID, key, value, change)` | `PUT /v1/orgs/{orgId}/settings/{key}` `{value, version, reason?}` | Owners and admins (`orgs.settings.write`) |
| `store.ResetForOrg(ctx, orgID, key, change)` | `DELETE /v1/orgs/{orgId}/settings/{key}` `{version, reason?}`: back to the platform value | Owners and admins |
| `store.HistoryForOrg(ctx, orgID, key, before, limit)` | `GET /v1/orgs/{orgId}/settings/{key}/history` | Members |
| `store.Overrides(ctx, key, after, limit)` | `GET /ops/settings/{key}/overrides` | Operators (`ops.settings.read`) |

- Organisation values have their own versions, history rows (with `org_id`) and `settings.value.changed` audit events carrying the organisation. `ReasonRequired` applies to them too.
- Responses show `value` (what the organisation gets), `platform_value` (what it gets without its own) and `overridden`.
- The library doesn't check membership: call `orgs.RequireMember` first, as the orgs module does.
- Purging an organisation deletes its values and history through foreign keys (`20260918000002_settings_org_purge.sql`).

> [!DONT]
> Don't mark a setting `OrgOverridable` if it protects accounts or the platform: sign-in, rate limits, retention, maintenance, email senders, or where links go. A v0.1 app has `TestSecuritySettingsArentOrgOverridable` in `internal/app/settings_test.go`, which fails for those groups and keys; extend its lists when you add a security-relevant group. An app on `gorbital.Main` has no such test of its own: the library holds the rule for its modules' settings, and the judgement is yours for your modules'.

## Storage

| Table | Holds |
|---|---|
| `settings_values` | One row per changed setting: `key`, `value` (jsonb, NULL = default), `version`, `updated_at`, `updated_by`, and `org_id` (NULL for the platform value, else the organisation's own value) |
| `settings_history` | Every change with old and new values, reason, actor, request ID and `org_id` |

## Limitations

- Settings apply to everyone, or per organisation; to turn a feature on for some users or a percentage of them, use a [feature flag](feature-flags.md) (ADR-0057).
- A setting cannot be locked from the environment.
