package recipes

import (
	"strings"
	"testing"
)

// Apps generated before the rename carry "# aps:" markers; the block is
// still found and comes back with the current markers.
func TestBlockLegacyMarkers(t *testing.T) {
	src := []byte("A=1\n# aps:begin mail (managed)\nRESEND_API_KEY=\n# aps:end mail\nB=2\n")
	got, err := Block(src, MailBlock)
	if err != nil {
		t.Fatalf("Block() error = %v", err)
	}
	if !strings.HasPrefix(string(got), "# aps:begin mail") || !strings.HasSuffix(string(got), "# aps:end mail\n") {
		t.Errorf("Block() = %q", got)
	}
	out, err := ReplaceBlock(src, MailBlock, []byte("# orb:begin mail\nSMTP_HOST=\n# orb:end mail\n"))
	if err != nil {
		t.Fatalf("ReplaceBlock() error = %v", err)
	}
	if want := "A=1\n# orb:begin mail\nSMTP_HOST=\n# orb:end mail\nB=2\n"; string(out) != want {
		t.Errorf("ReplaceBlock() = %q, want %q", out, want)
	}
	if _, err := Block([]byte("# orb:begin mail\n# aps:end mail\n"), MailBlock); err == nil {
		t.Error("mixed markers should not match")
	}
}
