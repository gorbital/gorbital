module gorbital.dev/modules/auth

go 1.26.0

require (
	github.com/coreos/go-oidc/v3 v3.21.0
	github.com/fxamacker/cbor/v2 v2.9.3
	github.com/go-jose/go-jose/v4 v4.1.4
	github.com/go-webauthn/webauthn v0.18.1
	golang.org/x/crypto v0.57.0
	golang.org/x/oauth2 v0.37.0
	gorbital.dev v0.3.4
	rsc.io/qr v0.2.0
)

require (
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/go-webauthn/x v0.3.1 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/philhofer/fwd v1.2.0 // indirect
	github.com/tinylib/msgp v1.6.4 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	golang.org/x/sys v0.48.0 // indirect
)

// Local development: consumers' own replace directives are unaffected.
replace (
	gorbital.dev => ../..
	gorbital.dev/modules/postgres => ../postgres
)

// v0.3.0 requires gorbital.dev modules at v0.2.1, so gorbital.dev/gorbital
// does not build and the rest resolve a combination that was never tested.
// v0.3.1 is the same code with the requirements corrected.
retract v0.3.0
