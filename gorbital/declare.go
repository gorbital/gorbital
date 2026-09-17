package gorbital

import (
	"fmt"
	"slices"

	"gorbital.dev/modules/flags"
	"gorbital.dev/modules/settings"
)

// A PermissionDeclarer declares permissions. *auth.Catalog implements it.
type PermissionDeclarer interface {
	Permission(name, description string)
}

// Declarations are the registries [Declare] adds modules' declarations to.
type Declarations struct {
	// Permissions receives every module's platform permissions; nil skips
	// them.
	Permissions PermissionDeclarer
	// OrgPermissions receives every module's organisation permissions, those
	// with [Permission.OrgRoles]; nil skips them.
	OrgPermissions PermissionDeclarer
	// Settings and Flags are required when a module declares settings or
	// flags.
	Settings *settings.Registry
	Flags    *flags.Registry
}

// Declare adds the modules' permissions, runtime settings and feature flags
// to d's registries, in module order. Call it once, before building the
// settings and flags stores and before freezing the permission catalog; then
// grant each role its permissions with [Grants].
//
// Permissions with OrgRoles go to d.OrgPermissions, the others to
// d.Permissions; grant organisation roles theirs with [OrgGrants].
//
// It returns an error naming the module for an invalid or duplicate module
// name, a permission declared by two modules, a permission with both Roles
// and OrgRoles, a missing registry, or an invalid declaration (which the
// registries report by panicking).
func Declare(d Declarations, modules ...Module) error {
	if err := validateModules(modules); err != nil {
		return err
	}
	owner := map[string]string{}
	for _, m := range modules {
		for _, p := range m.Permissions {
			if prev, ok := owner[p.Name]; ok {
				return fmt.Errorf("gorbital: permission %q is declared by modules %q and %q", p.Name, prev, m.Name)
			}
			if len(p.Roles) > 0 && len(p.OrgRoles) > 0 {
				return fmt.Errorf("gorbital: module %q: permission %q has both Roles and OrgRoles; a permission is held on the platform or in an organisation", m.Name, p.Name)
			}
			owner[p.Name] = m.Name
		}
	}
	for _, m := range modules {
		for _, p := range m.Permissions {
			target := d.Permissions
			if len(p.OrgRoles) > 0 {
				target = d.OrgPermissions
			}
			if target == nil {
				continue
			}
			if err := catchPanic(m.Name, "permission "+p.Name, func() { target.Permission(p.Name, p.Description) }); err != nil {
				return err
			}
		}
		if m.Settings != nil {
			if d.Settings == nil {
				return fmt.Errorf("gorbital: module %q declares settings, but Declarations.Settings is nil", m.Name)
			}
			if err := catchPanic(m.Name, "settings", func() { m.Settings(d.Settings) }); err != nil {
				return err
			}
		}
		if m.Flags != nil {
			if d.Flags == nil {
				return fmt.Errorf("gorbital: module %q declares flags, but Declarations.Flags is nil", m.Name)
			}
			if err := catchPanic(m.Name, "flags", func() { m.Flags(d.Flags) }); err != nil {
				return err
			}
		}
	}
	return nil
}

// Grants returns the permissions the modules give to the platform role
// role, sorted and without duplicates, for declaring the role in the
// permission catalog.
func Grants(role string, modules ...Module) []string {
	var perms []string
	for _, m := range modules {
		for _, p := range m.Permissions {
			if slices.Contains(p.Roles, role) {
				perms = append(perms, p.Name)
			}
		}
	}
	slices.Sort(perms)
	return slices.Compact(perms)
}

// OrgGrants returns the permissions the modules give to the organisation
// role role ([Permission.OrgRoles]), sorted and without duplicates, for
// declaring the role in the organisation catalog.
func OrgGrants(role string, modules ...Module) []string {
	var perms []string
	for _, m := range modules {
		for _, p := range m.Permissions {
			if slices.Contains(p.OrgRoles, role) {
				perms = append(perms, p.Name)
			}
		}
	}
	slices.Sort(perms)
	return slices.Compact(perms)
}
