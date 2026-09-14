package smtp

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/textproto"
	"strings"
	"time"

	"apistock.dev/mail"
)

// buildMessage formats m as an RFC 5322 message with CRLF line endings. m
// must be valid, so no header can contain a line break.
func buildMessage(m mail.Message, now time.Time) ([]byte, error) {
	var b bytes.Buffer
	header := func(name, value string) {
		b.WriteString(name + ": " + value + "\r\n")
	}
	header("From", m.From.String())
	header("To", joinAddresses(m.To))
	if len(m.ReplyTo) > 0 {
		header("Reply-To", joinAddresses(m.ReplyTo))
	}
	header("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	header("Date", now.UTC().Format(time.RFC1123Z))
	header("Message-ID", messageID(m))
	header("MIME-Version", "1.0")

	if m.Text != "" && m.HTML != "" {
		var body bytes.Buffer
		parts := multipart.NewWriter(&body)
		header("Content-Type", mime.FormatMediaType("multipart/alternative", map[string]string{"boundary": parts.Boundary()}))
		b.WriteString("\r\n")
		for _, part := range []struct{ contentType, content string }{
			{"text/plain; charset=utf-8", m.Text},
			{"text/html; charset=utf-8", m.HTML},
		} {
			w, err := parts.CreatePart(textproto.MIMEHeader{
				"Content-Type":              {part.contentType},
				"Content-Transfer-Encoding": {"quoted-printable"},
			})
			if err != nil {
				return nil, err
			}
			if err := writeQuotedPrintable(w, part.content); err != nil {
				return nil, err
			}
		}
		if err := parts.Close(); err != nil {
			return nil, err
		}
		b.Write(body.Bytes())
		return b.Bytes(), nil
	}

	contentType, content := "text/plain; charset=utf-8", m.Text
	if m.Text == "" {
		contentType, content = "text/html; charset=utf-8", m.HTML
	}
	header("Content-Type", contentType)
	header("Content-Transfer-Encoding", "quoted-printable")
	b.WriteString("\r\n")
	if err := writeQuotedPrintable(&b, content); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func writeQuotedPrintable(w interface{ Write([]byte) (int, error) }, content string) error {
	qp := quotedprintable.NewWriter(w)
	if _, err := qp.Write([]byte(content)); err != nil {
		return err
	}
	return qp.Close()
}

func joinAddresses(addrs []mail.Address) string {
	formatted := make([]string, len(addrs))
	for i, a := range addrs {
		formatted[i] = a.String()
	}
	return strings.Join(formatted, ", ")
}

// messageID derives the Message-ID from the idempotency key when there is
// one, so a retried job sends the same ID and receivers can drop duplicates.
func messageID(m mail.Message) string {
	var id string
	if m.IdempotencyKey != "" {
		sum := sha256.Sum256([]byte(m.IdempotencyKey))
		id = hex.EncodeToString(sum[:16])
	} else {
		id = rand.Text()
	}
	_, domain, _ := strings.Cut(m.From.Email, "@")
	return "<" + id + "@" + domain + ">"
}
