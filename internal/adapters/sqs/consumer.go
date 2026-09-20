package sqs

import (
	"context"
	"errors"
	"log/slog"
	"math/rand"
	"strconv"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/jg-backend-challenge/wallet/internal/domain"
	"github.com/jg-backend-challenge/wallet/internal/obs"
	"github.com/jg-backend-challenge/wallet/internal/wagering"
	"go.opentelemetry.io/otel/attribute"
)

// ConsumerName is the durable inbox identity of the SQS consumer role.
const ConsumerName = "wallet-consumer"

const (
	// batchSize and maxHandlers bound one poll and its fan-out (spec: batch
	// of 10, 10 concurrent handlers per instance).
	batchSize   = 10
	maxHandlers = 10
	// visibilityTimeout matches the queue default: 30s of exclusive
	// treatment per delivery.
	visibilityTimeout = 30
	// shutdownGrace bounds SIGTERM drain: in-flight handlers finish, the
	// rest redeliver safely through the inbox.
	shutdownGrace = 30 * time.Second
)

// Consumer polls wager-transactions.fifo and treats each message through the
// shared wagering.Service. The queue message is deleted only after the
// treatment commits; redeliveries replay through the inbox without moving
// money twice.
type Consumer struct {
	Client   *sqs.Client
	QueueURL string
	DLQURL   string
	Service  *wagering.Service
	Log      *slog.Logger
}

// Run polls until ctx is done (SIGTERM path), then waits for in-flight
// handlers up to shutdownGrace. Messages still held keep their visibility
// timeout and redeliver; the inbox makes redelivery a replay.
func (c *Consumer) Run(ctx context.Context) error {
	sem := make(chan struct{}, maxHandlers)
	var wg sync.WaitGroup
poll:
	for {
		select {
		case <-ctx.Done():
			break poll
		default:
		}
		out, err := c.Client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            &c.QueueURL,
			MaxNumberOfMessages: batchSize,
			WaitTimeSeconds:     10,
			VisibilityTimeout:   visibilityTimeout,
			MessageSystemAttributeNames: []types.MessageSystemAttributeName{
				types.MessageSystemAttributeNameApproximateReceiveCount,
			},
		})
		if err != nil {
			if ctx.Err() != nil {
				break poll
			}
			c.log().Error("sqs receive failed", "err", err)
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				break poll
			case <-timer.C:
			}
			continue
		}
		for _, m := range out.Messages {
			select {
			case <-ctx.Done():
				break poll
			default:
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(msg types.Message) {
				defer wg.Done()
				defer func() { <-sem }()
				// Handlers run detached: Submit owns no ctx, and a
				// cancelled poll must not abort a committing treatment.
				_ = c.Handle(context.Background(), msg)
			}(m)
		}
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(shutdownGrace):
		c.log().Warn("sqs shutdown timed out with handlers in flight")
	}
	return nil
}

// Handle treats one delivery. It is exported so tests can drive the exact
// commit/delete/redelivery interleavings without timing the poll loop.
func (c *Consumer) Handle(ctx context.Context, msg types.Message) error {
	id := aws.ToString(msg.MessageId)
	receipt := aws.ToString(msg.ReceiptHandle)
	body := aws.ToString(msg.Body)

	env, err := ParseEnvelope([]byte(body))
	if err != nil {
		c.toDLQ(ctx, msg, body, "invalid", "malformed", err)
		c.delete(ctx, id, receipt)
		consumeResults.WithLabelValues("invalid").Inc()
		return nil
	}
	corr := env.CorrelationID
	if corr == "" {
		corr = id
	}
	// One span per delivery sharing the message correlationId (D9): the
	// redelivery of a killed commit replays under the same correlation.
	ctx, span := obs.Start(ctx, "sqs handle", corr, attribute.String("messageId", id))
	defer span.End()
	d := env.Data
	res, err := c.Service.Submit(wagering.SubmitInput{
		ProviderID: d.ProviderID, ExternalID: d.ExternalTransactionID,
		IdempotencyKey: d.IdempotencyKey, PlayerID: d.PlayerID, WalletID: d.WalletID,
		RoundID: d.RoundID, GameID: d.GameID,
		Kind:              domain.TransactionKind(d.Kind),
		AmountText:        d.Amount,
		Currency:          d.Currency,
		ReferenceExternal: d.ReferenceExternalID,
		ConsumerName:      ConsumerName, MessageID: id, CorrelationID: corr,
	})
	switch {
	case err == nil:
		span.SetAttributes(attribute.String("outcome", submitOutcome(res)))
		c.delete(ctx, id, receipt)
		consumeResults.WithLabelValues(submitOutcome(res)).Inc()
		return nil
	case isPoison(err):
		span.SetAttributes(attribute.String("outcome", "poison"))
		c.toDLQ(ctx, msg, body, d.WalletID, "poison", err)
		c.delete(ctx, id, receipt)
		consumeResults.WithLabelValues("poison").Inc()
		return nil
	default:
		// Correctable or infrastructure failure: defer with
		// ChangeMessageVisibility in exponential backoff (base 1s, cap
		// 30s). The queue redrive (maxReceiveCount 5) moves exhausted
		// messages to the DLQ; nothing is deleted before its commit.
		c.deferVisibility(ctx, id, receipt, msg, err)
		consumeResults.WithLabelValues("retry").Inc()
		return err
	}
}

func submitOutcome(res *wagering.SubmitResult) string {
	if res == nil {
		return "error"
	}
	switch res.Outcome {
	case wagering.OutcomeProcessed:
		return "processed"
	case wagering.OutcomeRejected:
		return "rejected"
	case wagering.OutcomePending:
		return "pending"
	default:
		return "replay"
	}
}

func isPoison(err error) bool {
	var poison *wagering.PoisonError
	return errors.As(err, &poison)
}

// deferVisibility extends the message timeout by backoff over its receive
// count, with full jitter.
func (c *Consumer) deferVisibility(ctx context.Context, id, receipt string, msg types.Message, cause error) {
	n := 1
	if raw, ok := msg.Attributes["ApproximateReceiveCount"]; ok {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			n = v
		}
	}
	delay := int32(1 << min(n-1, 5)) // base 1s exponential, cap 32 -> 30
	if delay > 30 {
		delay = 30
	}
	delay += int32(rand.Intn(1000)) / 1000 // up to +1s jitter
	if delay > 30 {
		delay = 30
	}
	if _, err := c.Client.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
		QueueUrl: &c.QueueURL, ReceiptHandle: &receipt, VisibilityTimeout: delay,
	}); err != nil {
		c.log().Error("sqs change visibility failed", "messageId", id, "err", err)
		return
	}
	visibilityRetries.Inc()
	c.log().Info("sqs treatment deferred", "messageId", id,
		"receiveCount", n, "visibilityTimeout", delay, "err", cause.Error())
}

// toDLQ routes a never-processable message to the documented DLQ with the
// original body, wallet group and error metadata, preserving identity for
// inspection. The deduplication id is stable per message so a failed DLQ
// send retried on redelivery does not multiply DLQ entries in the window.
func (c *Consumer) toDLQ(ctx context.Context, msg types.Message, body, group, reason string, cause error) {
	id := aws.ToString(msg.MessageId)
	if group == "" {
		group = "invalid"
	}
	if _, err := c.Client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:               &c.DLQURL,
		MessageBody:            &body,
		MessageGroupId:         &group,
		MessageDeduplicationId: aws.String(id + "-dlq"),
		MessageAttributes: map[string]types.MessageAttributeValue{
			"ErrorReason": {DataType: aws.String("String"), StringValue: &reason},
			"ErrorDetail": {DataType: aws.String("String"), StringValue: aws.String(truncate(cause.Error(), 500))},
		},
	}); err != nil {
		c.log().Error("sqs DLQ send failed", "messageId", id, "err", err)
		return
	}
	dlqMoves.WithLabelValues(reason).Inc()
	c.log().Warn("sqs message routed to DLQ", "messageId", id, "reason", reason)
}

// delete removes the message only after its durable outcome committed.
// Delete failures are logged, never fatal: redelivery replays idempotently.
func (c *Consumer) delete(ctx context.Context, id, receipt string) {
	if _, err := c.Client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl: &c.QueueURL, ReceiptHandle: &receipt,
	}); err != nil {
		c.log().Error("sqs delete failed, redelivery will replay", "messageId", id, "err", err)
	}
}

func (c *Consumer) log() *slog.Logger {
	if c.Log != nil {
		return c.Log
	}
	return slog.Default()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
