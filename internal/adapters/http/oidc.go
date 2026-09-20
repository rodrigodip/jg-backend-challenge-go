package httpapi

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ClockSkewLeeway is the accepted clock difference for exp/nbf (D8).
const ClockSkewLeeway = 30 * time.Second

// jwksCacheTTL bounds how long signing keys are reused without refetch.
const jwksCacheTTL = 5 * time.Minute

// OIDCValidator verifies Keycloak access tokens locally via JWKS: RS256
// signature, iss, exp/nbf with 30s leeway, realm roles and the provider_id
// claim. No per-request introspection call is made.
//
// Audience: enforced only when the validator is configured with one AND the
// token carries aud. The wallet realm issues no aud claim (verified against
// live tokens), so verification rests on iss + signature + roles.
type OIDCValidator struct {
	Issuer   string
	Audience string
	JWKSURL  string
	Leeway   time.Duration
	HTTP     *http.Client

	mu      sync.Mutex
	keys    map[string]*rsa.PublicKey
	fetched time.Time
}

func (v *OIDCValidator) leeway() time.Duration {
	if v.Leeway != 0 {
		return v.Leeway
	}
	return ClockSkewLeeway
}

func (v *OIDCValidator) client() *http.Client {
	if v.HTTP != nil {
		return v.HTTP
	}
	return &http.Client{Timeout: 5 * time.Second}
}

// ValidateToken verifies the bearer token and returns the caller identity.
// Any verification failure maps to ErrUnauthorized: 401 with no data and no
// financial effect.
func (v *OIDCValidator) ValidateToken(ctx context.Context, token string) (Identity, error) {
	claims := jwt.MapClaims{}
	keyFunc := func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != jwt.SigningMethodRS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method %q", t.Method.Alg())
		}
		kid, _ := t.Header["kid"].(string)
		return v.keyFor(ctx, kid)
	}
	parsed, err := jwt.ParseWithClaims(token, claims,
		keyFunc,
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithLeeway(v.leeway()))
	if err != nil || !parsed.Valid {
		return Identity{}, ErrUnauthorized
	}
	now := time.Now()
	iss, err := claims.GetIssuer()
	if err != nil || iss != v.Issuer {
		return Identity{}, ErrUnauthorized
	}
	if exp, err := claims.GetExpirationTime(); err != nil || exp == nil || now.After(exp.Time.Add(v.leeway())) {
		return Identity{}, ErrUnauthorized
	}
	if nbf, err := claims.GetNotBefore(); err != nil || (nbf != nil && now.Before(nbf.Time.Add(-v.leeway()))) {
		return Identity{}, ErrUnauthorized
	}
	if v.Audience != "" {
		if aud, err := claims.GetAudience(); err != nil || len(aud) == 0 || !audContains(aud, v.Audience) {
			return Identity{}, ErrUnauthorized
		}
	}

	roles := map[string]bool{}
	if ra, ok := claims["realm_access"].(map[string]any); ok {
		if list, ok := ra["roles"].([]any); ok {
			for _, r := range list {
				if s, ok := r.(string); ok {
					roles[s] = true
				}
			}
		}
	}
	providerID, _ := claims["provider_id"].(string)
	if roles[RoleProvider] && providerID == "" {
		// A provider identity without scope cannot be authorized: fail
		// closed as unauthenticated rather than guessing the scope.
		return Identity{}, ErrUnauthorized
	}
	return Identity{ProviderID: providerID, Roles: roles}, nil
}

func audContains(aud jwt.ClaimStrings, want string) bool {
	for _, a := range aud {
		if a == want {
			return true
		}
	}
	return false
}

// keyFor resolves the RSA key for kid, refetching once on unknown kids and
// after the cache TTL.
func (v *OIDCValidator) keyFor(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if k, ok := v.keys[kid]; ok && time.Since(v.fetched) < jwksCacheTTL {
		return k, nil
	}
	if err := v.refreshLocked(ctx); err != nil {
		return nil, err
	}
	if k, ok := v.keys[kid]; ok {
		return k, nil
	}
	return nil, fmt.Errorf("unknown key id %q", kid)
}

type jwksKey struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func (v *OIDCValidator) refreshLocked(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.JWKSURL, nil)
	if err != nil {
		return err
	}
	resp, err := v.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jwks status %d", resp.StatusCode)
	}
	var doc struct {
		Keys []jwksKey `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return err
	}
	keys := map[string]*rsa.PublicKey{}
	for _, k := range doc.Keys {
		if k.Kty != "RSA" || k.Kid == "" {
			continue
		}
		nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			continue
		}
		eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			continue
		}
		e := 0
		for _, b := range eBytes {
			e = e<<8 + int(b)
		}
		keys[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: e}
	}
	if len(keys) == 0 {
		return fmt.Errorf("no RSA keys in JWKS")
	}
	v.keys = keys
	v.fetched = time.Now()
	return nil
}
