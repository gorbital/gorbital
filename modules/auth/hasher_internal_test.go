package auth

import (
	"testing"
	"time"
)

func TestHasherRehashesOutdatedParameters(t *testing.T) {
	old, err := newHasher(defaultParams)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := old.Hash("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	stronger, err := newHasher(argonParams{memory: 32 * 1024, iterations: 3, parallelism: 1, saltLen: 16, keyLen: 32})
	if err != nil {
		t.Fatal(err)
	}
	if ok, rehash := stronger.Verify("correct horse battery", encoded); !ok || !rehash {
		t.Errorf("Verify with newer parameters = %t, %t; want a rehash", ok, rehash)
	}
}

func TestHumanDuration(t *testing.T) {
	for in, want := range map[time.Duration]string{15 * time.Minute: "15 minutes", time.Hour: "1 hour", 2 * time.Hour: "2 hours", time.Minute: "1 minute"} {
		if got := humanDuration(in); got != want {
			t.Errorf("humanDuration(%v) = %q, want %q", in, got, want)
		}
	}
}
