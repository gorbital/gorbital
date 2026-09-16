# Governance

How decisions are made in gorbital, who makes them, and how to take part. The reasons for this shape are in [ADR-0055](docs/adr/0055-governance-and-contribution.md).

## Roles

| Role | Who | Can |
|---|---|---|
| **Contributor** | Anyone who opens an issue, reviews, or sends a pull request | Propose changes and decision records; review any pull request |
| **Reviewer** | Contributors listed for an area in [`.github/CODEOWNERS`](.github/CODEOWNERS) | Approve pull requests in their area |
| **Maintainer** | Members of the `@gorbital/maintainers` team | Merge, release, accept or reject decision records, triage security reports, change this document |

Maintainers are listed in the `@gorbital/maintainers` team on GitHub. A contributor becomes a reviewer, and a reviewer a maintainer, when the existing maintainers agree (see *Decisions*) after a record of sustained, careful work in the area. Maintainers who are inactive for six months move to emeritus and can return by asking.

## Decisions

| Kind of change | How it's decided |
|---|---|
| Bug fixes, docs, tests, internal refactors | Pull request with one approving review from a reviewer or maintainer of the area |
| Anything that changes a public surface ([stability tiers](docs/adr/0015-public-api-and-stability-tiers.md)): exported Go API, error codes, audit actions, permissions, setting keys, job names, `/ops` responses, CLI commands, flags and `--json` output, file formats | A decision record (ADR) accepted before the code is merged |
| Security-sensitive code: `modules/auth`, `modules/orgs`, `httpx`, `ratelimit`, `modules/ratelimitpg`, the generator and `orb upgrade`, release workflows | Two approving reviews, at least one from a maintainer ([threat model row 7](docs/adr/0029-threat-model.md)) |
| Scope: what a milestone contains, new modules, new required services | A decision record and a roadmap change, accepted by the maintainers |
| This document and the licence | All active maintainers |

**Decision records.** Anyone can propose an ADR in a pull request with status **Proposed**. Maintainers decide by lazy consensus: a record is accepted when a maintainer approves it and no maintainer objects within five working days. An objection has to name what would change the objector's mind. If consensus fails, a simple majority of active maintainers decides, and the record says so.

**Breaking changes.** From 1.0 there are none within a major version ([ADR-0015](docs/adr/0015-public-api-and-stability-tiers.md), [ADR-0016](docs/adr/0016-scaffold-compatibility-and-upgrades.md)). The API listings, surface inventories and OpenAPI checks fail a pull request that removes something; overriding them needs an accepted ADR for the next major.

## Releases

- Minor and patch releases are cut by a maintainer from `main` when its checks pass, with a [changelog](CHANGELOG.md) entry and [upgrade notes](docs/guides/upgrade-notes.md).
- Releases are built and signed only in CI; tags are protected.
- Security fixes are released as patches for the supported versions in [SECURITY.md](SECURITY.md), after a coordinated disclosure.

## Security reports

Private, through GitHub's vulnerability reporting ([SECURITY.md](SECURITY.md)). Maintainers acknowledge within three business days, fix in a private fork, and credit the reporter unless asked not to.

## Code of conduct

Everyone follows the [code of conduct](CODE_OF_CONDUCT.md). Maintainers enforce it and can remove anyone, including a maintainer, who repeatedly breaks it.
