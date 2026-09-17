//go:build unix

package portal

import (
	"bufio"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorbital.dev/cli/internal/tunnel"
)

const tunnelSecret = "cG9ydGFsLXR1bm5lbC1zZWNyZXQtdmFsdWU"

var tunnelToken = base64.StdEncoding.EncodeToString([]byte(`{"a":"acct","t":"b1e3c0de-0000-4000-8000-000000000001","s":"` + tunnelSecret + `"}`))

// fakeCloudflaredScript writes a cloudflared that prints a connection and
// the token it was given, then waits to be stopped.
func fakeCloudflaredScript(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cloudflared")
	script := `#!/bin/sh
echo "2026-09-17T10:00:00Z INF |  https://portal-test-tunnel.trycloudflare.com  |" >&2
echo "2026-09-17T10:00:01Z INF Registered tunnel connection connIndex=0 protocol=quic" >&2
echo "2026-09-17T10:00:01Z ERR token=$TUNNEL_TOKEN" >&2
exec sleep 3600
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil { //nolint:gosec // a test program
		t.Fatal(err)
	}
	return path
}

// newTunnelServer is a portal whose tunnel runs binary (or finds none).
func newTunnelServer(t *testing.T, binary string, env ...string) (*Server, *tunnel.Manager, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	var m *tunnel.Manager
	s, ts, _ := newTestServer(t, func(c *Config) {
		hub := c.Hub
		m = tunnel.New(tunnel.Config{
			Dir:    dir,
			Env:    func() []string { return append([]string{"APP_ENV=development"}, env...) },
			Target: func() (string, error) { return "http://127.0.0.1:18080", nil },
			LookPath: func(string) (string, error) {
				if binary == "" {
					return "", exec.ErrNotFound
				}
				return binary, nil
			},
			OnChange:      hub.SetTunnel,
			CheckAttempts: -1,
			StopTimeout:   time.Second,
		})
		c.Tunnel = TunnelConfig{Manager: m, Setup: func(_ context.Context, st tunnel.Status) (tunnel.Setup, error) {
			setup, _ := tunnel.BuildSetup(tunnel.SetupInput{Status: st, Env: env, AppPort: "18080"})
			return setup, nil
		}}
	})
	t.Cleanup(m.Close)
	return s, m, ts
}

func TestTunnelEndpoints(t *testing.T) {
	_, m, ts := newTunnelServer(t, fakeCloudflaredScript(t), tunnel.TokenVar+"="+tunnelToken)

	info := decode[tunnel.Info](t, call(t, ts, http.MethodGet, APIPrefix+"tunnel", "", nil))
	if !info.Cloudflared.Found || !info.Allowed || info.TokenSource != tunnel.TokenVar || info.Status.State != tunnel.StateOff || info.Target != "http://127.0.0.1:18080" || len(info.Install) == 0 {
		t.Errorf("info %+v", info)
	}
	if res := call(t, ts, http.MethodGet, APIPrefix+"tunnel/setup", "", nil); res.StatusCode != http.StatusConflict {
		t.Errorf("setup without a tunnel = %d", res.StatusCode)
	}
	if res := call(t, ts, http.MethodPost, APIPrefix+"tunnel/check", "", nil); res.StatusCode != http.StatusConflict {
		t.Errorf("check without a tunnel = %d", res.StatusCode)
	}

	// Follow the event stream while the tunnel starts.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+APIPrefix+"events", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: testToken})
	stream, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	events := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(stream.Body)
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				events <- data
			}
		}
		close(events)
	}()

	res := call(t, ts, http.MethodPost, APIPrefix+"tunnel/start", `{"mode":"named","hostname":"dev-api.example.com"}`, nil)
	if res.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("start = %d %s", res.StatusCode, body)
	}
	var all strings.Builder
	deadline := time.After(10 * time.Second)
wait:
	for {
		select {
		case data := <-events:
			all.WriteString(data + "\n")
			if strings.Contains(data, `"type":"tunnel"`) && strings.Contains(data, `"state":"connected"`) {
				break wait
			}
		case <-deadline:
			t.Fatalf("no connected tunnel event; got:\n%s", all.String())
		}
	}
	if !strings.Contains(all.String(), `"public_url":"https://dev-api.example.com"`) {
		t.Errorf("events lack the URL:\n%s", all.String())
	}

	setup := decode[tunnel.Setup](t, call(t, ts, http.MethodGet, APIPrefix+"tunnel/setup", "", nil))
	if setup.PublicURL != "https://dev-api.example.com" || setup.Set["APP_PUBLIC_URL"] != setup.PublicURL || !setup.Stable {
		t.Errorf("setup %+v", setup)
	}

	// The token appears nowhere the portal answers.
	var bodies strings.Builder
	for _, path := range []string{"tunnel", "tunnel/setup", "status", "output"} {
		b, _ := io.ReadAll(call(t, ts, http.MethodGet, APIPrefix+path, "", nil).Body)
		bodies.Write(b)
	}
	bodies.WriteString(all.String())
	for _, secret := range []string{tunnelToken, tunnelSecret} {
		if strings.Contains(bodies.String(), secret) {
			t.Fatalf("a portal answer holds the tunnel token:\n%s", bodies.String())
		}
	}
	if !strings.Contains(bodies.String(), "token=[redacted]") {
		t.Errorf("cloudflared's line with the token isn't kept redacted:\n%s", bodies.String())
	}

	pid := m.Status().PID
	if res := call(t, ts, http.MethodPost, APIPrefix+"tunnel/stop", "", nil); res.StatusCode != http.StatusAccepted {
		t.Errorf("stop = %d", res.StatusCode)
	}
	if st := m.Status(); st.State != tunnel.StateOff || pid == 0 {
		t.Errorf("after stop %+v (pid was %d)", st, pid)
	}

	// Settings, and what the portal refuses.
	if got := decode[TunnelSettings](t, call(t, ts, http.MethodPut, APIPrefix+"tunnel/settings", `{"hostname":"HTTPS://Dev.Example.com/"}`, nil)); got.Hostname != "dev.example.com" {
		t.Errorf("settings = %+v", got)
	}
	for _, tt := range []struct {
		method, path, body string
		status             int
		code               string
	}{
		{http.MethodPut, "tunnel/settings", `{"hostname":"localhost"}`, 422, tunnel.CodeHostnameInvalid},
		{http.MethodPost, "tunnel/start", `{"mode":"public"}`, 400, tunnel.CodeInvalidMode},
		{http.MethodPost, "tunnel/start", `{"mode":"quick","port":1}`, 400, "invalid_json"},
		{http.MethodPost, "tunnel/start", `{"mode":"named","hostname":"127.0.0.1"}`, 422, tunnel.CodeHostnameInvalid},
	} {
		res := call(t, ts, tt.method, APIPrefix+tt.path, tt.body, nil)
		if p := decode[problem](t, res); res.StatusCode != tt.status || p.Code != tt.code {
			t.Errorf("%s %s %s = %d %+v, want %d %s", tt.method, tt.path, tt.body, res.StatusCode, p, tt.status, tt.code)
		}
	}
	// Starting needs the mutation header, like every write.
	if res := call(t, ts, http.MethodPost, APIPrefix+"tunnel/start", `{"mode":"quick"}`, func(r *http.Request) { r.Header.Del(MutationHeader) }); res.StatusCode != http.StatusForbidden {
		t.Errorf("start without %s = %d", MutationHeader, res.StatusCode)
	}
}

func TestTunnelWithoutCloudflared(t *testing.T) {
	_, _, ts := newTunnelServer(t, "")
	info := decode[tunnel.Info](t, call(t, ts, http.MethodGet, APIPrefix+"tunnel", "", nil))
	if info.Cloudflared.Found || info.TokenSource != "" {
		t.Errorf("info %+v", info)
	}
	res := call(t, ts, http.MethodPost, APIPrefix+"tunnel/start", `{"mode":"quick"}`, nil)
	if p := decode[problem](t, res); res.StatusCode != 422 || p.Code != tunnel.CodeCloudflaredMissing || !strings.Contains(p.Detail, "never downloads it") {
		t.Errorf("start = %d %+v", res.StatusCode, p)
	}
}

func TestNoTunnel(t *testing.T) {
	_, ts, _ := newTestServer(t, nil)
	res := call(t, ts, http.MethodGet, APIPrefix+"tunnel", "", nil)
	if p := decode[problem](t, res); res.StatusCode != http.StatusNotFound || p.Code != "no_tunnel" {
		t.Errorf("GET tunnel = %d %+v", res.StatusCode, p)
	}
}
