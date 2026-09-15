module apistock.dev/modules/orgs

go 1.26.0

require (
	apistock.dev v0.0.0
	apistock.dev/modules/auth v0.0.0
)

require (
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	rsc.io/qr v0.2.0 // indirect
)

// Local development: consumers' own replace directives are unaffected.
replace (
	apistock.dev => ../..
	apistock.dev/modules/auth => ../auth
	apistock.dev/modules/postgres => ../postgres
)
