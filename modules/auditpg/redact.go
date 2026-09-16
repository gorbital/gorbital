package auditpg

import (
	"encoding/json"
	"strings"
	"unicode"
)

// redactedValue replaces values under sensitive metadata keys.
const redactedValue = "[REDACTED]"

// defaultRedactedKeys are snake_case names whose values are never stored.
// Keys match in singular or plural and in any case style (see sensitive).
// A bare "code" is not among them: it usually names an error or status code,
// so secret codes need a qualifier such as recovery_code.
var defaultRedactedKeys = []string{
	"password", "passwd", "passphrase", "passcode", "secret", "token", "cookie", "authorization", "bearer",
	"api_key", "apikey", "private_key", "signing_key", "encryption_key", "credential", "jwt", "pin",
	"otp", "totp", "recovery_code", "verification_code", "reset_code", "login_code", "sign_in_code",
	"mfa_code", "backup_code", "security_code", "access_code", "magic_link",
}

// encodeMetadata returns metadata as JSON with sensitive values redacted and
// NUL characters removed (jsonb rejects them). Metadata that can't be
// encoded, or is too large, is replaced by a marker so the event is still
// recorded.
func (s *Store) encodeMetadata(metadata map[string]any) string {
	if len(metadata) == 0 {
		return "{}"
	}
	// A JSON round trip reaches values inside structs and typed maps too.
	raw, err := json.Marshal(metadata)
	if err != nil {
		return `{"metadata_dropped":"not_json"}`
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return `{"metadata_dropped":"not_json"}`
	}
	raw, err = json.Marshal(s.redact(generic))
	if err != nil {
		return `{"metadata_dropped":"not_json"}`
	}
	if len(raw) > s.maxMetadataBytes {
		return `{"metadata_dropped":"too_large"}`
	}
	return string(raw)
}

// redact walks a decoded JSON value.
func (s *Store) redact(v any) any {
	switch v := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, value := range v {
			key = strings.ReplaceAll(key, "\x00", "")
			if s.sensitive(key) {
				out[key] = redactedValue
				continue
			}
			out[key] = s.redact(value)
		}
		return out
	case []any:
		for i, item := range v {
			v[i] = s.redact(item)
		}
		return v
	case string:
		return strings.ReplaceAll(v, "\x00", "")
	default:
		return v
	}
}

// sensitive reports whether key contains a redacted name as whole
// snake_case segments, ignoring a plural "s" on each segment: "token"
// matches "refresh_tokens" and "sessionTokens", "api_key" matches "apiKeys",
// but "token" doesn't match "tokenizer".
func (s *Store) sensitive(key string) bool {
	padded := "_" + normalizeKey(key) + "_"
	for _, name := range s.redacted {
		if strings.Contains(padded, "_"+name+"_") {
			return true
		}
	}
	return false
}

func normalizedKeys(names []string) []string {
	out := make([]string, len(names))
	for i, name := range names {
		out[i] = normalizeKey(name)
	}
	return out
}

// normalizeKey returns key in snake_case with each segment singular.
func normalizeKey(key string) string {
	segments := strings.Split(snakeCase(key), "_")
	for i, seg := range segments {
		if len(seg) > 2 && strings.HasSuffix(seg, "s") && !strings.HasSuffix(seg, "ss") {
			segments[i] = seg[:len(seg)-1]
		}
	}
	return strings.Join(segments, "_")
}

// snakeCase lowercases key and separates words with underscores:
// "accessToken", "Access-Token" and "access.token" become "access_token".
func snakeCase(key string) string {
	var b strings.Builder
	prevLower := false
	for _, r := range key {
		switch {
		case unicode.IsUpper(r):
			if prevLower {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
			prevLower = false
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			prevLower = true
		default:
			b.WriteByte('_')
			prevLower = false
		}
	}
	return strings.Trim(b.String(), "_")
}
