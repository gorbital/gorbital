package auth_test

import (
	"context"
	"strings"
	"testing"

	"gorbital.dev/modules/auth"
)

func TestEmailPreviews(t *testing.T) {
	previews := auth.EmailPreviews("Acme")
	if len(previews) != 11 {
		t.Fatalf("previews = %d", len(previews))
	}
	seen := map[string]bool{}
	for _, p := range previews {
		if p.Name == "" || p.Description == "" || p.Build == nil || seen[p.Name] {
			t.Errorf("preview %+v", p)
		}
		seen[p.Name] = true
		m, err := p.Build(context.Background(), "ada@example.com")
		if err != nil {
			t.Fatalf("%s: %v", p.Name, err)
		}
		if len(m.To) != 1 || m.To[0].Email != "ada@example.com" || m.Subject == "" || m.Text == "" || m.HTML == "" || m.Tags["category"] != p.Category {
			t.Errorf("%s: message = %+v", p.Name, m)
		}
	}
	m, _ := previews[0].Build(context.Background(), "x@example.com")
	if !strings.Contains(m.Text, "483920") || !strings.Contains(m.Subject, "Acme") {
		t.Errorf("verification preview = %+v", m)
	}
}
