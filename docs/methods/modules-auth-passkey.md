# modules/auth/passkey

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/modules/auth/passkey"
```

Package passkey provides passkeys (WebAuthn) for apps: registration and sign-in ceremonies checked against the relying party's ID and allowed origins, credential records to store, and the association files native apps need (ADR-0044). It wraps github.com/go-webauthn/webauthn, so apps never import its types.

A ceremony has two steps. Begin returns options for the client (navigator.credentials.create or get) and a state the app keeps on the server; Finish takes that state and the client's response, and verifies the response.

Stability: stable (ADR-0015, ADR-0054).

## Contents

- Constants: [`CeremonyTTL`](#CeremonyTTL), [`UserHandleSize`](#UserHandleSize)
- Variables: [`ErrInvalidConfig`](#ErrInvalidConfig), [`ErrInvalidResponse`](#ErrInvalidResponse), [`ErrUnknownCredential`](#ErrUnknownCredential), [`ErrCloneWarning`](#ErrCloneWarning)
- Functions: [`AndroidOrigin`](#AndroidOrigin), [`AppleAppSiteAssociation`](#AppleAppSiteAssociation), [`AssetLinks`](#AssetLinks), [`CheckOrigin`](#CheckOrigin), [`FormatFingerprint`](#FormatFingerprint), [`NewUserHandle`](#NewUserHandle), [`ParseAppleAppIDs`](#ParseAppleAppIDs)
- Types:
  - [`AndroidApp`](#AndroidApp): [`ParseAndroidApps`](#ParseAndroidApps)
  - [`Ceremony`](#Ceremony)
  - [`Config`](#Config)
  - [`Credential`](#Credential)
  - [`Service`](#Service): [`New`](#New), [`Service.BeginDiscoverableLogin`](#Service.BeginDiscoverableLogin), [`Service.BeginRegistration`](#Service.BeginRegistration), [`Service.BeginUserLogin`](#Service.BeginUserLogin), [`Service.Config`](#Service.Config), [`Service.FinishLogin`](#Service.FinishLogin), [`Service.FinishRegistration`](#Service.FinishRegistration)
  - [`User`](#User)

## Constants

<a id="CeremonyTTL"></a>
<a id="UserHandleSize"></a>

```go
const (
	// CeremonyTTL is how long a started ceremony can be finished.
	CeremonyTTL = 5 * time.Minute
	// UserHandleSize is the size of a user handle, the account identifier
	// stored in passkeys.
	UserHandleSize = 64
)
```

*Since `v0.1.0`*

## Variables

<a id="ErrInvalidConfig"></a>
<a id="ErrInvalidResponse"></a>
<a id="ErrUnknownCredential"></a>
<a id="ErrCloneWarning"></a>

```go
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
```

Errors returned by the package. Check them with [errors.Is](https://pkg.go.dev/errors#Is).

*Since `v0.1.0`*

## Functions

<a id="AndroidOrigin"></a>

### func AndroidOrigin

```go
func AndroidOrigin(fp [sha256.Size]byte) string
```

AndroidOrigin returns the origin an Android app signed with the certificate fingerprint fp presents.

*Since `v0.1.0`*

<a id="AppleAppSiteAssociation"></a>

### func AppleAppSiteAssociation

```go
func AppleAppSiteAssociation(appIDs []string) []byte
```

AppleAppSiteAssociation returns the apple-app-site-association document that lets iOS apps use passkeys for the relying party, or nil without apps. Serve it at /.well-known/apple-app-site-association with the JSON content type.

*Since `v0.1.0`*

<a id="AssetLinks"></a>

### func AssetLinks

```go
func AssetLinks(apps []AndroidApp) []byte
```

AssetLinks returns the Digital Asset Links statements that let Android apps use passkeys for the relying party, or nil without apps. Serve it at /.well-known/assetlinks.json.

*Since `v0.1.0`*

<a id="CheckOrigin"></a>

### func CheckOrigin

```go
func CheckOrigin(rpID, origin string) error
```

CheckOrigin reports whether origin can use passkeys for rpID: https (or http on localhost), with no path, on rpID or a subdomain of it.

*Since `v0.1.0`*

<a id="FormatFingerprint"></a>

### func FormatFingerprint

```go
func FormatFingerprint(fp [sha256.Size]byte) string
```

FormatFingerprint writes fp as AB:CD:…, the form assetlinks.json uses.

*Since `v0.1.0`*

<a id="NewUserHandle"></a>

### func NewUserHandle

```go
func NewUserHandle() []byte
```

NewUserHandle returns a random user handle.

*Since `v0.1.0`*

<a id="ParseAppleAppIDs"></a>

### func ParseAppleAppIDs

```go
func ParseAppleAppIDs(spec string) ([]string, error)
```

ParseAppleAppIDs parses comma-separated TEAMID.bundle.id identifiers, such as ABCDE12345.com.example.app.

*Since `v0.1.0`*

## Types

<a id="AndroidApp"></a>
<a id="AndroidApp.Package"></a>
<a id="AndroidApp.Fingerprints"></a>

### type AndroidApp

```go
type AndroidApp struct {
	// Package is the app's package name (applicationId).
	Package string
	// Fingerprints are the SHA-256 fingerprints of its signing certificates.
	Fingerprints [][sha256.Size]byte
}
```

AndroidApp is an Android app allowed to use passkeys for the relying party.

*Since `v0.1.0`*

<a id="ParseAndroidApps"></a>

#### func ParseAndroidApps

```go
func ParseAndroidApps(spec string) ([]AndroidApp, error)
```

ParseAndroidApps parses comma-separated package=fingerprint entries, such as com.example.app=SHA256:AB:CD:…; several fingerprints of one app are joined with +. Fingerprints are 32 bytes in hex, with or without colons and the SHA256: prefix.

*Since `v0.1.0`*

<a id="Ceremony"></a>
<a id="Ceremony.Options"></a>
<a id="Ceremony.State"></a>

### type Ceremony

```go
type Ceremony struct {
	// Options are for the client: pass them to navigator.credentials.create
	// or navigator.credentials.get ({"publicKey": ...}).
	Options json.RawMessage
	// State stays on the server until Finish: it holds the challenge.
	State []byte
}
```

Ceremony is a started ceremony.

*Since `v0.1.0`*

<a id="Config"></a>
<a id="Config.RPID"></a>
<a id="Config.RPDisplayName"></a>
<a id="Config.Origins"></a>
<a id="Config.AppleAppIDs"></a>
<a id="Config.AndroidApps"></a>

### type Config

```go
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
```

Config describes the relying party.

*Since `v0.1.0`*

<a id="Credential"></a>
<a id="Credential.ID"></a>
<a id="Credential.Record"></a>
<a id="Credential.AAGUID"></a>
<a id="Credential.BackupEligible"></a>
<a id="Credential.BackupState"></a>
<a id="Credential.SignCount"></a>

### type Credential

```go
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
```

Credential is a passkey to store.

*Since `v0.1.0`*

<a id="Service"></a>

### type Service

```go
type Service struct {
	// contains filtered or unexported fields
}
```

Service runs passkey ceremonies. It is safe for concurrent use.

*Since `v0.1.0`*

<a id="New"></a>

#### func New

```go
func New(cfg Config) (*Service, error)
```

New checks cfg and returns a Service. It returns [ErrInvalidConfig](#ErrInvalidConfig).

*Since `v0.1.0`*

<a id="Service.BeginDiscoverableLogin"></a>

#### func (*Service) BeginDiscoverableLogin

```go
func (s *Service) BeginDiscoverableLogin() (Ceremony, error)
```

BeginDiscoverableLogin starts a passwordless sign-in: the client picks a passkey, which names its account.

*Since `v0.1.0`*

<a id="Service.BeginRegistration"></a>

#### func (*Service) BeginRegistration

```go
func (s *Service) BeginRegistration(u User) (Ceremony, error)
```

BeginRegistration starts adding a passkey to u: a discoverable credential with user verification, attestation none, excluding u's passkeys.

*Since `v0.1.0`*

<a id="Service.BeginUserLogin"></a>

#### func (*Service) BeginUserLogin

```go
func (s *Service) BeginUserLogin(u User) (Ceremony, error)
```

BeginUserLogin starts a sign-in limited to u's passkeys, such as a second factor after a password.

*Since `v0.1.0`*

<a id="Service.Config"></a>

#### func (*Service) Config

```go
func (s *Service) Config() Config
```

Config returns the relying party configuration.

*Since `v0.1.0`*

<a id="Service.FinishLogin"></a>

#### func (*Service) FinishLogin

```go
func (s *Service) FinishLogin(state, response []byte, lookup func(userHandle, credentialID []byte) (User, error)) (User, Credential, error)
```

FinishLogin verifies the client's response to a sign-in. lookup returns the account for a user handle and credential ID, or [ErrUnknownCredential](#ErrUnknownCredential); a lookup error other than that is returned as is. It returns the account and the credential with its updated counter and flags, which the app stores. It returns [ErrInvalidResponse](#ErrInvalidResponse), or [ErrCloneWarning](#ErrCloneWarning) with a verified credential whose counter didn't increase.

*Since `v0.1.0`*

<a id="Service.FinishRegistration"></a>

#### func (*Service) FinishRegistration

```go
func (s *Service) FinishRegistration(u User, state, response []byte) (Credential, error)
```

FinishRegistration verifies the client's response to a registration started for u, and returns the credential to store. It returns [ErrInvalidResponse](#ErrInvalidResponse).

*Since `v0.1.0`*

<a id="User"></a>
<a id="User.Handle"></a>
<a id="User.Name"></a>
<a id="User.DisplayName"></a>
<a id="User.Credentials"></a>

### type User

```go
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
```

User is an account taking part in a ceremony.

*Since `v0.1.0`*
