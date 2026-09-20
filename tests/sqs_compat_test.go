//go:build integration

// Package tests holds integration tests that require real infrastructure.
// Run with: go test -tags integration ./tests/ -run TestMiniStackSQSCompat -v
// against MiniStack at $SQS_ENDPOINT (default http://localhost:4566).
package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

func sqsClient(t *testing.T, ctx context.Context, key, secret string) *sqs.Client {
	t.Helper()
	endpoint := envOr("SQS_ENDPOINT", "http://localhost:4566")
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(key, secret, "")),
		config.WithBaseEndpoint(endpoint),
	)
	if err != nil {
		t.Fatalf("load aws config: %v", err)
	}
	return sqs.NewFromConfig(cfg)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func createFIFOQueue(t *testing.T, ctx context.Context, c *sqs.Client, name string, extra map[string]string) string {
	t.Helper()
	attrs := map[string]string{"FifoQueue": "true", "ContentBasedDedup": "false"}
	for k, v := range extra {
		attrs[k] = v
	}
	out, err := c.CreateQueue(ctx, &sqs.CreateQueueInput{QueueName: aws.String(name), Attributes: attrs})
	if err != nil {
		t.Fatalf("create queue %s: %v", name, err)
	}
	return aws.ToString(out.QueueUrl)
}

func queueARN(t *testing.T, ctx context.Context, c *sqs.Client, url string) string {
	t.Helper()
	out, err := c.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(url),
		AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameQueueArn},
	})
	if err != nil {
		t.Fatalf("get queue arn: %v", err)
	}
	return out.Attributes[string(types.QueueAttributeNameQueueArn)]
}

func receiveAll(t *testing.T, ctx context.Context, c *sqs.Client, url string) []types.Message {
	t.Helper()
	var all []types.Message
	for {
		out, err := c.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(url),
			MaxNumberOfMessages: 10,
			WaitTimeSeconds:     1,
			AttributeNames:      []types.QueueAttributeName{types.QueueAttributeNameAll},
		})
		if err != nil {
			t.Fatalf("receive: %v", err)
		}
		if len(out.Messages) == 0 {
			return all
		}
		all = append(all, out.Messages...)
		if len(all) > 100 {
			t.Fatalf("runaway receive loop")
		}
	}
}

func deleteAll(t *testing.T, ctx context.Context, c *sqs.Client, url string, msgs []types.Message) {
	t.Helper()
	for _, m := range msgs {
		_, err := c.DeleteMessage(ctx, &sqs.DeleteMessageInput{
			QueueUrl:      aws.String(url),
			ReceiptHandle: m.ReceiptHandle,
		})
		if err != nil {
			t.Fatalf("delete: %v", err)
		}
	}
}

// TestMiniStackSQSCompat validates the emulator behaviors the financial
// correctness depends on NOT relying on: FIFO per group, dedup window,
// ChangeMessageVisibility, batches, long polling and redrive to DLQ.
// Findings feed ARCHITECTURE.md; the app must stay correct even where the
// broker diverges (README §5.3).
func TestMiniStackSQSCompat(t *testing.T) {
	ctx := context.Background()
	c := sqsClient(t, ctx, "test", "test")
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	t.Run("FIFOOrderingPerGroup", func(t *testing.T) {
		q := createFIFOQueue(t, ctx, c, "compat-order-"+suffix+".fifo", nil)
		t.Cleanup(func() { _, _ = c.DeleteQueue(ctx, &sqs.DeleteQueueInput{QueueUrl: aws.String(q)}) })
		for i := 0; i < 5; i++ {
			_, err := c.SendMessage(ctx, &sqs.SendMessageInput{
				QueueUrl:               aws.String(q),
				MessageBody:            aws.String(fmt.Sprintf("seq-%d", i)),
				MessageGroupId:         aws.String("wallet-1"),
				MessageDeduplicationId: aws.String(fmt.Sprintf("order-%s-%d", suffix, i)),
			})
			if err != nil {
				t.Fatalf("send: %v", err)
			}
		}
		msgs := receiveAll(t, ctx, c, q)
		if len(msgs) != 5 {
			t.Fatalf("expected 5 messages, got %d", len(msgs))
		}
		for i, m := range msgs {
			if got := aws.ToString(m.Body); got != fmt.Sprintf("seq-%d", i) {
				t.Fatalf("out of order at %d: got %q", i, got)
			}
		}
		t.Logf("SUPPORT fifo-order-per-group=true")
		deleteAll(t, ctx, c, q, msgs)
	})

	t.Run("DeduplicationWindow", func(t *testing.T) {
		q := createFIFOQueue(t, ctx, c, "compat-dedup-"+suffix+".fifo", nil)
		t.Cleanup(func() { _, _ = c.DeleteQueue(ctx, &sqs.DeleteQueueInput{QueueUrl: aws.String(q)}) })
		body := "dup-body-" + suffix
		for i := 0; i < 2; i++ {
			_, err := c.SendMessage(ctx, &sqs.SendMessageInput{
				QueueUrl:               aws.String(q),
				MessageBody:            aws.String(body),
				MessageGroupId:         aws.String("wallet-1"),
				MessageDeduplicationId: aws.String("same-dedup-" + suffix),
			})
			if err != nil {
				t.Fatalf("send: %v", err)
			}
		}
		if _, err := c.SendMessage(ctx, &sqs.SendMessageInput{
			QueueUrl:               aws.String(q),
			MessageBody:            aws.String(body),
			MessageGroupId:         aws.String("wallet-1"),
			MessageDeduplicationId: aws.String("other-dedup-" + suffix),
		}); err != nil {
			t.Fatalf("send: %v", err)
		}
		msgs := receiveAll(t, ctx, c, q)
		if len(msgs) != 2 {
			t.Fatalf("expected 2 stored messages (same dedup id collapsed), got %d", len(msgs))
		}
		t.Logf("SUPPORT dedup-window-5min=true")
		deleteAll(t, ctx, c, q, msgs)
	})

	t.Run("BatchAndVisibility", func(t *testing.T) {
		q := createFIFOQueue(t, ctx, c, "compat-batch-"+suffix+".fifo", nil)
		t.Cleanup(func() { _, _ = c.DeleteQueue(ctx, &sqs.DeleteQueueInput{QueueUrl: aws.String(q)}) })
		var entries []types.SendMessageBatchRequestEntry
		for i := 0; i < 10; i++ {
			entries = append(entries, types.SendMessageBatchRequestEntry{
				Id:                     aws.String(fmt.Sprintf("e%d", i)),
				MessageBody:            aws.String(fmt.Sprintf("batch-%d", i)),
				MessageGroupId:         aws.String("wallet-9"),
				MessageDeduplicationId: aws.String(fmt.Sprintf("batch-%s-%d", suffix, i)),
			})
		}
		out, err := c.SendMessageBatch(ctx, &sqs.SendMessageBatchInput{QueueUrl: aws.String(q), Entries: entries})
		if err != nil || len(out.Failed) > 0 {
			t.Fatalf("batch send failed: %v %+v", err, out.Failed)
		}
		msgs := receiveAll(t, ctx, c, q)
		if len(msgs) != 10 {
			t.Fatalf("expected 10 batched messages, got %d", len(msgs))
		}
		t.Logf("SUPPORT batch-10=true")
		deleteAll(t, ctx, c, q, msgs)
	})

	t.Run("ChangeVisibility", func(t *testing.T) {
		q := createFIFOQueue(t, ctx, c, "compat-vis-"+suffix+".fifo", nil)
		t.Cleanup(func() { _, _ = c.DeleteQueue(ctx, &sqs.DeleteQueueInput{QueueUrl: aws.String(q)}) })
		if _, err := c.SendMessage(ctx, &sqs.SendMessageInput{
			QueueUrl:               aws.String(q),
			MessageBody:            aws.String("vis"),
			MessageGroupId:         aws.String("wallet-1"),
			MessageDeduplicationId: aws.String("vis-" + suffix),
		}); err != nil {
			t.Fatalf("send: %v", err)
		}
		// Visibility uses the freshest receipt handle: MiniStack silently
		// ignores release-to-0 on a handle superseded by a later receive
		// on the same queue (divergence from AWS, documented for the
		// consumer shutdown path which always uses current handles).
		r, err := c.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl: aws.String(q), MaxNumberOfMessages: 1,
		})
		if err != nil || len(r.Messages) != 1 {
			t.Fatalf("receive: %v", err)
		}
		rh := r.Messages[0].ReceiptHandle
		if _, err := c.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
			QueueUrl:          aws.String(q),
			ReceiptHandle:     rh,
			VisibilityTimeout: 30,
		}); err != nil {
			t.Fatalf("extend visibility: %v", err)
		}
		if _, err := c.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
			QueueUrl:          aws.String(q),
			ReceiptHandle:     rh,
			VisibilityTimeout: 0,
		}); err != nil {
			t.Fatalf("release visibility: %v", err)
		}
		again, err := c.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl: aws.String(q), MaxNumberOfMessages: 1, WaitTimeSeconds: 2,
		})
		if err != nil {
			t.Fatalf("receive after release: %v", err)
		}
		if len(again.Messages) != 1 {
			t.Fatalf("message not visible after visibility release")
		}
		t.Logf("SUPPORT change-visibility=true (fresh handle; stale handles ignored by emulator)")
		deleteAll(t, ctx, c, q, again.Messages)
	})

	t.Run("LongPolling", func(t *testing.T) {
		q := createFIFOQueue(t, ctx, c, "compat-poll-"+suffix+".fifo", nil)
		t.Cleanup(func() { _, _ = c.DeleteQueue(ctx, &sqs.DeleteQueueInput{QueueUrl: aws.String(q)}) })
		start := time.Now()
		out, err := c.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl: aws.String(q), MaxNumberOfMessages: 1, WaitTimeSeconds: 5,
		})
		if err != nil {
			t.Fatalf("long poll: %v", err)
		}
		elapsed := time.Since(start)
		t.Logf("SUPPORT long-poll elapsed=%s empty=%v (no error = supported call shape)", elapsed, len(out.Messages) == 0)
	})

	t.Run("RedriveToDLQ", func(t *testing.T) {
		dlq := createFIFOQueue(t, ctx, c, "compat-dlq-"+suffix+".fifo", nil)
		t.Cleanup(func() { _, _ = c.DeleteQueue(ctx, &sqs.DeleteQueueInput{QueueUrl: aws.String(dlq)}) })
		arn := queueARN(t, ctx, c, dlq)
		policy, _ := json.Marshal(map[string]string{"deadLetterTargetArn": arn, "maxReceiveCount": "2"})
		q := createFIFOQueue(t, ctx, c, "compat-redrive-"+suffix+".fifo",
			map[string]string{"VisibilityTimeout": "1", "RedrivePolicy": string(policy)})
		t.Cleanup(func() { _, _ = c.DeleteQueue(ctx, &sqs.DeleteQueueInput{QueueUrl: aws.String(q)}) })
		if _, err := c.SendMessage(ctx, &sqs.SendMessageInput{
			QueueUrl:               aws.String(q),
			MessageBody:            aws.String("poison"),
			MessageGroupId:         aws.String("wallet-1"),
			MessageDeduplicationId: aws.String("redrive-" + suffix),
		}); err != nil {
			t.Fatalf("send: %v", err)
		}
		// Exhaust maxReceiveCount=2 without deleting; the message must
		// surface on the DLQ afterwards.
		moved := false
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) && !moved {
			r, err := c.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
				QueueUrl: aws.String(q), MaxNumberOfMessages: 1, WaitTimeSeconds: 1,
				AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameAll},
			})
			if err != nil {
				t.Fatalf("receive: %v", err)
			}
			if len(r.Messages) == 0 {
				d, err := c.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
					QueueUrl: aws.String(dlq), MaxNumberOfMessages: 1, WaitTimeSeconds: 1,
				})
				if err != nil {
					t.Fatalf("dlq receive: %v", err)
				}
				if len(d.Messages) == 1 && aws.ToString(d.Messages[0].Body) == "poison" {
					moved = true
					deleteAll(t, ctx, c, dlq, d.Messages)
				}
				continue
			}
			time.Sleep(1200 * time.Millisecond) // let visibility expire
		}
		if !moved {
			t.Fatalf("message never reached DLQ after maxReceiveCount")
		}
		t.Logf("SUPPORT redrive-maxReceiveCount=true")
	})

	t.Run("QueuePolicyStorage", func(t *testing.T) {
		q := createFIFOQueue(t, ctx, c, "compat-policy-"+suffix+".fifo", nil)
		t.Cleanup(func() { _, _ = c.DeleteQueue(ctx, &sqs.DeleteQueueInput{QueueUrl: aws.String(q)}) })
		arn := queueARN(t, ctx, c, q)
		policy := fmt.Sprintf(`{"Version":"2012-10-17","Id":"compat-%s","Statement":[{"Sid":"AllowAll","Effect":"Allow","Principal":"*","Action":"sqs:*","Resource":%q}]}`,
			suffix, arn)
		if _, err := c.SetQueueAttributes(ctx, &sqs.SetQueueAttributesInput{
			QueueUrl:   aws.String(q),
			Attributes: map[string]string{"Policy": policy},
		}); err != nil {
			t.Fatalf("set policy: %v", err)
		}
		got, err := c.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
			QueueUrl:       aws.String(q),
			AttributeNames: []types.QueueAttributeName{types.QueueAttributeNamePolicy},
		})
		if err != nil {
			t.Fatalf("get policy: %v", err)
		}
		stored := got.Attributes[string(types.QueueAttributeNamePolicy)]
		if !strings.Contains(stored, "AllowAll") {
			t.Fatalf("policy not stored, got: %s", stored)
		}
		// Enforcement probe with distinct non-numeric credentials (same
		// MiniStack account by design). Result is informational: domain
		// validation in the consumer is mandatory regardless.
		intruder := sqsClient(t, ctx, "intruderuser", "intrudersecret")
		_, err = intruder.SendMessage(ctx, &sqs.SendMessageInput{
			QueueUrl:               aws.String(q),
			MessageBody:            aws.String("probe"),
			MessageGroupId:         aws.String("wallet-1"),
			MessageDeduplicationId: aws.String("probe-" + suffix),
		})
		t.Logf("SUPPORT policy-storage=true enforcement-probe err=%v (nil means no enforcement; domain checks still required)", err)
	})
}
