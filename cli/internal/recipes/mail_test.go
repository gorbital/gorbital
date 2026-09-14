package recipes

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestMailMatchesGoldenApp checks that the Resend recipe reproduces
// examples/full-single exactly (ADR-0021, ADR-0037).
func TestMailMatchesGoldenApp(t *testing.T) {
	r, err := RenderMail(MailResend)
	if err != nil {
		t.Fatalf("RenderMail() error = %v", err)
	}
	golden := filepath.Join("..", "..", "..", "examples", "full-single")
	infra, err := os.ReadFile(filepath.Join(golden, InfraMailPath))
	if err != nil {
		t.Fatal(err)
	}
	if string(r.InfraMail) != string(infra) {
		t.Errorf("resend %s differs from examples/full-single:\n--- recipe\n%s\n--- golden\n%s", InfraMailPath, r.InfraMail, infra)
	}
	example, err := os.ReadFile(filepath.Join(golden, ".env.example"))
	if err != nil {
		t.Fatal(err)
	}
	block, err := Block(example, MailBlock)
	if err != nil || string(block) != string(r.EnvBlock) {
		t.Errorf("resend .env.example block differs from examples/full-single (%v):\n--- recipe\n%s\n--- golden\n%s", err, r.EnvBlock, block)
	}
	if !slices.Equal(r.EnvKeys, []string{"RESEND_API_KEY"}) || len(r.Modules) != 2 {
		t.Errorf("EnvKeys, Modules = %v, %v", r.EnvKeys, r.Modules)
	}
}

func TestRenderMailSMTP(t *testing.T) {
	r, err := RenderMail(MailSMTP)
	if err != nil {
		t.Fatalf("RenderMail() error = %v", err)
	}
	if !strings.Contains(string(r.InfraMail), `const mailProvider = "smtp"`) || r.Label != "SMTP" {
		t.Errorf("smtp recipe = %s", r.InfraMail)
	}
	if want := []string{"SMTP_HOST", "SMTP_PORT", "SMTP_TLS", "SMTP_USERNAME", "SMTP_PASSWORD"}; !slices.Equal(r.EnvKeys, want) {
		t.Errorf("EnvKeys = %v, want %v", r.EnvKeys, want)
	}
	if !slices.Equal(r.Modules, []string{"apistock.dev/modules/mail/smtp"}) {
		t.Errorf("Modules = %v", r.Modules)
	}
	if _, err := RenderMail("sendgrid"); err == nil {
		t.Error("RenderMail(sendgrid) error = nil")
	}
}

func TestReplaceBlock(t *testing.T) {
	src := []byte("A=1\n# aps:begin mail (managed)\nOLD=1\n# aps:end mail\nB=2\n")
	got, err := ReplaceBlock(src, MailBlock, []byte("# aps:begin mail\nNEW=1\n# aps:end mail\n"))
	if err != nil || string(got) != "A=1\n# aps:begin mail\nNEW=1\n# aps:end mail\nB=2\n" {
		t.Errorf("ReplaceBlock() = %q, %v", got, err)
	}
	block, err := Block(src, MailBlock)
	if err != nil || string(block) != "# aps:begin mail (managed)\nOLD=1\n# aps:end mail\n" {
		t.Errorf("Block() = %q, %v", block, err)
	}
	for _, bad := range []string{"A=1\n", "# aps:begin mail\nA=1\n", "# aps:begin mailbox\n# aps:end mail\n"} {
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
