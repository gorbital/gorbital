package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// maxOrgIDLen bounds organisation IDs, which travel in notification
// payloads.
const maxOrgIDLen = 100

// checkOrgID accepts 1 to 100 visible ASCII characters: no spaces, which
// separate the key from the organisation in notifications.
func checkOrgID(orgID string) error {
	if orgID == "" || len(orgID) > maxOrgIDLen {
		return ErrInvalidOrgID
	}
	for i := range len(orgID) {
		if c := orgID[i]; c <= ' ' || c > '~' {
			return ErrInvalidOrgID
		}
	}
	return nil
}

// orgValue is an organisation's stored state of one setting. Organisations
// keep a short slice of them rather than a map: a map's buckets would cost
// about a kilobyte per organisation.
type orgValue struct {
	key string
	stored
}

// findOrgValue returns the stored state of key in values.
func findOrgValue(values []orgValue, key string) (stored, bool) {
	for _, v := range values {
		if v.key == key {
			return v.stored, true
		}
	}
	return stored{}, false
}

// orgValue returns orgID's stored value of key and whether there is one.
func (r *Registry) orgValue(orgID, key string) (stored, bool) {
	values, ok := r.orgs.Load(orgID)
	if !ok {
		return stored{}, false
	}
	return findOrgValue(values.([]orgValue), key)
}

// currentOrgSeq returns the sequence number of the last organisation value
// applied.
func (r *Registry) currentOrgSeq() uint64 {
	r.snapMu.Lock()
	defer r.snapMu.Unlock()
	return r.orgSeq
}

// applyOrg stores one organisation value unless a newer version is already
// known, like apply.
func (r *Registry) applyOrg(orgID, key string, st stored) {
	r.snapMu.Lock()
	defer r.snapMu.Unlock()
	cur, _ := r.orgs.Load(orgID)
	old, _ := cur.([]orgValue)
	if prev, ok := findOrgValue(old, key); ok && prev.version > st.version {
		return
	}
	r.orgSeq++
	st.seq = r.orgSeq
	next := make([]orgValue, 0, len(old)+1)
	for _, v := range old {
		if v.key != key {
			next = append(next, v)
		}
	}
	r.orgs.Store(orgID, append(next, orgValue{key: key, stored: st}))
}

// replaceOrgs installs a full reload of organisation values that started
// after the value numbered since was applied. A newer version already
// applied is kept. A value missing from loaded is dropped, since its row was
// deleted with its organisation, unless it was applied while the reload ran.
func (r *Registry) replaceOrgs(loaded map[string][]orgValue, since uint64) {
	r.snapMu.Lock()
	defer r.snapMu.Unlock()
	r.orgs.Range(func(k, v any) bool {
		orgID := k.(string)
		next := loaded[orgID]
		for _, old := range v.([]orgValue) {
			i := slices.IndexFunc(next, func(n orgValue) bool { return n.key == old.key })
			switch {
			case i < 0 && old.seq > since:
				next = append(next, old)
			case i >= 0 && old.version > next[i].version:
				next[i] = old
			}
		}
		if len(next) == 0 {
			r.orgs.Delete(orgID)
		} else {
			loaded[orgID] = next
		}
		return true
	})
	for orgID, values := range loaded {
		r.orgs.Store(orgID, slices.Clip(values))
	}
}

// reloadOrgs reads every organisation value of the settings declared
// OrgOverridable. since is the organisation sequence number before the
// reload started.
func (s *Store) reloadOrgs(ctx context.Context, since uint64) error {
	var keys []string
	for _, d := range s.reg.definitions() {
		if d.orgOverridable {
			keys = append(keys, d.key)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	rows, err := selectOrgValues(ctx, s.pool, keys)
	if err != nil {
		return fmt.Errorf("settings: load organisation values: %v", err) //nolint:errorlint // driver errors aren't API (ADR-0018)
	}
	loaded := make(map[string][]orgValue)
	for _, row := range rows {
		if d, ok := s.reg.lookup(row.key); ok {
			loaded[row.orgID] = append(loaded[row.orgID], orgValue{key: row.key, stored: s.decodeRow(ctx, d, row)})
		}
	}
	s.reg.replaceOrgs(loaded, since)
	return nil
}

// reloadOrgKey reloads orgID's value of an OrgOverridable setting after a
// notification.
func (s *Store) reloadOrgKey(ctx context.Context, d *definition, orgID string) error {
	// A notification for a setting no organisation can override, or for an
	// ID that is not an organisation's, is not this store's to apply.
	if !d.orgOverridable || checkOrgID(orgID) != nil {
		return nil //nolint:nilerr // a notification that doesn't concern us is ignored, not an error
	}
	row, found, err := selectOrgValue(ctx, s.pool, orgID, d.key)
	if err != nil || !found {
		return err
	}
	s.reg.applyOrg(orgID, d.key, s.decodeRow(ctx, d, row))
	return nil
}

// orgDefinition returns key's definition for an organisation value, or
// [ErrInvalidOrgID], [ErrUnknownSetting] or [ErrNotOrgOverridable].
func (s *Store) orgDefinition(orgID, key string) (*definition, error) {
	if err := checkOrgID(orgID); err != nil {
		return nil, err
	}
	d, ok := s.reg.lookup(key)
	if !ok {
		return nil, ErrUnknownSetting
	}
	if !d.orgOverridable {
		return nil, ErrNotOrgOverridable
	}
	return d, nil
}

// orgView is the view of d for orgID, from memory.
func (s *Store) orgView(d *definition, orgID string) View {
	st, ok := s.reg.orgValue(orgID, d.key)
	return s.orgViewOf(d, orgID, st, ok)
}

// orgViewOf is the view of d for orgID with the organisation's stored state
// st, when found.
func (s *Store) orgViewOf(d *definition, orgID string, st stored, found bool) View {
	v := s.view(d)
	v.OrgID, v.PlatformValue = orgID, v.Value
	v.Modified, v.InvalidStoredValue = false, false
	v.Version, v.UpdatedAt, v.UpdatedBy = 0, time.Time{}, ""
	if found {
		v.Version, v.UpdatedAt, v.UpdatedBy, v.InvalidStoredValue = st.version, st.updatedAt, st.updatedBy, st.invalid
		if st.value != nil {
			v.Value, v.Modified = d.encode(st.value), true
		}
	}
	return v
}

// ListForOrg returns the settings declared [OrgOverridable], in declaration
// order, as organisation orgID sees them: Value is the organisation's own
// value when it has a valid one, else PlatformValue. It reads memory only.
// It returns [ErrInvalidOrgID] for an empty or malformed ID; checking that
// the organisation exists, and that the caller may see it, is the caller's
// job.
func (s *Store) ListForOrg(orgID string) ([]View, error) {
	if err := checkOrgID(orgID); err != nil {
		return nil, err
	}
	views := []View{}
	for _, d := range s.reg.definitions() {
		if d.orgOverridable {
			views = append(views, s.orgView(d, orgID))
		}
	}
	return views, nil
}

// GetForOrg returns one setting as organisation orgID sees it, or
// [ErrInvalidOrgID], [ErrUnknownSetting] or [ErrNotOrgOverridable].
func (s *Store) GetForOrg(orgID, key string) (View, error) {
	d, err := s.orgDefinition(orgID, key)
	if err != nil {
		return View{}, err
	}
	return s.orgView(d, orgID), nil
}

// SetForOrg validates value like [Store.Set] and stores it as organisation
// orgID's own value of key, a setting declared [OrgOverridable]. Pass the
// organisation view's Version. The change is recorded in the organisation's
// history and as a "settings.value.changed" audit event carrying the
// organisation, and applies on every instance within moments.
//
// It returns the errors of [Store.Set], [ErrInvalidOrgID] and
// [ErrNotOrgOverridable]. The organisation must exist; checking that the
// caller may change its settings is the caller's job.
func (s *Store) SetForOrg(ctx context.Context, orgID, key string, value json.RawMessage, change Change) (View, error) {
	d, err := s.orgDefinition(orgID, key)
	if err != nil {
		return View{}, err
	}
	v, err := d.parse(value)
	if err != nil {
		return View{}, &InvalidValueError{Key: key, Reason: err.Error()}
	}
	return s.write(ctx, d, orgID, d.encode(v), change)
}

// ResetForOrg removes organisation orgID's own value of key, so the
// organisation gets the platform value again. It returns the same errors as
// [Store.SetForOrg], except for invalid values.
func (s *Store) ResetForOrg(ctx context.Context, orgID, key string, change Change) (View, error) {
	d, err := s.orgDefinition(orgID, key)
	if err != nil {
		return View{}, err
	}
	return s.write(ctx, d, orgID, nil, change)
}

// HistoryForOrg returns changes to organisation orgID's value of key, newest
// first, paged like [Store.History].
func (s *Store) HistoryForOrg(ctx context.Context, orgID, key string, before int64, limit int) ([]HistoryEntry, error) {
	if _, err := s.orgDefinition(orgID, key); err != nil {
		return nil, err
	}
	limit = min(max(limit, 1), 100)
	entries, err := selectHistory(ctx, s.pool, key, orgID, before, limit)
	if err != nil {
		return nil, fmt.Errorf("settings: history %s for an organisation: %v", key, err) //nolint:errorlint // driver errors aren't API (ADR-0018)
	}
	return entries, nil
}

// Overrides returns the organisations that have their own value of key, by
// organisation ID, as views for each organisation. For the next page, pass
// the last view's OrgID as after ("" starts from the first). limit is
// clamped to 1–100. A setting not declared [OrgOverridable] has none; an
// undeclared key returns [ErrUnknownSetting].
func (s *Store) Overrides(ctx context.Context, key, after string, limit int) ([]View, error) {
	d, ok := s.reg.lookup(key)
	if !ok {
		return nil, ErrUnknownSetting
	}
	if !d.orgOverridable {
		return []View{}, nil
	}
	limit = min(max(limit, 1), 100)
	rows, err := selectOverrides(ctx, s.pool, key, after, limit)
	if err != nil {
		return nil, fmt.Errorf("settings: overrides of %s: %v", key, err) //nolint:errorlint // driver errors aren't API (ADR-0018)
	}
	views := make([]View, len(rows))
	for i, row := range rows {
		st, _ := toStored(d, row) // an invalid value is flagged in the view
		views[i] = s.orgViewOf(d, row.orgID, st, true)
	}
	return views, nil
}
