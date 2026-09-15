package usecase

import (
	"context"
	"errors"

	orgslib "apistock.dev/modules/orgs"

	orgsdomain "example.com/acme-api/internal/modules/orgs/domain"
)

// EnsurePersonalWorkspace returns userID's personal workspace, creating it
// if the account has none. It is safe to call again and from concurrent
// requests: a unique index allows one per user.
func (s *Service) EnsurePersonalWorkspace(ctx context.Context, userID string) (orgsdomain.Org, error) {
	o, found, err := s.store.SelectPersonalOrg(ctx, userID)
	if err != nil {
		return orgsdomain.Org{}, storeError("personal workspace", err)
	}
	if found {
		return o, nil
	}
	now := s.clock()
	if o, err = orgsdomain.NewOrg(string(orgslib.NewID()), orgsdomain.PersonalName, userID, true, now); err != nil {
		return orgsdomain.Org{}, err
	}
	err = s.store.InTx(ctx, func(tx Store) error {
		if o, err = tx.InsertOrg(ctx, o); err != nil {
			return err
		}
		return tx.InsertMember(ctx, orgsdomain.Member{OrgID: o.ID, UserID: userID, Role: orgslib.RoleOwner, JoinedAt: now, AddedBy: "system:orgs"})
	})
	if err != nil {
		// Another request may have created it at the same moment.
		if existing, found, lookupErr := s.store.SelectPersonalOrg(ctx, userID); lookupErr == nil && found {
			return existing, nil
		}
		return orgsdomain.Org{}, storeError("personal workspace", err)
	}
	s.audit(ctx, ActionOrgCreated, orgslib.ID(o.ID), "org", o.ID, map[string]any{"personal": true})
	return o, nil
}

// CheckAccountDeletion returns a *SoleOwnerError when userID is the only
// owner of an organisation with other members: they must make someone else
// an owner, or delete the organisation, first.
func (s *Service) CheckAccountDeletion(ctx context.Context, userID string) error {
	orgs, err := s.store.SelectUserOrgs(ctx, userID)
	if err != nil {
		return storeError("check account deletion", err)
	}
	var blocking []orgsdomain.SoleOwnership
	for _, o := range orgs {
		if soleOwnerWithOthers(o) {
			blocking = append(blocking, orgsdomain.SoleOwnership{OrgID: string(o.OrgID), Personal: o.Personal, Members: o.Members})
		}
	}
	if len(blocking) > 0 {
		return &orgsdomain.SoleOwnerError{Orgs: blocking}
	}
	return nil
}

func soleOwnerWithOthers(o UserOrg) bool {
	return !o.Personal && !o.Deleted && o.Role == orgslib.RoleOwner && o.Owners == 1 && o.Members > 1
}

// RemoveAccount runs after userID's account is deleted. The personal
// workspace and organisations where the user was the only member are
// deleted, to be purged after the retention period. In other organisations
// the user stops being a member; if they were the only owner (someone joined
// after CheckAccountDeletion), the earliest admin, or else the earliest
// member, becomes owner. It is safe to run again.
func (s *Service) RemoveAccount(ctx context.Context, userID string) error {
	orgs, err := s.store.SelectUserOrgs(ctx, userID)
	if err != nil {
		return storeError("remove account", err)
	}
	var errs []error
	for _, o := range orgs {
		if o.Deleted {
			continue
		}
		if err := s.removeFromOrg(ctx, userID, o); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return storeError("remove account", errors.Join(errs...))
	}
	return nil
}

func (s *Service) removeFromOrg(ctx context.Context, userID string, o UserOrg) error {
	now := s.clock()
	var (
		deleted  bool
		promoted string
	)
	err := s.store.InTx(ctx, func(tx Store) error {
		if _, err := tx.SelectOrg(ctx, o.OrgID, true); err != nil {
			if errors.Is(err, orgslib.ErrOrgNotFound) {
				return nil // deleted meanwhile
			}
			return err
		}
		members, err := tx.SelectMembers(ctx, o.OrgID)
		if err != nil {
			return err
		}
		if o.Personal || len(members) <= 1 {
			deleted = true
			return tx.MarkOrgDeleted(ctx, o.OrgID, now, now.Add(s.retention.Get(ctx)))
		}
		if o.Role == orgslib.RoleOwner {
			owners, err := tx.CountOwners(ctx, o.OrgID)
			if err != nil {
				return err
			}
			if owners <= 1 {
				next, err := tx.SelectEarliestMember(ctx, o.OrgID, orgslib.RoleAdmin, userID)
				if errors.Is(err, orgsdomain.ErrMemberNotFound) {
					next, err = tx.SelectEarliestMember(ctx, o.OrgID, "", userID)
				}
				if err != nil {
					return err
				}
				if err := tx.UpdateMemberRole(ctx, o.OrgID, next.UserID, orgslib.RoleOwner); err != nil {
					return err
				}
				promoted = next.UserID
			}
		}
		return tx.DeleteMember(ctx, o.OrgID, userID)
	})
	if err != nil {
		return err
	}
	reason := map[string]any{"reason": "account_deleted"}
	switch {
	case deleted:
		s.audit(ctx, ActionOrgDeleted, o.OrgID, "org", string(o.OrgID), reason)
	default:
		if promoted != "" {
			s.audit(ctx, ActionMemberRoleChanged, o.OrgID, "user", promoted, map[string]any{"to": orgslib.RoleOwner, "reason": "owner_account_deleted"})
		}
		s.audit(ctx, ActionMemberRemoved, o.OrgID, "user", userID, reason)
	}
	return nil
}
