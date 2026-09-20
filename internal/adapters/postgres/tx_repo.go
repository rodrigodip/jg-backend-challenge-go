package postgres

import (
	"errors"

	"github.com/jg-backend-challenge/wallet/internal/ports"
	"gorm.io/gorm"
)

type txRepo struct{ db *gorm.DB }

func toTxRecord(m *WagerTxModel) *ports.TxRecord {
	return &ports.TxRecord{
		ID: m.ID, Origin: m.Origin, ProviderID: m.ProviderID,
		ExternalID: m.ExternalID, IdempotencyKey: m.IdempotencyKey,
		PayloadHash: m.PayloadHash, WalletID: m.WalletID, PlayerID: m.PlayerID,
		RoundID: m.RoundID, GameID: m.GameID, Kind: m.Kind,
		AmountMinor: m.AmountMinor, Currency: m.Currency,
		ReferenceExternalID: m.ReferenceExternalID, ReferenceTxID: m.ReferenceTxID,
		State: m.State, FailureCode: m.FailureCode,
		ResultBalanceMinor: m.ResultBalanceMinor, ResultCurrency: m.ResultCurrency,
		ReversedByKind: m.ReversedByKind, ReversedByTxID: m.ReversedByTxID,
		WalletVersionObserved: m.WalletVersionObserved,
	}
}

// Insert persists a new transaction row (single insert, never Save).
func (r *txRepo) Insert(tx *ports.TxRecord) error {
	return r.db.Create(&WagerTxModel{
		ID: tx.ID, Origin: tx.Origin, ProviderID: tx.ProviderID,
		ExternalID: tx.ExternalID, IdempotencyKey: tx.IdempotencyKey,
		PayloadHash: tx.PayloadHash, WalletID: tx.WalletID, PlayerID: tx.PlayerID,
		RoundID: tx.RoundID, GameID: tx.GameID, Kind: tx.Kind,
		AmountMinor: tx.AmountMinor, Currency: tx.Currency,
		ReferenceExternalID: tx.ReferenceExternalID, ReferenceTxID: tx.ReferenceTxID,
		State: tx.State, FailureCode: tx.FailureCode,
		ResultBalanceMinor: tx.ResultBalanceMinor, ResultCurrency: tx.ResultCurrency,
		ReversedByKind: tx.ReversedByKind, ReversedByTxID: tx.ReversedByTxID,
		WalletVersionObserved: tx.WalletVersionObserved,
	}).Error
}

// Get fetches a transaction by id, or nil when absent.
func (r *txRepo) Get(id string) (*ports.TxRecord, error) {
	var m WagerTxModel
	if err := r.db.Where("id = ?", id).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toTxRecord(&m), nil
}

// FindByProviderKey resolves the financial idempotency key.
func (r *txRepo) FindByProviderKey(providerID, key string) (*ports.TxRecord, error) {
	var m WagerTxModel
	if err := r.db.Where("provider_id = ? AND idempotency_key = ?", providerID, key).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toTxRecord(&m), nil
}

// FindByProviderExternal resolves the external transaction identity.
func (r *txRepo) FindByProviderExternal(providerID, externalID string) (*ports.TxRecord, error) {
	var m WagerTxModel
	if err := r.db.Where("provider_id = ? AND external_transaction_id = ?", providerID, externalID).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toTxRecord(&m), nil
}

// FindDependents lists transactions referencing (provider, referenceExternal)
// for immediate re-evaluation when the reference processes.
func (r *txRepo) FindDependents(providerID, referenceExternalID string) ([]*ports.TxRecord, error) {
	var ms []WagerTxModel
	if err := r.db.Where("provider_id = ? AND reference_external_transaction_id = ?",
		providerID, referenceExternalID).Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]*ports.TxRecord, 0, len(ms))
	for i := range ms {
		out = append(out, toTxRecord(&ms[i]))
	}
	return out, nil
}

// UpdateState writes a state transition plus outcome columns.
// Column-scoped update: never Save, never rewrites immutable inputs.
func (r *txRepo) UpdateState(tx *ports.TxRecord) error {
	return r.db.Model(&WagerTxModel{}).Where("id = ?", tx.ID).Updates(map[string]any{
		"state":                    tx.State,
		"failure_code":             tx.FailureCode,
		"result_balance_minor":     tx.ResultBalanceMinor,
		"result_currency":          tx.ResultCurrency,
		"reference_transaction_id": tx.ReferenceTxID,
		"wallet_version_observed":  tx.WalletVersionObserved,
	}).Error
}

// MarkReversed records a successful direct reversal on the target.
func (r *txRepo) MarkReversed(targetID, byKind, byTxID string) error {
	return r.db.Model(&WagerTxModel{}).Where("id = ?", targetID).Updates(map[string]any{
		"reversed_by_kind":           byKind,
		"reversed_by_transaction_id": byTxID,
	}).Error
}

// ClearReversal frees the target slot when a ROLLBACK voids a REFUND.
func (r *txRepo) ClearReversal(targetID string) error {
	return r.db.Model(&WagerTxModel{}).Where("id = ?", targetID).Updates(map[string]any{
		"reversed_by_kind":           nil,
		"reversed_by_transaction_id": nil,
	}).Error
}

// HasActiveWin reports whether betTxID owns a dependent PROCESSED WIN that
// is not itself reversed (the BET_HAS_ACTIVE_WIN guard). A WIN depends on a
// BET via reference_transaction_id.
func (r *txRepo) HasActiveWin(betTxID string) (bool, error) {
	var n int64
	if err := r.db.Model(&WagerTxModel{}).
		Where("reference_transaction_id = ? AND kind = 'WIN' AND state = 'PROCESSED' AND reversed_by_transaction_id IS NULL", betTxID).
		Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}
