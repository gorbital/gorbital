package usecase

import (
	"context"
	"errors"
	"slices"
	"time"

	"gorbital.dev/actor"
	authlib "gorbital.dev/modules/auth"

	authdomain "example.com/shelfie/internal/modules/auth/domain"
)

// Permissions for the platform's service accounts (ADR-0058).
// authhttp's module declares them and gives them to the ops roles,
// which require two-factor authentication. They are public API.
const (
	PermServiceAccountsRead  = "ops.service_accounts.read"
	PermServiceAccountsWrite = "ops.service_accounts.write"
)

// OrgAccess connects organisation service accounts to organisations in a
// multi-tenant app, without the auth module importing the orgs module. The
// composition root sets it in Config; without it, only platform service
// accounts exist.
type OrgAccess interface {
	// AuthorizeServiceAccounts checks that the signed-in user is a member of
	// orgID whose role may manage its service accounts, and returns a context
	// acting in the organisation and the user's role there. Its errors are
	// returned as they are.
	AuthorizeServiceAccounts(ctx context.Context, orgID string) (context.Context, string, error)
	// CanAssign reports whether a member with role callerRole may give a
	// service account role, or manage one that has it. The owner role is
	// never allowed.
	CanAssign(callerRole, role string) bool
	// Catalog returns the organisation role catalog.
	Catalog() *authlib.Catalog
}

// ServiceAccountInput describes a new service account.
type ServiceAccountInput struct {
	Name        string
	Description string
	// Roles are platform roles for a platform service account, or exactly one
	// organisation role. Roles that require two-factor authentication, and
	// an organisation's owner role, aren't allowed.
	Roles []string
}

// ServiceAccountPatch changes a service account; nil fields stay.
type ServiceAccountPatch struct {
	Name        *string
	Description *string
	Roles       *[]string
	// Disabled true disables the service account and revokes every key;
	// false enables it again, without its old keys.
	Disabled *bool
}

// ListServiceAccounts returns the service accounts of orgID, or of the
// platform for an empty orgID, oldest first.
func (s *Service) ListServiceAccounts(ctx context.Context, orgID string) ([]authdomain.ServiceAccount, error) {
	ctx, _, err := s.serviceAccountAccess(ctx, orgID, false)
	if err != nil {
		return nil, err
	}
	accounts, err := s.store.SelectServiceAccounts(ctx, orgID)
	if err != nil {
		return nil, dbError("list service accounts", err)
	}
	return accounts, nil
}

// GetServiceAccount returns one of orgID's service accounts, or
// ErrServiceAccountNotFound.
func (s *Service) GetServiceAccount(ctx context.Context, orgID, id string) (authdomain.ServiceAccount, error) {
	ctx, _, err := s.serviceAccountAccess(ctx, orgID, false)
	if err != nil {
		return authdomain.ServiceAccount{}, err
	}
	a, err := s.store.SelectServiceAccount(ctx, orgID, id, false)
	if err != nil && !errors.Is(err, authdomain.ErrServiceAccountNotFound) {
		return authdomain.ServiceAccount{}, dbError("get service account", err)
	}
	return a, err
}

// CreateServiceAccount creates a service account of orgID, or of the
// platform for an empty orgID. It returns ErrInvalidServiceAccount,
// ErrInvalidServiceAccountRole, ErrServiceAccountLimitReached, and for the
// platform ErrForbidden or ErrStepUpRequired without ops.service_accounts.write.
func (s *Service) CreateServiceAccount(ctx context.Context, orgID string, in ServiceAccountInput) (authdomain.ServiceAccount, error) {
	ctx, callerRole, err := s.serviceAccountAccess(ctx, orgID, true)
	if err != nil {
		return authdomain.ServiceAccount{}, err
	}
	name, description, err := authdomain.ServiceAccountFields(in.Name, in.Description)
	if err != nil {
		return authdomain.ServiceAccount{}, err
	}
	roles, err := s.serviceAccountRoles(orgID, callerRole, in.Roles)
	if err != nil {
		return authdomain.ServiceAccount{}, err
	}
	now := s.now().UTC().Truncate(time.Microsecond)
	a := authdomain.ServiceAccount{
		ID: authlib.NewID("svc"), OrgID: orgID, Name: name, Description: description, Roles: roles,
		CreatedBy: by(ctx), CreatedAt: now, UpdatedAt: now,
	}
	var state error
	err = s.store.InTx(ctx, func(tx Store) error {
		n, err := tx.CountServiceAccounts(ctx, orgID)
		if err != nil || n >= authdomain.MaxServiceAccounts {
			if err == nil {
				state = authdomain.ErrServiceAccountLimitReached
			}
			return err
		}
		return tx.InsertServiceAccount(ctx, a)
	})
	switch {
	case err != nil:
		return authdomain.ServiceAccount{}, dbError("create service account", err)
	case state != nil:
		return authdomain.ServiceAccount{}, state
	}
	s.audit(ctx, serviceAccountEvent("auth.service_account.created", a, map[string]any{"roles": a.Roles}))
	return a, nil
}

// UpdateServiceAccount changes one of orgID's service accounts. Disabling it
// revokes every key at once. In an organisation, a member can only change a
// service account whose role they could give. It returns the errors of
// CreateServiceAccount and ErrServiceAccountNotFound.
func (s *Service) UpdateServiceAccount(ctx context.Context, orgID, id string, patch ServiceAccountPatch) (authdomain.ServiceAccount, error) {
	ctx, callerRole, err := s.serviceAccountAccess(ctx, orgID, true)
	if err != nil {
		return authdomain.ServiceAccount{}, err
	}
	var (
		a       authdomain.ServiceAccount
		changed []string
		revoked int64
		state   error
	)
	disabling := false
	err = s.store.InTx(ctx, func(tx Store) error {
		if a, err = tx.SelectServiceAccount(ctx, orgID, id, true); err != nil {
			return err
		}
		if state = s.canManage(callerRole, a); state != nil {
			return nil //nolint:nilerr // state is the refusal, returned after the transaction ends
		}
		name, description := a.Name, a.Description
		if patch.Name != nil && *patch.Name != name {
			name, changed = *patch.Name, append(changed, "name")
		}
		if patch.Description != nil && *patch.Description != description {
			description, changed = *patch.Description, append(changed, "description")
		}
		if a.Name, a.Description, state = authdomain.ServiceAccountFields(name, description); state != nil {
			return nil //nolint:nilerr // state is the refusal, returned after the transaction ends
		}
		if patch.Roles != nil {
			roles, err := s.serviceAccountRoles(orgID, callerRole, *patch.Roles)
			if err != nil {
				state = err
				return nil //nolint:nilerr // state is the refusal, returned after the transaction ends
			}
			if !slices.Equal(roles, a.Roles) {
				a.Roles, changed = roles, append(changed, "roles")
			}
		}
		now := s.now().UTC().Truncate(time.Microsecond)
		switch {
		case patch.Disabled == nil || *patch.Disabled == a.Disabled():
		case *patch.Disabled:
			a.DisabledAt, disabling = &now, true
			if revoked, err = tx.RevokeOwnerAPIKeys(ctx, "", a.ID, now, authdomain.RevokedAccountDisabled); err != nil {
				return err
			}
		default:
			a.DisabledAt, changed = nil, append(changed, "enabled")
		}
		if len(changed) == 0 && !disabling {
			return nil
		}
		a.UpdatedAt = now
		return tx.UpdateServiceAccount(ctx, a)
	})
	switch {
	case errors.Is(err, authdomain.ErrServiceAccountNotFound):
		return authdomain.ServiceAccount{}, err
	case err != nil:
		return authdomain.ServiceAccount{}, dbError("update service account", err)
	case state != nil:
		return authdomain.ServiceAccount{}, state
	}
	if len(changed) > 0 {
		s.audit(ctx, serviceAccountEvent("auth.service_account.updated", a, map[string]any{"changed": changed, "roles": a.Roles}))
	}
	if disabling {
		s.audit(ctx, serviceAccountEvent("auth.service_account.disabled", a, map[string]any{"revoked_keys": revoked}))
		s.keysRevoked(ctx, authdomain.OwnerServiceAccount, a.ID, orgID, revoked, authdomain.RevokedAccountDisabled)
	}
	return a, nil
}

// DeleteServiceAccount deletes one of orgID's service accounts with its
// keys. It returns ErrServiceAccountNotFound, or ErrInvalidServiceAccountRole
// in an organisation for a service account whose role the member couldn't
// give.
func (s *Service) DeleteServiceAccount(ctx context.Context, orgID, id string) error {
	ctx, callerRole, err := s.serviceAccountAccess(ctx, orgID, true)
	if err != nil {
		return err
	}
	var (
		a     authdomain.ServiceAccount
		state error
	)
	err = s.store.InTx(ctx, func(tx Store) error {
		if a, err = tx.SelectServiceAccount(ctx, orgID, id, true); err != nil {
			return err
		}
		if state = s.canManage(callerRole, a); state != nil {
			return nil //nolint:nilerr // state is the refusal, returned after the transaction ends
		}
		_, err := tx.DeleteServiceAccount(ctx, orgID, id)
		return err
	})
	switch {
	case errors.Is(err, authdomain.ErrServiceAccountNotFound):
		return err
	case err != nil:
		return dbError("delete service account", err)
	case state != nil:
		return state
	}
	s.audit(ctx, serviceAccountEvent("auth.service_account.deleted", a, nil))
	return nil
}

// ListServiceAccountKeys returns the keys of one of orgID's service
// accounts, newest first.
func (s *Service) ListServiceAccountKeys(ctx context.Context, orgID, id string) ([]authdomain.APIKey, error) {
	a, err := s.GetServiceAccount(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	keys, err := s.store.SelectAPIKeys(ctx, "", a.ID)
	if err != nil {
		return nil, dbError("list API keys", err)
	}
	return keys, nil
}

// CreateServiceAccountKey creates a key for one of orgID's enabled service
// accounts, limited to permissions its roles grant. Like CreateAPIKey, it
// checks the signed-in user as confirmUser does, since a key outlives the
// session. It returns the errors of CreateAPIKey, ErrServiceAccountNotFound
// and ErrServiceAccountDisabled.
func (s *Service) CreateServiceAccountKey(ctx context.Context, orgID, id, password string, in APIKeyInput) (CreatedAPIKey, error) {
	ctx, callerRole, err := s.serviceAccountAccess(ctx, orgID, true)
	if err != nil {
		return CreatedAPIKey{}, err
	}
	if in, err = s.apiKeyInput(ctx, in); err != nil {
		return CreatedAPIKey{}, err
	}
	p, err := s.reauthPrincipal(ctx)
	if err != nil {
		return CreatedAPIKey{}, err
	}
	var (
		out   CreatedAPIKey
		state error
	)
	err = s.store.InTx(ctx, func(tx Store) error {
		u, err := tx.SelectUserByID(ctx, p.UserID, true)
		if errors.Is(err, authdomain.ErrUserNotFound) {
			return authlib.ErrUnauthenticated
		}
		if err != nil {
			return err
		}
		if state, err = s.confirmUser(ctx, tx, p, u, password); err != nil || state != nil {
			return err
		}
		a, err := tx.SelectServiceAccount(ctx, orgID, id, true)
		if err != nil {
			return err
		}
		if state = s.canManage(callerRole, a); state != nil {
			return nil //nolint:nilerr // state is the refusal, returned after the transaction ends
		}
		if a.Disabled() {
			state = authdomain.ErrServiceAccountDisabled
			return nil
		}
		catalog := s.catalog
		if orgID != "" {
			catalog = s.orgs.Catalog()
		}
		granted, _ := catalog.PermissionsFor(a.Roles, false)
		if !scopesAllowed(in.Scopes, granted) {
			state = authdomain.ErrInvalidAPIKeyScopes
			return nil
		}
		out, state, err = s.insertAPIKey(ctx, tx, authdomain.APIKey{ServiceAccountID: a.ID}, in)
		return err
	})
	switch {
	case errors.Is(err, authdomain.ErrServiceAccountNotFound), errors.Is(err, authlib.ErrUnauthenticated):
		return CreatedAPIKey{}, err
	case err != nil:
		return CreatedAPIKey{}, dbError("create API key", err)
	case state != nil:
		return CreatedAPIKey{}, s.reauthFailed(ctx, p.UserID, state)
	}
	s.audit(ctx, apiKeyEvent("auth.api_key.created", out.APIKey, orgID))
	return out, nil
}

// RevokeServiceAccountKey revokes a key of one of orgID's service accounts.
// It returns ErrServiceAccountNotFound or ErrAPIKeyNotFound.
func (s *Service) RevokeServiceAccountKey(ctx context.Context, orgID, id, keyID string) error {
	ctx, callerRole, err := s.serviceAccountAccess(ctx, orgID, true)
	if err != nil {
		return err
	}
	a, err := s.store.SelectServiceAccount(ctx, orgID, id, false)
	if err != nil {
		if errors.Is(err, authdomain.ErrServiceAccountNotFound) {
			return err
		}
		return dbError("revoke API key", err)
	}
	if err := s.canManage(callerRole, a); err != nil {
		return err
	}
	return s.revokeAPIKey(ctx, authdomain.APIKey{ID: keyID, ServiceAccountID: a.ID}, orgID, authdomain.RevokedByAdmin)
}

// serviceAccountAccess checks that a signed-in session (never an API key)
// may read, or with write change, orgID's service accounts, and returns the
// context to continue with and, in an organisation, the user's role. The
// platform's need ops.service_accounts.read or .write.
func (s *Service) serviceAccountAccess(ctx context.Context, orgID string, write bool) (context.Context, string, error) {
	if _, err := requirePrincipal(ctx); err != nil {
		return ctx, "", err
	}
	if orgID == "" {
		permission := PermServiceAccountsRead
		if write {
			permission = PermServiceAccountsWrite
		}
		switch err := actor.Require(ctx, permission); {
		case err == nil:
			return ctx, "", nil
		case errors.Is(err, actor.ErrUnauthenticated):
			return ctx, "", authlib.ErrUnauthenticated
		case errors.Is(err, actor.ErrStepUpRequired):
			return ctx, "", authdomain.ErrStepUpRequired
		default:
			return ctx, "", authdomain.ErrForbidden
		}
	}
	if s.orgs == nil {
		return ctx, "", authdomain.ErrServiceAccountNotFound
	}
	orgCtx, role, err := s.orgs.AuthorizeServiceAccounts(ctx, orgID)
	if err != nil {
		return ctx, "", err
	}
	if a, _ := actor.From(orgCtx); a.OrgID != orgID || role == "" {
		// OrgAccess must return a context acting in the organisation.
		return ctx, "", authdomain.ErrServiceAccountNotFound
	}
	return orgCtx, role, nil
}

// serviceAccountRoles validates roles for a service account of orgID given
// by a member with callerRole: declared platform roles (at most
// MaxServiceAccountRoles) other than RoleUser, which covers users' own data,
// or exactly one organisation role the member may give; never a role that
// requires two-factor authentication.
func (s *Service) serviceAccountRoles(orgID, callerRole string, roles []string) ([]string, error) {
	roles = authdomain.Roles(roles)
	if orgID == "" {
		if len(roles) > authdomain.MaxServiceAccountRoles {
			return nil, authdomain.ErrInvalidServiceAccountRole
		}
		for _, r := range roles {
			if !s.catalog.HasRole(r) || s.catalog.RequiresMFA(r) || r == RoleUser {
				return nil, authdomain.ErrInvalidServiceAccountRole
			}
		}
		return roles, nil
	}
	c := s.orgs.Catalog()
	if len(roles) != 1 || !c.HasRole(roles[0]) || c.RequiresMFA(roles[0]) || !s.orgs.CanAssign(callerRole, roles[0]) {
		return nil, authdomain.ErrInvalidServiceAccountRole
	}
	return roles, nil
}

// canManage returns ErrInvalidServiceAccountRole when a member with
// callerRole couldn't give the organisation service account's role, so an
// admin can't take over a key of a service account above them. Platform
// service accounts are managed with ops.service_accounts.write alone.
func (s *Service) canManage(callerRole string, a authdomain.ServiceAccount) error {
	if a.OrgID == "" {
		return nil
	}
	for _, r := range a.Roles {
		if !s.orgs.CanAssign(callerRole, r) {
			return authdomain.ErrInvalidServiceAccountRole
		}
	}
	return nil
}

// serviceAccountEvent is an event about service account a.
func serviceAccountEvent(action string, a authdomain.ServiceAccount, metadata map[string]any) auditEvent {
	return auditEvent{Action: action, OrgID: a.OrgID, ResourceType: "service_account", ResourceID: a.ID, Metadata: metadata}
}
