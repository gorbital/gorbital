package passkey

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var (
	appleAppIDPattern     = regexp.MustCompile(`^[A-Z0-9]{10}\.[A-Za-z0-9][A-Za-z0-9.-]*$`)
	androidPackagePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)+$`)
)

// ParseAppleAppIDs parses comma-separated TEAMID.bundle.id identifiers, such
// as ABCDE12345.com.example.app.
func ParseAppleAppIDs(spec string) ([]string, error) {
	var ids []string
	for id := range strings.SplitSeq(spec, ",") {
		if id = strings.TrimSpace(id); id == "" {
			continue
		}
		if !appleAppIDPattern.MatchString(id) {
			return nil, fmt.Errorf("%w: Apple app ID %q must be TEAMID.bundle.id, such as ABCDE12345.com.example.app", ErrInvalidConfig, id)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// ParseAndroidApps parses comma-separated package=fingerprint entries, such
// as com.example.app=SHA256:AB:CD:…; several fingerprints of one app are
// joined with +. Fingerprints are 32 bytes in hex, with or without colons
// and the SHA256: prefix.
func ParseAndroidApps(spec string) ([]AndroidApp, error) {
	var apps []AndroidApp
	for entry := range strings.SplitSeq(spec, ",") {
		if entry = strings.TrimSpace(entry); entry == "" {
			continue
		}
		pkg, fps, ok := strings.Cut(entry, "=")
		pkg = strings.TrimSpace(pkg)
		if !ok || !androidPackagePattern.MatchString(pkg) {
			return nil, fmt.Errorf("%w: Android app %q must be package.name=SHA256:FINGERPRINT", ErrInvalidConfig, entry)
		}
		app := AndroidApp{Package: pkg}
		for fp := range strings.SplitSeq(fps, "+") {
			parsed, err := parseFingerprint(fp)
			if err != nil {
				return nil, fmt.Errorf("%w: Android app %s: %v", ErrInvalidConfig, pkg, err) //nolint:errorlint // detail only
			}
			app.Fingerprints = append(app.Fingerprints, parsed)
		}
		apps = append(apps, app)
	}
	return apps, nil
}

func parseFingerprint(s string) ([sha256.Size]byte, error) {
	var fp [sha256.Size]byte
	s = strings.TrimSpace(s)
	if len(s) >= 7 && strings.EqualFold(s[:7], "SHA256:") {
		s = s[7:]
	}
	raw, err := hex.DecodeString(strings.ReplaceAll(s, ":", ""))
	if err != nil || len(raw) != sha256.Size {
		return fp, fmt.Errorf("fingerprint %q must be the certificate's SHA-256, 32 bytes in hex", s)
	}
	copy(fp[:], raw)
	return fp, nil
}

// FormatFingerprint writes fp as AB:CD:…, the form assetlinks.json uses.
func FormatFingerprint(fp [sha256.Size]byte) string {
	parts := make([]string, len(fp))
	for i, b := range fp {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return strings.Join(parts, ":")
}

// AppleAppSiteAssociation returns the apple-app-site-association document
// that lets iOS apps use passkeys for the relying party, or nil without apps.
// Serve it at /.well-known/apple-app-site-association with the JSON content
// type.
func AppleAppSiteAssociation(appIDs []string) []byte {
	if len(appIDs) == 0 {
		return nil
	}
	doc := map[string]any{"webcredentials": map[string]any{"apps": appIDs}}
	b, _ := json.MarshalIndent(doc, "", "  ") // plain maps and strings always marshal
	return b
}

// AssetLinks returns the Digital Asset Links statements that let Android
// apps use passkeys for the relying party, or nil without apps. Serve it at
// /.well-known/assetlinks.json.
func AssetLinks(apps []AndroidApp) []byte {
	if len(apps) == 0 {
		return nil
	}
	type target struct {
		Namespace    string   `json:"namespace"`
		PackageName  string   `json:"package_name"`
		Fingerprints []string `json:"sha256_cert_fingerprints"`
	}
	type statement struct {
		Relation []string `json:"relation"`
		Target   target   `json:"target"`
	}
	statements := make([]statement, len(apps))
	for i, app := range apps {
		fps := make([]string, len(app.Fingerprints))
		for j, fp := range app.Fingerprints {
			fps[j] = FormatFingerprint(fp)
		}
		statements[i] = statement{
			Relation: []string{"delegate_permission/common.get_login_creds", "delegate_permission/common.handle_all_urls"},
			Target:   target{Namespace: "android_app", PackageName: app.Package, Fingerprints: fps},
		}
	}
	b, _ := json.MarshalIndent(statements, "", "  ") // plain structs always marshal
	return b
}
