# Examples

Real applications built with gorbital, explained step by step. Each chapter adds one feature to a working app, and the code on every page is included from that app's source, which CI builds and tests, so the text can't drift from code that compiles.

> [!NOTE]
> The examples arrive with the v0.2 phases that ship the features they use ([v0.2 roadmap](../v0.2-roadmap.md#the-examples-tab)). Chapters 0, 1 and 4 of Shelfie are written; the tables below say which phase brings the others.

## Shelfie

Shelfie is a reading-tracker API for a web and a mobile app: people keep a shelf of books, track what they're reading, and share shelves in book clubs. It starts as an empty project and grows chapter by chapter into an app you could deploy. Its code is in `examples/apps/shelfie/`.

| Chapter | Shows | Arrives with |
|---|---|---|
| [0. Start a project](shelfie/00-start-a-project.md) | `main.go` with `gorbital.Main`, migrations, the module list, `.env`, `orb dev` | Phase 3 |
| [1. A books module](shelfie/01-books-module.md) | A migration, `Module`, the four layers with one file per operation, the route table, error mappings | Phases 1 and 3 |
| 2. Protecting routes | Deny by default, `guard.Permission`, `guard.RateLimit`, `guard.RecentReauth`, `guard.Public` | Phase 2 |
| 3. Your own middleware and guards | `requireClientVersion`, context values, `httpx.Capture`, a subscription guard with `guard.New` | Phase 2 |
| [4. Tests](shelfie/04-tests.md) | `gorbitaltest`: signed-in requests, problem assertions, queued mail and jobs | Phase 3 |
| 5. Operations | `/ops`, runtime settings and flags declared by a module | Phase 4 |
| 6. Accounts | `authhttp` options, `BeforeLogin`/`AfterLogin`/`OnRegister`, extra registration fields | Phase 6 |
| 7. Phone-code sign-in | A custom sign-in method through `Deps.Auth.SignIn` | Phase 6 |
| 8. Book clubs | Organisations, `guard.OrgMember`, row-level security | Phase 7 |
| 9. Generators | `orb gen module`, `orb gen middleware`, `orb routes` | Phase 8 |
| 10. Hardening and partners | Timeouts, `/ops` IP allow list, verifying partner webhooks | Phase 10 |
| 11. Deploy | Production configuration, migrations in CI, health checks | Phase 11 |

Chapters are numbered in reading order, not in the order they're written: chapters 2 and 3 follow once sign-in can be shown end to end.

## Recipes

Each recipe is a small runnable app that solves one problem end to end.

| Recipe | Shows | Arrives with |
|---|---|---|
| Multi-tenant invoicing | Organisations, tenancy and row-level security for billing data | Phase 7 |
| Internal admin tool | `/ops`, runtime settings and flags, restricting who can reach them | Phases 4 and 10 |
| Mobile backend with an external identity provider | Signing in with JWTs from another provider | Phase 10 |
| Receiving payment webhooks | Signature verification, idempotency, `InsertTx` | Phase 10 |
| Upgrading a v0.1 app | Moving a generated v0.1 app onto the v0.2 library | Phase 9 |

## Related

- [Methods](../methods/index.md): every exported function, type and method of the library, with examples.
- [Quickstart](../start/quickstart.md): create and run an app today, with `v0.1.0`.
