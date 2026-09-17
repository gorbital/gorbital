package resend_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"gorbital.dev/config"
	"gorbital.dev/modules/mail/resend"
)

// fuzzKey is bounceSecret's key: bytes 1 to 24.
var fuzzKey = []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24}

func sign(key []byte, id, timestamp string, body []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + timestamp + "."))
	mac.Write(body)
	return mac.Sum(nil)
}

// FuzzVerifyWebhook sends malformed secrets, headers and bodies: the
// verifier never panics, and accepts only a request carrying a v1 signature
// of its own ID, timestamp and body within the tolerance.
func FuzzVerifyWebhook(f *testing.F) {
	f.Add(bounceSecret.Reveal(), bounceID, bounceTimestamp, bounceSignature, []byte(bounceBody), int64(0))
	f.Add(svixSecret.Reveal(), svixID, svixTimestamp, svixSignature, []byte(svixBody), int64(0))
	f.Add("AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcY", bounceID, bounceTimestamp, "v1,AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA= v2,whatever "+bounceSignature+" v1,not-base64", []byte(bounceBody), int64(299))
	f.Add(bounceSecret.Reveal(), bounceID, bounceTimestamp, bounceSignature, []byte(bounceBody), int64(301))
	f.Add("", "", "", "", []byte(nil), int64(0))
	f.Add("whsec_", "msg.1", "-1", "v1,", []byte("{}"), int64(-301))
	f.Add("whsec_not base64!", "msg_1", "2026-09-16T10:00:00Z", "v1", []byte("x"), int64(0))
	f.Add("whsec_c2hvcnQ=", "msg 1", "+1789552800", "v1,,v1,=", []byte("x"), int64(1<<62))
	f.Fuzz(func(t *testing.T, secret, id, timestamp, signature string, body []byte, offset int64) {
		now := unix(bounceTimestamp).Add(time.Duration(offset) * time.Second)
		err := resend.VerifyWebhook(config.NewSecret(secret), headers(id, timestamp, signature), body, now)
		key, keyErr := decodeKey(secret)
		if keyErr {
			if err == nil || resend.CheckWebhookSecret(config.NewSecret(secret)) == nil {
				t.Fatalf("VerifyWebhook(secret %q) error = %v; CheckWebhookSecret should refuse it too", secret, err)
			}
			return
		}
		if err != nil && !errors.Is(err, resend.ErrInvalidWebhook) {
			t.Fatalf("VerifyWebhook() error = %v, want ErrInvalidWebhook", err)
		}
		if err != nil {
			return
		}
		// Accepted: the headers are well formed, the timestamp is within the
		// tolerance and one v1 entry is the expected signature.
		if id == "" || len(id) > 255 || strings.ContainsAny(id, ". \t\r\n") {
			t.Fatalf("VerifyWebhook() accepted svix-id %q", id)
		}
		seconds, perr := strconv.ParseInt(timestamp, 10, 64)
		if perr != nil || seconds <= 0 {
			t.Fatalf("VerifyWebhook() accepted svix-timestamp %q", timestamp)
		}
		if d := time.Unix(seconds, 0).Sub(now); d < -resend.WebhookTolerance || d > resend.WebhookTolerance {
			t.Fatalf("VerifyWebhook() accepted timestamp %q, %v from now", timestamp, d)
		}
		expected := sign(key, id, timestamp, body)
		found := false
		for _, entry := range strings.Fields(signature) {
			if enc, ok := strings.CutPrefix(entry, "v1,"); ok {
				if sig, err := base64.StdEncoding.DecodeString(enc); err == nil && bytes.Equal(sig, expected) {
					found = true
				}
			}
		}
		if !found {
			t.Fatalf("VerifyWebhook() accepted signature header %q without the expected v1 signature", signature)
		}
	})
}

// decodeKey mirrors the documented secret format, "whsec_" (optional) then
// base64 of at least 16 bytes; it reports true for a secret to refuse.
func decodeKey(secret string) ([]byte, bool) {
	enc := strings.TrimPrefix(secret, "whsec_")
	key, err := base64.StdEncoding.DecodeString(enc)
	return key, enc == "" || err != nil || len(key) < 16
}

// FuzzVerifyWebhookSigned signs arbitrary deliveries correctly: they verify,
// also next to other signatures, and changing any byte of the body, the ID,
// the timestamp or the signature makes them fail.
func FuzzVerifyWebhookSigned(f *testing.F) {
	f.Add(bounceID, int64(0), []byte(bounceBody), "", uint(0), byte(1))
	f.Add(svixID, int64(-299), []byte(svixBody), "v1,AAAA v2,x", uint(17), byte(0x80))
	f.Add("m", int64(300), []byte{}, "v1,not-base64", uint(1000), byte(0xff))
	f.Fuzz(func(t *testing.T, id string, skew int64, body []byte, others string, at uint, flip byte) {
		if id == "" || len(id) > 255 || strings.ContainsAny(id, ". \t\r\n") {
			return // refused before the signature is checked
		}
		if skew < -300 || skew > 300 {
			skew %= 301
		}
		now := unix(bounceTimestamp)
		timestamp := strconv.FormatInt(now.Unix()+skew, 10)
		sig := "v1," + base64.StdEncoding.EncodeToString(sign(fuzzKey, id, timestamp, body))
		verify := func(id, timestamp, signature string, body []byte) error {
			return resend.VerifyWebhook(bounceSecret, headers(id, timestamp, signature), body, now)
		}
		if err := verify(id, timestamp, sig, body); err != nil {
			t.Fatalf("VerifyWebhook(correctly signed id %q, body %q) error = %v", id, body, err)
		}
		// headers() uses Set, which keeps the value as is: only whitespace
		// separates signature entries.
		if err := verify(id, timestamp, others+" "+sig, body); err != nil {
			t.Fatalf("VerifyWebhook(signatures %q) error = %v, want the valid one to count", others+" "+sig, err)
		}
		if flip == 0 {
			flip = 1
		}
		if len(body) > 0 {
			tampered := bytes.Clone(body)
			tampered[at%uint(len(body))] ^= flip
			if err := verify(id, timestamp, sig, tampered); !errors.Is(err, resend.ErrInvalidWebhook) {
				t.Fatalf("VerifyWebhook(body %q tampered to %q) error = %v", body, tampered, err)
			}
		}
		if err := verify(id, timestamp, sig, append(bytes.Clone(body), 'x')); !errors.Is(err, resend.ErrInvalidWebhook) {
			t.Fatalf("VerifyWebhook(body with an extra byte) error = %v", err)
		}
		tamperedID := []byte(id)
		tamperedID[at%uint(len(id))] ^= flip
		if err := verify(string(tamperedID), timestamp, sig, body); !errors.Is(err, resend.ErrInvalidWebhook) {
			t.Fatalf("VerifyWebhook(id %q tampered to %q) error = %v", id, tamperedID, err)
		}
		if err := verify(id, strconv.FormatInt(now.Unix()+skew+1, 10), sig, body); !errors.Is(err, resend.ErrInvalidWebhook) {
			t.Fatalf("VerifyWebhook(timestamp changed) error = %v", err)
		}
		raw, _ := base64.StdEncoding.DecodeString(sig[3:])
		raw[at%uint(len(raw))] ^= flip
		if err := verify(id, timestamp, "v1,"+base64.StdEncoding.EncodeToString(raw), body); !errors.Is(err, resend.ErrInvalidWebhook) {
			t.Fatalf("VerifyWebhook(signature tampered) error = %v", err)
		}
	})
}
