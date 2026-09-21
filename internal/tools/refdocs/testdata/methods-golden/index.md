# Methods

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

Every exported constant, variable, function, type and method of the gorbital library, one page per package: its signature, its doc comment, runnable examples from the package's `Example` functions, and the release it arrived in. The pages are generated from the Go source, so they match `go doc` for the same version.

*Since* names the first release whose API listing (`api/*.txt`) has the identifier; `v0.3.0 (unreleased)` marks API added on this branch. What the tiers promise: [Stability](../guides/stability.md).

## Core

The root module, `gorbital.dev`: small packages every app uses, with no dependencies beyond the standard library, OpenTelemetry and `golang.org/x`.

| Package | Summary |
|---|---|
| [`gorbital.dev/widget`](widget.md) | Package widget makes widgets. |

## Modules

One Go module per directory under `modules/`, each added to an app on its own.

| Package | Summary |
|---|---|
| [`gorbital.dev/modules/gadget`](modules-gadget.md) | Package gadget makes gadgets. |
