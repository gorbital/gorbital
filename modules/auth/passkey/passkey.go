// Package passkey provides passkeys (WebAuthn) for apps: registration and
// sign-in ceremonies checked against the relying party's ID and allowed
// origins, credential records to store, and the association files native
// apps need (ADR-0044). It wraps github.com/go-webauthn/webauthn, so apps
// never import its types.
//
// A ceremony has two steps. Begin returns options for the client
// (navigator.credentials.create or get) and a state the app keeps on the
// server; Finish takes that state and the client's response, and verifies
// the response.
//
// Stability: pre-1.0 (ADR-0015).
package passkey

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

const (
	// CeremonyTTL is how long a started ceremony can be finished.
	CeremonyTTL = 5 * time.Minute
	// UserHandleSize is the size of a user handle, the account identifier
	// stored in passkeys.
	UserHandleSize = 64
)

// Errors returned by the package. Check them with [errors.Is].
var (
	// ErrInvalidConfig reports a configuration that can't be used.
	ErrInvalidConfig = errors.New("passkey: invalid configuration")
	// ErrInvalidResponse reports a response that fails verification: a
	// wrong origin, relying party, challenge or signature, user verification
	// missing, an expired ceremony, or an unknown credential.
	ErrInvalidResponse = errors.New("passkey: invalid passkey response")
	// ErrUnknownCredential is returned by a lookup function for a credential
	// or user handle it doesn't know.
	ErrUnknownCredential = errors.New("passkey: unknown credential")
	// ErrCloneWarning reports an authenticator whose signature counter didn't
	// increase: the passkey may have been copied. FinishLogin returns it with
	// the verified credential.
	ErrCloneWarning = errors.New("passkey: signature counter didn't increase")
)

// AndroidApp is an Android app allowed to use passkeys for the relying party.
type AndroidApp struct {
	// Package is the app's package name (applicationId).
	Package string
	// Fingerprints are the SHA-256 fingerprints of its signing certificates.
	Fingerprints [][sha256.Size]byte
}

// Config describes the relying party.
type Config struct {
	// RPID is the relying party ID: the site's registrable domain, such as
	// example.com, or localhost in development.
	RPID string
	// RPDisplayName names the app in passkey prompts.
	RPDisplayName string
	// Origins are the browser origins allowed to use passkeys, such as
	// https://app.example.com. Each must be https (or http://localhost), on
	// RPID or a subdomain of it.
	Origins []string
	// AppleAppIDs are TEAMID.bundle.id identifiers of iOS apps.
	AppleAppIDs []string
	// AndroidApps are Android apps allowed to use passkeys.
	AndroidApps []AndroidApp
}

// Service runs passkey ceremonies. It is safe for concurrent use.
type Service struct {
	w   *webauthn.WebAuthn
	cfg Config
}

// User is an account taking part in a ceremony.
type User struct {
	// Handle is the account's user handle ([NewUserHandle]), never its
	// database ID.
	Handle []byte
	// Name and DisplayName appear in passkey prompts, such as the email
	// address.
	Name        string
	DisplayName string
	// Credentials are the account's stored passkeys.
	Credentials []Credential
}

// Credential is a passkey to store.
type Credential struct {
	// ID is the credential ID, unique across the relying party.
	ID []byte
	// Record is the verified credential record: store it and pass it back
	// unchanged in User.Credentials.
	Record []byte
	// AAGUID identifies the authenticator model.
	AAGUID []byte
	// BackupEligible and BackupState report a passkey that can be, and is,
	// synced between devices.
	BackupEligible bool
	BackupState    bool
	// SignCount is the authenticator's signature counter; synced passkeys
	// report 0.
	SignCount uint32
}

// Ceremony is a started ceremony.
type Ceremony struct {
	// Options are for the client: pass them to navigator.credentials.create
	// or navigator.credentials.get ({"publicKey": ...}).
	Options json.RawMessage
	// State stays on the server until Finish: it holds the challenge.
	State []byte
}

// New checks cfg and returns a Service. It returns [ErrInvalidConfig].
func New(cfg Config) (*Service, error) {
	if cfg.RPID == "" || cfg.RPDisplayName == "" || len(cfg.Origins) == 0 {
		return nil, fmt.Errorf("%w: a relying party ID, display name and at least one origin are required", ErrInvalidConfig)
	}
	for _, origin := range cfg.Origins {
		if err := CheckOrigin(cfg.RPID, origin); err != nil {
			return nil, err
		}
	}
	var opaque []string
	for _, app := range cfg.AndroidApps {
		for _, fp := range app.Fingerprints {
			opaque = append(opaque, AndroidOrigin(fp))
		}
	}
	timeout := webauthn.TimeoutConfig{Enforce: true, Timeout: CeremonyTTL, TimeoutUVD: CeremonyTTL}
	w, err := webauthn.New(&webauthn.Config{
		RPID:                   cfg.RPID,
		RPDisplayName:          cfg.RPDisplayName,
		RPOrigins:              cfg.Origins,
		RPOpaqueOrigins:        opaque,
		AttestationPreference:  protocol.PreferNoAttestation,
		AuthenticatorSelection: selection(),
		Timeouts:               webauthn.TimeoutsConfig{Login: timeout, Registration: timeout},
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidConfig, err) //nolint:errorlint // the library's errors aren't API
	}
	return &Service{w: w, cfg: cfg}, nil
}

// Config returns the relying party configuration.
func (s *Service) Config() Config { return s.cfg }

// CheckOrigin reports whether origin can use passkeys for rpID: https (or
// http on localhost), with no path, on rpID or a subdomain of it.
func CheckOrigin(rpID, origin string) error {
	u, err := url.Parse(origin)
	switch {
	case err != nil || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "":
		return fmt.Errorf("%w: origin %q must be a scheme and host, such as https://app.example.com", ErrInvalidConfig, origin)
	case u.Scheme != "https" && (u.Scheme != "http" || u.Hostname() != "localhost"):
		return fmt.Errorf("%w: origin %q must use https (http only for localhost)", ErrInvalidConfig, origin)
	case u.Hostname() != rpID && !strings.HasSuffix(u.Hostname(), "."+rpID):
		return fmt.Errorf("%w: origin %q isn't on the relying party ID %q or a subdomain of it", ErrInvalidConfig, origin, rpID)
	}
	return nil
}

// AndroidOrigin returns the origin an Android app signed with the
// certificate fingerprint fp presents.
func AndroidOrigin(fp [sha256.Size]byte) string {
	return "android:apk-key-hash:" + base64.RawURLEncoding.EncodeToString(fp[:])
}

// NewUserHandle returns a random user handle.
func NewUserHandle() []byte {
	b := make([]byte, UserHandleSize)
	_, _ = rand.Read(b) // never fails (crypto/rand)
	return b
}

// BeginRegistration starts adding a passkey to u: a discoverable credential
// with user verification, attestation none, excluding u's passkeys.
func (s *Service) BeginRegistration(u User) (Ceremony, error) {
	wu, err := adapt(u)
	if err != nil {
		return Ceremony{}, err
	}
	exclude := make([]protocol.CredentialDescriptor, len(wu.creds))
	for i := range wu.creds {
		exclude[i] = wu.creds[i].Descriptor()
	}
	creation, session, err := s.w.BeginRegistration(wu,
		webauthn.WithExclusions(exclude),
		webauthn.WithAuthenticatorSelection(selection()),
		webauthn.WithConveyancePreference(protocol.PreferNoAttestation),
	)
	if err != nil {
		return Ceremony{}, fmt.Errorf("passkey: begin registration: %w", err)
	}
	return newCeremony(creation, session)
}

// FinishRegistration verifies the client's response to a registration
// started for u, and returns the credential to store. It returns
// [ErrInvalidResponse].
func (s *Service) FinishRegistration(u User, state, response []byte) (Credential, error) {
	wu, err := adapt(u)
	if err != nil {
		return Credential{}, err
	}
	session, err := decodeState(state)
	if err != nil {
		return Credential{}, err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(response)
	if err != nil {
		return Credential{}, invalid(err)
	}
	cred, err := s.w.CreateCredential(wu, session, parsed)
	if err != nil {
		return Credential{}, invalid(err)
	}
	return fromLibrary(cred)
}

// BeginDiscoverableLogin starts a passwordless sign-in: the client picks a
// passkey, which names its account.
func (s *Service) BeginDiscoverableLogin() (Ceremony, error) {
	assertion, session, err := s.w.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return Ceremony{}, fmt.Errorf("passkey: begin sign-in: %w", err)
	}
	return newCeremony(assertion, session)
}

// BeginUserLogin starts a sign-in limited to u's passkeys, such as a second
// factor after a password.
func (s *Service) BeginUserLogin(u User) (Ceremony, error) {
	wu, err := adapt(u)
	if err != nil {
		return Ceremony{}, err
	}
	assertion, session, err := s.w.BeginLogin(wu, webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return Ceremony{}, fmt.Errorf("passkey: begin sign-in: %w", err)
	}
	return newCeremony(assertion, session)
}

// FinishLogin verifies the client's response to a sign-in. lookup returns the
// account for a user handle and credential ID, or [ErrUnknownCredential]; a
// lookup error other than that is returned as is. It returns the account and
// the credential with its updated counter and flags, which the app stores.
// It returns [ErrInvalidResponse], or [ErrCloneWarning] with a verified
// credential whose counter didn't increase.
func (s *Service) FinishLogin(state, response []byte, lookup func(userHandle, credentialID []byte) (User, error)) (User, Credential, error) {
	session, err := decodeState(state)
	if err != nil {
		return User{}, Credential{}, err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(response)
	if err != nil {
		return User{}, Credential{}, invalid(err)
	}
	var (
		found     User
		lookupErr error
	)
	find := func(handle, credentialID []byte) (webauthnUser, error) {
		u, err := lookup(handle, credentialID)
		if err != nil {
			lookupErr = err
			return webauthnUser{}, err
		}
		found = u
		return adapt(u)
	}

	var cred *webauthn.Credential
	if len(session.UserID) == 0 {
		_, cred, err = s.w.ValidatePasskeyLogin(func(rawID, handle []byte) (webauthn.User, error) { return find(handle, rawID) }, session, parsed)
	} else {
		var wu webauthnUser
		if wu, err = find(session.UserID, parsed.RawID); err == nil {
			cred, err = s.w.ValidateLogin(wu, session, parsed)
		}
	}
	switch {
	case lookupErr != nil && !errors.Is(lookupErr, ErrUnknownCredential):
		return User{}, Credential{}, lookupErr
	case err != nil:
		return User{}, Credential{}, invalid(err)
	}
	out, err := fromLibrary(cred)
	if err != nil {
		return User{}, Credential{}, err
	}
	if cred.Authenticator.CloneWarning {
		return found, out, ErrCloneWarning
	}
	return found, out, nil
}

func selection() protocol.AuthenticatorSelection {
	required := true
	return protocol.AuthenticatorSelection{
		ResidentKey:        protocol.ResidentKeyRequirementRequired,
		RequireResidentKey: &required,
		UserVerification:   protocol.VerificationRequired,
	}
}

func newCeremony(options any, session *webauthn.SessionData) (Ceremony, error) {
	opts, err := json.Marshal(options)
	if err != nil {
		return Ceremony{}, err
	}
	state, err := json.Marshal(session)
	if err != nil {
		return Ceremony{}, err
	}
	return Ceremony{Options: opts, State: state}, nil
}

func decodeState(state []byte) (webauthn.SessionData, error) {
	var session webauthn.SessionData
	if err := json.Unmarshal(state, &session); err != nil || session.Challenge == "" {
		return webauthn.SessionData{}, fmt.Errorf("%w: unreadable ceremony state", ErrInvalidResponse)
	}
	return session, nil
}

func invalid(err error) error {
	return fmt.Errorf("%w: %v", ErrInvalidResponse, err) //nolint:errorlint // the library's errors aren't API
}

func fromLibrary(c *webauthn.Credential) (Credential, error) {
	record, err := json.Marshal(c)
	if err != nil {
		return Credential{}, err
	}
	return Credential{
		ID: c.ID, Record: record, AAGUID: c.Authenticator.AAGUID,
		BackupEligible: c.Flags.BackupEligible, BackupState: c.Flags.BackupState, SignCount: c.Authenticator.SignCount,
	}, nil
}

// webauthnUser adapts a User to the library.
type webauthnUser struct {
	u     User
	creds []webauthn.Credential
}

func adapt(u User) (webauthnUser, error) {
	if len(u.Handle) == 0 || len(u.Handle) > UserHandleSize {
		return webauthnUser{}, fmt.Errorf("passkey: a user handle of 1 to %d bytes is required", UserHandleSize)
	}
	wu := webauthnUser{u: u, creds: make([]webauthn.Credential, len(u.Credentials))}
	for i, c := range u.Credentials {
		if err := json.Unmarshal(c.Record, &wu.creds[i]); err != nil {
			return webauthnUser{}, fmt.Errorf("passkey: unreadable credential record: %w", err)
		}
	}
	return wu, nil
}

func (w webauthnUser) WebAuthnID() []byte                         { return w.u.Handle }
func (w webauthnUser) WebAuthnName() string                       { return w.u.Name }
func (w webauthnUser) WebAuthnDisplayName() string                { return w.u.DisplayName }
func (w webauthnUser) WebAuthnCredentials() []webauthn.Credential { return w.creds }
