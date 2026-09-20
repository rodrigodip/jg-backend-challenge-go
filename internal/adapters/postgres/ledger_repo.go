package postgres

import (
	"github.com/jg-backend-challenge/wallet/internal/ports"
	"gorm.io/gorm"
)

type ledgerRepo struct{ db *gorm.DB }

// Append inserts one immutable ledger entry. No update/delete path exists:
// the schema trigger rejects writes and the app role lacks the privilege.
func (r *ledgerRepo) Append(e *ports.LedgerRecord) error {
	return r.db.Create(&LedgerModel{
		ID: e.ID, WalletID: e.WalletID, TransactionID: e.TransactionID,
		Direction: e.Direction, AmountMinor: e.AmountMinor, Currency: e.Currency,
		BeforeMinor: e.BeforeMinor, AfterMinor: e.AfterMinor,
		WalletVersion: e.WalletVersion,
	}).Error
}

// Page returns entries after afterVersion (exclusive), ordered by the
// per-wallet sequence, bounded by limit. The cursor is the last seen
// wallet_version; pagination never skips or duplicates under concurrency
// because versions are unique per wallet and monotonically assigned.
func (r *ledgerRepo) Page(walletID string, afterVersion int64, limit int) ([]*ports.LedgerRecord, error) {
	var ms []LedgerModel
	if err := r.db.Where("wallet_id = ? AND wallet_version > ?", walletID, afterVersion).
		Order("wallet_version ASC").Limit(limit).Find(&ms).Error; err != nil {
		return nil, err
	}
	return toLedgerRecords(ms), nil
}

// AllOrdered returns every entry for reconciliation, in sequence order.
func (r *ledgerRepo) AllOrdered(walletID string) ([]*ports.LedgerRecord, error) {
	var ms []LedgerModel
	if err := r.db.Where("wallet_id = ?", walletID).
		Order("wallet_version ASC").Find(&ms).Error; err != nil {
		return nil, err
	}
	return toLedgerRecords(ms), nil
}

func toLedgerRecords(ms []LedgerModel) []*ports.LedgerRecord {
	out := make([]*ports.LedgerRecord, 0, len(ms))
	for i := range ms {
		out = append(out, &ports.LedgerRecord{
			ID: ms[i].ID, WalletID: ms[i].WalletID, TransactionID: ms[i].TransactionID,
			Direction: ms[i].Direction, AmountMinor: ms[i].AmountMinor, Currency: ms[i].Currency,
			BeforeMinor: ms[i].BeforeMinor, AfterMinor: ms[i].AfterMinor,
			WalletVersion: ms[i].WalletVersion,
		})
	}
	return out
}
