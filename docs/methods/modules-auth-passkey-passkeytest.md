# modules/auth/passkey/passkeytest

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/modules/auth/passkey/passkeytest"
```

Package passkeytest is a software passkey authenticator for tests. It answers registration and sign-in options with real, signed responses (P-256 keys, attestation none), so tests exercise the same verification browsers and devices go through.

Stability: stable, for tests only: the API follows the compatibility promise; what the helpers do inside a test may change (ADR-0015, ADR-0054).

## Contents

- Types:
  - [`Authenticator`](#Authenticator): [`New`](#New), [`Authenticator.Create`](#Authenticator.Create), [`Authenticator.CredentialID`](#Authenticator.CredentialID), [`Authenticator.Get`](#Authenticator.Get), [`Authenticator.SetSignCount`](#Authenticator.SetSignCount)

## Types

<a id="Authenticator"></a>
<a id="Authenticator.UserVerified"></a>
<a id="Authenticator.Synced"></a>

### type Authenticator

```go
type Authenticator struct {

	// UserVerified sets the user-verification flag (default true). Set it
	// false to test a passkey used without verifying the person.
	UserVerified bool
	// Synced makes it behave like a synced passkey: backup flags set and a
	// signature counter that stays 0.
	Synced bool
	// contains filtered or unexported fields
}
```

Authenticator holds one passkey and answers ceremonies with it.

*Since `v0.1.0`*

<a id="New"></a>

#### func New

```go
func New(origin string) *Authenticator
```

New returns an authenticator that reports origin, such as [http://localhost:8080](http://localhost:8080) or an android:apk-key-hash: origin.

*Since `v0.1.0`*

<a id="Authenticator.Create"></a>

#### func (*Authenticator) Create

```go
func (a *Authenticator) Create(options []byte) ([]byte, error)
```

Create answers registration options ({"publicKey": ...}) with a new passkey, returning the JSON a browser would send.

*Since `v0.1.0`*

<a id="Authenticator.CredentialID"></a>

#### func (*Authenticator) CredentialID

```go
func (a *Authenticator) CredentialID() []byte
```

CredentialID returns the passkey's credential ID, after Create.

*Since `v0.1.0`*

<a id="Authenticator.Get"></a>

#### func (*Authenticator) Get

```go
func (a *Authenticator) Get(options []byte) ([]byte, error)
```

Get answers sign-in options ({"publicKey": ...}) with a signed assertion, returning the JSON a browser would send.

*Since `v0.1.0`*

<a id="Authenticator.SetSignCount"></a>

#### func (*Authenticator) SetSignCount

```go
func (a *Authenticator) SetSignCount(n uint32)
```

SetSignCount sets the signature counter; the next Get reports n+1.

*Since `v0.1.0`*
