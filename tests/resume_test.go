//go:build integration

package tests

import (
	"testing"

	"github.com/jg-backend-challenge/wallet/internal/domain"
	"github.com/jg-backend-challenge/wallet/internal/ports"
	"github.com/jg-backend-challenge/wallet/internal/wagering"
)

// TestReferenceResume covers 3.3: REFUND before BET parks, resolves on the
// reference commit, and exhausts into REFERENCE_NOT_FOUND; another instance
// resumes released work.
func TestReferenceResume(t *testing.T) {
	svc := openService(t)
	w := openWallet(t, svc, "1000.00")
	betExt := "bet-" + uid(t)

	refund, err := svc.Submit(wagering.SubmitInput{
		ProviderID: "provider-a", ExternalID: "refund-" + uid(t), IdempotencyKey: "key-" + uid(t),
		PlayerID: w.PlayerID, WalletID: w.ID, RoundID: "round-1", GameID: "game-1",
		Kind: domain.KindRefund, AmountText: "80.00", Currency: "BRL",
		ReferenceExternal: betExt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if refund.Outcome != wagering.OutcomePending {
		t.Fatalf("refund outcome = %s, want pending", refund.Outcome)
	}
	if refund.Balance != nil {
		t.Fatal("pending must omit balance")
	}

	bet := submitBET(t, svc, w, betExt, "key-"+uid(t), "80.00")
	if bet.Outcome != wagering.OutcomeProcessed {
		t.Fatalf("bet outcome = %s", bet.Outcome)
	}
	// Immediate re-evaluation should have processed the parked refund.
	var refundState string
	err = svc.DB.Transact(func(db ports.DB) error {
		rec, err := db.Tx().Get(refund.TransactionID)
		if err != nil || rec == nil {
			return err
		}
		refundState = rec.State
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if refundState != "PROCESSED" {
		t.Fatalf("refund state = %s, want PROCESSED", refundState)
	}

	// Exhaustion: park then force the attempt budget, another instance runs it.
	stuck, err := svc.Submit(wagering.SubmitInput{
		ProviderID: "provider-a", ExternalID: "refund-" + uid(t), IdempotencyKey: "key-" + uid(t),
		PlayerID: w.PlayerID, WalletID: w.ID, RoundID: "round-1", GameID: "game-1",
		Kind: domain.KindRefund, AmountText: "5.00", Currency: "BRL",
		ReferenceExternal: "never-" + uid(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	err = svc.DB.Transact(func(db ports.DB) error {
		return db.Aux().TouchWork(stuck.TransactionID, wagering.MaxRefAttempts-1, 0,
			svc.Clock.Now(), true)
	})
	if err != nil {
		t.Fatal(err)
	}
	n, err := svc.ProcessDueWork("instance-b", 10)
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatal("expected instance-b to settle the stuck refund")
	}
	err = svc.DB.Transact(func(db ports.DB) error {
		rec, err := db.Tx().Get(stuck.TransactionID)
		if err != nil || rec == nil {
			return err
		}
		if rec.State != "REJECTED" || rec.FailureCode == nil || *rec.FailureCode != wagering.CodeRefNotFound {
			t.Errorf("stuck = %s %v", rec.State, rec.FailureCode)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
