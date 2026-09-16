package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// consoleEnvExample is an .env.example declaring the dev console token.
func consoleEnvExample(ports devPorts) string {
	return "APP_ENV=development\n" + ports.env() + "DEV_CONSOLE_TOKEN=\n"
}

// prepareMinimal runs prepare in a stand-in Minimal app and returns the
// runner and its output.
func prepareMinimal(t *testing.T, envExample, env string) (*devRunner, string) {
	t.Helper()
	newDevApp(t, "preset: minimal\n", envExample)
	if env != "" {
		writeFile(t, ".env", env)
	}
	var out bytes.Buffer
	d := newDevRunner(&out)
	(&fakeCommands{noTool: true}).install(d)
	if err := d.prepare(context.Background()); err != nil {
		t.Fatalf("prepare() error = %v", err)
	}
	return d, out.String()
}

func TestDevConsoleTokenPerRun(t *testing.T) {
	t.Setenv(devConsoleTokenVar, "")
	ports := newDevPorts(t)
	example := consoleEnvExample(ports)
	d, out := prepareMinimal(t, example, example)
	dir, _ := os.Getwd()

	raw, err := base64.RawURLEncoding.DecodeString(d.consoleToken)
	if err != nil || len(raw) != 32 {
		t.Fatalf("token %q: %d bytes, %v; want 256 bits in base64url", d.consoleToken, len(raw), err)
	}
	if got := envValue(d.appEnv(nil), devConsoleTokenVar, ""); got != d.consoleToken {
		t.Errorf("the app's %s = %q, want the token", devConsoleTokenVar, got)
	}
	for _, want := range []string{
		"✓ Dev APIs   http://127.0.0.1:" + ports.app + "/_dev/",
		"Token      " + d.consoleToken + " (Authorization: Bearer",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	// Never on disk: not in .env nor any file of the app.
	err = filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		if data, _ := os.ReadFile(p); bytes.Contains(data, []byte(d.consoleToken)) {
			t.Errorf("%s holds the dev console token", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Every run gets a new token.
	again, _ := prepareMinimal(t, example, example)
	if again.consoleToken == d.consoleToken || again.consoleToken == "" {
		t.Errorf("second run token = %q, want a new one", again.consoleToken)
	}
}

func TestDevConsoleTokenOnlyWhenSupported(t *testing.T) {
	t.Setenv(devConsoleTokenVar, "")
	ports := newDevPorts(t)
	for name, tt := range map[string]struct{ example, env string }{
		"production":           {consoleEnvExample(ports), "APP_ENV=production\n" + ports.env()},
		"app before ADR-0065":  {"APP_ENV=development\n" + ports.env(), ""},
		"example in a comment": {"# DEV_CONSOLE_TOKEN=\n" + ports.env(), ""},
	} {
		t.Run(name, func(t *testing.T) {
			d, out := prepareMinimal(t, tt.example, tt.env)
			if d.consoleToken != "" || strings.Contains(out, "Dev APIs") || envValue(d.appEnv(nil), devConsoleTokenVar, "") != "" {
				t.Errorf("token %q, output:\n%s", d.consoleToken, out)
			}
		})
	}
}

func TestDevConsoleTokenFromEnvironment(t *testing.T) {
	const mine = "my-stable-dev-console-token-for-a-ui-0123456789"
	t.Setenv(devConsoleTokenVar, mine)
	ports := newDevPorts(t)
	d, out := prepareMinimal(t, consoleEnvExample(ports), "")
	if d.consoleToken != mine || !d.consoleTokenFromEnv || strings.Contains(out, mine) || !strings.Contains(out, "DEV_CONSOLE_TOKEN from your environment") {
		t.Errorf("token %q (from env %v), output:\n%s", d.consoleToken, d.consoleTokenFromEnv, out)
	}
}
