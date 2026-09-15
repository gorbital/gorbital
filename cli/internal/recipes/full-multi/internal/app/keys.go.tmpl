package app

import (
	"errors"
	"fmt"

	"apistock.dev/config"
	authlib "apistock.dev/modules/auth"
)

// loadKeyring parses AUTH_ENCRYPTION_KEYS, which encrypts authenticator app
// secrets (ADR-0043). Production requires it; in development, without it,
// two-factor authentication is unavailable and accounts using it can't sign
// in.
func loadKeyring(keys config.Secret, production bool) (*authlib.Keyring, error) {
	if keys.IsZero() {
		if production {
			return nil, errors.New(`AUTH_ENCRYPTION_KEYS is required in production: it encrypts two-factor authentication secrets (generate one: echo "k1:$(openssl rand -base64 32)")`)
		}
		return nil, nil
	}
	k, err := authlib.ParseKeyring(keys.Reveal())
	if err != nil {
		return nil, fmt.Errorf("AUTH_ENCRYPTION_KEYS: %w", err)
	}
	return k, nil
}

// keyring returns the parsed AUTH_ENCRYPTION_KEYS, or nil without keys.
// LoadConfig has already validated them.
func (c Config) keyring() *authlib.Keyring {
	k, _ := loadKeyring(c.AuthEncryptionKeys, false)
	return k
}
