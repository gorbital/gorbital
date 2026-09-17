package tunnel

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Environment variables the tunnel reads, from orb dev's environment or the
// app's .env (the environment wins).
const (
	// BinaryVar names the cloudflared program to run instead of the one on
	// PATH.
	BinaryVar = "ORB_CLOUDFLARED"
	// TokenVar holds a named tunnel's token (a secret).
	TokenVar = "CLOUDFLARE_TUNNEL_TOKEN" //nolint:gosec // the variable's name, not a credential
	// TokenFileVar names a file holding the token instead.
	TokenFileVar = TokenVar + "_FILE"
	// HostnameVar is a named tunnel's public hostname.
	HostnameVar = "ORB_TUNNEL_HOSTNAME"
)

// SettingsFile keeps the hostname entered in the Dev Portal, inside the
// app directory (git ignores .orb). It never holds the token.
const SettingsFile = ".orb/portal/tunnel.json"

// Error is a refusal to start a tunnel, with a stable code the portal
// answers with.
type Error struct {
	Code   string
	Detail string
}

func (e *Error) Error() string { return e.Detail }

// Codes of [Error].
const (
	CodeCloudflaredMissing = "cloudflared_missing"
	CodeNotDevelopment     = "not_development"
	CodeTokenMissing       = "tunnel_token_missing"
	CodeTokenInvalid       = "tunnel_token_invalid"
	CodeHostnameMissing    = "tunnel_hostname_missing"
	CodeHostnameInvalid    = "tunnel_hostname_invalid"
	CodeInvalidMode        = "invalid_tunnel_mode"
	CodeNoTarget           = "no_app_address"
	CodeNotConnected       = "tunnel_not_connected"
)

func refusal(code, format string, args ...any) *Error {
	return &Error{Code: code, Detail: fmt.Sprintf(format, args...)}
}

// envValue returns the last non-empty value of key in env, or "".
func envValue(env []string, key string) string {
	v := ""
	for _, kv := range env {
		if s, ok := strings.CutPrefix(kv, key+"="); ok && s != "" {
			v = s
		}
	}
	return v
}

// maxTokenBytes bounds a token; Cloudflare's are a few hundred bytes.
const maxTokenBytes = 8 << 10

// LoadToken reads a named tunnel's token from env: CLOUDFLARE_TUNNEL_TOKEN
// or the file CLOUDFLARE_TUNNEL_TOKEN_FILE names. It returns the token and
// the variable it came from. Errors never include the token.
func LoadToken(env []string) (token, source string, err error) {
	value, file := envValue(env, TokenVar), envValue(env, TokenFileVar)
	switch {
	case value != "" && file != "":
		return "", "", refusal(CodeTokenInvalid, "both %s and %s are set; keep one", TokenVar, TokenFileVar)
	case file != "":
		f, err := os.Open(file) //nolint:gosec // the developer names their own file
		if err != nil {
			return "", "", refusal(CodeTokenInvalid, "%s: %v", TokenFileVar, err)
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, maxTokenBytes+1))
		if err != nil {
			return "", "", refusal(CodeTokenInvalid, "%s: %v", TokenFileVar, err)
		}
		value, source = strings.TrimSpace(string(data)), TokenFileVar
	case value != "":
		source = TokenVar
	default:
		return "", "", refusal(CodeTokenMissing, "a named tunnel needs its token: set %s in .env (Cloudflare dashboard → Zero Trust → Networks → Tunnels → your tunnel → the value after --token in the install command), or %s", TokenVar, TokenFileVar)
	}
	if err := CheckToken(value); err != nil {
		return "", "", refusal(CodeTokenInvalid, "%s: %v", source, err)
	}
	return value, source, nil
}

// CheckToken reports whether token looks like a Cloudflare tunnel token:
// base64 of a JSON object with the tunnel's ID ("t") and secret ("s").
// The error never includes the token.
func CheckToken(token string) error {
	if token == "" || len(token) > maxTokenBytes {
		return errors.New("the token is empty or too long")
	}
	for i := range len(token) {
		if token[i] <= ' ' || token[i] > '~' {
			return errors.New("the token holds spaces or characters a tunnel token doesn't have; copy only the value after --token")
		}
	}
	if _, ok := tokenSecret(token); !ok {
		return errors.New("this isn't a Cloudflare tunnel token; copy the long value after --token in the tunnel's install command")
	}
	return nil
}

// tokenSecret decodes a token and returns its secret.
func tokenSecret(token string) (string, bool) {
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		data, err := enc.DecodeString(token)
		if err != nil {
			continue
		}
		var t struct {
			Tunnel string `json:"t"`
			Secret string `json:"s"`
		}
		if json.Unmarshal(data, &t) == nil && t.Tunnel != "" && t.Secret != "" {
			return t.Secret, true
		}
	}
	return "", false
}

// NormalizeHostname turns what a developer types (api.example.com,
// https://api.example.com/) into a hostname, or refuses it: it must be a
// DNS name with at least two labels, not an IP address, localhost or a
// quick tunnel's name.
func NormalizeHostname(raw string) (string, error) {
	h := strings.ToLower(strings.TrimSpace(raw))
	h = strings.TrimPrefix(strings.TrimPrefix(h, "https://"), "http://")
	h = strings.TrimSuffix(h, "/")
	h = strings.TrimSuffix(h, ".")
	switch {
	case h == "":
		return "", refusal(CodeHostnameMissing, "a named tunnel needs its public hostname, such as dev-api.example.com: the one you gave the tunnel's public hostname in the Cloudflare dashboard (or %s)", HostnameVar)
	case len(h) > 253 || strings.ContainsAny(h, "/:@?#[] "):
		return "", refusal(CodeHostnameInvalid, "%q isn't a hostname: give only the name, such as dev-api.example.com", truncate(raw, 80))
	case net.ParseIP(h) != nil:
		return "", refusal(CodeHostnameInvalid, "%q is an IP address: a named tunnel's hostname is a DNS name on a Cloudflare zone", h)
	case h == "localhost" || strings.HasSuffix(h, ".localhost"):
		return "", refusal(CodeHostnameInvalid, "%q is this machine: give the tunnel's public hostname", h)
	case strings.HasSuffix(h, quickDomain):
		return "", refusal(CodeHostnameInvalid, "%q is a quick tunnel's name, which changes on every run: start a quick tunnel instead, or give your named tunnel's hostname", h)
	}
	labels := strings.Split(h, ".")
	if len(labels) < 2 {
		return "", refusal(CodeHostnameInvalid, "%q isn't a public hostname: give a name such as dev-api.example.com", h)
	}
	for _, l := range labels {
		if !validLabel(l) {
			return "", refusal(CodeHostnameInvalid, "%q isn't a valid hostname", truncate(h, 80))
		}
	}
	return h, nil
}

// settings is what SettingsFile holds.
type settings struct {
	Hostname string `json:"hostname,omitempty"`
}

func readSettings(dir string) settings {
	var s settings
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(SettingsFile)))
	if err == nil {
		_ = json.Unmarshal(data, &s)
	}
	return s
}

func writeSettings(dir string, s settings) error {
	path := filepath.Join(dir, filepath.FromSlash(SettingsFile))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// Binary describes the cloudflared orb would run.
type Binary struct {
	Found bool `json:"found"`
	// Path is the program; FromEnv reports it came from ORB_CLOUDFLARED.
	Path    string `json:"path,omitempty"`
	FromEnv bool   `json:"from_env"`
	// Version is the first line of cloudflared --version.
	Version string `json:"version,omitempty"`
	// Problem says why it wasn't found.
	Problem string `json:"problem,omitempty"`
}

// FindBinary looks for cloudflared: ORB_CLOUDFLARED when set (a path or a
// program on PATH), else cloudflared on PATH. It never downloads anything.
// version, when not nil, reports the version of the program found.
func FindBinary(env []string, lookPath func(string) (string, error), version func(path string) string) Binary {
	name, fromEnv := "cloudflared", false
	if v := envValue(env, BinaryVar); v != "" {
		name, fromEnv = v, true
	}
	path, err := lookPath(name)
	if err != nil {
		b := Binary{FromEnv: fromEnv, Problem: "cloudflared isn't installed (not found on PATH)"}
		if fromEnv {
			b.Problem = fmt.Sprintf("%s=%s: %v", BinaryVar, truncate(name, 200), err)
		}
		return b
	}
	b := Binary{Found: true, Path: path, FromEnv: fromEnv}
	if version != nil {
		b.Version = version(path)
	}
	return b
}

// Version runs cloudflared --version and returns its first line, or "".
func Version(ctx context.Context, path string) string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version") //nolint:gosec // the developer's own cloudflared
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if cmd.Run() != nil {
		return ""
	}
	line, _, _ := strings.Cut(strings.TrimSpace(out.String()), "\n")
	return truncate(line, 120)
}

// InstallStep is one way to install cloudflared.
type InstallStep struct {
	// OS is darwin, linux or windows (as Go names them).
	OS      string `json:"os"`
	Label   string `json:"label"`
	Command string `json:"command,omitempty"`
	URL     string `json:"url,omitempty"`
}

// downloadsURL is Cloudflare's page with every package.
const downloadsURL = "https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/"

// InstallHelp lists how to install cloudflared on each operating system;
// orb never installs it.
func InstallHelp() []InstallStep {
	return []InstallStep{
		{OS: "darwin", Label: "macOS with Homebrew", Command: "brew install cloudflared"},
		{OS: "linux", Label: "Debian and Ubuntu (Cloudflare's apt repository)", URL: "https://pkg.cloudflare.com/index.html"},
		{OS: "linux", Label: "Fedora, RHEL and CentOS (Cloudflare's rpm repository)", URL: "https://pkg.cloudflare.com/index.html"},
		{OS: "linux", Label: "Any Linux: the release binary", URL: "https://github.com/cloudflare/cloudflared/releases/latest"},
		{OS: "windows", Label: "Windows with winget", Command: "winget install --id Cloudflare.cloudflared"},
		{OS: "", Label: "Every package and platform", URL: downloadsURL},
	}
}

// InstallText is InstallHelp for a terminal, for the current OS first.
func InstallText() string {
	var b strings.Builder
	b.WriteString("cloudflared isn't installed; orb runs your own cloudflared and never downloads it. Install it:\n")
	for _, s := range InstallHelp() {
		if s.OS != runtime.GOOS && s.OS != "" {
			continue
		}
		fmt.Fprintf(&b, "    %s: %s%s\n", s.Label, s.Command, s.URL)
	}
	fmt.Fprintf(&b, "  then run orb dev again (or set %s to its path)", BinaryVar)
	return b.String()
}

// truncate shortens s to at most n bytes, on a rune boundary.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n] + "…"
}
