package portal

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"gorbital.dev/cli/internal/genplan"
)

const testToken = "portal-test-token-0123456789abcdefghijklmnop"

// fakeSupervisor records actions and serves a status.
type fakeSupervisor struct {
	mu      sync.Mutex
	status  AppStatus
	actions []string
	fail    bool
}

func (f *fakeSupervisor) Status() AppStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status
}

func (f *fakeSupervisor) record(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, name)
	if f.fail {
		return errors.New("the app is already " + name + "ing")
	}
	return nil
}

func (f *fakeSupervisor) Restart() error     { return f.record("restart") }
func (f *fakeSupervisor) Stop() error        { return f.record("stop") }
func (f *fakeSupervisor) Start() error       { return f.record("start") }
func (f *fakeSupervisor) Migrate() error     { return f.record("migrate") }
func (f *fakeSupervisor) MigrateDown() error { return f.record("migrate-down") }
func (f *fakeSupervisor) MigrateRedo() error { return f.record("migrate-redo") }

// newTestServer returns a portal over a fake app and its test server.
func newTestServer(t *testing.T, mutate func(*Config)) (*Server, *httptest.Server, *fakeSupervisor) {
	t.Helper()
	sup := &fakeSupervisor{status: AppStatus{State: StateRunning, Addr: "127.0.0.1:8080", URL: "http://127.0.0.1:8080", Console: true}}
	cfg := Config{
		Token:        testToken,
		Version:      "v0.1.0-test",
		Project:      Project{Name: "acme-api", Module: "example.com/acme-api", Preset: "full", Features: []string{"postgres"}, Dir: t.TempDir(), Database: true},
		Supervisor:   sup,
		Hub:          NewHubSize(10),
		ConsoleToken: "console-token-0123456789abcdefghijklmnopqrstuv",
		Links:        map[string]string{"api": "http://127.0.0.1:8080"},
		Generators: map[string]Generator{
			"migration": {
				Plan: func(_ context.Context, input json.RawMessage) (genplan.Plan, error) {
					var in struct{ Name string }
					_ = json.Unmarshal(input, &in)
					if in.Name == "" {
						return genplan.Plan{}, errors.New("missing migration name")
					}
					return genplan.Plan{Generator: "migration", Name: in.Name, Changes: []genplan.Change{{Path: "db/migrations/1_" + in.Name + ".sql", Kind: genplan.Create, Content: []byte("-- +goose Up\n")}}}, nil
				},
				Apply: func(_ context.Context, _ json.RawMessage, allowDirty bool) (genplan.Plan, error) {
					if !allowDirty {
						return genplan.Plan{}, errors.New("uncommitted changes")
					}
					return genplan.Plan{Generator: "migration", Name: "applied"}, nil
				},
			},
		},
	}
	if mutate != nil {
		mutate(&cfg)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	t.Cleanup(s.Close)
	return s, ts, sup
}

// call sends a request with the cookie unless noAuth, and the mutation
// header on non-GET requests.
func call(t *testing.T, ts *httptest.Server, method, path string, body string, mutate func(*http.Request)) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: CookieName, Value: testToken})
	if method != http.MethodGet {
		req.Header.Set(MutationHeader, "1")
	}
	if mutate != nil {
		mutate(req)
	}
	res, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

func decode[T any](t *testing.T, res *http.Response) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(res.Body).Decode(&v); err != nil {
		t.Fatalf("decode %s: %v", res.Request.URL, err)
	}
	return v
}

func TestNewChecksConfig(t *testing.T) {
	if _, err := New(Config{Token: "short", Supervisor: &fakeSupervisor{}, Hub: NewHub()}); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("New() with a short token = %v, want ErrInvalidToken", err)
	}
	if _, err := New(Config{Token: testToken}); err == nil {
		t.Error("New() without a supervisor succeeded")
	}
	for _, token := range []string{strings.Repeat("a", 32), strings.Repeat("~", 512)} {
		if err := CheckToken(token); err != nil {
			t.Errorf("CheckToken(%q) = %v", token, err)
		}
	}
	for _, token := range []string{strings.Repeat("a", 31), strings.Repeat("a", 513), strings.Repeat("a", 31) + " ", strings.Repeat("a", 31) + "é"} {
		if err := CheckToken(token); err == nil {
			t.Errorf("CheckToken(%q) accepted", token)
		}
	}
}

func TestGuardRefusesWhatItMust(t *testing.T) {
	_, ts, _ := newTestServer(t, nil)
	tests := []struct {
		name   string
		method string
		mutate func(*http.Request)
		status int
		code   string
	}{
		{"cookie", http.MethodGet, nil, http.StatusOK, ""},
		{"bearer", http.MethodGet, func(r *http.Request) {
			r.Header.Del("Cookie")
			r.Header.Set("Authorization", "Bearer "+testToken)
		}, http.StatusOK, ""},
		{"no token", http.MethodGet, func(r *http.Request) { r.Header.Del("Cookie") }, http.StatusUnauthorized, "unauthorized"},
		{"wrong cookie", http.MethodGet, func(r *http.Request) {
			r.Header.Del("Cookie")
			r.AddCookie(&http.Cookie{Name: CookieName, Value: "nope-" + testToken})
		}, http.StatusUnauthorized, "unauthorized"},
		{"token in query", http.MethodGet, func(r *http.Request) {
			r.Header.Del("Cookie")
			r.URL.RawQuery = "token=" + testToken
		}, http.StatusUnauthorized, "unauthorized"},
		{"rebound host", http.MethodGet, func(r *http.Request) { r.Host = "evil.example:3100" }, http.StatusForbidden, "forbidden"},
		{"localhost host", http.MethodGet, func(r *http.Request) { r.Host = "localhost:9" }, http.StatusOK, ""},
		{"ipv6 host", http.MethodGet, func(r *http.Request) { r.Host = "[::1]:3100" }, http.StatusOK, ""},
		{"post without header", http.MethodPost, func(r *http.Request) { r.Header.Del(MutationHeader) }, http.StatusForbidden, "forbidden"},
		{"post with header", http.MethodPost, nil, http.StatusAccepted, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := APIPrefix + "status"
			if tt.method == http.MethodPost {
				path = APIPrefix + "app/restart"
			}
			res := call(t, ts, tt.method, path, "", tt.mutate)
			if res.StatusCode != tt.status {
				body, _ := io.ReadAll(res.Body)
				t.Fatalf("status = %d, want %d: %s", res.StatusCode, tt.status, body)
			}
			if res.Header.Get("Access-Control-Allow-Origin") != "" {
				t.Error("CORS header present")
			}
			if tt.code != "" {
				p := decode[problem](t, res)
				if p.Code != tt.code {
					t.Errorf("code = %q, want %q", p.Code, tt.code)
				}
				if tt.status == http.StatusUnauthorized && !strings.Contains(res.Header.Get("WWW-Authenticate"), "Bearer") {
					t.Error("401 without WWW-Authenticate")
				}
			}
		})
	}
}

func TestGuardRefusesRemotePeers(t *testing.T) {
	s, _, _ := newTestServer(t, nil)
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:3100"+APIPrefix+"status", nil)
	req.RemoteAddr = "10.0.0.7:4242"
	req.AddCookie(&http.Cookie{Name: CookieName, Value: testToken})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d from a remote peer, want 403", rec.Code)
	}
	if !loopbackPeer("[::1]:55") || loopbackPeer("192.168.1.1:1") || loopbackPeer("bad") {
		t.Error("loopbackPeer")
	}
	for host, want := range map[string]bool{"localhost": true, "LOCALHOST:3100": true, "127.0.0.1": true, "[::1]": true, "[::1]:80": true,
		"127.0.0.1.evil.example": false, "localhost.evil.example:3100": false, "": false, "[::2]:1": false, "0.0.0.0:3100": false} {
		if got := loopbackHost(host); got != want {
			t.Errorf("loopbackHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestAuthLinkSetsCookie(t *testing.T) {
	_, ts, _ := newTestServer(t, nil)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Get(ts.URL + AuthPath + "?t=" + testToken)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/" {
		t.Fatalf("auth link = %d %q, want 303 to /", res.StatusCode, res.Header.Get("Location"))
	}
	var cookie *http.Cookie
	for _, c := range res.Cookies() {
		if c.Name == CookieName {
			cookie = c
		}
	}
	if cookie == nil || cookie.Value != testToken || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" {
		t.Fatalf("cookie = %+v, want the token, HttpOnly, SameSite=Strict, Path=/", cookie)
	}

	res, err = client.Get(ts.URL + AuthPath + "?t=wrong-" + testToken)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusForbidden || len(res.Cookies()) != 0 || !strings.Contains(string(body), "another") {
		t.Errorf("wrong token = %d, cookies %v, body %q", res.StatusCode, res.Cookies(), body)
	}
}

func TestStatusOutputAndActions(t *testing.T) {
	s, ts, sup := newTestServer(t, nil)
	status := decode[Status](t, call(t, ts, http.MethodGet, APIPrefix+"status", "", nil))
	if status.Portal.Version != "v0.1.0-test" || status.Portal.UI != "placeholder" || status.Project.Name != "acme-api" ||
		status.App.State != StateRunning || status.Links["api"] != "http://127.0.0.1:8080" || len(status.Generators) != 1 || status.Generators[0] != "migration" {
		t.Errorf("status = %+v", status)
	}

	w := s.cfg.Hub.Writer("app")
	_, _ = io.WriteString(w, "listening on :8080\npartial")
	_, _ = io.WriteString(w, " line\r\n")
	s.cfg.Hub.AddLine("orb", "orb: change detected")
	out := decode[OutputList](t, call(t, ts, http.MethodGet, APIPrefix+"output?limit=2", "", nil))
	if out.Max != 10 || len(out.Lines) != 2 || out.Lines[0].Text != "partial line" || out.Lines[0].Stream != "app" || out.Lines[1].Text != "orb: change detected" {
		t.Errorf("output = %+v", out)
	}
	if res := call(t, ts, http.MethodGet, APIPrefix+"output?limit=x", "", nil); res.StatusCode != http.StatusBadRequest {
		t.Errorf("bad limit = %d", res.StatusCode)
	}

	for _, action := range []string{"restart", "stop", "start", "migrate", "migrate-down", "migrate-redo"} {
		res := call(t, ts, http.MethodPost, APIPrefix+"app/"+action, "", nil)
		if res.StatusCode != http.StatusAccepted {
			t.Errorf("%s = %d", action, res.StatusCode)
		}
		if a := decode[Accepted](t, res); !a.Accepted || a.App.State != StateRunning {
			t.Errorf("%s answer = %+v", action, a)
		}
	}
	if strings.Join(sup.actions, ",") != "restart,stop,start,migrate,migrate-down,migrate-redo" {
		t.Errorf("actions = %q", sup.actions)
	}
	sup.fail = true
	if res := call(t, ts, http.MethodPost, APIPrefix+"app/stop", "", nil); res.StatusCode != http.StatusConflict {
		t.Errorf("failed action = %d, want 409", res.StatusCode)
	}
	if res := call(t, ts, http.MethodGet, APIPrefix+"nothing", "", nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("unknown endpoint = %d", res.StatusCode)
	}
	if res := call(t, ts, http.MethodGet, Prefix+"nothing", "", nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("unknown prefix path = %d", res.StatusCode)
	}
}

func TestEventsStream(t *testing.T) {
	s, ts, _ := newTestServer(t, func(c *Config) { c.MaxStreams = 1 })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+APIPrefix+"events", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: testToken})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("events = %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	reader := bufio.NewReader(res.Body)
	next := func() (string, string) {
		t.Helper()
		var event, data string
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			line = strings.TrimRight(line, "\n")
			switch {
			case strings.HasPrefix(line, "event: "):
				event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				data = strings.TrimPrefix(line, "data: ")
			case line == "" && event != "":
				return event, data
			}
		}
	}
	if event, data := next(); event != "state" || !strings.Contains(data, `"state":"running"`) {
		t.Fatalf("first event = %s %s, want the current state", event, data)
	}
	s.cfg.Hub.AddLine("app", "hello")
	if event, data := next(); event != "output" || !strings.Contains(data, `"text":"hello"`) {
		t.Errorf("output event = %s %s", event, data)
	}
	s.cfg.Hub.SetState(AppStatus{State: StateBuilding})
	if event, data := next(); event != "state" || !strings.Contains(data, `"state":"building"`) {
		t.Errorf("state event = %s %s", event, data)
	}

	// A second stream is over the limit.
	if res2 := call(t, ts, http.MethodGet, APIPrefix+"events", "", nil); res2.StatusCode != http.StatusTooManyRequests {
		t.Errorf("second stream = %d, want 429", res2.StatusCode)
	}

	s.Close()
	if event, data := next(); event != "end" || !strings.Contains(data, "shutdown") {
		t.Errorf("end event = %s %s", event, data)
	}
}

func TestProxyReachesTheApp(t *testing.T) {
	var got *http.Request
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Clone(context.Background())
		w.Header().Set("X-App", "yes")
		_, _ = io.WriteString(w, "app says "+r.URL.Path)
	}))
	defer app.Close()
	_, ts, sup := newTestServer(t, nil)
	sup.status.Addr = strings.TrimPrefix(app.URL, "http://")

	res := call(t, ts, http.MethodGet, AppPrefix+"_dev/routes?x=1", "", func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: "app_session", Value: "keep"})
	})
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || string(body) != "app says /_dev/routes" || res.Header.Get("X-App") != "yes" {
		t.Fatalf("proxied = %d %q", res.StatusCode, body)
	}
	if got.Host != sup.status.Addr || got.URL.RawQuery != "x=1" {
		t.Errorf("app saw Host %q query %q, want %q x=1", got.Host, got.URL.RawQuery, sup.status.Addr)
	}
	if got.Header.Get("Authorization") != "Bearer console-token-0123456789abcdefghijklmnopqrstuv" {
		t.Errorf("app saw Authorization %q, want the console token", got.Header.Get("Authorization"))
	}
	if got.Header.Get(MutationHeader) != "" {
		t.Error("the portal's mutation header reached the app")
	}
	cookies := got.Cookies()
	if len(cookies) != 1 || cookies[0].Name != "app_session" {
		t.Errorf("app saw cookies %v, want only app_session", cookies)
	}

	// Outside /_dev and /ops the token isn't added; a caller's own Authorization wins.
	call(t, ts, http.MethodGet, AppPrefix+"v1/ping", "", nil)
	if got.Header.Get("Authorization") != "" {
		t.Errorf("/v1/ping got Authorization %q", got.Header.Get("Authorization"))
	}
	call(t, ts, http.MethodGet, AppPrefix+"ops/settings", "", nil)
	if got.Header.Get("Authorization") != "Bearer console-token-0123456789abcdefghijklmnopqrstuv" {
		t.Errorf("/ops/settings got Authorization %q, want the console token", got.Header.Get("Authorization"))
	}
	call(t, ts, http.MethodGet, AppPrefix+"_dev/app", "", func(r *http.Request) { r.Header.Set("Authorization", "Bearer mine") })
	if got.Header.Get("Authorization") != "Bearer mine" {
		t.Errorf("own Authorization replaced: %q", got.Header.Get("Authorization"))
	}
	// The portal's own bearer token never reaches the app; the console token does.
	call(t, ts, http.MethodGet, AppPrefix+"_dev/app", "", func(r *http.Request) {
		r.Header.Del("Cookie")
		r.Header.Set("Authorization", "Bearer "+testToken)
	})
	if got.Header.Get("Authorization") != "Bearer console-token-0123456789abcdefghijklmnopqrstuv" {
		t.Errorf("portal bearer forwarded: %q", got.Header.Get("Authorization"))
	}
	call(t, ts, http.MethodGet, AppPrefix+"v1/ping", "", func(r *http.Request) {
		r.Header.Del("Cookie")
		r.Header.Set("Authorization", "Bearer "+testToken)
	})
	if got.Header.Get("Authorization") != "" {
		t.Errorf("portal bearer forwarded outside /_dev: %q", got.Header.Get("Authorization"))
	}

	// A stopped app answers 502 with a problem.
	app.Close()
	res = call(t, ts, http.MethodGet, AppPrefix+"v1/ping", "", nil)
	if p := decode[problem](t, res); res.StatusCode != http.StatusBadGateway || p.Code != "app_unavailable" {
		t.Errorf("stopped app = %d %+v", res.StatusCode, p)
	}
}

func TestProxyNormalisesWildcardAddr(t *testing.T) {
	s, _, sup := newTestServer(t, nil)
	for addr, want := range map[string]string{"0.0.0.0:8080": "127.0.0.1:8080", ":9000": "127.0.0.1:9000", "[::]:1": "127.0.0.1:1", "localhost:8080": "localhost:8080"} {
		sup.status.Addr = addr
		if got := s.appURL().Host; got != want {
			t.Errorf("appURL(%q) = %q, want %q", addr, got, want)
		}
	}
}

func TestGenerators(t *testing.T) {
	_, ts, _ := newTestServer(t, nil)
	res := call(t, ts, http.MethodPost, APIPrefix+"generators/migration/plan", `{"input":{"name":"add_phone"}}`, nil)
	out := decode[GeneratorResponse](t, res)
	if res.StatusCode != http.StatusOK || out.Applied || out.Plan.Name != "add_phone" || len(out.Plan.Changes) != 1 || string(out.Plan.Changes[0].Content) != "-- +goose Up\n" {
		t.Errorf("plan = %d %+v", res.StatusCode, out)
	}
	res = call(t, ts, http.MethodPost, APIPrefix+"generators/migration/plan", `{"input":{}}`, nil)
	if p := decode[problem](t, res); res.StatusCode != http.StatusUnprocessableEntity || p.Code != "generator_failed" || !strings.Contains(p.Detail, "missing") {
		t.Errorf("invalid input = %d %+v", res.StatusCode, p)
	}
	res = call(t, ts, http.MethodPost, APIPrefix+"generators/migration/apply", `{"input":{"name":"x"}}`, nil)
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("apply with a dirty tree = %d", res.StatusCode)
	}
	res = call(t, ts, http.MethodPost, APIPrefix+"generators/migration/apply", `{"input":{"name":"x"},"allow_dirty":true}`, nil)
	if out := decode[GeneratorResponse](t, res); res.StatusCode != http.StatusOK || !out.Applied || out.Plan.Name != "applied" {
		t.Errorf("apply = %d %+v", res.StatusCode, out)
	}
	res = call(t, ts, http.MethodPost, APIPrefix+"generators/nope/plan", `{}`, nil)
	if p := decode[problem](t, res); res.StatusCode != http.StatusNotFound || p.Code != "generator_not_found" {
		t.Errorf("unknown generator = %d %+v", res.StatusCode, p)
	}
	res = call(t, ts, http.MethodPost, APIPrefix+"generators/migration/plan", `{"input":`, nil)
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("bad JSON = %d", res.StatusCode)
	}
}

func TestStaticServesTheExport(t *testing.T) {
	ui := fstest.MapFS{
		"index.html":                  {Data: []byte("<html>home</html>")},
		"modules.html":                {Data: []byte("<html>modules</html>")},
		"jobs/index.html":             {Data: []byte("<html>jobs</html>")},
		"404.html":                    {Data: []byte("<html>lost</html>")},
		"_next/static/chunks/main.js": {Data: []byte("console.log(1)")},
		"favicon.svg":                 {Data: []byte("<svg/>")},
	}
	_, ts, _ := newTestServer(t, func(c *Config) { c.UI = ui })
	tests := []struct {
		path, want string
		status     int
		cache      string
	}{
		{"/", "home", http.StatusOK, "no-cache"},
		{"/modules", "modules", http.StatusOK, "no-cache"},
		{"/modules.html", "modules", http.StatusOK, "no-cache"},
		{"/jobs", "jobs", http.StatusOK, "no-cache"},
		{"/jobs/", "jobs", http.StatusOK, "no-cache"},
		{"/_next/static/chunks/main.js", "console.log(1)", http.StatusOK, "public, max-age=31536000, immutable"},
		{"/favicon.svg", "<svg/>", http.StatusOK, "no-cache"},
		{"/does-not-exist", "lost", http.StatusNotFound, "no-cache"},
		{"/../index.html", "home", http.StatusOK, "no-cache"},
	}
	for _, tt := range tests {
		res, err := http.Get(ts.URL + tt.path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != tt.status || !strings.Contains(string(body), tt.want) || res.Header.Get("Cache-Control") != tt.cache {
			t.Errorf("GET %s = %d %q cache %q; want %d containing %q cache %q", tt.path, res.StatusCode, body, res.Header.Get("Cache-Control"), tt.status, tt.want, tt.cache)
		}
	}
	status := decode[Status](t, call(t, ts, http.MethodGet, APIPrefix+"status", "", nil))
	if status.Portal.UI != "bundled" {
		t.Errorf("ui = %q, want bundled", status.Portal.UI)
	}
}

func TestStaticPlaceholderWithoutUI(t *testing.T) {
	_, ts, _ := newTestServer(t, func(c *Config) { c.UI = fstest.MapFS{"dist/.gitkeep": {}} })
	for _, path := range []string{"/", "/modules", "/anything/else"} {
		res, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK || !strings.Contains(string(body), "isn't bundled") || !strings.Contains(string(body), "sync-portal.sh") {
			t.Errorf("GET %s = %d %q", path, res.StatusCode, body)
		}
	}
}

func TestHubKeepsRecentLines(t *testing.T) {
	h := NewHubSize(3)
	h.now = func() time.Time { return time.Date(2026, 9, 16, 10, 0, 0, 0, time.UTC) }
	sub := h.Subscribe()
	for _, s := range []string{"a", "b", "c", "d"} {
		h.AddLine("app", s)
	}
	lines := h.Lines(0)
	if len(lines) != 3 || lines[0].Text != "b" || lines[2].Text != "d" || h.Capacity() != 3 {
		t.Errorf("Lines() = %+v", lines)
	}
	if got := h.Lines(1); len(got) != 1 || got[0].Text != "d" {
		t.Errorf("Lines(1) = %+v", got)
	}
	if len(sub.C) != 4 {
		t.Errorf("subscriber got %d events, want 4", len(sub.C))
	}
	h.Unsubscribe(sub)
	h.AddLine("app", "e")
	if len(sub.C) != 4 {
		t.Error("unsubscribed subscription still receives")
	}

	// A slow subscriber loses events and learns how many.
	slow := h.Subscribe()
	for range subscriberBuffer + 5 {
		h.AddLine("app", "x")
	}
	if n := h.takeDropped(slow); n != 5 {
		t.Errorf("dropped = %d, want 5", n)
	}
	if n := h.takeDropped(slow); n != 0 {
		t.Errorf("dropped again = %d, want 0", n)
	}

	// Long lines are cut.
	h.AddLine("app", strings.Repeat("y", maxLineBytes+10))
	if last := h.Lines(1)[0].Text; len(last) != maxLineBytes+len("…") {
		t.Errorf("long line kept %d bytes", len(last))
	}
}

func TestJobsListsTheAppsJobs(t *testing.T) {
	_, ts, _ := newTestServer(t, func(c *Config) {
		c.Jobs = func() ([]JobSource, error) {
			return []JobSource{{Name: "ping_hook", Ident: "PingHook", Package: "pinghook", Definition: "internal/app/job_ping_hook.go", Worker: "internal/jobs/pinghook/pinghook.go", Generated: true, Kind: "http", Form: json.RawMessage(`{"kind":"http"}`)}}, nil
		}
	})
	res := call(t, ts, http.MethodGet, APIPrefix+"jobs", "", nil)
	raw, _ := io.ReadAll(res.Body)
	body := string(raw)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, `"name":"ping_hook"`) || !strings.Contains(body, `"form":{"kind":"http"}`) || !strings.Contains(body, `"ejected":false`) {
		t.Errorf("jobs = %d %s", res.StatusCode, body)
	}

	// Without a source (an app without jobs) the list is empty, not null.
	_, ts, _ = newTestServer(t, nil)
	res = call(t, ts, http.MethodGet, APIPrefix+"jobs", "", nil)
	raw, _ = io.ReadAll(res.Body)
	if body := string(raw); res.StatusCode != http.StatusOK || body != `{"jobs":[]}`+"\n" {
		t.Errorf("jobs without a source = %d %q", res.StatusCode, body)
	}
}
