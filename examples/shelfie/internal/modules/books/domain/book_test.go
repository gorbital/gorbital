package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"example.com/shelfie/internal/modules/books/domain"
)

func TestNewBook(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name    string
		fields  domain.Fields
		want    domain.Book
		wantErr error
	}{
		{"defaults", domain.Fields{Title: " Dune "}, domain.Book{ID: "bok_1", OwnerID: "usr_1", Title: "Dune", Status: domain.WantToRead, CreatedAt: now, UpdatedAt: now}, nil},
		{"ISBN-10 with X", domain.Fields{Title: "Dune", ISBN: "0-8044-2957-x", Status: domain.Read}, domain.Book{ID: "bok_1", OwnerID: "usr_1", Title: "Dune", ISBN: "080442957X", Status: domain.Read, CreatedAt: now, UpdatedAt: now}, nil},
		{"blank title", domain.Fields{Title: "  "}, domain.Book{}, domain.ErrTitleRequired},
		{"long title", domain.Fields{Title: strings.Repeat("é", 301)}, domain.Book{}, domain.ErrTitleRequired},
		{"long author", domain.Fields{Title: "Dune", Author: strings.Repeat("a", 201)}, domain.Book{}, domain.ErrAuthorTooLong},
		{"unknown status", domain.Fields{Title: "Dune", Status: "abandoned"}, domain.Book{}, domain.ErrInvalidStatus},
		{"X in an ISBN-13", domain.Fields{Title: "Dune", ISBN: "978044117271X"}, domain.Book{}, domain.ErrInvalidISBN},
	} {
		got, err := domain.NewBook("bok_1", "usr_1", tt.fields, now)
		if !errors.Is(err, tt.wantErr) || got != tt.want {
			t.Errorf("%s: NewBook() = %+v, %v; want %+v, %v", tt.name, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestApply(t *testing.T) {
	then := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	book, _ := domain.NewBook("bok_1", "usr_1", domain.Fields{Title: "Dune", ISBN: "0441172717"}, then)
	now := then.Add(time.Hour)
	empty, reading := "", domain.Reading
	got, err := book.Apply(domain.Changes{ISBN: &empty, Status: &reading}, now)
	if err != nil || got.ISBN != "" || got.Status != domain.Reading || got.Title != "Dune" || !got.UpdatedAt.Equal(now) || !got.CreatedAt.Equal(then) {
		t.Errorf("Apply() = %+v, %v", got, err)
	}
	blank := " "
	if _, err := book.Apply(domain.Changes{Title: &blank}, now); !errors.Is(err, domain.ErrTitleRequired) {
		t.Errorf("Apply(blank title) error = %v", err)
	}
}

// FuzzNormalizeISBN: an accepted ISBN is 10 or 13 characters of digits, with
// an X only last in an ISBN-10, and normalizing it again changes nothing.
func FuzzNormalizeISBN(f *testing.F) {
	for _, seed := range []string{"978-0-441-17271-9", "0441172717", "080442957x", "978044117271X", "", " - ", "12345", "٠٤٤١١٧٢٧١٧"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		isbn, err := domain.NormalizeISBN(s)
		if err != nil {
			if !errors.Is(err, domain.ErrInvalidISBN) || isbn != "" {
				t.Fatalf("NormalizeISBN(%q) = %q, %v", s, isbn, err)
			}
			return
		}
		if isbn == "" {
			return
		}
		if n := len(isbn); n != 10 && n != 13 {
			t.Fatalf("NormalizeISBN(%q) = %q: length %d", s, isbn, n)
		}
		for i, r := range isbn {
			digit, checkX := r >= '0' && r <= '9', r == 'X' && len(isbn) == 10 && i == 9
			if !digit && !checkX {
				t.Fatalf("NormalizeISBN(%q) = %q: %q at %d", s, isbn, r, i)
			}
		}
		if again, err := domain.NormalizeISBN(isbn); err != nil || again != isbn {
			t.Fatalf("NormalizeISBN(%q) = %q, %v; not stable", isbn, again, err)
		}
	})
}
