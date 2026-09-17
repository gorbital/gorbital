package auth_test

import (
	"encoding/base32"
	"errors"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"gorbital.dev/modules/auth"
)

// keyEncoding is the lowercase base32 API keys use (ADR-0058).
var keyEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

const base32Lower = "abcdefghijklmnopqrstuvwxyz234567"

func FuzzParseAPIKey(f *testing.F) {
	key, lookupID, _ := auth.NewAPIKey()
	secret := key[len("gbk_")+27:]
	for _, seed := range []string{
		key, "", "gbk_", "c2Vzc2lvbi10b2tlbi13aXRoLTMyLWJ5dGVzLW9mLXJhbmRvbW5lc3M", "gbx_" + key[4:], "GBK_" + key[4:],
		"gbk_" + strings.ToUpper(key[4:]), key[:len(key)-1], key + "a",
		"gbk_" + lookupID[:25] + "_" + lookupID[25:] + secret, "gbk_" + lookupID + "a" + secret,
		"gbk_" + lookupID + "_" + secret[:51] + "_", "gbk_" + lookupID + "_" + secret[:51] + "1",
		"gbk_" + lookupID + "_" + secret[:50] + "а", "gbk_gbk_" + key[8:], "gbk__" + secret + strings.Repeat("a", 26),
	} {
		f.Add(seed, []byte(seed))
	}
	f.Fuzz(func(t *testing.T, key string, random []byte) {
		if got, want := auth.IsAPIKey(key), strings.HasPrefix(key, auth.APIKeyPrefix); got != want {
			t.Fatalf("IsAPIKey(%q) = %t, want %t", key, got, want)
		}
		lookupID, err := auth.ParseAPIKey(key)
		if err != nil {
			if !errors.Is(err, auth.ErrUnauthenticated) || lookupID != "" {
				t.Fatalf("ParseAPIKey(%q) = %q, %v; want \"\", ErrUnauthenticated", key, lookupID, err)
			}
		} else {
			// Only gbk_<26 base32>_<52 base32> parses, and the lookup ID is its middle.
			if len(key) != auth.APIKeyLength || !auth.IsAPIKey(key) || key[30] != '_' || lookupID != key[4:30] ||
				strings.Trim(key[4:30], base32Lower) != "" || strings.Trim(key[31:], base32Lower) != "" {
				t.Fatalf("ParseAPIKey(%q) = %q, want an error for a malformed key", key, lookupID)
			}
		}

		// A key built from any random bytes parses to its lookup ID.
		lookup, sec := make([]byte, 16), make([]byte, 32)
		copy(lookup, random)
		if len(random) > 16 {
			copy(sec, random[16:])
		}
		wantLookup := keyEncoding.EncodeToString(lookup)
		built := auth.APIKeyPrefix + wantLookup + "_" + keyEncoding.EncodeToString(sec)
		if got, err := auth.ParseAPIKey(built); err != nil || got != wantLookup {
			t.Fatalf("ParseAPIKey(%q) = %q, %v; want %q", built, got, err, wantLookup)
		}
		if !auth.APIKeyMatches(built, auth.HashAPIKey(built)) || auth.APIKeyMatches(built, auth.HashAPIKey(key)) != (key == built) {
			t.Fatalf("APIKeyMatches(%q) disagrees with HashAPIKey", built)
		}
	})
}

func FuzzNewAPIKey(f *testing.F) {
	f.Add(uint8(1))
	f.Fuzz(func(t *testing.T, _ uint8) {
		key, lookupID, hash := auth.NewAPIKey()
		if got, err := auth.ParseAPIKey(key); err != nil || got != lookupID || !auth.IsAPIKey(key) {
			t.Fatalf("ParseAPIKey(NewAPIKey() = %q) = %q, %v; want %q", key, got, err, lookupID)
		}
		if !auth.APIKeyMatches(key, hash) || auth.APIKeyMatches(lookupID, hash) {
			t.Fatalf("NewAPIKey() hash doesn't match exactly its key %q", key)
		}
	})
}

func FuzzNormalizeEmail(f *testing.F) {
	for _, seed := range []string{
		"  Ada@Example.com ", "jürgen@bücher.example", "", "not an email", "Ada <ada@example.com>",
		strings.Repeat("a", 250) + "@x.io", "Kevin@example.com", "Åsa@example.com", "Ωmega@example.com",
		"Äda@example.com", "ada@Äxample.com", `"a b"@example.com`, "root@[192.0.2.1]", "a@b\xff",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		email, normalized, err := auth.NormalizeEmail(s)
		if err != nil {
			if !errors.Is(err, auth.ErrInvalidEmail) || email != "" || normalized != "" {
				t.Fatalf("NormalizeEmail(%q) = %q, %q, %v; want ErrInvalidEmail", s, email, normalized, err)
			}
			return
		}
		if email != strings.TrimSpace(s) || email == "" || len(email) > 254 || normalized != strings.ToLower(email) {
			t.Fatalf("NormalizeEmail(%q) = %q, %q", s, email, normalized)
		}
		if !utf8.ValidString(email) {
			t.Fatalf("NormalizeEmail(%q) accepted invalid UTF-8", s)
		}
		// No character lowercases into another address's account key.
		for _, r := range email {
			if r >= utf8.RuneSelf && unicode.ToLower(r) != r {
				t.Fatalf("NormalizeEmail(%q) accepted %q, which lowercasing changes", s, r)
			}
		}
		// The account key is a fixed point.
		e2, n2, err := auth.NormalizeEmail(normalized)
		if err != nil || e2 != normalized || n2 != normalized {
			t.Fatalf("NormalizeEmail(%q) = %q; normalizing that = %q, %q, %v", s, normalized, e2, n2, err)
		}
		// Addresses differing only in ASCII case share one account key.
		upper := strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' {
				return r - 'a' + 'A'
			}
			return r
		}, email)
		if _, n3, err := auth.NormalizeEmail(upper); err != nil || n3 != normalized {
			t.Fatalf("NormalizeEmail(%q) = %q, but %q gives %q, %v", s, normalized, upper, n3, err)
		}
	})
}

func FuzzNormalizeRecoveryCode(f *testing.F) {
	for _, seed := range []string{"abcd-efgh-ijkl-mnop", "ABCDE-FGHIJ", "abcde fghij", " abcdefghij ", "", "--  --", "İ-x", "\xff-\xfe"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, code string) {
		got := auth.NormalizeRecoveryCode(code)
		if strings.ContainsAny(got, " -") || strings.ToLower(got) != got || strings.TrimSpace(got) != got {
			t.Fatalf("NormalizeRecoveryCode(%q) = %q, want trimmed, lowercase, without spaces or hyphens", code, got)
		}
		if again := auth.NormalizeRecoveryCode(got); again != got {
			t.Fatalf("NormalizeRecoveryCode(%q) = %q, but normalizing that gives %q", code, got, again)
		}
		// However it is typed, a code normalizes the same. (Lowercasing
		// replaces invalid UTF-8 before separators are removed, so removing
		// them first can join bytes into a character: codes are text.)
		if !utf8.ValidString(code) {
			return
		}
		variants := []string{
			strings.ReplaceAll(code, "-", " "), strings.ReplaceAll(code, " ", "-"),
			" " + code + " ", strings.ReplaceAll(strings.ReplaceAll(code, "-", ""), " ", ""),
		}
		// Uppercase only where lowercasing it gives the same text, unlike
		// U+0130; not strings.EqualFold, which compares by simple folding.
		upper, lower := strings.ToUpper(code), strings.ToLower(code)
		if roundTrip := strings.ToLower(upper); roundTrip == lower {
			variants = append(variants, upper)
		}
		for _, variant := range variants {
			if v := auth.NormalizeRecoveryCode(variant); v != got {
				t.Fatalf("NormalizeRecoveryCode(%q) = %q, but %q gives %q", code, got, variant, v)
			}
		}
	})
}
