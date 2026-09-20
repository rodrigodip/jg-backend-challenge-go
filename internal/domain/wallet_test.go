package domain

import (
	"errors"
	"testing"
)

func testWallet(t *testing.T, balance string) *Wallet {
	t.Helper()
	m, err := ParseMoney(balance, "BRL")
	if err != nil {
		t.Fatal(err)
	}
	w, err := NewWallet("w1", "p1", "BRL", m)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestWalletCreationVersion1(t *testing.T) {
	w := testWallet(t, "1000.00")
	if w.Version() != 1 {
		t.Fatalf("version = %d", w.Version())
	}
	// Zero opening allowed.
	z, err := ParseMoney("0.00", "BRL")
	if err != nil {
		t.Fatal(err)
	}
	w0, err := NewWallet("w0", "p1", "BRL", z)
	if err != nil || w0.Version() != 1 {
		t.Fatalf("zero opening = %v %v", w0, err)
	}
	// Duplicate (player,currency) conflict is a persistence unique; domain
	// keeps identity so repositories can enforce it — ids differ here.
}

func TestWalletDebitCredit(t *testing.T) {
	w := testWallet(t, "100.00")
	b80, _ := ParseMoney("80.00", "BRL")
	before, after, err := w.Debit("p1", b80)
	if err != nil {
		t.Fatal(err)
	}
	if before.AmountMinor() != 10000 || after.AmountMinor() != 2000 {
		t.Fatalf("before/after = %d/%d", before.AmountMinor(), after.AmountMinor())
	}
	if w.Version() != 2 {
		t.Fatalf("version = %d", w.Version())
	}
	// Second 80 over 20 fails with INSUFFICIENT_BALANCE, no version bump.
	if _, _, err := w.Debit("p1", b80); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("err = %v", err)
	}
	if w.Version() != 2 || w.Balance().AmountMinor() != 2000 {
		t.Fatalf("state moved on failed debit: v=%d b=%d", w.Version(), w.Balance().AmountMinor())
	}
	// Credit back works.
	if _, _, err := w.Credit("p1", b80); err != nil {
		t.Fatal(err)
	}
	if w.Balance().AmountMinor() != 10000 || w.Version() != 3 {
		t.Fatalf("after credit v=%d b=%d", w.Version(), w.Balance().AmountMinor())
	}
}

func TestWalletPlayerMismatch(t *testing.T) {
	w := testWallet(t, "100.00")
	amt, _ := ParseMoney("10.00", "BRL")
	if _, _, err := w.Debit("other", amt); !errors.Is(err, ErrWalletPlayerMismatch) {
		t.Fatalf("debit err = %v", err)
	}
	if _, _, err := w.Credit("other", amt); !errors.Is(err, ErrWalletPlayerMismatch) {
		t.Fatalf("credit err = %v", err)
	}
}

func TestWalletCurrencyMismatch(t *testing.T) {
	w := testWallet(t, "100.00")
	usd, _ := ParseMoney("10.00", "USD")
	if _, _, err := w.Debit("p1", usd); !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf("err = %v", err)
	}
}

func TestWalletRehydrate(t *testing.T) {
	w, err := RehydrateWallet("w1", "p1", "BRL", 2000, 2)
	if err != nil {
		t.Fatal(err)
	}
	if w.Balance().AmountMinor() != 2000 || w.Version() != 2 {
		t.Fatalf("rehydrated = %d v%d", w.Balance().AmountMinor(), w.Version())
	}
}
