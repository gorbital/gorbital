package requestid_test

import (
	"context"
	"strings"
	"testing"

	"gorbital.dev/requestid"
)

func TestNew(t *testing.T) {
	a, b := requestid.New(), requestid.New()
	if !strings.HasPrefix(a, "req_") || len(a) != 20 || a == b {
		t.Errorf("New() = %q, %q; want distinct req_ IDs of length 20", a, b)
	}
	if !requestid.Valid(a) {
		t.Errorf("Valid(New()) = false for %q, want true", a)
	}
}

func TestValid(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"req_abc123", true},
		{"0b6f4c2e-7a1d-4b9e-8c3f-2d5e6f7a8b9c", true},
		{"trace:span.1", true},
		{"", false},
		{strings.Repeat("a", requestid.MaxLength+1), false},
		{"abc\ninjected=true", false},
		{"<script>", false},
		{"id with space", false},
	}
	for _, tt := range tests {
		if got := requestid.Valid(tt.id); got != tt.want {
			t.Errorf("Valid(%q) = %t, want %t", tt.id, got, tt.want)
		}
	}
}

func TestContext(t *testing.T) {
	ctx := context.Background()
	if got := requestid.From(ctx); got != "" {
		t.Errorf("From(empty context) = %q, want empty", got)
	}
	if got := requestid.From(requestid.With(ctx, "req_1")); got != "req_1" {
		t.Errorf("From(With(req_1)) = %q, want req_1", got)
	}
}
