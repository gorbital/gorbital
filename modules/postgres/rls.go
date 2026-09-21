package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/actor"
)

// Row-level security (ADR-0061). Every connection a pool from [Open] hands
// out carries the organisation of the context that acquired it, in two
// session settings that policies read:
//
//	org_id = current_setting('gorbital.org_id', true)
//	    OR current_setting('gorbital.rls_bypass', true) = 'on'
const (
	// OrgSetting holds the organisation ID of the acquiring context, or ""
	// when it has none.
	OrgSetting = "gorbital.org_id"
	// ScopeSetting is [OrgSetting] under the name the framework uses for
	// tenancy since v0.2.2 (ADR-0088). There is one session setting whatever
	// an app calls its scope: it is invisible to people, row-level-security
	// policies in live databases name it, and renaming it would change what
	// those policies mean.
	ScopeSetting = OrgSetting
	// BypassSetting is "on" for a context from [WithoutRowLevelSecurity],
	// otherwise "".
	BypassSetting = "gorbital.rls_bypass"
)

type orgKey struct{}

type bypassKey struct{}

// bypass is the value [WithoutRowLevelSecurity] stores: its reason, and
// whether a connection acquired with it was logged yet.
type bypass struct {
	reason string
	logged atomic.Bool
}

// WithOrg returns a copy of ctx whose connections carry orgID in
// [OrgSetting], so row-level security policies limit them to that
// organisation's rows. Without it, a connection carries the organisation of
// the context's actor ([actor.Actor.OrgID], set by orgs.RequireMember after
// checking membership), or none. Use it where code acts in one organisation
// without an organisation actor, such as a job reading one organisation's
// rows.
//
// The organisation is set when a connection is acquired: a transaction
// keeps the one its context had at BeginTx.
func WithOrg(ctx context.Context, orgID string) context.Context {
	return context.WithValue(ctx, orgKey{}, orgID)
}

// WithScope returns a copy of ctx whose connections carry scopeID in
// [ScopeSetting], so row-level security policies limit them to that
// scope's rows. It is [WithOrg] under the name the framework uses for
// tenancy since v0.2.2, and is what gorbital.Scope.Session is usually set
// to.
func WithScope(ctx context.Context, scopeID string) context.Context {
	return WithOrg(ctx, scopeID)
}

// WithoutRowLevelSecurity returns a copy of ctx whose connections set
// [BypassSetting] to "on", so policies let them read and write every
// organisation's rows. It is for system paths that work across
// organisations, such as migrations and maintenance jobs; reason names the
// path ("migrate", "job:orgs_purge"). It panics when reason is empty.
//
// Decide it in code, never from request input. The first connection
// acquired with the returned context is logged at info level through the
// pool's logger ([WithLogger]), and every query span it runs carries
// gorbital.rls_bypass with the reason.
func WithoutRowLevelSecurity(ctx context.Context, reason string) context.Context {
	if strings.TrimSpace(reason) == "" {
		panic("postgres: WithoutRowLevelSecurity needs a reason naming the system path")
	}
	return context.WithValue(ctx, bypassKey{}, &bypass{reason: reason})
}

// connScope is what a connection's session settings hold.
type connScope struct {
	org    string
	bypass bool
}

// scopeKey stores a connection's connScope in its pgconn custom data.
const scopeKey = "gorbital.dev/modules/postgres.scope"

func scopeOf(ctx context.Context) (connScope, *bypass) {
	var s connScope
	b, _ := ctx.Value(bypassKey{}).(*bypass)
	s.bypass = b != nil
	if org, ok := ctx.Value(orgKey{}).(string); ok {
		s.org = org
	} else if a, ok := actor.From(ctx); ok {
		s.org = a.OrgID
	}
	return s, b
}

// setScopeSQL sets both settings for the session, not a transaction, so
// they last until the next acquire changes them.
const setScopeSQL = `SELECT set_config('` + OrgSetting + `', $1, false), set_config('` + BypassSetting + `', $2, false)`

// prepareConn is the pool's PrepareConn hook: it brings the connection's
// settings in line with ctx. Each connection remembers what it holds, so an
// acquire that doesn't change them sends nothing to the server. A new
// connection is always set, since a role or database default could give it
// either setting. The statement runs through pgconn: one round trip, no
// statement cache entry and no query span.
//
// When setting fails, the connection is destroyed and the query fails: its
// settings are unknown, and a connection that might still carry another
// organisation is never handed out.
func prepareConn(logger *slog.Logger) func(context.Context, *pgx.Conn) (bool, error) {
	return func(ctx context.Context, conn *pgx.Conn) (bool, error) {
		want, b := scopeOf(ctx)
		if b != nil && b.logged.CompareAndSwap(false, true) {
			logger.InfoContext(ctx, "row-level security bypassed", slog.String("reason", b.reason))
		}
		data := conn.PgConn().CustomData()
		if have, ok := data[scopeKey].(connScope); ok && have == want {
			return true, nil
		}
		on := ""
		if want.bypass {
			on = "on"
		}
		delete(data, scopeKey)
		if _, err := conn.PgConn().ExecParams(ctx, setScopeSQL, [][]byte{[]byte(want.org), []byte(on)}, nil, nil, nil).Close(); err != nil {
			return false, fmt.Errorf("postgres: set the organisation on the connection: %w", err)
		}
		data[scopeKey] = want
		return true, nil
	}
}

// bypassReason returns the reason of a context from WithoutRowLevelSecurity.
func bypassReason(ctx context.Context) (string, bool) {
	b, ok := ctx.Value(bypassKey{}).(*bypass)
	if !ok {
		return "", false
	}
	return b.reason, true
}
