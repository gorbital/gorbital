package recipes

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestMailMatchesGoldenApp checks that the Resend recipe reproduces the
// golden Full apps exactly (ADR-0021, ADR-0037).
func TestMailMatchesGoldenApp(t *testing.T) {
	r, err := RenderMail(MailResend, "example.com/acme-api")
	if err != nil {
		t.Fatalf("RenderMail() error = %v", err)
	}
	for _, app := range []string{"full-single", "full-multi"} {
		golden := filepath.Join("..", "..", "..", "examples", app)
		for path, content := range map[string][]byte{InfraMailPath: r.InfraMail, InfraMailTestPath: r.InfraMailTest} {
			want, err := os.ReadFile(filepath.Join(golden, path))
			if err != nil {
				t.Fatal(err)
			}
			if string(content) != string(want) {
				t.Errorf("resend %s differs from examples/%s:\n--- recipe\n%s\n--- golden\n%s", path, app, content, want)
			}
		}
		example, err := os.ReadFile(filepath.Join(golden, ".env.example"))
		if err != nil {
			t.Fatal(err)
		}
		block, err := Block(example, MailBlock)
		if err != nil || string(block) != string(r.EnvBlock) {
			t.Errorf("resend .env.example block differs from examples/%s (%v):\n--- recipe\n%s\n--- golden\n%s", app, err, r.EnvBlock, block)
		}
	}
	if !slices.Equal(r.EnvKeys, []string{"RESEND_API_KEY", "RESEND_WEBHOOK_SECRET"}) || len(r.Modules) != 2 {
		t.Errorf("EnvKeys, Modules = %v, %v", r.EnvKeys, r.Modules)
	}
}

func TestRenderMailSMTP(t *testing.T) {
	r, err := RenderMail(MailSMTP, "example.com/shop-api")
	if err != nil {
		t.Fatalf("RenderMail() error = %v", err)
	}
	if !strings.Contains(string(r.InfraMail), `const mailProvider = "smtp"`) || r.Label != "SMTP" {
		t.Errorf("smtp recipe = %s", r.InfraMail)
	}
	if test := string(r.InfraMailTest); !strings.Contains(test, `"example.com/shop-api/internal/app"`) || !strings.Contains(test, `mailProviderName    = "smtp"`) {
		t.Errorf("smtp test recipe = %s", test)
	}
	if want := []string{"SMTP_HOST", "SMTP_PORT", "SMTP_TLS", "SMTP_USERNAME", "SMTP_PASSWORD"}; !slices.Equal(r.EnvKeys, want) {
		t.Errorf("EnvKeys = %v, want %v", r.EnvKeys, want)
	}
	if !slices.Equal(r.Modules, []string{"gorbital.dev/modules/mail/smtp"}) {
		t.Errorf("Modules = %v", r.Modules)
	}
	if _, err := RenderMail("sendgrid", "example.com/shop-api"); err == nil {
		t.Error("RenderMail(sendgrid) error = nil")
	}
}

func TestReplaceBlock(t *testing.T) {
	src := []byte("A=1\n# orb:begin mail (managed)\nOLD=1\n# orb:end mail\nB=2\n")
	got, err := ReplaceBlock(src, MailBlock, []byte("# orb:begin mail\nNEW=1\n# orb:end mail\n"))
	if err != nil || string(got) != "A=1\n# orb:begin mail\nNEW=1\n# orb:end mail\nB=2\n" {
		t.Errorf("ReplaceBlock() = %q, %v", got, err)
	}
	block, err := Block(src, MailBlock)
	if err != nil || string(block) != "# orb:begin mail (managed)\nOLD=1\n# orb:end mail\n" {
		t.Errorf("Block() = %q, %v", block, err)
	}
	for _, bad := range []string{"A=1\n", "# orb:begin mail\nA=1\n", "# orb:begin mailbox\n# orb:end mail\n"} {
		if _, err := ReplaceBlock([]byte(bad), MailBlock, nil); !errors.Is(err, ErrBlockMissing) {
			t.Errorf("ReplaceBlock(%q) error = %v, want ErrBlockMissing", bad, err)
		}
	}
	for line, want := range map[string]string{"KEY=1": "KEY", "export K_2 = x": "K_2", "# KEY=1": "", "no": ""} {
		if got, _ := EnvKey(line); got != want {
			t.Errorf("EnvKey(%q) = %q, want %q", line, got, want)
		}
	}
}
