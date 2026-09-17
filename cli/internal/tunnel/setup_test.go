package tunnel

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNormalizeHostname(t *testing.T) {
	ok := map[string]string{
		"dev-api.example.com":          "dev-api.example.com",
		"  HTTPS://Dev.Example.COM/  ": "dev.example.com",
		"http://a.b.c.example.org":     "a.b.c.example.org",
		"tunnel.example.com.":          "tunnel.example.com",
	}
	for in, want := range ok {
		if got, err := NormalizeHostname(in); got != want || err != nil {
			t.Errorf("NormalizeHostname(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	refused := map[string]string{
		"":                          CodeHostnameMissing,
		"example":                   CodeHostnameInvalid,
		"localhost":                 CodeHostnameInvalid,
		"app.localhost":             CodeHostnameInvalid,
		"127.0.0.1":                 CodeHostnameInvalid,
		"dev.example.com:8443":      CodeHostnameInvalid,
		"https://dev.example.com/x": CodeHostnameInvalid,
		"user@dev.example.com":      CodeHostnameInvalid,
		"abc.trycloudflare.com":     CodeHostnameInvalid,
		"bad_label.example.com":     CodeHostnameInvalid,
		"-bad.example.com":          CodeHostnameInvalid,
		"dev..example.com":          CodeHostnameInvalid,
		strings.Repeat("a.", 130):   CodeHostnameInvalid,
	}
	for in, code := range refused {
		if got, err := NormalizeHostname(in); errCodeOf(err) != code {
			t.Errorf("NormalizeHostname(%q) = %q, %v; want %s", in, got, err, code)
		}
	}
}

func errCodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestLoadToken(t *testing.T) {
	token := base64.StdEncoding.EncodeToString([]byte(`{"a":"x","t":"tunnel-id","s":"the-secret-value"}`))
	file := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(file, []byte("\n"+token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, source, err := LoadToken([]string{TokenVar + "=" + token}); got != token || source != TokenVar || err != nil {
		t.Errorf("from the variable: %q %q %v", got, source, err)
	}
	if got, source, err := LoadToken([]string{TokenFileVar + "=" + file}); got != token || source != TokenFileVar || err != nil {
		t.Errorf("from the file: %q %q %v", got, source, err)
	}
	for _, env := range [][]string{
		{},
		{TokenVar + "=eyJub3QiOiJhIHR1bm5lbCJ9"}, // base64 JSON without t and s
		{TokenVar + "=has space"},
		{TokenFileVar + "=" + filepath.Join(t.TempDir(), "missing")},
	} {
		if _, _, err := LoadToken(env); err == nil {
			t.Errorf("LoadToken(%v) accepted", env)
		} else if strings.Contains(err.Error(), "the-secret-value") {
			t.Errorf("error holds the secret: %v", err)
		}
	}
	if s := secretsOf(token); len(s) != 2 || s[1] != "the-secret-value" {
		t.Errorf("secretsOf = %v", s)
	}
	if got := redact("token="+token+" s=the-secret-value", secretsOf(token)); got != "token=[redacted] s=[redacted]" {
		t.Errorf("redact = %q", got)
	}
}

func TestChildEnvDropsTokenVariables(t *testing.T) {
	got := childEnv([]string{"PATH=/bin", "TUNNEL_TOKEN=old", "TUNNEL_URL=http://x", TokenVar + "=t", TokenFileVar + "=/f", "HOME=/h"}, "")
	if strings.Join(got, " ") != "PATH=/bin HOME=/h" {
		t.Errorf("quick: %v", got)
	}
	got = childEnv([]string{"PATH=/bin", "TUNNEL_TOKEN=old"}, "new")
	if strings.Join(got, " ") != "PATH=/bin TUNNEL_TOKEN=new" {
		t.Errorf("named: %v", got)
	}
}

func TestInstallHelp(t *testing.T) {
	text := InstallText()
	if !strings.Contains(text, "never downloads it") || !strings.Contains(text, downloadsURL) {
		t.Errorf("InstallText:\n%s", text)
	}
	want := map[string]string{"darwin": "brew install cloudflared", "windows": "winget install --id Cloudflare.cloudflared", "linux": "pkg.cloudflare.com"}
	if w, ok := want[runtime.GOOS]; ok && !strings.Contains(text, w) {
		t.Errorf("InstallText on %s lacks %q:\n%s", runtime.GOOS, w, text)
	}
	joined := ""
	for _, s := range InstallHelp() {
		joined += s.OS + " " + s.Command + s.URL + "\n"
	}
	for os, w := range want {
		if !strings.Contains(joined, os+" ") || !strings.Contains(joined, w) {
			t.Errorf("InstallHelp lacks %s %q", os, w)
		}
	}
}

func TestBuildSetup(t *testing.T) {
	routes := []Route{
		{"GET", "/v1/auth/google/callback"}, {"POST", "/v1/auth/apple/callback"}, {"POST", "/v1/auth/apple/notifications"},
		{"GET", "/v1/auth/github/callback"}, {"GET", "/v1/books"},
	}
	named := Status{State: StateConnected, Mode: ModeNamed, PublicURL: "https://dev.example.com", Hostname: "dev.example.com", Stable: true}
	env := []string{
		"APP_ADDR=127.0.0.1:8090",
		"AUTH_DEFAULT_RETURN_TO=http://localhost:8090/docs?x=1",
		"APP_CORS_ORIGINS=http://localhost:5173",
		"APP_TRUSTED_PROXIES=10.0.0.0/8",
		"GOOGLE_CLIENT_ID=abc.apps.googleusercontent.com",
	}
	s, ok := BuildSetup(SetupInput{Status: named, Env: env, AppPort: "8090", Routes: routes, RoutesKnown: true})
	if !ok {
		t.Fatal("no setup")
	}
	byKey := map[string]EnvChange{}
	for _, c := range s.Changes {
		byKey[c.Key] = c
	}
	want := map[string]string{
		"APP_PUBLIC_URL":         "https://dev.example.com",
		"WEBAUTHN_RP_ID":         "dev.example.com",
		"WEBAUTHN_ORIGINS":       "https://dev.example.com",
		"AUTH_DEFAULT_RETURN_TO": "https://dev.example.com/docs?x=1",
		"APP_CORS_ORIGINS":       "http://localhost:5173,https://dev.example.com",
		"APP_TRUSTED_PROXIES":    "10.0.0.0/8,127.0.0.1/32,::1/128",
	}
	for k, v := range want {
		if byKey[k].Proposed != v {
			t.Errorf("%s = %+v, want %q", k, byKey[k], v)
		}
	}
	if len(s.Changes) != len(want) {
		t.Errorf("changes %+v", s.Changes)
	}
	if !byKey["APP_CORS_ORIGINS"].Optional || s.Set["APP_CORS_ORIGINS"] != "" || s.Set["APP_PUBLIC_URL"] != "https://dev.example.com" || len(s.Set) != 5 {
		t.Errorf("set %v", s.Set)
	}
	if byKey["AUTH_DEFAULT_RETURN_TO"].Current != "http://localhost:8090/docs?x=1" {
		t.Errorf("current %+v", byKey["AUTH_DEFAULT_RETURN_TO"])
	}
	var urls []string
	for _, c := range s.Callbacks {
		urls = append(urls, c.Provider+" "+c.URL)
		if c.Provider == "google" && !c.Configured || c.Provider == "github" && c.Configured {
			t.Errorf("configured %+v", c)
		}
	}
	if got := strings.Join(urls, "\n"); got != "google https://dev.example.com/v1/auth/google/callback\napple https://dev.example.com/v1/auth/apple/callback\napple https://dev.example.com/v1/auth/apple/notifications\ngithub https://dev.example.com/v1/auth/github/callback" {
		t.Errorf("callbacks (the app serves no Resend webhook):\n%s", got)
	}
	for _, w := range s.Warnings {
		if strings.Contains(w, "changes every time") {
			t.Errorf("a named tunnel warned about a changing URL: %v", s.Warnings)
		}
	}

	// A quick tunnel, already configured, another local frontend, unknown routes.
	quick := Status{State: StateConnected, Mode: ModeQuick, PublicURL: "https://calm-river.trycloudflare.com"}
	env = []string{
		"APP_PUBLIC_URL=https://calm-river.trycloudflare.com",
		"WEBAUTHN_RP_ID=calm-river.trycloudflare.com",
		"WEBAUTHN_ORIGINS=https://calm-river.trycloudflare.com",
		"AUTH_DEFAULT_RETURN_TO=http://127.0.0.1:3000/signed-in",
		"APP_TRUSTED_PROXIES=127.0.0.0/8, ::1",
	}
	s, _ = BuildSetup(SetupInput{Status: quick, Env: env, AppPort: "8090"})
	if len(s.Changes) != 1 || s.Changes[0].Key != "AUTH_DEFAULT_RETURN_TO" || s.Changes[0].Proposed != "https://calm-river.trycloudflare.com/docs" {
		t.Errorf("quick changes %+v", s.Changes)
	}
	if len(s.Callbacks) != 5 || s.RoutesKnown {
		t.Errorf("unknown routes list every default address: %+v", s.Callbacks)
	}
	if !strings.Contains(strings.Join(s.Warnings, " "), "changes every time it starts") {
		t.Errorf("warnings %v", s.Warnings)
	}
	if _, ok := BuildSetup(SetupInput{Status: Status{State: StateStarting}}); ok {
		t.Error("setup without a URL")
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	m := New(Config{Dir: dir})
	if h, err := m.SaveHostname("HTTPS://Saved.Example.com"); h != "saved.example.com" || err != nil {
		t.Fatalf("SaveHostname = %q, %v", h, err)
	}
	info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(SettingsFile)))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("settings file %v %v", info, err)
	}
	if readSettings(dir).Hostname != "saved.example.com" {
		t.Error("not saved")
	}
	if _, err := m.SaveHostname("localhost"); err == nil {
		t.Error("saved localhost")
	}
}
