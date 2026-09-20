package wagering

import (
	"errors"
	"math/rand"
	"time"

	"github.com/jg-backend-challenge/wallet/internal/domain"
	"github.com/jg-backend-challenge/wallet/internal/ports"
)

// ReevaluateDependents retries PENDING_REFERENCE transactions parked on
// (providerID, externalID) now that the reference processed. It runs in a
// follow-up transaction (polling remains the fallback); dependents on other
// wallets are left for the worker to avoid cross-wallet lock ordering.
func (s *Service) ReevaluateDependents(providerID, externalID string) error {
	return s.DB.Transact(func(db ports.DB) error {
		deps, err := db.Tx().FindDependents(providerID, externalID)
		if err != nil {
			return err
		}
		for _, dep := range deps {
			if dep.State != string(domain.StatePendingReference) {
				continue
			}
			if _, err := s.resumeOne(db, dep); err != nil {
				// One stuck dependent must not block its siblings.
				continue
			}
		}
		return nil
	})
}

// ProcessDueWork claims due work items for owner and advances each: missing
// references back off until TTL/attempts exhaust into REFERENCE_NOT_FOUND;
// still-pending references wait; resolved references process inline in the
// same wallet lock; infrastructure errors consume the infra budget into
// FAILED (no event, per spec). It returns the number of items settled.
func (s *Service) ProcessDueWork(owner string, limit int) (int, error) {
	items, err := s.DB.Aux().ClaimWork(owner, WorkLeaseTTL, limit)
	if err != nil {
		return 0, err
	}
	settled := 0
	for _, item := range items {
		ok, err := s.processWorkItem(item, owner)
		if err != nil {
			continue
		}
		if ok {
			settled++
		}
	}
	return settled, nil
}

func (s *Service) processWorkItem(item *ports.WorkItem, owner string) (bool, error) {
	_ = owner
	var settled bool
	err := s.DB.Transact(func(db ports.DB) error {
		rec, err := db.Tx().Get(item.TransactionID)
		if err != nil {
			return s.noteInfra(db, item, err)
		}
		if rec == nil || domain.TransactionState(rec.State).IsTerminal() {
			_ = db.Aux().DeleteWork(item.TransactionID)
			settled = true
			return nil
		}
		done, err := s.resumeOne(db, rec)
		if err != nil {
			return s.noteInfra(db, item, err)
		}
		if done {
			settled = true
		}
		return nil
	})
	return settled, err
}

// noteInfra consumes one infra attempt with backoff; past the budget the
// transaction finalizes FAILED without events.
func (s *Service) noteInfra(db ports.DB, item *ports.WorkItem, cause error) error {
	next := item.InfraAttempts + 1
	if next >= MaxInfraAttempts {
		rec, err := db.Tx().Get(item.TransactionID)
		if err != nil {
			return err
		}
		if rec != nil {
			rec.State = string(domain.StateFailed)
			fc := "INFRA_EXHAUSTED"
			rec.FailureCode = &fc
			if err := db.Tx().UpdateState(rec); err != nil {
				return err
			}
		}
		return db.Aux().DeleteWork(item.TransactionID)
	}
	r := rand.New(rand.NewSource(s.now().UnixNano()))
	return db.Aux().TouchWork(item.TransactionID, item.RefAttempts, next,
		s.now().Add(BackoffFullJitter(next, BackoffBase, BackoffCap, r)), false)
}

// resumeOne advances a single PENDING_REFERENCE transaction. It returns true
// when the item settled terminally (work row removed).
func (s *Service) resumeOne(db ports.DB, rec *ports.TxRecord) (bool, error) {
	now := s.now()
	wallet, err := db.Wallet().LockForUpdate(rec.WalletID)
	if err != nil {
		return false, err
	}
	if wallet == nil {
		return false, errors.New("wallet vanished")
	}
	w, err := domain.RehydrateWallet(wallet.ID, wallet.PlayerID, wallet.Currency, wallet.BalanceMinor, wallet.Version)
	if err != nil {
		return false, err
	}
	amount, err := domain.NewMoneyFromMinor(rec.AmountMinor, rec.Currency)
	if err != nil {
		return false, err
	}
	in := SubmitInput{
		ProviderID: str(rec.ProviderID), ExternalID: str(rec.ExternalID),
		PlayerID: rec.PlayerID, WalletID: rec.WalletID,
		RoundID: str(rec.RoundID), GameID: str(rec.GameID),
		Kind:       domain.TransactionKind(rec.Kind),
		AmountText: amount.String(), Currency: amount.Currency(),
		ReferenceExternal: str(rec.ReferenceExternalID),
	}
	r := rand.New(rand.NewSource(now.UnixNano()))
	backoff := func(attempts int) time.Time {
		return now.Add(BackoffFullJitter(attempts, BackoffBase, BackoffCap, r))
	}

	target, err := db.Tx().FindByProviderExternal(in.ProviderID, in.ReferenceExternal)
	if err != nil {
		return false, err
	}
	if target == nil {
		// TTL or attempt budget exhausted: definitive REFERENCE_NOT_FOUND.
		workAge := now.Sub(workCreatedAt(db, rec.ID))
		if workAge > ReferenceTTL || workRefAttempts(db, rec.ID)+1 >= MaxRefAttempts {
			if err := s.settleRejectedExisting(db, w, rec, in, "", CodeRefNotFound); err != nil {
				return false, err
			}
			return true, nil
		}
		return false, db.Aux().TouchWork(rec.ID, workRefAttempts(db, rec.ID)+1, 0, backoff(0), true)
	}
	switch domain.TransactionState(target.State) {
	case domain.StatePending, domain.StatePendingReference:
		return false, db.Aux().TouchWork(rec.ID, workRefAttempts(db, rec.ID)+1, 0, backoff(0), true)
	case domain.StateProcessed:
		return s.resumeApply(db, w, rec, in, amount, target)
	default:
		if err := s.settleRejectedExisting(db, w, rec, in, target.ID, domain.CodeReferenceNotProcessed); err != nil {
			return false, err
		}
		return true, nil
	}
}

// resumeApply runs the reference-agreement + matrix + movement flow for a
// parked transaction whose reference is now PROCESSED.
func (s *Service) resumeApply(db ports.DB, w *domain.Wallet, rec *ports.TxRecord, in SubmitInput, amount domain.Money, target *ports.TxRecord) (bool, error) {
	if in.Kind == domain.KindWin {
		bet := referenceView(target)
		if err := domain.ValidateWinReference(winOperation(in, amount), bet); err != nil {
			code := domain.CodeOf(err)
			if code == "" {
				code = domain.CodeReferenceNotProcessed
			}
			if err := s.settleRejectedExisting(db, w, rec, in, target.ID, code); err != nil {
				return false, err
			}
			return true, nil
		}
		before, after, err := w.Credit(in.PlayerID, amount)
		if err != nil {
			return false, err
		}
		if err := s.settleProcessedExisting(db, w, rec, in, target.ID, domain.DirectionCredit, before, after); err != nil {
			return false, err
		}
		return true, nil
	}
	// REFUND / ROLLBACK.
	op := reversalOperation(in, amount)
	ref := referenceView(target)
	if err := domain.ValidateReferenceAgreement(op, *ref); err != nil {
		if err := s.settleRejectedExisting(db, w, rec, in, target.ID, domain.CodeOf(err)); err != nil {
			return false, err
		}
		return true, nil
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
			return false, err
		}
	}
	if err := domain.ValidateReversalTarget(in.Kind, targetDomain, activeWin); err != nil {
		if err := s.settleRejectedExisting(db, w, rec, in, target.ID, domain.CodeOf(err)); err != nil {
			return false, err
		}
		return true, nil
	}
	var before, after domain.Money
	var dir domain.LedgerDirection
	var err error
	switch {
	case in.Kind == domain.KindRefund:
		dir = domain.DirectionCredit
		before, after, err = w.Credit(in.PlayerID, amount)
	case target.Kind == string(domain.KindBet):
		dir = domain.DirectionCredit
		before, after, err = w.Credit(in.PlayerID, amount)
	default:
		dir = domain.DirectionDebit
		before, after, err = w.Debit(in.PlayerID, amount)
		if err != nil && errors.Is(err, domain.ErrInsufficientBalance) {
			if err := s.settleRejectedExisting(db, w, rec, in, target.ID, domain.CodeRollbackInsufficientBalance); err != nil {
				return false, err
			}
			return true, nil
		}
	}
	if err != nil {
		return false, err
	}
	if err := s.settleProcessedExisting(db, w, rec, in, target.ID, dir, before, after); err != nil {
		return false, err
	}
	if err := db.Tx().MarkReversed(target.ID, string(in.Kind), rec.ID); err != nil {
		return false, err
	}
	if in.Kind == domain.KindRollback && target.Kind == string(domain.KindRefund) && target.ReferenceTxID != nil {
		if err := db.Tx().ClearReversal(*target.ReferenceTxID); err != nil {
			return false, err
		}
	}
	return true, nil
}

// settleProcessedExisting finalizes a parked transaction as PROCESSED with
// ledger + wallet + events, then removes its work item.
func (s *Service) settleProcessedExisting(db ports.DB, w *domain.Wallet, rec *ports.TxRecord, in SubmitInput, refTxID string, dir domain.LedgerDirection, before, after domain.Money) error {
	amount, _ := domain.NewMoneyFromMinor(rec.AmountMinor, rec.Currency)
	entry, err := domain.NewLedgerEntry(s.newID(), w.ID(), rec.ID, dir, amount, before, after, w.Version())
	if err != nil {
		return err
	}
	rec.State = string(domain.StateProcessed)
	bm, bc := after.AmountMinor(), after.Currency()
	rec.ResultBalanceMinor = &bm
	rec.ResultCurrency = &bc
	rec.WalletVersionObserved = &[]int64{w.Version()}[0]
	if refTxID != "" {
		rec.ReferenceTxID = &refTxID
	}
	if err := db.Tx().UpdateState(rec); err != nil {
		return err
	}
	if err := db.Ledger().Append(toLedgerRecord(entry)); err != nil {
		return err
	}
	if err := db.Wallet().UpdateBalance(w.ID(), after.AmountMinor(), w.Version()); err != nil {
		return err
	}
	if err := s.emitProcessed(db, walletFace{w}, rec.ID, rebuiltInput(in, amount), after, before, dir); err != nil {
		return err
	}
	return db.Aux().DeleteWork(rec.ID)
}

// settleRejectedExisting finalizes a parked transaction as REJECTED with its
// rejection event, then removes its work item.
func (s *Service) settleRejectedExisting(db ports.DB, w *domain.Wallet, rec *ports.TxRecord, in SubmitInput, refTxID, code string) error {
	bal := w.Balance()
	rec.State = string(domain.StateRejected)
	rec.FailureCode = &code
	bm, bc := bal.AmountMinor(), bal.Currency()
	rec.ResultBalanceMinor = &bm
	rec.ResultCurrency = &bc
	rec.WalletVersionObserved = &[]int64{w.Version()}[0]
	if refTxID != "" {
		rec.ReferenceTxID = &refTxID
	}
	if err := db.Tx().UpdateState(rec); err != nil {
		return err
	}
	amount, _ := domain.NewMoneyFromMinor(rec.AmountMinor, rec.Currency)
	if err := s.emitRejected(db, walletFace{w}, rec.ID, rebuiltInput(in, amount), bal, code); err != nil {
		return err
	}
	return db.Aux().DeleteWork(rec.ID)
}

// walletFace adapts *domain.Wallet to the emit helpers' structural type.
type walletFace struct{ w *domain.Wallet }

func (f walletFace) ID() string     { return f.w.ID() }
func (f walletFace) Version() int64 { return f.w.Version() }

// rebuiltInput restores amount text/currency for event snapshots.
func rebuiltInput(in SubmitInput, amount domain.Money) SubmitInput {
	in.AmountText = amount.String()
	in.Currency = amount.Currency()
	return in
}

// workCreatedAt / workRefAttempts read scheduling metadata for TTL math.
// Missing rows keep the TTL conservative (expire).
func workCreatedAt(db ports.DB, txID string) time.Time {
	item, err := db.Aux().GetWork(txID)
	if err != nil || item == nil {
		return time.Time{}
	}
	return item.CreatedAt
}

func workRefAttempts(db ports.DB, txID string) int {
	item, err := db.Aux().GetWork(txID)
	if err != nil || item == nil {
		return 0
	}
	return item.RefAttempts
}
