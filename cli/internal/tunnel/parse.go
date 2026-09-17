package tunnel

import (
	"strings"
)

// quickDomain is the domain of Cloudflare's quick tunnels.
const quickDomain = ".trycloudflare.com"

// ParseQuickURL finds the public URL of a quick tunnel in one line of
// cloudflared's output: https://<name>.trycloudflare.com, wherever it is on
// the line (cloudflared draws it inside a box). It depends on nothing else
// in the line's wording. The API host cloudflared asks for the tunnel,
// api.trycloudflare.com, is not a tunnel URL. The URL is returned in lower
// case, without a path.
func ParseQuickURL(line string) (string, bool) {
	rest := line
	for {
		i := indexFold(rest, "https://")
		if i < 0 {
			return "", false
		}
		rest = rest[i+len("https://"):]
		end := 0
		for end < len(rest) && isHostByte(rest[end]) {
			end++
		}
		host := strings.ToLower(rest[:end])
		// A host followed by a port or user info isn't the tunnel's URL.
		if end < len(rest) && (rest[end] == ':' || rest[end] == '@') {
			continue
		}
		if quickHost(host) {
			return "https://" + host, true
		}
	}
}

// quickHost reports whether host is exactly one DNS label under
// trycloudflare.com, other than api.
func quickHost(host string) bool {
	label, ok := strings.CutSuffix(host, quickDomain)
	if !ok || label == "api" || !validLabel(label) {
		return false
	}
	return true
}

// validLabel reports whether s is a DNS label: 1 to 63 lower-case letters,
// digits and hyphens, not starting or ending with a hyphen.
func validLabel(s string) bool {
	if len(s) == 0 || len(s) > 63 || s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

func isHostByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '.'
}

// indexFold is strings.Index ignoring ASCII case of substr, which must be
// lower case.
func indexFold(s, substr string) int {
	n := len(substr)
	for i := 0; i+n <= len(s); i++ {
		if strings.EqualFold(s[i:i+n], substr) {
			return i
		}
	}
	return -1
}

// connectedLine reports a line saying cloudflared registered a connection
// to Cloudflare's edge: the tunnel serves requests from then on.
func connectedLine(line string) bool {
	return indexFold(line, "registered tunnel connection") >= 0
}

// levelOf returns cloudflared's level for a line of its default output,
// "2026-09-17T10:00:00Z ERR message": the second field when it is a level,
// else "".
func levelOf(line string) string {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return ""
	}
	switch l := strings.ToUpper(fields[1]); l {
	case "DBG", "INF", "WRN", "ERR", "FTL":
		return l
	}
	return ""
}

// messageOf returns the line without its time and level, for people.
func messageOf(line string) string {
	fields := strings.Fields(line)
	if len(fields) >= 2 && levelOf(line) != "" {
		i := strings.Index(line, fields[1]) + len(fields[1])
		return strings.TrimSpace(line[i:])
	}
	return strings.TrimSpace(line)
}
