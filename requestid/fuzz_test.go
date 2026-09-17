package requestid_test

import (
	"strings"
	"testing"

	"gorbital.dev/requestid"
)

func FuzzValid(f *testing.F) {
	for _, seed := range []string{
		"req_abc123", "0b6f4c2e-7a1d-4b9e-8c3f-2d5e6f7a8b9c", "trace:span.1", "",
		strings.Repeat("a", requestid.MaxLength+1), "abc\ninjected=true", "<script>", "id with space", requestid.New(),
	} {
		f.Add(seed)
	}
	const allowed = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_.:"
	f.Fuzz(func(t *testing.T, id string) {
		want := id != "" && len(id) <= requestid.MaxLength && strings.Trim(id, allowed) == ""
		if got := requestid.Valid(id); got != want {
			t.Fatalf("Valid(%q) = %t, want %t", id, got, want)
		}
		if requestid.Valid(id) && strings.ContainsAny(id, " \t\r\n\"'<>=\\\x00") {
			t.Fatalf("Valid(%q) accepted a character that could inject into logs", id)
		}
		if n := requestid.New(); !requestid.Valid(n) || len(n) != 20 || !strings.HasPrefix(n, "req_") {
			t.Fatalf("New() = %q, want a valid req_ ID of length 20", n)
		}
	})
}
