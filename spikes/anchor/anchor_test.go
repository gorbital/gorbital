package anchor

import (
	"errors"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

const appGo = `package app

import (
	"context"
	"net/http"
)

// New builds the application.
func New(ctx context.Context, cfg Config) (*App, error) {
	tel, err := setupTelemetry(ctx, cfg)
	if err != nil {
		return nil, err
	}

	//orb:anchor modules

	mux := http.NewServeMux()
	return &App{tel: tel, mux: mux}, nil
}

// Keys shows the file may use generics.
func Keys[K comparable, V any](m map[K]V) []K { return nil }
`

const routesGo = `package app

func routes(mux *http.ServeMux, m Modules) {
	mux.Handle("GET /livez", livez())
	//orb:anchor routes
}
`

const emptyBlockGo = `package app

func debug(cfg Config) {
	if cfg.Debug {
		//orb:anchor debug
	}
}
`

const userEditedGo = `package app

func New(ctx context.Context, cfg Config) (*App, error) {
	// custom: warm the cache before modules start
	warmCache(ctx)

	//orb:anchor modules
	db := setupPostgres(ctx, cfg) // user note: tuned pool

	mux := http.NewServeMux() // keep default mux
	mux.Handle("GET /custom", custom())
	return &App{mux: mux}, nil
}
`

const stringOnlyGo = "package app\n\nvar tmpl = `\n//orb:anchor modules\n`\n\nfunc New() {\n}\n"

type impl struct {
	name string
	fn   func([]byte, string, string) ([]byte, Outcome, error)
}

var impls = []impl{{"text", InsertText}, {"dst", InsertDST}}

func TestInsert(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		anchor  string
		stmts   []string
		wantErr error
		inOrder []string // must appear in this order in the result
		exact   []string // must appear exactly once
		oneLine bool     // each insertion adds exactly one line
	}{
		{
			name: "empty anchor block", src: appGo, anchor: "modules",
			stmts:   []string{"db := setupPostgres(ctx, cfg)"},
			inOrder: []string{"//orb:anchor modules\n\tdb := setupPostgres(ctx, cfg)\n\n\tmux := http.NewServeMux()"},
			oneLine: true,
		},
		{
			name: "second module keeps order", src: appGo, anchor: "modules",
			stmts:   []string{"db := setupPostgres(ctx, cfg)", "users := setupUsers(db)"},
			inOrder: []string{"//orb:anchor modules\n\tdb := setupPostgres(ctx, cfg)\n\tusers := setupUsers(db)\n\n\tmux"},
			oneLine: true,
		},
		{
			name: "anchor at end of block", src: routesGo, anchor: "routes",
			stmts:   []string{"m.Auth.Routes(mux)"},
			inOrder: []string{"//orb:anchor routes\n\tm.Auth.Routes(mux)\n}"},
			oneLine: true,
		},
		{
			name: "anchor alone in nested block", src: emptyBlockGo, anchor: "debug",
			stmts:   []string{"enablePprof(cfg)"},
			inOrder: []string{"//orb:anchor debug\n\t\tenablePprof(cfg)\n\t}"},
			oneLine: true,
		},
		{
			name: "user edits and comments preserved", src: userEditedGo, anchor: "modules",
			stmts:   []string{"users := setupUsers(db)"},
			inOrder: []string{"// custom: warm the cache", "//orb:anchor modules\n\tdb := setupPostgres(ctx, cfg) // user note: tuned pool\n\tusers := setupUsers(db)\n\n\tmux := http.NewServeMux() // keep default mux"},
			exact:   []string{"// custom: warm the cache before modules start", "// user note: tuned pool", "// keep default mux", `mux.Handle("GET /custom", custom())`},
			oneLine: true,
		},
		{name: "missing anchor", src: appGo, anchor: "nope", stmts: []string{"x := 1"}, wantErr: ErrAnchorMissing},
		{name: "duplicate anchor", src: appGo + "\nfunc other() {\n\t//orb:anchor modules\n}\n", anchor: "modules", stmts: []string{"x := 1"}, wantErr: ErrAnchorDuplicate},
		{name: "anchor text only inside string", src: stringOnlyGo, anchor: "modules", stmts: []string{"x := 1"}, wantErr: ErrAnchorMissing},
		{name: "invalid statement", src: appGo, anchor: "modules", stmts: []string{"db := ("}, wantErr: ErrInvalidStatement},
		{
			name: "CRLF line endings", src: strings.ReplaceAll(appGo, "\n", "\r\n"), anchor: "modules",
			stmts: []string{"db := setupPostgres(ctx, cfg)"},
			exact: []string{"db := setupPostgres(ctx, cfg)"},
		},
	}

	for _, im := range impls {
		for _, tt := range tests {
			t.Run(im.name+"/"+tt.name, func(t *testing.T) {
				cur := []byte(tt.src)
				for _, stmt := range tt.stmts {
					out, outcome, err := im.fn(cur, tt.anchor, stmt)
					if tt.wantErr != nil {
						if !errors.Is(err, tt.wantErr) {
							t.Fatalf("%s(%q) error = %v, want %v", im.name, stmt, err, tt.wantErr)
						}
						return
					}
					if err != nil {
						t.Fatalf("%s(%q) error = %v", im.name, stmt, err)
					}
					if outcome != Inserted {
						t.Fatalf("%s(%q) outcome = %s, want inserted", im.name, stmt, outcome)
					}
					if tt.oneLine {
						if added := lineCount(out) - lineCount(cur); added != 1 {
							t.Errorf("%s(%q) added %d lines, want 1\n%s", im.name, stmt, added, out)
						}
						if !isSubsequence(lines(cur), lines(out)) {
							t.Errorf("%s(%q) changed or removed existing lines\n--- before\n%s\n--- after\n%s", im.name, stmt, cur, out)
						}
					}
					cur = out
				}
				got := string(cur)
				for _, want := range tt.inOrder {
					if !strings.Contains(got, want) {
						t.Errorf("%s result missing sequence %q\n%s", im.name, want, got)
					}
				}
				for _, want := range tt.exact {
					if n := strings.Count(got, want); n != 1 {
						t.Errorf("%s result has %q %d times, want 1\n%s", im.name, want, n, got)
					}
				}
				if _, err := parser.ParseFile(token.NewFileSet(), "", cur, 0); err != nil {
					t.Errorf("%s result does not parse: %v", im.name, err)
				}

				// Idempotency: re-applying the last statement changes nothing.
				last := tt.stmts[len(tt.stmts)-1]
				again, outcome, err := im.fn(cur, tt.anchor, last)
				if err != nil || outcome != AlreadyPresent || string(again) != got {
					t.Errorf("%s re-apply %q = (%s, %v), changed=%t; want already-present, unchanged", im.name, last, outcome, err, string(again) != got)
				}
			})
		}
	}
}

func lines(b []byte) []string { return strings.Split(strings.TrimRight(string(b), "\n"), "\n") }
func lineCount(b []byte) int  { return len(lines(b)) }

func isSubsequence(small, big []string) bool {
	i := 0
	for _, l := range big {
		if i < len(small) && strings.TrimRight(small[i], "\r") == strings.TrimRight(l, "\r") {
			i++
		}
	}
	return i == len(small)
}
