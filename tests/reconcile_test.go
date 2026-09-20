//go:build integration

package tests

import (
	"testing"

	"github.com/jg-backend-challenge/wallet/internal/domain"
	"github.com/jg-backend-challenge/wallet/internal/wagering"
)

// TestReconcile covers 3.4: stored == calculated with checkedEntries, zero
// opening reconciles with 0 entries, cursor pagination is stable.
func TestReconcile(t *testing.T) {
	svc := openService(t)
	w := openWallet(t, svc, "1000.00")
	submitBET(t, svc, w, "ext-"+uid(t), "key-"+uid(t), "80.00")
	// WIN without reference credits directly.
	if _, err := svc.Submit(wagering.SubmitInput{
		ProviderID: "provider-a", ExternalID: "ext-" + uid(t), IdempotencyKey: "key-" + uid(t),
		PlayerID: w.PlayerID, WalletID: w.ID, RoundID: "round-1", GameID: "game-1",
		Kind: domain.KindWin, AmountText: "50.00", Currency: "BRL",
	}); err != nil {
		t.Fatal(err)
	}

	rec, err := svc.Reconcile(w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !rec.Consistent || rec.Difference.String() != "0.00" {
		t.Fatalf("reconcile = consistent %v diff %s", rec.Consistent, rec.Difference)
	}
	if rec.CheckedEntries != 3 {
		t.Fatalf("checkedEntries = %d, want 3", rec.CheckedEntries)
	}
	if rec.StoredBalance.String() != "970.00" {
		t.Fatalf("stored = %s", rec.StoredBalance)
	}

	zero := openWallet(t, svc, "0.00")
	zrec, err := svc.Reconcile(zero.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !zrec.Consistent || zrec.CheckedEntries != 0 {
		t.Fatalf("zero reconcile = %v %d", zrec.Consistent, zrec.CheckedEntries)
	}

	p1, err := svc.PageLedger(w.ID, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(p1.Entries) != 2 || p1.NextCursor == "" {
		t.Fatalf("page1 = %d entries cursor %q", len(p1.Entries), p1.NextCursor)
	}
	p2, err := svc.PageLedger(w.ID, p1.NextCursor, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(p2.Entries) != 1 || p2.NextCursor != "" {
		t.Fatalf("page2 = %d entries cursor %q", len(p2.Entries), p2.NextCursor)
	}
	if p1.Entries[0].WalletVersion >= p1.Entries[1].WalletVersion {
		t.Fatal("ledger order not stable")
	}
}
