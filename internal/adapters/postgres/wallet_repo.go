package postgres

import (
	"errors"

	"github.com/jg-backend-challenge/wallet/internal/ports"
	"gorm.io/gorm"
)

type walletRepo struct{ db *gorm.DB }

// LockForUpdate serializes concurrent operations on one wallet row.
// Malformed ids (non-UUID text against the uuid PK) read as absent, so
// callers map them to 400/404 instead of 503.
func (r *walletRepo) LockForUpdate(walletID string) (*ports.WalletRecord, error) {
	var m WalletModel
	if err := lockUpdate(r.db).Where("id = ?", walletID).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || isInvalidInput(err) {
			return nil, nil
		}
		return nil, err
	}
	return &ports.WalletRecord{
		ID: m.ID, PlayerID: m.PlayerID, Currency: m.Currency,
		BalanceMinor: m.BalanceMinor, Version: m.Version,
	}, nil
}

// Get reads a wallet without locking (queries, reconciliation snapshots).
func (r *walletRepo) Get(walletID string) (*ports.WalletRecord, error) {
	var m WalletModel
	if err := r.db.Where("id = ?", walletID).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || isInvalidInput(err) {
			return nil, nil
		}
		return nil, err
	}
	return &ports.WalletRecord{
		ID: m.ID, PlayerID: m.PlayerID, Currency: m.Currency,
		BalanceMinor: m.BalanceMinor, Version: m.Version,
	}, nil
}

// Insert creates a wallet row (creation path only).
func (r *walletRepo) Insert(w *ports.WalletRecord) error {
	return r.db.Create(&WalletModel{
		ID: w.ID, PlayerID: w.PlayerID, Currency: w.Currency,
		BalanceMinor: w.BalanceMinor, Version: w.Version,
	}).Error
}

// UpdateBalance writes balance+version after a domain debit/credit.
// Explicit column update: never Save, never touches identity columns.
func (r *walletRepo) UpdateBalance(walletID string, balanceMinor, version int64) error {
	return r.db.Model(&WalletModel{}).Where("id = ?", walletID).Updates(map[string]any{
		"balance_minor": balanceMinor,
		"version":       version,
	}).Error
}
