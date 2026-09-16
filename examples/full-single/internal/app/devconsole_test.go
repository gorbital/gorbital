package app_test

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"gorbital.dev/config"

	"example.com/acme-api/internal/app"
)

// devToken is a dev console token as orb dev generates them: 256 bits,
// base64url.
const devToken = "q3Jt0tBq0Xvqf7i5Tq1hYw2m9x8Zr4Kc6Lp2Nd5Vb3E"

// devServer serves an app with the dev console on over a real loopback
// listener and returns the app and its URL.
func devServer(t *testing.T, env map[string]string) (*app.App, string) {
	t.Helper()
	full := map[string]string{"DEV_CONSOLE_TOKEN": devToken}
	for k, v := range env {
		full[k] = v
	}
	a := newApp(t, full)
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(srv.Close)
	return a, srv.URL
}

// devGet requests path with the console token; headers are name/value
// pairs, a "Host" pair sets the Host header, and an empty Authorization
// sends none.
func devGet(t *testing.T, base, path string, headers ...string) (int, http.Header, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+devToken)
	for i := 0; i+1 < len(headers); i += 2 {
		switch {
		case headers[i] == "Host":
			req.Host = headers[i+1]
		case headers[i] == "Authorization" && headers[i+1] == "":
			req.Header.Del("Authorization")
		default:
			req.Header.Set(headers[i], headers[i+1])
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, string(body)
}

func TestDevConsole(t *testing.T) {
	const querySecret, headerSecret = "query-secret-4f1e", "header-secret-9a2c"
	_, base := devServer(t, nil)

	// A live stream sees requests as they finish.
	req, _ := http.NewRequest(http.MethodGet, base+"/_dev/requests/stream", nil)
	req.Header.Set("Authorization", "Bearer "+devToken)
	stream, err := http.DefaultClient.Do(req)
	if err != nil || stream.StatusCode != http.StatusOK {
		t.Fatalf("stream = %v %v", stream, err)
	}
	defer stream.Body.Close()
	events := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(stream.Body)
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				events <- data
			}
		}
	}()

	for range 3 {
		req, _ := http.NewRequest(http.MethodGet, base+"/v1/ping?token="+querySecret, nil)
		req.Header.Set("X-Api-Key", headerSecret)
		req.Header.Set("Cookie", "session="+headerSecret)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	select {
	case data := <-events:
		if !strings.Contains(data, `"route":"/v1/ping"`) || strings.Contains(data, querySecret) {
			t.Errorf("stream event = %s", data)
		}
	case <-time.After(5 * time.Second):
		t.Error("no request event on the stream")
	}

	code, _, body := devGet(t, base, "/_dev/requests")
	if code != http.StatusOK || !strings.Contains(body, `"route":"/v1/ping"`) || !strings.Contains(body, `"path":"/v1/ping"`) || !strings.Contains(body, `"request_id":"req_`) {
		t.Errorf("/_dev/requests = %d %s", code, body)
	}
	code, _, logs := devGet(t, base, "/_dev/logs")
	if code != http.StatusOK || !strings.Contains(logs, "http request") {
		t.Errorf("/_dev/logs = %d %s", code, logs)
	}
	for _, leak := range []string{querySecret, headerSecret} {
		if strings.Contains(body+logs, leak) {
			t.Errorf("requests or logs hold %q", leak)
		}
	}

	code, _, body = devGet(t, base, "/_dev/app")
	var info struct {
		Name        string `json:"name"`
		Env         string `json:"env"`
		Modules     []string
		Libraries   []struct{ Path string }
		Jobs        []struct{ Name string }
		Settings    []struct{ Key string }
		Flags       []struct{ Key string }
		Permissions []struct {
			Name  string
			Roles []struct{ Name string }
		}
	}
	if err := json.Unmarshal([]byte(body), &info); err != nil || code != http.StatusOK {
		t.Fatalf("/_dev/app = %d %s", code, body)
	}
	// Libraries come from the binary's build information, which test
	// binaries don't carry.
	if info.Name != app.ServiceName || info.Env != "development" || len(info.Modules) == 0 || info.Libraries == nil ||
		len(info.Jobs) == 0 || len(info.Flags) == 0 || len(info.Permissions) == 0 || len(info.Permissions[0].Roles) == 0 ||
		!strings.Contains(body, `"key":"example.ping_message"`) {
		t.Errorf("/_dev/app = %s", body)
	}

	code, _, body = devGet(t, base, "/_dev/migrations")
	var m struct{ Current, Latest, Pending int64 }
	if err := json.Unmarshal([]byte(body), &m); err != nil || code != http.StatusOK || m.Current == 0 || m.Current != m.Latest || m.Pending != 0 {
		t.Errorf("/_dev/migrations = %d %s", code, body)
	}
	if code, _, body := devGet(t, base, "/_dev/jobs"); code != http.StatusOK || !strings.HasPrefix(body, `{"runs":[`) {
		t.Errorf("/_dev/jobs = %d %s", code, body)
	}
	if code, _, body := devGet(t, base, "/_dev/"); code != http.StatusOK || !strings.Contains(body, "/_dev/mail") || !strings.Contains(body, "/_dev/jobs") {
		t.Errorf("index = %d %s", code, body)
	}
	// The public contract never lists the console.
	if code, _, body := devGet(t, base, "/openapi.json"); code != http.StatusOK || strings.Contains(body, "_dev") {
		t.Errorf("/openapi.json = %d, mentions _dev: %v", code, strings.Contains(body, "_dev"))
	}
}

func TestDevConsoleMail(t *testing.T) {
	// Mailpit that isn't running answers 503.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, closedPort, _ := net.SplitHostPort(ln.Addr().String())
	ln.Close()
	_, base := devServer(t, map[string]string{"MAILPIT_WEB_PORT": closedPort})
	if code, _, body := devGet(t, base, "/_dev/mail"); code != http.StatusServiceUnavailable || !strings.Contains(body, `"code":"unavailable"`) {
		t.Errorf("/_dev/mail without Mailpit = %d %s, want 503", code, body)
	}

	mailpitURL, smtp := os.Getenv(envMailpitURL), os.Getenv(envMailpitSMTP)
	if mailpitURL == "" || smtp == "" {
		t.Skipf("set %s and %s to read email from Mailpit", envMailpitURL, envMailpitSMTP)
	}
	u, err := url.Parse(mailpitURL)
	if err != nil {
		t.Fatal(err)
	}
	_, base = devServer(t, map[string]string{"MAILPIT_SMTP_ADDR": smtp, "MAILPIT_WEB_PORT": u.Port()})
	code, _, body := devGet(t, base, "/_dev/mail")
	if code != http.StatusOK || !strings.Contains(body, `"web_url":"http://`) || !strings.Contains(body, `"messages":[`) {
		t.Errorf("/_dev/mail = %d %s", code, body)
	}
}

func TestDevConsoleSecurity(t *testing.T) {
	_, base := devServer(t, map[string]string{"APP_CORS_ORIGINS": "http://localhost:3000"})
	port := base[strings.LastIndex(base, ":")+1:]

	for _, host := range []string{"evil.example", "evil.example:" + port, "localhost.evil.example:" + port, "127.0.0.1.nip.io:" + port, "[::ffff:127.0.0.1]:" + port, "localhost:1"} {
		code, _, body := devGet(t, base, "/_dev/config", "Host", host)
		if code != http.StatusForbidden || strings.Contains(body, "variables") {
			t.Errorf("Host %s = %d %s, want 403", host, code, body)
		}
	}
	for _, auth := range []string{"", "Bearer wrong-token-wrong-token-wrong-token", "Bearer " + devToken[1:]} {
		if code, _, _ := devGet(t, base, "/_dev/config", "Authorization", auth); code != http.StatusUnauthorized {
			t.Errorf("Authorization %q = %d, want 401", auth, code)
		}
	}
	// The app's CORS configuration, maintenance mode and authentication
	// don't reach the console.
	for _, origin := range []string{"http://localhost:3000", "https://evil.example"} {
		code, header, _ := devGet(t, base, "/_dev/app", "Origin", origin)
		for name := range header {
			if strings.HasPrefix(strings.ToLower(name), "access-control-") {
				t.Errorf("Origin %s: /_dev/app (%d) has CORS header %s", origin, code, name)
			}
		}
		if header.Get("Cache-Control") != "no-store" {
			t.Errorf("Cache-Control = %q, want no-store", header.Get("Cache-Control"))
		}
	}
	if _, header, _ := devGet(t, base, "/v1/ping", "Origin", "http://localhost:3000"); header.Get("Access-Control-Allow-Origin") == "" {
		t.Error("the app's own CORS stopped working")
	}
}

func TestDevConsoleConfigHidesSecrets(t *testing.T) {
	// Secrets every Full app reads, whichever email provider it uses.
	secrets := map[string]string{
		"GOOGLE_CLIENT_SECRET": "google-secret-6d0f",
		"GITHUB_CLIENT_SECRET": "github-secret-83aa",
	}
	env := map[string]string{"GOOGLE_CLIENT_ID": "google-client-id", "GITHUB_CLIENT_ID": "github-client-id"}
	for k, v := range secrets {
		env[k] = v
	}
	_, base := devServer(t, env)
	code, _, body := devGet(t, base, "/_dev/config")
	if code != http.StatusOK {
		t.Fatalf("/_dev/config = %d %s", code, body)
	}
	// The real database URL (with its password) and encryption keys too.
	secrets["DATABASE_URL"] = ""
	secrets["AUTH_ENCRYPTION_KEYS"] = testEncryptionKeys
	secrets["DEV_CONSOLE_TOKEN"] = devToken
	for name, value := range secrets {
		if value != "" && strings.Contains(body, value) {
			t.Errorf("/_dev/config holds the value of %s: %s", name, body)
		}
		if !strings.Contains(body, `{"name":"`+name+`","secret":true,"set":true}`) {
			t.Errorf("/_dev/config doesn't list %s as a set secret: %s", name, body)
		}
	}
	if strings.Contains(body, "postgres://") || strings.Contains(body, "gorbital:gorbital@") {
		t.Errorf("/_dev/config holds the database URL: %s", body)
	}
	for _, want := range []string{
		`{"name":"APP_ENV","secret":false,"set":true,"value":"development"}`,
		`{"name":"GITHUB_CLIENT_ID","secret":false,"set":true,"value":"github-client-id"}`,
		`{"name":"METRICS_ADDR","secret":false,"set":false}`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/_dev/config lacks %s: %s", want, body)
		}
	}
}

func TestDevConsoleRoutes(t *testing.T) {
	_, base := devServer(t, nil)
	code, _, body := devGet(t, base, "/_dev/routes")
	var list struct {
		Routes []struct {
			Method, Path, Source string
		} `json:"routes"`
	}
	if err := json.Unmarshal([]byte(body), &list); err != nil || code != http.StatusOK {
		t.Fatalf("/_dev/routes = %d %s", code, body)
	}
	handlers := 0
	for _, r := range list.Routes {
		if r.Source != "handler" {
			continue
		}
		handlers++
		// Every plain handler listed is really served.
		if code, _, body := devGet(t, base, r.Path); code == http.StatusNotFound {
			t.Errorf("listed route %s %s isn't served: %s", r.Method, r.Path, body)
		}
	}
	if handlers == 0 || !strings.Contains(body, `"path":"/v1/ping"`) || !strings.Contains(body, `"path":"/ops/settings"`) {
		t.Errorf("routes = %s", body)
	}
}

func TestDevConsoleOnlyInDevelopment(t *testing.T) {
	// Without the token, /_dev/ is an ordinary unknown path.
	srv := httptest.NewServer(newApp(t, nil).Handler())
	defer srv.Close()
	if code, _, body := devGet(t, srv.URL, "/_dev/app"); code != http.StatusNotFound || !strings.Contains(body, "no route matches") {
		t.Errorf("/_dev/app without a token = %d %s, want the app's 404", code, body)
	}

	load := func(env map[string]string) error {
		_, err := app.LoadConfig(config.Source{Getenv: func(k string) string { return env[k] }, ReadFile: os.ReadFile})
		return err
	}
	if err := load(map[string]string{"APP_ENV": "production", "DEV_CONSOLE_TOKEN": devToken}); err == nil || !strings.Contains(err.Error(), "DEV_CONSOLE_TOKEN is for local development only") {
		t.Errorf("production with DEV_CONSOLE_TOKEN: error = %v", err)
	}
	if err := load(map[string]string{"APP_ENV": "development", "DEV_CONSOLE_TOKEN": "short"}); err == nil || !strings.Contains(err.Error(), "DEV_CONSOLE_TOKEN must be") {
		t.Errorf("short token: error = %v", err)
	}
	if err := load(map[string]string{"APP_ENV": "development", "MAILPIT_WEB_PORT": "web"}); err == nil || !strings.Contains(err.Error(), "MAILPIT_WEB_PORT") {
		t.Errorf("bad MAILPIT_WEB_PORT: error = %v", err)
	}
}
