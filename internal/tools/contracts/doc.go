// Package contracts holds the test that compares the golden apps' public
// contracts with the frozen v0.1.0 fixtures in internal/contracts/v0.1.0: the
// OpenAPI documents of the built-in endpoints and the api/surface.json names
// (error codes, audit actions, permissions, roles, settings, jobs, flags),
// found in the golden apps' api/surface.json or, for the library's, in
// docs/reference.
// Additions pass; removals and breaking changes fail.
//
//	go test -C internal/tools/contracts ./...
package contracts
