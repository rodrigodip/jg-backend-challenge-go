//go:build integration

package tests

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	adaptersqs "github.com/jg-backend-challenge/wallet/internal/adapters/sqs"
	"github.com/jg-backend-challenge/wallet/internal/ports"
	"github.com/jg-backend-challenge/wallet/internal/wagering"
)

// stubPublisher captures sends. When failWallet is set, the first send of
// each of that wallet's events fails, forcing every row through the nack +
// republish path with a stable eventId.
type stubPublisher struct {
	failWallet string
	failed     map[string]bool
	sent       []*ports.OutboxRecord
}

func (s *stubPublisher) Publish(_ context.Context, e *ports.OutboxRecord) error {
	if s.failed == nil {
		s.failed = map[string]bool{}
	}
	if e.AggregateID == s.failWallet && !s.failed[e.EventID] {
		s.failed[e.EventID] = true
		return errStubPublish
	}
	s.sent = append(s.sent, e)
	return nil
}

type stubError struct{}

func (stubError) Error() string { return "stub publish failure" }

var errStubPublish = stubError{}

// drainEvents receives and deletes events-queue messages for one wallet,
// returning the eventIds seen. Every envelope must carry the wallet version
// for consumer ordering. It polls until two consecutive empty rounds (the
// queue may hold unrelated backlog ahead of this wallet's events) with an
// overall deadline so a genuinely missing delivery still fails fast.
func drainEvents(t *testing.T, client *sqs.Client, eventsURL, walletID string) []string {
	t.Helper()
	ctx := context.Background()
	var seen []string
	deadline := time.Now().Add(70 * time.Second)
	empty := 0
	for i := 0; i < 30 && empty < 2; i++ {
		if time.Now().After(deadline) {
			break
		}
		out, err := client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl: &eventsURL, MaxNumberOfMessages: 10, WaitTimeSeconds: 2,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(out.Messages) == 0 {
			empty++
			continue
		}
		empty = 0
		for _, m := range out.Messages {
			var env struct {
				EventID     string         `json:"eventId"`
				AggregateID string         `json:"aggregateId"`
				Version     *int64         `json:"version"`
				Data        map[string]any `json:"data"`
			}
			if err := json.Unmarshal([]byte(aws.ToString(m.Body)), &env); err != nil {
				t.Fatal(err)
			}
			if env.AggregateID == walletID {
				if env.Version == nil {
					t.Errorf("envelope without version: %s", aws.ToString(m.Body))
				}
				if _, ok := env.Data["walletVersion"]; !ok {
					t.Errorf("envelope data without walletVersion: %s", aws.ToString(m.Body))
				}
				seen = append(seen, env.EventID)
			}
			if _, err := client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
				QueueUrl: &eventsURL, ReceiptHandle: m.ReceiptHandle,
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	return seen
}

func unpublishedIDs(t *testing.T, svc *wagering.Service, walletID string) map[string]bool {
	t.Helper()
	pending, err := svc.DB.Aux().ListUnpublished(walletID)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, p := range pending {
		ids[p.EventID] = true
	}
	return ids
}

// publishUntilDrained loops PublishDueOutbox until a quiescent round. The
// outbox table is global and accumulates rows across tests and runs, so one
// claim round (limit 100) may miss a wallet's rows. It reports errors
// instead of failing so racing goroutines can stay legal.
func publishUntilDrained(ctx context.Context, svc *wagering.Service, pub adaptersqs.Publisher, owner string) error {
	for i := 0; i < 20; i++ {
		n, err := adaptersqs.PublishDueOutbox(ctx, svc.DB, pub, owner, 100)
		if err != nil {
			return err
		}
		if n == 0 {
			return nil
		}
	}
	return errOutboxNeverQuiescent
}

type quiescentError struct{}

func (quiescentError) Error() string { return "outbox never quiescent" }

var errOutboxNeverQuiescent = quiescentError{}

// publishUntilWalletDrained loops PublishDueOutbox until the wallet has no
// unpublished rows left. Backoff-delayed nacks make global quiescence the
// wrong condition: a quiet round may precede a due retry.
func publishUntilWalletDrained(t *testing.T, ctx context.Context, svc *wagering.Service, pub adaptersqs.Publisher, owner, walletID string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := adaptersqs.PublishDueOutbox(ctx, svc.DB, pub, owner, 100); err != nil {
			t.Fatal(err)
		}
		if len(unpublishedIDs(t, svc, walletID)) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("wallet %s outbox never drained", walletID)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func TestOutboxPublishDispute(t *testing.T) {
	ctx := context.Background()
	svc := openService(t)
	w := openWallet(t, svc, "100.00")
	submitBET(t, svc, w, "bet-"+uid(t), "key-"+uid(t), "20.00")
	// Fresh wallet: 2 opening events + 2 bet events.
	want := unpublishedIDs(t, svc, w.ID)
	if len(want) != 4 {
		t.Fatalf("pending for wallet = %d, want 4 (2 opening + 2 bet)", len(want))
	}

	// Two publishers race the same pending rows with in-memory sinks: SKIP
	// LOCKED gives each event to exactly one of them (the outbox table is
	// shared with other tests, so assertions scope to this wallet).
	stubA, stubB := &stubPublisher{}, &stubPublisher{}
	errs := make(chan error, 2)
	race := func(owner string, stub *stubPublisher) {
		errs <- publishUntilDrained(ctx, svc, stub, owner)
	}
	go race("owner-a", stubA)
	go race("owner-b", stubB)
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
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
	if rest := unpublishedIDs(t, svc, w.ID); len(rest) != 0 {
		t.Fatalf("unpublished leftovers: %v", rest)
	}
}

func TestOutboxBrokerDelivery(t *testing.T) {
	ctx := context.Background()
	svc := openService(t)
	w := openWallet(t, svc, "100.00")
	submitBET(t, svc, w, "bet-"+uid(t), "key-"+uid(t), "20.00")
	want := unpublishedIDs(t, svc, w.ID)

	client := sqsTestClient(t)
	eventsURL, err := adaptersqs.QueueURL(ctx, client, "wager-events.fifo")
	if err != nil {
		t.Fatal(err)
	}
	pub := &adaptersqs.SQSPublisher{Client: client, EventsURL: eventsURL}
	publishUntilWalletDrained(t, ctx, svc, pub, "owner-e2e", w.ID)
	seen := drainEvents(t, client, eventsURL, w.ID)
	if len(seen) != len(want) {
		t.Fatalf("events for wallet = %d, want %d", len(seen), len(want))
	}
	var unexpected []string
	for _, id := range seen {
		if !want[id] {
			unexpected = append(unexpected, id)
		}
	}
	if len(unexpected) > 0 {
		t.Fatalf("unexpected events delivered: %v; want %v", unexpected, keys(want))
	}
}

func keys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestOutboxStableEventIDAndRecovery(t *testing.T) {
	ctx := context.Background()
	svc := openService(t)
	w := openWallet(t, svc, "100.00")
	submitBET(t, svc, w, "bet-"+uid(t), "key-"+uid(t), "20.00")

	pending, err := svc.DB.Aux().ListUnpublished(w.ID)
	if err != nil || len(pending) == 0 {
		t.Fatalf("pending = %d, %v", len(pending), err)
	}
	original := map[string]bool{}
	for _, p := range pending {
		original[p.EventID] = true
	}

	// Every send of this wallet's events fails once: all rows go through
	// nack + republish, and the eventIds must stay stable. (Only this
	// wallet's rows are asserted; the table is shared.)
	stub := &stubPublisher{failWallet: w.ID}
	publishUntilWalletDrained(t, ctx, svc, stub, "owner-a", w.ID)
	for _, s := range stub.sent {
		if s.AggregateID != w.ID {
			continue
		}
		if !original[s.EventID] {
			t.Fatalf("eventId changed across republish: %s", s.EventID)
		}
	}
	if rest := unpublishedIDs(t, svc, w.ID); len(rest) != 0 {
		t.Fatalf("unpublished leftovers: %v", rest)
	}

	// Crash recovery: rows leased to a dead owner resume after expiry.
	submitBET(t, svc, w, "bet-"+uid(t), "key-"+uid(t), "5.00")
	ours := unpublishedIDs(t, svc, w.ID)
	if _, err := svc.DB.Aux().ClaimOutbox("dead-owner", time.Second, 100); err != nil {
		t.Fatalf("crash claim: %v", err)
	}
	time.Sleep(1500 * time.Millisecond)
	stub2 := &stubPublisher{}
	publishUntilWalletDrained(t, ctx, svc, stub2, "owner-b", w.ID)
	for id := range ours {
		found := false
		for _, s := range stub2.sent {
			if s.EventID == id {
				found = true
			}
		}
		if !found {
			t.Fatalf("crashed event never recovered: %s", id)
		}
	}
	if rest := unpublishedIDs(t, svc, w.ID); len(rest) != 0 {
		t.Fatalf("unpublished leftovers after recovery: %v", rest)
	}
}

func TestReferenceWorkerLoop(t *testing.T) {
	svc := openService(t)
	w := openWallet(t, svc, "100.00")
	svc.NewID = wagering.NewUUID

	// Park a REFUND before its BET, then process the BET below the worker.
	refExt := "bet-" + uid(t)
	r, err := svc.Submit(wagering.SubmitInput{
		ProviderID: "provider-a", ExternalID: "refund-" + uid(t), IdempotencyKey: "key-" + uid(t),
		PlayerID: w.PlayerID, WalletID: w.ID, RoundID: "round-1", GameID: "game-1",
		Kind: "REFUND", AmountText: "10.00", Currency: "BRL", ReferenceExternal: refExt,
	})
	if err != nil || r.Outcome != wagering.OutcomePending {
		t.Fatalf("park = %v, %v", r, err)
	}
	submitBET(t, svc, w, refExt, "key-"+uid(t), "10.00")

	// The loop must settle work even if immediate re-evaluation is skipped;
	// here it converges the already-reevaluated item to nothing pending.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		adaptersqs.RunReferenceWorker(ctx, svc, "test-owner", 100*time.Millisecond, nil)
		close(done)
	}()
	<-done
	rec, err := svc.DB.Tx().Get(r.TransactionID)
	if err != nil || rec == nil {
		t.Fatal(err)
	}
	if rec.State != "PROCESSED" {
		t.Fatalf("dependent state = %s, want PROCESSED", rec.State)
	}
}
