package domain

import (
	"errors"
	"testing"
)

func TestStateMachine(t *testing.T) {
	amt := MustMoneyFromMinor(8000, "BRL")
	tx, err := NewTransaction("t1", KindBet, amt, "p1", "w1")
	if err != nil {
		t.Fatal(err)
	}
	if tx.State() != StatePending {
		t.Fatalf("initial = %s", tx.State())
	}
	// PENDING -> PENDING_REFERENCE -> PROCESSED is legal.
	if err := tx.MarkPendingReference(); err != nil {
		t.Fatal(err)
	}
	bal := MustMoneyFromMinor(2000, "BRL")
	if err := tx.MarkProcessed(bal, 2); err != nil {
		t.Fatal(err)
	}
	if got, ok := tx.ResultBalance(); !ok || got.AmountMinor() != 2000 {
		t.Fatalf("result = %v %v", got, ok)
	}
	// Terminal states refuse every exit.
	if err := tx.MarkRejected("X", bal, 2); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("terminal exit err = %v", err)
	}
	if err := tx.MarkFailed("X"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("terminal exit err = %v", err)
	}
}

func TestTerminalDirect(t *testing.T) {
	amt := MustMoneyFromMinor(8000, "BRL")
	tx, _ := NewTransaction("t2", KindBet, amt, "p1", "w1")
	bal := MustMoneyFromMinor(2000, "BRL")
	if err := tx.MarkRejected(CodeInsufficientBalance, bal, 1); err != nil {
		t.Fatal(err)
	}
	if tx.FailureCode() != CodeInsufficientBalance {
		t.Fatalf("code = %s", tx.FailureCode())
	}
	// FAILED only from non-terminal; business rejections never use it.
	tx2, _ := NewTransaction("t3", KindBet, amt, "p1", "w1")
	if err := tx2.MarkFailed("INFRA"); err != nil {
		t.Fatal(err)
	}
}

func TestLedgerMath(t *testing.T) {
	before := MustMoneyFromMinor(10000, "BRL")
	amt := MustMoneyFromMinor(8000, "BRL")
	after := MustMoneyFromMinor(2000, "BRL")
	e, err := NewLedgerEntry("l1", "w1", "t1", DirectionDebit, amt, before, after, 2)
	if err != nil {
		t.Fatal(err)
	}
	if e.WalletVersion() != 2 {
		t.Fatalf("version = %d", e.WalletVersion())
	}
	// Wrong math rejected.
	wrong := MustMoneyFromMinor(3000, "BRL")
	if _, err := NewLedgerEntry("l2", "w1", "t1", DirectionDebit, amt, before, wrong, 2); err == nil {
		t.Fatal("expected math rejection")
	}
	// Credit direction.
	c0 := MustMoneyFromMinor(2000, "BRL")
	c1 := MustMoneyFromMinor(10000, "BRL")
	if _, err := NewLedgerEntry("l3", "w1", "t2", DirectionCredit, amt, c0, c1, 3); err != nil {
		t.Fatalf("credit err = %v", err)
	}
	// Zero/negative ledger amounts never exist.
	zero := MustMoneyFromMinor(0, "BRL")
	if _, err := NewLedgerEntry("l4", "w1", "t3", DirectionCredit, zero, c0, c0, 3); !errors.Is(err, ErrInvalidAmountForKind) {
		t.Fatalf("zero err = %v", err)
	}
}

func TestOpeningInternal(t *testing.T) {
	if err := ValidateExternalKind(KindOpening); !errors.Is(err, ErrOpeningNotAllowed) {
		t.Fatalf("err = %v", err)
	}
	if err := ValidateExternalKind(KindBet); err != nil {
		t.Fatalf("bet err = %v", err)
	}
}

func TestZeroPolicy(t *testing.T) {
	pos := MustMoneyFromMinor(1000, "BRL")
	zero := MustMoneyFromMinor(0, "BRL")
	if _, err := NewTransaction("b", KindBet, zero, "p", "w"); !errors.Is(err, ErrInvalidAmountForKind) {
		t.Fatalf("bet zero err = %v", err)
	}
	if _, err := NewTransaction("l", KindLoss, pos, "p", "w"); !errors.Is(err, ErrInvalidAmountForKind) {
		t.Fatalf("loss pos err = %v", err)
	}
	if _, err := NewTransaction("l0", KindLoss, zero, "p", "w"); err != nil {
		t.Fatalf("loss zero err = %v", err)
	}
}
