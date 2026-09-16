package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixtureV1 = `// Package limit is a fixture.
package limit

import (
	"context"
	"time"
)

// Burst is the default burst.
const Burst = 10

// Mode is a mode.
type Mode string

// ModeStrict is strict.
const ModeStrict Mode = "strict"

// ErrEmpty is returned for an empty key.
var ErrEmpty error

// Limit is a rate.
type Limit struct {
	PerSecond float64
	Burst     int
	hidden    bool
}

// Taker takes.
type Taker interface {
	Take(ctx context.Context, key string) (bool, error)
}

// Limiter limits.
type Limiter struct{ Limit }

// New returns a limiter.
func New(l Limit, opts ...func(*Limiter)) *Limiter { return &Limiter{Limit: l} }

// Take takes.
func (l *Limiter) Take(ctx context.Context, key string) (bool, error) { return true, nil }

// Per returns a limit.
func (Limit) Per(n int, window time.Duration) Limit { return Limit{} }

// Map maps.
func Map[K comparable, V any](m map[K]V, f func(key K) V) {}
`

// fixtureRepo writes a repository with a root module gorbital.dev and a
// module under modules/, both holding the fixture package, plus an internal
// package and a command that must not be listed.
func fixtureRepo(t *testing.T, src string) string {
	t.Helper()
	root := t.TempDir()
	for path, content := range map[string]string{
		"go.mod":                              "module gorbital.dev\n\ngo 1.25\n",
		"limit/limit.go":                      src,
		"internal/secret/secret.go":           "package secret\n\nfunc Exported() {}\n",
		"cmd/tool/main.go":                    "package main\n\nfunc Exported() {}\n\nfunc main() {}\n",
		"modules/extra/go.mod":                "module gorbital.dev/modules/extra\n\ngo 1.25\n",
		"modules/extra/extra.go":              "// Package extra is a fixture.\npackage extra\n\n// Name is a name.\nconst Name = \"extra\"\n",
		"modules/extra/testdata/skip/skip.go": "package skip\n\nfunc Exported() {}\n",
	} {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestListing(t *testing.T) {
	root := fixtureRepo(t, fixtureV1)
	var out bytes.Buffer
	if err := run(root, true, &out); err != nil {
		t.Fatalf("run -write: %v\n%s", err, out.String())
	}
	got, err := os.ReadFile(filepath.Join(root, "api", "gorbital.dev.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want := `pkg gorbital.dev/limit, const Burst = 10
pkg gorbital.dev/limit, const Burst ideal-int
pkg gorbital.dev/limit, const ModeStrict = "strict"
pkg gorbital.dev/limit, const ModeStrict Mode
pkg gorbital.dev/limit, func Map[K comparable, V any](map[K]V, func(K) V)
pkg gorbital.dev/limit, func New(Limit, ...func(*Limiter)) *Limiter
pkg gorbital.dev/limit, method (*Limiter) Take(context.Context, string) (bool, error)
pkg gorbital.dev/limit, method (Limit) Per(int, time.Duration) Limit
pkg gorbital.dev/limit, method (Limiter) Per(int, time.Duration) Limit
pkg gorbital.dev/limit, type Limit struct
pkg gorbital.dev/limit, type Limit struct, Burst int
pkg gorbital.dev/limit, type Limit struct, PerSecond float64
pkg gorbital.dev/limit, type Limiter struct
pkg gorbital.dev/limit, type Limiter struct, embedded Limit
pkg gorbital.dev/limit, type Mode string
pkg gorbital.dev/limit, type Taker interface { Take }
pkg gorbital.dev/limit, type Taker interface, Take(context.Context, string) (bool, error)
pkg gorbital.dev/limit, var ErrEmpty error
`
	if string(got) != want {
		t.Errorf("api/gorbital.dev.txt =\n%s\nwant\n%s", got, want)
	}
	if extra, err := os.ReadFile(filepath.Join(root, "api", "modules-extra.txt")); err != nil ||
		string(extra) != "pkg gorbital.dev/modules/extra, const Name = \"extra\"\npkg gorbital.dev/modules/extra, const Name ideal-string\n" {
		t.Errorf("api/modules-extra.txt = %q, %v", extra, err)
	}

	out.Reset()
	if err := run(root, false, &out); err != nil {
		t.Errorf("check after -write: %v\n%s", err, out.String())
	}
}

func TestCheckReportsRemovedChangedAndAddedLines(t *testing.T) {
	root := fixtureRepo(t, fixtureV1)
	var out bytes.Buffer
	if err := run(root, true, &out); err != nil {
		t.Fatalf("run -write: %v\n%s", err, out.String())
	}

	// A renamed parameter changes nothing; a changed result type, a removed
	// field, a method added to an interface and a new function do.
	v2 := strings.NewReplacer(
		"func New(l Limit, opts ...func(*Limiter)) *Limiter { return &Limiter{Limit: l} }", "func New(limit Limit, options ...func(*Limiter)) *Limiter { return &Limiter{Limit: limit} }",
		"func (Limit) Per(n int, window time.Duration) Limit { return Limit{} }", "func (Limit) Per(n int, window time.Duration) *Limit { return &Limit{} }",
		"\tBurst     int\n", "",
		"Take(ctx context.Context, key string) (bool, error)\n}", "Take(ctx context.Context, key string) (bool, error)\n\tReset()\n}",
	).Replace(fixtureV1) + "\n// Added is new.\nfunc Added() {}\n"
	if err := os.WriteFile(filepath.Join(root, "limit", "limit.go"), []byte(v2), 0o644); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	err := run(root, false, &out)
	if !errors.Is(err, errIncompatible) {
		t.Fatalf("run = %v, want errIncompatible\n%s", err, out.String())
	}
	for _, want := range []string{
		"removed or changed (breaking): pkg gorbital.dev/limit, method (Limit) Per(int, time.Duration) Limit",
		"removed or changed (breaking): pkg gorbital.dev/limit, type Limit struct, Burst int",
		"removed or changed (breaking): pkg gorbital.dev/limit, type Taker interface { Take }",
		"new, not recorded: pkg gorbital.dev/limit, func Added()",
		"new, not recorded: pkg gorbital.dev/limit, type Taker interface { Reset, Take }",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "func New") {
		t.Errorf("a renamed parameter was reported:\n%s", out.String())
	}
}
