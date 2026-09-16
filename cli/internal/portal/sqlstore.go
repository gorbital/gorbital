package portal

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// SQLStore keeps the SQL editor's snippets and history (ADR-0068).
// Snippets are files under db/queries in the app, so they are committed and
// shared; favourites and history are the developer's own, under
// .orb/portal, which git ignores.
type SQLStore struct {
	dir string // the app directory
	mu  sync.Mutex
}

// Paths inside the app.
const (
	SnippetsDir = "db/queries"
	portalDir   = ".orb/portal"
	historyFile = portalDir + "/sql-history.jsonl"
	favFile     = portalDir + "/sql-favorites.json"
	// MaxHistory is how many runs the history keeps.
	MaxHistory = 500
	// maxSnippetBytes bounds a snippet.
	maxSnippetBytes = 1 << 20
)

// Snippet is a saved query.
type Snippet struct {
	// Name is the file name without .sql: letters, digits, hyphens and
	// underscores.
	Name     string    `json:"name"`
	SQL      string    `json:"sql"`
	Favorite bool      `json:"favorite"`
	Modified time.Time `json:"modified"`
	// Path is the file relative to the app, for the developer.
	Path string `json:"path"`
}

// HistoryEntry is one run.
type HistoryEntry struct {
	Time       time.Time `json:"time"`
	SQL        string    `json:"sql"`
	Mode       string    `json:"mode"`
	DurationMS float64   `json:"duration_ms"`
	// Rows is the rows of the last result set; Error the error message.
	Rows  int    `json:"rows"`
	Error string `json:"error,omitempty"`
}

// ErrSnippetName reports a name that isn't letters, digits, hyphens and
// underscores.
var ErrSnippetName = errors.New("portal: a snippet name is 1 to 80 letters, digits, hyphens or underscores")

var snippetName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,79}$`)

// NewSQLStore returns a store for the app at dir.
func NewSQLStore(dir string) *SQLStore { return &SQLStore{dir: dir} }

// Snippets lists the saved queries, favourites first, then by name.
func (s *SQLStore) Snippets() ([]Snippet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	favs, err := s.favorites()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(s.dir, filepath.FromSlash(SnippetsDir))
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Snippet{}, nil
	} else if err != nil {
		return nil, err
	}
	out := []Snippet{}
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".sql")
		if e.IsDir() || !ok || !snippetName.MatchString(name) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		info, err := e.Info()
		if err != nil {
			return nil, err
		}
		out = append(out, Snippet{Name: name, SQL: string(data), Favorite: favs[name], Modified: info.ModTime().UTC(), Path: SnippetsDir + "/" + e.Name()})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Favorite != out[j].Favorite {
			return out[i].Favorite
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// Save writes a snippet and its favourite flag.
func (s *SQLStore) Save(name, sql string, favorite bool) (Snippet, error) {
	if !snippetName.MatchString(name) {
		return Snippet{}, ErrSnippetName
	}
	if len(sql) > maxSnippetBytes {
		return Snippet{}, fmt.Errorf("portal: the snippet is over %d bytes", maxSnippetBytes)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	root, err := os.OpenRoot(s.dir)
	if err != nil {
		return Snippet{}, err
	}
	defer root.Close()
	if err := root.MkdirAll(filepath.FromSlash(SnippetsDir), 0o755); err != nil {
		return Snippet{}, err
	}
	path := filepath.Join(filepath.FromSlash(SnippetsDir), name+".sql")
	if err := root.WriteFile(path, []byte(sql), 0o644); err != nil {
		return Snippet{}, err
	}
	favs, err := s.favorites()
	if err != nil {
		return Snippet{}, err
	}
	if favorite {
		favs[name] = true
	} else {
		delete(favs, name)
	}
	if err := s.writeFavorites(favs); err != nil {
		return Snippet{}, err
	}
	return Snippet{Name: name, SQL: sql, Favorite: favorite, Modified: time.Now().UTC(), Path: SnippetsDir + "/" + name + ".sql"}, nil
}

// Delete removes a snippet; a missing one is fine.
func (s *SQLStore) Delete(name string) error {
	if !snippetName.MatchString(name) {
		return ErrSnippetName
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	root, err := os.OpenRoot(s.dir)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.Remove(filepath.Join(filepath.FromSlash(SnippetsDir), name+".sql")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	favs, err := s.favorites()
	if err != nil {
		return err
	}
	delete(favs, name)
	return s.writeFavorites(favs)
}

// favorites reads the favourite names. The caller holds the mutex.
func (s *SQLStore) favorites() (map[string]bool, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, filepath.FromSlash(favFile)))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]bool{}, nil
	} else if err != nil {
		return nil, err
	}
	var names []string
	if err := json.Unmarshal(data, &names); err != nil {
		return map[string]bool{}, nil //nolint:nilerr // a broken favourites file starts over; it holds only names
	}
	out := map[string]bool{}
	for _, n := range names {
		out[n] = true
	}
	return out, nil
}

func (s *SQLStore) writeFavorites(favs map[string]bool) error {
	names := make([]string, 0, len(favs))
	for n := range favs {
		names = append(names, n)
	}
	sort.Strings(names)
	data, _ := json.Marshal(names)
	if err := os.MkdirAll(filepath.Join(s.dir, filepath.FromSlash(portalDir)), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, filepath.FromSlash(favFile)), data, 0o644)
}

// Record appends a run to the history, keeping the newest MaxHistory.
func (s *SQLStore) Record(e HistoryEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(e.SQL) > 16<<10 {
		e.SQL = e.SQL[:16<<10] + "…"
	}
	path := filepath.Join(s.dir, filepath.FromSlash(historyFile))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) // queries may hold data the developer typed
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	entries, err := s.readHistory()
	if err != nil {
		return err
	}
	if len(entries) > MaxHistory+100 {
		return s.writeHistory(entries[len(entries)-MaxHistory:])
	}
	return nil
}

// History returns the runs, newest first, at most MaxHistory.
func (s *SQLStore) History() ([]HistoryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.readHistory()
	if err != nil {
		return nil, err
	}
	if len(entries) > MaxHistory {
		entries = entries[len(entries)-MaxHistory:]
	}
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	return entries, nil
}

// ClearHistory forgets every run.
func (s *SQLStore) ClearHistory() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(filepath.Join(s.dir, filepath.FromSlash(historyFile)))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *SQLStore) readHistory() ([]HistoryEntry, error) {
	f, err := os.Open(filepath.Join(s.dir, filepath.FromSlash(historyFile)))
	if errors.Is(err, os.ErrNotExist) {
		return []HistoryEntry{}, nil
	} else if err != nil {
		return nil, err
	}
	defer f.Close()
	entries := []HistoryEntry{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		var e HistoryEntry
		if json.Unmarshal(scanner.Bytes(), &e) == nil {
			entries = append(entries, e)
		}
	}
	return entries, scanner.Err()
}

func (s *SQLStore) writeHistory(entries []HistoryEntry) error {
	var b strings.Builder
	for _, e := range entries {
		line, _ := json.Marshal(e)
		b.Write(line)
		b.WriteByte('\n')
	}
	return os.WriteFile(filepath.Join(s.dir, filepath.FromSlash(historyFile)), []byte(b.String()), 0o600)
}
