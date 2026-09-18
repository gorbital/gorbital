package usecase

import (
	"context"

	orgslib "gorbital.dev/modules/orgs"

	orgsdomain "example.com/invoicing/internal/modules/orgs/domain"
)

// ListMembers returns an organisation's members, owners first.
func (s *Service) ListMembers(ctx context.Context, orgID orgslib.ID) ([]orgsdomain.Member, error) {
	ctx, _, err := orgslib.RequireMember(ctx, s.store, s.catalog, orgID, PermMembersRead)
	if err != nil {
		return nil, storeError("list members", err)
	}
	members, err := s.store.SelectMembers(ctx, orgID)
	if err != nil {
		return nil, storeError("list members", err)
	}
	return members, nil
}

// ChangeRole gives a member another role. Nobody can give or change a role
// above their own, only owners change owners, and the last owner can't be
// demoted.
func (s *Service) ChangeRole(ctx context.Context, orgID orgslib.ID, memberID, role string) (orgsdomain.Member, error) {
	ctx, _, err := orgslib.RequireMember(ctx, s.store, s.catalog, orgID, PermMembersManage)
	if err != nil {
		return orgsdomain.Member{}, storeError("change role", err)
	}
	if !s.catalog.HasRole(role) {
		return orgsdomain.Member{}, orgsdomain.ErrUnknownRole
	}
	var target orgsdomain.Member
	var from string
	err = s.store.InTx(ctx, func(tx Store) error {
		_, me, err := s.lockOrg(ctx, tx, orgID, PermMembersManage)
		if err != nil {
			return err
		}
		if target, err = tx.SelectMember(ctx, orgID, memberID); err != nil {
			return err
		}
		from = target.Role
		if from == role {
			return nil
		}
		if !s.canAssign(me.Role, from) || !s.canAssign(me.Role, role) {
			return orgsdomain.ErrRoleNotAllowed
		}
		if err := s.keepAnOwner(ctx, tx, orgID, memberID, from); err != nil {
			return err
		}
		target.Role = role
		return tx.UpdateMemberRole(ctx, orgID, memberID, role)
	})
	if err != nil {
		return orgsdomain.Member{}, storeError("change role", err)
	}
	if from != role {
		s.audit(ctx, ActionMemberRoleChanged, orgID, "user", memberID, map[string]any{"from": from, "to": role})
	}
	return target, nil
}

// RemoveMember removes a member. Removing yourself is Leave.
func (s *Service) RemoveMember(ctx context.Context, orgID orgslib.ID, memberID string) error {
	if uid, err := userID(ctx); err == nil && uid == memberID {
		return s.Leave(ctx, orgID)
	}
	ctx, _, err := orgslib.RequireMember(ctx, s.store, s.catalog, orgID, PermMembersManage)
	if err != nil {
		return storeError("remove member", err)
	}
	var role string
	err = s.store.InTx(ctx, func(tx Store) error {
		_, me, err := s.lockOrg(ctx, tx, orgID, PermMembersManage)
		if err != nil {
			return err
		}
		target, err := tx.SelectMember(ctx, orgID, memberID)
		if err != nil {
			return err
		}
		role = target.Role
		if !s.canAssign(me.Role, role) {
			return orgsdomain.ErrRoleNotAllowed
		}
		if err := s.keepAnOwner(ctx, tx, orgID, memberID, role); err != nil {
			return err
		}
		return tx.DeleteMember(ctx, orgID, memberID)
	})
	if err != nil {
		return storeError("remove member", err)
	}
	s.audit(ctx, ActionMemberRemoved, orgID, "user", memberID, map[string]any{"role": role})
	return nil
}

// Leave removes the signed-in user from an organisation. The last owner
// can't leave, nobody leaves their personal workspace, and an API key can't
// (ErrSessionRequired).
func (s *Service) Leave(ctx context.Context, orgID orgslib.ID) error {
	if _, err := requireSession(ctx); err != nil {
		return err
	}
	ctx, me, err := orgslib.RequireMember(ctx, s.store, s.catalog, orgID, PermOrgRead)
	if err != nil {
		return storeError("leave", err)
	}
	err = s.store.InTx(ctx, func(tx Store) error {
		o, current, err := s.lockOrg(ctx, tx, orgID, PermOrgRead)
		if err != nil {
			return err
		}
		me = current
		if o.Personal {
			return orgsdomain.ErrPersonalWorkspace
		}
		if err := s.keepAnOwner(ctx, tx, orgID, me.UserID, me.Role); err != nil {
			return err
		}
		return tx.DeleteMember(ctx, orgID, me.UserID)
	})
	if err != nil {
		return storeError("leave", err)
	}
	s.audit(ctx, ActionMemberLeft, orgID, "user", me.UserID, map[string]any{"role": me.Role})
	return nil
}

// keepAnOwner returns ErrLastOwner when userID, a member with role, is about
// to stop being an owner and no other owner has a live account. Owners whose
// accounts are deleted don't count (security review ORG-6), and can be
// removed. The organisation row must be locked.
func (s *Service) keepAnOwner(ctx context.Context, tx Store, orgID orgslib.ID, userID, role string) error {
	if role != orgslib.RoleOwner {
		return nil
	}
	others, err := tx.CountOwners(ctx, orgID, userID)
	if err != nil {
		return err
	}
	if others == 0 {
		return orgsdomain.ErrLastOwner
	}
	return nil
}
