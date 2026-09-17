package gorbital

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/actor"
	"gorbital.dev/gorbital/internal/builtinjobs"
	"gorbital.dev/gorbital/internal/route"
	"gorbital.dev/modules/settings"
	"gorbital.dev/ratelimit"
)

// notesModule declares a retention the retention job deletes, one its own
// job deletes, a named rate limiter, a route limited by a guard, and takes
// the Platform.
func notesModule(platform **Platform, deleted *[]string) Module {
	var keep *settings.Setting[time.Duration]
	limited := func(c *route.Config) {
		c.Guards = append(c.Guards, route.Guard{Name: "rate_limit", Limit: &route.Limit{
			Name: "notes_writes", Limit: ratelimit.Per(10, time.Minute), Keys: "actor",
			Key: func(context.Context, huma.Context) string { return "k" },
		}})
	}
	return Module{
		Name:         "notes",
		Settings:     func(r *settings.Registry) { keep = settings.Duration(r, "notes.retention", 24*time.Hour) },
		Jobs:         defineNotes,
		RateLimiters: []RateLimiter{{Name: "notes_imports", Keys: "user ID", Description: "Imports per user"}},
		Retention: func(Deps) []Retention {
			return []Retention{
				{Data: "notes", Setting: keep, Delete: func(_ context.Context, before time.Time, _ int) (int64, error) {
					*deleted = append(*deleted, "notes")
					return 0, nil
				}},
				{Data: "note_drafts", Setting: keep, Job: "notes_digest"},
			}
		},
		Platform: func(p *Platform) error { *platform = p; return nil },
		Routes: func(r *Router, _ Deps) {
			Post(r, "/v1/notes", noteHandler, limited)
			Put(r, "/v1/notes/{id}", noteHandler, limited)
		},
	}
}

func TestPlatformCollectsFromModules(t *testing.T) {
	var p *Platform
	var deleted []string
	a, err := testApp(t, nil, io.Discard, WithAuth(principalAuth{}), WithModules(notesModule(&p, &deleted)))
	if err != nil {
		t.Fatal(err)
	}
	if p == nil || p != a.platform || p.Jobs == nil || p.Health == nil || p.InstanceID == "" || p.Migrations == nil || p.MailSender.FromEmail == nil || p.Name == "" {
		t.Fatalf("Platform = %+v, want what New built", p)
	}

	var names []string
	for _, l := range p.RateLimiters() {
		names = append(names, l.Name)
	}
	if want := []string{"auth_ip", "notes_imports", "notes_writes"}; !slices.Equal(names, want) {
		t.Errorf("RateLimiters() = %v, want %v", names, want)
	}
	if l := p.RateLimiters()[2]; l.Keys != "actor" || l.Description != "guard.RateLimit on POST /v1/notes, PUT /v1/notes/{id}" {
		t.Errorf("guard limiter = %+v", l)
	}

	var data []string
	for _, r := range p.Retention() {
		data = append(data, r.Data)
	}
	want := []string{"audit_events", "settings_history", "flags_history", "job_definition_history", "observability_minutes", "idempotency_keys", "release_instances", "notes", "note_drafts"}
	if !slices.Equal(data, want) {
		t.Errorf("Retention() = %v, want %v", data, want)
	}
	var targets []string
	for _, target := range retentionTargets(p.Retention()) {
		targets = append(targets, target.Name)
	}
	if want := []string{"audit_events", "settings_history", "flags_history", "job_definition_history", "notes"}; !slices.Equal(targets, want) {
		t.Errorf("retention job targets = %v, want %v", targets, want)
	}
	if def, err := p.Jobs.Definition(context.Background(), builtinjobs.Retention); err != nil || def.Name != builtinjobs.Retention {
		t.Errorf("retention job = %+v, %v", def, err)
	}
}

func TestPlatformDeclarationErrors(t *testing.T) {
	var setting *settings.Setting[time.Duration]
	withSetting := func(m Module) Module {
		m.Settings = func(r *settings.Registry) { setting = settings.Duration(r, m.Name+".retention", time.Hour) }
		return m
	}
	del := func(context.Context, time.Time, int) (int64, error) { return 0, nil }
	tests := []struct {
		name    string
		modules []Module
		config  bool
		want    string
	}{
		{"rate limiter twice", []Module{
			{Name: "notes", RateLimiters: []RateLimiter{{Name: "imports"}}},
			{Name: "digests", RateLimiters: []RateLimiter{{Name: "imports"}}},
		}, false, `rate limiter "imports" is declared by modules "notes" and "digests"`},
		{"built-in rate limiter", []Module{{Name: "notes", RateLimiters: []RateLimiter{{Name: "auth_ip"}}}}, false, `rate limiter "auth_ip" is declared by modules "gorbital" and "notes"`},
		{"rate limiter without a name", []Module{{Name: "notes", RateLimiters: []RateLimiter{{Keys: "x"}}}}, false, `module "notes" declares a rate limiter without a name`},
		{"guard named like a declared limiter", []Module{{Name: "notes", RateLimiters: []RateLimiter{{Name: "notes_writes"}}, Routes: func(r *Router, _ Deps) {
			Post(r, "/v1/notes", noteHandler, func(c *route.Config) {
				c.Public = true
				c.Guards = append(c.Guards, route.Guard{Name: "rate_limit", Limit: &route.Limit{Name: "notes_writes", Limit: ratelimit.Per(1, time.Minute), Key: func(context.Context, huma.Context) string { return "k" }}})
			})
		}}}, false, `rate limiter "notes_writes" is declared by a module and used by guard.RateLimit on POST /v1/notes`},
		{"retention without a deleter", []Module{withSetting(Module{Name: "notes", Retention: func(Deps) []Retention {
			return []Retention{{Data: "notes", Setting: setting}}
		}})}, false, `module "notes": retention "notes": set exactly one of Delete, Job and EnforcedBy`},
		{"retention with two deleters", []Module{withSetting(Module{Name: "notes", Retention: func(Deps) []Retention {
			return []Retention{{Data: "notes", Setting: setting, Delete: del, EnforcedBy: "a person"}}
		}})}, false, `set exactly one of Delete, Job and EnforcedBy`},
		{"retention without a setting", []Module{{Name: "notes", Retention: func(Deps) []Retention {
			return []Retention{{Data: "notes", Delete: del}}
		}}}, false, `retention "notes": the setting is nil`},
		{"built-in retention", []Module{withSetting(Module{Name: "notes", Retention: func(Deps) []Retention {
			return []Retention{{Data: "audit_events", Setting: setting, Delete: del}}
		}})}, false, `retention of "audit_events" is declared by modules "gorbital" and "notes"`},
		{"retention naming an unknown job", []Module{withSetting(Module{Name: "notes", Retention: func(Deps) []Retention {
			return []Retention{{Data: "notes", Setting: setting, Job: "notes_cleanup"}}
		}})}, false, `retention of "notes" names the job "notes_cleanup", which no module defines`},
		{"platform error", []Module{{Name: "notes", Platform: func(*Platform) error { return errors.New("NOTES_KEY is invalid") }}}, true, `module "notes": NOTES_KEY is invalid`},
		{"platform panic", []Module{{Name: "notes", Platform: func(*Platform) error { panic("boom") }}}, false, `module "notes": platform: boom`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := testApp(t, nil, io.Discard, WithModules(tt.modules...))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("New() error = %v, want %q", err, tt.want)
			}
			if errors.Is(err, errInvalidConfig) != tt.config {
				t.Errorf("New() error is a configuration error: %t, want %t", errors.Is(err, errInvalidConfig), tt.config)
			}
		})
	}
}

func TestPlatformAuthenticate(t *testing.T) {
	var p *Platform
	var deleted []string
	if _, err := testApp(t, nil, io.Discard, WithAuth(principalAuth{}), WithModules(notesModule(&p, &deleted))); err != nil {
		t.Fatal(err)
	}
	// An actor already in the context doesn't carry over: only the
	// request's credentials count.
	ctx := actor.With(context.Background(), actor.Actor{Kind: actor.KindUser, ID: "usr_old"})
	req := httptest.NewRequest(http.MethodGet, "/ops/observability/stream", nil)
	if a, ok := p.Authenticate(ctx, req); ok || a.ID != "" {
		t.Errorf("Authenticate() without credentials = %+v, %t; want no actor", a, ok)
	}
	req.Header.Set("Authorization", "Bearer token")
	if a, ok := p.Authenticate(ctx, req); !ok || a.ID != "usr_1" {
		t.Errorf("Authenticate() with credentials = %+v, %t; want usr_1", a, ok)
	}

	// Without an authenticator nobody is authenticated.
	var bare *Platform
	if _, err := testApp(t, nil, io.Discard, WithModules(notesModule(&bare, &deleted))); err != nil {
		t.Fatal(err)
	}
	if _, ok := bare.Authenticate(context.Background(), req); ok {
		t.Error("Authenticate() without an authenticator = true")
	}
}

func TestPlatformOnShutdown(t *testing.T) {
	var p *Platform
	var deleted []string
	a, err := testApp(t, nil, io.Discard, WithModules(notesModule(&p, &deleted)))
	if err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	p.OnShutdown(func() { close(closed) })
	p.OnShutdown(nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	time.Sleep(100 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() = %v", err)
	}
	select {
	case <-closed:
	default:
		t.Error("the shutdown hook didn't run")
	}
}

// TestDevOperatorOnOps: with the dev console on, its token acts on /ops/ as
// a system actor holding the platform_admin role's permissions, and nowhere
// else.
func TestDevOperatorOnOps(t *testing.T) {
	const token = "q3Jt0tBq0Xvqf7i5Tq1hYw2m9x8Zr4Kc6Lp2Nd5Vb3E"
	var seen []actor.Actor
	record := func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		seen = append(seen, actor.FromOrAnonymous(ctx))
		return nil, nil
	}
	ops := Module{
		Name:        "ops",
		Permissions: []Permission{{Name: "ops.settings.read", Roles: []string{"platform_admin"}}, {Name: "ops.jobs.read", Roles: []string{"ops_viewer"}}},
		Routes: func(r *Router, _ Deps) {
			Get(r, "/ops/settings", record)
			Get(r, "/v1/me", record)
		},
	}
	a, err := testApp(t, map[string]string{"APP_ENV": "development", "DEV_CONSOLE_TOKEN": token}, io.Discard, WithModules(ops))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a.Handler())
	defer srv.Close()
	get := func(path string) int {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if code := get("/ops/settings"); code != http.StatusNoContent || len(seen) != 1 || seen[0].ID != "dev-console" || seen[0].Kind != actor.KindSystem || !slices.Equal(seen[0].Permissions, []string{"ops.settings.read"}) {
		t.Errorf("GET /ops/settings with the console token = %d, actors %+v; want the operator with platform_admin's permissions", code, seen)
	}
	if code := get("/v1/me"); code != http.StatusUnauthorized {
		t.Errorf("GET /v1/me with the console token = %d, want 401", code)
	}
}
