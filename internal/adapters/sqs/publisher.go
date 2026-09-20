package sqs

import (
	"context"
	"log/slog"
	"math/rand"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/jg-backend-challenge/wallet/internal/ports"
	"github.com/jg-backend-challenge/wallet/internal/wagering"
)

// Publisher sends one outbox event to the broker.
type Publisher interface {
	Publish(ctx context.Context, e *ports.OutboxRecord) error
}

// SQSPublisher publishes to wager-events.fifo with MessageGroupId set to
// the aggregate (wallet, preserving per-wallet order) and
// MessageDeduplicationId set to the stable eventId, so a republication
// after a crash converges instead of duplicating.
type SQSPublisher struct {
	Client    *sqs.Client
	EventsURL string
	Log       *slog.Logger
}

// Publish sends one event, preserving its stable eventId.
func (p *SQSPublisher) Publish(ctx context.Context, e *ports.OutboxRecord) error {
	body := string(e.Payload)
	_, err := p.Client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:               &p.EventsURL,
		MessageBody:            &body,
		MessageGroupId:         &e.AggregateID,
		MessageDeduplicationId: &e.EventID,
	})
	if err != nil && p.Log != nil {
		p.Log.Error("outbox publish failed", "eventId", e.EventID, "err", err)
	}
	return err
}

// PublishDueOutbox claims due outbox rows for owner and publishes each. A
// failed send nacks with backoff and a cleared lease, so the next tick or
// another instance resumes with the same eventId. A crash between send and
// mark leaves the lease to expire; the redelivery carries the same eventId
// and the consumer dedupes by it.
func PublishDueOutbox(ctx context.Context, db ports.DB, pub Publisher, owner string, limit int) (int, error) {
	now := time.Now().UTC()
	records, err := db.Aux().ClaimOutbox(owner, wagering.OutboxLeaseTTL, limit)
	if err != nil {
		return 0, err
	}
	rnd := rand.New(rand.NewSource(now.UnixNano()))
	published := 0
	for _, rec := range records {
		if err := pub.Publish(ctx, rec); err != nil {
			outboxPublishErrors.Inc()
			backoff := now.Add(wagering.BackoffFullJitter(rec.Attempts+1, wagering.BackoffBase, wagering.BackoffCap, rnd))
			if nerr := db.Aux().NackOutbox(rec.EventID, rec.Attempts+1, backoff); nerr != nil {
				return published, nerr
			}
			continue
		}
		if err := db.Aux().MarkOutboxPublished(rec.EventID); err != nil {
			return published, err
		}
		outboxPublished.Inc()
		if delay := now.Sub(rec.OccurredAt); delay >= 0 {
			outboxPublishDelay.Observe(delay.Seconds())
		}
		published++
	}
	return published, nil
}

// RunOutboxPublisher ticks PublishDueOutbox until ctx is done (workers role
// owns the loop; 500ms is the D4 polling fallback).
func RunOutboxPublisher(ctx context.Context, db ports.DB, pub Publisher, owner string, interval time.Duration, log *slog.Logger) {
	tick(ctx, interval, log, "outbox publisher", func() {
		if _, err := PublishDueOutbox(ctx, db, pub, owner, 100); err != nil && log != nil {
			log.Error("outbox publish tick failed", "err", err)
		}
	})
}

// RunReferenceWorker ticks the parked-reference resumption until ctx is
// done. Immediate re-evaluation on reference processing (Submit path) covers
// the fast lane; this loop is the fallback for crashed or slow instances.
func RunReferenceWorker(ctx context.Context, svc *wagering.Service, owner string, interval time.Duration, log *slog.Logger) {
	tick(ctx, interval, log, "reference worker", func() {
		if _, err := svc.ProcessDueWork(owner, 100); err != nil && log != nil {
			log.Error("reference work tick failed", "err", err)
		}
	})
}

func tick(ctx context.Context, interval time.Duration, log *slog.Logger, name string, fn func()) {
	if log != nil {
		log.Info("worker loop started", "worker", name)
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			if log != nil {
				log.Info("worker loop stopped", "worker", name)
			}
			return
		case <-t.C:
			fn()
		}
	}
}
