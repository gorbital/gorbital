module gorbital.dev/modules/mail/smtp

go 1.26.0

require gorbital.dev v0.3.2

// Local development: consumers' own replace directives are unaffected.
replace gorbital.dev => ../../..

// v0.3.0 requires gorbital.dev modules at v0.2.1, so gorbital.dev/gorbital
// does not build and the rest resolve a combination that was never tested.
// v0.3.1 is the same code with the requirements corrected.
retract v0.3.0
