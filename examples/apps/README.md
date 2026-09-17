# Example applications

Runnable applications for the documentation's **Examples** tab ([roadmap](../../docs/v0.2-roadmap.md#the-examples-tab)), such as Shelfie, the reading-tracker API. Every code block on an Examples page comes from an app here, so the text can't drift from code that compiles and passes its tests.

Unlike the golden apps next to this directory (`examples/minimal`, `examples/full-single`, `examples/full-multi`), which are the templates `orb new` renders, these apps are only documentation.

## Layout

One directory per app, each its own Go module:

```text
examples/apps/
└── shelfie/
    ├── go.mod        module example.com/shelfie
    ├── main.go
    └── ...
```

The `go.mod` uses the library in this checkout through `replace` directives, like the golden apps (see `examples/full-single/go.mod`). Require the published version and replace every `gorbital.dev` module the app uses, with paths relative to the app:

```text
require (
	gorbital.dev v0.1.0
	gorbital.dev/modules/postgres v0.1.0
)

replace (
	gorbital.dev => ../../..
	gorbital.dev/modules/postgres => ../../../modules/postgres
)
```

Then `go mod tidy` in the app's directory.

## Including code in a page

Mark a region in any source file with a name that is unique in the app:

```go
// docs:start create-book
func (h *Handlers) CreateBook(ctx context.Context, in *CreateBookInput) (*BookOutput, error) {
	...
}
// docs:end create-book
```

The page includes the region by app, file and name; the lines between the markers are shown, without the markers. The docs build fails when a marker is missing, so renaming or deleting a region breaks the build instead of the page.

## Checks

CI's `example apps` job finds every `examples/apps/*/go.mod` and, in each app, runs:

```bash
gofmt -l .           # must print nothing
go vet ./...
go build ./...
go test -race ./...  # against Docker PostgreSQL and Mailpit, like the library
```

Run the same locally with the test database from [CONTRIBUTING.md](../../CONTRIBUTING.md#set-up) (`GORBITAL_TEST_DATABASE_URL` and the Mailpit variables). The job passes when there are no apps yet.
