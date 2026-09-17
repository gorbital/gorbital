// Package migrations embeds the mobile backend's own goose migrations.
// gorbital.Migrate runs them in one history with the library's migrations,
// ordered by version (ADR-0083).
package migrations

import "embed"

// FS holds every migration file.
//
//go:embed *.sql
var FS embed.FS
