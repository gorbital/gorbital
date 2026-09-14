module apistock.dev/modules/auth

go 1.26.0

require (
	apistock.dev v0.0.0
	golang.org/x/crypto v0.55.0
)

require golang.org/x/sys v0.47.0 // indirect

// Local development: consumers' own replace directives are unaffected.
replace (
	apistock.dev => ../..
	apistock.dev/modules/postgres => ../postgres
)
