module gorbital.dev/modules/orgs

go 1.26.0

require (
	gorbital.dev v0.3.1
	gorbital.dev/modules/auth v0.3.1
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

// v0.3.0 requires gorbital.dev modules at v0.2.1, so gorbital.dev/gorbital
// does not build and the rest resolve a combination that was never tested.
// v0.3.1 is the same code with the requirements corrected.
retract v0.3.0
