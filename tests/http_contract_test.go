//go:build integration

package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	httpapi "github.com/jg-backend-challenge/wallet/internal/adapters/http"
	"github.com/jg-backend-challenge/wallet/internal/wagering"
)

// testIdentities mirrors the realm roles for provider-a, provider-b and the
// internal service. Task 4.2 replaces this static map with the OIDC
// validator; the contract assertions stay identical.
func testValidator() httpapi.StaticValidator {
	mk := func(provider string, roles ...string) httpapi.Identity {
		m := map[string]bool{}
		for _, r := range roles {
			m[r] = true
		}
		return httpapi.Identity{ProviderID: provider, Roles: m}
	}
	return httpapi.StaticValidator{Tokens: map[string]httpapi.Identity{
		"token-a":        mk("provider-a", "provider"),
		"token-b":        mk("provider-b", "provider"),
		"token-internal": mk("", "internal"),
	}}
}

type httpClient struct {
	t    *testing.T
	base string
}

func (h *httpClient) do(method, path, token string, headers map[string]string, body any) (int, map[string]any, http.Header) {
	h.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			h.t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, h.base+path, reader)
	if err != nil {
		h.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var decoded map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &decoded); err != nil {
			h.t.Fatalf("invalid json from %s %s: %v (%s)", method, path, err, raw)
		}
	}
	return resp.StatusCode, decoded, resp.Header
}

func TestHTTPContract(t *testing.T) {
	svc := openService(t)
	svc.NewID = wagering.NewUUID
	engine := httpapi.NewEngine(httpapi.NewHandler(svc), testValidator(), testLogger())
	srv := httptest.NewServer(engine)
	defer srv.Close()
	c := &httpClient{t: t, base: srv.URL}

	player := wagering.NewUUID()
	key := map[string]string{"Idempotency-Key": "key-" + uid(t)}

	// 401 without credentials, even on business routes; health stays public.
	if code, _, _ := c.do("GET", "/wallets/x", "", nil, nil); code != 401 {
		t.Fatalf("unauthenticated wallet read = %d, want 401", code)
	}
	if code, _, _ := c.do("GET", "/health/live", "", nil, nil); code != 200 {
		t.Fatalf("health/live = %d, want 200", code)
	}

	// Providers cannot touch wallet endpoints (internal role only).
	if code, body, _ := c.do("POST", "/wallets", "token-a", nil, map[string]any{
		"playerId": "p", "currency": "BRL", "initialAmount": "10.00",
	}); code != 403 || body["code"] != "PROVIDER_FORBIDDEN" {
		t.Fatalf("provider open wallet = %d %v, want 403 PROVIDER_FORBIDDEN", code, body)
	}

	// Open wallet (internal) -> 201.
	code, body, _ := c.do("POST", "/wallets", "token-internal", nil, map[string]any{
		"playerId": player, "currency": "BRL", "initialAmount": "100.00",
	})
	if code != 201 {
		t.Fatalf("open wallet = %d %v, want 201", code, body)
	}
	walletID, _ := body["walletId"].(string)
	if walletID == "" {
		t.Fatalf("open wallet missing walletId: %v", body)
	}
	if bal, _ := body["balance"].(map[string]any)["amount"].(string); bal != "100.00" {
		t.Fatalf("open wallet balance = %v, want 100.00", body["balance"])
	}

	// Duplicate wallet for the same player+currency -> 409.
	if code, body, _ := c.do("POST", "/wallets", "token-internal", nil, map[string]any{
		"playerId": player, "currency": "BRL", "initialAmount": "5.00",
	}); code != 409 {
		t.Fatalf("duplicate wallet = %d %v, want 409", code, body)
	}

	// Get wallet -> 200; unknown -> 404.
	if code, _, _ := c.do("GET", "/wallets/"+walletID, "token-internal", nil, nil); code != 200 {
		t.Fatalf("get wallet = %d, want 200", code)
	}
	if code, _, _ := c.do("GET", "/wallets/wallet-missing", "token-internal", nil, nil); code != 404 {
		t.Fatalf("get missing wallet = %d, want 404", code)
	}

	submit := func(token, ext, idemKey, kind, amount, ref string) (int, map[string]any, http.Header) {
		h := map[string]string{}
		if idemKey != "" {
			h["Idempotency-Key"] = idemKey
		}
		return c.do("POST", "/wagering/transactions", token, h, map[string]any{
			"providerId": "provider-a", "externalTransactionId": ext,
			"playerId": player, "walletId": walletID,
			"roundId": "round-1", "gameId": "game-1",
			"kind": kind, "amount": amount, "currency": "BRL",
			"referenceExternalTransactionId": ref,
		})
	}
	extBet, betKey := "bet-"+uid(t), "betkey-"+uid(t)

	// Missing Idempotency-Key -> 400, no financial effect.
	if code, body, _ := submit("token-a", extBet, "", "BET", "25.00", ""); code != 400 {
		t.Fatalf("missing key = %d %v, want 400", code, body)
	}

	// providerId diverging from the identity -> 403 with no data.
	if code, body, _ := c.do("POST", "/wagering/transactions", "token-a", key, map[string]any{
		"providerId": "provider-b", "externalTransactionId": "x",
		"playerId": player, "walletId": walletID, "kind": "BET",
		"amount": "1.00", "currency": "BRL",
	}); code != 403 || body["code"] != "PROVIDER_FORBIDDEN" {
		t.Fatalf("divergent provider = %d %v, want 403 PROVIDER_FORBIDDEN", code, body)
	}
	if _, ok := body["transactionId"]; ok {
		t.Fatalf("403 leaked transaction data: %v", body)
	}

	// First BET -> 201 with observed balance.
	code, body, hdr := submit("token-a", extBet, betKey, "BET", "25.00", "")
	if code != 201 {
		t.Fatalf("submit bet = %d %v, want 201", code, body)
	}
	txID, _ := body["transactionId"].(string)
	if bal, _ := body["balance"].(map[string]any)["amount"].(string); bal != "75.00" {
		t.Fatalf("bet balance = %v, want 75.00", body["balance"])
	}
	if body["idempotentReplay"] == true {
		t.Fatalf("first submit flagged as replay: %v", body)
	}
	if hdr.Get("X-Correlation-Id") == "" {
		t.Fatal("missing X-Correlation-Id response header")
	}
	// Correlation echo: inbound id flows back and reaches the service.
	code, _, hdr = c.do("POST", "/wagering/transactions", "token-a",
		map[string]string{"Idempotency-Key": "key-" + uid(t), "X-Correlation-Id": "corr-test-1"},
		map[string]any{
			"providerId": "provider-a", "externalTransactionId": "bet-" + uid(t),
			"playerId": player, "walletId": walletID,
			"roundId": "round-1", "gameId": "game-1",
			"kind": "BET", "amount": "1.00", "currency": "BRL",
		})
	if code != 201 || hdr.Get("X-Correlation-Id") != "corr-test-1" {
		t.Fatalf("correlation echo = %d corr=%q, want 201 corr-test-1", code, hdr.Get("X-Correlation-Id"))
	}

	// Replay: same key, same content -> 200, same tx, original balance.
	if code, replay, _ := submit("token-a", extBet, betKey, "BET", "25.00", ""); code != 200 {
		t.Fatalf("replay = %d %v, want 200", code, replay)
	} else {
		if replay["transactionId"] != txID || replay["idempotentReplay"] != true {
			t.Fatalf("replay body mismatch: %v", replay)
		}
		if bal, _ := replay["balance"].(map[string]any)["amount"].(string); bal != "75.00" {
			t.Fatalf("replay balance = %v, want original 75.00", replay["balance"])
		}
	}

	// Conflict: same key, divergent content -> 409.
	if code, body, _ := submit("token-a", extBet, betKey, "BET", "26.00", ""); code != 409 {
		t.Fatalf("conflict = %d %v, want 409", code, body)
	}

	// Correctable (unknown wallet) -> 400 without reserving the key: the
	// same key with the fixed wallet processes normally.
	fixKey := "fixkey-" + uid(t)
	if code, _, _ := c.do("POST", "/wagering/transactions", "token-a",
		map[string]string{"Idempotency-Key": fixKey}, map[string]any{
			"providerId": "provider-a", "externalTransactionId": "bet-" + uid(t),
			"playerId": player, "walletId": "wallet-missing",
			"roundId": "round-1", "gameId": "game-1",
			"kind": "BET", "amount": "5.00", "currency": "BRL",
		}); code != 400 {
		t.Fatalf("correctable = %d, want 400", code)
	}
	if code, _, _ := c.do("POST", "/wagering/transactions", "token-a",
		map[string]string{"Idempotency-Key": fixKey}, map[string]any{
			"providerId": "provider-a", "externalTransactionId": "bet-" + uid(t),
			"playerId": player, "walletId": walletID,
			"roundId": "round-1", "gameId": "game-1",
			"kind": "BET", "amount": "5.00", "currency": "BRL",
		}); code != 201 {
		t.Fatalf("key reuse after correctable = %d, want 201", code)
	}

	// Business rejection (insufficient balance) -> 422 with persisted body.
	if code, body, _ := submit("token-a", "bet-"+uid(t), "key-"+uid(t), "BET", "1000.00", ""); code != 422 {
		t.Fatalf("rejection = %d %v, want 422", code, body)
	} else {
		if body["status"] != "REJECTED" || body["failureCode"] != "INSUFFICIENT_BALANCE" {
			t.Fatalf("rejection body mismatch: %v", body)
		}
		if _, ok := body["transactionId"]; !ok {
			t.Fatalf("rejection missing transactionId: %v", body)
		}
		if _, ok := body["balance"]; !ok {
			t.Fatalf("rejection missing observed balance: %v", body)
		}
	}

	// REFUND before its BET parks PENDING -> 202 without balance.
	refExt := "bet-" + uid(t)
	code, pending, _ := submit("token-a", "refund-"+uid(t), "key-"+uid(t), "REFUND", "10.00", refExt)
	if code != 202 {
		t.Fatalf("pending refund = %d %v, want 202", code, pending)
	}
	if _, ok := pending["balance"]; ok {
		t.Fatalf("202 must omit balance: %v", pending)
	}
	pendingTx, _ := pending["transactionId"].(string)

	// The late BET processes and immediately re-evaluates the dependent.
	if code, _, _ := submit("token-a", refExt, "key-"+uid(t), "BET", "10.00", ""); code != 201 {
		t.Fatalf("late bet = %d, want 201", code)
	}
	if code, resolved, _ := c.do("GET", "/wagering/transactions/"+pendingTx, "token-a", nil, nil); code != 200 || resolved["status"] != "PROCESSED" {
		t.Fatalf("dependent after reference = %d %v, want 200 PROCESSED", code, resolved)
	}

	// Transaction query: owner reads, stranger gets 403 without data,
	// unknown id gets 404.
	if code, got, _ := c.do("GET", "/wagering/transactions/"+txID, "token-a", nil, nil); code != 200 || got["transactionId"] != txID {
		t.Fatalf("get tx = %d %v, want 200", code, got)
	}
	if code, body, _ := c.do("GET", "/wagering/transactions/"+txID, "token-b", nil, nil); code != 403 || body["code"] != "PROVIDER_FORBIDDEN" {
		t.Fatalf("cross-provider read = %d %v, want 403", code, body)
	} else if _, ok := body["transactionId"]; ok {
		t.Fatalf("403 leaked transaction data: %v", body)
	}
	if code, got, _ := c.do("GET", "/wagering/transactions/"+txID, "token-internal", nil, nil); code != 200 {
		t.Fatalf("internal read = %d %v, want 200", code, got)
	}
	if code, _, _ := c.do("GET", "/wagering/transactions/tx-missing", "token-a", nil, nil); code != 404 {
		t.Fatalf("get missing tx = %d, want 404", code)
	}

	// Provider-scoped read by external id (REQUISITOS §9 Leitura).
	if code, body, _ := c.do("GET", "/providers/provider-a/wagering/transactions/"+extBet, "token-a", nil, nil); code != 200 {
		t.Fatalf("provider-scoped read = %d %v, want 200", code, body)
	} else {
		if body["transactionId"] != txID || body["status"] != "PROCESSED" {
			t.Fatalf("provider-scoped read body mismatch: %v", body)
		}
		if bal, _ := body["balance"].(map[string]any)["amount"].(string); bal != "75.00" {
			t.Fatalf("provider-scoped read balance = %v, want 75.00", body["balance"])
		}
	}
	// Unauthenticated access to the provider-scoped route -> 401.
	if code, _, _ := c.do("GET", "/providers/provider-a/wagering/transactions/"+extBet, "", nil, nil); code != 401 {
		t.Fatalf("provider-scoped read unauthenticated = %d, want 401", code)
	}
	// Path providerId diverging from the identity -> 403 with no data.
	if code, body, _ := c.do("GET", "/providers/provider-b/wagering/transactions/"+extBet, "token-a", nil, nil); code != 403 || body["code"] != "PROVIDER_FORBIDDEN" {
		t.Fatalf("provider-scoped divergent path = %d %v, want 403 PROVIDER_FORBIDDEN", code, body)
	} else if _, ok := body["transactionId"]; ok {
		t.Fatalf("403 leaked transaction data: %v", body)
	}
	// External id of another provider (or unknown) under the own path -> 404.
	if code, _, _ := c.do("GET", "/providers/provider-a/wagering/transactions/ext-of-provider-b", "token-a", nil, nil); code != 404 {
		t.Fatalf("provider-scoped foreign external id = %d, want 404", code)
	}
	if code, _, _ := c.do("GET", "/providers/provider-a/wagering/transactions/unknown-ext-"+uid(t), "token-a", nil, nil); code != 404 {
		t.Fatalf("provider-scoped unknown external id = %d, want 404", code)
	}
	// Internal reads any provider's transaction by external id.
	if code, body, _ := c.do("GET", "/providers/provider-a/wagering/transactions/"+extBet, "token-internal", nil, nil); code != 200 || body["transactionId"] != txID {
		t.Fatalf("provider-scoped internal read = %d %v, want 200", code, body)
	}

	// Ledger page + invalid cursor.
	if code, page, _ := c.do("GET", "/wallets/"+walletID+"/ledger?limit=50", "token-internal", nil, nil); code != 200 {
		t.Fatalf("ledger = %d %v, want 200", code, page)
	} else if entries, _ := page["entries"].([]any); len(entries) == 0 {
		t.Fatalf("ledger empty: %v", page)
	}
	if code, _, _ := c.do("GET", "/wallets/"+walletID+"/ledger?cursor=!!!", "token-internal", nil, nil); code != 400 {
		t.Fatalf("bad cursor = %d, want 400", code)
	}

	// Reconciliation proves stored == calculated.
	if code, rec, _ := c.do("GET", "/wallets/"+walletID+"/reconciliation", "token-internal", nil, nil); code != 200 {
		t.Fatalf("reconcile = %d %v, want 200", code, rec)
	} else if rec["consistent"] != true {
		t.Fatalf("reconcile inconsistent: %v", rec)
	}

	fmt.Println("contract ok")
}
