# 14. Settings and feature flags

Two of Plateful's numbers have no business being in the code. How many orders one kitchen may hold before it stops taking more is a judgement that changes with the season. How long a restaurant may sit on an accepted order before the sweep from [chapter 13](13-background-jobs.md) complains is a judgement that changes with the first angry customer. Neither is worth a deploy.

And one of Plateful's features isn't finished. Assigning a courier automatically when an order is ready works, probably, but nobody wants to find out on a Friday night across three hundred restaurants at once.

Those are two different tools. This chapter is both of them: **runtime settings**, for a value that applies to everyone and has bounds, and **feature flags**, for who gets a feature while you find out whether it works.

| Use | When | Plateful's |
|---|---|---|
| **Runtime setting** | A value applies to everyone (or per organisation), within bounds | `orders.max_open_per_restaurant`, `orders.late_after`, `images.max_bytes` |
| **Feature flag** | *Who* gets something is decided per organisation, per user, or by percentage, usually for a while | `orders.scheduled_ordering`, `orders.courier_auto_assign` |
| **Permission** | Access control: who may do something at all | `orders.order.place`, `reviews.review.moderate` |

The third row is there because the first question people get wrong is which of the three they wanted. A flag is not access control: its lists are edited by operators, a client can read which client flags are on for it, and turning a flag on is not a grant. Permissions are declared in `Module.Permissions` and checked by guards, and they stay there.

Depth on either library lives in the [runtime settings guide](../guides/runtime-settings.md) and the [feature flags guide](../guides/feature-flags.md). This chapter is about using both in a module.

## 1. Declare the settings

**What we're doing.** Adding three settings and two flags to the orders module.

**Why.** Because an operator should be able to change them at 02:40 without waking anybody, and because a number with a name and a description is a number somebody can reason about — `50` in a `if` statement is not.

**What the framework already gives us.** `gorbital.dev/modules/settings` and `gorbital.dev/modules/flags`: a typed declaration, a PostgreSQL table for what operators change, validation against the bounds you declare, a version per value so two operators can't silently overwrite each other, a history row per change, an audit event per change, and propagation to every instance. `gorbital.New` creates both registries, calls each module's `Settings` and `Flags`, builds the stores and runs them.

**What we build ourselves.** The declarations, and nothing else.

**How.** `Module.Settings` is `func(r *settings.Registry)` and `Module.Flags` is `func(r *flags.Registry)`. Both run once at startup, before the stores exist, and both return their handles into variables the module closure holds — so `Routes` and `Jobs` use them without ever looking a setting up by name:

<!-- include examples/apps/plateful/internal/modules/orders/module.go#settings-and-flags -->

**What just happened.** Five entries appeared in `/ops`. `GET /ops/settings` now lists three keys grouped under `orders` with their defaults, bounds and help text; `GET /ops/flags` lists two. A `platform_admin` can change any of them over HTTP, and the next request to hit the app uses the new value.

The `var` declarations above this function, and the `config()` closure that packages them, are the shape worth copying:

```go
var (
    maxOpen    *settings.Setting[int]
    lateAfter  *settings.Setting[time.Duration]
    paused     *settings.Setting[bool]
    scheduled  *flags.Flag
    autoAssign *flags.Flag
)
```

They are assigned in `Settings` and `Flags`, and read in `Routes` and `Jobs`, which `gorbital.New` calls afterwards. No string key appears anywhere but the declaration, so a typo is a compile error rather than a setting that silently stays at its default.

## 2. The seven types, and the options worth using

There are seven declaration functions and no others:

| Declaration | Go type | Stored and edited as |
|---|---|---|
| `settings.Bool` | `bool` | `true` |
| `settings.Int` | `int` | `42` |
| `settings.Float` | `float64` | `0.5` |
| `settings.String` | `string` | `"text"` |
| `settings.Enum` | `string` | `"async"`, one of the values you list |
| `settings.Duration` | `time.Duration` | `"15m"`, `"1h30m"` |
| `settings.StringList` | `[]string` | `["a", "b"]` |

Three options do most of the work, and the images module's single setting shows all three with the reasoning written out:

<!-- include examples/apps/plateful/internal/modules/images/module.go#image-settings -->

**`Range(lo, hi)`** is the one that stops an incident. A setting an operator can type into is a setting an operator can type `0` into, at speed, during an outage. `Range(64<<10, 25<<20)` means `images.max_bytes` cannot be set to something that breaks every upload on the platform, and the refusal happens in the API with a message, not in production with a support queue. The bounds' types must match the setting's: `Range(0.0, 1.0)` for a `Float`, a `time.Duration` pair for a `Duration`.

**`ReasonRequired()`** makes every change carry a sentence, stored in the setting's history and in the `settings.value.changed` audit event. Use it for anything security-relevant — lifetimes, rate limits, retention, maintenance, and anything deciding where email or links go — and for anything whose value will one day make somebody ask "why is this 40,000?". All four of Plateful's settings have it. It costs the operator one field and buys [chapter 18](18-audit-logs-and-observability.md) an answer.

**`Group(name)`** decides which section of the admin listing the setting appears in. It defaults to the key's first segment, so `settings.Group("orders")` on `orders.late_after` is belt and braces; declare it anyway, because it is the thing that keeps `/ops/settings` navigable once an app has sixty keys.

The rest — `Describe`, `RestartRequired`, `OrgOverridable`, `OneOf`, `MaxLen`, `MaxItems`, `URL`, `Email`, `Validate` — are in the [settings guide](../guides/runtime-settings.md#declaring-settings). One of them is worth flagging here: `OrgOverridable()` lets each organisation have its own value within the same bounds, which for a multi-tenant app like Plateful is tempting for almost every setting. Resist it until a tenant actually asks. A setting with a per-organisation override is a setting with two answers, and every piece of code that reads it needs to be in an organisation's context to get the right one.

Keys are **public API**. `orders.late_after` is in the app's `api/surface.json`, in operators' scripts, in the history table and in a year of audit events. Add keys; never rename one.

## 3. Reading a setting

**What we're doing.** Using the value in a use case.

**Why.** The point of a runtime setting is that it is read late — not at startup, not once, but every time the decision is made.

**What the framework already gives us.** `Setting[T].Get(ctx)`.

**What we build ourselves.** Nothing. One call.

**How.** From `LateOrders`, the use case the sweep calls:

```go
cutoff := now.Add(-s.lateAfter.Get(ctx))
```

**What just happened.** `Get` read a value out of memory. **It never touches the database.** Every instance holds the whole set of settings in memory and `Get` is a map lookup behind an atomic pointer, so calling it in a loop, per row, per request is fine — there is no query to save by hoisting it into a variable, and hoisting it is how a long-running job ends up using a value that changed an hour ago.

`Get` returns the declared default when the setting was never changed, when it was reset, when a stored value no longer passes validation (bounds tightened since it was stored), and before the store has finished loading at startup. That last one is why a handle's `Get` is safe to call from anywhere: there is no window in which it fails.

Every handle satisfies `config.Value[T]`, so a use case takes `config.Value[int]` rather than `*settings.Setting[int]` and a test passes `config.Static(50)`. That is how the late sweep's test builds a service without an app:

```go
reg := settings.NewRegistry()
lateAfter := settings.Duration(reg, "orders.late_after", 30*time.Minute)
```

A registry that was never loaded from a database answers with the declared default, which is exactly what a worker built outside the app needs.

## 4. How a change reaches every instance

**What we're doing.** Following one `PUT` through the system.

**Why.** Because "changes without a deploy" is a promise with a mechanism behind it, and knowing the mechanism tells you the one way to break it.

**What the framework already gives us.** All of it:

```bash
curl -X PUT http://127.0.0.1:8080/ops/settings/orders.max_open_per_restaurant \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"value":80,"version":3,"reason":"the summer menu launch"}'
```

1. The value is **validated** against the declaration — type, range, custom checks. A bad one is 422 with a reason that never quotes the value.
2. The **version** must be the one the operator read, or the change is a 409. A never-changed setting is version 0.
3. The row is written, a **history row** is written with the old value, the new value, the reason, the actor and the request ID, and a **`settings.value.changed` audit event** is recorded with the actor's IP and user agent.
4. This instance applies it **immediately**. Every other instance is told through PostgreSQL `LISTEN`/`NOTIFY` on a dedicated connection and applies it **within moments**.
5. Every instance also **reloads everything every five minutes**, and after every reconnection of that listening connection. That resync is the safety net: a `NOTIFY` lost because an instance was reconnecting at that exact second costs you five minutes of staleness, not a permanently wrong value.

**What just happened.** Every instance of Plateful now allows eighty open orders per restaurant, and there is a row saying who did it, when, and why.

> **Don't do this.** Change the value in the database:
>
> ```sql
> UPDATE settings_values SET value = '80' WHERE key = 'orders.max_open_per_restaurant';
> ```
>
> It looks like it works. The row changes, and within five minutes every instance even picks it up, because of the resync. What you don't get: **no `NOTIFY`**, so nothing applies for up to five minutes while you conclude the setting is broken; **no version bump**, so the next operator's `PUT` with the version they read succeeds and silently discards your value; **no history row**, so `GET /ops/settings/{key}/history` says the setting still has its default; **no audit event**, so [chapter 18](18-audit-logs-and-observability.md) has no record that anything happened; and **no validation**, so a value outside the declared bounds is stored, ignored by `Get`, and reported as `invalid_stored_value` in a listing nobody is reading.

> **Do this instead.** Use `PUT /ops/settings/{key}`, or `Store.Set` in Go. If the problem is that nobody has an operator token at 3am, that is a problem to fix in your on-call runbook, not in psql.

## 5. Flags are boolean. Only boolean.

**What we're doing.** Declaring the two flags from step 1 properly, and being clear about the limit.

**Why.** Feature-flag products elsewhere have string variants, JSON payloads, multivariate experiments and targeting by country or plan. gorbital has none of that, and building on the assumption that it does is an expensive discovery.

**What the framework already gives us.** `flags.Bool` — and that is the entire list of declaration functions. A flag is on or off. There are no variants, no payloads and no attribute targeting. `DefaultOn()` makes a flag start life enabled with a default of true; without it a flag is off until an operator turns it on.

**What we build ourselves.** If you need three variants, you need three flags or a runtime setting with an `Enum`. If you need "10% of users in Germany", you need a percentage flag plus your own check on country, because a flag cannot see a country.

**How.** Plateful's two, from the declaration in step 1:

```go
scheduled = flags.Bool(r, "orders.scheduled_ordering",
    flags.Describe("Lets customers order for a later time. …"),
    flags.Group("orders"), flags.Client())

autoAssign = flags.Bool(r, "orders.courier_auto_assign",
    flags.Describe("Gives a ready order to a free courier automatically. …"),
    flags.Group("orders"))
```

**What just happened.** The difference between them is one option, and it is the most consequential option flags have.

`flags.Client()` puts the flag in `GET /v1/flags`, which any signed-in caller may read:

```json
{"flags": {"orders.scheduled_ordering": false}}
```

The customer app reads it after sign-in and when switching organisation, and shows or hides the "order for later" control. `GET /v1/orgs/{orgId}/flags` returns the same flags evaluated *as the organisation*, for any member.

`orders.courier_auto_assign` has no `Client()`, so it never appears there. That is deliberate and it is a security property, not tidiness: a flag key is the name of an unreleased feature, and a client flag list is a list of what you are about to ship, readable by every account on the platform. Keys go in `GET /v1/flags` only when a client genuinely has to draw something differently.

One rule follows: a client flag must never be the only thing standing between a caller and a feature. `orders.scheduled_ordering` hides a control *and* the API refuses a `scheduled_for` while the flag is off, so a client that ignores the flag gets the same answer as one that reads it. Flags are not access control.

## 6. Targeting, rollout, and the sharp edge

**What we're doing.** Turning `orders.courier_auto_assign` on for one restaurant.

**Why.** "Turn it on for everyone and watch" is how you find out that auto-assignment misbehaves during a dinner rush at three hundred restaurants simultaneously.

**What the framework already gives us.** Five targeting rules, applied in order; the first one that applies decides:

| # | Rule | Applies when | Answer |
|---|---|---|---|
| 1 | `enabled` is false | Always | `false` — a kill switch that keeps the rest of the state for later |
| 2 | `orgs.deny`, then `orgs.allow` | The caller acts in an organisation and it is listed | `false`, or `true` |
| 3 | `users.deny`, then `users.allow` | The caller is signed in and its ID is listed | `false`, or `true` |
| 4 | `percentage` | A percentage is set | `true` when the subject's bucket is below it |
| 5 | `default` | Otherwise | The default |

**What we build ourselves.** The rollout, as `PUT /ops/flags/{key}` calls. Each one sends the whole state, the version read, and a reason:

```bash
flag() {
  curl -X PUT http://127.0.0.1:8080/ops/flags/orders.courier_auto_assign \
    -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d "$1"
}
# 1. One restaurant that agreed to try it.
flag '{"version":0,"reason":"pilot with Trattoria Bruno","state":{"enabled":true,"default":false,"orgs":{"allow":["org_bruno"]}}}'
# 2. A tenth of the platform, the pilot still in.
flag '{"version":1,"reason":"10% rollout","state":{"enabled":true,"default":false,"orgs":{"allow":["org_bruno"]},"percentage":10}}'
# 3. Everyone.
flag '{"version":2,"reason":"released","state":{"enabled":true,"default":true}}'
# Something is wrong: off, keeping the state for when it is fixed.
flag '{"version":3,"reason":"incident 42","state":{"enabled":false,"default":true}}'
```

**What just happened.** Step 2 is where the sharp edge lives, and it is worth slowing down for.

**The bucket subject is the organisation when the caller acts in one, and the actor's own ID otherwise.** `flags.Bucket(key, subject)` is the first eight bytes of SHA-256 of the key, a zero byte and the subject, modulo 100 — stable across instances, stable over time, and different per flag so one unlucky organisation doesn't get every experiment at once.

For `orders.courier_auto_assign` that is exactly right, and it is why the flag's description says so. A restaurant's staff accept and advance orders inside their organisation, so the subject is the restaurant, and `percentage: 10` moves *whole restaurants*. Without that, two orders in the same kitchen five minutes apart could be bucketed by two different members of staff and get different answers — auto-assignment that flickers, which is worse than either state.

And it has a consequence that catches people:

> **Anonymous callers have no subject at all.** For a caller who is not signed in, `percentage: 0` and `percentage: 100` still work — nothing and everything. **Any other percentage does not apply to them**, and they fall through to `default`.

So a flag controlling something a stranger can see — like the public review list in [chapter 16](16-reviews-and-the-public-route.md) — cannot be rolled out to "10% of visitors". It is on for all of them or off for all of them. If you need a gradual rollout of anonymous-facing behaviour, the subject has to come from somewhere you choose (and store, and explain), not from a flag.

Bounds on the lists: at most 1,000 IDs each, an ID can't be in both the allow and deny list of the same rule, and a percentage is 0 to 100. For more targets than that, use a percentage or your own data.

## 7. `Enabled` or `Evaluate`

**What we're doing.** Reading the flag in a use case.

**Why.** There are two calls and the second one exists for a specific reason.

**What the framework already gives us.** `Enabled(ctx)` returns a `bool`, reads memory and the actor in the context, takes about 140 nanoseconds and allocates nothing. `Evaluate(ctx)` returns the same answer *and the rule that produced it* — `disabled`, `org_denied`, `org_allowed`, `user_denied`, `user_allowed`, `rollout` or `default`.

**What we build ourselves.** The choice between them. Plateful's auto-assignment uses `Evaluate`:

<!-- include examples/apps/plateful/internal/modules/orders/usecase/assign_courier.go#auto-assign-flag -->

**What just happened.** When no courier is assigned, the log line says *why* the flag said no. During a rollout those are two completely different problems: "the flag is off for everyone" means somebody forgot step 1, and "this restaurant is outside the rollout" means it is working exactly as intended. Without `Evaluate` they look identical from the outside, and the support ticket is "auto-assign is broken" either way.

Use `Enabled` when the answer is all you need. Use `Evaluate` when a "no" is something a human will one day have to explain.

A `*flags.Flag` is also a `config.Value[bool]`, so a module can take a flag as a live option without importing the flags package, and a test passes `config.Static(true)`.

## 8. Tests

**What we're doing.** Driving all four through `/ops`, the way an operator would.

**Why.** A setting that is never changed in a test is a setting whose bounds, whose reason requirement, and whose effect on behaviour are all untested. The interesting question is never "does `Get` return 50", it is "does the API behave differently after an operator changes it".

**What the framework already gives us.** [`gorbitaltest`](../guides/testing-with-gorbitaltest.md) gives a real app with a real settings store and a real flags store, and a client with any permissions you name:

```go
ops := app.As(gorbitaltest.User("usr_operator", "ops.settings.read", "ops.settings.write"))
```

**What we build ourselves.** Two helpers — `setSetting` and `setFlag` — that read the current version and `PUT` the new state with a reason, and then ordinary API assertions around them.

**How.** The settings and the client flag:

<!-- include examples/apps/plateful/internal/modules/orders/orders_test.go#test-settings-and-flags -->

Then the server-side flag, rolled out the way flags are actually rolled out — one organisation first:

<!-- include examples/apps/plateful/internal/modules/orders/orders_test.go#test-flag-targeting -->

And the smallest version of the same idea, from the restaurants module, where a setting's only job is to cap a number in a request body:

<!-- include examples/apps/plateful/internal/modules/restaurants/restaurants_test.go#test-radius-setting -->

**What just happened.** Three properties got locked down that no unit test would have caught. `orders.max_open_per_restaurant` set to 1 makes the *second* order 409 `restaurant_busy`. `GET /v1/flags` contains the client flag and, checked explicitly, does **not** contain the server-side one. And the org-targeted flag gives Bruno's restaurant a courier and Sara's none, from one rollout state, which is the whole behaviour of rule 2 in one assertion.

## Where to go next

- Every option, per-organisation settings and the Go API: [runtime settings guide](../guides/runtime-settings.md).
- Bucketing, precedence, storage and limitations in full: [feature flags guide](../guides/feature-flags.md).
- Every setting a Full app declares, with defaults and bounds: [settings reference](../reference/settings.md).
- The `/ops` endpoints: [ops API reference](../guides/ops-api.md#runtime-settings).
- Next chapter: [payments](15-payments-a-rule-across-modules.md), and a rule that has to live in two modules at once.
