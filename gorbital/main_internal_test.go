package gorbital

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorbital.dev/config"
	"gorbital.dev/modules/postgres"
	"gorbital.dev/modules/postgres/pgtest"
)

// env returns a configuration source over vars, in development unless vars
// sets APP_ENV.
func env(vars map[string]string) config.Source {
	return config.Source{Getenv: func(k string) string {
		if v, ok := vars[k]; ok {
			return v
		}
		if k == "APP_ENV" {
			return "development"
		}
		return ""
	}}
}

// commandAuth is an authenticator contributing commands.
type commandAuth struct{ commands []Command }

func (commandAuth) Middleware(*slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler { return next }
}

func (a commandAuth) Commands() []Command { return a.commands }

func TestMainExitCodes(t *testing.T) {
	greet := Command{Name: "greet", Usage: "greet <name>   say hello", Run: func(_ context.Context, cfg Config, args []string, w io.Writer) error {
		if len(args) != 1 {
			return fmt.Errorf("%w: greet <name>", ErrUsage)
		}
		if args[0] == "fail" {
			return fmt.Errorf("the greeting failed")
		}
		fmt.Fprintf(w, "hello %s in %s\n", args[0], cfg.Env)
		return nil
	}}
	opts := []Option{WithName("shelfie"), WithAuth(commandAuth{commands: []Command{greet}})}
	tests := []struct {
		name       string
		args       []string
		vars       map[string]string
		opts       []Option
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{"help", []string{"help"}, nil, opts, 0, "greet <name>   say hello", ""},
		{"version", []string{"version"}, nil, opts, 0, "shelfie ", ""},
		{"version as JSON", []string{"version", "--json"}, nil, opts, 0, `"go_version"`, ""},
		{"openapi without a database or APP_ENV", []string{"openapi"}, map[string]string{"APP_ENV": ""}, opts, 0, `"openapi"`, ""},
		{"unknown command", []string{"frobnicate"}, nil, opts, 2, "", `shelfie: usage: unknown command "frobnicate"`},
		{"unknown flag", []string{"migrate", "--sideways"}, nil, opts, 2, "", "usage:"},
		{"extra argument", []string{"version", "now"}, nil, opts, 2, "", "usage:"},
		{"--json without --status", []string{"migrate", "--json"}, nil, opts, 2, "", "usage:"},
		{"invalid configuration", []string{"serve"}, map[string]string{"APP_ENV": "staging", "APP_ADDR": "8080"}, opts, 2, "", "APP_ADDR"},
		{"missing APP_ENV", nil, map[string]string{"APP_ENV": ""}, opts, 2, "", "APP_ENV is required"},
		{"serve without DATABASE_URL", nil, nil, opts, 2, "", "DATABASE_URL is required"},
		{"migrate without DATABASE_URL", []string{"migrate"}, nil, opts, 2, "", "DATABASE_URL is required"},
		{"migrate-down in production", []string{"migrate-down"}, map[string]string{"APP_ENV": "production", "MAIL_DELIVERY": "provider", "STORAGE_DRIVER": "r2", "STORAGE_ENDPOINT": "x", "STORAGE_BUCKET": "b", "STORAGE_ACCESS_KEY": "a", "STORAGE_SECRET_KEY": "s", "DATABASE_URL": "postgres://127.0.0.1:1/x"}, opts, 2, "", "development only"},
		{"database unreachable", []string{"serve"}, map[string]string{"DATABASE_URL": "postgres://nobody@127.0.0.1:1/none?connect_timeout=2", "LOG_ARCHIVE_DIR": t.TempDir(), "APP_LOG_LEVEL": "error"}, opts, 1, "", "shelfie: "},
		{"migration status as JSON reports problems", []string{"migrate", "--status", "--json"}, map[string]string{"APP_ENV": ""}, opts, 0, `"config_error":`, ""},
		{"migration status", []string{"migrate", "--status"}, nil, opts, 2, "", "DATABASE_URL is required"},
		{"contributed command", []string{"greet", "Ada"}, nil, opts, 0, "hello Ada in development", ""},
		{"contributed command misused", []string{"greet"}, nil, opts, 2, "", "usage: greet <name>"},
		{"contributed command failing", []string{"greet", "fail"}, nil, opts, 1, "", "the greeting failed"},
		{"contributed command with invalid configuration", []string{"greet", "Ada"}, map[string]string{"APP_ENV": ""}, opts, 2, "", "APP_ENV"},
		{"command named like a built-in", []string{"version"}, nil, []Option{WithAuth(commandAuth{commands: []Command{{Name: "migrate", Run: greet.Run}}})}, 2, "", `command "migrate" is built in`},
		{"command defined twice", []string{"version"}, nil, []Option{WithAuth(commandAuth{commands: []Command{greet, greet}})}, 2, "", `command "greet" is defined twice`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(context.Background(), tt.args, env(tt.vars), &stdout, &stderr, tt.opts)
			if code != tt.wantCode || !strings.Contains(stdout.String(), tt.wantStdout) || !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("run(%q) = %d\nstdout: %s\nstderr: %s\nwant %d, stdout containing %q, stderr containing %q",
					tt.args, code, stdout.String(), stderr.String(), tt.wantCode, tt.wantStdout, tt.wantStderr)
			}
		})
	}
}

// TestOpenAPIExportIsStable: the document needs no database and no
// environment, and is the same on every run.
func TestOpenAPIExportIsStable(t *testing.T) {
	opts := []Option{WithName("shelfie"), WithModules(Module{Name: "books", Routes: func(r *Router, d Deps) {
		if d.DB != nil {
			t.Error("modules get zero Deps when the document is exported")
		}
		Post(r, "/v1/books", func(context.Context, *struct{}) (*struct{}, error) { return nil, nil })
	}})}
	var first, second bytes.Buffer
	if code := run(context.Background(), []string{"openapi"}, env(nil), &first, io.Discard, opts); code != 0 {
		t.Fatalf("openapi = %d", code)
	}
	if code := run(context.Background(), []string{"openapi"}, config.OS, &second, io.Discard, opts); code != 0 {
		t.Fatalf("openapi = %d", code)
	}
	if first.String() != second.String() {
		t.Error("the OpenAPI document depends on the environment")
	}
	var doc struct {
		Info  struct{ Title string }
		Paths map[string]map[string]struct {
			Parameters []struct{ Name string }
			Security   []map[string][]string
		}
	}
	if err := json.Unmarshal(first.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	post := doc.Paths["/v1/books"]["post"]
	if doc.Info.Title != "shelfie" || len(post.Security) == 0 || len(post.Parameters) != 1 || post.Parameters[0].Name != "Idempotency-Key" || doc.Paths["/version"] == nil {
		t.Errorf("document = %s", first.String())
	}

	dir := t.TempDir()
	if code := run(context.Background(), []string{"openapi", "--dir", dir}, env(nil), io.Discard, os.Stderr, opts); code != 0 {
		t.Fatalf("openapi --dir = %d", code)
	}
	for _, name := range apiFileNames {
		if info, err := os.Stat(filepath.Join(dir, name)); err != nil || info.Size() == 0 {
			t.Errorf("openapi --dir didn't write %s: %v", name, err)
		}
	}
}

func TestMainMigratesAndReports(t *testing.T) {
	vars := map[string]string{"DATABASE_URL": pgtest.NewDatabase(t)}
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"migrate", "--status"}, env(vars), &stdout, &stderr, nil); code != 0 || !strings.Contains(stdout.String(), "pending") {
		t.Fatalf("migrate --status = %d %s %s", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	if code := run(context.Background(), []string{"migrate"}, env(vars), &stdout, &stderr, nil); code != 0 || !strings.Contains(stdout.String(), "applied migration") {
		t.Fatalf("migrate = %d %s %s", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	if code := run(context.Background(), []string{"migrate", "--status", "--json"}, env(vars), &stdout, &stderr, nil); code != 0 {
		t.Fatalf("migrate --status --json = %d %s", code, stderr.String())
	}
	var s migrationStatus
	if err := json.Unmarshal(stdout.Bytes(), &s); err != nil || s.Pending != 0 || s.Current != 20260918000061 || s.RowLevelSecurity != nil {
		t.Errorf("status = %s (%v), want every migration applied", stdout.String(), err)
	}

	// With row-level security on, the status reports what orb doctor shows,
	// as a v0.1 app's does: here, a superuser the policies don't apply to.
	pool, err := postgres.Open(context.Background(), config.NewSecret(vars["DATABASE_URL"]))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(context.Background(), `CREATE TABLE notes (id text PRIMARY KEY, org_id text NOT NULL);
		ALTER TABLE notes ENABLE ROW LEVEL SECURITY; ALTER TABLE notes FORCE ROW LEVEL SECURITY;
		CREATE POLICY org_isolation ON notes USING (org_id = current_setting('gorbital.org_id', true))`); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if code := run(context.Background(), []string{"migrate", "--status", "--json"}, env(vars), &stdout, &stderr, nil); code != 0 {
		t.Fatalf("migrate --status --json = %d %s", code, stderr.String())
	}
	s = migrationStatus{}
	if err := json.Unmarshal(stdout.Bytes(), &s); err != nil || len(s.RowLevelSecurity) != 1 || !strings.Contains(s.RowLevelSecurity[0], "BYPASSRLS") {
		t.Errorf("status with row-level security = %s (%v), want the bypassing role reported", stdout.String(), err)
	}
}
