package domain

import "fmt"

// Reversal validation implements the seamless anti-overpayment matrix
// (ADR-0008, wagering spec):
//
//	reverser \ target | BET(proc) | WIN(proc) | REFUND(proc) | else
//	REFUND            | OK        | NEVER     | NEVER        | NEVER
//	ROLLBACK          | OK        | OK        | OK (void)    | NEVER
//
// "else" covers LOSS, OPENING, non-PROCESSED targets, and ROLLBACK targets.
// At most one successful direct reversal per BET (ALREADY_REVERSED); a BET
// with a dependent PROCESSED WIN not yet reversed blocks (BET_HAS_ACTIVE_WIN).
// ROLLBACK of a REFUND debits (void) and frees the original BET slot — the
// supported BET -> REFUND -> ROLLBACK-of-REFUND chain.

// IsReversalKind reports whether kind is a reversal operation.
func IsReversalKind(kind TransactionKind) bool {
	return kind == KindRefund || kind == KindRollback
}

// ValidateReversalTarget checks the matrix edge (reverser kind vs target
// kind/state) plus the one-reversal and active-WIN guards. hasActiveWin must
// be true when the target BET owns a dependent PROCESSED WIN not yet reversed.
func ValidateReversalTarget(reverserKind TransactionKind, target *WagerTransaction, hasActiveWin bool) error {
	if target == nil {
		return fmt.Errorf("%w: nil reference", ErrInvalidReversal)
	}
	if target.State() != StateProcessed {
		return fmt.Errorf("%w: target %s not processed", ErrInvalidReversal, target.State())
	}
	switch reverserKind {
	case KindRefund:
		if target.Kind() != KindBet {
			return fmt.Errorf("%w: REFUND only reverses BET", ErrInvalidReversal)
		}
	case KindRollback:
		switch target.Kind() {
		case KindBet, KindWin, KindRefund:
			// allowed below
		default:
			return fmt.Errorf("%w: ROLLBACK cannot reverse %s", ErrInvalidReversal, target.Kind())
		}
	default:
		return fmt.Errorf("%w: %s is not a reversal", ErrInvalidReversal, reverserKind)
	}
	if target.HasReversal() {
		return fmt.Errorf("%w: target already reversed by %s",
			ErrAlreadyReversed, target.ReversedByKind())
	}
	if target.Kind() == KindBet && hasActiveWin {
		return fmt.Errorf("%w: bet has dependent processed win", ErrBetHasActiveWin)
	}
	return nil
}

// Operation describes the incoming reversal for agreement checks.
type Operation struct {
	Kind              TransactionKind
	Amount            Money
	ProviderID        string
	PlayerID          string
	WalletID          string
	RoundID           string
	ReferenceExternal string
}

// Reference describes the resolved target for agreement checks.
type Reference struct {
	Kind       TransactionKind
	Amount     Money
	ProviderID string
	PlayerID   string
	WalletID   string
	RoundID    string
	State      TransactionState
	Reversed   bool
}

// ValidateReferenceAgreement enforces provider/player/wallet/currency/round
// concordance plus integral (non-partial) value equality.
func ValidateReferenceAgreement(op Operation, ref Reference) error {
	if op.ProviderID != ref.ProviderID {
		return fmt.Errorf("%w: provider mismatch", ErrReferenceProviderMismatch)
	}
	if op.PlayerID != ref.PlayerID {
		return fmt.Errorf("%w: player mismatch", ErrReferencePlayerMismatch)
	}
	if op.WalletID != ref.WalletID {
		return fmt.Errorf("%w: wallet mismatch", ErrReferenceWalletMismatch)
	}
	if op.Amount.Currency() != ref.Amount.Currency() {
		return fmt.Errorf("%w: currency mismatch", ErrReferenceCurrencyMismatch)
	}
	if op.RoundID != ref.RoundID {
		return fmt.Errorf("%w: round mismatch", ErrReferenceRoundMismatch)
	}
	if !op.Amount.Equal(ref.Amount) {
		return fmt.Errorf("%w: %s vs %s", ErrAmountMismatch, op.Amount.String(), ref.Amount.String())
	}
	return nil
}

// ValidateWinReference validates an optional WIN reference: when present it
// must point at a PROCESSED non-reversed BET in the same round/player/
// wallet/currency. Callers map a missing reference to PENDING_REFERENCE and
// a terminally-bad one to REFERENCE_NOT_PROCESSED / REFERENCE_REVERSED.
func ValidateWinReference(win Operation, bet *Reference) error {
	if bet == nil {
		return fmt.Errorf("%w: win reference not found", ErrReferenceNotProcessed)
	}
	if bet.Kind != KindBet {
		return fmt.Errorf("%w: win must reference a bet", ErrInvalidReversal)
	}
	if bet.State != StateProcessed {
		return fmt.Errorf("%w: bet %s", ErrReferenceNotProcessed, bet.State)
	}
	if bet.Reversed {
		return fmt.Errorf("%w: bet reversed", ErrReferenceReversed)
	}
	return ValidateReferenceAgreement(win, *bet)
}
