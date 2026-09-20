//go:build integration

package tests

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httpapi "github.com/jg-backend-challenge/wallet/internal/adapters/http"
	"github.com/jg-backend-challenge/wallet/internal/wagering"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// scrapeMetrics renders the default registry through the same promhttp
// handler the admin /metrics endpoint serves.
func scrapeMetrics(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(promhttp.Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return string(raw)
}

func TestObservability(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	svc := openService(t)
	svc.NewID = wagering.NewUUID
	engine := httpapi.NewEngine(httpapi.NewHandler(svc), testValidator(), logger)
	srv := httptest.NewServer(engine)
	defer srv.Close()
	c := &httpClient{t: t, base: srv.URL}

	playerID := wagering.NewUUID()
	code, body, _ := c.do("POST", "/wallets", "token-internal", nil, map[string]any{
		"playerId": playerID, "currency": "BRL", "initialAmount": "100.00",
	})
	if code != 201 {
		t.Fatalf("open wallet = %d %v", code, body)
	}
	walletID, _ := body["walletId"].(string)

	submit := func(ext, key, amount string) (int, map[string]any) {
		code, body, _ := c.do("POST", "/wagering/transactions", "token-a",
			map[string]string{"Idempotency-Key": key}, map[string]any{
				"providerId": "provider-a", "externalTransactionId": ext,
				"playerId": playerID, "walletId": walletID,
				"roundId": "round-1", "gameId": "game-1",
				"kind": "BET", "amount": amount, "currency": "BRL",
			})
		return code, body
	}
	ext, key := "bet-"+uid(t), "key-"+uid(t)
	if code, _ := submit(ext, key, "10.00"); code != 201 {
		t.Fatalf("submit = %d, want 201", code)
	}
	if code, _ := submit(ext, key, "10.00"); code != 200 {
		t.Fatalf("replay = %d, want 200", code)
	}
	if code, _ := submit(ext, key, "11.00"); code != 409 {
		t.Fatalf("conflict = %d, want 409", code)
	}

	// Divergence wiring: the reporter hooked into wagering.Reconcile counts.
	httpapi.DivergenceMetrics{}.ReportDivergence(context.Background(), walletID, "1.00", "2.00", "-1.00", 3)

	exposition := scrapeMetrics(t)
	for _, family := range []string{
		`wallet_tx_results_total{outcome="processed"}`,
		`wallet_tx_results_total{outcome="replay"}`,
		`wallet_idempotent_replays_total`,
		`wallet_idempotency_conflicts_total`,
		`wallet_http_request_duration_seconds`,
		`ledger_divergences_total`,
	} {
		if !strings.Contains(exposition, family) {
			t.Errorf("metrics exposition missing %s", family)
		}
	}

	// Every request logs JSON with its correlation id; authenticated lines
	// also carry the provider scope.
	corrHeaders := map[string]string{
		"Idempotency-Key":  "key-" + uid(t),
		"X-Correlation-Id": "corr-obs-1",
	}
	if code, _, _ := c.do("POST", "/wagering/transactions", "token-a", corrHeaders, map[string]any{
		"providerId": "provider-a", "externalTransactionId": "bet-" + uid(t),
		"playerId": playerID, "walletId": walletID,
		"roundId": "round-1", "gameId": "game-1",
		"kind": "BET", "amount": "1.00", "currency": "BRL",
	}); code != 201 {
		t.Fatalf("correlated submit = %d, want 201", code)
	}
	lines := strings.TrimSpace(logs.String())
	foundCorr, foundProvider := false, false
	for _, line := range strings.Split(lines, "\n") {
		if strings.Contains(line, `"correlationId":"corr-obs-1"`) {
			foundCorr = true
		}
		if strings.Contains(line, `"providerId":"provider-a"`) {
			foundProvider = true
		}
		if strings.Contains(line, "access_token") || strings.Contains(line, "Authorization") {
			t.Errorf("log leaks credentials: %s", line)
		}
	}
	if !foundCorr {
		t.Error("no log line carries the request correlation id")
	}
	if !foundProvider {
		t.Error("no log line carries the provider scope")
	}
}
