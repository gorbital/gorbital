package devconsole

import (
	"runtime/debug"
	"slices"
	"strings"
	"testing"
)

func TestAllowedHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"localhost:8080", true},
		{"LOCALHOST:8080", true},
		{"127.0.0.1:8080", true},
		{"[::1]:8080", true},

		// DNS rebinding: a page on another site keeps sending its own name.
		{"evil.example:8080", false},
		{"evil.example", false},
		{"localhost.evil.example:8080", false},
		{"127.0.0.1.nip.io:8080", false},
		{"localhost.:8080", false},
		{"sub.localhost:8080", false},
		// Another port, or none, is another origin.
		{"localhost:8081", false},
		{"localhost", false},
		{"127.0.0.1:08080", false},
		{"localhost:8080:8080", false},
		// Missing and malformed hosts.
		{"", false},
		{":8080", false},
		{"[::1]", false},
		// IPv6 variants other than [::1].
		{"::1", false},
		{"::1:8080", false},
		{"[::ffff:127.0.0.1]:8080", false},
		{"[0:0:0:0:0:0:0:1]:8080", false},
		{"[::]:8080", false},
		{"[fe80::1%25lo0]:8080", false},
		// Other loopback and unspecified addresses.
		{"127.0.0.2:8080", false},
		{"0.0.0.0:8080", false},
		{"2130706433:8080", false},
		{"localhost:8080 ", false},
	}
	for _, tt := range tests {
		if got := allowedHost(tt.host, "8080"); got != tt.want {
			t.Errorf("allowedHost(%q, 8080) = %v, want %v", tt.host, got, tt.want)
		}
	}
	if !allowedHost("localhost", "80") {
		t.Error(`allowedHost("localhost", "80") = false, want true: port 80 is implied`)
	}
	if allowedHost("localhost:8080", "") {
		t.Error("allowedHost with an unknown port = true, want false")
	}
}

func TestLoopbackPeer(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:50000":          true,
		"127.8.9.1:50000":          true,
		"[::1]:50000":              true,
		"[::ffff:127.0.0.1]:50000": true,
		"192.168.1.20:50000":       false,
		"10.0.0.1:50000":           false,
		"[fe80::1]:50000":          false,
		"":                         false,
		"127.0.0.1":                false,
	} {
		if got := loopbackPeer(addr); got != want {
			t.Errorf("loopbackPeer(%q) = %v, want %v", addr, got, want)
		}
	}
}

func TestValidTokenComparesHashes(t *testing.T) {
	token := strings.Repeat("a", 40)
	c, err := New(token)
	if err != nil {
		t.Fatal(err)
	}
	for header, want := range map[string]bool{
		"Bearer " + token:                   true,
		"bearer " + token:                   true,
		"Bearer " + token + " ":             true,
		"Bearer " + token[:39]:              false,
		"Bearer " + token + "a":             false,
		"Bearer " + strings.Repeat("b", 40): false,
		token:                               false,
		"Basic " + token:                    false,
		"Bearer":                            false,
		"":                                  false,
	} {
		if got := c.validToken(header); got != want {
			t.Errorf("validToken(%q) = %v, want %v", header, got, want)
		}
	}
}

func TestBuffer(t *testing.T) {
	b := newBuffer[int](3)
	if got := b.list(); len(got) != 0 {
		t.Errorf("empty list = %v", got)
	}
	sub := b.subscribe(2)
	for i := 1; i <= 7; i++ {
		b.add(i)
	}
	if got := b.list(); len(got) != 3 || got[0] != 5 || got[2] != 7 {
		t.Errorf("list = %v, want [5 6 7]", got)
	}
	if got := b.takeDropped(sub); got != 5 {
		t.Errorf("dropped = %d, want 5 (a channel of 2 and 7 items)", got)
	}
	if v := <-sub.ch; v != 1 {
		t.Errorf("first item = %d, want 1", v)
	}
	b.unsubscribe(sub)
	b.add(8)
	if len(sub.ch) != 1 {
		t.Errorf("an unsubscribed channel got items: %d", len(sub.ch))
	}
}

func TestLibrariesOf(t *testing.T) {
	bi := &debug.BuildInfo{Deps: []*debug.Module{
		{Path: "github.com/jackc/pgx/v5", Version: "v5.11.0"},
		{Path: "gorbital.dev/modules/telemetry", Version: "v1.1.0"},
		{Path: "gorbital.dev", Version: "v1.1.0", Replace: &debug.Module{Path: "../.."}},
		{Path: "gorbital.devious/x", Version: "v1"},
	}}
	got := librariesOf(bi)
	want := []Library{{Path: "gorbital.dev", Version: "v1.1.0", Replaced: true}, {Path: "gorbital.dev/modules/telemetry", Version: "v1.1.0"}}
	if !slices.Equal(got, want) {
		t.Errorf("librariesOf = %+v, want %+v", got, want)
	}
}
