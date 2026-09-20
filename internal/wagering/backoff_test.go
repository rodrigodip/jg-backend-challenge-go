package wagering

import (
	"math/rand"
	"testing"
	"time"
)

func TestBackoffFullJitterBounds(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	for attempt := 0; attempt < 12; attempt++ {
		for i := 0; i < 200; i++ {
			d := BackoffFullJitter(attempt, BackoffBase, BackoffCap, r)
			if d < 0 || d > BackoffCap {
				t.Fatalf("attempt %d delay %v out of [0,%v]", attempt, d, BackoffCap)
			}
		}
	}
	// Early attempts stay well under the cap on average; the ceiling grows.
	r0 := rand.New(rand.NewSource(1))
	if got := BackoffFullJitter(0, BackoffBase, BackoffCap, r0); got > BackoffBase {
		t.Fatalf("attempt 0 delay %v > base %v", got, BackoffBase)
	}
	// Deterministic seed replays the same sequence.
	a := rand.New(rand.NewSource(7))
	b := rand.New(rand.NewSource(7))
	for i := 0; i < 10; i++ {
		if da, db := BackoffFullJitter(3, BackoffBase, BackoffCap, a), BackoffFullJitter(3, BackoffBase, BackoffCap, b); da != db {
			t.Fatal("same seed diverged")
		}
	}
	_ = time.Second // keep time import meaningful for readers
}
