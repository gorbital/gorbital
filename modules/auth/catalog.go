package auth

import (
	"fmt"
	"regexp"
	"slices"
	"sync"
)

var catalogName = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)

// Role is a named set of permissions.
type Role struct {
	Name        string
	Description string
	Permissions []string
}

// Permission is a declared permission.
type Permission struct {
	Name        string
	Description string
}

// A Catalog declares the permissions modules check and the roles that grant
// them. Access is denied by default: a role grants only the permissions
// declared for it, and a role name stored for a user but missing from the
// catalog grants nothing. Declare everything at startup, then call
// [Catalog.Freeze]; invalid or late declarations are programming errors and
// panic.
type Catalog struct {
	mu          sync.RWMutex
	permissions []Permission
	roles       []Role
	frozen      bool
}

// NewCatalog returns an empty catalog.
func NewCatalog() *Catalog { return &Catalog{} }

// Permission declares a permission, such as "ops.settings.write".
func (c *Catalog) Permission(name, description string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checkOpen(name)
	if !catalogName.MatchString(name) {
		panic(fmt.Sprintf("auth: permission %q must be dotted lowercase, like module.resource.action", name))
	}
	if c.hasPermission(name) {
		panic(fmt.Sprintf("auth: permission %q declared twice", name))
	}
	c.permissions = append(c.permissions, Permission{Name: name, Description: description})
}

// Role declares a role granting permissions, each already declared.
func (c *Catalog) Role(name, description string, permissions ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checkOpen(name)
	if !catalogName.MatchString(name) {
		panic(fmt.Sprintf("auth: role %q must be lowercase, like platform_admin", name))
	}
	if slices.ContainsFunc(c.roles, func(r Role) bool { return r.Name == name }) {
		panic(fmt.Sprintf("auth: role %q declared twice", name))
	}
	for _, p := range permissions {
		if !c.hasPermission(p) {
			panic(fmt.Sprintf("auth: role %q grants undeclared permission %q", name, p))
		}
	}
	perms := slices.Clone(permissions)
	slices.Sort(perms)
	c.roles = append(c.roles, Role{Name: name, Description: description, Permissions: slices.Compact(perms)})
}

func (c *Catalog) checkOpen(name string) {
	if c.frozen {
		panic(fmt.Sprintf("auth: %q declared after the catalog was frozen; declare everything at startup", name))
	}
}

func (c *Catalog) hasPermission(name string) bool {
	return slices.ContainsFunc(c.permissions, func(p Permission) bool { return p.Name == name })
}

// Freeze stops further declarations. Services using the catalog call it.
func (c *Catalog) Freeze() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.frozen = true
}

// HasRole reports whether role is declared.
func (c *Catalog) HasRole(role string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return slices.ContainsFunc(c.roles, func(r Role) bool { return r.Name == role })
}

// Permissions returns the sorted permissions granted by roles. Unknown roles
// grant nothing.
func (c *Catalog) Permissions(roles ...string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var perms []string
	for _, r := range c.roles {
		if slices.Contains(roles, r.Name) {
			perms = append(perms, r.Permissions...)
		}
	}
	slices.Sort(perms)
	return slices.Compact(perms)
}

// Roles returns every declared role in declaration order.
func (c *Catalog) Roles() []Role {
	c.mu.RLock()
	defer c.mu.RUnlock()
	roles := make([]Role, len(c.roles))
	for i, r := range c.roles {
		r.Permissions = slices.Clone(r.Permissions)
		roles[i] = r
	}
	return roles
}

// AllPermissions returns every declared permission in declaration order.
func (c *Catalog) AllPermissions() []Permission {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return slices.Clone(c.permissions)
}
