package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type jwksFixture struct {
	server  *httptest.Server
	key     *rsa.PrivateKey
	kid     string
	issuer  string
	counter int
}

func newJWKSFixture(t *testing.T, issuer string) *jwksFixture {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &jwksFixture{key: key, kid: "test-key", issuer: issuer}
	mux := http.NewServeMux()
	mux.HandleFunc("/certs", func(w http.ResponseWriter, _ *http.Request) {
		b64 := func(n *big.Int) string { return base64.RawURLEncoding.EncodeToString(n.Bytes()) }
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{
			"kid": f.kid, "kty": "RSA", "n": b64(f.key.N), "e": b64(big.NewInt(int64(f.key.E))),
		}}})
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *jwksFixture) validator() *OIDCValidator {
	return &OIDCValidator{Issuer: f.issuer, JWKSURL: f.server.URL + "/certs"}
}

func (f *jwksFixture) mint(t *testing.T, mutate func(jwt.MapClaims)) string {
	t.Helper()
	f.counter++
	claims := jwt.MapClaims{
		"iss": f.issuer, "iat": time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix(),
		"realm_access": map[string]any{"roles": []string{"provider"}},
		"provider_id":  "provider-a",
	}
	if mutate != nil {
		mutate(claims)
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = f.kid
	signed, err := token.SignedString(f.key)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func TestOIDCValidator(t *testing.T) {
	ctx := context.Background()
	issuer := "http://keycloak:8080/realms/wallet"

	t.Run("valid provider token", func(t *testing.T) {
		f := newJWKSFixture(t, issuer)
		id, err := f.validator().ValidateToken(ctx, f.mint(t, nil))
		if err != nil {
			t.Fatalf("validate: %v", err)
		}
		if id.ProviderID != "provider-a" || !id.HasRole("provider") {
			t.Fatalf("identity = %+v", id)
		}
	})

	t.Run("expired token rejected", func(t *testing.T) {
		f := newJWKSFixture(t, issuer)
		tok := f.mint(t, func(c jwt.MapClaims) {
			c["exp"] = time.Now().Add(-time.Minute).Unix()
		})
		if _, err := f.validator().ValidateToken(ctx, tok); err == nil {
			t.Fatal("expired token accepted")
		}
	})

	t.Run("leeway honors near-boundary exp", func(t *testing.T) {
		f := newJWKSFixture(t, issuer)
		// Expired 10s ago: inside the 30s leeway, still accepted.
		tok := f.mint(t, func(c jwt.MapClaims) {
			c["exp"] = time.Now().Add(-10 * time.Second).Unix()
		})
		if _, err := f.validator().ValidateToken(ctx, tok); err != nil {
			t.Fatalf("leeway token rejected: %v", err)
		}
	})

	t.Run("future nbf beyond leeway rejected", func(t *testing.T) {
		f := newJWKSFixture(t, issuer)
		tok := f.mint(t, func(c jwt.MapClaims) {
			c["nbf"] = time.Now().Add(time.Hour).Unix()
		})
		if _, err := f.validator().ValidateToken(ctx, tok); err == nil {
			t.Fatal("future nbf accepted")
		}
	})

	t.Run("host alias accepted, foreign realm rejected", func(t *testing.T) {
		f := newJWKSFixture(t, issuer)
		alias := f.mint(t, func(c jwt.MapClaims) { c["iss"] = "http://localhost:8081/realms/wallet" })
		if _, err := f.validator().ValidateToken(ctx, alias); err != nil {
			t.Fatalf("host alias rejected: %v", err)
		}
		foreign := f.mint(t, func(c jwt.MapClaims) { c["iss"] = "http://keycloak:8080/realms/other" })
		if _, err := f.validator().ValidateToken(ctx, foreign); err == nil {
			t.Fatal("foreign realm accepted")
		}
	})

	t.Run("unknown signing key rejected", func(t *testing.T) {
		f := newJWKSFixture(t, issuer)
		other, _ := rsa.GenerateKey(rand.Reader, 2048)
		claims := jwt.MapClaims{"iss": issuer, "exp": time.Now().Add(time.Minute).Unix()}
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		token.Header["kid"] = "unknown"
		signed, _ := token.SignedString(other)
		if _, err := f.validator().ValidateToken(ctx, signed); err == nil {
			t.Fatal("unknown key accepted")
		}
	})

	t.Run("provider without provider_id rejected", func(t *testing.T) {
		f := newJWKSFixture(t, issuer)
		tok := f.mint(t, func(c jwt.MapClaims) { delete(c, "provider_id") })
		if _, err := f.validator().ValidateToken(ctx, tok); err == nil {
			t.Fatal("scopeless provider accepted")
		}
	})

	t.Run("audience enforced when configured", func(t *testing.T) {
		f := newJWKSFixture(t, issuer)
		v := f.validator()
		v.Audience = "wallet-api"
		withAud := f.mint(t, func(c jwt.MapClaims) { c["aud"] = "wallet-api" })
		if _, err := v.ValidateToken(ctx, withAud); err != nil {
			t.Fatalf("matching aud rejected: %v", err)
		}
		withoutAud := f.mint(t, nil)
		if _, err := v.ValidateToken(ctx, withoutAud); err == nil {
			t.Fatal("missing aud accepted while configured")
		}
	})
}
