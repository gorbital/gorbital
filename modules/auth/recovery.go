package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"strings"
)

// RecoveryCodeCount is how many recovery codes a user gets at a time.
const RecoveryCodeCount = 10

// NewRecoveryCodes returns [RecoveryCodeCount] single-use recovery codes,
// each 10 base32 characters (50 random bits) shown as "xxxxx-xxxxx". Show
// them once and store only [HashRecoveryCode].
func NewRecoveryCodes() []string {
	codes := make([]string, RecoveryCodeCount)
	for i := range codes {
		b := make([]byte, 7)
		_, _ = rand.Read(b) // never fails (crypto/rand)
		s := idEncoding.EncodeToString(b)[:10]
		codes[i] = s[:5] + "-" + s[5:]
	}
	return codes
}

// NormalizeRecoveryCode returns code lowercased without spaces or hyphens,
// so "ABCDE-FGHIJ", "abcde fghij" and "abcdefghij" match.
func NormalizeRecoveryCode(code string) string {
	return strings.NewReplacer(" ", "", "-", "").Replace(strings.ToLower(strings.TrimSpace(code)))
}

// HashRecoveryCode returns the hash to store for a user's recovery code.
// Codes carry 50 random bits and are single-use, so a fast hash suffices;
// binding it to the user makes equal codes of two users hash differently.
func HashRecoveryCode(userID, code string) []byte {
	sum := sha256.Sum256([]byte(userID + ":" + NormalizeRecoveryCode(code)))
	return sum[:]
}
