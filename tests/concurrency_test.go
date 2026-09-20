//go:build integration

package tests

import (
	"sync"
	"testing"

	"github.com/jg-backend-challenge/wallet/internal/domain"
	"github.com/jg-backend-challenge/wallet/internal/ports"
	"github.com/jg-backend-challenge/wallet/internal/wagering"
)

// Concurrency battery (6.1, §13): parallel submits must converge to exactly
// one financial effect per logical operation, with a consistent ledger and
// no lost updates. The outbox table is shared across tests, so every
// assertion scopes to the wallets created here.

// raceOne fires n Submit calls through a start barrier and returns per-index
// results. Errors are collected, never raised inside goroutines.
func raceSubmit(svc *wagering.Service, inputs []wagering.SubmitInput) ([]*wagering.SubmitResult, []error) {
	n := len(inputs)
	results := make([]*wagering.SubmitResult, n)
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range inputs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			r, err := svc.Submit(inputs[i])
			results[i], errs[i] = r, err
		}(i)
	}
	close(start)
	wg.Wait()
	return results, errs
}

// assertLedgerAndBalance checks the stored balance, the full ledger entry
// count (opening + processed balance changes) and REPEATABLE READ
// reconciliation for one wallet.
func assertLedgerAndBalance(t *testing.T, svc *wagering.Service, walletID, wantBalance string, wantEntries int) {
	t.Helper()
	entries := allLedgerEntries(t, svc, walletID)
	if len(entries) != wantEntries {
		t.Fatalf("wallet %s ledger entries = %d, want %d", walletID, len(entries), wantEntries)
	}
	rec, err := svc.Reconcile(walletID)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !rec.Consistent {
		t.Fatalf("wallet %s inconsistent: stored %s calculated %s", walletID, rec.StoredBalance, rec.CalculatedBalance)
	}
	if rec.StoredBalance.String() != wantBalance {
		t.Fatalf("wallet %s stored = %s, want %s", walletID, rec.StoredBalance, wantBalance)
	}
	if rec.CheckedEntries != wantEntries {
		t.Fatalf("wallet %s checkedEntries = %d, want %d", walletID, rec.CheckedEntries, wantEntries)
	}
}

func allLedgerEntries(t *testing.T, svc *wagering.Service, walletID string) []*ports.LedgerRecord {
	t.Helper()
	var all []*ports.LedgerRecord
	cursor := ""
	for {
		page, err := svc.PageLedger(walletID, cursor, 100)
		if err != nil {
			t.Fatalf("page ledger: %v", err)
		}
		all = append(all, page.Entries...)
		if page.NextCursor == "" {
			return all
		}
		cursor = page.NextCursor
	}
}

// TestConcurrentSameBet50x covers the idempotency-contracts scenario "same
// operation 50 times in parallel, crossing HTTP and SQS": one debit, every
// other submit replays the persisted result with the original balance.
func TestConcurrentSameBet50x(t *testing.T) {
	svc := openService(t)
	w := openWallet(t, svc, "1000.00")
	ext := "ext-" + uid(t)
	key := "key-" + uid(t)

	const n = 50
	msgs := make([]string, n)
	for i := range msgs {
		msgs[i] = "msg-" + uid(t)
	}
	inputs := make([]wagering.SubmitInput, n)
	for i := range inputs {
		inputs[i] = wagering.SubmitInput{
			ProviderID: "provider-a", ExternalID: ext, IdempotencyKey: key,
			PlayerID: w.PlayerID, WalletID: w.ID, RoundID: "round-1", GameID: "game-1",
			Kind: domain.KindBet, AmountText: "80.00", Currency: "BRL",
		}
		// Odd lanes arrive as SQS redeliveries of the same business
		// content in distinct envelopes: cross-channel replay, not just
		// inbox dedup on one messageId.
		if i%2 == 1 {
			inputs[i].ConsumerName = "consumer"
			inputs[i].MessageID = msgs[i]
		}
	}

	results, errs := raceSubmit(svc, inputs)
	var processed, replays int
	var txID string
	for i := range results {
		if errs[i] != nil {
			t.Fatalf("submit %d err = %v", i, errs[i])
		}
		r := results[i]
		if txID == "" {
			txID = r.TransactionID
		} else if r.TransactionID != txID {
			t.Fatalf("submit %d transaction %s, want single %s", i, r.TransactionID, txID)
		}
		switch r.Outcome {
		case wagering.OutcomeProcessed:
			processed++
		case wagering.OutcomeReplay:
			replays++
			if !r.IdempotentReplay {
				t.Fatalf("submit %d replay without flag: %+v", i, r)
			}
		default:
			t.Fatalf("submit %d outcome = %s, want processed|replay", i, r.Outcome)
		}
		if r.Balance == nil || r.Balance.String() != "920.00" {
			t.Fatalf("submit %d balance = %v, want original 920.00", i, r.Balance)
		}
	}
	if processed != 1 || replays != n-1 {
		t.Fatalf("processed = %d, replays = %d, want 1 and %d", processed, replays, n-1)
	}

	assertLedgerAndBalance(t, svc, w.ID, "920.00", 2) // OPENING + one debit
	entries := allLedgerEntries(t, svc, w.ID)
	debits := 0
	for _, e := range entries {
		if e.Direction == "DEBIT" {
			debits++
			if e.AmountMinor != 8000 || e.TransactionID != txID {
				t.Fatalf("debit entry = %+v, want 8000 on %s", e, txID)
			}
		}
	}
	if debits != 1 {
		t.Fatalf("debit entries = %d, want exactly 1", debits)
	}
}

// TestConcurrentDistinctBets100_80_80 covers the wallet-ledger scenario
// "two 80 bets racing on a 100 wallet": exactly one bet wins, losers persist
// REJECTED with INSUFFICIENT_BALANCE, and the ledger holds a single debit.
func TestConcurrentDistinctBets100_80_80(t *testing.T) {
	svc := openService(t)
	amounts := []string{"100.00", "80.00", "80.00"}
	for iter := 0; iter < 3; iter++ {
		w := openWallet(t, svc, "100.00")
		inputs := make([]wagering.SubmitInput, len(amounts))
		for i, amount := range amounts {
			inputs[i] = wagering.SubmitInput{
				ProviderID: "provider-a", ExternalID: "ext-" + uid(t), IdempotencyKey: "key-" + uid(t),
				PlayerID: w.PlayerID, WalletID: w.ID, RoundID: "round-1", GameID: "game-1",
				Kind: domain.KindBet, AmountText: amount, Currency: "BRL",
			}
		}
		results, errs := raceSubmit(svc, inputs)
		var winner string
		processed, rejected := 0, 0
		for i := range results {
			if errs[i] != nil {
				t.Fatalf("iter %d submit %d err = %v", iter, i, errs[i])
			}
			switch results[i].Outcome {
			case wagering.OutcomeProcessed:
				processed++
				winner = amounts[i]
			case wagering.OutcomeRejected:
				rejected++
				if results[i].FailureCode != domain.CodeInsufficientBalance {
					t.Fatalf("iter %d loser code = %s, want INSUFFICIENT_BALANCE", iter, results[i].FailureCode)
				}
			default:
				t.Fatalf("iter %d submit %d outcome = %s", iter, i, results[i].Outcome)
			}
		}
		if processed != 1 || rejected != 2 {
			t.Fatalf("iter %d processed = %d rejected = %d, want 1 and 2", iter, processed, rejected)
		}
		wantBalance := "20.00"
		if winner == "100.00" {
			wantBalance = "0.00"
		}
		assertLedgerAndBalance(t, svc, w.ID, wantBalance, 2)
	}
}

// TestConcurrentDistinctWallets covers the wallet-ledger scenario "distinct
// wallets in parallel": no global lock, no lost updates, every wallet settles
// independently.
func TestConcurrentDistinctWallets(t *testing.T) {
	svc := openService(t)
	const wallets = 8
	ids := make([]*ports.WalletRecord, wallets)
	for i := range ids {
		ids[i] = openWallet(t, svc, "100.00")
	}
	inputs := make([]wagering.SubmitInput, wallets)
	for i, w := range ids {
		inputs[i] = wagering.SubmitInput{
			ProviderID: "provider-a", ExternalID: "ext-" + uid(t), IdempotencyKey: "key-" + uid(t),
			PlayerID: w.PlayerID, WalletID: w.ID, RoundID: "round-1", GameID: "game-1",
			Kind: domain.KindBet, AmountText: "30.00", Currency: "BRL",
		}
	}
	results, errs := raceSubmit(svc, inputs)
	for i := range results {
		if errs[i] != nil {
			t.Fatalf("wallet %d err = %v", i, errs[i])
		}
		if results[i].Outcome != wagering.OutcomeProcessed {
			t.Fatalf("wallet %d outcome = %s, want processed", i, results[i].Outcome)
		}
	}
	for _, w := range ids {
		assertLedgerAndBalance(t, svc, w.ID, "70.00", 2)
	}
}

// TestConcurrentThreeInstances runs three independent Service instances over
// the same database: distinct operations spread across instances all apply
// without lost updates, and one logical operation raced across instances
// debits exactly once.
func TestConcurrentThreeInstances(t *testing.T) {
	svcs := []*wagering.Service{openService(t), openService(t), openService(t)}

	// Distinct bets round-robin over instances: 10 x 10.00 on 100.00.
	w := openWallet(t, svcs[0], "100.00")
	const bets = 10
	inputs := make([]wagering.SubmitInput, bets)
	for i := range inputs {
		inputs[i] = wagering.SubmitInput{
			ProviderID: "provider-a", ExternalID: "ext-" + uid(t), IdempotencyKey: "key-" + uid(t),
			PlayerID: w.PlayerID, WalletID: w.ID, RoundID: "round-1", GameID: "game-1",
			Kind: domain.KindBet, AmountText: "10.00", Currency: "BRL",
		}
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]*wagering.SubmitResult, bets)
	errs := make([]error, bets)
	for i := range inputs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			r, err := svcs[i%len(svcs)].Submit(inputs[i])
			results[i], errs[i] = r, err
		}(i)
	}
	close(start)
	wg.Wait()
	for i := range results {
		if errs[i] != nil {
			t.Fatalf("bet %d err = %v", i, errs[i])
		}
		if results[i].Outcome != wagering.OutcomeProcessed {
			t.Fatalf("bet %d outcome = %s, want processed", i, results[i].Outcome)
		}
	}
	assertLedgerAndBalance(t, svcs[0], w.ID, "0.00", 11) // OPENING + 10 debits

	// Same operation raced across all three instances: one debit.
	w2 := openWallet(t, svcs[0], "1000.00")
	ext := "ext-" + uid(t)
	key := "key-" + uid(t)
	const racers = 30
	same := make([]wagering.SubmitInput, racers)
	for i := range same {
		same[i] = wagering.SubmitInput{
			ProviderID: "provider-a", ExternalID: ext, IdempotencyKey: key,
			PlayerID: w2.PlayerID, WalletID: w2.ID, RoundID: "round-1", GameID: "game-1",
			Kind: domain.KindBet, AmountText: "5.00", Currency: "BRL",
		}
	}
	start2 := make(chan struct{})
	results2 := make([]*wagering.SubmitResult, racers)
	errs2 := make([]error, racers)
	for i := range same {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start2
			r, err := svcs[i%len(svcs)].Submit(same[i])
			results2[i], errs2[i] = r, err
		}(i)
	}
	close(start2)
	wg.Wait()
	processed, replays := 0, 0
	for i := range results2 {
		if errs2[i] != nil {
			t.Fatalf("racer %d err = %v", i, errs2[i])
		}
		switch results2[i].Outcome {
		case wagering.OutcomeProcessed:
			processed++
		case wagering.OutcomeReplay:
			replays++
		default:
			t.Fatalf("racer %d outcome = %s", i, results2[i].Outcome)
		}
	}
	if processed != 1 || replays != racers-1 {
		t.Fatalf("processed = %d replays = %d, want 1 and %d", processed, replays, racers-1)
	}
	assertLedgerAndBalance(t, svcs[1], w2.ID, "995.00", 2)
}
