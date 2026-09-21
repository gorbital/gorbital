# Writing an example

Notes for contributors writing a page in the Examples tab ([index.md](index.md)). The pages explain a real app; the app's code is the source of truth, and pages include it instead of copying it.

## Where things live

| What | Where |
|---|---|
| A Shelfie chapter | `docs/examples/shelfie/<nn>-<name>.md`, listed in the Examples tab of `docs/docs.json` |
| A recipe page | `docs/examples/recipes/<name>.md`, listed there too |
| Shelfie's code | `examples/shelfie/`, in this repository: it is also the golden output `orb gen module` is compared against, so it stays here (ADR-0093) |
| Any other app's code | `<app>/` in <https://github.com/gorbital/examples>, at the ref `docs/examples.json` pins; a page still writes the path as `examples/apps/<app>/…` |

Each phase that ships a feature adds or extends its chapter in the same pull request as the feature ([v0.2 roadmap](../v0.2-roadmap.md#what-every-phase-writes)).

## Including code

Mark the region in the app's source with a pair of comments naming it:

```go
// docs:start create-book
func (h handlers) createBook(ctx context.Context, in *createBookInput) (*bookOutput, error) {
	// ...
}
// docs:end create-book
```

On the page, put an HTML comment on its own line where the code goes. It holds the word `include`, the file path from the repository root, `#`, and the region name:

```text
<!-- include examples/shelfie/internal/modules/books/delivery/create_book.go#create-book -->
```

`#` and `--` comments mark regions too (`.env.example`, SQL, YAML), and without `#name` the whole file is included. The marker lines themselves aren't shown.

Two checks keep a page and its code together. In this repository, `go run -C internal/tools/docscheck .` (CI's "docs links, includes and docs/docs.json resolve" step) fails when a page includes a file or a region that doesn't exist. The docs site resolves the includes again when it syncs the docs (`pnpm sync-docs` in gorbital-web) and fails its build for the same reason, so a renamed function breaks a build rather than leaving stale code on a page.

Rules:

- Region names are unique within a file, and a `docs:start` always has a matching `docs:end`.
- Keep regions small: one function, type or block the text talks about. Include the same region twice rather than widening it.
- Never paste code a page could include. Short commands (`orb new shelfie`) and requests (`curl …`) are written inline.
- Show output a reader can check (a response body, a log line) as written text next to the command.

## Writing a chapter

- Start from what the reader wants to do in the app ("only club members can see a club's shelves"), then build it in the order they would.
- Every chapter ends with the app building and its tests passing: the chapter's tests are part of the example.
- Link to [Methods](../methods/index.md) wherever a library function or type is named, and to the guide that explains the concept.
- Keep to the documentation style in [CONTRIBUTING.md](../../CONTRIBUTING.md#style): plain, direct, the command shown.
