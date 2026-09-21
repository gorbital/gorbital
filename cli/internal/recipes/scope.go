package recipes

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// The resource access scopes of orb gen resource --scope and orb gen module
// --scope (ADR-0091): who may read and write the records a module serves.
// The generator states the rule instead of assuming one.
const (
	// ScopeUser: the records belong to the signed-in user, who is the only
	// one who can read or change them.
	ScopeUser = "user"
	// ScopeTenant: the records belong to a tenant — an organisation, a
	// merchant, a clinic, whatever the app calls it (ADR-0088) — and its
	// members reach them through their role.
	ScopeTenant = "tenant"
	// ScopePublic: the records are world-readable. Reads need no sign-in;
	// writes are permission-guarded.
	ScopePublic = "public"
	// ScopeCustom: the module decides. The generator writes policy.go with
	// unimplemented stubs and adds no filter of its own.
	ScopeCustom = "custom"

	// ScopeOrg is the v0.2 name of [ScopeTenant].
	//
	// Deprecated: use ScopeTenant. orb accepts org silently for all of
	// v0.x, so every script and documented command keeps working.
	ScopeOrg = "org"
)

// Scopes are the scopes --scope accepts, in the order the help lists them.
// ScopeOrg is accepted too and means ScopeTenant.
var Scopes = []string{ScopeUser, ScopeTenant, ScopePublic, ScopeCustom}

// ScopeUsage is how --scope's values are written in help text and errors.
const ScopeUsage = "user|tenant|public|custom"

// ParseScope returns the canonical name of the scope s names: ScopeTenant
// for both tenant and its deprecated alias org, and s itself for the
// others. An empty s stays empty, meaning the app's default. Every place
// that reads --scope calls this, so the four values can't drift apart.
func ParseScope(s string) (string, error) {
	switch s {
	case "", ScopeUser, ScopeTenant, ScopePublic, ScopeCustom:
		return s, nil
	case ScopeOrg:
		return ScopeTenant, nil // the v0.2 name, accepted silently
	}
	return "", fmt.Errorf("unknown --scope %q (want %s; org is the old name of tenant)", s, ScopeUsage)
}

// A Vocabulary is what an app calls its tenant (ADR-0088): the words the
// code, routes, columns and refusals of a ScopeTenant module use. An app
// records it in gorbital.yaml; without one the generator falls back to
// [OrganisationVocabulary], the vocabulary of every v0.1 and v0.2 app.
type Vocabulary struct {
	// Name is the concept in lowercase singular, such as "organisation" or
	// "merchant".
	Name string
	// Plural is Name in plural, for prose such as "the merchant's orders".
	Plural string
	// Segment is the path segment the scope's routes live under, the
	// "orgs" of /v1/orgs/{orgId}.
	Segment string
	// PathParam is the path parameter carrying the scope ID, such as
	// "orgId".
	PathParam string
	// ParamField is PathParam as a Go field name, such as "OrgID".
	ParamField string
	// Column is the table column holding the scope ID, such as "org_id".
	Column string
	// Field is Column as a Go field name, such as "OrgID".
	Field string
	// Var is Column as a Go parameter name, such as "orgID".
	Var string
	// NotFoundCode refuses a request for a scope the actor is not a member
	// of, such as "org_not_found".
	NotFoundCode string
	// IDPrefix starts a scope ID in examples, such as "org".
	IDPrefix string
	// Roles are the scope's roles, most privileged first.
	Roles []string
	// Declared reports a vocabulary the app wrote in gorbital.yaml. Without
	// one the generator writes the v0.2 organisation code — guard.OrgMember
	// and Permission.OrgRoles — so an app that never named its tenant gets
	// byte for byte what v0.2.1 generated (ADR-0091 §3).
	Declared bool
}

// OrganisationVocabulary is the tenancy of every v0.1 and v0.2 app, and the
// fallback for an app whose gorbital.yaml names no scope.
func OrganisationVocabulary() Vocabulary {
	return Vocabulary{
		Name: "organisation", Plural: "organisations", Segment: "orgs",
		PathParam: "orgId", ParamField: "OrgID",
		Column: "org_id", Field: "OrgID", Var: "orgID",
		NotFoundCode: "org_not_found", IDPrefix: "org",
		Roles: []string{"owner", "admin", "member"},
	}
}

// Guard is the guard a tenant module's routes use: guard.Scope under the
// app's own vocabulary, and guard.OrgMember while the app has named no
// tenant, which is what v0.2 generated.
func (v Vocabulary) Guard() string {
	if v.Declared {
		return "guard.Scope"
	}
	return "guard.OrgMember"
}

// RolesField is the gorbital.Permission field a tenant module's permissions
// declare their roles in: ScopeRoles, or its v0.2 name OrgRoles while the
// app has named no tenant.
func (v Vocabulary) RolesField() string {
	if v.Declared {
		return "ScopeRoles"
	}
	return "OrgRoles"
}

// RoleList is the roles in prose: owner, admin and member.
func (v Vocabulary) RoleList() string { return andList(v.Roles) }

// GoRoles is a Go []string literal of the roles' arguments, such as
// "owner", "admin", "member".
func (v Vocabulary) GoRoles() string {
	quoted := make([]string, len(v.Roles))
	for i, r := range v.Roles {
		quoted[i] = `"` + r + `"`
	}
	return strings.Join(quoted, ", ")
}

// A is the indefinite article for the tenant's name: "an organisation",
// "a merchant".
func (v Vocabulary) A() string { return article(v.Name) }

// AColumn is the indefinite article for the tenant's column name: "an
// org_id", "a merchant_id".
func (v Vocabulary) AColumn() string { return article(v.Column) }

// Title is the tenant's name starting with a capital letter.
func (v Vocabulary) Title() string {
	return strings.ToUpper(v.Name[:1]) + v.Name[1:]
}

// CodeRoles is the roles as inline code, for prose: `owner`, `admin` and
// `member`.
func (v Vocabulary) CodeRoles() string {
	quoted := make([]string, len(v.Roles))
	for i, r := range v.Roles {
		quoted[i] = "`" + r + "`"
	}
	return andList(quoted)
}

// A ScopeRole is one of the scope's roles with its Go identifier, for the
// templates that declare them.
type ScopeRole struct {
	Name  string
	Ident string
}

// ScopeRoles are the scope's roles with the constant name each one gets,
// most privileged first.
func (v Vocabulary) ScopeRoles() []ScopeRole {
	roles := make([]ScopeRole, len(v.Roles))
	for i, r := range v.Roles {
		roles[i] = ScopeRole{Name: r, Ident: identFromWords(splitWords(r))}
	}
	return roles
}

// IsOrganisations reports the supplied organisations vocabulary.
func (v Vocabulary) IsOrganisations() bool { return v.Name == "organisation" }

// withDefaults fills in the words derivable from the scope's name.
func (v Vocabulary) withDefaults() Vocabulary {
	if v.Name == "" {
		return OrganisationVocabulary()
	}
	ident := identFromWords(splitWords(v.Name))
	if v.Plural == "" {
		v.Plural = pluralize(v.Name)
	}
	if v.Segment == "" {
		v.Segment = v.Plural
	}
	if v.PathParam == "" {
		v.PathParam = strings.ToLower(ident[:1]) + ident[1:] + "Id"
	}
	if v.ParamField == "" {
		v.ParamField = ident + "ID"
	}
	if v.Column == "" {
		v.Column = strings.Join(splitWords(v.Name), "_") + "_id"
	}
	if v.Field == "" {
		v.Field = ident + "ID"
	}
	if v.Var == "" {
		v.Var = strings.ToLower(ident[:1]) + ident[1:] + "ID"
	}
	if v.NotFoundCode == "" {
		v.NotFoundCode = strings.Join(splitWords(v.Name), "_") + "_not_found"
	}
	if v.IDPrefix == "" {
		v.IDPrefix = deriveIDPrefix(strings.Join(splitWords(v.Name), "_"))
	}
	if len(v.Roles) == 0 {
		v.Roles = OrganisationVocabulary().Roles
	}
	return v
}

// ScopeKey is the gorbital.yaml key holding the app's tenancy vocabulary.
const ScopeKey = "scope"

// The keys of the scope block, in both snake_case and camelCase.
var vocabularyKeys = map[string]func(*Vocabulary, string){
	"name":           func(v *Vocabulary, s string) { v.Name = s },
	"plural":         func(v *Vocabulary, s string) { v.Plural = s },
	"segment":        func(v *Vocabulary, s string) { v.Segment = s },
	"path_param":     func(v *Vocabulary, s string) { v.PathParam = s },
	"pathparam":      func(v *Vocabulary, s string) { v.PathParam = s },
	"column":         func(v *Vocabulary, s string) { v.Column = s },
	"not_found_code": func(v *Vocabulary, s string) { v.NotFoundCode = s },
	"notfoundcode":   func(v *Vocabulary, s string) { v.NotFoundCode = s },
	"id_prefix":      func(v *Vocabulary, s string) { v.IDPrefix = s },
	"idprefix":       func(v *Vocabulary, s string) { v.IDPrefix = s },
}

var vocabularyWord = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,40}$`)

// ReadVocabulary returns the tenancy vocabulary recorded in an app's
// gorbital.yaml, or the organisations one when it records none.
//
// It reads the manifest defensively, by hand: the scope block is written by
// a later release of orb than the one that may be reading it, so an
// unknown key, a missing key or a malformed value is ignored rather than
// refused. A manifest with no scope block — every app up to v0.2.1 — gets
// [OrganisationVocabulary], so the code generated for it is unchanged.
func ReadVocabulary(manifest []byte) Vocabulary {
	var v Vocabulary
	inScope := false
	for line := range strings.Lines(string(manifest)) {
		line = strings.TrimRight(line, "\r\n")
		if line == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			key, rest, ok := strings.Cut(line, ":")
			inScope = ok && key == ScopeKey && strings.TrimSpace(rest) == ""
			continue
		}
		if !inScope {
			continue
		}
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key == "roles" {
			v.Roles = parseRoles(value)
			continue
		}
		set, known := vocabularyKeys[strings.ToLower(key)]
		if known && vocabularyWord.MatchString(value) {
			set(&v, value)
		}
	}
	if v.Name == "" {
		return OrganisationVocabulary()
	}
	v.Declared = true
	return v.withDefaults()
}

// parseRoles reads a flow sequence such as [owner, manager, staff].
func parseRoles(value string) []string {
	value = strings.TrimSuffix(strings.TrimPrefix(value, "["), "]")
	var roles []string
	for _, r := range strings.Split(value, ",") {
		r = strings.Trim(strings.TrimSpace(r), `"'`)
		if vocabularyWord.MatchString(r) {
			roles = append(roles, r)
		}
	}
	return roles
}

// andList joins items as "a", "a and b" or "a, b and c".
func andList(items []string) string {
	if len(items) <= 1 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

// The generator's view of a resource's access rule (ADR-0091). The
// templates branch on these instead of on the scope's name.

// Public reports a world-readable resource: reads need no sign-in.
func (d ResourceData) Public() bool { return d.Scope == ScopePublic }

// Custom reports a resource whose access rule is the module's own, in
// policy.go.
func (d ResourceData) Custom() bool { return d.Scope == ScopeCustom }

// Filtered reports a resource the repository filters by an ownership
// column: the owner for ScopeUser, the tenant for ScopeTenant. ScopePublic
// and ScopeCustom have none, so their queries carry no filter the developer
// didn't write.
func (d ResourceData) Filtered() bool { return d.Scope == ScopeUser || d.Scope == ScopeTenant }

// HasCreatedBy reports a resource that records who created each record: a
// tenant's, where access comes from membership rather than authorship, and
// a custom one, whose policy usually needs somebody to compare the actor
// with. It is never an access rule by itself.
func (d ResourceData) HasCreatedBy() bool { return d.Scope == ScopeTenant || d.Scope == ScopeCustom }

// OwnerColumns are the columns identifying who a record belongs to, in the
// order the table declares them: none for ScopePublic.
func (d ResourceData) OwnerColumns() []string {
	switch d.Scope {
	case ScopeUser:
		return []string{"owner_id"}
	case ScopeTenant:
		return []string{d.Vocabulary.Column, "created_by"}
	case ScopeCustom:
		return []string{"created_by"}
	}
	return nil
}

// OwnerColumnList is OwnerColumns as SQL, ending with ", " when there are
// any: "org_id, created_by, ".
func (d ResourceData) OwnerColumnList() string {
	if cols := d.OwnerColumns(); len(cols) > 0 {
		return strings.Join(cols, ", ") + ", "
	}
	return ""
}

// ScopeName is what the app calls its tenant, such as organisation.
func (d ResourceData) ScopeName() string { return d.Vocabulary.Name }

// ScopeSegment is the path segment a tenant's routes live under: the
// "orgs" of /v1/orgs/{orgId}.
func (d ResourceData) ScopeSegment() string { return d.Vocabulary.Segment }

// ScopePathParam is the path parameter carrying the tenant ID, such as
// orgId.
func (d ResourceData) ScopePathParam() string { return d.Vocabulary.PathParam }

// ScopeParamField is ScopePathParam as a Go field, such as OrgID.
func (d ResourceData) ScopeParamField() string { return d.Vocabulary.ParamField }

// ScopeNotFoundCode refuses a request for a tenant the caller isn't a
// member of, such as org_not_found.
func (d ResourceData) ScopeNotFoundCode() string { return d.Vocabulary.NotFoundCode }

// ScopeIDExample is an example tenant ID for the OpenAPI document.
func (d ResourceData) ScopeIDExample() string {
	return d.Vocabulary.IDPrefix + "_mfrggzdfmztwq2lkmfrggzdfmy"
}

// ScopeTitle is the tenant's name starting with a capital letter.
func (d ResourceData) ScopeTitle() string {
	return strings.ToUpper(d.Vocabulary.Name[:1]) + d.Vocabulary.Name[1:]
}

// ScopeA is the indefinite article for the tenant's name.
func (d ResourceData) ScopeA() string { return article(d.Vocabulary.Name) }

// ScopeGuard is the guard a tenant module's routes use.
func (d ResourceData) ScopeGuard() string { return d.Vocabulary.Guard() }

// ScopeRolesField is the gorbital.Permission field holding a tenant
// module's roles: ScopeRoles, or its v0.2 name OrgRoles.
func (d ResourceData) ScopeRolesField() string { return d.Vocabulary.RolesField() }

// ScopeRoleList is the tenant's roles in prose: owner, admin and member.
func (d ResourceData) ScopeRoleList() string { return d.Vocabulary.RoleList() }

// ScopeGoRoles is the tenant's roles as Go string literals.
func (d ResourceData) ScopeGoRoles() string { return d.Vocabulary.GoRoles() }

// Owner names who a filtered resource's records belong to, without an
// article: "organisation", "merchant" or "owner". A public or custom
// resource has no owner, and the templates that use this don't ask.
func (d ResourceData) Owner() string {
	if d.Org {
		return d.Vocabulary.Name
	}
	return "owner"
}

// OwnerA is the indefinite article for Owner.
func (d ResourceData) OwnerA() string { return article(d.Owner()) }

// ModuleScopesKey is the gorbital.yaml block recording the scope each
// generated module was created with, so orb routes and orb doctor can read
// the access rule back (ADR-0091 §2).
const ModuleScopesKey = "modules"

// moduleScopesComment introduces the block when it is first written.
const moduleScopesComment = "# The access rule each generated module was created with (orb gen module\n" +
	"# --scope). orb routes and orb doctor read it back; it doesn't change what\n" +
	"# the app does, which is in the module's own code.\n"

// ReadModuleScopes returns the scope each module in the app's gorbital.yaml
// was generated with, keyed by the module's package. It reads the manifest
// defensively: a missing block, an unknown scope or a malformed line is no
// scope rather than an error.
func ReadModuleScopes(manifest []byte) map[string]string {
	scopes := map[string]string{}
	inBlock := false
	for line := range strings.Lines(string(manifest)) {
		line = strings.TrimRight(line, "\r\n")
		if line == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			key, rest, ok := strings.Cut(line, ":")
			inBlock = ok && key == ModuleScopesKey && strings.TrimSpace(rest) == ""
			continue
		}
		if !inBlock {
			continue
		}
		name, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if !ok || !vocabularyWord.MatchString(name) {
			continue
		}
		if scope, err := ParseScope(value); err == nil && scope != "" {
			scopes[name] = scope
		}
	}
	return scopes
}

// SetModuleScope returns the manifest with module recorded as generated
// with scope, adding the block when the app has none and keeping every
// other line.
func SetModuleScope(src []byte, module, scope string) []byte {
	entry := "  " + module + ": " + scope + "\n"
	lines := strings.SplitAfter(string(src), "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, ModuleScopesKey+":") || strings.TrimSpace(strings.TrimPrefix(line, ModuleScopesKey+":")) != "" {
			continue
		}
		// Insert in the block, keeping its entries sorted by module.
		end := i + 1
		for end < len(lines) && (strings.HasPrefix(lines[end], " ") || strings.HasPrefix(lines[end], "\t")) {
			name := strings.TrimSpace(strings.SplitN(lines[end], ":", 2)[0])
			if name == module {
				lines[end] = entry
				return []byte(strings.Join(lines, ""))
			}
			if name > module {
				break
			}
			end++
		}
		lines = slices.Insert(lines, end, entry)
		return []byte(strings.Join(lines, ""))
	}
	out := string(src)
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return []byte(out + moduleScopesComment + ModuleScopesKey + ":\n" + entry)
}

// indexOwner is the ownership part of an index name: "owner" for a user's
// records, the tenant column without its _id for a tenant's, and nothing
// when the table has no ownership column.
func (d ResourceData) indexOwner() string {
	switch d.Scope {
	case ScopeUser:
		return "owner"
	case ScopeTenant:
		return strings.TrimSuffix(d.Vocabulary.Column, "_id")
	}
	return ""
}

// UniqueIndex names the unique index enforcing a field's uniqueness:
// projects_org_name, or projects_name when the table has no ownership
// column.
func (d ResourceData) UniqueIndex(field string) string {
	if owner := d.indexOwner(); owner != "" {
		return d.Table + "_" + owner + "_" + field
	}
	return d.Table + "_" + field
}

// SortIndex names an index supporting one sort of the list query:
// projects_org_created, or projects_created.
func (d ResourceData) SortIndex(suffix string) string {
	if owner := d.indexOwner(); owner != "" {
		return d.Table + "_" + owner + "_" + suffix
	}
	return d.Table + "_" + suffix
}

// IndexColumns are the columns an index on the list query leads with: the
// ownership column, when there is one, and then cols.
func (d ResourceData) IndexColumns(cols string) string {
	if d.Filtered() {
		return d.ScopeColumnOrOwner() + ", " + cols
	}
	return cols
}

// ScopeColumnOrOwner is the ownership column: the tenant's, or owner_id.
func (d ResourceData) ScopeColumnOrOwner() string {
	if d.Org {
		return d.Vocabulary.Column
	}
	return "owner_id"
}
