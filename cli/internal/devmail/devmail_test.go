package devmail

import (
	"context"
	"net/smtp"
	"strings"
	"testing"
	"time"
)

const sample = "From: \"Acme\" <no-reply@acme.test>\r\nTo: Ada <ada@example.com>, bob@example.com\r\nSubject: =?utf-8?q?Verify_your_email?=\r\nDate: Tue, 16 Sep 2026 10:00:00 +0000\r\nMessage-ID: <abc@acme.test>\r\nX-Category: auth_verification\r\nMIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=\"b1\"\r\n\r\n--b1\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\nYour code is 483920.\r\nOpen https://acme.test/verify?code=3D483920 to continue.\r\n--b1\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p>Your code is <b>483920</b>.</p><p><a href=\"https://acme.test/verify?code=483920\">Verify now</a></p>\r\n--b1--\r\n"

func TestParse(t *testing.T) {
	sum, detail, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if sum.Subject != "Verify your email" || sum.From.Email != "no-reply@acme.test" || sum.From.Name != "Acme" || len(sum.To) != 2 || sum.To[0].Name != "Ada" ||
		!sum.HasHTML || !sum.HasText || sum.Category != "auth_verification" || !sum.Time.Equal(time.Date(2026, 9, 16, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("summary = %+v", sum)
	}
	if len(sum.Codes) != 1 || sum.Codes[0] != "483920" {
		t.Errorf("codes = %v", sum.Codes)
	}
	if !strings.Contains(detail.Text, "Your code is 483920.") || !strings.Contains(detail.HTML, "<b>483920</b>") || detail.MessageID != "abc@acme.test" {
		t.Errorf("detail = %+v", detail)
	}
	if len(detail.Links) != 1 || detail.Links[0].URL != "https://acme.test/verify?code=483920" || detail.Links[0].Text != "Verify now" {
		t.Errorf("links = %+v", detail.Links)
	}
	if !strings.HasPrefix(sum.Snippet, "Your code is 483920.") {
		t.Errorf("snippet = %q", sum.Snippet)
	}

	// An attachment and a base64 body.
	withFile := "Subject: report\r\nContent-Type: multipart/mixed; boundary=\"m\"\r\n\r\n--m\r\nContent-Type: text/plain\r\nContent-Transfer-Encoding: base64\r\n\r\naGVsbG8gd29ybGQ=\r\n--m\r\nContent-Type: application/pdf; name=\"r.pdf\"\r\nContent-Disposition: attachment; filename=\"r.pdf\"\r\nContent-Transfer-Encoding: base64\r\n\r\nJVBERi0=\r\n--m--\r\n"
	sum, detail, err = Parse([]byte(withFile))
	if err != nil {
		t.Fatal(err)
	}
	if detail.Text != "hello world" || sum.Attachments != 1 || detail.Attachment[0].Name != "r.pdf" || detail.Attachment[0].Size != 5 {
		t.Errorf("attachment message = %+v %+v", sum, detail.Attachment)
	}
	// Garbage is kept as text.
	if sum, _, err := Parse([]byte("not a message")); err != nil || !sum.HasText {
		t.Errorf("garbage = %+v, %v", sum, err)
	}
}

func TestServerAndStore(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(store)
	if err := srv.Listen("0.0.0.0:0"); err == nil {
		t.Error("a non-loopback address was accepted")
	}
	if err := srv.Listen("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	got, stop := store.Subscribe()
	defer stop()

	// The app's sender uses net/smtp without TLS or authentication.
	if err := smtp.SendMail(srv.Addr(), nil, "no-reply@acme.test", []string{"ada@example.com", "bob@example.com"}, []byte(sample)); err != nil {
		t.Fatalf("SendMail() error = %v", err)
	}
	select {
	case m := <-got:
		if m.Subject != "Verify your email" || m.ID == "" {
			t.Errorf("subscribed = %+v", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no message delivered")
	}
	list, total := store.List("", 10)
	if total != 1 || len(list) != 1 || list[0].Read {
		t.Fatalf("list = %+v, %d", list, total)
	}
	d, err := store.Get(list[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Envelope.From != "no-reply@acme.test" || len(d.Envelope.To) != 2 || !d.Read || d.SourceBytes == 0 || !strings.Contains(d.Text, "483920") {
		t.Errorf("detail = %+v", d)
	}
	if list, _ := store.List("verify", 10); len(list) != 1 {
		t.Errorf("search = %+v", list)
	}
	if list, _ := store.List("nothing here", 10); len(list) != 0 {
		t.Errorf("search miss = %+v", list)
	}
	src, err := store.Source(d.ID)
	if err != nil || string(src) != sample {
		t.Errorf("source = %q, %v", src, err)
	}

	// The store reopens with its messages, then deletes.
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if again.Count() != 1 {
		t.Errorf("reopened count = %d", again.Count())
	}
	if _, err := again.Add(context.Background(), "a@b.test", []string{"c@d.test"}, []byte("Subject: two\r\n\r\nhello")); err != nil {
		t.Fatal(err)
	}
	if again.Count() != 2 {
		t.Errorf("count after add = %d", again.Count())
	}
	if err := again.Delete(d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := again.Get(d.ID); err != ErrNotFound {
		t.Errorf("Get(deleted) = %v", err)
	}
	if err := again.Clear(); err != nil || again.Count() != 0 {
		t.Errorf("Clear() = %v, count %d", err, again.Count())
	}
	if _, err := again.Get("../../etc/passwd"); err != ErrNotFound {
		t.Errorf("traversal = %v", err)
	}

	// The inbox is bounded.
	small, _ := Open(t.TempDir())
	small.max = 3
	for i := range 5 {
		if _, err := small.Add(context.Background(), "a@b.test", []string{"c@d.test"}, []byte("Subject: n"+string(rune('0'+i))+"\r\n\r\nx")); err != nil {
			t.Fatal(err)
		}
	}
	if list, total := small.List("", 10); total != 3 || list[0].Subject != "n4" || list[2].Subject != "n2" {
		t.Errorf("bounded list = %+v", list)
	}
}
