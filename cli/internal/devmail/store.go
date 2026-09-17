package devmail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Limits of the store.
const (
	// MaxMessages is how many messages the inbox keeps; older ones are
	// deleted first.
	MaxMessages = 500
	// Dir is where the messages live, inside the app.
	Dir = ".orb/portal/mail"
)

// ErrNotFound reports a message ID the store doesn't have.
var ErrNotFound = errors.New("no such message")

// Summary is a message in the list.
type Summary struct {
	ID       string    `json:"id"`
	Time     time.Time `json:"time"`
	From     Address   `json:"from"`
	To       []Address `json:"to"`
	Subject  string    `json:"subject"`
	Snippet  string    `json:"snippet"`
	Size     int64     `json:"size"`
	HasHTML  bool      `json:"has_html"`
	HasText  bool      `json:"has_text"`
	Codes    []string  `json:"codes"`
	Category string    `json:"category,omitempty"`
	// Attachments counts them; the detail lists them.
	Attachments int  `json:"attachments"`
	Read        bool `json:"read"`
}

// Address is a parsed address.
type Address struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email"`
}

// Detail is a message with its bodies.
type Detail struct {
	Summary
	ReplyTo     []Address         `json:"reply_to,omitempty"`
	CC          []Address         `json:"cc,omitempty"`
	Headers     map[string]string `json:"headers"`
	Text        string            `json:"text"`
	HTML        string            `json:"html"`
	Links       []Link            `json:"links"`
	Attachment  []Attachment      `json:"attachment_list"`
	MessageID   string            `json:"message_id,omitempty"`
	Envelope    Envelope          `json:"envelope"`
	SourceBytes int64             `json:"source_bytes"`
}

// Envelope is what SMTP said, as opposed to the headers.
type Envelope struct {
	From string   `json:"from"`
	To   []string `json:"to"`
}

// Link is a URL found in the message.
type Link struct {
	URL  string `json:"url"`
	Text string `json:"text,omitempty"`
}

// Attachment describes a part with a file name.
type Attachment struct {
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Size        int    `json:"size"`
}

// Store keeps messages as .eml files with a JSON summary next to each,
// and serves them to the portal. It is safe for concurrent use.
type Store struct {
	dir  string
	mu   sync.Mutex
	seq  int64
	list []Summary // newest last
	subs map[chan Summary]struct{}
	now  func() time.Time
	max  int
}

// Open opens the store under the app directory dir, creating it, and
// reads the summaries it has.
func Open(dir string) (*Store, error) {
	s := &Store{dir: filepath.Join(dir, Dir), subs: map[chan Summary]struct{}{}, now: time.Now, max: MaxMessages}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var sum Summary
		if json.Unmarshal(data, &sum) != nil || sum.ID != id {
			continue
		}
		if n, err := strconv.ParseInt(id, 10, 64); err == nil && n > s.seq {
			s.seq = n
		}
		s.list = append(s.list, sum)
	}
	sort.Slice(s.list, func(i, j int) bool { return s.list[i].Time.Before(s.list[j].Time) })
	return s, nil
}

// Add stores a message received from from for rcpts.
func (s *Store) Add(_ context.Context, from string, rcpts []string, raw []byte) (Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	id := fmt.Sprintf("%012d", s.seq)
	sum, _, err := Parse(raw)
	if err != nil {
		return Summary{}, err
	}
	sum.ID, sum.Size = id, int64(len(raw))
	if sum.Time.IsZero() {
		sum.Time = s.now().UTC()
	}
	if len(sum.To) == 0 {
		for _, r := range rcpts {
			sum.To = append(sum.To, Address{Email: r})
		}
	}
	if sum.From.Email == "" {
		sum.From.Email = from
	}
	env, _ := json.Marshal(Envelope{From: from, To: rcpts})
	if err := os.WriteFile(filepath.Join(s.dir, id+".eml"), raw, 0o600); err != nil {
		return Summary{}, err
	}
	if err := os.WriteFile(filepath.Join(s.dir, id+".env.json"), env, 0o600); err != nil {
		return Summary{}, err
	}
	data, _ := json.Marshal(sum)
	if err := os.WriteFile(filepath.Join(s.dir, id+".json"), data, 0o600); err != nil {
		return Summary{}, err
	}
	s.list = append(s.list, sum)
	for len(s.list) > s.max {
		s.remove(s.list[0].ID)
		s.list = s.list[1:]
	}
	for c := range s.subs {
		select {
		case c <- sum:
		default:
		}
	}
	return sum, nil
}

func (s *Store) remove(id string) {
	for _, suffix := range []string{".eml", ".json", ".env.json"} {
		_ = os.Remove(filepath.Join(s.dir, id+suffix))
	}
}

// List returns the messages newest first, those matching q (a
// case-insensitive substring of the subject, addresses or snippet) when
// q isn't empty, at most limit (default 100).
func (s *Store) List(q string, limit int) ([]Summary, int) {
	if limit <= 0 {
		limit = 100
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	q = strings.ToLower(strings.TrimSpace(q))
	out := []Summary{}
	total := 0
	for i := len(s.list) - 1; i >= 0; i-- {
		m := s.list[i]
		if q != "" && !matches(m, q) {
			continue
		}
		total++
		if len(out) < limit {
			out = append(out, m)
		}
	}
	return out, total
}

func matches(m Summary, q string) bool {
	if strings.Contains(strings.ToLower(m.Subject), q) || strings.Contains(strings.ToLower(m.Snippet), q) || strings.Contains(strings.ToLower(m.From.Email), q) || strings.Contains(strings.ToLower(m.From.Name), q) {
		return true
	}
	for _, a := range m.To {
		if strings.Contains(strings.ToLower(a.Email), q) || strings.Contains(strings.ToLower(a.Name), q) {
			return true
		}
	}
	return false
}

// Count is how many messages the store keeps.
func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.list)
}

// Get returns a message with its bodies, marking it read.
func (s *Store) Get(id string) (Detail, error) {
	raw, err := s.Source(id)
	if err != nil {
		return Detail{}, err
	}
	sum, detail, err := Parse(raw)
	if err != nil {
		return Detail{}, err
	}
	s.mu.Lock()
	for i := range s.list {
		if s.list[i].ID == id {
			if !s.list[i].Read {
				s.list[i].Read = true
				data, _ := json.Marshal(s.list[i])
				_ = os.WriteFile(filepath.Join(s.dir, id+".json"), data, 0o600)
			}
			sum = s.list[i]
		}
	}
	s.mu.Unlock()
	detail.Summary = sum
	detail.SourceBytes = int64(len(raw))
	if env, err := os.ReadFile(filepath.Join(s.dir, id+".env.json")); err == nil {
		_ = json.Unmarshal(env, &detail.Envelope)
	}
	return detail, nil
}

// Source returns the message as received.
func (s *Store) Source(id string) ([]byte, error) {
	if _, err := strconv.ParseInt(id, 10, 64); err != nil || len(id) != 12 {
		return nil, ErrNotFound
	}
	raw, err := os.ReadFile(filepath.Join(s.dir, id+".eml"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return raw, err
}

// Delete removes one message.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.list {
		if s.list[i].ID == id {
			s.remove(id)
			s.list = append(s.list[:i], s.list[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

// Clear removes every message.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.list {
		s.remove(m.ID)
	}
	s.list = nil
	return nil
}

// Subscribe returns a channel of new messages; stop with the returned
// function.
func (s *Store) Subscribe() (<-chan Summary, func()) {
	c := make(chan Summary, 64)
	s.mu.Lock()
	s.subs[c] = struct{}{}
	s.mu.Unlock()
	return c, func() {
		s.mu.Lock()
		delete(s.subs, c)
		s.mu.Unlock()
	}
}
