package usecase

import (
	"slices"

	orgslib "gorbital.dev/modules/orgs"
)

// canAssign reports whether a member with role actorRole may give someone
// role, or change or remove a member who has it: only owners assign owners,
// and nobody assigns a role that grants a permission their own role
// doesn't. Comparing permissions rather than role names keeps roles an app
// adds in declareOrgPermissions in check: an admin can't give anyone, or
// themselves, a custom role that may delete the organisation (security
// review ORG-2). A role the catalog doesn't declare grants nothing.
func (s *Service) canAssign(actorRole, role string) bool {
	if role == orgslib.RoleOwner {
		return actorRole == orgslib.RoleOwner
	}
	mine := s.catalog.Permissions(actorRole)
	for _, p := range s.catalog.Permissions(role) {
		if !slices.Contains(mine, p) {
			return false
		}
	}
	return true
}
