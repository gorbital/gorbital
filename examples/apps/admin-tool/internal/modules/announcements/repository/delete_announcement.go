package repository

import "context"

const deleteAnnouncementSQL = `DELETE FROM announcements WHERE id = $1`

// DeleteAnnouncement removes the announcement with this ID, and reports
// whether there was one.
func (s *Store) DeleteAnnouncement(ctx context.Context, id string) (bool, error) {
	tag, err := s.db.Exec(ctx, deleteAnnouncementSQL, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
