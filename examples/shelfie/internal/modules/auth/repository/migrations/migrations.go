// Package migrations embeds sign-in's goose migrations, numbered locally.
// authhttp's module places each in the app's history under the version v0.1
// apps carry it under (ADR-0083); the content never changes.
package migrations

import "embed"

// FS holds every migration file.
//
//go:embed *.sql
var FS embed.FS
