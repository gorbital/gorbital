package orgs_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"gorbital.dev/actor"
	"gorbital.dev/mail"
	"gorbital.dev/modules/auth"
	"gorbital.dev/modules/orgs"
)

func TestIDs(t *testing.T) {
	id := orgs.NewID()
	if !strings.HasPrefix(id.String(), "org_") || len(id) != 30 {
		t.Fatalf("NewID() = %q, want org_ and 26 characters", id)
	}
	if other := orgs.NewID(); other == id {
		t.Fatalf("NewID() returned %q twice", id)
	}
	if got, err := orgs.ParseID(string(id)); err != nil || got != id {
		t.Errorf("ParseID(%q) = %q, %v", id, got, err)
	}
	for _, bad := range []string{"", "org_", "usr_" + string(id)[4:], string(id) + "a", "org_ABCDEFGHIJKLMNOPQRSTUVWXYZ", "org_0000000000000000000000000!"} {
		if _, err := orgs.ParseID(bad); !errors.Is(err, orgs.ErrInvalidID) {
			t.Errorf("ParseID(%q) error = %v, want ErrInvalidID", bad, err)
		}
	}
}

type memberships map[string]string // "org/user" -> role

func (m memberships) MemberRole(_ context.Context, orgID orgs.ID, userID string) (string, error) {
	if role, ok := m[string(orgID)+"/"+userID]; ok {
		return role, nil
	}
	return "", orgs.ErrNotMember
}

type failingMemberships struct{ err error }

func (f failingMemberships) MemberRole(context.Context, orgs.ID, string) (string, error) {
	return "", f.err
}

func catalog() *auth.Catalog {
	c := auth.NewCatalog()
	c.Permission("orgs.org.read", "Read the organisation")
	c.Permission("orgs.org.delete", "Delete the organisation")
	c.Role(orgs.RoleOwner, "Everything", "orgs.org.read", "orgs.org.delete")
	c.Role(orgs.RoleMember, "Use the organisation", "orgs.org.read")
	c.Role("auditor", "Owner-level reads that need 2FA", "orgs.org.read", "orgs.org.delete")
	c.RequireMFA("auditor")
	c.Freeze()
	return c
}

func signedIn(userID string, mfa bool, platformPerms ...string) context.Context {
	ctx := auth.WithPrincipal(context.Background(), auth.Principal{UserID: userID, MFAVerified: mfa})
	return actor.With(ctx, actor.Actor{Kind: actor.KindUser, ID: userID, Permissions: platformPerms})
}

func TestRequireMember(t *testing.T) {
	org, other := orgs.NewID(), orgs.NewID()
	m := memberships{
		string(org) + "/usr_owner":   orgs.RoleOwner,
		string(org) + "/usr_member":  orgs.RoleMember,
		string(org) + "/usr_auditor": "auditor",
		string(other) + "/usr_admin": "platform_admin", // a role the org catalog doesn't declare grants nothing
	}
	c := catalog()

	tests := []struct {
		name       string
		ctx        context.Context
		org        orgs.ID
		permission string
		wantErr    error
	}{
		{"owner", signedIn("usr_owner", false), org, "orgs.org.delete", nil},
		{"member reads", signedIn("usr_member", false), org, "orgs.org.read", nil},
		{"member can't delete", signedIn("usr_member", false), org, "orgs.org.delete", actor.ErrForbidden},
		{"not a member", signedIn("usr_owner", false), other, "orgs.org.read", orgs.ErrOrgNotFound},
		{"platform permissions don't count", signedIn("usr_outsider", false, "orgs.org.read"), org, "orgs.org.read", orgs.ErrOrgNotFound},
		{"undeclared role", signedIn("usr_admin", false), other, "orgs.org.read", actor.ErrForbidden},
		{"malformed org ID", signedIn("usr_owner", false), "org_nope", "orgs.org.read", orgs.ErrOrgNotFound},
		{"role needing 2FA without it", signedIn("usr_auditor", false), org, "orgs.org.read", actor.ErrStepUpRequired},
		{"role needing 2FA with it", signedIn("usr_auditor", true), org, "orgs.org.delete", nil},
		{"anonymous", actor.With(context.Background(), actor.Anonymous), org, "orgs.org.read", actor.ErrUnauthenticated},
		{"system actor", actor.With(context.Background(), actor.System("job")), org, "orgs.org.read", actor.ErrUnauthenticated},
		{"no actor", context.Background(), org, "orgs.org.read", actor.ErrUnauthenticated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, member, err := orgs.RequireMember(tt.ctx, m, c, tt.org, tt.permission)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("RequireMember() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			a, _ := actor.From(ctx)
			if a.OrgID != string(tt.org) || member.OrgID != tt.org || member.UserID != a.ID || member.Role == "" {
				t.Errorf("actor = %+v, member = %+v; want both in %s", a, member, tt.org)
			}
			if !a.Can(tt.permission) || slices.Contains(a.Permissions, "ops.settings.read") {
				t.Errorf("actor permissions = %v, want the role's permissions only", a.Permissions)
			}
		})
	}
}

// TestAuthorize checks the permission path for a membership read another
// way, such as in a deleted organisation: the same step-up as RequireMember
// (security review ORG-5).
func TestAuthorize(t *testing.T) {
	org := orgs.NewID()
	c := catalog()
	tests := []struct {
		name    string
		ctx     context.Context
		member  orgs.Member
		wantErr error
	}{
		{"owner", signedIn("usr_owner", false), orgs.Member{OrgID: org, UserID: "usr_owner", Role: orgs.RoleOwner}, nil},
		{"member", signedIn("usr_member", false), orgs.Member{OrgID: org, UserID: "usr_member", Role: orgs.RoleMember}, actor.ErrForbidden},
		{"role needing 2FA without it", signedIn("usr_auditor", false), orgs.Member{OrgID: org, UserID: "usr_auditor", Role: "auditor"}, actor.ErrStepUpRequired},
		{"role needing 2FA with it", signedIn("usr_auditor", true), orgs.Member{OrgID: org, UserID: "usr_auditor", Role: "auditor"}, nil},
		{"someone else's membership", signedIn("usr_member", false), orgs.Member{OrgID: org, UserID: "usr_owner", Role: orgs.RoleOwner}, orgs.ErrOrgNotFound},
		{"malformed org ID", signedIn("usr_owner", false), orgs.Member{OrgID: "org_nope", UserID: "usr_owner", Role: orgs.RoleOwner}, orgs.ErrOrgNotFound},
		{"system actor", actor.With(context.Background(), actor.System("job")), orgs.Member{OrgID: org, UserID: "job", Role: orgs.RoleOwner}, actor.ErrUnauthenticated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, err := orgs.Authorize(tt.ctx, c, tt.member, "orgs.org.delete")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Authorize() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if a, _ := actor.From(ctx); a.OrgID != string(org) || !a.Can("orgs.org.delete") {
				t.Errorf("actor = %+v, want acting in %s with the role's permissions", a, org)
			}
		})
	}
}

// withKey is a request authenticated with an API key: a user's (userID
// usr_…) or a service account's (svc_…, of orgID), limited to scopes.
func withKey(id string, orgID orgs.ID, scopes ...string) context.Context {
	p := auth.Principal{APIKeyID: "key_1", Scopes: scopes}
	if strings.HasPrefix(id, "svc_") {
		p.ServiceAccountID, p.OrgID = id, string(orgID)
	} else {
		p.UserID = id
	}
	return auth.WithPrincipal(context.Background(), p)
}

// TestRequireMemberWithAPIKeys checks what API keys reach in an organisation
// (ADR-0058): never permissions of roles that require two-factor
// authentication, only the key's scopes, and for a service account only its
// own organisation.
func TestRequireMemberWithAPIKeys(t *testing.T) {
	org, other := orgs.NewID(), orgs.NewID()
	m := memberships{
		string(org) + "/usr_owner":     orgs.RoleOwner,
		string(org) + "/usr_auditor":   "auditor",
		string(org) + "/svc_robot":     orgs.RoleMember,
		string(other) + "/svc_robot":   orgs.RoleOwner, // a lying store can't take a service account elsewhere
		string(org) + "/svc_elsewhere": orgs.RoleOwner,
	}
	c := catalog()
	tests := []struct {
		name       string
		ctx        context.Context
		org        orgs.ID
		permission string
		wantErr    error
		wantPerms  []string
	}{
		{"owner's key without scopes", withKey("usr_owner", ""), org, "orgs.org.delete", nil, []string{"orgs.org.delete", "orgs.org.read"}},
		{"owner's key scoped to reading", withKey("usr_owner", "", "orgs.org.read"), org, "orgs.org.read", nil, []string{"orgs.org.read"}},
		{"scope escalation: reading key deletes", withKey("usr_owner", "", "orgs.org.read"), org, "orgs.org.delete", actor.ErrForbidden, nil},
		{"scope the role doesn't grant", withKey("usr_auditor", "", "orgs.org.delete"), org, "orgs.org.delete", actor.ErrForbidden, nil},
		{"role needing 2FA is never reachable", withKey("usr_auditor", ""), org, "orgs.org.read", actor.ErrForbidden, nil},
		{"service account in its organisation", withKey("svc_robot", org), org, "orgs.org.read", nil, []string{"orgs.org.read"}},
		{"service account above its role", withKey("svc_robot", org), org, "orgs.org.delete", actor.ErrForbidden, nil},
		{"service account in another organisation", withKey("svc_robot", org), other, "orgs.org.read", orgs.ErrOrgNotFound, nil},
		{"service account of another organisation", withKey("svc_elsewhere", other), org, "orgs.org.read", orgs.ErrOrgNotFound, nil},
		{"service account without an organisation", withKey("svc_robot", ""), org, "orgs.org.read", orgs.ErrOrgNotFound, nil},
		{"service actor without a key", actor.With(context.Background(), actor.Actor{Kind: actor.KindService, ID: "svc_robot"}), org, "orgs.org.read", actor.ErrUnauthenticated, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _, err := orgs.RequireMember(tt.ctx, m, c, tt.org, tt.permission)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("RequireMember() error = %v, want %v", err, tt.wantErr)
			}
			if a, _ := actor.From(ctx); err == nil && (!slices.Equal(a.Permissions, tt.wantPerms) || len(a.StepUp) != 0) {
				t.Errorf("actor = %+v, want permissions %v and no step-up", a, tt.wantPerms)
			}
			if !errors.Is(err, orgs.ErrOrgNotFound) && !errors.Is(err, actor.ErrUnauthenticated) {
				// Authorize, used on a membership read another way, agrees.
				member := orgs.Member{OrgID: tt.org, UserID: actor.FromOrAnonymous(tt.ctx).ID, Role: m[string(tt.org)+"/"+actor.FromOrAnonymous(tt.ctx).ID]}
				if _, err := orgs.Authorize(tt.ctx, c, member, tt.permission); !errors.Is(err, tt.wantErr) {
					t.Errorf("Authorize() error = %v, want %v", err, tt.wantErr)
				}
			}
		})
	}
}

func TestRequireMemberPassesStoreErrors(t *testing.T) {
	boom := errors.New("connection refused")
	_, _, err := orgs.RequireMember(signedIn("usr_1", false), failingMemberships{boom}, catalog(), orgs.NewID(), "orgs.org.read")
	if !errors.Is(err, boom) {
		t.Errorf("RequireMember() error = %v, want the store error", err)
	}

	// A store that wraps ErrNotMember still hides the organisation.
	wrapped := fmt.Errorf("select member: %w", orgs.ErrNotMember)
	_, _, err = orgs.RequireMember(signedIn("usr_1", false), failingMemberships{wrapped}, catalog(), orgs.NewID(), "orgs.org.read")
	if !errors.Is(err, orgs.ErrOrgNotFound) {
		t.Errorf("RequireMember() with a wrapped ErrNotMember error = %v, want ErrOrgNotFound", err)
	}
}

type captureSender struct{ msgs []mail.Message }

func (c *captureSender) Send(_ context.Context, m mail.Message) error {
	c.msgs = append(c.msgs, m)
	return nil
}

func TestInvitationEmail(t *testing.T) {
	s := &captureSender{}
	err := orgs.NewMailEmails(s, "Acme").SendInvitation(context.Background(), "sam@example.com", orgs.Invitation{
		OrgName: "Road\r\nBcc: x@evil.test <Runners>", InvitedBy: "ada@example.com", Role: orgs.RoleAdmin,
		URL: "https://app.example.com/invitations#token=abc", ExpiresIn: 7 * 24 * time.Hour,
	})
	if err != nil || len(s.msgs) != 1 {
		t.Fatalf("SendInvitation() = %v, %d messages", err, len(s.msgs))
	}
	m := s.msgs[0]
	if strings.ContainsAny(m.Subject, "\r\n") || m.Subject != "Join Road  Bcc: x@evil.test <Runners> on Acme" {
		t.Errorf("subject = %q", m.Subject)
	}
	for _, want := range []string{"ada@example.com invited you to join", "as admin", "https://app.example.com/invitations#token=abc", "expires in 7 days"} {
		if !strings.Contains(m.Text, want) {
			t.Errorf("text lacks %q:\n%s", want, m.Text)
		}
	}
	if strings.Contains(m.HTML, "<Runners>") || m.To[0].Email != "sam@example.com" || m.Tags["category"] != "orgs_invitation" {
		t.Errorf("message = %+v; want escaped HTML, recipient and category", m)
	}
}
