//go:build integration

package tests

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	adaptersqs "github.com/jg-backend-challenge/wallet/internal/adapters/sqs"
)

// sqsPublisherClient builds the broker client with the publisher role's
// keypair, proving per-role credentials work end-to-end (5.3).
func sqsPublisherClient(t *testing.T) *sqs.Client {
	t.Helper()
	key := os.Getenv("SQS_PUBLISHER_KEY")
	if key == "" {
		key = "publisheruser"
	}
	secret := os.Getenv("SQS_PUBLISHER_SECRET")
	if secret == "" {
		secret = "publishersecret"
	}
	client, err := adaptersqs.NewClient(context.Background(), sqsEndpoint(), "us-east-1", key, secret)
	if err != nil {
		t.Fatalf("publisher sqs client: %v", err)
	}
	return client
}

// TestBrokerRolesEndToEnd publishes with the publisher keypair and consumes
// with the consumer keypair: the role topology works against the emulator
// even though it enforces no separation (see package docs).
func TestBrokerRolesEndToEnd(t *testing.T) {
	ctx := context.Background()
	svc := openService(t)
	w := openWallet(t, svc, "100.00")

	pubClient := sqsPublisherClient(t)
	conClient := sqsTestClient(t)
	queue, _ := sqsQueueURLs(t, pubClient)
	dlq, _ := sqsQueueURLs(t, conClient)

	ext := "bet-" + uid(t)
	env := adaptersqs.Envelope{Data: adaptersqs.OperationData{
		ProviderID: "provider-a", ExternalTransactionID: ext,
		PlayerID: w.PlayerID, WalletID: w.ID,
		RoundID: "round-1", GameID: "game-1",
		Kind: "BET", Amount: "15.00", Currency: "BRL",
		IdempotencyKey: "key-" + uid(t),
	}}
	sendEnvelope(t, pubClient, queue, w.ID, env)

	consumer := testConsumer(t, conClient, queue, dlq, svc)
	runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := consumer.Run(runCtx); err != nil {
		t.Fatalf("run: %v", err)
	}
	rec, err := svc.DB.Tx().FindByProviderExternal("provider-a", ext)
	if err != nil || rec == nil || rec.State != "PROCESSED" {
		t.Fatalf("tx = %v, %v; want PROCESSED", rec, err)
	}
}

// TestConsumerRevalidatesDomain sends a domain-invalid operation through the
// broker under the permissive emulator policy: the consumer must still
// reject it definitively (persisted REJECTED, no money moved) and delete it
// after commit instead of trusting the transport.
func TestConsumerRevalidatesDomain(t *testing.T) {
	ctx := context.Background()
	svc := openService(t)
	w := openWallet(t, svc, "100.00")
	client := sqsTestClient(t)
	queue, dlq := sqsQueueURLs(t, client)
	consumer := testConsumer(t, client, queue, dlq, svc)

	// Insufficient balance is definitive over SQS too.
	ext := "bet-" + uid(t)
	env := adaptersqs.Envelope{Data: adaptersqs.OperationData{
		ProviderID: "provider-a", ExternalTransactionID: ext,
		PlayerID: w.PlayerID, WalletID: w.ID,
		RoundID: "round-1", GameID: "game-1",
		Kind: "BET", Amount: "9999.00", Currency: "BRL",
		IdempotencyKey: "key-" + uid(t),
	}}
	raw, _ := json.Marshal(env)
	msgID := "msg-" + uid(t)
	msg := types.Message{MessageId: &msgID, ReceiptHandle: aws.String("receipt-1"), Body: aws.String(string(raw))}
	if err := consumer.Handle(ctx, msg); err != nil {
		t.Fatalf("handle: %v", err)
	}
	rec, err := svc.DB.Tx().FindByProviderExternal("provider-a", ext)
	if err != nil || rec == nil {
		t.Fatalf("rejection not persisted: %v", rec)
	}
	if rec.State != "REJECTED" || rec.FailureCode == nil || *rec.FailureCode != "INSUFFICIENT_BALANCE" {
		t.Fatalf("tx = %s %v, want REJECTED INSUFFICIENT_BALANCE", rec.State, rec.FailureCode)
	}
	got, err := svc.DB.Wallet().Get(w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.BalanceMinor != 10000 {
		t.Fatalf("balance moved on rejection: %d", got.BalanceMinor)
	}

	// A forged provider scope in the envelope is still just data: it lands
	// under its own (providerId, key) identity and can never touch another
	// provider's transactions.
	ext2 := "bet-" + uid(t)
	env2 := adaptersqs.Envelope{Data: adaptersqs.OperationData{
		ProviderID: "provider-evil", ExternalTransactionID: ext2,
		PlayerID: w.PlayerID, WalletID: w.ID,
		RoundID: "round-1", GameID: "game-1",
		Kind: "BET", Amount: "5.00", Currency: "BRL",
		IdempotencyKey: "key-" + uid(t),
	}}
	raw2, _ := json.Marshal(env2)
	msgID2 := "msg-" + uid(t)
	msg2 := types.Message{MessageId: &msgID2, ReceiptHandle: aws.String("receipt-2"), Body: aws.String(string(raw2))}
	if err := consumer.Handle(ctx, msg2); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if _, err := svc.DB.Tx().FindByProviderExternal("provider-a", ext2); err != nil {
		t.Fatal(err)
	}
	evil, err := svc.DB.Tx().FindByProviderExternal("provider-evil", ext2)
	if err != nil || evil == nil || evil.State != "PROCESSED" {
		t.Fatalf("scoped tx = %v, %v", evil, err)
	}
}
