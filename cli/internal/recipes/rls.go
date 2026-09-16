package recipes

// RowLevelSecurityPath is the row-level security migration in the
// multi-tenant tree. orb add rls copies the app's copy of it into
// db/migrations under a new version; until then it isn't applied, so every
// multi-tenant app renders the same tree (ADR-0061).
const RowLevelSecurityPath = "db/row_level_security.sql"

// RowLevelSecurityKey is the gorbital.yaml key orb add rls sets to true.
const RowLevelSecurityKey = "rls"

// SetRowLevelSecurity records row-level security in tree's gorbital.yaml,
// as orb add rls writes it, so a rebuilt tree matches the app's lock.
func SetRowLevelSecurity(tree map[string][]byte) {
	if manifest, ok := tree[manifestPath]; ok {
		tree[manifestPath] = SetManifestKey(manifest, RowLevelSecurityKey, "true")
	}
}
