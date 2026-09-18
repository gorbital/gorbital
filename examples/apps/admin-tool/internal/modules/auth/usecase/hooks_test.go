package usecase_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"gorbital.dev/actor"

	authdomain "example.com/admin-tool/internal/modules/auth/domain"
	authusecase "example.com/admin-tool/internal/modules/auth/usecase"
)

type fakeHooks struct {
	mu       sync.Mutex
	calls    []string
	refuse   error
	failNext error
}

func (h *fakeHooks) record(call string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, call)
	err := h.failNext
	h.failNext = nil
	return err
}

func (h *fakeHooks) AccountCreated(_ context.Context, userID string) error {
	return h.record("created:" + userID)
}

func (h *fakeHooks) CheckAccountDeletion(_ context.Context, userID string) error {
	if err := h.record("check:" + userID); err != nil {
		return err
	}
	return h.refuse
}

func (h *fakeHooks) AccountDeleted(_ context.Context, userID string) error {
	return h.record("deleted:" + userID)
}

func (h *fakeHooks) seen() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.calls)
}

func TestAccountHooks(t *testing.T) {
	hooks := &fakeHooks{}
	f := newFixture(t, func(c *authusecase.Config) { c.Hooks = hooks })
	ctx := requestCtx()

	// A hook that fails after registration doesn't fail the registration.
	hooks.failNext = errors.New("orgs database unavailable")
	f.signUp(t, "ada@example.com")
	session, res := f.login(t, "ada@example.com")
	userID := res.User.ID
	if got := hooks.seen(); !slices.Equal(got, []string{"created:" + userID}) {
		t.Fatalf("hooks after registration = %v", got)
	}

	// A refusal stops the deletion before the password is checked.
	errSoleOwner := errors.New("sole owner of an organisation with members")
	hooks.refuse = errSoleOwner
	if err := f.svc.DeleteAccount(session, "wrong password here", authdomain.SecondFactor{}); !errors.Is(err, errSoleOwner) {
		t.Fatalf("DeleteAccount() refused by a hook error = %v, want the hook's error", err)
	}
	if _, err := f.svc.Authenticate(ctx, res.Token); err != nil {
		t.Fatalf("session after a refused deletion error = %v, want it still active", err)
	}

	hooks.refuse = nil
	if err := f.svc.DeleteAccount(session, password, authdomain.SecondFactor{}); err != nil {
		t.Fatalf("DeleteAccount() error = %v", err)
	}
	want := []string{"created:" + userID, "check:" + userID, "check:" + userID, "deleted:" + userID}
	if got := hooks.seen(); !slices.Equal(got, want) {
		t.Errorf("hooks = %v, want %v", got, want)
	}

	operator := actor.With(ctx, actor.System("cli"))
	u, err := f.svc.CreateUser(operator, "bob@example.com", password, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := hooks.seen(); got[len(got)-1] != "created:"+u.ID {
		t.Errorf("hooks after CreateUser = %v, want created:%s last", got, u.ID)
	}
}
