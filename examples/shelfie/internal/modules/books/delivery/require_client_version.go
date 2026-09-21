package delivery

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"gorbital.dev/httpx"
)

// docs:start require-client-version

// minClientVersion is the oldest Shelfie app the books routes answer. Below
// it the mobile app sends statuses the module no longer accepts, so it is
// told to update rather than given data it will mishandle.
var minClientVersion = ClientVersion{Major: 2}

// RequireClientVersion is middleware for the books module: routes.go adds it
// to the books group with gorbital.Use. It runs before the sign-in check and
// the guards, so for anonymous callers too.
func RequireClientVersion(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		version, problem := checkRequireClientVersion(r)
		if problem != nil {
			// A refused request stops here: next never runs.
			httpx.WriteProblem(w, r, problem)
			return
		}
		if version != (ClientVersion{}) {
			r = r.WithContext(withClientVersion(r.Context(), version))
		}
		// docs:start capture
		// The access log already wraps w, so this is its record, not a
		// second one: the status below is the one it will log.
		cw := httpx.Capture(w)
		next.ServeHTTP(cw, r)
		if cw.Status() >= http.StatusInternalServerError && version != (ClientVersion{}) {
			// Which build saw the error, on the line someone will read.
			httpx.AccessNoteFrom(r.Context()).Add(slog.String("app_version", version.String()))
		}
		// docs:end capture
	})
}

// checkRequireClientVersion is the middleware's rule. It returns the version
// the caller says it is, and why the request is refused, as a problem with
// its status and code, or nil to let it through. The web app and curl send
// no version, and are let through without one.
func checkRequireClientVersion(r *http.Request) (ClientVersion, *httpx.Problem) {
	header := r.Header.Get("X-App-Version")
	if header == "" {
		return ClientVersion{}, nil
	}
	version, ok := parseClientVersion(header)
	if !ok {
		return ClientVersion{}, httpx.NewProblem(http.StatusBadRequest, "invalid_app_version",
			"X-App-Version is a version such as 2.4.0")
	}
	if version.Before(minClientVersion) {
		return ClientVersion{}, httpx.NewProblem(http.StatusUpgradeRequired, "app_outdated",
			"update the Shelfie app to "+minClientVersion.String()+" or later to continue")
	}
	return version, nil
}

// docs:end require-client-version

// docs:start client-version

// A ClientVersion is the version of the app that sent a request, from its
// X-App-Version header.
type ClientVersion struct{ Major, Minor, Patch int }

// String returns the version as the app sends it, such as "2.4.0".
func (v ClientVersion) String() string {
	return strconv.Itoa(v.Major) + "." + strconv.Itoa(v.Minor) + "." + strconv.Itoa(v.Patch)
}

// Before reports whether v is older than o. Comparing the numbers, not the
// strings: "10.0.0" is newer than "9.9.0".
func (v ClientVersion) Before(o ClientVersion) bool {
	switch {
	case v.Major != o.Major:
		return v.Major < o.Major
	case v.Minor != o.Minor:
		return v.Minor < o.Minor
	default:
		return v.Patch < o.Patch
	}
}

// parseClientVersion reads "major.minor.patch"; ok is false for anything
// else, including a version with a suffix such as "2.4.0-beta1".
func parseClientVersion(s string) (ClientVersion, bool) {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return ClientVersion{}, false
	}
	var v ClientVersion
	for i, into := range []*int{&v.Major, &v.Minor, &v.Patch} {
		n, err := strconv.Atoi(parts[i])
		if err != nil || n < 0 || len(parts[i]) > 4 {
			return ClientVersion{}, false
		}
		*into = n
	}
	return v, true
}

// clientVersionKey is the books module's own context key: a type declared
// here, so nothing outside this package can read the value or overwrite it.
type clientVersionKey struct{}

// withClientVersion returns ctx carrying v.
func withClientVersion(ctx context.Context, v ClientVersion) context.Context {
	return context.WithValue(ctx, clientVersionKey{}, v)
}

// ClientVersionFrom returns the version RequireClientVersion read from
// X-App-Version, and whether the caller sent one.
func ClientVersionFrom(ctx context.Context) (ClientVersion, bool) {
	v, ok := ctx.Value(clientVersionKey{}).(ClientVersion)
	return v, ok
}

// docs:end client-version
