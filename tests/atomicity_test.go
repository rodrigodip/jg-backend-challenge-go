//go:build integration

package tests

import (
	"testing"

	"github.com/jg-backend-challenge/wallet/internal/ports"
	"github.com/jg-backend-challenge/wallet/internal/wagering"
)

// TestAtomicCommit covers 3.1: state+balance+ledger+inbox+outbox commit
// together; a positive opening writes wallet + OPENING + ledger + 2 events.
func TestAtomicCommit(t *testing.T) {
	svc := openService(t)
	w := openWallet(t, svc, "1000.00")
	if w.Version != 1 {
		t.Fatalf("version = %d", w.Version)
	}

	var ledgerN int
	var outboxN int
	err := svc.DB.Transact(func(db ports.DB) error {
		page, err := db.Ledger().Page(w.ID, 0, 100)
		if err != nil {
			return err
		}
		ledgerN = len(page)
		pending, err := db.Aux().ListUnpublished(w.ID)
		if err != nil {
			return err
		}
		outboxN = len(pending)
		for _, o := range pending {
			if o.EventType != wagering.EventProcessed && o.EventType != wagering.EventBalance {
				t.Errorf("unexpected opening event %s", o.EventType)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if ledgerN != 1 {
		t.Fatalf("opening ledger entries = %d, want 1", ledgerN)
	}
	if outboxN != 2 {
		t.Fatalf("opening outbox events = %d, want 2", outboxN)
	}

	r := submitBET(t, svc, w, "ext-bet-1-"+uid(t), "key-1-"+uid(t), "80.00")
	if r.Outcome != wagering.OutcomeProcessed {
		t.Fatalf("outcome = %s", r.Outcome)
	}
	if r.Balance == nil || r.Balance.String() != "920.00" {
		t.Fatalf("balance = %v", r.Balance)
	}
}
