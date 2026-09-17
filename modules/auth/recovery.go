package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"strings"
)

// RecoveryCodeCount is how many recovery codes a user gets at a time.
const RecoveryCodeCount = 10

// NewRecoveryCodes returns [RecoveryCodeCount] single-use recovery codes,
// each 16 base32 characters (80 random bits) shown as "xxxx-xxxx-xxxx-xxxx".
// Show them once and store only [HashRecoveryCode].
func NewRecoveryCodes() []string {
	codes := make([]string, RecoveryCodeCount)
	for i := range codes {
		b := make([]byte, 10)
		_, _ = rand.Read(b)               // never fails (crypto/rand)
		s := idEncoding.EncodeToString(b) // exactly 16 characters
		codes[i] = s[:4] + "-" + s[4:8] + "-" + s[8:12] + "-" + s[12:]
	}
	return codes
}

// NormalizeRecoveryCode returns code lowercased without spaces or hyphens,
// so "ABCDE-FGHIJ", "abcde fghij" and "abcdefghij" match.
func NormalizeRecoveryCode(code string) string {
	// Trim after removing separators, so whitespace such as a tab next to
	// one is trimmed however the code was separated.
	return strings.TrimSpace(strings.NewReplacer(" ", "", "-", "").Replace(strings.ToLower(code)))
}

// HashRecoveryCode returns the hash to store for a user's recovery code.
// Codes carry 80 random bits, so a stolen hash can't be reversed by trying
// codes; binding it to the user makes equal codes of two users hash
// differently. Codes made before 2026-09-16 carry 50 bits and still match
// (security review AUTH-M-4).
func HashRecoveryCode(userID, code string) []byte {
	sum := sha256.Sum256([]byte(userID + ":" + NormalizeRecoveryCode(code)))
	return sum[:]
}
