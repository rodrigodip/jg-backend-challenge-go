package wagering

import (
	"encoding/json"
	"time"

	"github.com/jg-backend-challenge/wallet/internal/domain"
	"github.com/jg-backend-challenge/wallet/internal/ports"
)

// mustEventPayload builds the stable outbox envelope: eventId, eventType,
// aggregateId (= walletId), correlationId, occurredAt UTC RFC 3339, version
// (= walletVersion, current when no bump) and typed data as an immutable
// snapshot with decimal strings. No event is emitted for FAILED.
func mustEventPayload(eventID, eventType, walletID, correlationID string, now time.Time, walletVersion int64, data map[string]any) []byte {
	env := map[string]any{
		"eventId":       eventID,
		"eventType":     eventType,
		"aggregateId":   walletID,
		"correlationId": correlationID,
		"occurredAt":    now.UTC().Format(time.RFC3339),
		"version":       walletVersion,
		"data":          data,
	}
	raw, err := json.Marshal(env)
	if err != nil {
		panic("wagering: event marshal: " + err.Error())
	}
	return raw
}

// emitProcessed stages WagerTransactionProcessed and, only on effective
// balance change, WalletBalanceChanged in the same commit.
func (s *Service) emitProcessed(db ports.DB, w interface {
	ID() string
	Version() int64
}, txID string, in SubmitInput, after, before domain.Money, dir domain.LedgerDirection) error {
	now := s.now()
	procData := map[string]any{
		"transactionId": txID, "kind": string(in.Kind),
		"providerId": in.ProviderID, "externalTransactionId": in.ExternalID,
		"playerId": in.PlayerID, "walletId": w.ID(),
		"amount": in.AmountText, "currency": in.Currency,
		"balance": after.String(), "walletVersion": w.Version(),
	}
	if err := db.Aux().EnqueueOutbox(&ports.OutboxRecord{
		EventID: s.newID(), AggregateID: w.ID(), EventType: EventProcessed,
		Payload: mustEventPayload(s.newID(), EventProcessed, w.ID(), in.CorrelationID, now, w.Version(), procData),
	}); err != nil {
		return err
	}
	if dir == "" {
		return nil // LOSS / no-movement: no balance event
	}
	balData := map[string]any{
		"walletId": w.ID(), "transactionId": txID, "direction": string(dir),
		"money":         map[string]any{"amount": in.AmountText, "currency": in.Currency},
		"balanceBefore": before.String(), "balanceAfter": after.String(),
		"walletVersion": w.Version(),
	}
	return db.Aux().EnqueueOutbox(&ports.OutboxRecord{
		EventID: s.newID(), AggregateID: w.ID(), EventType: EventBalance,
		Payload: mustEventPayload(s.newID(), EventBalance, w.ID(), in.CorrelationID, now, w.Version(), balData),
	})
}

// emitRejected stages WagerTransactionRejected with the definitive code.
func (s *Service) emitRejected(db ports.DB, w interface {
	ID() string
	Version() int64
}, txID string, in SubmitInput, balance domain.Money, code string) error {
	data := map[string]any{
		"transactionId": txID, "kind": string(in.Kind),
		"providerId": in.ProviderID, "externalTransactionId": in.ExternalID,
		"playerId": in.PlayerID, "walletId": w.ID(),
		"amount": in.AmountText, "currency": in.Currency,
		"failureCode": code, "balance": balance.String(), "walletVersion": w.Version(),
	}
	return db.Aux().EnqueueOutbox(&ports.OutboxRecord{
		EventID: s.newID(), AggregateID: w.ID(), EventType: EventRejected,
		Payload: mustEventPayload(s.newID(), EventRejected, w.ID(), in.CorrelationID, nowOf(s), w.Version(), data),
	})
}

// emitPendingRef stages WagerTransactionPendingReference.
func (s *Service) emitPendingRef(db ports.DB, w interface {
	ID() string
	Version() int64
}, txID string, in SubmitInput) error {
	data := map[string]any{
		"transactionId": txID, "kind": string(in.Kind),
		"providerId": in.ProviderID, "externalTransactionId": in.ExternalID,
		"playerId": in.PlayerID, "walletId": w.ID(),
		"referenceExternalTransactionId": in.ReferenceExternal,
		"walletVersion":                  w.Version(),
	}
	return db.Aux().EnqueueOutbox(&ports.OutboxRecord{
		EventID: s.newID(), AggregateID: w.ID(), EventType: EventPendingRef,
		Payload: mustEventPayload(s.newID(), EventPendingRef, w.ID(), in.CorrelationID, nowOf(s), w.Version(), data),
	})
}

func nowOf(s *Service) time.Time { return s.now() }
