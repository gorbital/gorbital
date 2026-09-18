package usecase

import (
	"context"
	"errors"
	"time"

	orgslib "gorbital.dev/modules/orgs"

	orgsdomain "example.com/plateful/internal/modules/orgs/domain"
)

// Create adds an organisation with the signed-in user as its owner. The
// user needs PermOrgCreate, their email address must be verified, and they
// may own at most MaxOwnedOrgs live organisations.
func (s *Service) Create(ctx context.Context, name string) (orgsdomain.Membership, error) {
	uid, err := requireUser(ctx, PermOrgCreate)
	if err != nil {
		return orgsdomain.Membership{}, err
	}
	now := s.clock()
	o, err := orgsdomain.NewOrg(string(orgslib.NewID()), name, uid, false, now)
	if err != nil {
		return orgsdomain.Membership{}, err
	}
	if _, err := s.requireVerified(ctx, uid); err != nil {
		return orgsdomain.Membership{}, storeError("create", err)
	}
	err = s.store.InTx(ctx, func(tx Store) error {
		if err := s.checkOwnedOrgs(ctx, tx, uid); err != nil {
			return err
		}
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
// first; the user needs PermOrgList. It creates the personal workspace if an
// earlier failure left the account without one.
func (s *Service) List(ctx context.Context) ([]orgsdomain.Membership, error) {
	uid, err := requireUser(ctx, PermOrgList)
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
	var o orgsdomain.Org
	err = s.store.InTx(ctx, func(tx Store) error {
		if _, me, err = s.lockOrg(ctx, tx, orgID, PermOrgUpdate); err != nil {
			return err
		}
		o, err = tx.UpdateOrgName(ctx, orgID, name, version, s.clock())
		return err
	})
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
		o, _, err := s.lockOrg(ctx, tx, orgID, PermOrgDelete)
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
// whose role may delete it can restore it, with the same two-factor step-up
// as deleting; for anyone else it doesn't exist. Restoring counts toward
// MaxOwnedOrgs like creating.
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
		// The same permission path as RequireMember, step-up included
		// (security review ORG-5).
		if _, err := orgslib.Authorize(ctx, s.catalog, orgslib.Member{OrgID: orgID, UserID: uid, Role: m.Role}, PermOrgDelete); err != nil {
			return err
		}
		if m.Role == orgslib.RoleOwner {
			if err := s.checkOwnedOrgs(ctx, tx, uid); err != nil {
				return err
			}
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
// with every org-scoped row, and returns how many it removed. It works in
// batches until none are left or its time budget is spent, so a backlog of
// deleted organisations can't push legitimate purges past the retention
// period (security review ORG-3). The orgs_purge job runs it.
func (s *Service) Purge(ctx context.Context) (int, error) {
	now := s.clock()
	stop := time.Now().Add(purgeBudget)
	purged := 0
	for time.Now().Before(stop) {
		if err := ctx.Err(); err != nil {
			return purged, err
		}
		ids, err := s.store.SelectOrgsToPurge(ctx, now, purgeBatch)
		if err != nil {
			return purged, storeError("purge", err)
		}
		removed := 0
		for _, id := range ids {
			deleted, err := s.store.DeleteOrg(ctx, id, now)
			if err != nil {
				return purged, storeError("purge", err)
			}
			if deleted {
				removed++
				s.audit(ctx, ActionOrgPurged, id, "org", string(id), nil)
			}
		}
		purged += removed
		// A short batch was the last one. A full batch that removed nothing
		// changed under the purge; stop rather than list it again.
		if len(ids) < purgeBatch || removed == 0 {
			break
		}
	}
	return purged, nil
}

// checkOwnedOrgs returns ErrTooManyOrgs when userID already owns as many
// live organisations, personal workspaces aside, as MaxOwnedOrgs allows. It
// takes a lock per user until the transaction ends, so concurrent requests
// can't each pass the check.
func (s *Service) checkOwnedOrgs(ctx context.Context, tx Store, userID string) error {
	if err := tx.LockUserOrgs(ctx, userID); err != nil {
		return err
	}
	owned, err := tx.CountOwnedOrgs(ctx, userID)
	if err != nil {
		return err
	}
	if owned >= s.maxOwned.Get(ctx) {
		return orgsdomain.ErrTooManyOrgs
	}
	return nil
}
