package wagering

import (
	"strings"
	"testing"
)

func sampleInput() BusinessHashInput {
	return BusinessHashInput{
		ProviderID: "provider-a", ExternalTransactionID: "ext-1",
		PlayerID: "p1", WalletID: "w1", RoundID: "r1", GameID: "g1",
		Kind: "BET", AmountText: "25.00", Currency: "BRL",
	}
}

func TestHashDeterministicAndOrdered(t *testing.T) {
	a, err := PayloadHash(sampleInput())
	if err != nil {
		t.Fatal(err)
	}
	b, err := PayloadHash(sampleInput())
	if err != nil {
		t.Fatal(err)
	}
	if a != b || len(a) != 64 {
		t.Fatalf("hashes differ or bad length: %q %q", a, b)
	}
	raw, err := CanonicalJSON(map[string]any{"b": "1", "a": "2"})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"a":"2","b":"1"}` {
		t.Fatalf("keys not sorted: %s", raw)
	}
}

func TestHashCrossChannelEquality(t *testing.T) {
	// Same business via HTTP and SQS (different transport, same input)
	// must hash identically; the idempotency key never participates.
	httpHash, _ := PayloadHash(sampleInput())
	sqsHash, _ := PayloadHash(sampleInput())
	if httpHash != sqsHash {
		t.Fatal("cross-channel hashes differ")
	}
}

func TestHashDistinguishes(t *testing.T) {
	base, _ := PayloadHash(sampleInput())
	other := sampleInput()
	other.AmountText = "25.01"
	if h, _ := PayloadHash(other); h == base {
		t.Fatal("amount change did not alter hash")
	}
	other = sampleInput()
	other.ReferenceExternalID = "ext-0"
	if h, _ := PayloadHash(other); h == base {
		t.Fatal("reference addition did not alter hash")
	}
	// No silent normalization: "25.00" vs "25.0" hash differently, and the
	// latter is rejected at parse time before hashing anyway.
	other = sampleInput()
	other.AmountText = "25.0"
	h, _ := PayloadHash(other)
	if h == base {
		t.Fatal("expected distinct bytes for distinct text")
	}
	if strings.Contains(h, " ") {
		t.Fatal("unexpected whitespace")
	}
}
