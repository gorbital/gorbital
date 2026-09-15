package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// lockedBuffer is a bytes.Buffer safe for a writer and a reader at once.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestDevWithDocker creates a Full app and runs orb dev in it with real
// Docker: services start, migrations and seed data run, the API serves its
// docs, the seeded administrator signs in, and a registration's email code
// reaches Mailpit. A multi-tenant app also shows the administrator's
// personal workspace with the example projects. It pulls images the first
// time. Set ORB_E2E_DOCKER=1 to run it.
func TestDevWithDocker(t *testing.T) {
	if os.Getenv("ORB_E2E_DOCKER") == "" {
		t.Skip("set ORB_E2E_DOCKER=1 to run orb dev against Docker")
	}
	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, tenancy := range []string{"single", "multi"} {
		t.Run(tenancy, func(t *testing.T) { devWithDocker(t, repo, tenancy) })
	}
}

func devWithDocker(t *testing.T, repo, tenancy string) {
	t.Chdir(t.TempDir())
	name := "e2e-dev-" + tenancy
	if code, _, errOut := runOrb(t, "new", name, "--preset", "full", "--tenancy", tenancy, "--local", repo, "--no-git"); code != 0 {
		t.Fatalf("orb new --preset full --tenancy %s = %d: %s", tenancy, code, errOut)
	}
	dir, _ := filepath.Abs(name)
	t.Chdir(dir)

	// Free host ports, so the test runs next to other databases and apps.
	ports := newDevPorts(t)
	env := ports.env() + fmt.Sprintf("DATABASE_URL=postgres://%[1]s:%[1]s@127.0.0.1:%[2]s/%[1]s?sslmode=disable\nMAILPIT_SMTP_ADDR=127.0.0.1:%[3]s\n",
		name, ports.postgres, ports.smtp)
	writeFile(t, ".env", readFile(t, ".env.example")+env)
	t.Cleanup(func() {
		down := exec.Command("docker", "compose", "down", "-v")
		down.Dir = dir
		if out, err := down.CombinedOutput(); err != nil {
			t.Logf("docker compose down -v: %v\n%s", err, out)
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	var stderr lockedBuffer
	done := make(chan int, 1)
	start := time.Now()
	go func() { done <- Main(ctx, []string{"dev", "--no-reload"}, strings.NewReader(""), io.Discard, &stderr) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	api := "http://127.0.0.1:" + ports.app
	deadline := time.Now().Add(8 * time.Minute)
	for {
		if r, err := http.Get(api + "/readyz"); err == nil {
			_ = r.Body.Close()
			if r.StatusCode == http.StatusOK {
				break
			}
		}
		select {
		case code := <-done:
			t.Fatalf("orb dev exited with %d before the API was ready:\n%s", code, stderr.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("API not ready after 8 minutes:\n%s", stderr.String())
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Logf("orb dev: API ready %s after start", time.Since(start).Round(100*time.Millisecond))

	out := stderr.String()
	for _, want := range []string{"docker compose up -d --wait", "go run ./cmd/migrate", "Seed data created", "✓ Emails     http://127.0.0.1:" + ports.web} {
		if !strings.Contains(out, want) {
			t.Errorf("orb dev output lacks %q:\n%s", want, out)
		}
	}
	if r, err := http.Get(api + "/docs"); err != nil || r.StatusCode != http.StatusOK {
		t.Errorf("GET /docs = %v, %v", r, err)
	} else {
		_ = r.Body.Close()
	}

	var password, secret string
	for line := range strings.Lines(out) {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "Password:"); ok {
			password = strings.TrimSpace(v)
		}
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "2FA key:"); ok {
			secret = strings.TrimSpace(v)
		}
	}
	if !strings.Contains(out, "wrote a random development AUTH_ENCRYPTION_KEYS") {
		t.Errorf("orb dev output lacks the encryption key step:\n%s", out)
	}
	// The administrator's role requires two-factor authentication (ADR-0043).
	started := postJSON(t, api+"/v1/auth/login", fmt.Sprintf(`{"email":"admin@example.com","password":%q}`, password))
	mfa, _ := started["mfa"].(map[string]any)
	challenge, _ := mfa["challenge_token"].(string)
	if challenge == "" || secret == "" {
		t.Fatalf("login as the seeded administrator = %v, 2FA key %q", started, secret)
	}
	login := postJSON(t, api+"/v1/auth/login/mfa", fmt.Sprintf(`{"challenge_token":%q,"code":%q,"transport":"bearer"}`, challenge, totpCode(t, secret, time.Now())))
	token, _ := login["token"].(string)
	if token == "" {
		t.Fatalf("second factor for the seeded administrator = %v", login)
	}
	req, _ := http.NewRequest(http.MethodGet, api+"/ops/settings", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if r, err := http.DefaultClient.Do(req); err != nil || r.StatusCode != http.StatusOK {
		t.Errorf("GET /ops/settings as the administrator = %v, %v", r, err)
	} else {
		_ = r.Body.Close()
	}

	if tenancy == "multi" {
		orgs := getJSON(t, api+"/v1/orgs", token)
		items, _ := orgs["items"].([]any)
		if len(items) == 0 || items[0].(map[string]any)["personal"] != true {
			t.Fatalf("GET /v1/orgs as the administrator = %v, want the personal workspace", orgs)
		}
		workspace, _ := items[0].(map[string]any)["id"].(string)
		projects := getJSON(t, api+"/v1/orgs/"+workspace+"/projects", token)
		if list, _ := projects["items"].([]any); len(list) != 3 {
			t.Errorf("seeded projects in the personal workspace = %v, want 3", projects)
		}
	}

	postJSON(t, api+"/v1/auth/register", `{"email":"new-user@example.com","password":"a long enough password"}`)
	inbox := "http://127.0.0.1:" + ports.web + "/api/v1/search?query=to:new-user@example.com"
	for deadline := time.Now().Add(time.Minute); ; {
		if r, err := http.Get(inbox); err == nil {
			var res struct {
				MessagesCount int `json:"messages_count"`
			}
			_ = json.NewDecoder(r.Body).Decode(&res)
			_ = r.Body.Close()
			if res.MessagesCount > 0 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no email for new-user@example.com in Mailpit after a minute:\n%s", stderr.String())
		}
		time.Sleep(time.Second)
	}
}

func getJSON(t *testing.T, url, token string) map[string]any {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer r.Body.Close()
	var v map[string]any
	_ = json.NewDecoder(r.Body).Decode(&v)
	if r.StatusCode >= 300 {
		t.Fatalf("GET %s = %d %v", url, r.StatusCode, v)
	}
	return v
}

func postJSON(t *testing.T, url, body string) map[string]any {
	t.Helper()
	r, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer r.Body.Close()
	var v map[string]any
	_ = json.NewDecoder(r.Body).Decode(&v)
	if r.StatusCode >= 300 {
		t.Fatalf("POST %s = %d %v", url, r.StatusCode, v)
	}
	return v
}
