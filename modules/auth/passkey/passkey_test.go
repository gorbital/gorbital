package passkey_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"apistock.dev/modules/auth/passkey"
	"apistock.dev/modules/auth/passkey/passkeytest"
)

const origin = "http://localhost:8080"

func newService(t *testing.T, apps ...passkey.AndroidApp) *passkey.Service {
	t.Helper()
	svc, err := passkey.New(passkey.Config{RPID: "localhost", RPDisplayName: "acme-api", Origins: []string{origin, "http://localhost:3000"}, AndroidApps: apps})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return svc
}

func newUser() passkey.User {
	return passkey.User{Handle: passkey.NewUserHandle(), Name: "ada@example.com", DisplayName: "ada@example.com"}
}

// register adds a passkey from auth to user.
func register(t *testing.T, svc *passkey.Service, auth *passkeytest.Authenticator, user *passkey.User) passkey.Credential {
	t.Helper()
	c, err := svc.BeginRegistration(*user)
	if err != nil {
		t.Fatalf("BeginRegistration() error = %v", err)
	}
	resp, err := auth.Create(c.Options)
	if err != nil {
		t.Fatal(err)
	}
	cred, err := svc.FinishRegistration(*user, c.State, resp)
	if err != nil {
		t.Fatalf("FinishRegistration() error = %v", err)
	}
	user.Credentials = append(user.Credentials, cred)
	return cred
}

// lookupOf finds user by handle and credential ID.
func lookupOf(user passkey.User) func(handle, id []byte) (passkey.User, error) {
	return func(handle, id []byte) (passkey.User, error) {
		if !bytes.Equal(handle, user.Handle) {
			return passkey.User{}, passkey.ErrUnknownCredential
		}
		for _, c := range user.Credentials {
			if bytes.Equal(c.ID, id) {
				return user, nil
			}
		}
		return passkey.User{}, passkey.ErrUnknownCredential
	}
}

func TestRegistrationAndSignIn(t *testing.T) {
	svc := newService(t)
	user := newUser()
	c, err := svc.BeginRegistration(user)
	if err != nil {
		t.Fatal(err)
	}
	var opts struct {
		PublicKey struct {
			AuthenticatorSelection map[string]any      `json:"authenticatorSelection"`
			Attestation            string              `json:"attestation"`
			RP                     struct{ ID string } `json:"rp"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(c.Options, &opts); err != nil || opts.PublicKey.AuthenticatorSelection["residentKey"] != "required" ||
		opts.PublicKey.AuthenticatorSelection["userVerification"] != "required" || opts.PublicKey.RP.ID != "localhost" {
		t.Errorf("registration options = %s", c.Options)
	}

	auth := passkeytest.New(origin)
	resp, _ := auth.Create(c.Options)
	cred, err := svc.FinishRegistration(user, c.State, resp)
	if err != nil || !bytes.Equal(cred.ID, auth.CredentialID()) || len(cred.Record) == 0 || cred.SignCount != 0 {
		t.Fatalf("FinishRegistration() = %+v, %v", cred, err)
	}
	user.Credentials = []passkey.Credential{cred}

	// Passwordless: the passkey names its account.
	lc, err := svc.BeginDiscoverableLogin()
	if err != nil {
		t.Fatal(err)
	}
	resp, _ = auth.Get(lc.Options)
	found, used, err := svc.FinishLogin(lc.State, resp, lookupOf(user))
	if err != nil || !bytes.Equal(found.Handle, user.Handle) || used.SignCount != 1 {
		t.Fatalf("FinishLogin(discoverable) = %+v, %+v, %v", found, used, err)
	}
	user.Credentials = []passkey.Credential{used}

	// A second factor for a known account.
	uc, err := svc.BeginUserLogin(user)
	if err != nil || !strings.Contains(string(uc.Options), "allowCredentials") {
		t.Fatalf("BeginUserLogin() = %s, %v", uc.Options, err)
	}
	second, _ := auth.Get(uc.Options)
	if _, used, err = svc.FinishLogin(uc.State, second, lookupOf(user)); err != nil || used.SignCount != 2 {
		t.Fatalf("FinishLogin(user) = %+v, %v", used, err)
	}

	// A response answers only its own challenge.
	other, _ := svc.BeginDiscoverableLogin()
	if _, _, err := svc.FinishLogin(other.State, second, lookupOf(user)); !errors.Is(err, passkey.ErrInvalidResponse) {
		t.Errorf("FinishLogin(response to another challenge) error = %v", err)
	}
	if _, _, err := svc.FinishLogin([]byte("{}"), second, lookupOf(user)); !errors.Is(err, passkey.ErrInvalidResponse) {
		t.Errorf("FinishLogin(bad state) error = %v", err)
	}
	if _, _, err := svc.FinishLogin(other.State, []byte(`{"id":"x"}`), lookupOf(user)); !errors.Is(err, passkey.ErrInvalidResponse) {
		t.Errorf("FinishLogin(garbage) error = %v", err)
	}
}

func TestRejectsWrongOriginAndMissingUserVerification(t *testing.T) {
	svc := newService(t)
	user := newUser()

	phishing := passkeytest.New("https://login.example.net")
	c, _ := svc.BeginRegistration(user)
	resp, _ := phishing.Create(c.Options)
	if _, err := svc.FinishRegistration(user, c.State, resp); !errors.Is(err, passkey.ErrInvalidResponse) {
		t.Errorf("FinishRegistration(wrong origin) error = %v", err)
	}

	unverified := passkeytest.New(origin)
	unverified.UserVerified = false
	c, _ = svc.BeginRegistration(user)
	resp, _ = unverified.Create(c.Options)
	if _, err := svc.FinishRegistration(user, c.State, resp); !errors.Is(err, passkey.ErrInvalidResponse) {
		t.Errorf("FinishRegistration(no user verification) error = %v", err)
	}

	auth := passkeytest.New(origin)
	register(t, svc, auth, &user)
	auth.UserVerified = false
	lc, _ := svc.BeginDiscoverableLogin()
	resp, _ = auth.Get(lc.Options)
	if _, _, err := svc.FinishLogin(lc.State, resp, lookupOf(user)); !errors.Is(err, passkey.ErrInvalidResponse) {
		t.Errorf("FinishLogin(no user verification) error = %v", err)
	}
}

func TestLookupErrors(t *testing.T) {
	svc := newService(t)
	user := newUser()
	auth := passkeytest.New(origin)
	register(t, svc, auth, &user)

	lc, _ := svc.BeginDiscoverableLogin()
	resp, _ := auth.Get(lc.Options)
	unknown := func([]byte, []byte) (passkey.User, error) { return passkey.User{}, passkey.ErrUnknownCredential }
	if _, _, err := svc.FinishLogin(lc.State, resp, unknown); !errors.Is(err, passkey.ErrInvalidResponse) {
		t.Errorf("FinishLogin(unknown credential) error = %v, want ErrInvalidResponse", err)
	}
	down := errors.New("database is down")
	failing := func([]byte, []byte) (passkey.User, error) { return passkey.User{}, down }
	lc, _ = svc.BeginDiscoverableLogin()
	resp, _ = auth.Get(lc.Options)
	if _, _, err := svc.FinishLogin(lc.State, resp, failing); !errors.Is(err, down) {
		t.Errorf("FinishLogin(lookup failure) error = %v, want the lookup's error", err)
	}
}

func TestSignCounter(t *testing.T) {
	svc := newService(t)
	user := newUser()
	auth := passkeytest.New(origin)
	register(t, svc, auth, &user)

	signIn := func() (passkey.Credential, error) {
		lc, _ := svc.BeginDiscoverableLogin()
		resp, _ := auth.Get(lc.Options)
		_, cred, err := svc.FinishLogin(lc.State, resp, lookupOf(user))
		return cred, err
	}
	cred, err := signIn()
	if err != nil {
		t.Fatal(err)
	}
	user.Credentials = []passkey.Credential{cred}
	auth.SetSignCount(0) // the next assertion reports 1 again, as a copied passkey would
	if cred, err := signIn(); !errors.Is(err, passkey.ErrCloneWarning) || cred.ID == nil {
		t.Errorf("FinishLogin(counter didn't increase) = %+v, %v; want ErrCloneWarning with the credential", cred, err)
	}

	synced := passkeytest.New(origin)
	synced.Synced = true
	syncedUser := newUser()
	stored := register(t, svc, synced, &syncedUser)
	if !stored.BackupEligible || !stored.BackupState {
		t.Errorf("synced passkey flags = %+v", stored)
	}
	for range 2 {
		lc, _ := svc.BeginDiscoverableLogin()
		resp, _ := synced.Get(lc.Options)
		if _, _, err := svc.FinishLogin(lc.State, resp, lookupOf(syncedUser)); err != nil {
			t.Errorf("FinishLogin(synced passkey, counter 0) error = %v", err)
		}
	}
}

func TestRegistrationExcludesExistingPasskeys(t *testing.T) {
	svc := newService(t)
	user := newUser()
	cred := register(t, svc, passkeytest.New(origin), &user)
	c, err := svc.BeginRegistration(user)
	if err != nil {
		t.Fatal(err)
	}
	var opts struct {
		PublicKey struct {
			ExcludeCredentials []struct{ ID string } `json:"excludeCredentials"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(c.Options, &opts); err != nil || len(opts.PublicKey.ExcludeCredentials) != 1 {
		t.Errorf("registration options for a user with a passkey = %s, want it excluded (credential %x)", c.Options, cred.ID)
	}
}

func TestConfig(t *testing.T) {
	for name, cfg := range map[string]passkey.Config{
		"no RP ID":               {RPDisplayName: "acme", Origins: []string{"https://example.com"}},
		"no origins":             {RPID: "example.com", RPDisplayName: "acme"},
		"http outside localhost": {RPID: "example.com", RPDisplayName: "acme", Origins: []string{"http://app.example.com"}},
		"another domain":         {RPID: "example.com", RPDisplayName: "acme", Origins: []string{"https://example.net"}},
		"lookalike domain":       {RPID: "example.com", RPDisplayName: "acme", Origins: []string{"https://evilexample.com"}},
		"path":                   {RPID: "example.com", RPDisplayName: "acme", Origins: []string{"https://app.example.com/login"}},
	} {
		if _, err := passkey.New(cfg); !errors.Is(err, passkey.ErrInvalidConfig) {
			t.Errorf("%s: New() error = %v, want ErrInvalidConfig", name, err)
		}
	}
	if _, err := passkey.New(passkey.Config{RPID: "example.com", RPDisplayName: "acme", Origins: []string{"https://example.com", "https://app.example.com"}}); err != nil {
		t.Errorf("New(valid production config) error = %v", err)
	}
}

func TestNativeApps(t *testing.T) {
	ids, err := passkey.ParseAppleAppIDs("ABCDE12345.com.example.app, ABCDE12345.com.example.beta")
	if err != nil || len(ids) != 2 {
		t.Fatalf("ParseAppleAppIDs() = %v, %v", ids, err)
	}
	for _, bad := range []string{"com.example.app", "abcde12345.com.example.app", "ABCDE12345."} {
		if _, err := passkey.ParseAppleAppIDs(bad); !errors.Is(err, passkey.ErrInvalidConfig) {
			t.Errorf("ParseAppleAppIDs(%q) error = %v", bad, err)
		}
	}
	aasa := passkey.AppleAppSiteAssociation(ids)
	if !strings.Contains(string(aasa), `"webcredentials"`) || !strings.Contains(string(aasa), "ABCDE12345.com.example.beta") || passkey.AppleAppSiteAssociation(nil) != nil {
		t.Errorf("AppleAppSiteAssociation() = %s", aasa)
	}

	fp := sha256.Sum256([]byte("signing certificate"))
	spec := "com.example.app=SHA256:" + passkey.FormatFingerprint(fp) + "+" + strings.ToLower(strings.ReplaceAll(passkey.FormatFingerprint(fp), ":", ""))
	apps, err := passkey.ParseAndroidApps(spec)
	if err != nil || len(apps) != 1 || apps[0].Package != "com.example.app" || len(apps[0].Fingerprints) != 2 || apps[0].Fingerprints[1] != fp {
		t.Fatalf("ParseAndroidApps() = %+v, %v", apps, err)
	}
	for _, bad := range []string{"com.example.app", "example=SHA256:AB", "com.example.app=SHA256:ZZ"} {
		if _, err := passkey.ParseAndroidApps(bad); !errors.Is(err, passkey.ErrInvalidConfig) {
			t.Errorf("ParseAndroidApps(%q) error = %v", bad, err)
		}
	}
	links := passkey.AssetLinks(apps)
	if !strings.Contains(string(links), "delegate_permission/common.get_login_creds") || !strings.Contains(string(links), passkey.FormatFingerprint(fp)) || passkey.AssetLinks(nil) != nil {
		t.Errorf("AssetLinks() = %s", links)
	}

	// The Android app's origin is accepted.
	svc := newService(t, apps...)
	user := newUser()
	android := passkeytest.New(passkey.AndroidOrigin(fp))
	register(t, svc, android, &user)
}
