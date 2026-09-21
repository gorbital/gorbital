package delivery

// A Method is one way of signing in, whose operations an app chooses to
// serve or not (ADR-0089). The values are authhttp's Method constants,
// which the package converts; delivery can't import it.
type Method string

// The sign-in methods, each the operations of one delivery file.
const (
	MethodPassword  Method = "password"  // auth.go and registration.go
	MethodOperators Method = "operators" // ops_users.go
	MethodTOTP      Method = "totp"      // mfa.go
	MethodPasskeys  Method = "passkeys"  // passkeys.go
	MethodSocial    Method = "social"    // social.go
	MethodAPIKeys   Method = "api_keys"  // apikeys.go
)

// A MethodSet is the sign-in methods an app serves (Config.Methods). The
// zero set is every method, so the zero Config stays v0.1's operations.
type MethodSet map[Method]bool

// Has reports whether the app serves m.
func (s MethodSet) Has(m Method) bool { return s == nil || s[m] }
