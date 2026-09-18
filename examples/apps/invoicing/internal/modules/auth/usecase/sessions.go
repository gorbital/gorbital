package usecase

import (
	"context"
	"errors"

	authlib "gorbital.dev/modules/auth"

	authdomain "example.com/invoicing/internal/modules/auth/domain"
)

// Authenticate returns the principal for a session token, and implements
// auth.Authenticator for the request middleware. It returns
// auth.ErrUnauthenticated for an unknown, ended or expired session, or a
// deleted account. Using a session extends its idle expiry, up to its
// absolute expiry. Permissions come from the user's current roles and
// RoleUser, so a grant or revocation applies to the next request.
func (s *Service) Authenticate(ctx context.Context, token string) (authlib.Principal, error) {
	if token == "" || len(token) > 256 {
		return authlib.Principal{}, authlib.ErrUnauthenticated
	}
	session, user, err := s.store.SelectSessionByTokenHash(ctx, authlib.HashToken(token))
	if errors.Is(err, authdomain.ErrSessionNotFound) {
		return authlib.Principal{}, authlib.ErrUnauthenticated
	}
	if err != nil {
		return authlib.Principal{}, dbError("authenticate", err)
	}
	now := s.now()
	if !session.ActiveAt(now) || user.Banned() {
		return authlib.Principal{}, authlib.ErrUnauthenticated
	}
	if now.Sub(session.LastSeenAt) >= authlib.SessionTouchInterval {
		idle := minTime(now.Add(authlib.SessionIdleLimits.Clamp(s.sessionIdle.Get(ctx))), session.AbsoluteExpiresAt)
		if err := s.store.TouchSession(ctx, session.ID, now, idle); err != nil {
			s.logger.WarnContext(ctx, "update session last seen", "session_id", session.ID, "err", err)
		}
	}
	roles, err := s.store.SelectUserRoles(ctx, user.ID)
	if err != nil {
		return authlib.Principal{}, dbError("authenticate", err)
	}
	// Roles that require two-factor authentication grant their permissions
	// only to sessions verified with a second factor (ADR-0043).
	granted, stepUp := s.userPermissions(roles, session.MFAVerified())
	p := authlib.Principal{
		UserID: user.ID, SessionID: session.ID, Permissions: granted, StepUp: stepUp,
		MFAVerified: session.MFAVerified(), SignedInAt: session.CreatedAt,
	}
	if session.MFAVerifiedAt != nil {
		p.MFAVerifiedAt = *session.MFAVerifiedAt
	}
	return p, nil
}

// MeView is the signed-in user, their current session and permissions.
type MeView struct {
	User        authdomain.User
	Session     authdomain.Session
	Permissions []string
	// StepUp are permissions granted after signing in with a second factor.
	StepUp []string
	// MFAEnabled reports whether the user has two-factor authentication on,
	// and MFARequired whether a role of theirs requires it.
	MFAEnabled  bool
	MFARequired bool
}

// Me returns the signed-in user.
func (s *Service) Me(ctx context.Context) (MeView, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return MeView{}, err
	}
	sessions, err := s.Sessions(ctx)
	if err != nil {
		return MeView{}, err
	}
	for _, view := range sessions {
		if view.Current {
			u, err := s.User(ctx, p.UserID)
			if errors.Is(err, authdomain.ErrUserNotFound) {
				return MeView{}, authlib.ErrUnauthenticated
			}
			if err != nil {
				return MeView{}, err
			}
			mfaEnabled, err := s.hasSecondFactor(ctx, s.store, u.ID)
			if err != nil {
				return MeView{}, dbError("get the signed-in user", err)
			}
			return MeView{
				User: u, Session: view.Session, Permissions: p.Permissions, StepUp: p.StepUp,
				MFAEnabled: mfaEnabled, MFARequired: s.catalog.RequiresMFA(u.Roles...),
			}, nil
		}
	}
	return MeView{}, authlib.ErrUnauthenticated
}

// SessionView is a session in a user's list.
type SessionView struct {
	authdomain.Session
	// Current reports whether this session made the request.
	Current bool
}

// Sessions returns the user's active sessions, most recently used first.
func (s *Service) Sessions(ctx context.Context) ([]SessionView, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	sessions, err := s.store.SelectActiveSessions(ctx, p.UserID, s.now())
	if err != nil {
		return nil, dbError("list sessions", err)
	}
	views := make([]SessionView, len(sessions))
	for i, session := range sessions {
		views[i] = SessionView{Session: session, Current: session.ID == p.SessionID}
	}
	return views, nil
}

// Logout ends the session that made the request.
func (s *Service) Logout(ctx context.Context) error {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return err
	}
	if _, err := s.store.RevokeSession(ctx, p.SessionID, p.UserID, s.now(), "logout"); err != nil {
		return dbError("logout", err)
	}
	s.audit(ctx, sessionEvent(p.UserID, p.SessionID, "logout"))
	return nil
}

// LogoutAll ends every session of the user, including the current one, and
// returns how many ended.
func (s *Service) LogoutAll(ctx context.Context) (int64, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return 0, err
	}
	n, err := s.store.RevokeUserSessions(ctx, p.UserID, "", s.now(), "logout_all")
	if err != nil {
		return 0, dbError("logout all", err)
	}
	e := userEvent("auth.sessions.revoked", p.UserID, authlib.ClientInfoFromContext(ctx))
	e.Metadata = map[string]any{"reason": "logout_all", "count": n}
	s.audit(ctx, e)
	return n, nil
}

// RevokeSession ends one of the user's sessions. It returns
// ErrSessionNotFound for a session of another user or one already ended.
func (s *Service) RevokeSession(ctx context.Context, sessionID string) error {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return err
	}
	ok, err := s.store.RevokeSession(ctx, sessionID, p.UserID, s.now(), "revoked_by_user")
	if err != nil {
		return dbError("revoke session", err)
	}
	if !ok {
		return authdomain.ErrSessionNotFound
	}
	s.audit(ctx, sessionEvent(p.UserID, sessionID, "revoked_by_user"))
	return nil
}

func sessionEvent(userID, sessionID, reason string) auditEvent {
	return auditEvent{Action: "auth.session.revoked", ResourceType: "session", ResourceID: sessionID, Metadata: map[string]any{"reason": reason, "user_id": userID}}
}
