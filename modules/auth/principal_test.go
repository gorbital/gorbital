package auth_test

import (
	"testing"
	"time"

	"apistock.dev/modules/auth"
)

func TestPrincipalRecentlyVerified(t *testing.T) {
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		p    auth.Principal
		want bool
	}{
		{"not verified", auth.Principal{}, false},
		{"verified without a time", auth.Principal{MFAVerified: true}, false},
		{"verified a minute ago", auth.Principal{MFAVerified: true, MFAVerifiedAt: now.Add(-time.Minute)}, true},
		{"verified at the limit", auth.Principal{MFAVerified: true, MFAVerifiedAt: now.Add(-auth.RecentVerification)}, false},
		{"verified a day ago", auth.Principal{MFAVerified: true, MFAVerifiedAt: now.Add(-24 * time.Hour)}, false},
	}
	for _, tt := range tests {
		if got := tt.p.RecentlyVerified(now); got != tt.want {
			t.Errorf("%s: RecentlyVerified() = %v, want %v", tt.name, got, tt.want)
		}
	}
}
