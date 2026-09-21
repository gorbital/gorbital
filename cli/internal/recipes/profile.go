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

// Vocabulary is what the app calls its tenant: the scope's name with the
// words derived from it, or the organisations vocabulary for a profile
// that names no scope.
func (p Profile) Vocabulary() Vocabulary {
	if !p.Named() {
		return OrganisationVocabulary()
	}
	v := Vocabulary{Name: p.ScopeName}
	if p.ScopeName == DefaultScopeName {
		return OrganisationVocabulary()
	}
	v = v.withDefaults()
	v.Declared = true
	return v
}

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
