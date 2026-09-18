package usecase_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorbital.dev/actor"
	"gorbital.dev/config"
	authlib "gorbital.dev/modules/auth"
	orgslib "gorbital.dev/modules/orgs"
	"gorbital.dev/ratelimit"

	orgsdomain "example.com/plateful/internal/modules/orgs/domain"
	orgsrepository "example.com/plateful/internal/modules/orgs/repository"
	orgsusecase "example.com/plateful/internal/modules/orgs/usecase"
)

// These tests cover the internal security review of 2026-09 (ADR-0048,
// implementation notes "Security review fixes").

// deleteAccount soft deletes an account the way the auth module does.
func (f *fixture) deleteAccount(t *testing.T, userID string) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `UPDATE auth_users SET deleted_at = $2 WHERE id = $1`, userID, f.now); err != nil {
		t.Fatal(err)
	}
}

// setMember makes userID a member of orgID with role, directly in the
// database.
func (f *fixture) setMember(t *testing.T, orgID orgslib.ID, userID, role string) {
	t.Helper()
	_, err := f.pool.Exec(context.Background(), `
		INSERT INTO org_members (org_id, user_id, role, joined_at, added_by) VALUES ($1, $2, $3, $4, 'system:test')
		ON CONFLICT (org_id, user_id) DO UPDATE SET role = EXCLUDED.role`, orgID, userID, role, f.now)
	if err != nil {
		t.Fatal(err)
	}
}

// openInvitationTo returns the open invitation for email.
func (f *fixture) openInvitationTo(t *testing.T, orgID orgslib.ID, email string) orgsdomain.Invitation {
	t.Helper()
	open, err := f.svc.ListInvitations(as("usr_ada"), orgID)
	if err != nil {
		t.Fatal(err)
	}
	for _, inv := range open {
		if inv.NormalizedEmail == email {
			return inv
		}
	}
	t.Fatalf("no open invitation for %s in %+v", email, open)
	return orgsdomain.Invitation{}
}

// ORG-1: an invitation doesn't outlive its inviter's removal or demotion.
func TestInvitationsEndWithTheInvitersRole(t *testing.T) {
	f := newFixture(t)
	ada, carol := as("usr_ada"), as("usr_carol")
	id := f.team(t, orgslib.RoleAdmin)

	if _, err := f.svc.Invite(carol, id, "bob@example.com", orgslib.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Invite(carol, id, "erin@example.com", orgslib.RoleMember); err != nil {
		t.Fatal(err)
	}
	bobToken, erinToken := f.emails.token(t, "bob@example.com"), f.emails.token(t, "erin@example.com")

	// Demoted to member, carol may no longer manage members.
	if _, err := f.svc.ChangeRole(ada, id, "usr_carol", orgslib.RoleMember); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AcceptInvitation(as("usr_erin"), erinToken); !errors.Is(err, orgsdomain.ErrInvitationNotFound) {
		t.Errorf("AcceptInvitation() from a demoted inviter error = %v, want ErrInvitationNotFound", err)
	}
	// Removed, carol can't bring an address she controls back in as admin.
	if err := f.svc.RemoveMember(ada, id, "usr_carol"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AcceptInvitation(as("usr_bob"), bobToken); !errors.Is(err, orgsdomain.ErrInvitationNotFound) {
		t.Errorf("AcceptInvitation() from a removed inviter error = %v, want ErrInvitationNotFound", err)
	}

	// An owner who resends the invitation becomes its inviter and vouches
	// for it.
	resent, err := f.svc.ResendInvitation(ada, id, f.openInvitationTo(t, id, "bob@example.com").ID)
	if err != nil || resent.InvitedBy != "user:usr_ada" {
		t.Fatalf("ResendInvitation() = %+v, %v, want ada as the inviter", resent, err)
	}
	if joined, err := f.svc.AcceptInvitation(as("usr_bob"), f.emails.token(t, "bob@example.com")); err != nil || joined.Role != orgslib.RoleAdmin {
		t.Errorf("AcceptInvitation() after an owner resent it = %+v, %v", joined, err)
	}
}

// ORG-2: roles an app adds are compared by permissions, not ranked with
// members.
func TestCustomRolesCantExceedTheAssigner(t *testing.T) {
	f := newFixture(t, func(c *orgsusecase.Config) {
		cat := catalog()
		cat.Role("billing", "members who may also delete the organisation", orgsusecase.PermOrgRead, orgsusecase.PermMembersRead, orgsusecase.PermOrgDelete)
		cat.Role("viewer", "sees the organisation", orgsusecase.PermOrgRead)
		c.Catalog = cat
	})
	ada, carol := as("usr_ada"), as("usr_carol")
	id := f.team(t, orgslib.RoleAdmin)

	if _, err := f.svc.ChangeRole(carol, id, "usr_carol", "billing"); !errors.Is(err, orgsdomain.ErrRoleNotAllowed) {
		t.Errorf("ChangeRole(self to a role that may delete) by an admin error = %v, want ErrRoleNotAllowed", err)
	}
	if _, err := f.svc.Invite(carol, id, "bob@example.com", "billing"); !errors.Is(err, orgsdomain.ErrRoleNotAllowed) {
		t.Errorf("Invite(billing) by an admin error = %v, want ErrRoleNotAllowed", err)
	}
	if _, err := f.svc.Invite(carol, id, "bob@example.com", "viewer"); err != nil {
		t.Errorf("Invite(viewer) by an admin error = %v, want allowed", err)
	}
	if _, err := f.svc.Invite(ada, id, "erin@example.com", "billing"); err != nil {
		t.Fatalf("Invite(billing) by an owner error = %v", err)
	}
	if _, err := f.svc.AcceptInvitation(as("usr_erin"), f.emails.token(t, "erin@example.com")); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RemoveMember(carol, id, "usr_erin"); !errors.Is(err, orgsdomain.ErrRoleNotAllowed) {
		t.Errorf("RemoveMember(billing) by an admin error = %v, want ErrRoleNotAllowed", err)
	}
}

// ORG-3: verified addresses, a limit on owned organisations, invitations
// limited per user across organisations, and a purge that keeps up.
func TestOrganisationAndInvitationLimits(t *testing.T) {
	f := newFixture(t, func(c *orgsusecase.Config) {
		c.MaxOwnedOrgs = config.Static(2)
		c.InvitationLimiter = ratelimit.New(3/time.Hour.Seconds(), 3, ratelimit.WithClock(c.Now))
	})
	ada := as("usr_ada")
	if _, err := f.svc.Create(as("usr_dan"), "Unverified"); !errors.Is(err, orgsdomain.ErrEmailNotVerified) {
		t.Errorf("Create() with an unverified address error = %v, want ErrEmailNotVerified", err)
	}

	// The personal workspace doesn't count; deleted organisations don't
	// either, until they are restored.
	if _, err := f.svc.EnsurePersonalWorkspace(ada, "usr_ada"); err != nil {
		t.Fatal(err)
	}
	one, err := f.svc.Create(ada, "One")
	if err != nil {
		t.Fatal(err)
	}
	two, err := f.svc.Create(ada, "Two")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Create(ada, "Three"); !errors.Is(err, orgsdomain.ErrTooManyOrgs) {
		t.Errorf("Create() over orgs.max_owned error = %v, want ErrTooManyOrgs", err)
	}
	if err := f.svc.Delete(ada, orgslib.ID(two.ID)); err != nil {
		t.Fatal(err)
	}
	three, err := f.svc.Create(ada, "Three")
	if err != nil {
		t.Fatalf("Create() after deleting one error = %v", err)
	}
	if _, err := f.svc.Restore(ada, orgslib.ID(two.ID)); !errors.Is(err, orgsdomain.ErrTooManyOrgs) {
		t.Errorf("Restore() over orgs.max_owned error = %v, want ErrTooManyOrgs", err)
	}

	// Invitations are limited per user across organisations, resends
	// included.
	for i, to := range []struct {
		org   string
		email string
	}{{one.ID, "x1@example.com"}, {one.ID, "x2@example.com"}, {three.ID, "x3@example.com"}} {
		if _, err := f.svc.Invite(ada, orgslib.ID(to.org), to.email, orgslib.RoleMember); err != nil {
			t.Fatalf("Invite(#%d) error = %v", i+1, err)
		}
	}
	if _, err := f.svc.Invite(ada, orgslib.ID(three.ID), "x4@example.com", orgslib.RoleMember); !errors.Is(err, orgsdomain.ErrTooManyInvitations) {
		t.Errorf("Invite() over the per-user limit in another organisation error = %v, want ErrTooManyInvitations", err)
	}
	pending := f.openInvitationTo(t, orgslib.ID(one.ID), "x1@example.com")
	if _, err := f.svc.ResendInvitation(ada, orgslib.ID(one.ID), pending.ID); !errors.Is(err, orgsdomain.ErrTooManyInvitations) {
		t.Errorf("ResendInvitation() over the per-user limit error = %v, want ErrTooManyInvitations", err)
	}

	// A member whose address isn't verified can't send invitations.
	f.setMember(t, orgslib.ID(one.ID), "usr_dan", orgslib.RoleAdmin)
	if _, err := f.svc.Invite(as("usr_dan"), orgslib.ID(one.ID), "x5@example.com", orgslib.RoleMember); !errors.Is(err, orgsdomain.ErrEmailNotVerified) {
		t.Errorf("Invite() by an unverified member error = %v, want ErrEmailNotVerified", err)
	}
	f.now = f.now.Add(time.Hour)
	if _, err := f.svc.Invite(ada, orgslib.ID(three.ID), "x4@example.com", orgslib.RoleMember); err != nil {
		t.Errorf("Invite() an hour later error = %v", err)
	}
}

func TestOwnedOrganisationLimitHoldsUnderConcurrency(t *testing.T) {
	f := newFixture(t, func(c *orgsusecase.Config) { c.MaxOwnedOrgs = config.Static(2) })
	var (
		wg      sync.WaitGroup
		created atomic.Int32
	)
	for range 10 {
		wg.Go(func() {
			if _, err := f.svc.Create(as("usr_ada"), "Parallel"); err == nil {
				created.Add(1)
			} else if !errors.Is(err, orgsdomain.ErrTooManyOrgs) {
				t.Errorf("Create() error = %v", err)
			}
		})
	}
	wg.Wait()
	if n := created.Load(); n != 2 {
		t.Errorf("concurrent Create() created %d organisations, want 2", n)
	}
}

func TestPurgeWorksThroughABacklog(t *testing.T) {
	f := newFixture(t)
	const backlog = 250 // more than one purge batch
	_, err := f.pool.Exec(context.Background(), `
		INSERT INTO orgs (id, name, created_by, created_at, updated_at, deleted_at, purge_after)
		SELECT 'org_backlog_' || g, 'Backlog', 'usr_ada', $1, $1, $1, $1 FROM generate_series(1, $2) g`, f.now.Add(-time.Hour), backlog)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := f.svc.Purge(context.Background()); err != nil || n != backlog {
		t.Errorf("Purge() = %d, %v, want %d", n, err, backlog)
	}
}

// staleStore reads memberships outside transactions as they were, then runs
// meanwhile once when armed: a change another request commits after
// RequireMember read the role.
type staleStore struct {
	orgsusecase.Store
	armed     atomic.Bool
	meanwhile func()
}

func (s *staleStore) MemberRole(ctx context.Context, orgID orgslib.ID, userID string) (string, error) {
	role, err := s.Store.MemberRole(ctx, orgID, userID)
	if s.armed.CompareAndSwap(true, false) {
		s.meanwhile()
	}
	return role, err
}

// ORG-4: the caller's role is read again under the organisation's lock.
func TestRoleIsCheckedAgainUnderTheLock(t *testing.T) {
	f := newFixture(t)
	id := f.team(t, orgslib.RoleAdmin)
	f.setMember(t, id, "usr_bob", orgslib.RoleMember)
	store := &staleStore{Store: orgsrepository.NewStore(f.pool)}
	svc, err := orgsusecase.NewService(orgsusecase.Config{
		Store: store, Catalog: catalog(), Recorder: f.audit, Emails: f.emails,
		InvitationURL: config.Static("https://app.example.com/invitations"),
		Now:           func() time.Time { return f.now },
	})
	if err != nil {
		t.Fatal(err)
	}
	carol := as("usr_carol")
	operations := map[string]func() error{
		"Invite": func() error {
			_, err := svc.Invite(carol, id, "erin@example.com", orgslib.RoleMember)
			return err
		},
		"ChangeRole": func() error {
			_, err := svc.ChangeRole(carol, id, "usr_bob", orgslib.RoleAdmin)
			return err
		},
		"RemoveMember": func() error { return svc.RemoveMember(carol, id, "usr_bob") },
		"Rename": func() error {
			m, err := svc.Get(carol, id)
			if err != nil {
				return err
			}
			store.armed.Store(true)
			_, err = svc.Rename(carol, id, "Renamed", m.Version)
			return err
		},
	}
	for name, run := range operations {
		for change, want := range map[string]error{
			`DELETE FROM org_members WHERE org_id = $1 AND user_id = 'usr_carol'`:                orgslib.ErrOrgNotFound,
			`UPDATE org_members SET role = 'member' WHERE org_id = $1 AND user_id = 'usr_carol'`: actor.ErrForbidden,
		} {
			f.setMember(t, id, "usr_carol", orgslib.RoleAdmin)
			store.meanwhile = func() {
				if _, err := f.pool.Exec(context.Background(), change, id); err != nil {
					t.Error(err)
				}
			}
			store.armed.Store(name != "Rename")
			if err := run(); !errors.Is(err, want) {
				t.Errorf("%s() by an admin removed or demoted meanwhile error = %v, want %v", name, err, want)
			}
		}
	}
}

// ORG-5: restoring needs the same step-up as deleting.
func TestRestoreNeedsTheSameStepUpAsDelete(t *testing.T) {
	f := newFixture(t, func(c *orgsusecase.Config) {
		cat := catalog()
		cat.RequireMFA(orgslib.RoleOwner)
		c.Catalog = cat
	})
	password := as("usr_ada")
	verified := authlib.WithPrincipal(password, authlib.Principal{UserID: "usr_ada", MFAVerified: true})
	created, err := f.svc.Create(password, "Road Runners")
	if err != nil {
		t.Fatal(err)
	}
	id := orgslib.ID(created.ID)
	if err := f.svc.Delete(password, id); !errors.Is(err, actor.ErrStepUpRequired) {
		t.Fatalf("Delete() without a second factor error = %v, want ErrStepUpRequired", err)
	}
	if err := f.svc.Delete(verified, id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Restore(password, id); !errors.Is(err, actor.ErrStepUpRequired) {
		t.Errorf("Restore() without a second factor error = %v, want ErrStepUpRequired", err)
	}
	if _, err := f.svc.Restore(verified, id); err != nil {
		t.Errorf("Restore() with a second factor error = %v", err)
	}
}

// ORG-6: a deleted account doesn't stay an owner of an organisation that was
// deleted at the time, and owners with deleted accounts don't count.
func TestDeletedAccountsDontStayOwners(t *testing.T) {
	f := newFixture(t)
	carol := as("usr_carol")
	id := f.team(t, orgslib.RoleOwner)
	if err := f.svc.Delete(carol, id); err != nil {
		t.Fatal(err)
	}
	f.deleteAccount(t, "usr_ada")
	if err := f.svc.RemoveAccount(context.Background(), "usr_ada"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Restore(carol, id); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Leave(carol, id); !errors.Is(err, orgsdomain.ErrLastOwner) {
		t.Errorf("Leave() by the only owner left error = %v, want ErrLastOwner", err)
	}
	if members, err := f.svc.ListMembers(carol, id); err != nil || len(members) != 1 || members[0].UserID != "usr_carol" {
		t.Errorf("ListMembers() after restoring = %+v, %v, want carol only", members, err)
	}

	// A membership a deleted account kept from before doesn't count as an
	// owner, and can be removed.
	ghosts, err := f.svc.Create(as("usr_bob"), "Ghosts")
	if err != nil {
		t.Fatal(err)
	}
	gid := orgslib.ID(ghosts.ID)
	f.setMember(t, gid, "usr_erin", orgslib.RoleOwner)
	f.deleteAccount(t, "usr_bob")
	erin := as("usr_erin")
	if err := f.svc.Leave(erin, gid); !errors.Is(err, orgsdomain.ErrLastOwner) {
		t.Errorf("Leave() next to a deleted owner error = %v, want ErrLastOwner", err)
	}
	if err := f.svc.RemoveMember(erin, gid, "usr_bob"); err != nil {
		t.Errorf("RemoveMember(deleted owner) error = %v", err)
	}
}

// lockRecorder records, inside transactions, which rows use cases lock:
// "org" or "invitation".
type lockRecorder struct {
	orgsusecase.Store
	mu    *sync.Mutex
	locks *[]string
}

func (r lockRecorder) record(what string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	*r.locks = append(*r.locks, what)
}

func (r lockRecorder) InTx(ctx context.Context, fn func(tx orgsusecase.Store) error) error {
	return r.Store.InTx(ctx, func(tx orgsusecase.Store) error {
		return fn(lockRecorder{Store: tx, mu: r.mu, locks: r.locks})
	})
}

func (r lockRecorder) SelectOrg(ctx context.Context, id orgslib.ID, lock bool) (orgsdomain.Org, error) {
	if lock {
		r.record("org")
	}
	return r.Store.SelectOrg(ctx, id, lock)
}

func (r lockRecorder) SelectInvitation(ctx context.Context, orgID orgslib.ID, id string) (orgsdomain.Invitation, error) {
	r.record("invitation")
	return r.Store.SelectInvitation(ctx, orgID, id)
}

func (r lockRecorder) SelectInvitationByTokenHash(ctx context.Context, tokenHash []byte, lock bool) (orgsdomain.Invitation, error) {
	if lock {
		r.record("invitation")
	}
	return r.Store.SelectInvitationByTokenHash(ctx, tokenHash, lock)
}

// ORG-7: every invitation change locks the organisation before the
// invitation, so accepting and resending at once can't deadlock.
func TestInvitationChangesLockTheOrganisationFirst(t *testing.T) {
	f := newFixture(t)
	var locks []string
	rec := lockRecorder{Store: orgsrepository.NewStore(f.pool), mu: &sync.Mutex{}, locks: &locks}
	svc, err := orgsusecase.NewService(orgsusecase.Config{
		Store: rec, Catalog: catalog(), Recorder: f.audit, Emails: f.emails,
		InvitationURL: config.Static("https://app.example.com/invitations"),
		Now:           func() time.Time { return f.now },
	})
	if err != nil {
		t.Fatal(err)
	}
	ada := as("usr_ada")
	created, err := svc.Create(ada, "Road Runners")
	if err != nil {
		t.Fatal(err)
	}
	id := orgslib.ID(created.ID)
	var inv orgsdomain.Invitation
	steps := []struct {
		name string
		run  func() error
	}{
		{"Invite", func() (err error) {
			inv, err = svc.Invite(ada, id, "bob@example.com", orgslib.RoleMember)
			return err
		}},
		{"ResendInvitation", func() error {
			_, err := svc.ResendInvitation(ada, id, inv.ID)
			return err
		}},
		{"AcceptInvitation", func() error {
			_, err := svc.AcceptInvitation(as("usr_bob"), f.emails.token(t, "bob@example.com"))
			return err
		}},
		{"RevokeInvitation", func() error {
			erin, err := svc.Invite(ada, id, "erin@example.com", orgslib.RoleMember)
			if err != nil {
				return err
			}
			locks = nil
			return svc.RevokeInvitation(ada, id, erin.ID)
		}},
	}
	for _, step := range steps {
		locks = nil
		if err := step.run(); err != nil {
			t.Fatalf("%s() error = %v", step.name, err)
		}
		org, invitation := slices.Index(locks, "org"), slices.Index(locks, "invitation")
		if org < 0 || (invitation >= 0 && invitation < org) {
			t.Errorf("%s() locked %v, want the organisation first", step.name, locks)
		}
	}
}
