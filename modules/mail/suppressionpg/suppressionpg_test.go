package suppressionpg_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorbital.dev/mail"
	"gorbital.dev/modules/mail/suppressionpg"
	"gorbital.dev/modules/postgres/pgtest"
)

func newStore(t *testing.T) *suppressionpg.Store {
	t.Helper()
	s, err := suppressionpg.NewStore(pgtest.New(t, pgtest.WithMigrations(suppressionpg.Migrations)))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAddAndSuppressed(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	added, err := s.Add(ctx,
		suppressionpg.Entry{Email: " Ada@Example.com ", Reason: suppressionpg.ReasonBounce, Source: "resend", Detail: "Permanent/General"},
		suppressionpg.Entry{Email: "grace@example.com", Reason: suppressionpg.ReasonComplaint, Source: "resend"},
	)
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if len(added) != 2 || added[0].Email != "ada@example.com" || added[0].Reason != suppressionpg.ReasonBounce || added[0].Detail != "Permanent/General" ||
		added[0].ID == 0 || added[0].CreatedAt.IsZero() || added[0].CreatedAt.Location() != time.UTC {
		t.Errorf("Add() = %+v", added)
	}

	// A new event for a listed address updates it without adding it again.
	again, err := s.Add(ctx, suppressionpg.Entry{Email: "ADA@example.com", Reason: suppressionpg.ReasonComplaint, Source: "resend"})
	if err != nil || len(again) != 0 {
		t.Errorf("Add() for a listed address = %+v, %v; want nothing added", again, err)
	}

	got, err := s.Suppressed(ctx, []string{"ada@EXAMPLE.com", "nobody@example.com", "grace@example.com"})
	slices.Sort(got)
	if err != nil || !slices.Equal(got, []string{"ada@example.com", "grace@example.com"}) {
		t.Errorf("Suppressed() = %v, %v", got, err)
	}
	if got, err := s.Suppressed(ctx, nil); err != nil || len(got) != 0 {
		t.Errorf("Suppressed(nil) = %v, %v", got, err)
	}

	page, err := s.List(ctx, suppressionpg.Filter{})
	if err != nil || len(page.Suppressions) != 2 || page.Suppressions[1].Reason != suppressionpg.ReasonComplaint || page.Suppressions[1].ID != added[0].ID {
		t.Errorf("List() = %+v, %v; want ada updated to complaint", page, err)
	}

	for _, bad := range []suppressionpg.Entry{
		{Email: "not an address", Reason: suppressionpg.ReasonBounce, Source: "resend"},
		{Email: "", Reason: suppressionpg.ReasonBounce, Source: "resend"},
		{Email: "eve@example.com", Reason: "soft_bounce", Source: "resend"},
		{Email: "eve@example.com", Reason: suppressionpg.ReasonBounce, Source: "Resend!"},
		{Email: "eve@example.com", Reason: suppressionpg.ReasonBounce},
	} {
		_, err := s.Add(ctx, bad)
		if !errors.Is(err, suppressionpg.ErrInvalidEntry) {
			t.Errorf("Add(%+v) error = %v, want ErrInvalidEntry", bad, err)
		}
		if err != nil && bad.Email != "" && strings.Contains(err.Error(), bad.Email) {
			t.Errorf("Add() error quotes the address: %v", err)
		}
	}
	// One invalid entry adds none of them.
	if _, err := s.Add(ctx, suppressionpg.Entry{Email: "eve@example.com", Reason: suppressionpg.ReasonBounce, Source: "resend"}, suppressionpg.Entry{Email: "x"}); err == nil {
		t.Error("Add() with an invalid entry error = nil")
	}
	if got, _ := s.Suppressed(ctx, []string{"eve@example.com"}); len(got) != 0 {
		t.Error("a valid entry was added alongside an invalid one")
	}

	long, err := s.Add(ctx, suppressionpg.Entry{Email: "long@example.com", Reason: suppressionpg.ReasonBounce, Source: "resend", Detail: strings.Repeat("é", 150)})
	if err != nil || len(long) != 1 || len(long[0].Detail) > 200 || !strings.HasPrefix(long[0].Detail, "é") {
		t.Errorf("Add() with a long detail = %+v, %v; want it truncated to valid UTF-8", long, err)
	}
}

func TestSuppressionListStopsSending(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if _, err := s.Add(ctx, suppressionpg.Entry{Email: "ada@example.com", Reason: suppressionpg.ReasonBounce, Source: "resend"}); err != nil {
		t.Fatal(err)
	}
	var sent int
	sender := mail.WithSuppressionList(mail.SenderFunc(func(context.Context, mail.Message) error { sent++; return nil }), s)
	err := sender.Send(ctx, mail.Message{
		From: mail.Address{Email: "no-reply@acme.test"}, To: []mail.Address{{Email: "Ada@example.com"}}, Subject: "Hi", Text: "Hello",
	})
	if !errors.Is(err, mail.ErrSuppressed) || sent != 0 {
		t.Errorf("Send() to a suppressed address = %v (sent %d), want ErrSuppressed", err, sent)
	}
}

func TestAddOnce(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	entry := suppressionpg.Entry{Email: "ada@example.com", Reason: suppressionpg.ReasonComplaint, Source: "resend"}
	until := time.Now().Add(10 * time.Minute)

	added, err := s.AddOnce(ctx, "resend:msg_1", until, entry)
	if err != nil || len(added) != 1 {
		t.Fatalf("AddOnce() = %+v, %v", added, err)
	}
	// An operator removes the address; a replay of the delivery must not
	// put it back.
	if _, err := s.Remove(ctx, added[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddOnce(ctx, "resend:msg_1", until, entry); !errors.Is(err, suppressionpg.ErrDuplicateDelivery) {
		t.Errorf("AddOnce() replayed error = %v, want ErrDuplicateDelivery", err)
	}
	if got, _ := s.Suppressed(ctx, []string{"ada@example.com"}); len(got) != 0 {
		t.Error("a replayed delivery suppressed the address again")
	}

	// A delivery with no entries is still remembered.
	if _, err := s.AddOnce(ctx, "resend:msg_2", until); err != nil {
		t.Errorf("AddOnce() without entries error = %v", err)
	}
	if _, err := s.AddOnce(ctx, "resend:msg_2", until); !errors.Is(err, suppressionpg.ErrDuplicateDelivery) {
		t.Errorf("AddOnce() replayed without entries error = %v", err)
	}

	// An expired key can be applied again.
	if _, err := s.AddOnce(ctx, "resend:msg_3", time.Now().Add(-time.Second), entry); err != nil {
		t.Fatal(err)
	}
	if added, err := s.AddOnce(ctx, "resend:msg_3", until, entry); err != nil || len(added) != 0 {
		t.Errorf("AddOnce() after the key expired = %+v, %v; want applied (address already listed)", added, err)
	}

	// A failed delivery leaves no key behind, so the provider's retry works.
	if _, err := s.AddOnce(ctx, "resend:msg_4", until, suppressionpg.Entry{Email: "bad"}); err == nil {
		t.Fatal("AddOnce() with an invalid entry error = nil")
	}
	if _, err := s.AddOnce(ctx, "resend:msg_4", until, entry); err != nil {
		t.Errorf("AddOnce() retried after a failure error = %v", err)
	}

	for _, key := range []string{"", strings.Repeat("k", 301)} {
		if _, err := s.AddOnce(ctx, key, until, entry); err == nil {
			t.Errorf("AddOnce(key of %d bytes) error = nil", len(key))
		}
	}

	// Concurrent deliveries of one key apply it once.
	var applied, duplicates atomic.Int32
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			_, err := s.AddOnce(ctx, "resend:msg_concurrent", until, suppressionpg.Entry{Email: "grace@example.com", Reason: suppressionpg.ReasonBounce, Source: "resend"})
			switch {
			case err == nil:
				applied.Add(1)
			case errors.Is(err, suppressionpg.ErrDuplicateDelivery):
				duplicates.Add(1)
			default:
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if applied.Load() != 1 || duplicates.Load() != 9 {
		t.Errorf("concurrent deliveries: %d applied, %d duplicates; want 1 and 9", applied.Load(), duplicates.Load())
	}
}

func TestListAndRemove(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	for i := range 5 {
		reason := suppressionpg.ReasonBounce
		if i%2 == 1 {
			reason = suppressionpg.ReasonComplaint
		}
		if _, err := s.Add(ctx, suppressionpg.Entry{Email: fmt.Sprintf("user%d@example.com", i), Reason: reason, Source: "resend"}); err != nil {
			t.Fatal(err)
		}
	}

	var emails []string
	cursor := ""
	for {
		page, err := s.List(ctx, suppressionpg.Filter{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, sup := range page.Suppressions {
			emails = append(emails, sup.Email)
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if want := []string{"user4@example.com", "user3@example.com", "user2@example.com", "user1@example.com", "user0@example.com"}; !slices.Equal(emails, want) {
		t.Errorf("paged List() = %v, want newest first %v", emails, want)
	}
	complaints, err := s.List(ctx, suppressionpg.Filter{Reason: suppressionpg.ReasonComplaint})
	if err != nil || len(complaints.Suppressions) != 2 {
		t.Errorf("List(complaints) = %+v, %v", complaints, err)
	}
	for _, bad := range []string{"x", "0", "-3"} {
		if _, err := s.List(ctx, suppressionpg.Filter{Cursor: bad}); !errors.Is(err, suppressionpg.ErrInvalidCursor) {
			t.Errorf("List(cursor %q) error = %v, want ErrInvalidCursor", bad, err)
		}
	}

	removed, err := s.Remove(ctx, complaints.Suppressions[0].ID)
	if err != nil || removed.Email != "user3@example.com" {
		t.Errorf("Remove() = %+v, %v", removed, err)
	}
	if got, _ := s.Suppressed(ctx, []string{"user3@example.com"}); len(got) != 0 {
		t.Error("a removed address is still suppressed")
	}
	if _, err := s.Remove(ctx, removed.ID); !errors.Is(err, suppressionpg.ErrNotFound) {
		t.Errorf("Remove() twice error = %v, want ErrNotFound", err)
	}
	if _, err := suppressionpg.NewStore(nil); err == nil {
		t.Error("NewStore(nil) error = nil")
	}
}
