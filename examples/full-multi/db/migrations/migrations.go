// Package migrations embeds the app's goose migrations: one ordered history
// for the app's own tables and the gorbital modules' tables (ADR-0005).
// River's job tables are migrated separately by jobs.Migrate (ADR-0033).
package migrations

import "embed"

// FS holds every migration file.
//
//go:embed *.sql
var FS embed.FS
