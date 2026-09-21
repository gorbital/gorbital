// Package delivery is the near-miss fixture's HTTP adapter.
package delivery

// The invitation is identified in the path and proved in the body.
const acceptInvitationPath = "/v1/invitations/{invitationId}/accept"

// Paths returns the module's routes.
func Paths() []string { return []string{acceptInvitationPath, "/v1/auth/api-keys/{keyId}"} }
