package usecase

import (
	"context"
	"errors"
	"slices"

	"apistock.dev/actor"
	orgslib "apistock.dev/modules/orgs"

	orgsdomain "example.com/acme-api/internal/modules/orgs/domain"
)

// Create adds an organisation with the signed-in user as its owner.
func (s *Service) Create(ctx context.Context, name string) (orgsdomain.Membership, error) {
	uid, err := userID(ctx)
	if err != nil {
		return orgsdomain.Membership{}, err
	}
	now := s.clock()
	o, err := orgsdomain.NewOrg(string(orgslib.NewID()), name, uid, false, now)
	if err != nil {
		return orgsdomain.Membership{}, err
	}
	err = s.store.InTx(ctx, func(tx Store) error {
		if o, err = tx.InsertOrg(ctx, o); err != nil {
			return err
		}
		return tx.InsertMember(ctx, orgsdomain.Member{OrgID: o.ID, UserID: uid, Role: orgslib.RoleOwner, JoinedAt: now, AddedBy: by(ctx)})
	})
	if err != nil {
		return orgsdomain.Membership{}, storeError("create", err)
	}
	s.audit(ctx, ActionOrgCreated, orgslib.ID(o.ID), "org", o.ID, nil)
	return orgsdomain.Membership{Org: o, Role: orgslib.RoleOwner}, nil
}

// List returns the signed-in user's organisations, personal workspace
// first. It creates the personal workspace if an earlier failure left the
// account without one.
func (s *Service) List(ctx context.Context) ([]orgsdomain.Membership, error) {
	uid, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.EnsurePersonalWorkspace(ctx, uid); err != nil {
		return nil, err
	}
	list, err := s.store.SelectMemberships(ctx, uid)
	if err != nil {
		return nil, storeError("list", err)
	}
	return list, nil
}

// Get returns an organisation the signed-in user belongs to, with their
// role.
func (s *Service) Get(ctx context.Context, orgID orgslib.ID) (orgsdomain.Membership, error) {
	ctx, me, err := orgslib.RequireMember(ctx, s.store, s.catalog, orgID, PermOrgRead)
	if err != nil {
		return orgsdomain.Membership{}, storeError("get", err)
	}
	o, err := s.store.SelectOrg(ctx, orgID, false)
	if err != nil {
		return orgsdomain.Membership{}, storeError("get", err)
	}
	return orgsdomain.Membership{Org: o, Role: me.Role}, nil
}

// Rename changes an organisation's name. version is the version the caller
// read.
func (s *Service) Rename(ctx context.Context, orgID orgslib.ID, name string, version int64) (orgsdomain.Membership, error) {
	ctx, me, err := orgslib.RequireMember(ctx, s.store, s.catalog, orgID, PermOrgUpdate)
	if err != nil {
		return orgsdomain.Membership{}, storeError("rename", err)
	}
	if name, err = orgsdomain.NormalizeName(name); err != nil {
		return orgsdomain.Membership{}, err
	}
	o, err := s.store.UpdateOrgName(ctx, orgID, name, version, s.clock())
	if err != nil {
		return orgsdomain.Membership{}, storeError("rename", err)
	}
	s.audit(ctx, ActionOrgRenamed, orgID, "org", string(orgID), nil)
	return orgsdomain.Membership{Org: o, Role: me.Role}, nil
}

// Delete soft deletes an organisation. Its members lose access at once; an
// owner can restore it until the retention period ends, when the orgs_purge
// job removes it with its data. A personal workspace can't be deleted.
func (s *Service) Delete(ctx context.Context, orgID orgslib.ID) error {
	ctx, _, err := orgslib.RequireMember(ctx, s.store, s.catalog, orgID, PermOrgDelete)
	if err != nil {
		return storeError("delete", err)
	}
	now := s.clock()
	purgeAfter := now.Add(s.retention.Get(ctx))
	err = s.store.InTx(ctx, func(tx Store) error {
		o, err := tx.SelectOrg(ctx, orgID, true)
		if err != nil {
			return err
		}
		if o.Personal {
			return orgsdomain.ErrPersonalWorkspace
		}
		return tx.MarkOrgDeleted(ctx, orgID, now, purgeAfter)
	})
	if err != nil {
		return storeError("delete", err)
	}
	s.audit(ctx, ActionOrgDeleted, orgID, "org", string(orgID), map[string]any{"purge_after": purgeAfter})
	return nil
}

// Restore undoes Delete before the organisation is purged. Only a member
// whose role may delete it can restore it; for anyone else it doesn't exist.
func (s *Service) Restore(ctx context.Context, orgID orgslib.ID) (orgsdomain.Membership, error) {
	uid, err := userID(ctx)
	if err != nil {
		return orgsdomain.Membership{}, err
	}
	if _, err := orgslib.ParseID(string(orgID)); err != nil {
		return orgsdomain.Membership{}, orgslib.ErrOrgNotFound
	}
	var restored orgsdomain.Membership
	err = s.store.InTx(ctx, func(tx Store) error {
		o, err := tx.SelectDeletedOrg(ctx, orgID)
		if err != nil {
			return err
		}
		m, err := tx.SelectMember(ctx, orgID, uid)
		if errors.Is(err, orgsdomain.ErrMemberNotFound) {
			return orgslib.ErrOrgNotFound
		}
		if err != nil {
			return err
		}
		if !slices.Contains(s.catalog.Permissions(m.Role), PermOrgDelete) {
			return actor.ErrForbidden
		}
		if err := tx.RestoreOrg(ctx, orgID, s.clock()); err != nil {
			return err
		}
		o.DeletedAt, o.PurgeAfter = nil, nil
		restored = orgsdomain.Membership{Org: o, Role: m.Role}
		return nil
	})
	if err != nil {
		return orgsdomain.Membership{}, storeError("restore", err)
	}
	s.audit(ctx, ActionOrgRestored, orgID, "org", string(orgID), nil)
	return restored, nil
}

// Purge removes organisations deleted longer ago than the retention period,
// with every org-scoped row, and returns how many it removed. The orgs_purge
// job runs it.
func (s *Service) Purge(ctx context.Context) (int, error) {
	ids, err := s.store.SelectOrgsToPurge(ctx, s.clock(), purgeBatch)
	if err != nil {
		return 0, storeError("purge", err)
	}
	for i, id := range ids {
		if err := s.store.DeleteOrg(ctx, id); err != nil {
			return i, storeError("purge", err)
		}
		s.audit(ctx, ActionOrgPurged, id, "org", string(id), nil)
	}
	return len(ids), nil
}
