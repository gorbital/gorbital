package repository

import (
	"context"

	orgsdomain "example.com/plateful/internal/modules/orgs/domain"
)

const insertMemberSQL = `
	INSERT INTO org_members (org_id, user_id, role, joined_at, added_by)
	VALUES ($1, $2, $3, $4, $5)`

// InsertMember adds a member, or returns ErrAlreadyMember.
func (s *Store) InsertMember(ctx context.Context, m orgsdomain.Member) error {
	_, err := s.db.Exec(ctx, insertMemberSQL, m.OrgID, m.UserID, m.Role, m.JoinedAt, m.AddedBy)
	return constraintError(err)
}
