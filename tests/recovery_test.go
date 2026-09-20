//go:build integration

package tests

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	adaptersqs "github.com/jg-backend-challenge/wallet/internal/adapters/sqs"
	"github.com/jg-backend-challenge/wallet/internal/domain"
	"github.com/jg-backend-challenge/wallet/internal/ports"
	"github.com/jg-backend-challenge/wallet/internal/wagering"
)

// Recovery battery (6.2, §13): kills between durable steps must converge on
// restart without duplicating money. Assertions scope to each test's wallet;
// the outbox table is shared across tests.

// TestRecoveryKillBetweenCommitAndDelete drives a real broker message through
// the commit-then-delete path with the delete failing (the kill), then
// redelivers the same messageId: the inbox replays, money moves once, and the
// ledger reconciles. TestSQSRedeliveryAfterCommit covers the Handle-level
// interleaving with forged ids; this one round-trips MiniStack receipts.
func TestRecoveryKillBetweenCommitAndDelete(t *testing.T) {
	ctx := context.Background()
	svc := openService(t)
	w := openWallet(t, svc, "100.00")
	client := sqsTestClient(t)
	queue, _ := sqsQueueURLs(t, client)
	consumer := testConsumer(t, client, queue, "", svc)

	ext := "bet-" + uid(t)
	env := adaptersqs.Envelope{Data: adaptersqs.OperationData{
		ProviderID: "provider-a", ExternalTransactionID: ext,
		PlayerID: w.PlayerID, WalletID: w.ID,
		RoundID: "round-1", GameID: "game-1",
		Kind: "BET", Amount: "10.00", Currency: "BRL",
		IdempotencyKey: "key-" + uid(t),
	}}
	sendEnvelope(t, client, queue, w.ID, env)

	recv, err := client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl: &queue, MaxNumberOfMessages: 10, WaitTimeSeconds: 5,
		VisibilityTimeout: 2, // short flight: cleanup can reclaim a leftover
	})
	if err != nil || len(recv.Messages) == 0 {
		t.Fatalf("receive sent message: %v %d", err, len(recv.Messages))
	}
	var target types.Message
	for _, m := range recv.Messages {
		var parsed adaptersqs.Envelope
		if err := json.Unmarshal([]byte(aws.ToString(m.Body)), &parsed); err != nil {
			continue
		}
		if parsed.Data.ExternalTransactionID == ext {
			target = m
			break
		}
	}
	if target.MessageId == nil {
		t.Fatalf("sent message not received (got %d others)", len(recv.Messages))
	}

	// The kill: commit with an unusable receipt, so the broker delete fails
	// (logged, never fatal) and the message survives for redelivery.
	killed := types.Message{
		MessageId: target.MessageId, ReceiptHandle: aws.String("receipt-lost-in-crash"),
		Body: target.Body,
	}
	if err := consumer.Handle(ctx, killed); err != nil {
		t.Fatalf("killed handle: %v", err)
	}

	// Redelivery after the crash: same messageId, identical content.
	redelivery := types.Message{
		MessageId: target.MessageId, ReceiptHandle: target.ReceiptHandle,
		Body: target.Body,
	}
	if err := consumer.Handle(ctx, redelivery); err != nil {
		t.Fatalf("redelivery handle: %v", err)
	}
	got, err := svc.DB.Wallet().Get(w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.BalanceMinor != 9000 {
		t.Fatalf("balance = %d, want single debit to 9000", got.BalanceMinor)
	}
	assertLedgerAndBalance(t, svc, w.ID, "90.00", 2) // OPENING + one debit

	// Cleanup: the redelivery's Handle already deleted with the live receipt.
	deadline := time.Now().Add(10 * time.Second)
	for {
		rest, err := client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl: &queue, MaxNumberOfMessages: 10, WaitTimeSeconds: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		pending := false
		for _, m := range rest.Messages {
			var parsed adaptersqs.Envelope
			if err := json.Unmarshal([]byte(aws.ToString(m.Body)), &parsed); err != nil {
				continue
			}
			if parsed.Data.ExternalTransactionID == ext {
				pending = true
				if _, err := client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
					QueueUrl: &queue, ReceiptHandle: m.ReceiptHandle,
				}); err != nil {
					t.Fatal(err)
				}
			}
		}
		if !pending || time.Now().After(deadline) {
			break
		}
	}
}

// TestRecoveryOutboxDisputeAfterLeaseExpiry kills a publisher between commit
// and publish (dead-owner claim), lets the lease expire, then races two live
// publishers: each of this wallet's events publishes exactly once with stable
// eventIds.
func TestRecoveryOutboxDisputeAfterLeaseExpiry(t *testing.T) {
	ctx := context.Background()
	svc := openService(t)
	w := openWallet(t, svc, "100.00")
	submitBET(t, svc, w, "bet-"+uid(t), "key-"+uid(t), "20.00")
	// Fresh wallet: 2 opening events + 2 bet events.
	want := unpublishedIDs(t, svc, w.ID)
	if len(want) != 4 {
		t.Fatalf("pending for wallet = %d, want 4 (2 opening + 2 bet)", len(want))
	}

	// The crash: a publisher claims the rows and dies before publishing.
	if _, err := svc.DB.Aux().ClaimOutbox("dead-owner", time.Second, 100); err != nil {
		t.Fatalf("crash claim: %v", err)
	}
	time.Sleep(1500 * time.Millisecond) // lease expires, rows become due again

	stubA, stubB := &stubPublisher{}, &stubPublisher{}
	deadline := time.Now().Add(30 * time.Second)
	for len(unpublishedIDs(t, svc, w.ID)) != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("wallet %s outbox never drained after lease expiry", w.ID)
		}
		errs := make(chan error, 2)
		go func() { errs <- publishUntilDrained(ctx, svc, stubA, "owner-a") }()
		go func() { errs <- publishUntilDrained(ctx, svc, stubB, "owner-b") }()
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	got := map[string]int{}
	for _, s := range append(stubA.sent, stubB.sent...) {
		if s.AggregateID != w.ID {
			continue
		}
		got[s.EventID]++
		if !want[s.EventID] {
			t.Fatalf("unexpected event published: %s", s.EventID)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("published for wallet = %d, want %d", len(got), len(want))
	}
	for id, n := range got {
		if n != 1 {
			t.Fatalf("event %s published %d times", id, n)
		}
	}
}

// TestRecoveryRefundBeforeReferenceAndRestart parks a REFUND before its BET,
// "restarts" (a fresh Service over the same database submits the BET and the
// parked refund resolves), replays across the restart with the original
// balance, then exhausts a second REFUND into REFERENCE_NOT_FOUND on a third
// instance. Final proof is stored == credits - debits via Reconcile.
func TestRecoveryRefundBeforeReferenceAndRestart(t *testing.T) {
	svcA := openService(t)
	w := openWallet(t, svcA, "1000.00")

	betExt := "bet-" + uid(t)
	betKey := "key-" + uid(t)
	first, err := svcA.Submit(wagering.SubmitInput{
		ProviderID: "provider-a", ExternalID: betExt, IdempotencyKey: betKey,
		PlayerID: w.PlayerID, WalletID: w.ID, RoundID: "round-1", GameID: "game-1",
		Kind: domain.KindBet, AmountText: "80.00", Currency: "BRL",
	})
	if err != nil {
		t.Fatalf("bet: %v", err)
	}
	if first.Balance.String() != "920.00" {
		t.Fatalf("bet balance = %s, want 920.00", first.Balance)
	}

	refund, err := svcA.Submit(wagering.SubmitInput{
		ProviderID: "provider-a", ExternalID: "refund-" + uid(t), IdempotencyKey: "key-" + uid(t),
		PlayerID: w.PlayerID, WalletID: w.ID, RoundID: "round-1", GameID: "game-1",
		Kind: domain.KindRefund, AmountText: "80.00", Currency: "BRL",
		ReferenceExternal: betExt + "-late",
	})
	if err != nil {
		t.Fatal(err)
	}
	if refund.Outcome != wagering.OutcomePending {
		t.Fatalf("refund outcome = %s, want pending", refund.Outcome)
	}

	// Restart: svcA is dropped. The new instance submits the late BET, and
	// the commit's immediate re-evaluation resolves the parked refund.
	svcB := openService(t)
	lateExt := betExt + "-late"
	if _, err := svcB.Submit(wagering.SubmitInput{
		ProviderID: "provider-a", ExternalID: lateExt, IdempotencyKey: "key-" + uid(t),
		PlayerID: w.PlayerID, WalletID: w.ID, RoundID: "round-1", GameID: "game-1",
		Kind: domain.KindBet, AmountText: "80.00", Currency: "BRL",
	}); err != nil {
		t.Fatalf("late bet on restarted instance: %v", err)
	}
	var refundState string
	if err := svcB.DB.Transact(func(db ports.DB) error {
		rec, err := db.Tx().Get(refund.TransactionID)
		if err != nil || rec == nil {
			return err
		}
		refundState = rec.State
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if refundState != "PROCESSED" {
		t.Fatalf("refund state after restart = %s, want PROCESSED", refundState)
	}

	// Idempotency survives the restart: replay returns the original balance.
	again, err := svcB.Submit(wagering.SubmitInput{
		ProviderID: "provider-a", ExternalID: betExt, IdempotencyKey: betKey,
		PlayerID: w.PlayerID, WalletID: w.ID, RoundID: "round-1", GameID: "game-1",
		Kind: domain.KindBet, AmountText: "80.00", Currency: "BRL",
	})
	if err != nil {
		t.Fatalf("replay after restart: %v", err)
	}
	if !again.IdempotentReplay || again.Outcome != wagering.OutcomeReplay {
		t.Fatalf("replay flags = %+v, want replay", again)
	}
	if again.Balance.String() != "920.00" {
		t.Fatalf("replay balance = %s, want original 920.00", again.Balance)
	}

	// Expiry leg: a REFUND whose reference never arrives exhausts its budget
	// on a third instance and settles REJECTED with REFERENCE_NOT_FOUND.
	stuck, err := svcB.Submit(wagering.SubmitInput{
		ProviderID: "provider-a", ExternalID: "refund-" + uid(t), IdempotencyKey: "key-" + uid(t),
		PlayerID: w.PlayerID, WalletID: w.ID, RoundID: "round-1", GameID: "game-1",
		Kind: domain.KindRefund, AmountText: "5.00", Currency: "BRL",
		ReferenceExternal: "never-" + uid(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svcB.DB.Transact(func(db ports.DB) error {
		return db.Aux().TouchWork(stuck.TransactionID, wagering.MaxRefAttempts-1, 0,
			svcB.Clock.Now(), true)
	}); err != nil {
		t.Fatal(err)
	}
	svcC := openService(t)
	n, err := svcC.ProcessDueWork("instance-c", 10)
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatal("expected instance-c to settle the stuck refund")
	}
	if err := svcC.DB.Transact(func(db ports.DB) error {
		rec, err := db.Tx().Get(stuck.TransactionID)
		if err != nil || rec == nil {
			return err
		}
		if rec.State != "REJECTED" || rec.FailureCode == nil || *rec.FailureCode != wagering.CodeRefNotFound {
			t.Errorf("stuck = %s %v", rec.State, rec.FailureCode)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// 1000 - 80 (bet) - 80 (late bet) + 80 (refund) = 920... plus the opening:
	// OPENING credit + 3 balance changes settle at 920.00.
	assertLedgerAndBalance(t, svcC, w.ID, "920.00", 4)
}
