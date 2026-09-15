package auth

import (
	"encoding/base64"
	"fmt"

	"rsc.io/qr"
)

// TOTPQRCode returns a PNG image of uri (from [TOTPURI]) as a data URL,
// ready for an <img> tag, so an authenticator app can scan it before a client
// renders its own (ADR-0043).
func TOTPQRCode(uri string) (string, error) {
	code, err := qr.Encode(uri, qr.M)
	if err != nil {
		return "", fmt.Errorf("auth: encode QR code: %w", err)
	}
	code.Scale = 6
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(code.PNG()), nil
}
