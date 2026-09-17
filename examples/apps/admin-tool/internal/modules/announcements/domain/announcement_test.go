package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"example.com/admin-tool/internal/modules/announcements/domain"
)

func TestNewAnnouncement(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	tomorrow := now.Add(24 * time.Hour)
	for _, tt := range []struct {
		name    string
		fields  domain.Fields
		wantErr error
	}{
		{"valid", domain.Fields{Title: " Maintenance ", Body: "Saturday 02:00 UTC", EndsAt: tomorrow}, nil},
		{"blank title", domain.Fields{Title: " ", Body: "b", EndsAt: tomorrow}, domain.ErrTitleRequired},
		{"long title", domain.Fields{Title: strings.Repeat("é", 201), Body: "b", EndsAt: tomorrow}, domain.ErrTitleRequired},
		{"blank body", domain.Fields{Title: "t", EndsAt: tomorrow}, domain.ErrBodyRequired},
		{"ends now", domain.Fields{Title: "t", Body: "b", EndsAt: now}, domain.ErrInvalidEndsAt},
	} {
		got, err := domain.NewAnnouncement("ann_1", "usr_staff", tt.fields, now)
		if !errors.Is(err, tt.wantErr) {
			t.Errorf("%s: NewAnnouncement() error = %v, want %v", tt.name, err, tt.wantErr)
			continue
		}
		if err == nil && (got.Title != "Maintenance" || !got.StartsAt.Equal(now) || !got.Active(now) || got.Active(tomorrow)) {
			t.Errorf("%s: NewAnnouncement() = %+v", tt.name, got)
		}
	}
}
