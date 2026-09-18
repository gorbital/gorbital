package usecase_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/actor"
	"gorbital.dev/audit"
	"gorbital.dev/config"
	authlib "gorbital.dev/modules/auth"
	orgslib "gorbital.dev/modules/orgs"
	"gorbital.dev/modules/postgres/pgtest"
	"gorbital.dev/modules/settings"

	authhttp "example.com/shelfie/internal/modules/auth"
	orgshttp "example.com/shelfie/internal/modules/orgs"
	orgsdomain "example.com/shelfie/internal/modules/orgs/domain"
	orgsrepository "example.com/shelfie/internal/modules/orgs/repository"
	orgsusecase "example.com/shelfie/internal/modules/orgs/usecase"
	"gorbital.dev/gorbital"
)

// These tests run the use cases on the real repository and Docker
// PostgreSQL, with fake emails, audit recorder and clock.

type fakeRecorder struct {
	mu     sync.Mutex
	events []audit.Event
}

func (f *fakeRecorder) Record(ctx context.Context, e audit.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, audit.FromContext(ctx, e))
	return nil
}

func (f *fakeRecorder) actions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, e := range f.events {
		out = append(out, e.Action)
	}
	return out
}

type sentInvitation struct {
	to  string
	inv orgslib.Invitation
}

type fakeEmails struct {
	mu   sync.Mutex
	sent []sentInvitation
}

func (f *fakeEmails) SendInvitation(_ context.Context, to string, inv orgslib.Invitation) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sentInvitation{to: to, inv: inv})
	return nil
}

// token returns the token in the last invitation sent to to.
func (f *fakeEmails) token(t *testing.T, to string) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.sent) - 1; i >= 0; i-- {
		if f.sent[i].to == to {
			_, token, ok := strings.Cut(f.sent[i].inv.URL, "#token=")
			if !ok || token == "" {
				t.Fatalf("invitation URL %q has no token", f.sent[i].inv.URL)
			}
			return token
		}
	}
	t.Fatalf("no invitation was sent to %s", to)
	return ""
}

// migrationsFS holds what the use cases need of a multi-tenant app's
// history, as gorbital.Migrate merges it: the settings tables, sign-in's
// accounts and the organisations module's own migrations.
func migrationsFS(t *testing.T) fs.FS {
	t.Helper()
	files := fstest.MapFS{}
	add := func(m gorbital.Migration) {
		data, err := fs.ReadFile(m.FS, m.File)
		if err != nil {
			t.Fatal(err)
		}
		files[fmt.Sprintf("%d_%s.sql", m.Version, m.Name)] = &fstest.MapFile{Data: data}
	}
	add(gorbital.Migration{Version: 20260914000001, Name: "settings", FS: settings.Migrations, File: "00001_settings.sql"})
	add(gorbital.Migration{Version: 20260918000001, Name: "settings_org_values", FS: settings.Migrations, File: "00002_settings_org_values.sql"})
	for _, m := range append(authhttp.New().Module().Migrations, orgshttp.Module(nil).Migrations...) {
		add(m)
	}
	return files
}

type fixture struct {
	svc    *orgsusecase.Service
	emails *fakeEmails
	audit  *fakeRecorder
	pool   *pgxpool.Pool
	now    time.Time
}

// catalog mirrors internal/app/permissions.go.
func catalog() *authlib.Catalog {
	c := authlib.NewCatalog()
	for _, p := range []string{orgsusecase.PermOrgRead, orgsusecase.PermOrgUpdate, orgsusecase.PermOrgDelete, orgsusecase.PermMembersRead, orgsusecase.PermMembersManage} {
		c.Permission(p, p)
	}
	member := []string{orgsusecase.PermOrgRead, orgsusecase.PermMembersRead}
	admin := append([]string{orgsusecase.PermOrgUpdate, orgsusecase.PermMembersManage}, member...)
	c.Role(orgslib.RoleOwner, "owner", append([]string{orgsusecase.PermOrgDelete}, admin...)...)
	c.Role(orgslib.RoleAdmin, "admin", admin...)
	c.Role(orgslib.RoleMember, "member", member...)
	return c
}

// newFixture returns the use cases on a fresh database with verified
// accounts usr_ada, usr_bob, usr_carol and usr_erin, and usr_dan, whose
// address isn't verified. opts change the service's configuration.
func newFixture(t *testing.T, opts ...func(*orgsusecase.Config)) *fixture {
	t.Helper()
	f := &fixture{
		emails: &fakeEmails{}, audit: &fakeRecorder{},
		pool: pgtest.New(t, pgtest.WithMigrations(migrationsFS(t))),
		now:  time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC),
	}
	for _, name := range []string{"ada", "bob", "carol", "dan", "erin"} {
		var verified *time.Time
		if name != "dan" {
			verified = &f.now
		}
		_, err := f.pool.Exec(context.Background(),
			`INSERT INTO auth_users (id, email, email_normalized, email_verified_at, created_at, updated_at) VALUES ($1, $2, lower($2), $3, $4, $4)`,
			"usr_"+name, strings.ToUpper(name[:1])+name[1:]+"@example.com", verified, f.now)
		if err != nil {
			t.Fatal(err)
		}
	}
	c := orgsusecase.Config{
		Store: orgsrepository.NewStore(f.pool), Catalog: catalog(), Recorder: f.audit, Emails: f.emails,
		InvitationURL: config.Static("https://app.example.com/invitations/"),
		Now:           func() time.Time { return f.now },
	}
	for _, opt := range opts {
		opt(&c)
	}
	svc, err := orgsusecase.NewService(c)
	if err != nil {
		t.Fatal(err)
	}
	f.svc = svc
	return f
}

// as returns a request signed in as userID with the platform permissions
// every user holds through the user role.
func as(userID string) context.Context {
	return actor.With(context.Background(), actor.Actor{
		Kind: actor.KindUser, ID: userID, Permissions: []string{orgsusecase.PermOrgCreate, orgsusecase.PermOrgList},
	})
}

// withKey returns a request made with an API key of userID limited to
// scopes, as the auth middleware prepares it.
func withKey(userID string, scopes ...string) context.Context {
	p := authlib.Principal{
		UserID: userID, APIKeyID: "key_test", Scopes: scopes,
		Permissions: []string{orgsusecase.PermOrgCreate, orgsusecase.PermOrgList},
	}
	return authlib.WithPrincipal(context.Background(), p)
}

// TestAPIKeysNeedScopesOrASession checks the orgs module's limits on API
// keys (ADR-0058): creating and listing organisations need the user role's
// permissions in the key's scopes, organisation permissions are limited to
// them too, and joining or leaving needs the person's session.
func TestAPIKeysNeedScopesOrASession(t *testing.T) {
	f := newFixture(t)
	id := f.team(t, orgslib.RoleAdmin)

	readOnly := withKey("usr_ada", orgsusecase.PermOrgRead)
	if _, err := f.svc.Create(readOnly, "By a key"); !errors.Is(err, orgsdomain.ErrForbidden) {
		t.Errorf("Create() with a key scoped without orgs.org.create error = %v, want ErrForbidden", err)
	}
	if _, err := f.svc.List(readOnly); !errors.Is(err, orgsdomain.ErrForbidden) {
		t.Errorf("List() with a key scoped without orgs.org.list error = %v, want ErrForbidden", err)
	}
	if _, err := f.svc.Get(readOnly, id); err != nil {
		t.Errorf("Get() with orgs.org.read error = %v", err)
	}
	if _, err := f.svc.Rename(readOnly, id, "Renamed", 1); !errors.Is(err, actor.ErrForbidden) {
		t.Errorf("Rename() with orgs.org.read only error = %v, want actor.ErrForbidden", err)
	}
	if _, err := f.svc.Create(withKey("usr_ada", orgsusecase.PermOrgCreate), "By a key"); err != nil {
		t.Errorf("Create() with orgs.org.create error = %v", err)
	}
	if list, err := f.svc.List(withKey("usr_ada")); err != nil || len(list) != 3 {
		t.Errorf("List() with an unscoped key = %+v, %v, want 3 organisations", list, err)
	}

	unscoped := withKey("usr_carol")
	if err := f.svc.Leave(unscoped, id); !errors.Is(err, orgsdomain.ErrSessionRequired) {
		t.Errorf("Leave() with a key error = %v, want ErrSessionRequired", err)
	}
	if err := f.svc.RemoveMember(unscoped, id, "usr_carol"); !errors.Is(err, orgsdomain.ErrSessionRequired) {
		t.Errorf("RemoveMember(yourself) with a key error = %v, want ErrSessionRequired", err)
	}
	if _, err := f.svc.Invite(as("usr_ada"), id, "bob@example.com", orgslib.RoleMember); err != nil {
		t.Fatal(err)
	}
	token := f.emails.token(t, "bob@example.com")
	if _, err := f.svc.AcceptInvitation(withKey("usr_bob"), token); !errors.Is(err, orgsdomain.ErrSessionRequired) {
		t.Errorf("AcceptInvitation() with a key error = %v, want ErrSessionRequired", err)
	}
	if _, err := f.svc.AcceptInvitation(as("usr_bob"), token); err != nil {
		t.Errorf("AcceptInvitation() with a session after a key's refusal error = %v", err)
	}
}

// team creates an organisation owned by ada, with carol invited and accepted
// as role.
func (f *fixture) team(t *testing.T, role string) orgslib.ID {
	t.Helper()
	m, err := f.svc.Create(as("usr_ada"), "Road Runners")
	if err != nil {
		t.Fatal(err)
	}
	id := orgslib.ID(m.ID)
	if _, err := f.svc.Invite(as("usr_ada"), id, "carol@example.com", role); err != nil {
		t.Fatalf("Invite(carol) error = %v", err)
	}
	if _, err := f.svc.AcceptInvitation(as("usr_carol"), f.emails.token(t, "carol@example.com")); err != nil {
		t.Fatalf("AcceptInvitation(carol) error = %v", err)
	}
	return id
}

func TestPersonalWorkspace(t *testing.T) {
	f := newFixture(t)
	ctx := as("usr_ada")
	first, err := f.svc.EnsurePersonalWorkspace(ctx, "usr_ada")
	if err != nil || !first.Personal || first.Name != orgsdomain.PersonalName || first.CreatedBy != "usr_ada" {
		t.Fatalf("EnsurePersonalWorkspace() = %+v, %v", first, err)
	}
	if again, err := f.svc.EnsurePersonalWorkspace(ctx, "usr_ada"); err != nil || again.ID != first.ID {
		t.Errorf("EnsurePersonalWorkspace() again = %+v, %v, want the same workspace", again, err)
	}

	// Listing creates it if an earlier step failed.
	list, err := f.svc.List(as("usr_bob"))
	if err != nil || len(list) != 1 || !list[0].Personal || list[0].Role != orgslib.RoleOwner {
		t.Fatalf("List() for an account without a workspace = %+v, %v", list, err)
	}
	if _, err := f.svc.Create(ctx, "  Acme  "); err != nil {
		t.Fatal(err)
	}
	if list, _ := f.svc.List(ctx); len(list) != 2 || list[0].ID != first.ID || list[1].Name != "Acme" {
		t.Errorf("List() = %+v, want the personal workspace first, then Acme", list)
	}

	id := orgslib.ID(first.ID)
	if err := f.svc.Leave(ctx, id); !errors.Is(err, orgsdomain.ErrPersonalWorkspace) {
		t.Errorf("Leave(personal) error = %v, want ErrPersonalWorkspace", err)
	}
	if err := f.svc.Delete(ctx, id); !errors.Is(err, orgsdomain.ErrPersonalWorkspace) {
		t.Errorf("Delete(personal) error = %v, want ErrPersonalWorkspace", err)
	}
	if _, err := f.svc.Invite(ctx, id, "bob@example.com", orgslib.RoleMember); !errors.Is(err, orgsdomain.ErrPersonalWorkspace) {
		t.Errorf("Invite(into personal) error = %v, want ErrPersonalWorkspace", err)
	}
	if renamed, err := f.svc.Rename(ctx, id, "Ada's things", first.Version); err != nil || renamed.Name != "Ada's things" {
		t.Errorf("Rename(personal) = %+v, %v, want renamed", renamed, err)
	}
}

func TestOrganisationLifecycle(t *testing.T) {
	f := newFixture(t)
	ada, bob := as("usr_ada"), as("usr_bob")
	for _, name := range []string{"", " ", strings.Repeat("x", 101), "two\nlines"} {
		if _, err := f.svc.Create(ada, name); !errors.Is(err, orgsdomain.ErrInvalidName) {
			t.Errorf("Create(%q) error = %v, want ErrInvalidName", name, err)
		}
	}
	id := f.team(t, orgslib.RoleAdmin)
	m, err := f.svc.Get(ada, id)
	if err != nil || m.Role != orgslib.RoleOwner || m.Personal {
		t.Fatalf("Get() = %+v, %v", m, err)
	}

	// Outside the organisation, it doesn't exist.
	if _, err := f.svc.Get(bob, id); !errors.Is(err, orgslib.ErrOrgNotFound) {
		t.Errorf("Get() by a non-member error = %v, want ErrOrgNotFound", err)
	}
	if _, err := f.svc.Get(context.Background(), id); !errors.Is(err, actor.ErrUnauthenticated) {
		t.Errorf("Get() without an actor error = %v, want actor.ErrUnauthenticated", err)
	}

	if _, err := f.svc.Rename(ada, id, "Coyotes", m.Version+1); !errors.Is(err, orgsdomain.ErrOrgVersionConflict) {
		t.Errorf("Rename(stale version) error = %v, want ErrOrgVersionConflict", err)
	}
	renamed, err := f.svc.Rename(as("usr_carol"), id, "Coyotes", m.Version)
	if err != nil || renamed.Name != "Coyotes" || renamed.Version != m.Version+1 {
		t.Errorf("Rename() by an admin = %+v, %v", renamed, err)
	}

	if err := f.svc.Delete(as("usr_carol"), id); !errors.Is(err, actor.ErrForbidden) {
		t.Errorf("Delete() by an admin error = %v, want actor.ErrForbidden", err)
	}
	if err := f.svc.Delete(ada, id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Get(ada, id); !errors.Is(err, orgslib.ErrOrgNotFound) {
		t.Errorf("Get(deleted) error = %v, want ErrOrgNotFound", err)
	}
	for user, want := range map[string]error{"usr_bob": orgslib.ErrOrgNotFound, "usr_carol": actor.ErrForbidden} {
		if _, err := f.svc.Restore(as(user), id); !errors.Is(err, want) {
			t.Errorf("Restore() by %s error = %v, want %v", user, err, want)
		}
	}
	if restored, err := f.svc.Restore(ada, id); err != nil || restored.DeletedAt != nil || restored.Role != orgslib.RoleOwner {
		t.Errorf("Restore() = %+v, %v", restored, err)
	}

	if err := f.svc.Delete(ada, id); err != nil {
		t.Fatal(err)
	}
	if n, err := f.svc.Purge(context.Background()); err != nil || n != 0 {
		t.Errorf("Purge() before the retention ends = %d, %v, want 0", n, err)
	}
	f.now = f.now.Add(orgsusecase.DefaultDeletedOrgRetention + time.Minute)
	if n, err := f.svc.Purge(context.Background()); err != nil || n != 1 {
		t.Errorf("Purge() = %d, %v, want 1", n, err)
	}
	var left int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM org_members WHERE org_id = $1`, id).Scan(&left); err != nil || left != 0 {
		t.Errorf("members of a purged organisation = %d, %v, want 0", left, err)
	}
	if _, err := f.svc.Restore(ada, id); !errors.Is(err, orgslib.ErrOrgNotFound) {
		t.Errorf("Restore(purged) error = %v, want ErrOrgNotFound", err)
	}

	actions := f.audit.actions()
	for _, want := range []string{orgsusecase.ActionOrgCreated, orgsusecase.ActionInvitationCreated, orgsusecase.ActionInvitationAccepted,
		orgsusecase.ActionMemberAdded, orgsusecase.ActionOrgRenamed, orgsusecase.ActionOrgDeleted, orgsusecase.ActionOrgRestored, orgsusecase.ActionOrgPurged} {
		if !slices.Contains(actions, want) {
			t.Errorf("audit actions %v lack %s", actions, want)
		}
	}
	for _, e := range f.audit.events {
		if e.OrgID != string(id) {
			t.Errorf("audit event %s has org %q, want %s", e.Action, e.OrgID, id)
		}
	}

	// An organisation restored and deleted again between a purge listing it
	// and deleting it has a later purge time, so it stays.
	later, err := f.svc.Create(ada, "Later")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Delete(ada, orgslib.ID(later.ID)); err != nil {
		t.Fatal(err)
	}
	if deleted, err := orgsrepository.NewStore(f.pool).DeleteOrg(context.Background(), orgslib.ID(later.ID), f.now); err != nil || deleted {
		t.Errorf("DeleteOrg(before its purge time) = %v, %v, want false", deleted, err)
	}
}

func TestInvitations(t *testing.T) {
	f := newFixture(t)
	ada := as("usr_ada")
	created, err := f.svc.Create(ada, "Road Runners")
	if err != nil {
		t.Fatal(err)
	}
	id := orgslib.ID(created.ID)

	inv, err := f.svc.Invite(ada, id, " Carol@Example.com ", orgslib.RoleAdmin)
	if err != nil || inv.Email != "Carol@Example.com" || inv.NormalizedEmail != "carol@example.com" || inv.Role != orgslib.RoleAdmin ||
		!inv.ExpiresAt.Equal(f.now.Add(orgsusecase.DefaultInvitationTTL)) {
		t.Fatalf("Invite() = %+v, %v", inv, err)
	}
	sent := f.emails.sent[0]
	if sent.to != "Carol@Example.com" || sent.inv.OrgName != "Road Runners" || sent.inv.InvitedBy != "Ada@example.com" ||
		!strings.HasPrefix(sent.inv.URL, "https://app.example.com/invitations#token=") {
		t.Errorf("invitation email = %+v", sent)
	}
	token := f.emails.token(t, "Carol@Example.com")

	for _, tt := range []struct {
		email, role string
		want        error
	}{
		{"carol@example.com", orgslib.RoleMember, orgsdomain.ErrAlreadyInvited},
		{"ada@example.com", orgslib.RoleMember, orgsdomain.ErrAlreadyMember},
		{"not an address", orgslib.RoleMember, authlib.ErrInvalidEmail},
		{"erin@example.com", "boss", orgsdomain.ErrUnknownRole},
	} {
		if _, err := f.svc.Invite(ada, id, tt.email, tt.role); !errors.Is(err, tt.want) {
			t.Errorf("Invite(%s as %s) error = %v, want %v", tt.email, tt.role, err, tt.want)
		}
	}
	if _, err := f.svc.Invite(as("usr_bob"), id, "erin@example.com", orgslib.RoleMember); !errors.Is(err, orgslib.ErrOrgNotFound) {
		t.Errorf("Invite() by a non-member error = %v, want ErrOrgNotFound", err)
	}

	// Only the invited, verified address can accept.
	for user, want := range map[string]error{"usr_bob": orgsdomain.ErrInvitationEmail, "usr_dan": orgsdomain.ErrInvitationEmail} {
		if _, err := f.svc.AcceptInvitation(as(user), token); !errors.Is(err, want) {
			t.Errorf("AcceptInvitation() by %s error = %v, want %v", user, err, want)
		}
	}
	if _, err := f.svc.AcceptInvitation(as("usr_carol"), "wrong-token"); !errors.Is(err, orgsdomain.ErrInvitationNotFound) {
		t.Errorf("AcceptInvitation(wrong token) error = %v, want ErrInvitationNotFound", err)
	}

	// Resending replaces the token.
	if _, err := f.svc.ResendInvitation(ada, id, inv.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AcceptInvitation(as("usr_carol"), token); !errors.Is(err, orgsdomain.ErrInvitationNotFound) {
		t.Errorf("AcceptInvitation(old token after resend) error = %v, want ErrInvitationNotFound", err)
	}
	joined, err := f.svc.AcceptInvitation(as("usr_carol"), f.emails.token(t, "Carol@Example.com"))
	if err != nil || joined.ID != created.ID || joined.Role != orgslib.RoleAdmin {
		t.Fatalf("AcceptInvitation() = %+v, %v", joined, err)
	}
	if _, err := f.svc.AcceptInvitation(as("usr_carol"), f.emails.token(t, "Carol@Example.com")); !errors.Is(err, orgsdomain.ErrInvitationNotFound) {
		t.Errorf("AcceptInvitation(used token) error = %v, want ErrInvitationNotFound", err)
	}

	// An admin can't invite owners; revoking and expiry end invitations.
	carol := as("usr_carol")
	if _, err := f.svc.Invite(carol, id, "erin@example.com", orgslib.RoleOwner); !errors.Is(err, orgsdomain.ErrRoleNotAllowed) {
		t.Errorf("Invite(owner) by an admin error = %v, want ErrRoleNotAllowed", err)
	}
	erin, err := f.svc.Invite(carol, id, "erin@example.com", orgslib.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if open, err := f.svc.ListInvitations(ada, id); err != nil || len(open) != 1 || open[0].ID != erin.ID {
		t.Errorf("ListInvitations() = %+v, %v, want erin's", open, err)
	}
	if err := f.svc.RevokeInvitation(ada, id, erin.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RevokeInvitation(ada, id, erin.ID); !errors.Is(err, orgsdomain.ErrInvitationNotFound) {
		t.Errorf("RevokeInvitation(revoked) error = %v, want ErrInvitationNotFound", err)
	}
	if _, err := f.svc.Invite(ada, id, "bob@example.com", orgslib.RoleMember); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(orgsusecase.DefaultInvitationTTL)
	if _, err := f.svc.AcceptInvitation(as("usr_bob"), f.emails.token(t, "bob@example.com")); !errors.Is(err, orgsdomain.ErrInvitationNotFound) {
		t.Errorf("AcceptInvitation(expired) error = %v, want ErrInvitationNotFound", err)
	}
	// An expired invitation doesn't block a new one.
	if _, err := f.svc.Invite(ada, id, "bob@example.com", orgslib.RoleMember); err != nil {
		t.Errorf("Invite() after the last one expired error = %v", err)
	}
}

func TestInvitationLimit(t *testing.T) {
	f := newFixture(t)
	created, err := f.svc.Create(as("usr_ada"), "Road Runners")
	if err != nil {
		t.Fatal(err)
	}
	id := orgslib.ID(created.ID)
	for i := range orgsusecase.InvitationsPerHour {
		if _, err := f.svc.Invite(as("usr_ada"), id, strings.Repeat("x", i+1)+"@example.com", orgslib.RoleMember); err != nil {
			t.Fatalf("Invite(#%d) error = %v", i+1, err)
		}
	}
	if _, err := f.svc.Invite(as("usr_ada"), id, "one-more@example.com", orgslib.RoleMember); !errors.Is(err, orgsdomain.ErrTooManyInvitations) {
		t.Errorf("Invite() over the hourly limit error = %v, want ErrTooManyInvitations", err)
	}
	f.now = f.now.Add(time.Hour)
	if _, err := f.svc.Invite(as("usr_ada"), id, "one-more@example.com", orgslib.RoleMember); err != nil {
		t.Errorf("Invite() an hour later error = %v", err)
	}
}

func TestRolesAndOwners(t *testing.T) {
	f := newFixture(t)
	ada, carol := as("usr_ada"), as("usr_carol")
	id := f.team(t, orgslib.RoleAdmin)

	if _, err := f.svc.ChangeRole(carol, id, "usr_carol", orgslib.RoleOwner); !errors.Is(err, orgsdomain.ErrRoleNotAllowed) {
		t.Errorf("ChangeRole(owner) by an admin error = %v, want ErrRoleNotAllowed", err)
	}
	if _, err := f.svc.ChangeRole(carol, id, "usr_ada", orgslib.RoleMember); !errors.Is(err, orgsdomain.ErrRoleNotAllowed) {
		t.Errorf("ChangeRole() of an owner by an admin error = %v, want ErrRoleNotAllowed", err)
	}
	if err := f.svc.RemoveMember(carol, id, "usr_ada"); !errors.Is(err, orgsdomain.ErrRoleNotAllowed) {
		t.Errorf("RemoveMember(owner) by an admin error = %v, want ErrRoleNotAllowed", err)
	}
	if _, err := f.svc.ChangeRole(ada, id, "usr_ada", orgslib.RoleAdmin); !errors.Is(err, orgsdomain.ErrLastOwner) {
		t.Errorf("ChangeRole() demoting the last owner error = %v, want ErrLastOwner", err)
	}
	if err := f.svc.Leave(ada, id); !errors.Is(err, orgsdomain.ErrLastOwner) {
		t.Errorf("Leave() by the last owner error = %v, want ErrLastOwner", err)
	}
	if _, err := f.svc.ChangeRole(ada, id, "usr_carol", "boss"); !errors.Is(err, orgsdomain.ErrUnknownRole) {
		t.Errorf("ChangeRole(unknown role) error = %v, want ErrUnknownRole", err)
	}
	if _, err := f.svc.ChangeRole(ada, id, "usr_bob", orgslib.RoleMember); !errors.Is(err, orgsdomain.ErrMemberNotFound) {
		t.Errorf("ChangeRole(non-member) error = %v, want ErrMemberNotFound", err)
	}

	// Hand over ownership, then step back.
	promoted, err := f.svc.ChangeRole(ada, id, "usr_carol", orgslib.RoleOwner)
	if err != nil || promoted.Role != orgslib.RoleOwner || promoted.Email != "Carol@example.com" {
		t.Fatalf("ChangeRole(owner) by an owner = %+v, %v", promoted, err)
	}
	if _, err := f.svc.ChangeRole(ada, id, "usr_ada", orgslib.RoleMember); err != nil {
		t.Errorf("ChangeRole() demoting yourself with another owner error = %v", err)
	}
	members, err := f.svc.ListMembers(ada, id)
	if err != nil || len(members) != 2 || members[0].UserID != "usr_carol" || members[1].Role != orgslib.RoleMember {
		t.Errorf("ListMembers() = %+v, %v, want carol (owner) first", members, err)
	}
	// Ada is a member now: she can't manage members any more.
	if err := f.svc.RemoveMember(ada, id, "usr_carol"); !errors.Is(err, actor.ErrForbidden) {
		t.Errorf("RemoveMember() by a member error = %v, want actor.ErrForbidden", err)
	}
	if err := f.svc.Leave(ada, id); err != nil {
		t.Errorf("Leave() by a member error = %v", err)
	}
	if _, err := f.svc.Get(ada, id); !errors.Is(err, orgslib.ErrOrgNotFound) {
		t.Errorf("Get() after leaving error = %v, want ErrOrgNotFound", err)
	}
}

func TestAccountDeletion(t *testing.T) {
	f := newFixture(t)
	ada := as("usr_ada")
	personal, err := f.svc.EnsurePersonalWorkspace(ada, "usr_ada")
	if err != nil {
		t.Fatal(err)
	}
	shared := f.team(t, orgslib.RoleAdmin)
	solo, err := f.svc.Create(ada, "Side project")
	if err != nil {
		t.Fatal(err)
	}

	err = f.svc.CheckAccountDeletion(ada, "usr_ada")
	var sole *orgsdomain.SoleOwnerError
	if !errors.As(err, &sole) || !errors.Is(err, orgsdomain.ErrSoleOwner) || len(sole.Orgs) != 1 || sole.Orgs[0].OrgID != string(shared) || sole.Orgs[0].Members != 2 {
		t.Fatalf("CheckAccountDeletion() = %v (%+v), want the shared organisation only", err, sole)
	}
	if err := f.svc.CheckAccountDeletion(as("usr_carol"), "usr_carol"); err != nil {
		t.Errorf("CheckAccountDeletion() for an admin error = %v", err)
	}

	// The account was deleted anyway (someone joined after the check): the
	// earliest admin becomes owner; solo organisations are deleted.
	if err := f.svc.RemoveAccount(ada, "usr_ada"); err != nil {
		t.Fatal(err)
	}
	members, err := f.svc.ListMembers(as("usr_carol"), shared)
	if err != nil || len(members) != 1 || members[0].UserID != "usr_carol" || members[0].Role != orgslib.RoleOwner {
		t.Errorf("members after the owner's account was deleted = %+v, %v, want carol as owner", members, err)
	}
	for _, o := range []string{personal.ID, solo.ID} {
		var deleted bool
		if err := f.pool.QueryRow(context.Background(), `SELECT deleted_at IS NOT NULL FROM orgs WHERE id = $1`, o).Scan(&deleted); err != nil || !deleted {
			t.Errorf("organisation %s after its only member's account was deleted: deleted = %v, %v", o, deleted, err)
		}
	}
	if err := f.svc.RemoveAccount(ada, "usr_ada"); err != nil {
		t.Errorf("RemoveAccount() again error = %v, want it to be safe to repeat", err)
	}
}
