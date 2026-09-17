module gorbital.dev/modules/orgs

go 1.26.0

require (
	gorbital.dev v0.2.0
	gorbital.dev/modules/auth v0.2.0
)

require (
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	rsc.io/qr v0.2.0 // indirect
)

// Local development: consumers' own replace directives are unaffected.
replace (
	gorbital.dev => ../..
	gorbital.dev/modules/auth => ../auth
	gorbital.dev/modules/postgres => ../postgres
)
