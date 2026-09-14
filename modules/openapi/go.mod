module apistock.dev/modules/openapi

go 1.26.0

require (
	apistock.dev v0.0.0
	github.com/danielgtaylor/huma/v2 v2.39.1
)

// Local development: consumers' own replace directives are unaffected.
replace apistock.dev => ../..
