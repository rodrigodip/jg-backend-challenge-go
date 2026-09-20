package domain

import "errors"

// Stable failure codes shared by the domain and, later, HTTP/SQS adapters.
// They mirror the spec vocabularies (money, wallet-ledger, wagering,
// idempotency-contracts) so adapters can map them without string guessing.
const (
	CodeInvalidMoney                = "INVALID_MONEY"
	CodeInvalidAmountForKind        = "INVALID_AMOUNT_FOR_KIND"
	CodeCurrencyMismatch            = "CURRENCY_MISMATCH"
	CodeWalletPlayerMismatch        = "WALLET_PLAYER_MISMATCH"
	CodeInsufficientBalance         = "INSUFFICIENT_BALANCE"
	CodeRollbackInsufficientBalance = "ROLLBACK_INSUFFICIENT_BALANCE"
	CodeOpeningNotAllowed           = "OPENING_NOT_ALLOWED"
	CodeReferenceRequired           = "REFERENCE_REQUIRED"
	CodeAmountMismatch              = "AMOUNT_MISMATCH"
	CodeReferenceCurrencyMismatch   = "REFERENCE_CURRENCY_MISMATCH"
	CodeReferenceWalletMismatch     = "REFERENCE_WALLET_MISMATCH"
	CodeReferencePlayerMismatch     = "REFERENCE_PLAYER_MISMATCH"
	CodeReferenceRoundMismatch      = "REFERENCE_ROUND_MISMATCH"
	CodeReferenceProviderMismatch   = "REFERENCE_PROVIDER_MISMATCH"
	CodeReferenceReversed           = "REFERENCE_REVERSED"
	CodeReferenceNotProcessed       = "REFERENCE_NOT_PROCESSED"
	CodeAlreadyReversed             = "ALREADY_REVERSED"
	CodeBetHasActiveWin             = "BET_HAS_ACTIVE_WIN"
	CodeInvalidReversal             = "INVALID_REVERSAL"
	CodeInvalidTransition           = "INVALID_TRANSITION"
	CodeMoneyOverflow               = "MONEY_OVERFLOW"
)

// CodedError carries a stable failure code and participates in errors.Is/As.
type CodedError struct {
	Code string
	Msg  string
}

func (e *CodedError) Error() string { return e.Code + ": " + e.Msg }

// CodeOf returns the stable failure code of err, or "" when unknown.
func CodeOf(err error) string {
	var ce *CodedError
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}

func coded(code, msg string) *CodedError { return &CodedError{Code: code, Msg: msg} }

// Sentinel domain errors for errors.Is checks.
var (
	ErrInvalidMoney                = coded(CodeInvalidMoney, "invalid money")
	ErrInvalidAmountForKind        = coded(CodeInvalidAmountForKind, "invalid amount for kind")
	ErrCurrencyMismatch            = coded(CodeCurrencyMismatch, "currency mismatch")
	ErrWalletPlayerMismatch        = coded(CodeWalletPlayerMismatch, "wallet player mismatch")
	ErrInsufficientBalance         = coded(CodeInsufficientBalance, "insufficient balance")
	ErrRollbackInsufficientBalance = coded(CodeRollbackInsufficientBalance, "rollback would overdraw")
	ErrOpeningNotAllowed           = coded(CodeOpeningNotAllowed, "opening not allowed from external channel")
	ErrReferenceRequired           = coded(CodeReferenceRequired, "reference required")
	ErrAmountMismatch              = coded(CodeAmountMismatch, "amount mismatch")
	ErrReferenceCurrencyMismatch   = coded(CodeReferenceCurrencyMismatch, "reference currency mismatch")
	ErrReferenceWalletMismatch     = coded(CodeReferenceWalletMismatch, "reference wallet mismatch")
	ErrReferencePlayerMismatch     = coded(CodeReferencePlayerMismatch, "reference player mismatch")
	ErrReferenceRoundMismatch      = coded(CodeReferenceRoundMismatch, "reference round mismatch")
	ErrReferenceProviderMismatch   = coded(CodeReferenceProviderMismatch, "reference provider mismatch")
	ErrReferenceReversed           = coded(CodeReferenceReversed, "reference reversed")
	ErrReferenceNotProcessed       = coded(CodeReferenceNotProcessed, "reference not processed")
	ErrAlreadyReversed             = coded(CodeAlreadyReversed, "already reversed")
	ErrBetHasActiveWin             = coded(CodeBetHasActiveWin, "bet has active win")
	ErrInvalidReversal             = coded(CodeInvalidReversal, "invalid reversal")
	ErrInvalidTransition           = coded(CodeInvalidTransition, "invalid transition")
	ErrMoneyOverflow               = coded(CodeMoneyOverflow, "money overflow")
)
