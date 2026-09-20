package wagering

import (
	"errors"
	"fmt"
	"time"

	"github.com/jg-backend-challenge/wallet/internal/domain"
	"github.com/jg-backend-challenge/wallet/internal/ports"
)

// Tuning from D4/design: TTL, attempt budgets, backoff window.
const (
	ReferenceTTL      = 60 * time.Second
	MaxRefAttempts    = 5
	MaxInfraAttempts  = 10
	BackoffBase       = 200 * time.Millisecond
	BackoffCap        = 3 * time.Second
	WorkLeaseTTL      = 30 * time.Second
	OutboxLeaseTTL    = 30 * time.Second
	EventProcessed    = "WagerTransactionProcessed"
	EventRejected     = "WagerTransactionRejected"
	EventBalance      = "WalletBalanceChanged"
	EventPendingRef   = "WagerTransactionPendingReference"
	CodeConflict      = "IDEMPOTENCY_CONFLICT"
	CodeWalletMissing = "WALLET_NOT_FOUND"
	CodeRefNotFound   = "REFERENCE_NOT_FOUND"
	CodeInvalidReq    = "INVALID_REQUEST"
)

// Outcome classifies a submit result for adapters (HTTP status mapping
// happens in block 4; SQS routing in block 5).
type Outcome string

const (
	OutcomeProcessed Outcome = "processed"
	OutcomeRejected  Outcome = "rejected"
	OutcomePending   Outcome = "pending"
	OutcomeReplay    Outcome = "replay"
)

// CorrectableError marks input errors that persist nothing and reserve no
// idempotency key (INVALID_MONEY, WALLET_NOT_FOUND, ...). Adapters map it
// to 400 without a transaction body.
type CorrectableError struct {
	Code    string
	Message string
}

func (e *CorrectableError) Error() string { return e.Code + ": " + e.Message }

// ConflictError marks idempotency conflicts (HTTP 409): same key or same
// external id with divergent content.
type ConflictError struct {
	Code    string
	Message string
}

func (e *ConflictError) Error() string { return e.Code + ": " + e.Message }

// PoisonError marks an SQS redelivery with the same messageId but divergent
// content: tampering, never a financial replay. Block 5 routes it to the DLQ.
type PoisonError struct {
	MessageID string
	Want      string
	Got       string
}

func (e *PoisonError) Error() string {
	return fmt.Sprintf("poison message %s: hash divergence", e.MessageID)
}

func correctable(code, msg string) *CorrectableError {
	return &CorrectableError{Code: code, Message: msg}
}

// SubmitInput is one wager operation from either channel.
type SubmitInput struct {
	ProviderID        string
	ExternalID        string
	IdempotencyKey    string
	PlayerID          string
	WalletID          string
	RoundID           string
	GameID            string
	Kind              domain.TransactionKind
	AmountText        string // raw "25.00"
	Currency          string
	ReferenceExternal string // optional (WIN) / required (REFUND, ROLLBACK)

	ConsumerName  string // SQS only; empty on HTTP
	MessageID     string // SQS only; empty on HTTP
	CorrelationID string
}

// SubmitResult is the durable outcome with the originally observed balance.
// PENDING results carry no balance (adapters omit it / answer 202).
type SubmitResult struct {
	Outcome          Outcome
	TransactionID    string
	Balance          *domain.Money
	FailureCode      string
	WalletVersion    int64
	IdempotentReplay bool
}

// Service is the transactional wager use-case over ports.DB.
type Service struct {
	DB    ports.DB
	Clock ports.Clock
	NewID func() string
}

// now returns clock time or UTC when no clock is wired.
func (s *Service) now() time.Time {
	if s.Clock != nil {
		return s.Clock.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *Service) newID() string {
	if s.NewID != nil {
		return s.NewID()
	}
	return NewUUID()
}

// Submit accepts one operation: validates correctables without persisting,
// then commits state+balance+ledger+inbox+outbox(+work) atomically, or
// returns the persisted replay / a 409 conflict without reapplying money.
func (s *Service) Submit(in SubmitInput) (*SubmitResult, error) {
	if in.ProviderID == "" || in.ExternalID == "" || in.IdempotencyKey == "" {
		return nil, correctable(CodeInvalidReq, "provider, external id and idempotency key are required")
	}
	if err := domain.ValidateExternalKind(in.Kind); err != nil {
		return nil, correctable(domain.CodeOpeningNotAllowed, err.Error())
	}
	amount, err := domain.ParseMoney(in.AmountText, in.Currency)
	if err != nil {
		code := domain.CodeOf(err)
		if code == "" {
			code = domain.CodeInvalidMoney
		}
		return nil, correctable(code, err.Error())
	}
	if err := domain.ValidateAmountForKind(in.Kind, amount); err != nil {
		return nil, correctable(domain.CodeInvalidAmountForKind, err.Error())
	}
	if domain.IsReversalKind(in.Kind) && in.ReferenceExternal == "" {
		return nil, correctable(domain.CodeReferenceRequired, "reference required")
	}
	if in.Kind == domain.KindWin && in.ReferenceExternal != "" {
		// Reference validated inside the transaction; presence alone is fine.
	}
	hash, err := PayloadHash(BusinessHashInput{
		ProviderID: in.ProviderID, ExternalTransactionID: in.ExternalID,
		PlayerID: in.PlayerID, WalletID: in.WalletID, RoundID: in.RoundID,
		GameID: in.GameID, Kind: string(in.Kind), AmountText: in.AmountText,
		Currency: in.Currency, ReferenceExternalID: in.ReferenceExternal,
	})
	if err != nil {
		return nil, correctable(CodeInvalidReq, err.Error())
	}

	// SQS transport check (outside the business tx, read-only): same
	// messageId + same hash replays via the financial lookup below; same
	// messageId + divergent hash is poisoning and never touches money.
	if in.MessageID != "" {
		stored, _, found, err := s.DB.Aux().FindInbox(in.ConsumerName, in.MessageID)
		if err != nil {
			return nil, fmt.Errorf("inbox lookup: %w", err)
		}
		if found && stored != hash {
			return nil, &PoisonError{MessageID: in.MessageID, Want: stored, Got: hash}
		}
	}

	var result *SubmitResult
	if err := s.DB.Transact(func(db ports.DB) error {
		wallet, err := db.Wallet().LockForUpdate(in.WalletID)
		if err != nil {
			return err
		}
		if wallet == nil {
			return correctable(CodeWalletMissing, "wallet not found")
		}
		if in.PlayerID != wallet.PlayerID {
			return correctable(domain.CodeWalletPlayerMismatch, "operation player differs from wallet player")
		}
		if in.Currency != wallet.Currency {
			return correctable(domain.CodeCurrencyMismatch, "operation currency differs from wallet currency")
		}

		// Double-uniqueness handler: same key, or same external id.
		if prev, err := db.Tx().FindByProviderKey(in.ProviderID, in.IdempotencyKey); err != nil {
			return err
		} else if prev != nil {
			if str(prev.PayloadHash) != hash {
				return &ConflictError{Code: CodeConflict, Message: "idempotency key with divergent content"}
			}
			result = replayResult(prev)
			return nil
		}
		if prev, err := db.Tx().FindByProviderExternal(in.ProviderID, in.ExternalID); err != nil {
			return err
		} else if prev != nil {
			if str(prev.PayloadHash) != hash {
				return &ConflictError{Code: CodeConflict, Message: "external id with divergent content"}
			}
			result = replayResult(prev)
			return nil
		}

		r, processedRef, err := s.applyOperation(db, operationContext{
			in: in, amount: amount, hash: hash, wallet: wallet,
		})
		if err != nil {
			return err
		}
		result = r

		if in.MessageID != "" {
			if err := db.Aux().InsertInbox(in.ConsumerName, in.MessageID, hash); err != nil {
				return err
			}
			if err := db.Aux().CompleteInbox(in.ConsumerName, in.MessageID); err != nil {
				return err
			}
		}
		_ = processedRef
		return nil
	}); err != nil {
		return nil, err
	}

	// Immediate re-evaluation: when this submit processed a BET (or any
	// external reference target), dependents parked on it retry now in a
	// follow-up transaction; polling covers whatever remains.
	if result != nil && result.Outcome == OutcomeProcessed &&
		(resultIsReferenceTarget(in.Kind)) {
		_ = s.ReevaluateDependents(in.ProviderID, in.ExternalID)
	}
	return result, nil
}

func resultIsReferenceTarget(k domain.TransactionKind) bool {
	return k == domain.KindBet
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// replayResult rebuilds the original outcome without reapplying money.
func replayResult(prev *ports.TxRecord) *SubmitResult {
	r := &SubmitResult{
		Outcome: OutcomeReplay, TransactionID: prev.ID,
		FailureCode: str(prev.FailureCode), IdempotentReplay: true,
	}
	if prev.WalletVersionObserved != nil {
		r.WalletVersion = *prev.WalletVersionObserved
	}
	if prev.ResultBalanceMinor != nil && prev.ResultCurrency != nil {
		if m, err := domain.NewMoneyFromMinor(*prev.ResultBalanceMinor, *prev.ResultCurrency); err == nil {
			r.Balance = &m
		}
	}
	switch prev.State {
	case string(domain.StateProcessed):
		// replay keeps OutcomeReplay (adapters answer 200 with processed body)
	case string(domain.StateRejected):
		// failure code preserved above
	default:
		r.Outcome = OutcomePending
		r.Balance = nil
	}
	return r
}

type operationContext struct {
	in     SubmitInput
	amount domain.Money
	hash   string
	wallet *ports.WalletRecord
}

// applyOperation runs the per-kind domain flow inside the open transaction
// and persists tx + ledger + outbox (+ work) rows. It returns the result
// and, for successful reference targets, nothing extra (callers reevaluate).
func (s *Service) applyOperation(db ports.DB, oc operationContext) (*SubmitResult, bool, error) {
	in := oc.in
	txID := s.newID()
	now := s.now()

	w, err := domain.RehydrateWallet(oc.wallet.ID, oc.wallet.PlayerID, oc.wallet.Currency, oc.wallet.BalanceMinor, oc.wallet.Version)
	if err != nil {
		return nil, false, err
	}

	insertTx := func(state domain.TransactionState, failureCode string, resultBal *domain.Money, refTxID string) (*ports.TxRecord, error) {
		rec := &ports.TxRecord{
			ID: txID, Origin: "EXTERNAL",
			ProviderID: &in.ProviderID, ExternalID: &in.ExternalID,
			IdempotencyKey: &in.IdempotencyKey, PayloadHash: &oc.hash,
			WalletID: in.WalletID, PlayerID: in.PlayerID,
			RoundID: &in.RoundID, GameID: &in.GameID, Kind: string(in.Kind),
			AmountMinor: oc.amount.AmountMinor(), Currency: oc.amount.Currency(),
			State: string(state),
		}
		if in.ReferenceExternal != "" {
			rec.ReferenceExternalID = &in.ReferenceExternal
		}
		if refTxID != "" {
			rec.ReferenceTxID = &refTxID
		}
		if failureCode != "" {
			rec.FailureCode = &failureCode
		}
		if resultBal != nil {
			bm, bc := resultBal.AmountMinor(), resultBal.Currency()
			rec.ResultBalanceMinor = &bm
			rec.ResultCurrency = &bc
			rec.WalletVersionObserved = &[]int64{w.Version()}[0]
		}
		if err := db.Tx().Insert(rec); err != nil {
			return nil, err
		}
		return rec, nil
	}

	processed := func(before, after domain.Money, refTxID string, dir domain.LedgerDirection) (*SubmitResult, error) {
		entry, err := domain.NewLedgerEntry(s.newID(), w.ID(), txID, dir, oc.amount, before, after, w.Version())
		if err != nil {
			return nil, err
		}
		if _, err := insertTx(domain.StateProcessed, "", &after, refTxID); err != nil {
			return nil, err
		}
		if err := db.Ledger().Append(toLedgerRecord(entry)); err != nil {
			return nil, err
		}
		if err := db.Wallet().UpdateBalance(w.ID(), after.AmountMinor(), w.Version()); err != nil {
			return nil, err
		}
		afterCopy := after
		if err := s.emitProcessed(db, w, txID, in, after, before, dir); err != nil {
			return nil, err
		}
		return &SubmitResult{Outcome: OutcomeProcessed, TransactionID: txID,
			Balance: &afterCopy, WalletVersion: w.Version()}, nil
	}

	rejected := func(code string, refTxID string) (*SubmitResult, error) {
		bal := w.Balance()
		if _, err := insertTx(domain.StateRejected, code, &bal, refTxID); err != nil {
			return nil, err
		}
		if err := s.emitRejected(db, w, txID, in, bal, code); err != nil {
			return nil, err
		}
		balCopy := bal
		return &SubmitResult{Outcome: OutcomeRejected, TransactionID: txID,
			Balance: &balCopy, FailureCode: code, WalletVersion: w.Version()}, nil
	}

	parkReference := func() (*SubmitResult, error) {
		if _, err := insertTx(domain.StatePendingReference, "", nil, ""); err != nil {
			return nil, err
		}
		if err := s.emitPendingRef(db, w, txID, in); err != nil {
			return nil, err
		}
		if err := db.Aux().EnqueueWork(&ports.WorkItem{
			TransactionID: txID, Kind: "reference",
			NextAttemptAt: now.Add(BackoffBase),
		}); err != nil {
			return nil, err
		}
		return &SubmitResult{Outcome: OutcomePending, TransactionID: txID,
			WalletVersion: w.Version()}, nil
	}

	switch in.Kind {
	case domain.KindBet:
		before, after, err := w.Debit(in.PlayerID, oc.amount)
		if err != nil {
			if errors.Is(err, domain.ErrInsufficientBalance) {
				r, rerr := rejected(domain.CodeInsufficientBalance, "")
				return r, false, rerr
			}
			return nil, false, err
		}
		r, rerr := processed(before, after, "", domain.DirectionDebit)
		return r, false, rerr

	case domain.KindWin:
		if in.ReferenceExternal == "" {
			before, after, err := w.Credit(in.PlayerID, oc.amount)
			if err != nil {
				return nil, false, err
			}
			r, rerr := processed(before, after, "", domain.DirectionCredit)
			return r, false, rerr
		}
		return s.applyWithReference(db, oc, w, txID, func(target *ports.TxRecord) (*SubmitResult, error) {
			bet := referenceView(target)
			op := winOperation(in, oc.amount)
			if err := domain.ValidateWinReference(op, bet); err != nil {
				return rejected(domain.CodeOf(err), target.ID)
			}
			before, after, err := w.Credit(in.PlayerID, oc.amount)
			if err != nil {
				return nil, err
			}
			return processed(before, after, target.ID, domain.DirectionCredit)
		}, parkReference, rejected)

	case domain.KindLoss:
		// No movement, no ledger, no version bump; still a processed event.
		bal := w.Balance()
		if _, err := insertTx(domain.StateProcessed, "", &bal, ""); err != nil {
			return nil, false, err
		}
		if err := s.emitProcessed(db, w, txID, in, bal, bal, ""); err != nil {
			return nil, false, err
		}
		balCopy := bal
		return &SubmitResult{Outcome: OutcomeProcessed, TransactionID: txID,
			Balance: &balCopy, WalletVersion: w.Version()}, false, nil

	case domain.KindRefund, domain.KindRollback:
		return s.applyWithReference(db, oc, w, txID, func(target *ports.TxRecord) (*SubmitResult, error) {
			op := reversalOperation(in, oc.amount)
			ref := referenceView(target)
			if err := domain.ValidateReferenceAgreement(op, *ref); err != nil {
				return rejected(domain.CodeOf(err), target.ID)
			}
			targetDomain := domain.RehydrateTransaction(target.ID,
				domain.TransactionKind(target.Kind), domain.TransactionState(target.State),
				mustMinor(target.AmountMinor, target.Currency), target.PlayerID, target.WalletID)
			if target.ReversedByTxID != nil {
				targetDomain.MarkReversed(str(target.ReversedByKind), str(target.ReversedByTxID))
			}
			var activeWin bool
			if target.Kind == string(domain.KindBet) {
				var err error
				activeWin, err = db.Tx().HasActiveWin(target.ID)
				if err != nil {
					return nil, err
				}
			}
			if err := domain.ValidateReversalTarget(in.Kind, targetDomain, activeWin); err != nil {
				return rejected(domain.CodeOf(err), target.ID)
			}
			var before, after domain.Money
			var dir domain.LedgerDirection
			var err error
			switch {
			case in.Kind == domain.KindRefund:
				dir = domain.DirectionCredit
				before, after, err = w.Credit(in.PlayerID, oc.amount)
			case target.Kind == string(domain.KindBet):
				dir = domain.DirectionCredit
				before, after, err = w.Credit(in.PlayerID, oc.amount)
			default: // ROLLBACK of WIN / REFUND voids: debit
				dir = domain.DirectionDebit
				before, after, err = w.Debit(in.PlayerID, oc.amount)
				if err != nil && errors.Is(err, domain.ErrInsufficientBalance) {
					return rejected(domain.CodeRollbackInsufficientBalance, target.ID)
				}
			}
			if err != nil {
				return nil, err
			}
			r, err := processed(before, after, target.ID, dir)
			if err != nil {
				return nil, err
			}
			if err := db.Tx().MarkReversed(target.ID, string(in.Kind), txID); err != nil {
				return nil, err
			}
			// Supported chain: ROLLBACK of a REFUND voids it and frees the
			// original BET for a future reversal.
			if in.Kind == domain.KindRollback && target.Kind == string(domain.KindRefund) && target.ReferenceTxID != nil {
				if err := db.Tx().ClearReversal(*target.ReferenceTxID); err != nil {
					return nil, err
				}
			}
			return r, nil
		}, parkReference, rejected)

	default:
		return nil, false, correctable(CodeInvalidReq, "unknown kind")
	}
}

// applyWithReference resolves the reference and dispatches: missing or still
// pending goes to park; terminally bad goes to fail; good goes to apply.
func (s *Service) applyWithReference(db ports.DB, oc operationContext, w interface {
	ID() string
	Balance() domain.Money
	Version() int64
}, txID string, apply func(*ports.TxRecord) (*SubmitResult, error),
	park func() (*SubmitResult, error), fail func(string, string) (*SubmitResult, error)) (*SubmitResult, bool, error) {
	in := oc.in
	target, err := db.Tx().FindByProviderExternal(in.ProviderID, in.ReferenceExternal)
	if err != nil {
		return nil, false, err
	}
	if target == nil {
		r, err := park()
		return r, false, err
	}
	switch domain.TransactionState(target.State) {
	case domain.StatePending, domain.StatePendingReference:
		r, err := park()
		return r, false, err
	case domain.StateProcessed:
		r, err := apply(target)
		return r, false, err
	default: // REJECTED, FAILED
		r, err := fail(domain.CodeReferenceNotProcessed, target.ID)
		return r, false, err
	}
}

func winOperation(in SubmitInput, amount domain.Money) domain.Operation {
	return domain.Operation{Kind: in.Kind, Amount: amount, ProviderID: in.ProviderID,
		PlayerID: in.PlayerID, WalletID: in.WalletID, RoundID: in.RoundID}
}

func reversalOperation(in SubmitInput, amount domain.Money) domain.Operation {
	return domain.Operation{Kind: in.Kind, Amount: amount, ProviderID: in.ProviderID,
		PlayerID: in.PlayerID, WalletID: in.WalletID, RoundID: in.RoundID}
}

func referenceView(t *ports.TxRecord) *domain.Reference {
	amt, _ := domain.NewMoneyFromMinor(t.AmountMinor, t.Currency)
	return &domain.Reference{
		Kind: domain.TransactionKind(t.Kind), Amount: amt,
		ProviderID: str(t.ProviderID), PlayerID: t.PlayerID, WalletID: t.WalletID,
		RoundID: str(t.RoundID), State: domain.TransactionState(t.State),
		Reversed: t.ReversedByTxID != nil,
	}
}

func mustMinor(minor int64, cur string) domain.Money {
	m, err := domain.NewMoneyFromMinor(minor, cur)
	if err != nil {
		panic(err)
	}
	return m
}

func toLedgerRecord(e *domain.LedgerEntry) *ports.LedgerRecord {
	return &ports.LedgerRecord{
		ID: e.ID(), WalletID: e.WalletID(), TransactionID: e.TransactionID(),
		Direction: string(e.Direction()), AmountMinor: e.Amount().AmountMinor(),
		Currency: e.Amount().Currency(), BeforeMinor: e.BalanceBefore().AmountMinor(),
		AfterMinor: e.BalanceAfter().AmountMinor(), WalletVersion: e.WalletVersion(),
	}
}
