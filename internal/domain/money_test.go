package domain

import (
	"errors"
	"math"
	"testing"
)

func mustParse(t *testing.T, s, cur string) Money {
	t.Helper()
	m, err := ParseMoney(s, cur)
	if err != nil {
		t.Fatalf("ParseMoney(%q,%q) = %v", s, cur, err)
	}
	return m
}

func TestParseExact(t *testing.T) {
	m := mustParse(t, "25.00", "BRL")
	if m.AmountMinor() != 2500 || m.Currency() != "BRL" {
		t.Fatalf("got %d %s", m.AmountMinor(), m.Currency())
	}
}

func TestParseRejects(t *testing.T) {
	bad := []string{"25", "25.0", "25.000", " 25.00", "-5.00", "1e3", "", "25.00 ", "+5.00", "NaN", ".00", "25."}
	for _, s := range bad {
		if _, err := ParseMoney(s, "BRL"); !errors.Is(err, ErrInvalidMoney) && !errors.Is(err, ErrMoneyOverflow) {
			t.Fatalf("ParseMoney(%q) err = %v, want INVALID_MONEY", s, err)
		}
	}
	// Numeric JSON amount must be rejected (contract is string-only).
	var m Money
	if err := m.UnmarshalJSON([]byte(`{"amount":25.00,"currency":"BRL"}`)); !errors.Is(err, ErrInvalidMoney) {
		t.Fatalf("numeric JSON err = %v", err)
	}
	// Bad currency rejected.
	if _, err := ParseMoney("25.00", "brl"); !errors.Is(err, ErrInvalidMoney) {
		t.Fatalf("lowercase currency err = %v", err)
	}
}

func TestNoSilentNormalization(t *testing.T) {
	if _, err := ParseMoney("25.0", "BRL"); err == nil {
		t.Fatal("expected rejection for scale != 2")
	}
}

func TestOverflow(t *testing.T) {
	// Beyond int64 minor units.
	if _, err := ParseMoney("92233720368547758.08", "BRL"); !errors.Is(err, ErrMoneyOverflow) {
		t.Fatalf("want overflow, got %v", err)
	}
	max := MustMoneyFromMinor(math.MaxInt64, "BRL")
	one := MustMoneyFromMinor(1, "BRL")
	if _, err := max.Add(one); !errors.Is(err, ErrMoneyOverflow) {
		t.Fatalf("add overflow err = %v", err)
	}
	min := MustMoneyFromMinor(math.MinInt64, "BRL")
	if _, err := min.Sub(one); !errors.Is(err, ErrMoneyOverflow) {
		t.Fatalf("sub overflow err = %v", err)
	}
	if _, err := min.Negate(); !errors.Is(err, ErrMoneyOverflow) {
		t.Fatalf("neg MinInt64 err = %v", err)
	}
	// Safe edges still work.
	if _, err := max.Sub(one); err != nil {
		t.Fatalf("max-1 err = %v", err)
	}
}

func TestCurrencyMismatch(t *testing.T) {
	brl := mustParse(t, "10.00", "BRL")
	usd := mustParse(t, "10.00", "USD")
	if _, err := brl.Add(usd); !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf("add err = %v", err)
	}
	if _, err := brl.Sub(usd); !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf("sub err = %v", err)
	}
	if _, err := brl.Cmp(usd); !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf("cmp err = %v", err)
	}
}

func TestArithmeticAndCompare(t *testing.T) {
	a := mustParse(t, "100.00", "BRL")
	b := mustParse(t, "80.00", "BRL")
	sum, err := a.Add(b)
	if err != nil || sum.AmountMinor() != 18000 {
		t.Fatalf("sum = %v %v", sum, err)
	}
	diff, err := a.Sub(b)
	if err != nil || diff.AmountMinor() != 2000 {
		t.Fatalf("diff = %v %v", diff, err)
	}
	neg, err := b.Negate()
	if err != nil || neg.AmountMinor() != -8000 {
		t.Fatalf("neg = %v %v", neg, err)
	}
	c, err := a.Cmp(b)
	if err != nil || c != 1 {
		t.Fatalf("cmp = %d %v", c, err)
	}
}

func TestSerializationRoundTrip(t *testing.T) {
	m := mustParse(t, "25.00", "BRL")
	data, err := m.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"amount":"25.00","currency":"BRL"}` {
		t.Fatalf("json = %s", data)
	}
	var back Money
	if err := back.UnmarshalJSON(data); err != nil || !back.Equal(m) {
		t.Fatalf("round trip = %v %v", back, err)
	}
}
