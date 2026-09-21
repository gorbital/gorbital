package cli

import (
	"slices"
)

// How orb doctor --security decides that a name is a credential. Every
// rule that looks at a name uses these two functions and nothing else, so
// what the command flags can be stated in one sentence in the guide, and a
// name it doesn't flag is a name nobody has to argue about.

// credentialWords end the name of a value that is itself a secret.
var credentialWords = []string{"token", "secret", "password", "passphrase", "apikey"}

// credentialPairs end the name of a secret in two words, where the last on
// its own (key) is far too common to go in the list above.
var credentialPairs = []string{"api_key", "private_key", "secret_key", "signing_key", "encryption_key", "access_key", "session_key"}

// credentialName reports whether a column, field, variable or path
// parameter holds a credential, from its name alone: its last word is one
// of token, secret, password, passphrase or apikey, or its last two words
// are one of api_key, private_key, secret_key, signing_key,
// encryption_key, access_key or session_key.
//
// "credential" is deliberately not in the list. A WebAuthn credential is a
// public key, and auth_passkeys.credential holds one: the word means the
// record, not the secret, often enough that including it costs more than
// it finds.
//
// The test is deliberately the *last* word, so the names that mean "not the
// secret" carry themselves: token_hash, secret_ciphertext,
// refresh_token_ciphertext, password_changed_at, key_id and api_key_prefix
// are all false, and nobody has to maintain a list of exceptions. It costs
// the names that end in a word this doesn't know — an app's own
// pkce_verifier or recovery_code — which is the side to be wrong on: a rule
// that stays quiet is better than one people learn to ignore.
func credentialName(name string) bool {
	w := splitWords(name)
	if len(w) == 0 {
		return false
	}
	if len(w) >= 2 && slices.Contains(credentialPairs, w[len(w)-2]+"_"+w[len(w)-1]) {
		return true
	}
	return slices.Contains(credentialWords, w[len(w)-1])
}

// plaintextWords name a value that is a secret as the person typed it,
// before anything hashed it.
var plaintextWords = []string{"password", "passphrase", "plaintext", "plain", "pw", "pass", "token", "secret", "code"}

// plaintextName reports whether a value's name says it holds a secret in
// the clear: for the one rule that asks whether what reaches a hash column
// was hashed first. It is looser than credentialName on purpose — it
// decides nothing by itself, only which argument is worth looking at.
func plaintextName(name string) bool {
	w := splitWords(name)
	if len(w) == 0 || slices.Contains(hashedWords, w[len(w)-1]) {
		return false
	}
	return slices.ContainsFunc(w, func(s string) bool { return slices.Contains(plaintextWords, s) })
}

// hashedWords end the name of a value that has been through a hash, a key
// derivation or encryption.
var hashedWords = []string{"hash", "hashed", "digest", "sum", "ciphertext", "encrypted", "sealed", "mac", "hmac"}

// hashedName reports whether a name says the value is already hashed.
func hashedName(name string) bool {
	w := splitWords(name)
	return len(w) > 0 && slices.Contains(hashedWords, w[len(w)-1])
}

// kdfFunctions are the calls that turn a secret into something storable.
// The names are matched as words, so HashPassword, argon2id.Hash,
// bcrypt.GenerateFromPassword and sha256.Sum256 all count.
var kdfWords = []string{"hash", "argon2", "argon2id", "bcrypt", "scrypt", "pbkdf2", "sum256", "sum224", "sum512", "derive", "kdf", "encrypt", "seal", "hmac"}

// kdfCall reports whether a function name says it hashes, derives or
// encrypts.
func kdfCall(name string) bool {
	w := splitWords(name)
	return slices.ContainsFunc(w, func(s string) bool { return slices.Contains(kdfWords, s) })
}
