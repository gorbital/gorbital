package domain_test

import (
	"errors"
	"testing"

	"example.com/plateful/internal/modules/ping/domain"
)

func TestNewMessage(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr error
	}{
		{"hello", "hello", nil},
		{"  padded  ", "padded", nil},
		{"   ", "", domain.ErrMessageRequired},
		{"", "", domain.ErrMessageRequired},
	}
	for _, tt := range tests {
		m, err := domain.NewMessage(tt.in)
		if !errors.Is(err, tt.wantErr) || m.Text() != tt.want {
			t.Errorf("NewMessage(%q) = %q, %v; want %q, %v", tt.in, m.Text(), err, tt.want, tt.wantErr)
		}
	}
}
