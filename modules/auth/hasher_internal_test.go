package auth

import (
	"context"
	"errors"
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

// A flood of sign-ins can't hold requests without limit: a request waits for
// a hashing slot only until its context ends or the maximum wait passes
// (security review AUTH-S-8).
func TestHasherWaitIsBounded(t *testing.T) {
	h, err := newHasher(defaultParams, WithHashConcurrency(1), WithHashMaxWait(50*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if cap(h.slots) != 1 {
		t.Fatalf("slots = %d, want 1", cap(h.slots))
	}
	encoded, err := h.Hash("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	h.slots <- struct{}{} // every slot busy

	start := time.Now()
	if _, err := h.HashContext(context.Background(), "correct horse battery"); !errors.Is(err, ErrHasherBusy) {
		t.Errorf("HashContext(busy) error = %v, want ErrHasherBusy", err)
	}
	if waited := time.Since(start); waited > 2*time.Second {
		t.Errorf("HashContext(busy) waited %v, want about the maximum wait", waited)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := h.VerifyContext(ctx, "correct horse battery", encoded); !errors.Is(err, ErrHasherBusy) || !errors.Is(err, context.Canceled) {
		t.Errorf("VerifyContext(cancelled) error = %v, want ErrHasherBusy and context.Canceled", err)
	}
	if err := h.VerifyDummyContext(ctx, "x"); !errors.Is(err, ErrHasherBusy) {
		t.Errorf("VerifyDummyContext(cancelled) error = %v", err)
	}
	if ok, _ := h.Verify("correct horse battery", encoded); ok {
		t.Error("Verify(busy) = true, want false")
	}

	<-h.slots
	if ok, _, err := h.VerifyContext(ctx, "correct horse battery", encoded); !ok || err != nil {
		t.Errorf("VerifyContext(free slot, cancelled context) = %t, %v; want a match", ok, err)
	}
}

func TestHumanDuration(t *testing.T) {
	for in, want := range map[time.Duration]string{15 * time.Minute: "15 minutes", time.Hour: "1 hour", 2 * time.Hour: "2 hours", time.Minute: "1 minute"} {
		if got := humanDuration(in); got != want {
			t.Errorf("humanDuration(%v) = %q, want %q", in, got, want)
		}
	}
}
