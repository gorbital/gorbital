package portal

import (
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// newProxy returns the reverse proxy behind AppPrefix. The app's address
// comes from the supervisor on every request, since APP_ADDR can change
// between restarts. Requests to /_dev/ and /ops/ get the dev console token
// when they carry no Authorization of their own; the portal's own cookie
// and header never reach the app.
func (s *Server) newProxy() http.Handler {
	s.transport = http.DefaultTransport.(*http.Transport).Clone()
	proxy := &httputil.ReverseProxy{
		Transport: s.transport,
		Rewrite: func(pr *httputil.ProxyRequest) {
			target := s.appURL()
			pr.Out.URL.Scheme = target.Scheme
			pr.Out.URL.Host = target.Host
			pr.Out.Host = target.Host // the dev console checks Host (ADR-0065)
			if pr.Out.URL.Path == "" {
				pr.Out.URL.Path = "/"
			}
			pr.Out.Header.Del(MutationHeader)
			stripCookie(pr.Out, CookieName)
			// A script that authenticated with the portal token as a bearer
			// header keeps it for the portal, not for the app.
			if scheme, token, ok := strings.Cut(pr.Out.Header.Get("Authorization"), " "); ok && strings.EqualFold(scheme, "Bearer") && s.tokenMatches(strings.TrimSpace(token)) {
				pr.Out.Header.Del("Authorization")
			}
			// The console token opens /_dev/ and, as the development operator
			// (ADR-0066), /ops/; a caller's own Authorization wins.
			if s.cfg.ConsoleToken != "" && pr.Out.Header.Get("Authorization") == "" && (strings.HasPrefix(pr.Out.URL.Path, "/_dev") || strings.HasPrefix(pr.Out.URL.Path, "/ops/")) {
				pr.Out.Header.Set("Authorization", "Bearer "+s.cfg.ConsoleToken)
			}
		},
		// Streams (Server-Sent Events) must reach the UI as they happen.
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			writeProblem(w, http.StatusBadGateway, "app_unavailable", "the app isn't answering at "+s.appURL().String()+": "+err.Error())
		},
	}
	return proxy
}

// appURL returns where the app listens, as reachable from this machine: a
// wildcard host becomes 127.0.0.1.
func (s *Server) appURL() *url.URL {
	addr := s.cfg.Supervisor.Status().Addr
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host, port = addr, "80"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return &url.URL{Scheme: "http", Host: net.JoinHostPort(host, port)}
}

// stripCookie removes the named cookie from r, keeping the others.
func stripCookie(r *http.Request, name string) {
	cookies := r.Cookies()
	r.Header.Del("Cookie")
	for _, c := range cookies {
		if c.Name != name {
			r.AddCookie(c)
		}
	}
}
