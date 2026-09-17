package domain

// RateLimiter describes one of the app's rate limiters (ADR-0070).
type RateLimiter struct {
	// Name is the limiter's name, part of every key, such as auth_login.
	Name string
	// Keys says what a key is: an address, an email address, a client
	// network, an actor ID.
	Keys string
	// Description says what the limiter protects.
	Description string
}
