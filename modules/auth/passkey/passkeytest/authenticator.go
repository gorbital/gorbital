// Package passkeytest is a software passkey authenticator for tests. It
// answers registration and sign-in options with real, signed responses (P-256
// keys, attestation none), so tests exercise the same verification browsers
// and devices go through.
//
// Stability: pre-1.0 (ADR-0015).
package passkeytest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/fxamacker/cbor/v2"
)

// Authenticator flags (WebAuthn §6.1).
const (
	flagUserPresent    = 0x01
	flagUserVerified   = 0x04
	flagBackupEligible = 0x08
	flagBackupState    = 0x10
	flagAttestedData   = 0x40
)

var b64 = base64.RawURLEncoding

// Authenticator holds one passkey and answers ceremonies with it.
type Authenticator struct {
	origin       string
	key          *ecdsa.PrivateKey
	credentialID []byte
	userHandle   []byte
	signCount    uint32

	// UserVerified sets the user-verification flag (default true). Set it
	// false to test a passkey used without verifying the person.
	UserVerified bool
	// Synced makes it behave like a synced passkey: backup flags set and a
	// signature counter that stays 0.
	Synced bool
}

// New returns an authenticator that reports origin, such as
// http://localhost:8080 or an android:apk-key-hash: origin.
func New(origin string) *Authenticator {
	return &Authenticator{origin: origin, UserVerified: true}
}

// CredentialID returns the passkey's credential ID, after Create.
func (a *Authenticator) CredentialID() []byte { return a.credentialID }

// SetSignCount sets the signature counter; the next Get reports n+1.
func (a *Authenticator) SetSignCount(n uint32) { a.signCount = n }

// Create answers registration options ({"publicKey": ...}) with a new
// passkey, returning the JSON a browser would send.
func (a *Authenticator) Create(options []byte) ([]byte, error) {
	var opts struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
			RP        struct {
				ID string `json:"id"`
			} `json:"rp"`
			User struct {
				ID string `json:"id"`
			} `json:"user"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(options, &opts); err != nil {
		return nil, fmt.Errorf("passkeytest: unreadable registration options: %w", err)
	}
	if opts.PublicKey.Challenge == "" {
		return nil, errors.New("passkeytest: registration options have no challenge")
	}
	handle, err := b64.DecodeString(opts.PublicKey.User.ID)
	if err != nil {
		return nil, fmt.Errorf("passkeytest: user ID: %w", err)
	}
	if a.key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader); err != nil {
		return nil, err
	}
	a.credentialID = make([]byte, 32)
	_, _ = rand.Read(a.credentialID)
	a.userHandle = handle

	pub, err := a.key.PublicKey.Bytes() // 0x04 || X || Y
	if err != nil {
		return nil, err
	}
	coseKey, err := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: pub[1:33], -3: pub[33:65]})
	if err != nil {
		return nil, err
	}
	authData := a.authData(opts.PublicKey.RP.ID, flagAttestedData)
	authData = append(authData, make([]byte, 16)...)                                // AAGUID
	authData = binary.BigEndian.AppendUint16(authData, uint16(len(a.credentialID))) //nolint:gosec // credential IDs are 32 bytes
	authData = append(authData, a.credentialID...)
	authData = append(authData, coseKey...)

	attestation, err := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": authData})
	if err != nil {
		return nil, err
	}
	clientData := a.clientData("webauthn.create", opts.PublicKey.Challenge)
	return json.Marshal(map[string]any{
		"id": b64.EncodeToString(a.credentialID), "rawId": b64.EncodeToString(a.credentialID), "type": "public-key",
		"authenticatorAttachment": "platform", "clientExtensionResults": map[string]any{},
		"response": map[string]any{
			"clientDataJSON":    b64.EncodeToString(clientData),
			"attestationObject": b64.EncodeToString(attestation),
			"transports":        []string{"internal"},
		},
	})
}

// Get answers sign-in options ({"publicKey": ...}) with a signed assertion,
// returning the JSON a browser would send.
func (a *Authenticator) Get(options []byte) ([]byte, error) {
	if a.key == nil {
		return nil, errors.New("passkeytest: no passkey yet; call Create first")
	}
	var opts struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
			RPID      string `json:"rpId"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(options, &opts); err != nil {
		return nil, fmt.Errorf("passkeytest: unreadable sign-in options: %w", err)
	}
	if opts.PublicKey.Challenge == "" {
		return nil, errors.New("passkeytest: sign-in options have no challenge")
	}
	if !a.Synced {
		a.signCount++
	}
	authData := a.authData(opts.PublicKey.RPID, 0)
	clientData := a.clientData("webauthn.get", opts.PublicKey.Challenge)
	clientHash := sha256.Sum256(clientData)
	digest := sha256.Sum256(append(append([]byte{}, authData...), clientHash[:]...))
	signature, err := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"id": b64.EncodeToString(a.credentialID), "rawId": b64.EncodeToString(a.credentialID), "type": "public-key",
		"authenticatorAttachment": "platform", "clientExtensionResults": map[string]any{},
		"response": map[string]any{
			"clientDataJSON":    b64.EncodeToString(clientData),
			"authenticatorData": b64.EncodeToString(authData),
			"signature":         b64.EncodeToString(signature),
			"userHandle":        b64.EncodeToString(a.userHandle),
		},
	})
}

// authData returns the relying party hash, flags and counter.
func (a *Authenticator) authData(rpID string, extra byte) []byte {
	flags := byte(flagUserPresent) | extra
	if a.UserVerified {
		flags |= flagUserVerified
	}
	if a.Synced {
		flags |= flagBackupEligible | flagBackupState
	}
	rpHash := sha256.Sum256([]byte(rpID))
	data := append(rpHash[:], flags)
	return binary.BigEndian.AppendUint32(data, a.signCount)
}

func (a *Authenticator) clientData(kind, challenge string) []byte {
	b, _ := json.Marshal(map[string]any{"type": kind, "challenge": challenge, "origin": a.origin, "crossOrigin": false})
	return b
}
