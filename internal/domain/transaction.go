package domain

import "fmt"

// TransactionKind is the external/internal operation type.
type TransactionKind string

const (
	KindOpening  TransactionKind = "OPENING"
	KindBet      TransactionKind = "BET"
	KindWin      TransactionKind = "WIN"
	KindLoss     TransactionKind = "LOSS"
	KindRefund   TransactionKind = "REFUND"
	KindRollback TransactionKind = "ROLLBACK"
)

// TransactionState is the lifecycle state.
type TransactionState string

const (
	StatePending          TransactionState = "PENDING"
	StatePendingReference TransactionState = "PENDING_REFERENCE"
	StateProcessed        TransactionState = "PROCESSED"
	StateRejected         TransactionState = "REJECTED"
	StateFailed           TransactionState = "FAILED"
)

// IsTerminal reports whether no further transition is allowed.
func (s TransactionState) IsTerminal() bool {
	return s == StateProcessed || s == StateRejected || s == StateFailed
}

// WagerTransaction is the domain view of a wager row. Persistence,
// idempotency keys and hashes aren't treated here; only the lifecycle,
// money rules and reversal bookkeeping matter are treated here.
type WagerTransaction struct {
	id                  string
	kind                TransactionKind
	state               TransactionState
	amount              Money
	playerID            string
	walletID            string
	providerID          string
	externalID          string
	roundID             string
	referenceExternalID string

	failureCode string
	// resultBalance is the observed wallet balance at decision time. Replays
	// return this original value even after the wallet moved on.
	resultBalance Money
	hasResult     bool
	// walletVersionObserved is the wallet version seen at decision time.
	walletVersionObserved int64

	reversedByKind string
	reversedByTxID string
	hasReversal    bool
}

// NewTransaction validates kind/amount policy and starts in PENDING.
// Zero policy: BET/WIN/REFUND/ROLLBACK require amount > 0; LOSS requires
// exactly "0.00" (zero minor); OPENING requires >= 0. OPENING from external
// channels is rejected by ValidateExternalKind, not here.
func NewTransaction(id string, kind TransactionKind, amount Money, playerID, walletID string) (*WagerTransaction, error) {
	if id == "" {
		return nil, fmt.Errorf("%w: empty transaction id", ErrInvalidMoney)
	}
	if err := ValidateAmountForKind(kind, amount); err != nil {
		return nil, err
	}
	return &WagerTransaction{
		id: id, kind: kind, state: StatePending,
		amount: amount, playerID: playerID, walletID: walletID,
	}, nil
}

// RehydrateTransaction rebuilds a transaction from persistence.
func RehydrateTransaction(id string, kind TransactionKind, state TransactionState, amount Money, playerID, walletID string) *WagerTransaction {
	return &WagerTransaction{
		id: id, kind: kind, state: state,
		amount: amount, playerID: playerID, walletID: walletID,
	}
}

func (t *WagerTransaction) ID() string                   { return t.id }
func (t *WagerTransaction) Kind() TransactionKind        { return t.kind }
func (t *WagerTransaction) State() TransactionState      { return t.state }
func (t *WagerTransaction) Amount() Money                { return t.amount }
func (t *WagerTransaction) PlayerID() string             { return t.playerID }
func (t *WagerTransaction) WalletID() string             { return t.walletID }
func (t *WagerTransaction) FailureCode() string          { return t.failureCode }
func (t *WagerTransaction) HasReversal() bool            { return t.hasReversal }
func (t *WagerTransaction) ReversedByKind() string       { return t.reversedByKind }
func (t *WagerTransaction) ReversedByTxID() string       { return t.reversedByTxID }
func (t *WagerTransaction) ResultBalance() (Money, bool) { return t.resultBalance, t.hasResult }
func (t *WagerTransaction) WalletVersionObserved() int64 { return t.walletVersionObserved }

// WithReference attaches provider/external/round context (block 3 persists it).
func (t *WagerTransaction) WithReference(providerID, externalID, roundID, refExternalID string) *WagerTransaction {
	t.providerID = providerID
	t.externalID = externalID
	t.roundID = roundID
	t.referenceExternalID = refExternalID
	return t
}

// CanTransition reports whether s -> to is legal. Terminal states have no
// outgoing edges; PENDING fans out to all four; PENDING_REFERENCE to the
// three terminal-ish outcomes.
func (s TransactionState) CanTransition(to TransactionState) bool {
	switch s {
	case StatePending:
		return to == StateProcessed || to == StateRejected ||
			to == StatePendingReference || to == StateFailed
	case StatePendingReference:
		return to == StateProcessed || to == StateRejected || to == StateFailed
	default:
		return false
	}
}

// transition moves state, refusing terminal exits.
func (t *WagerTransaction) transition(to TransactionState) error {
	if !t.state.CanTransition(to) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, t.state, to)
	}
	t.state = to
	return nil
}

// MarkProcessed records a terminal success with the observed balance/version.
func (t *WagerTransaction) MarkProcessed(balance Money, walletVersion int64) error {
	if err := t.transition(StateProcessed); err != nil {
		return err
	}
	t.resultBalance = balance
	t.hasResult = true
	t.walletVersionObserved = walletVersion
	return nil
}

// MarkProcessedNoMovement records success without ledger/version bump
// (LOSS and zero OPENING paths).
func (t *WagerTransaction) MarkProcessedNoMovement(balance Money, walletVersion int64) error {
	return t.MarkProcessed(balance, walletVersion)
}

// MarkRejected records a terminal business rejection with code + observed balance.
func (t *WagerTransaction) MarkRejected(code string, balance Money, walletVersion int64) error {
	if err := t.transition(StateRejected); err != nil {
		return err
	}
	t.failureCode = code
	t.resultBalance = balance
	t.hasResult = true
	t.walletVersionObserved = walletVersion
	return nil
}

// MarkPendingReference parks the transaction awaiting its reference.
func (t *WagerTransaction) MarkPendingReference() error {
	return t.transition(StatePendingReference)
}

// MarkFailed records a permanent infrastructure failure (never business rules).
func (t *WagerTransaction) MarkFailed(code string) error {
	if err := t.transition(StateFailed); err != nil {
		return err
	}
	t.failureCode = code
	return nil
}

// MarkReversed records a successful direct reversal by (kind, tx).
func (t *WagerTransaction) MarkReversed(byKind, byTxID string) {
	t.hasReversal = true
	t.reversedByKind = byKind
	t.reversedByTxID = byTxID
}

// ClearReversal releases the slot (used when a ROLLBACK voids a REFUND,
// freeing the original BET for a future reversal per the supported chain).
func (t *WagerTransaction) ClearReversal() {
	t.hasReversal = false
	t.reversedByKind = ""
	t.reversedByTxID = ""
}

// ValidateExternalKind rejects OPENING arriving over HTTP/SQS.
func ValidateExternalKind(kind TransactionKind) error {
	if kind == KindOpening {
		return fmt.Errorf("%w: opening is internal only", ErrOpeningNotAllowed)
	}
	return nil
}

// ValidateAmountForKind enforces the zero policy per kind.
func ValidateAmountForKind(kind TransactionKind, amount Money) error {
	switch kind {
	case KindBet, KindWin, KindRefund, KindRollback:
		if !amount.IsPositive() {
			return fmt.Errorf("%w: %s requires positive amount", ErrInvalidAmountForKind, kind)
		}
	case KindLoss:
		if !amount.IsZero() {
			return fmt.Errorf("%w: LOSS requires 0.00", ErrInvalidAmountForKind)
		}
	case KindOpening:
		if amount.IsNegative() {
			return fmt.Errorf("%w: OPENING cannot be negative", ErrInvalidAmountForKind)
		}
	default:
		return fmt.Errorf("%w: unknown kind %q", ErrInvalidAmountForKind, kind)
	}
	return nil
}

// LedgerDirection is the immutable entry direction.
type LedgerDirection string

const (
	DirectionDebit  LedgerDirection = "DEBIT"
	DirectionCredit LedgerDirection = "CREDIT"
)

// LedgerEntry is an immutable balance-change record. Block 3 enforces
// append-only at the DB level (trigger + roles); the domain validates the
// math at construction so invalid entries never reach persistence.
type LedgerEntry struct {
	id            string
	walletID      string
	transactionID string
	direction     LedgerDirection
	amount        Money
	before        Money
	after         Money
	walletVersion int64
}

// NewLedgerEntry validates direction/amount/currency and the invariant
// balanceAfter = balanceBefore +/- amount with non-negative balances.
func NewLedgerEntry(id, walletID, transactionID string, direction LedgerDirection, amount, before, after Money, walletVersion int64) (*LedgerEntry, error) {
	if id == "" || walletID == "" || transactionID == "" {
		return nil, fmt.Errorf("%w: empty ledger identity", ErrInvalidMoney)
	}
	if direction != DirectionDebit && direction != DirectionCredit {
		return nil, fmt.Errorf("%w: direction %q", ErrInvalidMoney, direction)
	}
	if !amount.IsPositive() {
		return nil, fmt.Errorf("%w: ledger amount must be positive", ErrInvalidAmountForKind)
	}
	if amount.Currency() != before.Currency() || amount.Currency() != after.Currency() {
		return nil, fmt.Errorf("%w: ledger currency mismatch", ErrCurrencyMismatch)
	}
	if before.IsNegative() || after.IsNegative() {
		return nil, fmt.Errorf("%w: ledger balance negative", ErrInvalidMoney)
	}
	var expect Money
	var err error
	if direction == DirectionDebit {
		expect, err = before.Sub(amount)
	} else {
		expect, err = before.Add(amount)
	}
	if err != nil {
		return nil, err
	}
	if !expect.Equal(after) {
		return nil, fmt.Errorf("%w: balanceAfter != before +/- amount", ErrInvalidMoney)
	}
	return &LedgerEntry{
		id: id, walletID: walletID, transactionID: transactionID,
		direction: direction, amount: amount,
		before: before, after: after, walletVersion: walletVersion,
	}, nil
}

func (e *LedgerEntry) ID() string                 { return e.id }
func (e *LedgerEntry) WalletID() string           { return e.walletID }
func (e *LedgerEntry) TransactionID() string      { return e.transactionID }
func (e *LedgerEntry) Direction() LedgerDirection { return e.direction }
func (e *LedgerEntry) Amount() Money              { return e.amount }
func (e *LedgerEntry) BalanceBefore() Money       { return e.before }
func (e *LedgerEntry) BalanceAfter() Money        { return e.after }
func (e *LedgerEntry) WalletVersion() int64       { return e.walletVersion }
