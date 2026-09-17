package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"gorbital.dev/cli/internal/portal"
	"gorbital.dev/cli/internal/tunnel"
)

func TestDevTunnelFlags(t *testing.T) {
	for _, args := range [][]string{
		{"dev", "--tunnel", "public"},
		{"dev", "--tunnel-hostname", "dev.example.com"},
		{"dev", "--tunnel", "quick", "--tunnel-hostname", "dev.example.com"},
	} {
		var stderr bytes.Buffer
		if code := Main(context.Background(), args, strings.NewReader(""), io.Discard, &stderr); code != 2 {
			t.Errorf("orb %s = exit %d (%s), want 2", strings.Join(args, " "), code, stderr.String())
		}
	}
}

func TestDevTunnelTokenNeverReachesTheApp(t *testing.T) {
	d := newDevRunner(io.Discard)
	env := d.appEnv([]string{"APP_ENV=development", tunnel.TokenVar + "=secret", tunnel.TokenFileVar + "=/run/secrets/t", "OTHER=1"})
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, tunnel.TokenVar) || !strings.Contains(joined, "OTHER=1") {
		t.Errorf("app environment:\n%s", joined)
	}
}

// fakeCloudflared writes a quick-tunnel cloudflared that records its
// arguments in dir/args.
func fakeCloudflared(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "fake-cloudflared")
	writeFile(t, path, `#!/bin/sh
echo "$@" > "`+filepath.Join(dir, "args")+`"
echo "2026-09-17T10:00:00Z INF |  https://orb-dev-test.trycloudflare.com  |" >&2
echo "2026-09-17T10:00:01Z INF Registered tunnel connection connIndex=0" >&2
exec sleep 3600
`)
	if err := os.Chmod(path, 0o700); err != nil { //nolint:gosec // a test program
		t.Fatal(err)
	}
	return path
}

func waitTunnel(t *testing.T, d *devRunner, ok func(tunnel.Status) bool) tunnel.Status {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if s := d.tunnel.Status(); ok(s) {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("tunnel status %+v", d.tunnel.Status())
	return tunnel.Status{}
}

func TestDevTunnelFollowsTheApp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake cloudflared is a shell script")
	}
	ports := newDevPorts(t)
	dir := newDevApp(t, "preset: minimal\n", ports.env())
	bin := fakeCloudflared(t, t.TempDir())
	writeFile(t, ".env", ports.env()+"ORB_CLOUDFLARED="+bin+"\nAPP_CORS_ORIGINS=http://localhost:5173\n")
	d := rebuildTunnel(newDevRunner(io.Discard), dir)
	t.Cleanup(d.tunnel.Close)
	sub := d.hub.Subscribe()
	defer d.hub.Unsubscribe(sub)

	if err := d.tunnel.Start(context.Background(), tunnel.StartOptions{Mode: tunnel.ModeQuick}); err != nil {
		t.Fatal(err)
	}
	s := waitTunnel(t, d, func(s tunnel.Status) bool { return s.State == tunnel.StateConnected })
	if s.Target != "http://127.0.0.1:"+ports.app || s.PublicURL != "https://orb-dev-test.trycloudflare.com" {
		t.Errorf("status %+v", s)
	}
	// The portal's event stream gets the status.
	gotEvent := false
	for len(sub.C) > 0 {
		if e := <-sub.C; e.Type == "tunnel" && e.Tunnel != nil {
			gotEvent = true
		}
	}
	if !gotEvent {
		t.Error("no tunnel event on the hub")
	}

	setup, err := d.tunnelSetup(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if setup.Set["APP_PUBLIC_URL"] != s.PublicURL || setup.RoutesKnown || len(setup.Callbacks) == 0 {
		t.Errorf("setup %+v", setup)
	}

	// The app moves to another port: the quick tunnel follows.
	other := freePort(t)
	d.mu.Lock()
	d.addr = "127.0.0.1:" + other
	d.mu.Unlock()
	d.tunnel.TargetChanged(context.Background())
	s = waitTunnel(t, d, func(s tunnel.Status) bool {
		return s.State == tunnel.StateConnected && s.Restarts == 1
	})
	if s.Target != "http://127.0.0.1:"+other {
		t.Errorf("after the move %+v", s)
	}
	pid := s.PID
	d.tunnel.Close()
	if err := waitGone(pid); err != nil {
		t.Error(err)
	}
}

func TestDevTunnelRefusesProduction(t *testing.T) {
	ports := newDevPorts(t)
	dir := newDevApp(t, "preset: minimal\n", ports.env())
	writeFile(t, ".env", ports.env()+"APP_ENV=production\nORB_CLOUDFLARED=/bin/sh\n")
	d := rebuildTunnel(newDevRunner(io.Discard), dir)
	err := d.tunnel.Start(context.Background(), tunnel.StartOptions{Mode: tunnel.ModeQuick})
	var te *tunnel.Error
	if !errors.As(err, &te) || te.Code != tunnel.CodeNotDevelopment {
		t.Errorf("Start in production = %v", err)
	}
	if d.tunnel.Status().State != tunnel.StateOff || strings.Contains(strings.Join(linesText(d.hub.Lines(0)), "\n"), "starting") {
		t.Error("a refused tunnel started")
	}
}

// rebuildTunnel gives d a tunnel for the app in dir as newDevRunner does,
// without the automatic reachability check (tests use no network).
func rebuildTunnel(d *devRunner, dir string) *devRunner {
	d.dir = dir
	d.tunnel = tunnel.New(tunnel.Config{
		Dir: dir,
		Env: func() []string {
			env, _ := devEnv(".env")
			return withAppEnv(env)
		},
		Target:        func() (string, error) { return d.Status().URL, nil },
		OnChange:      d.hub.SetTunnel,
		CheckAttempts: -1,
		StopTimeout:   time.Second,
	})
	return d
}

func linesText(lines []portal.OutputLine) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.Text
	}
	return out
}

// waitGone waits for a process to exit.
func waitGone(pid int) error {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !processRuns(pid) {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return errors.New("process " + strconv.Itoa(pid) + " still runs")
}

// processRuns reports whether pid can still be signalled.
func processRuns(pid int) bool {
	p, err := os.FindProcess(pid)
	return err == nil && p.Signal(syscall.Signal(0)) == nil
}
