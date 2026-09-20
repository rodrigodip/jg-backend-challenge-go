package domain

import (
	"errors"
	"testing"
)

func procTx(kind TransactionKind) *WagerTransaction {
	tx := RehydrateTransaction("t", kind, StateProcessed, MustMoneyFromMinor(2500, "BRL"), "p1", "w1")
	return tx
}

func TestMatrixAllowed(t *testing.T) {
	bet := procTx(KindBet)
	if err := ValidateReversalTarget(KindRefund, bet, false); err != nil {
		t.Fatalf("refund bet err = %v", err)
	}
	if err := ValidateReversalTarget(KindRollback, bet, false); err != nil {
		t.Fatalf("rollback bet err = %v", err)
	}
	win := procTx(KindWin)
	if err := ValidateReversalTarget(KindRollback, win, false); err != nil {
		t.Fatalf("rollback win err = %v", err)
	}
	ref := procTx(KindRefund)
	if err := ValidateReversalTarget(KindRollback, ref, false); err != nil {
		t.Fatalf("rollback refund err = %v", err)
	}
}

func TestMatrixDenied(t *testing.T) {
	cases := []struct {
		name string
		rev  TransactionKind
		tgt  TransactionKind
		want error
	}{
		{"refund win", KindRefund, KindWin, ErrInvalidReversal},
		{"refund refund", KindRefund, KindRefund, ErrInvalidReversal},
		{"refund rollback", KindRefund, KindRollback, ErrInvalidReversal},
		{"refund loss", KindRefund, KindLoss, ErrInvalidReversal},
		{"refund opening", KindRefund, KindOpening, ErrInvalidReversal},
		{"rollback rollback", KindRollback, KindRollback, ErrInvalidReversal},
		{"rollback loss", KindRollback, KindLoss, ErrInvalidReversal},
		{"rollback opening", KindRollback, KindOpening, ErrInvalidReversal},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tgt := procTx(c.tgt)
			if err := ValidateReversalTarget(c.rev, tgt, false); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
		})
	}
	// Non-processed targets never reversible.
	pending := RehydrateTransaction("p", KindBet, StatePending, MustMoneyFromMinor(2500, "BRL"), "p1", "w1")
	if err := ValidateReversalTarget(KindRefund, pending, false); !errors.Is(err, ErrInvalidReversal) {
		t.Fatalf("pending err = %v", err)
	}
}

func TestAlreadyReversed(t *testing.T) {
	bet := procTx(KindBet)
	bet.MarkReversed("REFUND", "r1")
	if err := ValidateReversalTarget(KindRollback, bet, false); !errors.Is(err, ErrAlreadyReversed) {
		t.Fatalf("err = %v", err)
	}
}

func TestBetHasActiveWin(t *testing.T) {
	bet := procTx(KindBet)
	if err := ValidateReversalTarget(KindRefund, bet, true); !errors.Is(err, ErrBetHasActiveWin) {
		t.Fatalf("err = %v", err)
	}
	if err := ValidateReversalTarget(KindRollback, bet, true); !errors.Is(err, ErrBetHasActiveWin) {
		t.Fatalf("err = %v", err)
	}
	// Voiding the refund frees the BET slot (supported chain).
	bet.ClearReversal()
	if err := ValidateReversalTarget(KindRollback, bet, false); err != nil {
		t.Fatalf("after clear err = %v", err)
	}
}

func TestAgreement(t *testing.T) {
	amt := MustMoneyFromMinor(2500, "BRL")
	op := Operation{Kind: KindRefund, Amount: amt, ProviderID: "a", PlayerID: "p1", WalletID: "w1", RoundID: "r1"}
	ref := Reference{Kind: KindBet, Amount: amt, ProviderID: "a", PlayerID: "p1", WalletID: "w1", RoundID: "r1", State: StateProcessed}
	if err := ValidateReferenceAgreement(op, ref); err != nil {
		t.Fatalf("agree err = %v", err)
	}
	diff := MustMoneyFromMinor(2000, "BRL")
	op2 := op
	op2.Amount = diff
	if err := ValidateReferenceAgreement(op2, ref); !errors.Is(err, ErrAmountMismatch) {
		t.Fatalf("amount err = %v", err)
	}
	op3 := op
	op3.RoundID = "r2"
	if err := ValidateReferenceAgreement(op3, ref); !errors.Is(err, ErrReferenceRoundMismatch) {
		t.Fatalf("round err = %v", err)
	}
	op4 := op
	op4.Amount = MustMoneyFromMinor(2500, "USD")
	if err := ValidateReferenceAgreement(op4, ref); !errors.Is(err, ErrReferenceCurrencyMismatch) {
		t.Fatalf("currency err = %v", err)
	}
}

func TestWinReference(t *testing.T) {
	amt := MustMoneyFromMinor(2500, "BRL")
	win := Operation{Kind: KindWin, Amount: amt, ProviderID: "a", PlayerID: "p1", WalletID: "w1", RoundID: "r1"}
	good := &Reference{Kind: KindBet, Amount: amt, ProviderID: "a", PlayerID: "p1", WalletID: "w1", RoundID: "r1", State: StateProcessed}
	if err := ValidateWinReference(win, good); err != nil {
		t.Fatalf("good win ref err = %v", err)
	}
	rev := &Reference{Kind: KindBet, Amount: amt, ProviderID: "a", PlayerID: "p1", WalletID: "w1", RoundID: "r1", State: StateProcessed, Reversed: true}
	if err := ValidateWinReference(win, rev); !errors.Is(err, ErrReferenceReversed) {
		t.Fatalf("reversed err = %v", err)
	}
	if err := ValidateWinReference(win, nil); !errors.Is(err, ErrReferenceNotProcessed) {
		t.Fatalf("nil err = %v", err)
	}
}
