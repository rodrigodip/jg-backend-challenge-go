package domain

import (
	"fmt"
)

// Wallet is the root of the financial aggregate, identified by
// (playerID, currency). Balance never goes negative; version starts at 1
// and increments only on balance changes (it doubles as the ledger cursor
// sequence). LOSS and rejected operations never touch balance or version.
type Wallet struct {
	id       string
	playerID string
	currency string
	balance  Money
	version  int64
}

// ID returns the wallet identifier.
func (w *Wallet) ID() string { return w.id }

// PlayerID returns the owning player.
func (w *Wallet) PlayerID() string { return w.playerID }

// Currency returns the wallet currency.
func (w *Wallet) Currency() string { return w.currency }

// Balance returns the current balance.
func (w *Wallet) Balance() Money { return w.balance }

// Version returns the current version (ledger sequence).
func (w *Wallet) Version() int64 { return w.version }

// NewWallet creates a wallet at version 1. initial must be zero or positive
// in the wallet currency; negative openings are rejected. A zero opening
// creates no OPENING entry, ledger row or event (handled by callers).
func NewWallet(id, playerID, currency string, initial Money) (*Wallet, error) {
	if id == "" || playerID == "" {
		return nil, fmt.Errorf("%w: empty id or player", ErrInvalidMoney)
	}
	if !validCurrency(currency) {
		return nil, fmt.Errorf("%w: currency %q", ErrInvalidMoney, currency)
	}
	if initial.Currency() != currency {
		return nil, fmt.Errorf("%w: wallet %s vs initial %s",
			ErrCurrencyMismatch, currency, initial.Currency())
	}
	if initial.IsNegative() {
		return nil, fmt.Errorf("%w: negative opening", ErrInvalidMoney)
	}
	return &Wallet{id: id, playerID: playerID, currency: currency, balance: initial, version: 1}, nil
}

// RehydrateWallet rebuilds a wallet from persistence without revalidating
// business rules (used by repositories).
func RehydrateWallet(id, playerID, currency string, balanceMinor, version int64) (*Wallet, error) {
	if !validCurrency(currency) {
		return nil, fmt.Errorf("%w: currency %q", ErrInvalidMoney, currency)
	}
	if version < 1 {
		return nil, fmt.Errorf("%w: version %d", ErrInvalidMoney, version)
	}
	balance, err := NewMoneyFromMinor(balanceMinor, currency)
	if err != nil {
		return nil, err
	}
	return &Wallet{id: id, playerID: playerID, currency: currency, balance: balance, version: version}, nil
}

// checkParty validates the operating player and the money currency.
func (w *Wallet) checkParty(playerID string, amount Money) error {
	if playerID != w.playerID {
		return fmt.Errorf("%w: op player %s vs wallet player %s",
			ErrWalletPlayerMismatch, playerID, w.playerID)
	}
	if amount.Currency() != w.currency {
		return fmt.Errorf("%w: op %s vs wallet %s",
			ErrCurrencyMismatch, amount.Currency(), w.currency)
	}
	return nil
}

// Credit applies a positive credit, bumping version by exactly 1 and
// returning before/after balances for the ledger entry.
func (w *Wallet) Credit(playerID string, amount Money) (before, after Money, err error) {
	if err := w.checkParty(playerID, amount); err != nil {
		return Money{}, Money{}, err
	}
	if !amount.IsPositive() {
		return Money{}, Money{}, fmt.Errorf("%w: credit must be positive", ErrInvalidAmountForKind)
	}
	after, err = w.balance.Add(amount)
	if err != nil {
		return Money{}, Money{}, err
	}
	before = w.balance
	w.balance = after
	w.version++
	return before, after, nil
}

// Debit applies a positive debit with sufficient funds, bumping version by
// exactly 1 and returning before/after balances for the ledger entry.
func (w *Wallet) Debit(playerID string, amount Money) (before, after Money, err error) {
	if err := w.checkParty(playerID, amount); err != nil {
		return Money{}, Money{}, err
	}
	if !amount.IsPositive() {
		return Money{}, Money{}, fmt.Errorf("%w: debit must be positive", ErrInvalidAmountForKind)
	}
	cmp, err := w.balance.Cmp(amount)
	if err != nil {
		return Money{}, Money{}, err
	}
	if cmp < 0 {
		return Money{}, Money{}, fmt.Errorf("%w: balance %s < debit %s",
			ErrInsufficientBalance, w.balance.String(), amount.String())
	}
	after, err = w.balance.Sub(amount)
	if err != nil {
		return Money{}, Money{}, err
	}
	before = w.balance
	w.balance = after
	w.version++
	return before, after, nil
}
