# ADR-0092: What the framework owns

**Status:** Proposed (2026-09-21) · **Amends:** ADR-0015, ADR-0083 (ejection superseded in part) · **Related:** ADR-0088, ADR-0089

## Context

On 2026-09-18, after v0.2.0, the project decided that sign-in and organisations belong in the application's own repository, and `orb new --preset full` began running `orb eject` for the developer (v0.2.1). The reason was visibility: a developer should be able to read what registration, verification, login, password reset and an organisation's life actually do.

On 2026-09-21 that decision was restated as a product rule:

> I don't want auth and org managed internally in our framework — it's something the user can freely manage, all business logics. gorbital should help developers avoid boring security mistakes, but it must never lock them into one business model.

The rule contains two promises that pull against each other. *Avoid boring security mistakes* means some code must stay in a library, versioned, fixed once for everybody. *Never lock them into one business model* means some code must be the app's, editable without asking. This record draws the line, and removes a command that no longer makes sense once the line is drawn.

Facts checked on 2026-09-21 against `main` (`93040dff`, v0.2.1):

| Area | Today | Evidence |
|---|---|---|
| The command | `orb eject <module>`, one of auth, flags, mailevents, ops, orgs | `cli/internal/cli/cli.go:65`, `:157`, `eject.go` |
| What it can copy | five packages, all `gorbital.dev/gorbital/*http` | `ejectableModules`, `eject.go:85` |
| What it cannot copy | everything else, including `modules/auth` — by absence from that list, not by a rule | same |
| Copied by default | sign-in and organisations, at `orb new` | `Preset.Ejects`, `new_eject.go` |
| Opt-out | `orb new --no-eject` keeps them in the library | `new.go:55` |
| Other users of the copy planner | `orb add orgs`, `orb upgrade --layout v0.2`, `orb doctor` | `add_orgs_eject.go`, `upgrade_layout_plan.go`, `doctor_main.go`, `lock.go` |
| Provenance of a copy | module, package, library version, date, SHA-256, in `gorbital.lock` | `lock.go` |
| The word | 60 occurrences of "orb eject" / "ejected" across `docs/` | `grep -rn "orb eject" docs/` |

Two observations follow. First, `orb eject` now answers a question almost nobody has: the two modules with business logic in them are already in the app when it is created, so the only remaining users of the command are people undoing `--no-eject`, or taking `opshttp`, `flagshttp` or `mailevents`, none of which contain business rules. Second, the one package that must never be copied is kept out today by a list, not by a decision anyone wrote down.

## Options

### Where the line goes

| Option | Verdict |
|---|---|
| Two layers: library and app | Rejected: it puts password hashing and the invitation email on the same side of the line, whichever side that is |
| Everything in the app, including the crypto | Rejected: it is the one place where a small, invisible change is catastrophic, and where a fix must reach every app at once. It would break the first promise outright |
| Everything in the library, hooks only | Rejected: reversed on 2026-09-18, deliberately, for visibility. Not reopened |
| **Three layers: protocol and crypto (library, permanently) · flows (the app's) · policy (the app's, never in the library)** | **Chosen**: each promise maps onto one end, and the middle is where the 2026-09-18 decision already put things |

### What to do with `orb eject`

| Option | Verdict |
|---|---|
| Keep it, and add a rule that refuses layer 1 | Rejected: keeps a command whose remaining purpose is to undo a default, and keeps the word *ejected* in 60 documentation places for code that was never anywhere else |
| Remove the command and the copy machinery | Rejected: `orb new`, `orb add orgs` and `orb upgrade --layout v0.2` all run the planner |
| **Remove the command; keep the planner as internal machinery and rename its vocabulary from *eject* to *copy*** | **Chosen** |

### `orb new --no-eject`

| Option | Verdict |
|---|---|
| Keep it | Rejected: it is a second shape of every Full app — a second thing to document ("unless you passed `--no-eject`"), to test, and to support through upgrades |
| **Remove it** | **Chosen**: one shape. Sign-in and organisations are in the app whenever the profile has them |

## Decision

### 1. Three layers

| Layer | Owner | Contents | Moved into an app? |
|---|---|---|---|
| **1. Protocol and crypto** | the library, permanently | `gorbital.dev/modules/auth`: Argon2id parameters, session token generation and hashing, TOTP verification, API-key hashing, WebAuthn ceremonies, constant-time comparison, `Middleware`, `Catalog` | **Never** |
| **2. Flows** | the app | `gorbital/authhttp`, `gorbital/orgshttp`: registration, verification, password reset, login, account linking, invitations, member management, with their SQL, endpoints and migrations | Yes, by `orb new` |
| **3. Policy** | the app, only | role names, which role holds which permission, what a tenant is, who may join one, what happens on sign-up | Not in the library at all |

This becomes a tier table in ADR-0015 beside the existing Public / Experimental / Internal / Generated / Owned-scaffold tiers.

### 2. `orb eject` is removed

The command, its help line and its interactive picker go. For one release the name stays registered and exits non-zero with an explanation rather than "unknown command":

```text
orb eject was removed in v0.3.
Sign-in and organisations are already in your app, under internal/modules —
orb new put them there. There is nothing to eject.
```

### 3. The copy planner stays, internal

`lookupEjectable`, `planEjectFrom`, `ejectResult`, `ejectableModules` and the `//orb:noeject` source directive keep working, with their vocabulary renamed from *eject* to *copy*, for:

- `orb new` — writes sign-in and organisations into a new app;
- `orb add orgs` — adds a scope to an app created without one;
- `orb upgrade --layout v0.2` — moves a v0.1 app to the current layout;
- `orb doctor` — reads each copy's recorded source version from `gorbital.lock`.

What each of those copies is byte-for-byte what v0.2.1 copied. That equality is a release gate.

### 4. `--no-eject` is removed

### 5. `opshttp`, `flagshttp` and `mailevents` become library-only

They were copyable on demand and are not copied by default. With the command gone they stay in the library for good. They are plumbing — an operations API, a flags endpoint, a webhook receiver — not business rules, so nothing in the rule above asks for them to be the app's.

### 6. The word goes with the command

Code that `orb new` wrote is *the app's code*. It is not "ejected", because it was never anywhere else from the app's point of view. `docs/guides/ejecting-a-module.md` is replaced by `docs/guides/the-code-in-your-repo.md` — what `orb new` wrote, what stays in the library, and why — with a redirect from the old URL.

### 7. Copied code gets a fix path, not only a warning

Layer 2 is the app's, so a library fix does not reach it by `go get`. `orb doctor` already reports that the library's version of a copied module has changed and quotes the changelog. v0.3 adds `orb doctor --security`, which uses the source version and SHA-256 in `gorbital.lock` to show the upstream diff, name security-relevant changes by advisory ID and severity, and offer `orb upgrade --module <name> --only-security`.

This is what makes copying better than a fork: the copy knows where it came from.

## Consequences

- The first promise is structural, not a guideline: there is no command that moves layer 1 into an app.
- The second promise is testable: `byo-identity` (ADR-0088) is an application with a hand-written authenticator and a hand-written scope authorizer, none of `authhttp`, `orgshttp` or `modules/orgs`, in CI. If it stops building, the promise has stopped being true.
- One shape of Full app instead of two, in the templates, the tests, the upgrade path and the guides.
- Nobody can take `opshttp`, `flagshttp` or `mailevents` into their app any more. Accepted; reopen with an ADR if a real need appears.
- 60 documentation references and one guide change. ADR-0083's ejection section is superseded in part and gains a note.
- Removing a CLI command is a breaking change for anyone scripting it. In v0 this is allowed; the one-release explanatory error and a changelog entry are the mitigation.
