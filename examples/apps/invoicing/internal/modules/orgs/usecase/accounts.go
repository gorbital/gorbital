package usecase

import (
	"context"
	"errors"

	orgslib "gorbital.dev/modules/orgs"

	orgsdomain "example.com/invoicing/internal/modules/orgs/domain"
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

// RemoveAccount runs after userID's account is deleted: the user stops being
// a member of every organisation. The personal workspace and organisations
// where the user was the only member are deleted, to be purged after the
// retention period. Where the user was the only owner (someone joined after
// CheckAccountDeletion), the earliest admin, or else the earliest member,
// becomes owner. Organisations that are already deleted get no hand-over, but
// lose the member too, so a restore can't bring a deleted account back as an
// owner (security review ORG-6). It is safe to run again.
func (s *Service) RemoveAccount(ctx context.Context, userID string) error {
	orgs, err := s.store.SelectUserOrgs(ctx, userID)
	if err != nil {
		return storeError("remove account", err)
	}
	var errs []error
	for _, o := range orgs {
		if err := s.removeFromOrg(ctx, userID, o.OrgID); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return storeError("remove account", errors.Join(errs...))
	}
	return nil
}

// removeFromOrg removes userID from one organisation, reading the
// organisation and the user's role under the organisation's lock.
func (s *Service) removeFromOrg(ctx context.Context, userID string, orgID orgslib.ID) error {
	now := s.clock()
	var (
		removed, deleted bool
		promoted, role   string
	)
	err := s.store.InTx(ctx, func(tx Store) error {
		o, err := tx.SelectOrg(ctx, orgID, true)
		if errors.Is(err, orgslib.ErrOrgNotFound) {
			o, err = tx.SelectDeletedOrg(ctx, orgID)
		}
		if errors.Is(err, orgslib.ErrOrgNotFound) {
			return nil // purged meanwhile
		}
		if err != nil {
			return err
		}
		m, err := tx.SelectMember(ctx, orgID, userID)
		if errors.Is(err, orgsdomain.ErrMemberNotFound) {
			return nil // removed already
		}
		if err != nil {
			return err
		}
		role = m.Role
		if o.DeletedAt != nil {
			removed = true
			return tx.DeleteMember(ctx, orgID, userID)
		}
		_, err = tx.SelectEarliestMember(ctx, orgID, "", userID)
		alone := errors.Is(err, orgsdomain.ErrMemberNotFound)
		if err != nil && !alone {
			return err
		}
		if o.Personal || alone {
			deleted = true
			if err := tx.MarkOrgDeleted(ctx, orgID, now, now.Add(s.retention.Get(ctx))); err != nil {
				return err
			}
			return tx.DeleteMember(ctx, orgID, userID)
		}
		if role == orgslib.RoleOwner {
			owners, err := tx.CountOwners(ctx, orgID, userID)
			if err != nil {
				return err
			}
			if owners == 0 {
				next, err := tx.SelectEarliestMember(ctx, orgID, orgslib.RoleAdmin, userID)
				if errors.Is(err, orgsdomain.ErrMemberNotFound) {
					next, err = tx.SelectEarliestMember(ctx, orgID, "", userID)
				}
				if err != nil {
					return err
				}
				if err := tx.UpdateMemberRole(ctx, orgID, next.UserID, orgslib.RoleOwner); err != nil {
					return err
				}
				promoted = next.UserID
			}
		}
		removed = true
		return tx.DeleteMember(ctx, orgID, userID)
	})
	if err != nil {
		return err
	}
	reason := map[string]any{"reason": "account_deleted"}
	switch {
	case deleted:
		s.audit(ctx, ActionOrgDeleted, orgID, "org", string(orgID), reason)
	case removed:
		if promoted != "" {
			s.audit(ctx, ActionMemberRoleChanged, orgID, "user", promoted, map[string]any{"to": orgslib.RoleOwner, "reason": "owner_account_deleted"})
		}
		s.audit(ctx, ActionMemberRemoved, orgID, "user", userID, map[string]any{"reason": "account_deleted", "role": role})
	}
	return nil
}
