package releases_test

import (
	"regexp"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"apistock.dev/buildinfo"
	"apistock.dev/modules/releases"
)

func TestTrackerInstanceID(t *testing.T) {
	// The pool is never used: the ID exists before anything is recorded.
	a, err := releases.NewTracker(new(pgxpool.Pool), buildinfo.Info{})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := releases.NewTracker(new(pgxpool.Pool), buildinfo.Info{})
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(a.InstanceID()) || a.InstanceID() != a.InstanceID() || a.InstanceID() == b.InstanceID() {
		t.Errorf("InstanceID() = %q and %q, want a stable random 32-character hex ID per tracker", a.InstanceID(), b.InstanceID())
	}
}
