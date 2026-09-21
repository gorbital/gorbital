module gorbital.dev/modules/jwt

go 1.26.0

require (
	github.com/go-jose/go-jose/v4 v4.1.4
	gorbital.dev v0.3.0
)

// Local development: consumers' own replace directives are unaffected.
replace gorbital.dev => ../..
