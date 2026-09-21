package cli

import (
	"strings"
	"testing"
)

// orb eject was removed in v0.3 (ADR-0092): orb new writes sign-in and
// organisations into the app, so there is nothing to eject on demand. The
// name stays registered for one release and says so, rather than failing
// with "unknown command", because it is in scripts and in older pages.
func TestEjectWasRemoved(t *testing.T) {
	for _, args := range [][]string{{"eject"}, {"eject", "auth"}, {"eject", "modules/auth"}} {
		code, _, errOut := runOrb(t, args...)
		if code == 0 {
			t.Errorf("orb %v succeeded; the command is gone", args)
		}
		for _, want := range []string{"removed in v0.3", "internal/modules", "nothing to eject"} {
			if !strings.Contains(errOut, want) {
				t.Errorf("orb %v said %q, which doesn't mention %q", args, errOut, want)
			}
		}
	}
}

// The help text no longer offers it.
func TestUsageDoesNotOfferEject(t *testing.T) {
	_, out, _ := runOrb(t, "help")
	if strings.Contains(out, "orb eject") {
		t.Errorf("orb help still lists eject:\n%s", out)
	}
}
