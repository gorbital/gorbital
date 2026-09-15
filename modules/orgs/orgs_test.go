package orgs_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"apistock.dev/actor"
	"apistock.dev/mail"
	"apistock.dev/modules/auth"
	"apistock.dev/modules/orgs"
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

func TestRequireMemberPassesStoreErrors(t *testing.T) {
	boom := errors.New("connection refused")
	_, _, err := orgs.RequireMember(signedIn("usr_1", false), failingMemberships{boom}, catalog(), orgs.NewID(), "orgs.org.read")
	if !errors.Is(err, boom) {
		t.Errorf("RequireMember() error = %v, want the store error", err)
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
