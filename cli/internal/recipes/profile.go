package recipes

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// The sign-in profiles of orb new --auth (ADR-0089, ADR-0090): how much of
// gorbital's sign-in the app serves.
const (
	// AuthNone: no sign-in at all. The app has no accounts, no sessions and
	// no auth tables; its routes are public or guarded by middleware it
	// writes itself.
	AuthNone = "none"
	// AuthBasic: an email address, a password and the operators' account
	// APIs — 29 operations.
	AuthBasic = "basic"
	// AuthFull: every sign-in method, which is v0.2's sign-in byte for
	// byte — 74 operations.
	AuthFull = "full"
)

// Auths are the values --auth accepts, in the order the help lists them.
var Auths = []string{AuthNone, AuthBasic, AuthFull}

// AuthUsage is how --auth's values are written in help text and errors.
const AuthUsage = "none|basic|full"

// The app scopes of orb new --scope (ADR-0088, ADR-0090): what a tenant is.
// A fourth value, a name such as organisation or merchant, mounts the
// supplied organisations under that vocabulary.
const (
	// ScopeNone: the app has no tenants. Records belong to nobody but the
	// app.
	ScopeNone = "none"
	// ScopeSingle: one implicit scope. Records belong to users.
	ScopeSingle = "single"
	// ScopeCustom, in an app scope, is the membership tables and a
	// gorbital.Scope the app fills in itself, with no orgshttp at all. It
	// is the same word as the resource scope of the same name, and the
	// same idea: the rule is the app's to write.
)

// ScopeNameUsage is how --scope's values are written in help text and
// errors.
const ScopeNameUsage = "none|single|custom|<name>"

// DefaultScopeName is the scope a named scope has unless --scope names
// another, and what --tenancy multi means: the supplied organisations.
const DefaultScopeName = "organisation"

// A Method is one way of signing in, named as
// gorbital.dev/gorbital/authhttp names it (authhttp.Method). The profile
// records the set; cmd/api/main.go passes it to authhttp.Methods.
type Method string

// The sign-in methods a profile can serve, in the order authhttp lists
// them.
const (
	MethodPassword  Method = "password"
	MethodOperators Method = "operators"
	MethodTOTP      Method = "totp"
	MethodPasskeys  Method = "passkeys"
	MethodSocial    Method = "social"
	MethodAPIKeys   Method = "api_keys"
)

// Ident is the method's authhttp constant without its prefix, such as
// APIKeys for api_keys, so a template can write authhttp.MethodAPIKeys.
func (m Method) Ident() string {
	switch m {
	case MethodAPIKeys:
		return "APIKeys"
	case MethodTOTP:
		return "TOTP"
	}
	return strings.ToUpper(string(m)[:1]) + string(m)[1:]
}

// allMethods are the methods AuthFull serves, which is every one there is.
var allMethods = []Method{MethodPassword, MethodOperators, MethodTOTP, MethodPasskeys, MethodSocial, MethodAPIKeys}

// basicMethods are the methods AuthBasic serves: an email address, a
// password, and the operators' account APIs to run the place.
var basicMethods = []Method{MethodPassword, MethodOperators}

// Operations counts the HTTP operations a sign-in profile registers, for
// the prompt and the help: what the choice costs.
const (
	basicOperations = 29
	fullOperations  = 74
	// authMigrations are applied whichever methods are served, so adding a
	// method later is one line in main.go (ADR-0089).
	authMigrations = 8
)

// A Profile is the application orb new creates: how much sign-in, and what
// a tenant is (ADR-0090).
type Profile struct {
	// Auth is the value of --auth: AuthNone, AuthBasic or AuthFull.
	Auth string
	// Scope is the value of --scope: ScopeNone, ScopeSingle, ScopeCustom,
	// or the name of a scope.
	Scope string
	// ScopeName is what the app calls its tenant when Scope is a name:
	// "organisation", "merchant", …. It is empty for the other scopes.
	ScopeName string
	// Methods are the sign-in methods the app serves; empty means the
	// Auth profile's own set (AuthMethods).
	Methods []Method
}

// scopeNameWord is the shape a scope's name must have: what
// orgshttp.ScopeName accepts, so the name orb records is a name the
// library can mount.
var scopeNameWord = regexp.MustCompile(`^[a-z]+$`)

// LegalProfiles returns the nine combinations orb new can create, in the
// order the help and the CI matrix list them: --auth none only with
// --scope none, and each of basic and full with every scope. The named
// scope is represented by DefaultScopeName; any other name behaves the
// same way.
func LegalProfiles() []Profile {
	profiles := []Profile{{Auth: AuthNone, Scope: ScopeNone}}
	for _, auth := range []string{AuthBasic, AuthFull} {
		for _, scope := range []string{ScopeNone, ScopeSingle, ScopeCustom, DefaultScopeName} {
			p := Profile{Auth: auth, Scope: scope}
			if scope == DefaultScopeName {
				p.ScopeName = DefaultScopeName
			}
			profiles = append(profiles, p)
		}
	}
	return profiles
}

// ParseProfile returns the profile orb new --auth auth --scope scope
// creates, or an error saying why that combination isn't one. An empty
// auth or scope takes the default: full sign-in, and one implicit scope.
func ParseProfile(auth, scope string) (Profile, error) {
	if auth == "" {
		auth = AuthFull
	}
	if scope == "" {
		scope = ScopeSingle
	}
	if !slices.Contains(Auths, auth) {
		return Profile{}, fmt.Errorf("unknown --auth %q (want %s)", auth, AuthUsage)
	}
	p := Profile{Auth: auth, Scope: scope}
	switch scope {
	case ScopeNone, ScopeSingle, ScopeCustom:
	default:
		if !scopeNameWord.MatchString(scope) {
			return Profile{}, fmt.Errorf("unknown --scope %q (want %s, where a name is lowercase letters such as %s or merchant)", scope, ScopeNameUsage, DefaultScopeName)
		}
		p.ScopeName = scope
	}
	if err := p.check(); err != nil {
		return Profile{}, err
	}
	return p, nil
}

// check refuses the illegal combinations, saying why rather than merely
// that (ADR-0090 §2).
func (p Profile) check() error {
	if p.Auth != AuthNone || p.Scope == ScopeNone {
		return nil
	}
	return fmt.Errorf("--auth none --scope %s is not a shape orb can create.\n"+
		"A scope decides which user may act in it, and --auth none means the app has\n"+
		"no users. Use --scope none, or --auth basic.", p.Scope)
}

// Legal reports whether the profile is one of the nine.
func (p Profile) Legal() bool { return p.check() == nil }

// Named reports a profile whose scope is a name, so the app mounts the
// supplied organisations under that vocabulary.
func (p Profile) Named() bool { return p.ScopeName != "" }

// CustomScope reports a profile whose membership rules are the app's own:
// the tables and a gorbital.Scope stub, and no orgshttp.
func (p Profile) CustomScope() bool { return p.Scope == ScopeCustom }

// Scoped reports a profile with any tenancy at all, named or custom: one
// whose records belong to a tenant rather than to a user or to nobody.
func (p Profile) Scoped() bool { return p.Named() || p.CustomScope() }

// FullAuth reports a profile serving every sign-in method gorbital has.
func (p Profile) FullAuth() bool { return p.Auth == AuthFull }

// SignsIn reports a profile with gorbital's sign-in, basic or full.
func (p Profile) SignsIn() bool { return p.Auth == AuthBasic || p.Auth == AuthFull }

// AuthMethods are the sign-in methods the app serves: the profile's own
// Methods, or the Auth profile's set.
func (p Profile) AuthMethods() []Method {
	switch {
	case len(p.Methods) > 0:
		return slices.Clone(p.Methods)
	case p.Auth == AuthBasic:
		return slices.Clone(basicMethods)
	case p.Auth == AuthFull:
		return slices.Clone(allMethods)
	}
	return nil
}

// AllMethods reports a profile serving every sign-in method, which needs
// no authhttp.Methods call at all.
func (p Profile) AllMethods() bool {
	return slices.Equal(p.AuthMethods(), allMethods)
}

// Operations counts the HTTP operations the profile's sign-in registers,
// for the prompt and the help.
func (p Profile) Operations() int {
	switch {
	case !p.SignsIn():
		return 0
	case p.AllMethods():
		return fullOperations
	case slices.Equal(p.AuthMethods(), basicMethods):
		return basicOperations
	}
	return 0
}

// Migrations counts the sign-in migrations the app applies: all of them
// whichever methods it serves, so adding a method later is one line
// (ADR-0089).
func (p Profile) Migrations() int {
	if !p.SignsIn() {
		return 0
	}
	return authMigrations
}

// Layout returns the layout orb new writes for a profile: every profile is
// an app on gorbital.Main (ADR-0083). Minimal keeps the v0.1 layout and is
// a Preset, not a Profile.
func (p Profile) Layout() string { return LayoutV02 }

// Tenancy is the profile as the tenancy of v0.1 and v0.2 records it:
// multi for a named scope, single for everything else. gorbital.yaml,
// gorbital.lock and orb upgrade keep reading it, so a v0.2.2 app stays
// readable by v0.2.1's orb.
func (p Profile) Tenancy() string {
	if p.Named() {
		return TenancyMulti
	}
	return TenancySingle
}

// Copies returns the built-in modules orb new copies into an app of the
// profile, as the app's own code, in the order it copies them
// (ADR-0090 §1): none for --auth none with no scope, sign-in for basic and
// full, and organisations as well for a named scope. Organisations come
// first: the library's orgshttp takes the library's sign-in, so sign-in can
// only be copied once organisations are. A custom scope copies neither —
// its membership rules are the app's from the first commit.
func (p Profile) Copies() []EjectedModule {
	var modules []EjectedModule
	if p.Named() {
		modules = append(modules, EjectedModule{Name: "orgs", Package: "gorbital.dev/gorbital/orgshttp"})
	}
	if p.SignsIn() {
		modules = append(modules, EjectedModule{Name: "auth", Package: "gorbital.dev/gorbital/authhttp"})
	}
	return modules
}

// String is the profile as its flags: --auth full --scope organisation.
func (p Profile) String() string {
	return "--auth " + p.Auth + " --scope " + p.Scope
}

// CustomScopeName is what an app with its own membership rules calls its
// tenant until it renames it: the neutral word, so the generated code and
// the stub's tables agree from the first commit.
const CustomScopeName = "tenant"

// Vocabulary is what the app calls its tenant: the scope's name with the
// words derived from it, "tenant" for a custom scope, and the
// organisations vocabulary for a profile that names none. An app that
// never named its tenant gets an undeclared vocabulary, so orb gen module
// writes it byte for byte what v0.2.1 wrote (ADR-0091 §3).
func (p Profile) Vocabulary() Vocabulary {
	name := p.ScopeName
	if p.CustomScope() {
		name = CustomScopeName
	}
	if name == "" || name == DefaultScopeName {
		return OrganisationVocabulary()
	}
	v := Vocabulary{Name: name}.withDefaults()
	v.Declared = true
	return v
}

// DeclaresScope reports a profile whose gorbital.yaml records a scope
// block: one whose tenant has words of its own. A --scope organisation app
// records none, so orb gen module writes it exactly what v0.2.1 wrote.
func (p Profile) DeclaresScope() bool { return p.Vocabulary().Declared }

// ScopeTable is the table the app's tenants live in: orgs for the supplied
// organisations, whatever their words, and the scope's plural for an app
// with its own membership rules.
func (p Profile) ScopeTable() string {
	if p.Named() {
		return "orgs"
	}
	return p.Vocabulary().Plural
}

// ScopeMembersTable is the table holding who belongs to a tenant and with
// which role.
func (p Profile) ScopeMembersTable() string {
	if p.Named() {
		return "org_members"
	}
	return p.Vocabulary().Name + "_members"
}

// The demonstration module orb new generates into every app it creates
// (ADR-0090 §5): the module orb gen module writes, at the profile's scope,
// instead of 25 template files that differ only by their access rule. Its
// migration version is fixed, so every app of a release has the same one.
const (
	DemoModule = "Project"
	// DemoMigrationVersion is its migration's version: the first one after
	// the built-in modules', which run in the same history. A test in the
	// CLI keeps it in step with them.
	DemoMigrationVersion = "20260918000071"
	// DemoFields are the fields of the demonstration module, as orb gen
	// module takes them.
	demoFields = "name:string:unique description:text status:enum(active,archived)"
)

// DemoMigration is the demonstration module's migration file name.
func (p Profile) DemoMigration() string { return DemoMigrationVersion + "_projects.sql" }

// DemoFields returns the demonstration module's field specifications.
func DemoFields() []string { return strings.Fields(demoFields) }

// ResourceScope is the access rule orb gen resource gives the
// demonstration module in an app of this profile: a tenant's where the app
// has a tenancy, a user's where it has sign-in and no tenancy, and
// world-readable where it has neither.
func (p Profile) ResourceScope() string {
	switch {
	case p.Scoped():
		return ScopeTenant
	case p.SignsIn():
		return ScopeUser
	}
	return ScopePublic
}

// ScopeTitle is the tenant's plural starting with a capital letter, for a
// heading.
func (p Profile) ScopeTitle() string {
	plural := p.Vocabulary().Plural
	return strings.ToUpper(plural[:1]) + plural[1:]
}

// RolesFile is where an app of this profile changes its scope's roles.
func (p Profile) RolesFile() string {
	if p.CustomScope() {
		return "internal/modules/scope/scope.go"
	}
	return "gorbital.yaml"
}

// The API artefacts an app produces rather than orb new writing them
// (ADR-0090 §5): the app's own openapi command writes the first three,
// the fourth is the /ops contract it is held to, and the fifth is
// recorded by its own TestPublicSurface.
const (
	OpenAPIPath         = "api/openapi.json"
	OpenAPIBaselinePath = "api/openapi.baseline.json"
	PostmanPath         = "api/postman_collection.json"
	LLMsPath            = "api/llms.txt"
)

// APIArtefacts are the files orb new produces by running the app, in the
// order it lists them.
func APIArtefacts() []string {
	return []string{OpenAPIPath, OpenAPIBaselinePath, PostmanPath, LLMsPath}
}

// ScopeMigration is the migration that creates a custom scope's tables.
func (p Profile) ScopeMigration() string { return "20260916000001_scope.sql" }

// SQLRoles is the scope's roles as SQL string literals, for a CHECK
// constraint: 'owner', 'admin', 'member'.
func (p Profile) SQLRoles() string {
	roles := p.Vocabulary().Roles
	quoted := make([]string, len(roles))
	for i, r := range roles {
		quoted[i] = "'" + r + "'"
	}
	return strings.Join(quoted, ", ")
}

// RoleList is the scope's roles, comma-separated, for gorbital.yaml.
func (p Profile) RoleList() string { return strings.Join(p.Vocabulary().Roles, ", ") }

// appFeatures are the features gorbital.yaml lists, in the order it lists
// them: the app's infrastructure, then what the profile adds.
func (p Profile) appFeatures() []string {
	f := []string{"postgres", "settings", "jobs", "audit", "mail"}
	if p.SignsIn() {
		f = append(f, "auth")
	}
	if p.Named() {
		f = append(f, "orgs")
	}
	if p.CustomScope() {
		f = append(f, "scope")
	}
	return append(f, "ops")
}

// AppFeatures is appFeatures as gorbital.yaml writes them.
func (p Profile) AppFeatures() string { return strings.Join(p.appFeatures(), ", ") }

// MethodNames are the profile's sign-in methods as their names, for
// gorbital.yaml and the templates.
func (p Profile) MethodNames() []string {
	methods := p.AuthMethods()
	names := make([]string, len(methods))
	for i, m := range methods {
		names[i] = string(m)
	}
	return names
}

// MethodList is the profile's sign-in methods as a gorbital.yaml flow
// sequence: [password, operators].
func (p Profile) MethodList() string {
	return "[" + strings.Join(p.MethodNames(), ", ") + "]"
}

// The gorbital.yaml keys that record the profile (ADR-0090 §8).
const (
	AuthKey = "auth"
)

// ProfileFromManifest returns the profile an app's gorbital.yaml records,
// reading it defensively as ReadVocabulary does: an unknown or missing
// value is no value rather than an error, because a later release of orb
// writes the file than the one reading it. The zero Profile means the
// manifest records none, which is every app up to v0.2.1.
func ProfileFromManifest(manifest []byte) Profile {
	var p Profile
	for line := range strings.Lines(string(manifest)) {
		line = strings.TrimRight(line, "\r\n")
		if line == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch {
		case key == AuthKey && slices.Contains(Auths, value):
			p.Auth = value
		case key == ScopeKey && value != "":
			p.Scope = value
			if value != ScopeNone && value != ScopeSingle && value != ScopeCustom && scopeNameWord.MatchString(value) {
				p.ScopeName = value
			}
		}
	}
	return p
}

// ProfileFromTenancy returns the profile orb new --preset full --tenancy
// tenancy creates: the deprecated alias of --auth full with --scope single
// or --scope organisation (ADR-0090 §1).
func ProfileFromTenancy(tenancy string) (Profile, error) {
	switch tenancy {
	case TenancySingle:
		return ParseProfile(AuthFull, ScopeSingle)
	case TenancyMulti:
		return ParseProfile(AuthFull, DefaultScopeName)
	}
	return Profile{}, fmt.Errorf("unknown tenancy %q (want %s or %s)", tenancy, TenancySingle, TenancyMulti)
}
