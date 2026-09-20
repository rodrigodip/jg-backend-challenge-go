//go:build integration

package tests

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	httpapi "github.com/jg-backend-challenge/wallet/internal/adapters/http"
	"github.com/jg-backend-challenge/wallet/internal/wagering"
)

const keycloakIssuer = "http://localhost:8081/realms/wallet"

// keycloakToken fetches a client_credentials token for the given client.
func keycloakToken(t *testing.T, clientID, secret string) string {
	t.Helper()
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {clientID},
		"client_secret": {secret},
	}
	resp, err := http.PostForm(keycloakIssuer+"/protocol/openid-connect/token", form)
	if err != nil {
		t.Fatalf("token request: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("token status %d: %s", resp.StatusCode, raw)
	}
	var decoded struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded.AccessToken
}

// tokenExp reads the exp claim without verifying the signature.
func tokenExp(t *testing.T, token string) time.Time {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatal("malformed token")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatal(err)
	}
	return time.Unix(claims.Exp, 0)
}

func oidcTestServer(t *testing.T) (*httpClient, *wagering.Service) {
	t.Helper()
	svc := openService(t)
	svc.NewID = wagering.NewUUID
	validator := &httpapi.OIDCValidator{
		Issuer:  keycloakIssuer,
		JWKSURL: keycloakIssuer + "/protocol/openid-connect/certs",
	}
	srv := httptest.NewServer(httpapi.NewEngine(httpapi.NewHandler(svc), validator))
	t.Cleanup(srv.Close)
	return &httpClient{t: t, base: srv.URL}, svc
}

func oidcTxBody(provider, ext, walletID, playerID string) map[string]any {
	return map[string]any{
		"providerId": provider, "externalTransactionId": ext,
		"playerId": playerID, "walletId": walletID,
		"roundId": "round-1", "gameId": "game-1",
		"kind": "BET", "amount": "5.00", "currency": "BRL",
	}
}

func TestKeycloakIsolation(t *testing.T) {
	c, _ := oidcTestServer(t)
	tokenA := keycloakToken(t, "provider-a", "provider-a-secret")
	tokenB := keycloakToken(t, "provider-b", "provider-b-secret")
	tokenInternal := keycloakToken(t, "internal-service", "internal-secret")

	// Open a wallet as internal.
	playerID := wagering.NewUUID()
	code, body, _ := c.do("POST", "/wallets", tokenInternal, nil, map[string]any{
		"playerId": playerID, "currency": "BRL", "initialAmount": "100.00",
	})
	if code != 201 {
		t.Fatalf("open wallet = %d %v", code, body)
	}
	walletID, _ := body["walletId"].(string)

	// Provider-a submits a BET -> 201.
	ext := "bet-" + uid(t)
	code, body, _ = c.do("POST", "/wagering/transactions", tokenA,
		map[string]string{"Idempotency-Key": "key-" + uid(t)},
		oidcTxBody("provider-a", ext, walletID, playerID))
	if code != 201 {
		t.Fatalf("provider-a submit = %d %v", code, body)
	}
	txA, _ := body["transactionId"].(string)

	// Provider-b reads provider-a's transaction -> 403 without data.
	if code, body, _ := c.do("GET", "/wagering/transactions/"+txA, tokenB, nil, nil); code != 403 {
		t.Fatalf("cross-provider read = %d %v, want 403", code, body)
	} else if _, ok := body["transactionId"]; ok {
		t.Fatalf("403 leaked data: %v", body)
	}

	// Same external id under another provider is an independent operation
	// (uniqueness is per provider): it processes instead of replaying.
	code, body, _ = c.do("POST", "/wagering/transactions", tokenB,
		map[string]string{"Idempotency-Key": "key-" + uid(t)},
		oidcTxBody("provider-b", ext, walletID, playerID))
	if code != 201 {
		t.Fatalf("provider-b own-scope submit = %d %v, want 201", code, body)
	}
	if body["transactionId"] == txA {
		t.Fatalf("cross-provider scope collision: %v", body)
	}

	// Provider-b sending provider-a's identity in the body -> 403, no effect.
	if code, body, _ := c.do("POST", "/wagering/transactions", tokenB,
		map[string]string{"Idempotency-Key": "key-" + uid(t)},
		oidcTxBody("provider-a", "bet-"+uid(t), walletID, playerID)); code != 403 {
		t.Fatalf("divergent provider = %d %v, want 403", code, body)
	}

	// Internal reads anyone's transaction but cannot submit.
	if code, _, _ := c.do("GET", "/wagering/transactions/"+txA, tokenInternal, nil, nil); code != 200 {
		t.Fatalf("internal read = %d, want 200", code)
	}
	if code, _, _ := c.do("POST", "/wagering/transactions", tokenInternal,
		map[string]string{"Idempotency-Key": "key-" + uid(t)},
		oidcTxBody("", "bet-"+uid(t), walletID, playerID)); code != 403 {
		t.Fatalf("internal submit = %d, want 403", code)
	}

	// Garbage token -> 401.
	if code, _, _ := c.do("GET", "/wagering/transactions/"+txA, "garbage", nil, nil); code != 401 {
		t.Fatalf("garbage token = %d, want 401", code)
	}
}

func TestKeycloakExpiredToken(t *testing.T) {
	c, _ := oidcTestServer(t)

	// test-short-lived issues 20s tokens: wait past exp plus the 30s clock
	// leeway, then every call must 401 with no financial effect and no data.
	token := keycloakToken(t, "test-short-lived", "test-secret")
	if wait := time.Until(tokenExp(t, token)) + 32*time.Second; wait > 0 {
		t.Logf("waiting %s for token expiry", wait.Round(time.Second))
		time.Sleep(wait)
	}
	if code, _, _ := c.do("GET", "/health/live", token, nil, nil); code != 200 {
		t.Fatalf("health must stay public, got %d", code)
	}
	if code, body, _ := c.do("GET", "/wallets/"+wagering.NewUUID(), token, nil, nil); code != 401 {
		t.Fatalf("expired token = %d %v, want 401", code, body)
	}
}
