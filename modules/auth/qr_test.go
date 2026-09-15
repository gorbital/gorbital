package auth_test

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"strings"
	"testing"

	authlib "apistock.dev/modules/auth"
)

func TestTOTPQRCode(t *testing.T) {
	url, err := authlib.TOTPQRCode(authlib.TOTPURI("acme-api", "ada@example.com", authlib.NewTOTPSecret()))
	data, ok := strings.CutPrefix(url, "data:image/png;base64,")
	if err != nil || !ok {
		t.Fatalf("TOTPQRCode() = %.40q…, %v; want a PNG data URL", url, err)
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil || img.Bounds().Dx() < 150 || img.Bounds().Dx() != img.Bounds().Dy() {
		t.Errorf("QR image = %v, %v; want a square PNG large enough to scan", img.Bounds(), err)
	}
}
