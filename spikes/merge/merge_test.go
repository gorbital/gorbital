package merge

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// appV1 is app.go as the base template renders it in "release 1".
const appV1 = `package app

import (
    "context"
    "net/http"

    "gorbital.dev/httpx"
    "gorbital.dev/obs"
)

func New(ctx context.Context, cfg Config) (*App, error) {
    tel, err := obs.Setup(ctx, cfg.Obs)
    if err != nil {
        return nil, err
    }

    //orb:anchor modules

    mux := http.NewServeMux()
    //orb:anchor routes

    srv := httpx.NewServer(cfg.HTTP, mux, tel)
    return &App{parts: []any{tel, srv}}, nil
}
`

// appV2 is the same template in "release 2": it adds security headers.
const appV2 = `package app

import (
    "context"
    "net/http"

    "gorbital.dev/httpx"
    "gorbital.dev/obs"
)

func New(ctx context.Context, cfg Config) (*App, error) {
    tel, err := obs.Setup(ctx, cfg.Obs)
    if err != nil {
        return nil, err
    }

    //orb:anchor modules

    mux := http.NewServeMux()
    //orb:anchor routes

    handler := httpx.SecureHeaders(mux)
    srv := httpx.NewServer(cfg.HTTP, handler, tel)
    return &App{parts: []any{tel, srv}}, nil
}
`

const (
	userRoute   = `mux.HandleFunc("GET /hello", hello)`
	userFunc    = "\nfunc hello(w http.ResponseWriter, r *http.Request) {\n    w.Write([]byte(\"hi\"))\n}\n"
	moduleLine  = `db := postgres.MustOpen(ctx, cfg.DB, tel)`
	templateNew = `handler := httpx.SecureHeaders(mux)`
)

func insert(t *testing.T, src, anchor, line string) string {
	t.Helper()
	out, err := InsertAfterAnchor([]byte(src), anchor, line)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestUpgrade(t *testing.T) {
	tests := []struct {
		name string
		// build returns base, ours, theirs
		build func(t *testing.T) (string, string, string)
		// want is the expected outcome; empty means "record, don't assert"
		want Outcome
		// mustContain lists lines that must survive (developer edits and template changes)
		mustContain []string
	}{
		{
			name: "untouched file takes template changes",
			build: func(t *testing.T) (string, string, string) {
				return appV1, appV1, appV2
			},
			want:        TakeTheirs,
			mustContain: []string{templateNew},
		},
		{
			name: "unchanged template keeps developer edits",
			build: func(t *testing.T) (string, string, string) {
				return appV1, insert(t, appV1, "routes", userRoute), appV1
			},
			want:        KeepOurs,
			mustContain: []string{userRoute},
		},
		{
			name: "edits in different regions merge cleanly",
			build: func(t *testing.T) (string, string, string) {
				ours := insert(t, appV1, "routes", userRoute) + userFunc
				return appV1, ours, appV2
			},
			want:        CleanMerge,
			mustContain: []string{userRoute, "func hello(", templateNew},
		},
		{
			name: "same line changed on both sides conflicts",
			build: func(t *testing.T) (string, string, string) {
				ours := strings.Replace(appV1,
					"srv := httpx.NewServer(cfg.HTTP, mux, tel)",
					"srv := httpx.NewServer(cfg.HTTP, mux, tel, httpx.WithGzip())", 1)
				return appV1, ours, appV2
			},
			want:        Conflict,
			mustContain: []string{"httpx.WithGzip()", templateNew},
		},
		{
			name: "module added by orb add survives when base and theirs replay the insert",
			build: func(t *testing.T) (string, string, string) {
				base := insert(t, appV1, "modules", moduleLine)
				theirs := insert(t, appV2, "modules", moduleLine)
				ours := insert(t, base, "routes", userRoute)
				return base, ours, theirs
			},
			want:        CleanMerge,
			mustContain: []string{moduleLine, userRoute, templateNew},
		},
		{
			name: "module added by orb add with a naive base (no replay)",
			build: func(t *testing.T) (string, string, string) {
				ours := insert(t, insert(t, appV1, "modules", moduleLine), "routes", userRoute)
				return appV1, ours, appV2
			},
			mustContain: []string{moduleLine, userRoute, templateNew},
		},
		{
			name: "developer edit directly next to the template change",
			build: func(t *testing.T) (string, string, string) {
				ours := strings.Replace(appV1,
					"\n    srv := httpx.NewServer",
					"\n    mux.Handle(\"GET /metrics\", metricsHandler)\n    srv := httpx.NewServer", 1)
				return appV1, ours, appV2
			},
			mustContain: []string{`mux.Handle("GET /metrics", metricsHandler)`, templateNew},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, ours, theirs := tt.build(t)
			res, err := Upgrade([]byte(base), []byte(ours), []byte(theirs))
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("outcome=%s conflicts=%d", res.Outcome, res.Conflicts)

			if tt.want != "" && res.Outcome != tt.want {
				t.Errorf("outcome = %s, want %s\n%s", res.Outcome, tt.want, res.Content)
			}

			got := string(res.Content)
			for _, line := range tt.mustContain {
				if n := strings.Count(got, line); n != 1 {
					t.Errorf("line %q appears %d times, want exactly 1\n%s", line, n, got)
				}
			}

			if res.Outcome == Conflict {
				if !strings.Contains(got, "<<<<<<< yours") || !strings.Contains(got, ">>>>>>> gorbital upgrade") {
					t.Errorf("conflict without labelled markers:\n%s", got)
				}
				t.Logf("conflict content:\n%s", got)
				return
			}
			if _, err := parser.ParseFile(token.NewFileSet(), "app.go", got, 0); err != nil {
				t.Errorf("merged file does not parse: %v\n%s", err, got)
			}
		})
	}
}
