//go:build integration

package tests

import (
	"errors"
	"testing"

	"github.com/jg-backend-challenge/wallet/internal/domain"
	"github.com/jg-backend-challenge/wallet/internal/wagering"
)

// TestReplayAndConflict covers 3.2: same hash replays with the original
// observed balance; divergent content conflicts; inbox poisons to DLQ signal.
func TestReplayAndConflict(t *testing.T) {
	svc := openService(t)
	w := openWallet(t, svc, "1000.00")
	ext := "ext-" + uid(t)
	key := "key-" + uid(t)

	first := submitBET(t, svc, w, ext, key, "80.00")
	if first.Balance.String() != "920.00" {
		t.Fatalf("first balance = %s", first.Balance)
	}
	// Move the wallet so replay fidelity is observable.
	submitBET(t, svc, w, "ext-"+uid(t), "key-"+uid(t), "20.00")

	again, err := svc.Submit(wagering.SubmitInput{
		ProviderID: "provider-a", ExternalID: ext, IdempotencyKey: key,
		PlayerID: w.PlayerID, WalletID: w.ID, RoundID: "round-1", GameID: "game-1",
		Kind: domain.KindBet, AmountText: "80.00", Currency: "BRL",
	})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !again.IdempotentReplay || again.Outcome != wagering.OutcomeReplay {
		t.Fatalf("replay flags = %+v", again)
	}
	if again.Balance.String() != "920.00" {
		t.Fatalf("replay balance = %s, want original 920.00", again.Balance)
	}

	// Same key, divergent content -> 409.
	_, err = svc.Submit(wagering.SubmitInput{
		ProviderID: "provider-a", ExternalID: ext, IdempotencyKey: key,
		PlayerID: w.PlayerID, WalletID: w.ID, RoundID: "round-1", GameID: "game-1",
		Kind: domain.KindBet, AmountText: "81.00", Currency: "BRL",
	})
	var conflict *wagering.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("conflict err = %v", err)
	}

	// Cross-channel: same business, new key, same external -> replay.
	cross, err := svc.Submit(wagering.SubmitInput{
		ProviderID: "provider-a", ExternalID: ext, IdempotencyKey: "key-" + uid(t),
		PlayerID: w.PlayerID, WalletID: w.ID, RoundID: "round-1", GameID: "game-1",
		Kind: domain.KindBet, AmountText: "80.00", Currency: "BRL",
	})
	if err != nil {
		t.Fatalf("cross-channel: %v", err)
	}
	if !cross.IdempotentReplay {
		t.Fatalf("cross-channel not replay: %+v", cross)
	}

	// SQS inbox: same message + same content replays; divergent poisons.
	msg := "msg-" + uid(t)
	sqsExt := "ext-" + uid(t)
	sqsIn := wagering.SubmitInput{
		ProviderID: "provider-a", ExternalID: sqsExt, IdempotencyKey: "key-" + uid(t),
		PlayerID: w.PlayerID, WalletID: w.ID, RoundID: "round-1", GameID: "game-1",
		Kind: domain.KindBet, AmountText: "10.00", Currency: "BRL",
		ConsumerName: "consumer", MessageID: msg,
	}
	if _, err := svc.Submit(sqsIn); err != nil {
		t.Fatalf("sqs first: %v", err)
	}
	sqsIn.IdempotencyKey = "key-" + uid(t)
	if _, err := svc.Submit(sqsIn); err != nil {
		t.Fatalf("sqs redelivery: %v", err)
	}
	sqsIn.AmountText = "11.00"
	sqsIn.IdempotencyKey = "key-" + uid(t)
	_, err = svc.Submit(sqsIn)
	var poison *wagering.PoisonError
	if !errors.As(err, &poison) {
		t.Fatalf("poison err = %v", err)
	}
}
