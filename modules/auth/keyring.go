package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Errors returned by [Keyring].
var (
	// ErrInvalidKeyring reports an AUTH_ENCRYPTION_KEYS value that can't be
	// parsed.
	ErrInvalidKeyring = errors.New("auth: invalid encryption keys")
	// ErrUnknownKey reports a ciphertext encrypted with a key the keyring
	// doesn't hold.
	ErrUnknownKey = errors.New("auth: ciphertext was encrypted with an unknown key")
	// ErrDecrypt reports a ciphertext that was changed, or bound to other
	// additional data.
	ErrDecrypt = errors.New("auth: ciphertext can't be decrypted")
)

var keyIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// A Keyring encrypts small secrets, such as TOTP secrets, with AES-256-GCM.
// It holds one or more keys, each with an ID: the first key encrypts, and
// every key decrypts, so a key can be replaced without a flag day (put the
// new key first, re-encrypt, then remove the old one).
type Keyring struct {
	ids   []string
	aeads map[string]cipher.AEAD
}

// ParseKeyring parses keys written as comma-separated "id:base64key"
// entries, each key 32 bytes, the first used to encrypt. IDs are lowercase
// letters, digits, hyphens and underscores (at most 32).
func ParseKeyring(spec string) (*Keyring, error) {
	k := &Keyring{aeads: map[string]cipher.AEAD{}}
	for entry := range strings.SplitSeq(spec, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		id, encoded, ok := strings.Cut(entry, ":")
		if !ok || !keyIDPattern.MatchString(id) {
			return nil, fmt.Errorf("%w: each entry is id:base64key with a lowercase id", ErrInvalidKeyring)
		}
		if _, dup := k.aeads[id]; dup {
			return nil, fmt.Errorf("%w: key ID %q appears twice", ErrInvalidKeyring, id)
		}
		key, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(key) != 32 {
			return nil, fmt.Errorf("%w: key %q must be 32 bytes in base64 (openssl rand -base64 32)", ErrInvalidKeyring, id)
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, err
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, err
		}
		k.ids = append(k.ids, id)
		k.aeads[id] = aead
	}
	if len(k.ids) == 0 {
		return nil, fmt.Errorf("%w: no keys", ErrInvalidKeyring)
	}
	return k, nil
}

// NewKeyringKey returns a new random key entry "id:base64key", for
// generating AUTH_ENCRYPTION_KEYS.
func NewKeyringKey(id string) string {
	key := make([]byte, 32)
	_, _ = rand.Read(key) // never fails (crypto/rand)
	return id + ":" + base64.StdEncoding.EncodeToString(key)
}

// CurrentKeyID returns the ID of the key that encrypts.
func (k *Keyring) CurrentKeyID() string { return k.ids[0] }

// Encrypt seals plaintext with the current key, bound to additionalData (for
// example the owning user's ID and the secret's purpose), and returns the
// key's ID and the nonce followed by the ciphertext.
func (k *Keyring) Encrypt(plaintext, additionalData []byte) (keyID string, ciphertext []byte, err error) {
	id := k.ids[0]
	aead := k.aeads[id]
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", nil, err
	}
	return id, aead.Seal(nonce, nonce, plaintext, additionalData), nil
}

// Decrypt opens a ciphertext from [Keyring.Encrypt] with the key keyID and
// the same additionalData. It returns [ErrUnknownKey] or [ErrDecrypt].
func (k *Keyring) Decrypt(keyID string, ciphertext, additionalData []byte) ([]byte, error) {
	aead, ok := k.aeads[keyID]
	if !ok {
		return nil, ErrUnknownKey
	}
	if len(ciphertext) < aead.NonceSize()+aead.Overhead() {
		return nil, ErrDecrypt
	}
	nonce, sealed := ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, sealed, additionalData)
	if err != nil {
		return nil, ErrDecrypt
	}
	return plaintext, nil
}
