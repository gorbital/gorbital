package domain

import (
	"strings"
	"testing"
)

func TestChallengeToken(t *testing.T) {
	for _, tc := range []struct{ token, method, want, parsed string }{
		{"abc-_DEF", "", "abc-_DEF", MethodPassword},
		{"abc-_DEF", MethodPassword, "abc-_DEF", MethodPassword},
		{"abc-_DEF", "github", "abc-_DEF.github", "github"},
		{"abc-_DEF", "phone_code", "abc-_DEF.phone_code", "phone_code"},
	} {
		got := ChallengeToken(tc.token, tc.method)
		if got != tc.want || ChallengeMethod(got) != tc.parsed {
			t.Errorf("ChallengeToken(%q, %q) = %q (method %q), want %q (%q)", tc.token, tc.method, got, ChallengeMethod(got), tc.want, tc.parsed)
		}
	}
}

func TestCheckCustomMethod(t *testing.T) {
	for _, ok := range []string{"phone_code", "magic_link", "sms"} {
		if err := CheckCustomMethod(ok); err != nil {
			t.Errorf("CheckCustomMethod(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"", "x", "password", "passkey", "google", "apple", "github", "operator", "mfa", "Phone", "phone-code", "a.b", strings.Repeat("a", 33)} {
		if err := CheckCustomMethod(bad); err == nil {
			t.Errorf("CheckCustomMethod(%q) = nil", bad)
		}
	}
}
