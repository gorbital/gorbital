# 2. The framework and your app

[Chapter 1](01-create-the-app.md) generated an app you have not read yet. Before writing any of Plateful, it is worth knowing which half of that app is yours.

This is the one chapter with no new code in it. It is the mental model the rest of the guide assumes, and getting it wrong is what makes people fight a framework instead of using one.

## The two halves

```text
                    THE LIBRARY  ·  gorbital.dev
        ┌───────────────────────────────────────────────┐
        │  Sign-in: accounts, sessions, 2FA, passkeys,  │
        │  Google · Apple · GitHub, API keys            │
        │  Organisations: membership, roles, invites    │
        │  Routing, guards, the middleware stack        │
        │  Migrations · transactions · jobs · email     │
        │  Audit · observability · settings · flags     │
        │  The operations API at /ops                   │
        └───────────────────────────────────────────────┘
                              │
                      you name what you want
                              │
                              ▼
                    YOUR APP  ·  internal/modules
        ┌───────────────────────────────────────────────┐
        │  restaurants · menus · orders · couriers      │
        │  payments · reviews · images · notifications  │
        │                                               │
        │  Your tables, your SQL, your rules,           │
        │  your endpoints, your tests                   │
        └───────────────────────────────────────────────┘
```

The library holds what every product needs and nobody wants to write twice. Your app holds what makes it *your* product. Plateful is eight modules of restaurant business sitting on five lines of wiring for everything else.

## Where the two meet

There are exactly two places, and that is the whole coupling surface.

**`cmd/api/main.go`** names what the app is made of:

<!-- include examples/apps/plateful/cmd/api/main.go#main -->

Every line naming a library module — `opshttp`, `flagshttp`, `mailevents`, `orgshttp`, `authhttp` — is a feature you get without writing it. Every line under `modules.All()` is code you own.

**Each module's `module.go`** declares what that module contributes: its routes, the errors it maps to HTTP responses, the permissions it checks, the settings and flags it declares, the jobs it runs. [Chapter 5](05-the-restaurants-module.md) reads one in full.

That is it. No dependency-injection container, no registry to configure, no base classes. A module is a value; `main.go` is a list.

## Why the line falls where it does

Three questions decide whether something belongs in the library or in your app.

**Does every product need it, in the same shape?** Sessions expire. Passwords are hashed. A tenant's data must not leak to another tenant. Nobody's restaurant platform differentiates on how a session cookie is set, and getting it wrong is expensive. That belongs in the library.

**Does a mistake here become everybody's problem?** This is the strongest argument, and it is not theoretical. During v0.2's security review, `OPS_ALLOWED_IPS` turned out not to cover the `/ops` routes registered by sign-in — operations like banning a user or minting an API key answered from any address. One fix in the library, and every app gets it with `go get`. Had that code been generated into each app, it would be a changelog entry that a hundred teams have to notice and merge, and most would not.

**Is it your business?** An order may not be accepted until payment is authorised. A review is editable for twenty-four hours. A courier may advance only the order assigned to them. No framework can know these, and any framework that tried would be wrong for the next product. That is yours, and the library's job is to stay out of the way.

## What you can change, and how

"The library owns it" does not mean "you cannot touch it". There are four ways to bend the framework, and they are in order of cost — reach for the cheapest that works.

| You want to | Use | Cost |
|---|---|---|
| Change a value | A setting or a feature flag | None; changes at runtime with no deploy |
| Add behaviour at a known point | A hook — `OnRegister`, `BeforeLogin`, `AfterLogin`, `RegisterFields` | A few lines; keeps upgrading cleanly |
| Add a rule or a route of your own | A module, a `guard.New`, your own middleware | Ordinary app code |
| Own a built-in module outright | `orb eject auth` (or `flags`, `mailevents`, `ops`, `orgs`) | You own it forever, including its security fixes |

Plateful uses the first three for its own code. Sign-in and organisations are already the fourth: `orb new` generated them into `internal/modules/auth` and `internal/modules/orgs`, so you can read and change every line.

Owning code has one cost, and it is smaller than it sounds. What moved into your repository is the flows — handlers, use cases, repositories and migrations. The primitives they call stay in the library: password hashing, session tokens, TOTP, passkey and OAuth verification, and the organisation authorisation rules, so fixes to those still reach you with `go get`. A fix to a flow you own does not. `orb doctor` reports each module your app owns and warns when the library's version of it has changed, quoting the changelog, so you hear about a fix and apply it yourself. The other built-in modules — flags, email events and `/ops` — stay in the library, and `orb eject` copies any of them into your repository the same way if an option or hook can't express what you need.

## ❌ Don't do this

**Don't reimplement what the library already does.** Writing your own session table because you want a different expiry is a week of work and a new attack surface, when a setting would have done it.

**Don't put business logic in handlers.** A handler's job is to turn a request into a call and a result into a response. The rule — *may this order be accepted?* — belongs in the domain layer, where a job, a command and a test can all reach it. [Chapter 8](08-orders-rules-in-the-domain.md) shows the difference on real code.

**Don't reach around a guard.** If a route is protected by `guard.OrgMember`, do not also accept an `orgId` from the body and trust it. The guard is the declaration; the query is the enforcement; they must agree.

**Don't edit the library to change your app.** Every time you are tempted, there is an extension point. If there genuinely is not one, that is worth reporting — [chapter 22](22-where-to-go-from-here.md) lists the places where this guide found the framework wanting, and they came from exactly that feeling.

## ✅ Do this instead

**Start from what you get.** Before building anything, check whether the library already has it. Sign-in, organisations, audit, jobs, email, settings, flags and `/ops` are all there.

**Keep your business in `internal/modules`.** Four layers per module — `domain`, `usecase`, `repository`, `delivery` — one file per operation. [Chapter 5](05-the-restaurants-module.md) explains what each layer is for and why the split pays off.

**Let the declaration and the enforcement match.** A route says what it requires; the use case enforces it; a test proves it. [Chapter 10](10-who-may-see-this-row.md) is entirely about the case where this is hardest, and about a gap in the framework you need to know exists.

## What just happened

You have not written any code yet. What you have is the question to ask every time you are about to build something: *is this my business, or is this plumbing every product needs?*

Plumbing is already there — name it in `main.go`. Business is yours — it goes in `internal/modules`, and the rest of this guide is about writing it.

Next: [3. Configuration and the first run](03-configuration-and-first-run.md), which gets Plateful running before we add anything to it.
