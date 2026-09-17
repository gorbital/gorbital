# Examples

Real applications built with gorbital, explained step by step. Each chapter adds one feature to a working app, and the code on every page is included from that app's source, which CI builds and tests, so the text can't drift from code that compiles.

> [!NOTE]
> The examples arrive with the v0.2 phases that ship the features they use ([v0.2 roadmap](../v0.2-roadmap.md#the-examples-tab)). Chapters 0, 1, 4 and 5 of Shelfie, and part 1 of the *Internal admin tool* recipe, are written; the tables below say which phase brings the others.
> The examples arrive with the v0.2 phases that ship the features they use ([v0.2 roadmap](../v0.2-roadmap.md#the-examples-tab)). Chapters 0, 1, 4 and 9 of Shelfie are written; the tables below say which phase brings the others.
> The examples arrive with the v0.2 phases that ship the features they use ([v0.2 roadmap](../v0.2-roadmap.md#the-examples-tab)). Chapters 0, 1, 4, 5, 6 and 7 of Shelfie, and part 1 of the *Internal admin tool* recipe, are written; the tables below say which phase brings the others.

## Shelfie

Shelfie is a reading-tracker API for a web and a mobile app: people keep a shelf of books, track what they're reading, and share shelves in book clubs. It starts as an empty project and grows chapter by chapter into an app you could deploy. Its code is in `examples/apps/shelfie/`.

| Chapter | Shows | Arrives with |
|---|---|---|
| [0. Start a project](shelfie/00-start-a-project.md) | `main.go` with `gorbital.Main`, migrations, the module list, `.env`, `orb dev` | Phase 3 |
| [1. A books module](shelfie/01-books-module.md) | A migration, `Module`, the four layers with one file per operation, the route table, error mappings | Phases 1 and 3 |
| 2. Protecting routes | Deny by default, `guard.Permission`, `guard.RateLimit`, `guard.RecentReauth`, `guard.Public` | Phase 2 |
| 3. Your own middleware and guards | `requireClientVersion`, context values, `httpx.Capture`, a subscription guard with `guard.New` | Phase 2 |
| [4. Tests](shelfie/04-tests.md) | `gorbitaltest`: signed-in requests, problem assertions, queued mail and jobs | Phase 3 |
| [5. Operations](shelfie/05-operations.md) | `opshttp` and `flagshttp` in `main.go`, a runtime setting and a flag declared by a module, `OPS_ALLOWED_IPS` | Phase 4 |
| [6. Accounts](shelfie/06-accounts.md) | `authhttp` options, `RegisterFields` into a profiles module, `OnRegister` creating a default shelf, `BeforeLogin` refusing suspended readers | Phase 6 |
| [7. Phone-code sign-in](shelfie/07-phone-code-sign-in.md) | A sign-in method of the app's own through `Authenticator.SignIn`, with a fake SMS sender in tests | Phase 6 |
| 8. Book clubs | Organisations, `guard.OrgMember`, row-level security | Phase 7 |
| [9. Generators](shelfie/09-generators.md) | `orb gen module`, `orb gen middleware`, `orb routes`, `orb doctor` | Phase 8 |
| 10. Hardening and partners | Timeouts, `/ops` IP allow list, verifying partner webhooks | Phase 10 |
| 11. Deploy | Production configuration, migrations in CI, health checks | Phase 12 |

Chapters are numbered in reading order, not in the order they're written: chapters 2 and 3 follow once sign-in can be shown end to end.

## Recipes

Each recipe is a small runnable app that solves one problem end to end.

| Recipe | Shows | Arrives with |
|---|---|---|
| Multi-tenant invoicing | Organisations, tenancy and row-level security for billing data | Phase 7 |
| [Internal admin tool, part 1: operations](recipes/internal-admin-tool.md) | `/ops` in an app on `gorbital.Main`: a module's runtime setting, client flag, retention and named rate limiter; restricting `/ops` with `OPS_ALLOWED_IPS`. Part 2 (reviewing changes, further restrictions) comes later | Phase 4 (part 2: Phase 10) |
| Mobile backend with an external identity provider | Signing in with JWTs from another provider | Phase 10 |
| Receiving payment webhooks | Signature verification, idempotency, `InsertTx` | Phase 10 |
| Upgrading a v0.1 app | Moving a generated v0.1 app onto the v0.2 library | Phase 9 |

## Related

- [Methods](../methods/index.md): every exported function, type and method of the library, with examples.
- [Quickstart](../start/quickstart.md): create and run an app today, with `v0.1.0`.
