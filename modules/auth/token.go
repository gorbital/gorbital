package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"fmt"
	"math/big"
	netmail "net/mail"
	"net/netip"
	"strings"
)

var idEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// NewID returns a random identifier such as usr_2x7…, with 128 bits of
// randomness.
func NewID(prefix string) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // never fails (crypto/rand)
	return prefix + "_" + idEncoding.EncodeToString(b)
}

// NewToken returns a session token with 256 bits of randomness and the hash
// to store. Store only the hash.
func NewToken() (token string, hash []byte) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashToken(token)
}

// HashToken returns the SHA-256 of a token, for storing and looking it up.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

var million = big.NewInt(1_000_000)

// NewCode returns a uniformly random 6-digit code.
func NewCode() string {
	n, _ := rand.Int(rand.Reader, million)
	return fmt.Sprintf("%06d", n.Int64())
}

// HashCode returns the hash to store for code, bound to its row ID so equal
// codes hash differently.
func HashCode(id, code string) []byte {
	sum := sha256.Sum256([]byte(id + ":" + strings.TrimSpace(code)))
	return sum[:]
}

// CodeMatches reports, in constant time, whether code is the one hashed as
// hash for row id.
func CodeMatches(id, code string, hash []byte) bool {
	return subtle.ConstantTimeCompare(HashCode(id, code), hash) == 1
}

// NormalizeEmail returns the address to send to and the lowercased address
// accounts are unique by, or [ErrInvalidEmail].
func NormalizeEmail(s string) (email, normalized string, err error) {
	email = strings.TrimSpace(s)
	if email == "" || len(email) > 254 {
		return "", "", ErrInvalidEmail
	}
	addr, err := netmail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return "", "", ErrInvalidEmail
	}
	return email, strings.ToLower(email), nil
}

// ClientInfo describes the client making a request, for sessions and audit
// events.
type ClientInfo struct {
	IP        string
	UserAgent string
}

// Clean returns c with the IP address in canonical form (empty when it isn't
// one) and a bounded, valid user agent, ready to store.
func (c ClientInfo) Clean() ClientInfo {
	if addr, err := netip.ParseAddr(strings.TrimSpace(c.IP)); err == nil {
		c.IP = addr.WithZone("").Unmap().String()
	} else {
		c.IP = ""
	}
	ua := strings.ReplaceAll(strings.ToValidUTF8(c.UserAgent, ""), "\x00", "")
	if len(ua) > 512 {
		ua = strings.ToValidUTF8(ua[:512], "")
	}
	c.UserAgent = ua
	return c
}
