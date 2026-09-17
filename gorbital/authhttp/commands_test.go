package authhttp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gorbital.dev/actor"
	"gorbital.dev/config"
	"gorbital.dev/gorbital"
	authlib "gorbital.dev/modules/auth"
)

// TestCommands runs sign-in's commands as gorbital.Main serves them, with
// the output and messages of a v0.1 app's cmd/api.
func TestCommands(t *testing.T) {
	a := newApp(t, nil)
	ctx := actor.With(context.Background(), actor.System("test"))

	wantUsage := map[string][]string{
		"grant-role": {"ada@example.com"}, "revoke-role": {}, "reset-mfa": {"a@example.com", "b@example.com"},
	}
	for name, args := range wantUsage {
		if _, err := runCommand(t, a, name, args...); !errors.Is(err, gorbital.ErrUsage) || !strings.Contains(err.Error(), "usage: "+name+" <email>") {
			t.Errorf("%s %v error = %v, want a usage error (exit 2)", name, args, err)
		}
	}
	names := map[string]bool{}
	for _, c := range a.auth.Commands() {
		names[c.Name] = true
		if !strings.HasPrefix(c.Usage, c.Name) || c.Run == nil {
			t.Errorf("command %q: usage %q", c.Name, c.Usage)
		}
	}
	for _, name := range []string{"roles", "grant-role", "revoke-role", "reset-mfa", "rotate-auth-keys", "auth-providers"} {
		if !names[name] {
			t.Errorf("no %s command", name)
		}
	}

	out, err := runCommand(t, a, "roles")
	for _, want := range []string{
		"platform_admin\n  Operates the platform: every /ops permission\n  permissions: ops.auth.read, ops.auth.write, ops.service_accounts.read, ops.service_accounts.write\n",
		"ops_viewer\n  Reads operational data without changing anything\n  permissions: ops.auth.read, ops.service_accounts.read\n",
		"user\n  Held by every signed-in user without a grant; by API keys only within their scopes\n  permissions: flags.flag.read, projects.project.read, projects.project.write\n",
	} {
		if err != nil || !strings.Contains(out, want) {
			t.Errorf("roles = %q, %v; want %q", out, err, want)
		}
	}

	ada, err := a.Auth().CreateUser(ctx, "ada@example.com", testPassword, true)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := runCommand(t, a, "grant-role", "ada@example.com", "platform_admin"); err != nil || out != "✓ ada@example.com now has roles: platform_admin\n" {
		t.Errorf("grant-role = %q, %v", out, err)
	}
	if _, err := runCommand(t, a, "grant-role", "ada@example.com", "user"); err == nil || err.Error() != "every account holds the user role without a grant" {
		t.Errorf("grant-role user error = %v", err)
	}
	if out, err := runCommand(t, a, "revoke-role", "ada@example.com", "platform_admin"); err != nil || out != "✓ ada@example.com now has roles: none\n" {
		t.Errorf("revoke-role = %q, %v", out, err)
	}

	if _, err := runCommand(t, a, "reset-mfa", "ada@example.com"); err == nil || err.Error() != "ada@example.com doesn't have two-factor authentication on" {
		t.Errorf("reset-mfa without 2FA error = %v", err)
	}
	if _, err := runCommand(t, a, "reset-mfa", "nobody@example.com"); err == nil || err.Error() != "no account uses nobody@example.com" {
		t.Errorf("reset-mfa for an unknown account error = %v", err)
	}
	if err := a.Auth().GrantRole(ctx, ada.ID, "platform_admin"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.Auth().EnrollTOTP(ctx, ada.ID); err != nil {
		t.Fatal(err)
	}
	want := "✓ Two-factor authentication is off for ada@example.com, and its sessions ended.\n" +
		"  Its roles require two-factor authentication: it has to turn it on again before using them.\n"
	if out, err := runCommand(t, a, "reset-mfa", "ada@example.com"); err != nil || out != want {
		t.Errorf("reset-mfa = %q, %v; want %q", out, err, want)
	}
	if events := auditEvents(t, a.url, "auth.mfa.reset"); len(events) != 1 || events[0].ActorKind != "system" || events[0].ActorID != "cli" {
		t.Errorf("auth.mfa.reset events = %+v, want one by the cli system actor", events)
	}

	// Rotation: a secret encrypted with the old key is re-encrypted with the
	// new one, listed first.
	if _, _, err := a.Auth().EnrollTOTP(ctx, ada.ID); err != nil {
		t.Fatal(err)
	}
	rotated := a.cfg
	next := authlib.NewKeyringKey("next")
	rotated.Auth.EncryptionKeys = config.NewSecret(next + "," + testEncryptionKeys)
	rotate := command(t, a, "rotate-auth-keys")
	var b strings.Builder
	if err := rotate.Run(context.Background(), rotated, nil, &b); err != nil || !strings.HasPrefix(b.String(), `✓ 1 secrets re-encrypted with key "next".`) {
		t.Errorf("rotate-auth-keys = %q, %v", b.String(), err)
	}
	empty := a.cfg
	empty.Auth.EncryptionKeys = config.Secret{}
	if err := rotate.Run(context.Background(), empty, nil, &b); err == nil || !strings.Contains(err.Error(), "AUTH_ENCRYPTION_KEYS is empty") {
		t.Errorf("rotate-auth-keys without keys error = %v", err)
	}

	if out, err := runCommand(t, a, "auth-providers"); err != nil || !strings.HasPrefix(out, "Sign-in methods\n  ✓ Email and password") || !strings.Contains(out, "set GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET in .env") {
		t.Errorf("auth-providers = %q, %v", out, err)
	}
}

// TestCommandsBeforeSetup: the commands refuse to run without the catalog
// gorbital.Main hands the authenticator.
func TestCommandsBeforeSetup(t *testing.T) {
	for _, c := range New().Commands() {
		if c.Name == "auth-providers" {
			continue
		}
		args := []string{"ada@example.com", "platform_admin"}[:map[string]int{"reset-mfa": 1, "grant-role": 2, "revoke-role": 2}[c.Name]]
		if err := c.Run(context.Background(), gorbital.Config{}, args, &strings.Builder{}); err == nil || !strings.Contains(err.Error(), "before Setup") {
			t.Errorf("%s before Setup error = %v", c.Name, err)
		}
	}
}

// command returns the authenticator's command name.
func command(t *testing.T, a *testApp, name string) gorbital.Command {
	t.Helper()
	for _, c := range a.auth.Commands() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no command %q", name)
	return gorbital.Command{}
}
