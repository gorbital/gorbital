package mail_test

import (
	netmail "net/mail"
	"strings"
	"testing"
	"unicode"

	"gorbital.dev/mail"
)

func FuzzNormalizeAddress(f *testing.F) {
	for _, seed := range []string{"  Ada.Lovelace@Example.COM\n", "", "JÜRGEN@bücher.example", "Kelvin@example.com", "\xff@x", "İ@x"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, email string) {
		got := mail.NormalizeAddress(email)
		if again := mail.NormalizeAddress(got); again != got {
			t.Fatalf("NormalizeAddress(%q) = %q, but normalizing that gives %q", email, got, again)
		}
		if strings.TrimSpace(got) != got || strings.ToLower(got) != got {
			t.Fatalf("NormalizeAddress(%q) = %q, want trimmed and lowercase", email, got)
		}
		// Addresses differing only in ASCII case are the same list entry.
		if upper := mail.NormalizeAddress(asciiUpper(email)); upper != got {
			t.Fatalf("NormalizeAddress(%q) = %q, but its uppercase form gives %q", email, got, upper)
		}
	})
}

func asciiUpper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 'a' + 'A'
		}
	}
	return string(b)
}

func FuzzRedactAddresses(f *testing.F) {
	for _, seed := range []string{
		"550 5.1.1 <jane.doe+tag@example.co.uk>: Recipient address rejected",
		"bad recipients ada@example.com, bob_o'neil@mail.example",
		"josé@exämple.de and root@[192.0.2.1]",
		"smtp.example.com:587 refused DATA: 554 no", "",
		"z@.a@b z@[email]", "a@[b@c] d@e", "René <rene\u0301@example.com>", "zoe\u0308y@exa\u0301mple.com", "@@@", "rcpt <a@b>\r\n",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		got := mail.RedactAddresses(text)
		if !strings.Contains(text, "@") {
			if got != text {
				t.Fatalf("RedactAddresses(%q) = %q, want text without @ unchanged", text, got)
			}
			return
		}
		if strings.Count(got, "@") > strings.Count(text, "@") {
			t.Fatalf("RedactAddresses(%q) = %q added an @", text, got)
		}
		// Every valid address of letters, digits and the punctuation RFC 5322
		// allows unquoted, at a host name or address literal, delimited as
		// servers quote it, is gone from the result. (Mail servers accept
		// other characters too, such as symbols, but "shaped like an email
		// address" is about text people and servers write.)
		for _, token := range strings.FieldsFunc(text, func(r rune) bool {
			return unicode.IsSpace(r) || strings.ContainsRune("<>,;:()\"", r)
		}) {
			addr, err := netmail.ParseAddress(token)
			at := strings.LastIndex(token, "@")
			if err != nil || addr.Address != token || strings.Contains(token, mail.RedactedAddress) || !isLocal(token[:at]) || !isHost(token[at+1:]) {
				continue
			}
			if strings.Contains(got, token) {
				t.Fatalf("RedactAddresses(%q) = %q still contains the address %q", text, got, token)
			}
		}
	})
}

// isLocal reports whether local is a dot-atom local part of letters (with
// their combining marks, as in decomposed "rene\u0301"), digits and ASCII
// punctuation.
func isLocal(local string) bool {
	return !strings.ContainsFunc(local, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r) && !unicode.IsMark(r) && !strings.ContainsRune("!#$%&'*+/=?^_`{|}~.-", r)
	})
}

// isHost reports whether domain is a host name of letters, marks, digits
// and hyphens, or an address literal such as [192.0.2.1].
func isHost(domain string) bool {
	if strings.HasPrefix(domain, "[") && strings.HasSuffix(domain, "]") {
		return true
	}
	for label := range strings.SplitSeq(domain, ".") {
		if label == "" || strings.ContainsFunc(label, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsNumber(r) && !unicode.IsMark(r) && r != '-'
		}) {
			return false
		}
	}
	return true
}
