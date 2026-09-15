package usecase

import orgslib "apistock.dev/modules/orgs"

// rank orders roles for what a member may assign. Roles an app adds rank
// with members.
func rank(role string) int {
	switch role {
	case orgslib.RoleOwner:
		return 3
	case orgslib.RoleAdmin:
		return 2
	default:
		return 1
	}
}

// canAssign reports whether a member with role actorRole may give someone
// role, or change or remove a member who has it: never a role above their
// own, and owners only by owners.
func canAssign(actorRole, role string) bool {
	if role == orgslib.RoleOwner {
		return actorRole == orgslib.RoleOwner
	}
	return rank(actorRole) >= rank(role)
}
