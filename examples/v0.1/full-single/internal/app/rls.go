package app

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"gorbital.dev/modules/postgres"
)

// rlsLeftOut are the organisation tables db/row_level_security.sql leaves
// without row-level security, for the reasons given there (ADR-0061).
var rlsLeftOut = []string{"org_invitations", "org_members"}

// warnRowLevelSecurity logs what keeps row-level security from protecting
// organisation data, when the database has it on (orb add rls). The app
// still starts: the other isolation layers hold.
func warnRowLevelSecurity(ctx context.Context, logger *slog.Logger, db postgres.DBTX) {
	warnings, err := rowLevelSecurityWarnings(ctx, db)
	if err != nil {
		logger.WarnContext(ctx, "couldn't check row-level security", slog.Any("error", err))
	}
	for _, w := range warnings {
		logger.WarnContext(ctx, w)
	}
}

// rowLevelSecurityWarnings returns one line per problem with row-level
// security, or none when it is off or sound.
func rowLevelSecurityWarnings(ctx context.Context, db postgres.DBTX) ([]string, error) {
	r, err := postgres.CheckRowLevelSecurity(ctx, db)
	if err != nil || !r.On() {
		return nil, err
	}
	var warnings []string
	if r.Bypasses {
		warnings = append(warnings, fmt.Sprintf("row-level security is on, but the database role %s is a superuser or has BYPASSRLS, so no policy applies to it; connect as a role without either (docs/guides/row-level-security.md)", r.Role))
	}
	if len(r.NotForced) > 0 {
		warnings = append(warnings, "row-level security isn't forced on "+strings.Join(r.NotForced, ", ")+", so its policies don't limit the tables' owner; run ALTER TABLE … FORCE ROW LEVEL SECURITY")
	}
	var unprotected []string
	for _, table := range r.Unprotected {
		if !slices.Contains(rlsLeftOut, table) {
			unprotected = append(unprotected, table)
		}
	}
	if len(unprotected) > 0 {
		warnings = append(warnings, "organisation tables without row-level security: "+strings.Join(unprotected, ", ")+"; add the org_isolation policy in a migration (docs/guides/row-level-security.md)")
	}
	return warnings, nil
}
