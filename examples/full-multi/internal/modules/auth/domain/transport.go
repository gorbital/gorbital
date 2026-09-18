// Package domain holds the auth module's rules that don't depend on storage
// or HTTP. Passwords, sessions and codes are handled by the gorbital auth
// module (ADR-0024, ADR-0038). It imports only the standard library.
package domain

// How a new session's token reaches the client.
const (
	// TransportCookie sets the token in an HttpOnly session cookie, for
	// browsers. Scripts can't read it.
	TransportCookie = "cookie"
	// TransportBearer returns the token in the response body, for mobile and
	// other native clients, which send it as "Authorization: Bearer <token>".
	TransportBearer = "bearer"
)
