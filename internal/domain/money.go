package domain

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Money is an exact monetary value in minor units (cents) with its ISO 4217
// currency. No float32/float64 may transport money on any path; parsing,
// arithmetic, serialization and persistence all stay in int64.
type Money struct {
	minor    int64
	currency string
}

// AmountMinor returns the value in minor units.
func (m Money) AmountMinor() int64 { return m.minor }

// Currency returns the ISO 4217 code.
func (m Money) Currency() string { return m.currency }

// IsZero reports whether the amount is exactly zero.
func (m Money) IsZero() bool { return m.minor == 0 }

// IsPositive reports whether the amount is strictly positive.
func (m Money) IsPositive() bool { return m.minor > 0 }

// IsNegative reports whether the amount is strictly negative.
// Negative values only appear in internal differences, never in wallet
// balances or external input.
func (m Money) IsNegative() bool { return m.minor < 0 }

// validCurrency reports whether 's' is a plausible ISO 4217 code: exactly 3
// uppercase ASCII letters. The platform operates BRL primarily plus USD for
// incompatibility tests; any other well-formed code is carried opaquely so
// mismatch detection (not allow-listing) decides.
func validCurrency(s string) bool {
	if len(s) != 3 {
		return false
	}
	for i := 0; i < 3; i++ {
		if s[i] < 'A' || s[i] > 'Z' {
			return false
		}
	}
	return true
}

// ParseMoney parses amountStr strictly as digits, one dot, exactly two
// decimals (^[0-9]+\.[0-9]{2}$) and attaches currency. It rejects empty,
// signs, spaces, NaN, Infinity, scientific notation, wrong scale, JSON
// numbers (callers must pass the raw string), and int64 overflow. No silent
// normalization or rounding is applied: "25.00" and "25.0" are distinct
// inputs and the latter is rejected.
func ParseMoney(amountStr, currency string) (Money, error) {
	if !validCurrency(currency) {
		return Money{}, fmt.Errorf("%w: currency %q", ErrInvalidMoney, currency)
	}
	if len(amountStr) < 4 { // shortest valid is "0.00"
		return Money{}, fmt.Errorf("%w: amount %q", ErrInvalidMoney, amountStr)
	}
	dot := strings.IndexByte(amountStr, '.')
	if dot < 0 || strings.IndexByte(amountStr[dot+1:], '.') >= 0 {
		return Money{}, fmt.Errorf("%w: amount %q", ErrInvalidMoney, amountStr)
	}
	intPart, fracPart := amountStr[:dot], amountStr[dot+1:]
	if len(fracPart) != 2 {
		return Money{}, fmt.Errorf("%w: amount %q", ErrInvalidMoney, amountStr)
	}
	if len(intPart) == 0 {
		return Money{}, fmt.Errorf("%w: amount %q", ErrInvalidMoney, amountStr)
	}
	for i := 0; i < len(intPart); i++ {
		if intPart[i] < '0' || intPart[i] > '9' {
			return Money{}, fmt.Errorf("%w: amount %q", ErrInvalidMoney, amountStr)
		}
	}
	for i := 0; i < 2; i++ {
		if fracPart[i] < '0' || fracPart[i] > '9' {
			return Money{}, fmt.Errorf("%w: amount %q", ErrInvalidMoney, amountStr)
		}
	}
	dollars, err := strconv.ParseInt(intPart, 10, 64)
	if err != nil {
		return Money{}, fmt.Errorf("%w: amount %q: %v", ErrMoneyOverflow, amountStr, err)
	}
	cents, err := strconv.ParseInt(fracPart, 10, 64)
	if err != nil {
		return Money{}, fmt.Errorf("%w: amount %q", ErrInvalidMoney, amountStr)
	}
	// dollars*100 + cents with overflow detection.
	if dollars > (math.MaxInt64-cents)/100 {
		return Money{}, fmt.Errorf("%w: amount %q", ErrMoneyOverflow, amountStr)
	}
	return Money{minor: dollars*100 + cents, currency: currency}, nil
}

// NewMoneyFromMinor builds a Money from already-validated minor units.
// Currency must still be well-formed; negativity is allowed here because
// internal differences (e.g. reconciliation) need it, while wallet balances
// and external input reject negatives at their own layer.
func NewMoneyFromMinor(minor int64, currency string) (Money, error) {
	if !validCurrency(currency) {
		return Money{}, fmt.Errorf("%w: currency %q", ErrInvalidMoney, currency)
	}
	return Money{minor: minor, currency: currency}, nil
}

// MustMoneyFromMinor is a test/construction helper that panics on bad currency.
func MustMoneyFromMinor(minor int64, currency string) Money {
	m, err := NewMoneyFromMinor(minor, currency)
	if err != nil {
		panic(err)
	}
	return m
}

// String renders "25.00" for 2500 minor. Negative internals render "-1.23".
func (m Money) String() string {
	neg := m.minor < 0
	abs := m.minor
	if neg {
		if abs == math.MinInt64 {
			// Avoid overflow on negation: format via unsigned math.
			u := uint64(math.MaxInt64) + 1
			q, r := u/100, u%100
			return fmt.Sprintf("-%d.%02d", q, r)
		}
		abs = -abs
	}
	return fmt.Sprintf("%s%d.%02d", map[bool]string{true: "-", false: ""}[neg], abs/100, abs%100)
}

// Format returns the canonical external string form.
func (m Money) Format() string { return m.String() }

// checkCurrency ensures both operands share a currency.
func (m Money) checkCurrency(o Money) error {
	if m.currency != o.currency {
		return fmt.Errorf("%w: %s vs %s", ErrCurrencyMismatch, m.currency, o.currency)
	}
	return nil
}

// Add returns m+o for same-currency operands, detecting int64 overflow.
func (m Money) Add(o Money) (Money, error) {
	if err := m.checkCurrency(o); err != nil {
		return Money{}, err
	}
	sum := m.minor + o.minor
	// Overflow iff both signs equal and result sign differs.
	if ((m.minor ^ sum) & (o.minor ^ sum)) < 0 {
		return Money{}, fmt.Errorf("%w: add overflow", ErrMoneyOverflow)
	}
	return Money{minor: sum, currency: m.currency}, nil
}

// Sub returns m-o for same-currency operands, detecting int64 overflow.
func (m Money) Sub(o Money) (Money, error) {
	if err := m.checkCurrency(o); err != nil {
		return Money{}, err
	}
	diff := m.minor - o.minor
	if ((m.minor ^ o.minor) & (m.minor ^ diff)) < 0 {
		return Money{}, fmt.Errorf("%w: sub overflow", ErrMoneyOverflow)
	}
	return Money{minor: diff, currency: m.currency}, nil
}

// Negate returns -m, failing safely on MinInt64.
func (m Money) Negate() (Money, error) {
	if m.minor == math.MinInt64 {
		return Money{}, fmt.Errorf("%w: negate MinInt64", ErrMoneyOverflow)
	}
	return Money{minor: -m.minor, currency: m.currency}, nil
}

// Cmp compares same-currency values: -1, 0, +1.
func (m Money) Cmp(o Money) (int, error) {
	if err := m.checkCurrency(o); err != nil {
		return 0, err
	}
	switch {
	case m.minor < o.minor:
		return -1, nil
	case m.minor > o.minor:
		return 1, nil
	default:
		return 0, nil
	}
}

// Equal reports same currency and same minor units.
func (m Money) Equal(o Money) bool { return m.currency == o.currency && m.minor == o.minor }

// moneyJSON is the external contract: amount as string, currency as code.
type moneyJSON struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

// MarshalJSON emits {"amount":"25.00","currency":"BRL"} without floats.
func (m Money) MarshalJSON() ([]byte, error) {
	return json.Marshal(moneyJSON{Amount: m.String(), Currency: m.currency})
}

// UnmarshalJSON parses the strict external contract. Numeric JSON amounts
// are rejected: Amount must be a string in strict form.
func (m *Money) UnmarshalJSON(data []byte) error {
	var raw struct {
		Amount   json.RawMessage `json:"amount"`
		Currency string          `json:"currency"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidMoney, err)
	}
	var s string
	if err := json.Unmarshal(raw.Amount, &s); err != nil {
		return fmt.Errorf("%w: amount must be string", ErrInvalidMoney)
	}
	parsed, err := ParseMoney(s, raw.Currency)
	if err != nil {
		return err
	}
	*m = parsed
	return nil
}
