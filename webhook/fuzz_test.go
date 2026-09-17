package webhook_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"gorbital.dev/webhook"
)

// FuzzStandardVerify sends malformed signature headers, IDs, timestamps and
// bodies: the verifier never panics, returns only its own errors, and
// accepts only a request carrying a v1 signature of its own ID, timestamp
// and body, made with one of its secrets, within the tolerance.
func FuzzStandardVerify(f *testing.F) {
	const id, ts, body = "msg_1", "1789552800", `{"type":"payment.succeeded"}`
	f.Add(id, ts, sign(keyA, id, ts, body), []byte(body), int64(0))
	f.Add(id, ts, sign(keyB, id, ts, body)+" "+sign(keyA, id, ts, body), []byte(body), int64(299))
	f.Add(id, ts, "v1,AAAA v2,x v1,not-base64 ,,, v1,", []byte(body), int64(301))
	f.Add("", "", "", []byte(nil), int64(0))
	f.Add("msg.1", "-1", "v1,", []byte("{}"), int64(-301))
	f.Add("msg 1", "+1789552800", "v1,,v1,=", []byte("x"), int64(1<<62))
	f.Add(strings.Repeat("a", 300), "99999999999999999999", "v1,"+strings.Repeat("A", 1000), []byte("x"), int64(0))
	now := unix(ts)
	f.Fuzz(func(t *testing.T, id, timestamp, signature string, body []byte, offset int64) {
		v, err := webhook.NewStandard(webhook.StandardConfig{
			Secrets: []string{standardSecret(keyA), standardSecret(keyB)},
			Now:     fixed(now.Add(time.Duration(offset) * time.Second)),
		})
		if err != nil {
			t.Fatal(err)
		}
		err = v.Verify(context.Background(), standardHeaders("webhook-", id, timestamp, signature), body)
		if err != nil {
			if !errors.Is(err, webhook.ErrInvalidSignature) {
				t.Fatalf("Verify() error = %v, want ErrInvalidSignature", err)
			}
			return
		}
		// Accepted: check every rule independently.
		if id == "" || len(id) > webhook.MaxIDLength || strings.ContainsAny(id, ". \t\r\n") {
			t.Fatalf("Verify() accepted webhook-id %q", id)
		}
		seconds, perr := strconv.ParseInt(timestamp, 10, 64)
		signedAt, current := time.Unix(seconds, 0), now.Add(time.Duration(offset)*time.Second)
		if perr != nil || seconds <= 0 || signedAt.Before(current.Add(-5*time.Minute)) || signedAt.After(current.Add(5*time.Minute)) {
			t.Fatalf("Verify() accepted timestamp %q at %v", timestamp, current)
		}
		signedWith := func(key []byte) bool {
			mac := hmac.New(sha256.New, key)
			mac.Write([]byte(id + "." + timestamp + "."))
			mac.Write(body)
			want := mac.Sum(nil)
			for _, entry := range strings.Fields(signature) {
				enc, ok := strings.CutPrefix(entry, "v1,")
				if got, derr := base64.StdEncoding.DecodeString(enc); ok && derr == nil && bytes.Equal(got, want) {
					return true
				}
			}
			return false
		}
		if !signedWith(keyA) && !signedWith(keyB) {
			t.Fatalf("Verify() accepted signature %q without a valid entry", signature)
		}
	})
}

// FuzzDecodeStandardSecret checks the secret decoder never panics, and that
// a secret it accepts is long enough and never quoted in errors.
func FuzzDecodeStandardSecret(f *testing.F) {
	for _, s := range []string{"", "whsec_", standardSecret(keyA), "whsec_not base64!", "whsec_c2hvcnQ=", base64.StdEncoding.EncodeToString(keyB), "whsec_whsec_" + base64.StdEncoding.EncodeToString(keyA)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, secret string) {
		key, err := webhook.DecodeStandardSecret(secret)
		if err != nil {
			if enc := strings.TrimPrefix(secret, "whsec_"); len(enc) > 8 && strings.Contains(err.Error(), enc) {
				t.Fatalf("DecodeStandardSecret() error quotes the secret: %v", err)
			}
			return
		}
		if len(key) < webhook.MinSecretBytes {
			t.Fatalf("DecodeStandardSecret(%q) = %d bytes", secret, len(key))
		}
	})
}
