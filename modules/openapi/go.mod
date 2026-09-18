module gorbital.dev/modules/openapi

go 1.26.0

require (
	github.com/danielgtaylor/huma/v2 v2.39.1
	gorbital.dev v0.2.1
)

// Local development: consumers' own replace directives are unaffected.
replace gorbital.dev => ../..
