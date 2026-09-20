//go:build integration

package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	adaptersqs "github.com/jg-backend-challenge/wallet/internal/adapters/sqs"
	"github.com/jg-backend-challenge/wallet/internal/wagering"
)

func sqsEndpoint() string {
	if v := os.Getenv("SQS_ENDPOINT"); v != "" {
		return v
	}
	return "http://localhost:4566"
}

func sqsTestClient(t *testing.T) *sqs.Client {
	t.Helper()
	key := os.Getenv("SQS_CONSUMER_KEY")
	if key == "" {
		key = "consumeruser"
	}
	secret := os.Getenv("SQS_CONSUMER_SECRET")
	if secret == "" {
		secret = "consumersecret"
	}
	client, err := adaptersqs.NewClient(context.Background(), sqsEndpoint(), "us-east-1", key, secret)
	if err != nil {
		t.Fatalf("sqs client: %v", err)
	}
	return client
}

func sqsQueueURLs(t *testing.T, client *sqs.Client) (queue, dlq string) {
	t.Helper()
	ctx := context.Background()
	var err error
	if queue, err = adaptersqs.QueueURL(ctx, client, "wager-transactions.fifo"); err != nil {
		t.Fatalf("tx queue: %v", err)
	}
	if dlq, err = adaptersqs.QueueURL(ctx, client, "wager-transactions-dlq.fifo"); err != nil {
		t.Fatalf("dlq: %v", err)
	}
	return queue, dlq
}

func sqsDepth(t *testing.T, client *sqs.Client, queueURL string) int {
	t.Helper()
	out, err := client.GetQueueAttributes(context.Background(), &sqs.GetQueueAttributesInput{
		QueueUrl:       &queueURL,
		AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameApproximateNumberOfMessages},
	})
	if err != nil {
		t.Fatalf("depth: %v", err)
	}
	n := 0
	for _, v := range out.Attributes {
		var m int
		if _, err := fmt.Sscanf(v, "%d", &m); err == nil {
			n += m
		}
	}
	return n
}

func sendEnvelope(t *testing.T, client *sqs.Client, queueURL, group string, env adaptersqs.Envelope) {
	t.Helper()
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if _, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:               &queueURL,
		MessageBody:            &body,
		MessageGroupId:         &group,
		MessageDeduplicationId: aws.String("dedup-" + uid(t)),
	}); err != nil {
		t.Fatalf("send: %v", err)
	}
}

func testConsumer(t *testing.T, client *sqs.Client, queue, dlq string, svc *wagering.Service) *adaptersqs.Consumer {
	t.Helper()
	return &adaptersqs.Consumer{
		Client: client, QueueURL: queue, DLQURL: dlq,
		Service: svc,
		Log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
	}
}

func TestSQSConsume(t *testing.T) {
	ctx := context.Background()
	svc := openService(t)
	w := openWallet(t, svc, "100.00")
	client := sqsTestClient(t)
	queue, dlq := sqsQueueURLs(t, client)
	consumer := testConsumer(t, client, queue, dlq, svc)

	ext := "bet-" + uid(t)
	env := adaptersqs.Envelope{Data: adaptersqs.OperationData{
		ProviderID: "provider-a", ExternalTransactionID: ext,
		PlayerID: w.PlayerID, WalletID: w.ID,
		RoundID: "round-1", GameID: "game-1",
		Kind: "BET", Amount: "25.00", Currency: "BRL",
		IdempotencyKey: "key-" + uid(t),
	}}
	sendEnvelope(t, client, queue, w.ID, env)

	runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := consumer.Run(runCtx); err != nil {
		t.Fatalf("run: %v", err)
	}

	rec, err := svc.DB.Tx().FindByProviderExternal("provider-a", ext)
	if err != nil || rec == nil {
		t.Fatalf("tx not processed: %v", rec)
	}
	if rec.State != "PROCESSED" {
		t.Fatalf("state = %s, want PROCESSED", rec.State)
	}
	if rec.ResultBalanceMinor == nil || *rec.ResultBalanceMinor != 7500 {
		t.Fatalf("balance = %v, want 7500", rec.ResultBalanceMinor)
	}
	// Deleted only after commit: the queue drains.
	deadline := time.Now().Add(10 * time.Second)
	for sqsDepth(t, client, queue) != 0 && time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
	}
	if d := sqsDepth(t, client, queue); d != 0 {
		t.Fatalf("queue depth = %d, want 0", d)
	}
}

func TestSQSCrossChannelReplay(t *testing.T) {
	svc := openService(t)
	w := openWallet(t, svc, "100.00")
	client := sqsTestClient(t)
	queue, dlq := sqsQueueURLs(t, client)
	consumer := testConsumer(t, client, queue, dlq, svc)

	// Same business content arrives via HTTP first, then SQS with a
	// different key: the second is a replay, never a second debit.
	ext := "bet-" + uid(t)
	keyHTTP := "key-http-" + uid(t)
	submitBET(t, svc, w, ext, keyHTTP, "30.00")

	env := adaptersqs.Envelope{Data: adaptersqs.OperationData{
		ProviderID: "provider-a", ExternalTransactionID: ext,
		PlayerID: w.PlayerID, WalletID: w.ID,
		RoundID: "round-1", GameID: "game-1",
		Kind: "BET", Amount: "30.00", Currency: "BRL",
		IdempotencyKey: "key-sqs-" + uid(t),
	}}
	raw, _ := json.Marshal(env)
	msg := types.Message{MessageId: aws.String("msg-" + uid(t)), Body: aws.String(string(raw))}
	if err := consumer.Handle(context.Background(), msg); err != nil {
		t.Fatalf("handle: %v", err)
	}
	got, err := svc.DB.Wallet().Get(w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.BalanceMinor != 7000 {
		t.Fatalf("balance = %d, want single debit to 7000", got.BalanceMinor)
	}
}

func TestSQSRedeliveryAfterCommit(t *testing.T) {
	svc := openService(t)
	w := openWallet(t, svc, "100.00")
	client := sqsTestClient(t)
	queue, dlq := sqsQueueURLs(t, client)
	consumer := testConsumer(t, client, queue, dlq, svc)

	// Crash between commit and delete: the same messageId redelivers with
	// identical content. The inbox recognizes the duplicate; money moves once.
	ext := "bet-" + uid(t)
	env := adaptersqs.Envelope{Data: adaptersqs.OperationData{
		ProviderID: "provider-a", ExternalTransactionID: ext,
		PlayerID: w.PlayerID, WalletID: w.ID,
		RoundID: "round-1", GameID: "game-1",
		Kind: "BET", Amount: "10.00", Currency: "BRL",
		IdempotencyKey: "key-" + uid(t),
	}}
	raw, _ := json.Marshal(env)
	msgID := "msg-" + uid(t)
	first := types.Message{MessageId: &msgID, ReceiptHandle: aws.String("receipt-1"), Body: aws.String(string(raw))}
	if err := consumer.Handle(context.Background(), first); err != nil {
		t.Fatalf("first handle: %v", err)
	}
	redelivery := types.Message{MessageId: &msgID, ReceiptHandle: aws.String("receipt-2"), Body: aws.String(string(raw))}
	if err := consumer.Handle(context.Background(), redelivery); err != nil {
		t.Fatalf("redelivery handle: %v", err)
	}
	got, err := svc.DB.Wallet().Get(w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.BalanceMinor != 9000 {
		t.Fatalf("balance = %d, want single debit to 9000", got.BalanceMinor)
	}

	// Same messageId with divergent content is poisoning: DLQ, no finance.
	dlqBefore := sqsDepth(t, client, dlq)
	env.Data.Amount = "99.00"
	raw2, _ := json.Marshal(env)
	poison := types.Message{MessageId: &msgID, ReceiptHandle: aws.String("receipt-3"), Body: aws.String(string(raw2))}
	if err := consumer.Handle(context.Background(), poison); err != nil {
		t.Fatalf("poison handle: %v", err)
	}
	if got, _ := svc.DB.Wallet().Get(w.ID); got.BalanceMinor != 9000 {
		t.Fatalf("poison moved money: %d", got.BalanceMinor)
	}
	if d := sqsDepth(t, client, dlq); d != dlqBefore+1 {
		t.Fatalf("dlq depth = %d, want %d", d, dlqBefore+1)
	}
}

func TestSQSMalformedToDLQ(t *testing.T) {
	svc := openService(t)
	client := sqsTestClient(t)
	queue, dlq := sqsQueueURLs(t, client)
	consumer := testConsumer(t, client, queue, dlq, svc)

	dlqBefore := sqsDepth(t, client, dlq)
	body := "{not json"
	msg := types.Message{MessageId: aws.String("msg-" + uid(t)), ReceiptHandle: aws.String("r"), Body: &body}
	if err := consumer.Handle(context.Background(), msg); err != nil {
		t.Fatalf("malformed handle: %v", err)
	}
	if d := sqsDepth(t, client, dlq); d != dlqBefore+1 {
		t.Fatalf("dlq depth = %d, want %d", d, dlqBefore+1)
	}
}
