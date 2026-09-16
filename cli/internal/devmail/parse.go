package devmail

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"strings"
	"unicode"
)

var (
	// codePattern finds verification codes: 6 to 8 digits, or groups such
	// as ABCD-EFGH, standing alone.
	codePattern = regexp.MustCompile(`\b(\d{6,8}|[A-Z0-9]{4,6}-[A-Z0-9]{4,6})\b`)
	linkPattern = regexp.MustCompile(`https?://[^\s"'<>)\]]+`)
	hrefPattern = regexp.MustCompile(`(?is)<a[^>]+href\s*=\s*["']([^"']+)["'][^>]*>(.*?)</a>`)
	tagPattern  = regexp.MustCompile(`(?s)<[^>]*>`)
)

// Parse reads a message: the summary for the list and the detail with
// its bodies, links and attachments.
func Parse(raw []byte) (Summary, Detail, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		// Not a parseable message: keep it whole as text.
		text := string(raw)
		sum := Summary{Subject: "(unparseable message)", Snippet: snippet(text), HasText: true, Codes: []string{}}
		return sum, Detail{Summary: sum, Headers: map[string]string{}, Text: text, Links: []Link{}, Attachment: []Attachment{}}, nil
	}
	dec := new(mime.WordDecoder)
	decodeHeader := func(v string) string {
		if d, err := dec.DecodeHeader(v); err == nil {
			return d
		}
		return v
	}
	sum := Summary{Subject: decodeHeader(msg.Header.Get("Subject")), Codes: []string{}}
	if t, err := msg.Header.Date(); err == nil {
		sum.Time = t.UTC()
	}
	sum.From = firstAddress(msg.Header.Get("From"))
	sum.To = addresses(msg.Header.Get("To"))
	sum.Category = msg.Header.Get("X-Category")
	if sum.Category == "" {
		sum.Category = msg.Header.Get("X-Tag-Category")
	}
	detail := Detail{Headers: map[string]string{}, Links: []Link{}, Attachment: []Attachment{}, MessageID: strings.Trim(msg.Header.Get("Message-ID"), "<>")}
	detail.ReplyTo = addresses(msg.Header.Get("Reply-To"))
	detail.CC = addresses(msg.Header.Get("Cc"))
	for k, v := range msg.Header {
		detail.Headers[k] = decodeHeader(strings.Join(v, ", "))
	}
	body, _ := io.ReadAll(io.LimitReader(msg.Body, MaxMessageBytes))
	walk(msg.Header.Get("Content-Type"), msg.Header.Get("Content-Transfer-Encoding"), msg.Header.Get("Content-Disposition"), body, &detail)
	sum.HasText, sum.HasHTML = detail.Text != "", detail.HTML != ""
	sum.Attachments = len(detail.Attachment)
	plain := detail.Text
	if plain == "" {
		plain = htmlToText(detail.HTML)
	}
	sum.Snippet = snippet(plain)
	sum.Codes = codes(plain, sum.Subject)
	detail.Links = links(detail.HTML, plain)
	detail.Summary = sum
	return sum, detail, nil
}

// walk descends into MIME parts, filling the bodies and attachments.
func walk(contentType, encoding, disposition string, body []byte, d *Detail) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = "text/plain"
	}
	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return
		}
		mr := multipart.NewReader(bytes.NewReader(body), boundary)
		for {
			p, err := mr.NextPart()
			if err != nil {
				return
			}
			part, _ := io.ReadAll(io.LimitReader(p, MaxMessageBytes))
			walk(p.Header.Get("Content-Type"), p.Header.Get("Content-Transfer-Encoding"), p.Header.Get("Content-Disposition"), part, d)
		}
	}
	decoded := decode(body, encoding)
	dispType, dispParams, _ := mime.ParseMediaType(disposition)
	name := dispParams["filename"]
	if name == "" {
		name = params["name"]
	}
	if dispType == "attachment" || (name != "" && !strings.HasPrefix(mediaType, "text/")) {
		d.Attachment = append(d.Attachment, Attachment{Name: name, ContentType: mediaType, Size: len(decoded)})
		return
	}
	switch mediaType {
	case "text/plain":
		if d.Text == "" {
			d.Text = string(decoded)
		}
	case "text/html":
		if d.HTML == "" {
			d.HTML = string(decoded)
		}
	default:
		if name != "" {
			d.Attachment = append(d.Attachment, Attachment{Name: name, ContentType: mediaType, Size: len(decoded)})
		}
	}
}

func decode(body []byte, encoding string) []byte {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "quoted-printable":
		out, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(body)))
		if err == nil {
			return out
		}
	case "base64":
		out, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, bytes.NewReader(bytes.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return -1
			}
			return r
		}, body))))
		if err == nil {
			return out
		}
	}
	return body
}

func firstAddress(v string) Address {
	list := addresses(v)
	if len(list) == 0 {
		return Address{}
	}
	return list[0]
}

func addresses(v string) []Address {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parsed, err := mail.ParseAddressList(v)
	if err != nil {
		return []Address{{Email: strings.TrimSpace(v)}}
	}
	out := make([]Address, 0, len(parsed))
	for _, a := range parsed {
		out = append(out, Address{Name: a.Name, Email: a.Address})
	}
	return out
}

// htmlToText strips tags for a snippet and code search.
func htmlToText(h string) string {
	h = regexp.MustCompile(`(?is)<(style|script)[^>]*>.*?</(style|script)>`).ReplaceAllString(h, " ")
	h = regexp.MustCompile(`(?i)<br\s*/?>|</p>|</div>|</tr>|</li>|</h[1-6]>`).ReplaceAllString(h, "\n")
	h = tagPattern.ReplaceAllString(h, " ")
	return strings.TrimSpace(strings.NewReplacer("&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'").Replace(h))
}

func snippet(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 160 {
		text = text[:160] + "…"
	}
	return text
}

// codes finds verification codes in the text and subject, deduplicated
// in order of appearance.
func codes(text, subject string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, m := range codePattern.FindAllString(subject+"\n"+text, -1) {
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

// links lists the URLs in the HTML (with their link text) and the text.
func links(h, text string) []Link {
	seen := map[string]bool{}
	out := []Link{}
	for _, m := range hrefPattern.FindAllStringSubmatch(h, -1) {
		u := strings.TrimSpace(m[1])
		if !strings.HasPrefix(u, "http") || seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, Link{URL: u, Text: strings.TrimSpace(htmlToText(m[2]))})
	}
	for _, u := range linkPattern.FindAllString(text, -1) {
		u = strings.TrimRight(u, ".,;:")
		if !seen[u] {
			seen[u] = true
			out = append(out, Link{URL: u})
		}
	}
	return out
}
