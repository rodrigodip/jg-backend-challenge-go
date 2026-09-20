package postgres

import (
	"errors"
	"time"

	"github.com/jg-backend-challenge/wallet/internal/ports"
	"gorm.io/gorm"
)

type auxRepo struct{ db *gorm.DB }

// InsertInbox records a consumed SQS message durably in the same commit as
// its treatment. The (consumer, message) primary key makes replays visible.
func (r *auxRepo) InsertInbox(consumer, messageID, hash string) error {
	return r.db.Create(&InboxModel{
		Consumer: consumer, MessageID: messageID, PayloadHash: hash,
	}).Error
}

// CompleteInbox marks a message treatment concluded.
func (r *auxRepo) CompleteInbox(consumer, messageID string) error {
	now := time.Now().UTC()
	return r.db.Model(&InboxModel{}).
		Where("consumer_name = ? AND message_id = ?", consumer, messageID).
		Update("concluded_at", now).Error
}

// FindInbox returns the stored hash and conclusion flag for a message.
func (r *auxRepo) FindInbox(consumer, messageID string) (string, bool, bool, error) {
	var m InboxModel
	if err := r.db.Where("consumer_name = ? AND message_id = ?", consumer, messageID).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", false, false, nil
		}
		return "", false, false, err
	}
	return m.PayloadHash, m.ConcludedAt != nil, true, nil
}

// EnqueueOutbox stages an immutable event payload for post-commit publish.
func (r *auxRepo) EnqueueOutbox(e *ports.OutboxRecord) error {
	return r.db.Create(&OutboxModel{
		EventID: e.EventID, AggregateID: e.AggregateID, EventType: e.EventType,
		Payload: e.Payload, Attempts: e.Attempts,
	}).Error
}

// ListUnpublished returns unpublished events for one aggregate, in send
// order. Read-only: used by tests and operational inspection, never by the
// publisher (which claims through ClaimOutbox).
func (r *auxRepo) ListUnpublished(aggregateID string) ([]*ports.OutboxRecord, error) {
	var ms []OutboxModel
	if err := r.db.Where("aggregate_id = ? AND published_at IS NULL", aggregateID).
		Order("occurred_at ASC").Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]*ports.OutboxRecord, 0, len(ms))
	for i := range ms {
		out = append(out, &ports.OutboxRecord{
			EventID: ms[i].EventID, AggregateID: ms[i].AggregateID,
			EventType: ms[i].EventType, Payload: ms[i].Payload, Attempts: ms[i].Attempts,
		})
	}
	return out, nil
}

// ClaimOutbox takes up to limit unpublished, due, unlocked-or-expired rows
// with SKIP LOCKED so concurrent publishers never double-claim. It sets the
// lease in the same statement batch.
func (r *auxRepo) ClaimOutbox(owner string, leaseTTL time.Duration, limit int) ([]*ports.OutboxRecord, error) {
	now := time.Now().UTC()
	var ms []OutboxModel
	if err := lockUpdateSkipLocked(r.db).
		Where("published_at IS NULL AND next_send_at <= ? AND (lease_expires_at IS NULL OR lease_expires_at <= ?)", now, now).
		Order("next_send_at ASC").Limit(limit).Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]*ports.OutboxRecord, 0, len(ms))
	expires := now.Add(leaseTTL)
	for i := range ms {
		if err := r.db.Model(&OutboxModel{}).Where("event_id = ?", ms[i].EventID).Updates(map[string]any{
			"lease_owner": owner, "lease_expires_at": expires,
		}).Error; err != nil {
			return nil, err
		}
		out = append(out, &ports.OutboxRecord{
			EventID: ms[i].EventID, AggregateID: ms[i].AggregateID,
			EventType: ms[i].EventType, Payload: ms[i].Payload, Attempts: ms[i].Attempts,
		})
	}
	return out, nil
}

// MarkOutboxPublished records a successful publish (mutable progress column).
func (r *auxRepo) MarkOutboxPublished(eventID string) error {
	now := time.Now().UTC()
	return r.db.Model(&OutboxModel{}).Where("event_id = ?", eventID).Updates(map[string]any{
		"published_at": now, "lease_owner": nil, "lease_expires_at": nil,
	}).Error
}

// EnqueueWork stages a pending acceptance for worker resumption.
func (r *auxRepo) EnqueueWork(w *ports.WorkItem) error {
	return r.db.Create(&WorkModel{
		TransactionID: w.TransactionID, Kind: w.Kind,
		RefAttempts: w.RefAttempts, InfraAttempts: w.InfraAttempts,
		NextAttemptAt: w.NextAttemptAt,
	}).Error
}

// GetWork fetches one work item with its scheduling metadata.
func (r *auxRepo) GetWork(txID string) (*ports.WorkItem, error) {
	var m WorkModel
	if err := r.db.Where("transaction_id = ?", txID).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &ports.WorkItem{
		TransactionID: m.TransactionID, Kind: m.Kind,
		RefAttempts: m.RefAttempts, InfraAttempts: m.InfraAttempts,
		NextAttemptAt:  m.NextAttemptAt,
		LeaseExpiresAt: m.LeaseExpiresAt, CreatedAt: m.CreatedAt,
	}, nil
}

// ClaimWork takes due work rows with SKIP LOCKED and leases them to owner.
func (r *auxRepo) ClaimWork(owner string, leaseTTL time.Duration, limit int) ([]*ports.WorkItem, error) {
	now := time.Now().UTC()
	var ms []WorkModel
	if err := lockUpdateSkipLocked(r.db).
		Where("next_attempt_at <= ? AND (lease_expires_at IS NULL OR lease_expires_at <= ?)", now, now).
		Order("next_attempt_at ASC").Limit(limit).Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]*ports.WorkItem, 0, len(ms))
	expires := now.Add(leaseTTL)
	for i := range ms {
		if err := r.db.Model(&WorkModel{}).Where("transaction_id = ?", ms[i].TransactionID).Updates(map[string]any{
			"lease_owner": owner, "lease_expires_at": expires,
		}).Error; err != nil {
			return nil, err
		}
		out = append(out, &ports.WorkItem{
			TransactionID: ms[i].TransactionID, Kind: ms[i].Kind,
			RefAttempts: ms[i].RefAttempts, InfraAttempts: ms[i].InfraAttempts,
			NextAttemptAt: ms[i].NextAttemptAt,
			LeaseOwner:    owner, LeaseExpiresAt: &expires, CreatedAt: ms[i].CreatedAt,
		})
	}
	return out, nil
}

// TouchWork advances attempt counters and reschedules, optionally clearing
// the lease so another instance resumes immediately.
func (r *auxRepo) TouchWork(txID string, refAttempts, infraAttempts int, nextAttemptAt time.Time, clearLease bool) error {
	updates := map[string]any{
		"ref_attempts": refAttempts, "infra_attempts": infraAttempts,
		"next_attempt_at": nextAttemptAt,
	}
	if clearLease {
		updates["lease_owner"] = nil
		updates["lease_expires_at"] = nil
	}
	return r.db.Model(&WorkModel{}).Where("transaction_id = ?", txID).Updates(updates).Error
}

// DeleteWork removes a finished work item (terminal transaction state).
func (r *auxRepo) DeleteWork(txID string) error {
	return r.db.Where("transaction_id = ?", txID).Delete(&WorkModel{}).Error
}
