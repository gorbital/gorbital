package domain

// SignInMethod is a sign-in method and whether this deployment has it
// configured (ADR-0045). It never holds configuration values.
type SignInMethod struct {
	Key     string
	Name    string
	Enabled bool
	// Detail describes an enabled method, such as its relying party ID.
	Detail string
	// Missing are the environment variables that turn a disabled method on.
	Missing []string
	// Guide is the AUTH_PROVIDERS.md section that explains the method.
	Guide string
}
