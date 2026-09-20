package wagering

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jg-backend-challenge/wallet/internal/domain"
	"github.com/jg-backend-challenge/wallet/internal/ports"
)

// OpenWallet creates a wallet and, for a positive opening, its internal
// OPENING transaction + credit ledger entry + two events in the same commit.
// A zero opening creates the wallet alone. A second wallet for the same
// (player, currency) is a 409 conflict via the unique constraint.
func (s *Service) OpenWallet(playerID, currency, initialText string) (*ports.WalletRecord, error) {
	initial, err := domain.ParseMoney(initialText, currency)
	if err != nil {
		return nil, correctable(domain.CodeInvalidMoney, err.Error())
	}
	if initial.IsNegative() {
		return nil, correctable(domain.CodeInvalidMoney, "negative opening")
	}
	walletID := s.newID()
	var out *ports.WalletRecord
	if err := s.DB.Transact(func(db ports.DB) error {
		w, err := domain.NewWallet(walletID, playerID, currency, initial)
		if err != nil {
			return correctable(domain.CodeInvalidMoney, err.Error())
		}
		rec := &ports.WalletRecord{ID: w.ID(), PlayerID: w.PlayerID(),
			Currency: w.Currency(), BalanceMinor: w.Balance().AmountMinor(), Version: w.Version()}
		if err := db.Wallet().Insert(rec); err != nil {
			if isUniqueViolation(err) {
				return &ConflictError{Code: CodeConflict, Message: "wallet already exists for player and currency"}
			}
			return err
		}
		if initial.IsZero() {
			out = rec
			return nil
		}
		// Positive opening: OPENING (internal) + ledger + 2 events, same commit.
		txID := s.newID()
		now := s.now()
		zero, _ := domain.NewMoneyFromMinor(0, currency)
		before := zero
		after := initial
		entry, err := domain.NewLedgerEntry(s.newID(), walletID, txID,
			domain.DirectionCredit, initial, before, after, 1)
		if err != nil {
			return err
		}
		bm, bc := after.AmountMinor(), after.Currency()
		if err := db.Tx().Insert(&ports.TxRecord{
			ID: txID, Origin: "INTERNAL", WalletID: walletID, PlayerID: playerID,
			Kind: string(domain.KindOpening), AmountMinor: initial.AmountMinor(), Currency: currency,
			State:              string(domain.StateProcessed),
			ResultBalanceMinor: &bm, ResultCurrency: &bc,
			WalletVersionObserved: &[]int64{1}[0],
		}); err != nil {
			return err
		}
		if err := db.Ledger().Append(toLedgerRecord(entry)); err != nil {
			return err
		}
		procData := map[string]any{
			"transactionId": txID, "kind": "OPENING",
			"playerId": playerID, "walletId": walletID,
			"amount": initialText, "currency": currency,
			"balance": after.String(), "walletVersion": int64(1),
		}
		if err := db.Aux().EnqueueOutbox(&ports.OutboxRecord{
			EventID: s.newID(), AggregateID: walletID, EventType: EventProcessed,
			Payload: mustEventPayload(s.newID(), EventProcessed, walletID, "", now, 1, procData),
		}); err != nil {
			return err
		}
		balData := map[string]any{
			"walletId": walletID, "transactionId": txID, "direction": "CREDIT",
			"money":         map[string]any{"amount": initialText, "currency": currency},
			"balanceBefore": zero.String(), "balanceAfter": after.String(),
			"walletVersion": int64(1),
		}
		if err := db.Aux().EnqueueOutbox(&ports.OutboxRecord{
			EventID: s.newID(), AggregateID: walletID, EventType: EventBalance,
			Payload: mustEventPayload(s.newID(), EventBalance, walletID, "", now, 1, balData),
		}); err != nil {
			return err
		}
		out = rec
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}

// isUniqueViolation reports Postgres 23505 through GORM/pgx wrapping.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
