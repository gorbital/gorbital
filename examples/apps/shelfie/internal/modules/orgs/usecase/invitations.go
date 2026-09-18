package usecase

import (
	"context"
	"crypto/subtle"
	"errors"
	"slices"
	"strings"
	"time"

	authlib "gorbital.dev/modules/auth"
	orgslib "gorbital.dev/modules/orgs"

	orgsdomain "example.com/shelfie/internal/modules/orgs/domain"
)

// Invite invites an email address to join an organisation with role and
// emails a link carrying a single-use token. Nobody can invite with a role
// they couldn't assign, personal workspaces don't take invitations, and the
// inviter's email address must be verified. Only the token's hash is
// stored.
func (s *Service) Invite(ctx context.Context, orgID orgslib.ID, email, role string) (orgsdomain.Invitation, error) {
	ctx, me, err := orgslib.RequireMember(ctx, s.store, s.catalog, orgID, PermMembersManage)
	if err != nil {
		return orgsdomain.Invitation{}, storeError("invite", err)
	}
	if role == "" {
		role = orgslib.RoleMember
	}
	if !s.catalog.HasRole(role) {
		return orgsdomain.Invitation{}, orgsdomain.ErrUnknownRole
	}
	if !s.canAssign(me.Role, role) {
		return orgsdomain.Invitation{}, orgsdomain.ErrRoleNotAllowed
	}
	email, normalized, err := authlib.NormalizeEmail(email)
	if err != nil {
		return orgsdomain.Invitation{}, err
	}
	inviter, err := s.requireVerified(ctx, me.UserID)
	if err != nil {
		return orgsdomain.Invitation{}, storeError("invite", err)
	}
	token, hash := authlib.NewToken()
	ttl := s.invitationTTL.Get(ctx)
	now := s.clock()

	var (
		o   orgsdomain.Org
		inv orgsdomain.Invitation
	)
	err = s.store.InTx(ctx, func(tx Store) error {
		if o, _, err = s.lockForInvitations(ctx, tx, orgID, role, now); err != nil {
			return err
		}
		member, err := tx.IsMemberEmail(ctx, orgID, normalized)
		if err != nil {
			return err
		}
		if member {
			return orgsdomain.ErrAlreadyMember
		}
		if err := tx.RevokeExpiredInvitation(ctx, orgID, normalized, now); err != nil {
			return err
		}
		if err := s.takeInvitation(ctx, me.UserID); err != nil {
			return err
		}
		inv, err = tx.InsertInvitation(ctx, orgsdomain.Invitation{
			ID: authlib.NewID("inv"), OrgID: string(orgID), Email: email, NormalizedEmail: normalized, Role: role,
			TokenHash: hash, InvitedBy: by(ctx), CreatedAt: now, ExpiresAt: now.Add(ttl),
		})
		return err
	})
	if err != nil {
		return orgsdomain.Invitation{}, storeError("invite", err)
	}
	s.sendInvitation(ctx, inviter, o, inv, token, ttl)
	s.audit(ctx, ActionInvitationCreated, orgID, "invitation", inv.ID, map[string]any{"role": role})
	return inv, nil
}

// ListInvitations returns an organisation's invitations that weren't
// accepted or revoked, expired ones included, newest first.
func (s *Service) ListInvitations(ctx context.Context, orgID orgslib.ID) ([]orgsdomain.Invitation, error) {
	ctx, _, err := orgslib.RequireMember(ctx, s.store, s.catalog, orgID, PermMembersManage)
	if err != nil {
		return nil, storeError("list invitations", err)
	}
	list, err := s.store.SelectOpenInvitations(ctx, orgID)
	if err != nil {
		return nil, storeError("list invitations", err)
	}
	return list, nil
}

// ResendInvitation sends an open invitation again with a new token and a
// new expiry; the old link stops working. The member who resends it becomes
// its inviter, as the email says, so it stays valid only while they may
// assign its role.
func (s *Service) ResendInvitation(ctx context.Context, orgID orgslib.ID, id string) (orgsdomain.Invitation, error) {
	ctx, me, err := orgslib.RequireMember(ctx, s.store, s.catalog, orgID, PermMembersManage)
	if err != nil {
		return orgsdomain.Invitation{}, storeError("resend invitation", err)
	}
	inviter, err := s.requireVerified(ctx, me.UserID)
	if err != nil {
		return orgsdomain.Invitation{}, storeError("resend invitation", err)
	}
	token, hash := authlib.NewToken()
	ttl := s.invitationTTL.Get(ctx)
	now := s.clock()
	var (
		o   orgsdomain.Org
		inv orgsdomain.Invitation
	)
	err = s.store.InTx(ctx, func(tx Store) error {
		var me orgslib.Member
		if o, me, err = s.lockForInvitations(ctx, tx, orgID, "", now); err != nil {
			return err
		}
		if inv, err = s.openInvitation(ctx, tx, orgID, id, me.Role); err != nil {
			return err
		}
		if err := s.takeInvitation(ctx, me.UserID); err != nil {
			return err
		}
		inv.TokenHash, inv.InvitedBy, inv.ExpiresAt = hash, by(ctx), now.Add(ttl)
		return tx.ReplaceInvitationToken(ctx, id, hash, inv.InvitedBy, now, inv.ExpiresAt)
	})
	if err != nil {
		return orgsdomain.Invitation{}, storeError("resend invitation", err)
	}
	s.sendInvitation(ctx, inviter, o, inv, token, ttl)
	s.audit(ctx, ActionInvitationResent, orgID, "invitation", id, nil)
	return inv, nil
}

// RevokeInvitation cancels an open invitation whose role the caller may
// assign: owners revoke any, admins those for admins and members.
func (s *Service) RevokeInvitation(ctx context.Context, orgID orgslib.ID, id string) error {
	ctx, _, err := orgslib.RequireMember(ctx, s.store, s.catalog, orgID, PermMembersManage)
	if err != nil {
		return storeError("revoke invitation", err)
	}
	err = s.store.InTx(ctx, func(tx Store) error {
		_, me, err := s.lockOrg(ctx, tx, orgID, PermMembersManage)
		if err != nil {
			return err
		}
		if _, err := s.openInvitation(ctx, tx, orgID, id, me.Role); err != nil {
			return err
		}
		return tx.RevokeInvitation(ctx, id, s.clock())
	})
	if err != nil {
		return storeError("revoke invitation", err)
	}
	s.audit(ctx, ActionInvitationRevoked, orgID, "invitation", id, nil)
	return nil
}

// AcceptInvitation adds the signed-in user to the invitation's organisation.
// The account's verified email address must be the invited one, so a
// forwarded link is no use to anyone else. The inviter must still be a
// member who may assign the invitation's role: an invitation doesn't
// outlive its inviter's removal or demotion. An API key can't accept one
// (ErrSessionRequired).
func (s *Service) AcceptInvitation(ctx context.Context, token string) (orgsdomain.Membership, error) {
	uid, err := requireSession(ctx)
	if err != nil {
		return orgsdomain.Membership{}, err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return orgsdomain.Membership{}, orgsdomain.ErrInvitationNotFound
	}
	_, normalized, verified, err := s.store.SelectUserEmail(ctx, uid)
	if errors.Is(err, orgsdomain.ErrMemberNotFound) {
		return orgsdomain.Membership{}, orgsdomain.ErrUnauthenticated
	}
	if err != nil {
		return orgsdomain.Membership{}, storeError("accept invitation", err)
	}
	now := s.clock()
	var (
		accepted orgsdomain.Membership
		inv      orgsdomain.Invitation
	)
	hash := authlib.HashToken(token)
	found, err := s.store.SelectInvitationByTokenHash(ctx, hash, false)
	if err != nil {
		return orgsdomain.Membership{}, storeError("accept invitation", err)
	}
	orgID := orgslib.ID(found.OrgID)
	err = s.store.InTx(ctx, func(tx Store) error {
		// Lock the organisation, then the invitation: the order every
		// invitation change uses, so accepting and resending at once can't
		// deadlock (security review ORG-7).
		o, err := tx.SelectOrg(ctx, orgID, true)
		if errors.Is(err, orgslib.ErrOrgNotFound) {
			return orgsdomain.ErrInvitationNotFound
		}
		if err != nil {
			return err
		}
		if inv, err = tx.SelectInvitation(ctx, orgID, found.ID); err != nil {
			return err
		}
		if subtle.ConstantTimeCompare(inv.TokenHash, hash) != 1 || !inv.PendingAt(now) {
			return orgsdomain.ErrInvitationNotFound // resent or ended meanwhile
		}
		if !verified || normalized != inv.NormalizedEmail {
			return orgsdomain.ErrInvitationEmail
		}
		if !s.catalog.HasRole(inv.Role) {
			return orgsdomain.ErrUnknownRole
		}
		if err := s.checkInviter(ctx, tx, inv); err != nil {
			return err
		}
		err = tx.InsertMember(ctx, orgsdomain.Member{OrgID: o.ID, UserID: uid, Role: inv.Role, JoinedAt: now, AddedBy: "invitation:" + inv.ID})
		if err != nil {
			return err
		}
		accepted = orgsdomain.Membership{Org: o, Role: inv.Role}
		return tx.AcceptInvitation(ctx, inv.ID, now)
	})
	if err != nil {
		return orgsdomain.Membership{}, storeError("accept invitation", err)
	}
	s.audit(ctx, ActionInvitationAccepted, orgslib.ID(inv.OrgID), "invitation", inv.ID, nil)
	s.audit(ctx, ActionMemberAdded, orgslib.ID(inv.OrgID), "user", uid, map[string]any{"role": inv.Role, "invitation_id": inv.ID})
	return accepted, nil
}

// lockForInvitations locks a live organisation that takes invitations,
// checks under the lock that the caller may manage members and assign role
// (unless empty), and checks the organisation's hourly limit.
func (s *Service) lockForInvitations(ctx context.Context, tx Store, orgID orgslib.ID, role string, now time.Time) (orgsdomain.Org, orgslib.Member, error) {
	o, me, err := s.lockOrg(ctx, tx, orgID, PermMembersManage)
	if err != nil {
		return orgsdomain.Org{}, orgslib.Member{}, err
	}
	if role != "" && !s.canAssign(me.Role, role) {
		return orgsdomain.Org{}, orgslib.Member{}, orgsdomain.ErrRoleNotAllowed
	}
	if o.Personal {
		return orgsdomain.Org{}, orgslib.Member{}, orgsdomain.ErrPersonalWorkspace
	}
	sent, err := tx.CountInvitationsSince(ctx, orgID, now.Add(-time.Hour))
	if err != nil {
		return orgsdomain.Org{}, orgslib.Member{}, err
	}
	if sent >= InvitationsPerHour {
		return orgsdomain.Org{}, orgslib.Member{}, orgsdomain.ErrTooManyInvitations
	}
	return o, me, nil
}

// openInvitation returns an invitation that wasn't accepted or revoked and
// whose role a member with myRole may assign, and locks it. The caller locks
// the organisation first with lockOrg, which reads myRole.
func (s *Service) openInvitation(ctx context.Context, tx Store, orgID orgslib.ID, id, myRole string) (orgsdomain.Invitation, error) {
	inv, err := tx.SelectInvitation(ctx, orgID, id)
	if err != nil {
		return orgsdomain.Invitation{}, err
	}
	if inv.AcceptedAt != nil || inv.RevokedAt != nil {
		return orgsdomain.Invitation{}, orgsdomain.ErrInvitationNotFound
	}
	if !s.canAssign(myRole, inv.Role) {
		return orgsdomain.Invitation{}, orgsdomain.ErrRoleNotAllowed
	}
	return inv, nil
}

// checkInviter returns ErrInvitationNotFound unless the invitation's inviter
// still has a live account and is a member whose role may manage members
// and assign the invitation's role. Without it, an admin or owner who
// expects to be removed could invite an address they control and come back
// after the removal (security review ORG-1). The organisation must be
// locked.
func (s *Service) checkInviter(ctx context.Context, tx Store, inv orgsdomain.Invitation) error {
	inviterID, ok := strings.CutPrefix(inv.InvitedBy, "user:")
	if !ok || inviterID == "" {
		return orgsdomain.ErrInvitationNotFound
	}
	if _, _, _, err := tx.SelectUserEmail(ctx, inviterID); err != nil {
		if errors.Is(err, orgsdomain.ErrMemberNotFound) {
			return orgsdomain.ErrInvitationNotFound
		}
		return err
	}
	role, err := tx.MemberRole(ctx, orgslib.ID(inv.OrgID), inviterID)
	if errors.Is(err, orgslib.ErrNotMember) {
		return orgsdomain.ErrInvitationNotFound
	}
	if err != nil {
		return err
	}
	if !slices.Contains(s.catalog.Permissions(role), PermMembersManage) || !s.canAssign(role, inv.Role) {
		return orgsdomain.ErrInvitationNotFound
	}
	return nil
}

// takeInvitation takes one invitation from userID's hourly budget across
// organisations, or returns ErrTooManyInvitations. A limiter that can't
// decide allows it: shared limiters fall back to memory themselves.
func (s *Service) takeInvitation(ctx context.Context, userID string) error {
	d, err := s.invitations.Take(ctx, "user:"+userID)
	if err != nil {
		s.logger.ErrorContext(ctx, "invitation limiter couldn't decide; allowing the invitation", "user_id", userID, "err", err)
		return nil
	}
	if !d.Allowed {
		return orgsdomain.ErrTooManyInvitations
	}
	return nil
}

// sendInvitation emails the invitation link from inviter, the inviter's
// email address; a failure is logged, and the invitation can be resent.
func (s *Service) sendInvitation(ctx context.Context, inviter string, o orgsdomain.Org, inv orgsdomain.Invitation, token string, ttl time.Duration) {
	link := strings.TrimRight(s.invitationURL.Get(ctx), "/") + "#token=" + token
	err := s.emails.SendInvitation(ctx, inv.Email, orgslib.Invitation{
		OrgName: o.Name, InvitedBy: inviter, Role: inv.Role, URL: link, ExpiresIn: ttl,
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "send invitation email", "invitation_id", inv.ID, "err", err)
	}
}
