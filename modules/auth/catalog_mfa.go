package auth

import (
	"fmt"
	"slices"
)

// RequireMFA marks declared roles as requiring two-factor authentication:
// their permissions are granted only to sessions verified with a second
// factor (ADR-0043). It is code, not a runtime setting, so operators can't
// weaken it.
func (c *Catalog) RequireMFA(roles ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range roles {
		c.checkOpen(r)
		if !slices.ContainsFunc(c.roles, func(role Role) bool { return role.Name == r }) {
			panic(fmt.Sprintf("auth: RequireMFA(%q): the role isn't declared", r))
		}
		if !slices.Contains(c.mfaRoles, r) {
			c.mfaRoles = append(c.mfaRoles, r)
		}
	}
}

// RequiresMFA reports whether any of roles requires two-factor
// authentication.
func (c *Catalog) RequiresMFA(roles ...string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return slices.ContainsFunc(roles, func(r string) bool { return slices.Contains(c.mfaRoles, r) })
}

// PermissionsFor returns the sorted permissions roles grant a session:
// granted holds those of roles that don't require two-factor authentication,
// plus those of roles that do when mfaVerified; stepUp holds the permissions
// only a verified session would add. Unknown roles grant nothing.
func (c *Catalog) PermissionsFor(roles []string, mfaVerified bool) (granted, stepUp []string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, r := range c.roles {
		if !slices.Contains(roles, r.Name) {
			continue
		}
		if mfaVerified || !slices.Contains(c.mfaRoles, r.Name) {
			granted = append(granted, r.Permissions...)
		} else {
			stepUp = append(stepUp, r.Permissions...)
		}
	}
	slices.Sort(granted)
	granted = slices.Compact(granted)
	stepUp = slices.DeleteFunc(stepUp, func(p string) bool { return slices.Contains(granted, p) })
	slices.Sort(stepUp)
	return granted, slices.Compact(stepUp)
}
