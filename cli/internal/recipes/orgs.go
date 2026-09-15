package recipes

import (
	_ "embed"
	"slices"
)

// OrgsMigrationPath is the organisations migration in the multi-tenant tree.
// orb add orgs copies its content under a new version (ADR-0050).
const OrgsMigrationPath = "db/migrations/20260916000001_orgs.sql"

//go:embed orgs/convert.sql
var orgsConversion []byte

// OrgsConversion returns the migration orb add orgs writes after the
// organisations migration: a personal workspace for every account, and each
// project moved to its owner's workspace. It changes projects in place, so
// columns developers added are kept.
func OrgsConversion() []byte { return slices.Clone(orgsConversion) }
