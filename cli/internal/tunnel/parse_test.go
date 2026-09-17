package tunnel

import (
	"regexp"
	"strings"
	"testing"
)

func TestParseQuickURL(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		// cloudflared 2024–2026 draws the URL in a box.
		{"2026-09-17T10:00:03Z INF |  https://nice-words-here-today.trycloudflare.com                                             |", "https://nice-words-here-today.trycloudflare.com"},
		{"https://abc-123.trycloudflare.com", "https://abc-123.trycloudflare.com"},
		{"visit it at HTTPS://Mixed-Case.TryCloudflare.com/ now", "https://mixed-case.trycloudflare.com"},
		{"url=https://a.trycloudflare.com,", "https://a.trycloudflare.com"},
		{"first https://example.com then https://second-one.trycloudflare.com", "https://second-one.trycloudflare.com"},
		// Not a tunnel URL.
		{"2026-09-17T10:00:00Z INF Requesting new quick Tunnel on trycloudflare.com...", ""},
		{"POST https://api.trycloudflare.com/tunnel failed", ""},
		{"http://plain.trycloudflare.com", ""},
		{"https://two.labels.trycloudflare.com", ""},
		{"https://-bad.trycloudflare.com", ""},
		{"https://bad-.trycloudflare.com", ""},
		{"https://under_score.trycloudflare.com", ""},
		{"https://x.trycloudflare.com.evil.example", ""},
		{"https://x.trycloudflare.community", ""},
		{"https://user@x.trycloudflare.com", ""},
		{"https://x.trycloudflare.com:8443/", ""},
		{"https://.trycloudflare.com", ""},
		{"https://" + strings.Repeat("a", 64) + ".trycloudflare.com", ""},
		{"", ""},
		{"https://", ""},
	}
	for _, tt := range tests {
		got, ok := ParseQuickURL(tt.line)
		if got != tt.want || ok != (tt.want != "") {
			t.Errorf("ParseQuickURL(%q) = %q, %v; want %q", tt.line, got, ok, tt.want)
		}
	}
}

func TestLineHelpers(t *testing.T) {
	line := "2026-09-17T10:00:05Z INF Registered tunnel connection connIndex=0 connection=abc event=0 ip=198.41.200.13 location=ams01 protocol=quic"
	if !connectedLine(line) || levelOf(line) != "INF" {
		t.Errorf("connected %v level %q", connectedLine(line), levelOf(line))
	}
	if got := messageOf("2026-09-17T10:00:05Z ERR failed to dial   edge"); got != "failed to dial   edge" {
		t.Errorf("messageOf = %q", got)
	}
	if levelOf("just text") != "" || messageOf("  just text ") != "just text" || levelOf("") != "" {
		t.Error("plain lines")
	}
}

var strictQuickURL = regexp.MustCompile(`^https://[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.trycloudflare\.com$`)

func FuzzParseQuickURL(f *testing.F) {
	for _, seed := range []string{
		"2026-09-17T10:00:03Z INF |  https://nice-words-here-today.trycloudflare.com  |",
		"https://api.trycloudflare.com",
		"HTTPS://X.TRYCLOUDFLARE.COM:1",
		"https://a.b.trycloudflare.com https://c.trycloudflare.com",
		"https://\x00.trycloudflare.com",
		"ｈttps://x.trycloudflare.com",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, line string) {
		got, ok := ParseQuickURL(line)
		if !ok {
			if got != "" {
				t.Fatalf("not ok but %q", got)
			}
			return
		}
		if !strictQuickURL.MatchString(got) || got == "https://api.trycloudflare.com" {
			t.Fatalf("ParseQuickURL(%q) = %q, not a quick tunnel URL", line, got)
		}
		if !strings.Contains(strings.ToLower(line), strings.TrimPrefix(got, "https://")) {
			t.Fatalf("ParseQuickURL(%q) = %q, which the line doesn't hold", line, got)
		}
	})
}
