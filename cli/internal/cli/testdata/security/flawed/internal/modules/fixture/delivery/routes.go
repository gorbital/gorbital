// Package delivery is the flawed fixture's HTTP adapter.
package delivery

// credential-in-route-path: the invitation token is in the URL.
const acceptInvitationPath = "/v1/invitations/{token}/accept"

// Paths returns the module's routes.
func Paths() []string { return []string{acceptInvitationPath, "/v1/invitations/{invitationId}"} }
