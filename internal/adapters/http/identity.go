// Package httpapi holds the gin HTTP handlers and routes (block 4).
//
// Authentication is enforced on every business route through the Validator
// interface: production binds the OIDC/Keycloak validator , while
// contract tests bind StaticValidator. Identities carry the provider scope
// and roles; handlers map them to 401/403 without leaking data.
package httpapi

import (
	"context"
	"errors"
)

// Role names from the Keycloak realm (ADR-0010).
const (
	RoleProvider = "provider"
	RoleInternal = "internal"
)

// ErrUnauthorized marks missing, invalid or expired credentials.
// Validators return it (possibly wrapped); the auth middleware maps it to
// 401 without a financial effect.
var ErrUnauthorized = errors.New("unauthorized")

// Identity is the verified caller scope: the providerId claim plus roles.
type Identity struct {
	ProviderID string
	Roles      map[string]bool
}

// HasRole reports whether the identity carries the given realm role.
func (i Identity) HasRole(role string) bool { return i.Roles[role] }

// Validator verifies a bearer token and returns the caller identity.
type Validator interface {
	ValidateToken(ctx context.Context, token string) (Identity, error)
}

// StaticValidator maps fixed tokens to identities. It serves contract tests
// and local development; production binds the OIDC validator. Unknown tokens
// fail closed with ErrUnauthorized.
type StaticValidator struct {
	Tokens map[string]Identity
}

// ValidateToken resolves the token in the static map.
func (s StaticValidator) ValidateToken(_ context.Context, token string) (Identity, error) {
	if id, ok := s.Tokens[token]; ok {
		return id, nil
	}
	return Identity{}, ErrUnauthorized
}
