// Package migrations embeds the app's own goose migrations. gorbital.Migrate
// runs them in one history with the migrations of gorbital's modules,
// ordered by version (ADR-0083); River's job tables come after.
package migrations

import "embed"

// FS holds every migration file.
//
//go:embed *.sql
var FS embed.FS
