package flags

// An Option configures a flag declaration.
type Option interface{ apply(*definition) }

type optionFunc func(*definition)

func (f optionFunc) apply(d *definition) { f(d) }

// Describe sets the help text shown to operators.
func Describe(text string) Option {
	return optionFunc(func(d *definition) { d.description = text })
}

// Group sets the group used to organise flags in listings. Default: the
// key's first segment.
func Group(name string) Option {
	return optionFunc(func(d *definition) { d.group = name })
}

// Client marks a flag clients may read: [Store.ClientFlags] evaluates only
// these, for endpoints such as GET /v1/flags. Other flags stay server-side,
// so their keys don't reveal unreleased features.
func Client() Option {
	return optionFunc(func(d *definition) { d.client = true })
}

// DefaultOn declares the flag enabled with a default of true: on for
// everyone until an operator changes it. Use it for kill switches of
// features already released. Without it, a flag is disabled until an
// operator turns it on.
func DefaultOn() Option {
	return optionFunc(func(d *definition) { d.defaultOn = true })
}
