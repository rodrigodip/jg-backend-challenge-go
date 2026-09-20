//go:build integration

// Package tests holds integration tests that require real infrastructure.
// Run with: go test -tags integration ./tests/ -timeout 5m
// against Postgres at $TEST_DATABASE_URL
// (default postgres://app:appsecret@localhost:5433/wallet?sslmode=disable).
package tests

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"

	"github.com/jg-backend-challenge/wallet/internal/adapters/postgres"
	"github.com/jg-backend-challenge/wallet/internal/domain"
	"github.com/jg-backend-challenge/wallet/internal/ports"
	"github.com/jg-backend-challenge/wallet/internal/wagering"
)

func testDSN() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://app:appsecret@localhost:5433/wallet?sslmode=disable"
}

func openService(t *testing.T) *wagering.Service {
	t.Helper()
	store, err := postgres.Open(testDSN())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	return &wagering.Service{DB: store, Clock: ports.SystemClock{}}
}

func uid(t *testing.T) string {
	t.Helper()
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b[:])
}

// Note: tests intentionally leave rows behind. Ledger entries are
// append-only by schema trigger, so cleanup would violate the very
// invariant under test; every test uses unique players/keys.

func openWallet(t *testing.T, svc *wagering.Service, initial string) *ports.WalletRecord {
	t.Helper()
	w, err := svc.OpenWallet(wagering.NewUUID(), "BRL", initial)
	if err != nil {
		t.Fatalf("open wallet: %v", err)
	}
	return w
}

func submitBET(t *testing.T, svc *wagering.Service, w *ports.WalletRecord, ext, key, amount string) *wagering.SubmitResult {
	t.Helper()
	r, err := svc.Submit(wagering.SubmitInput{
		ProviderID: "provider-a", ExternalID: ext, IdempotencyKey: key,
		PlayerID: w.PlayerID, WalletID: w.ID, RoundID: "round-1", GameID: "game-1",
		Kind: domain.KindBet, AmountText: amount, Currency: "BRL",
	})
	if err != nil {
		t.Fatalf("submit bet: %v", err)
	}
	return r
}
