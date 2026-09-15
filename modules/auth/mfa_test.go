package auth_test

import (
	"encoding/base32"
	"errors"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	authlib "gorbital.dev/modules/auth"
)

// rfcSecret is RFC 6238's SHA-1 test key, "12345678901234567890".
var rfcSecret = base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))

func TestTOTPMatchesRFC6238(t *testing.T) {
	// RFC 6238 appendix B, SHA-1; the 6-digit codes are the last six digits
	// of the 8-digit ones.
	for unix, want := range map[int64]string{
		59:          "287082",
		1111111109:  "081804",
		1111111111:  "050471",
		1234567890:  "005924",
		2000000000:  "279037",
		20000000000: "353130",
	} {
		if got, err := authlib.TOTPCode(rfcSecret, time.Unix(unix, 0)); err != nil || got != want {
			t.Errorf("TOTPCode(t=%d) = %q, %v, want %q", unix, got, err, want)
		}
	}
}

func TestVerifyTOTP(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	secret := authlib.NewTOTPSecret()
	code, _ := authlib.TOTPCode(secret, now)

	step, ok := authlib.VerifyTOTP(secret, code, now)
	if !ok || step != authlib.TOTPStep(now) {
		t.Fatalf("VerifyTOTP(current code) = %d, %v", step, ok)
	}
	if step, ok := authlib.VerifyTOTP(strings.ToLower(secret), code[:3]+" "+code[3:], now.Add(30*time.Second)); !ok || step != authlib.TOTPStep(now) {
		t.Errorf("VerifyTOTP(one step later, spaced code, lowercase secret) = %d, %v; want the earlier step accepted", step, ok)
	}
	for name, at := range map[string]time.Time{"two steps later": now.Add(61 * time.Second), "two steps earlier": now.Add(-61 * time.Second)} {
		if _, ok := authlib.VerifyTOTP(secret, code, at); ok {
			t.Errorf("VerifyTOTP() %s accepted the code", name)
		}
	}
	for _, bad := range []string{"", "12345", "1234567", "abcdef"} {
		if _, ok := authlib.VerifyTOTP(secret, bad, now); ok {
			t.Errorf("VerifyTOTP(%q) accepted", bad)
		}
	}
	if _, ok := authlib.VerifyTOTP("not base32!", code, now); ok {
		t.Error("VerifyTOTP() with an invalid secret accepted")
	}
	if _, err := authlib.TOTPCode("AAAA", now); !errors.Is(err, authlib.ErrInvalidTOTPSecret) {
		t.Errorf("TOTPCode(short secret) error = %v", err)
	}
	if a, b := authlib.NewTOTPSecret(), authlib.NewTOTPSecret(); a == b || len(a) != 32 {
		t.Errorf("NewTOTPSecret() = %q, %q; want distinct 32-character secrets", a, b)
	}
}

func TestTOTPURI(t *testing.T) {
	u, err := url.Parse(authlib.TOTPURI("acme api", "ada@example.com", "JBSWY3DPEHPK3PXP"))
	if err != nil || u.Scheme != "otpauth" || u.Host != "totp" {
		t.Fatalf("TOTPURI() = %v, %v", u, err)
	}
	q := u.Query()
	if u.Path != "/acme api:ada@example.com" || q.Get("secret") != "JBSWY3DPEHPK3PXP" || q.Get("issuer") != "acme api" ||
		q.Get("digits") != "6" || q.Get("period") != "30" || q.Get("algorithm") != "SHA1" {
		t.Errorf("TOTPURI() = %s", u)
	}
}

func TestKeyring(t *testing.T) {
	old := authlib.NewKeyringKey("k1")
	keys, err := authlib.ParseKeyring(old)
	if err != nil || keys.CurrentKeyID() != "k1" {
		t.Fatalf("ParseKeyring() = %v, %v", keys, err)
	}
	id, ciphertext, err := keys.Encrypt([]byte("JBSWY3DPEHPK3PXP"), []byte("usr_1:totp"))
	if err != nil || id != "k1" || strings.Contains(string(ciphertext), "JBSWY3DPEHPK3PXP") {
		t.Fatalf("Encrypt() = %q, %q, %v", id, ciphertext, err)
	}
	if plain, err := keys.Decrypt(id, ciphertext, []byte("usr_1:totp")); err != nil || string(plain) != "JBSWY3DPEHPK3PXP" {
		t.Errorf("Decrypt() = %q, %v", plain, err)
	}
	if _, err := keys.Decrypt(id, ciphertext, []byte("usr_2:totp")); !errors.Is(err, authlib.ErrDecrypt) {
		t.Errorf("Decrypt() for another user error = %v, want ErrDecrypt", err)
	}
	tampered := slices.Clone(ciphertext)
	tampered[len(tampered)-1] ^= 1
	if _, err := keys.Decrypt(id, tampered, []byte("usr_1:totp")); !errors.Is(err, authlib.ErrDecrypt) {
		t.Errorf("Decrypt(tampered) error = %v, want ErrDecrypt", err)
	}
	if _, err := keys.Decrypt(id, ciphertext[:5], []byte("usr_1:totp")); !errors.Is(err, authlib.ErrDecrypt) {
		t.Errorf("Decrypt(short) error = %v, want ErrDecrypt", err)
	}

	// Rotation: a new key encrypts, the old one still decrypts.
	rotated, err := authlib.ParseKeyring(authlib.NewKeyringKey("k2") + ", " + old)
	if err != nil || rotated.CurrentKeyID() != "k2" {
		t.Fatalf("ParseKeyring(rotated) = %v, %v", rotated, err)
	}
	if plain, err := rotated.Decrypt("k1", ciphertext, []byte("usr_1:totp")); err != nil || string(plain) != "JBSWY3DPEHPK3PXP" {
		t.Errorf("Decrypt() with the old key after rotation = %q, %v", plain, err)
	}
	if _, err := keys.Decrypt("k2", ciphertext, []byte("usr_1:totp")); !errors.Is(err, authlib.ErrUnknownKey) {
		t.Errorf("Decrypt(unknown key) error = %v, want ErrUnknownKey", err)
	}

	for _, spec := range []string{"", " , ", "k1", "K1:" + strings.SplitN(old, ":", 2)[1], "k1:c2hvcnQ=", "k1:not base64", old + "," + old} {
		if _, err := authlib.ParseKeyring(spec); !errors.Is(err, authlib.ErrInvalidKeyring) {
			t.Errorf("ParseKeyring(%q) error = %v, want ErrInvalidKeyring", spec, err)
		}
	}
}

func TestRecoveryCodes(t *testing.T) {
	codes := authlib.NewRecoveryCodes()
	if len(codes) != authlib.RecoveryCodeCount || len(slices.Compact(slices.Sorted(slices.Values(codes)))) != authlib.RecoveryCodeCount {
		t.Fatalf("NewRecoveryCodes() = %v, want %d distinct codes", codes, authlib.RecoveryCodeCount)
	}
	for _, c := range codes {
		if len(c) != 11 || c[5] != '-' {
			t.Errorf("recovery code %q, want xxxxx-xxxxx", c)
		}
	}
	h := authlib.HashRecoveryCode("usr_1", codes[0])
	for _, variant := range []string{strings.ToUpper(codes[0]), strings.ReplaceAll(codes[0], "-", " "), " " + strings.ReplaceAll(codes[0], "-", "") + " "} {
		if !slices.Equal(authlib.HashRecoveryCode("usr_1", variant), h) {
			t.Errorf("HashRecoveryCode(%q) differs from %q's", variant, codes[0])
		}
	}
	if slices.Equal(authlib.HashRecoveryCode("usr_2", codes[0]), h) {
		t.Error("the same code hashes the same for two users")
	}
}

func TestCatalogRequireMFA(t *testing.T) {
	c := authlib.NewCatalog()
	c.Permission("ops.settings.read", "Read settings")
	c.Permission("ops.settings.write", "Change settings")
	c.Permission("projects.read", "Read projects")
	c.Role("platform_admin", "Operators", "ops.settings.read", "ops.settings.write")
	c.Role("support", "Support staff", "ops.settings.read", "projects.read")
	c.RequireMFA("platform_admin")

	if !c.RequiresMFA("support", "platform_admin") || c.RequiresMFA("support") {
		t.Error("RequiresMFA() doesn't reflect RequireMFA")
	}
	granted, stepUp := c.PermissionsFor([]string{"platform_admin", "support"}, false)
	if !slices.Equal(granted, []string{"ops.settings.read", "projects.read"}) || !slices.Equal(stepUp, []string{"ops.settings.write"}) {
		t.Errorf("PermissionsFor(unverified) = %v, %v", granted, stepUp)
	}
	granted, stepUp = c.PermissionsFor([]string{"platform_admin", "support"}, true)
	if len(granted) != 3 || len(stepUp) != 0 {
		t.Errorf("PermissionsFor(verified) = %v, %v", granted, stepUp)
	}
	if granted, stepUp := c.PermissionsFor([]string{"unknown"}, true); granted != nil || stepUp != nil {
		t.Errorf("PermissionsFor(unknown role) = %v, %v", granted, stepUp)
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Error("RequireMFA(undeclared role) didn't panic")
			}
		}()
		c.RequireMFA("nobody")
	}()
	c.Freeze()
	func() {
		defer func() {
			if recover() == nil {
				t.Error("RequireMFA after Freeze didn't panic")
			}
		}()
		c.RequireMFA("support")
	}()
}
