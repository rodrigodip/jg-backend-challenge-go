package wagering

import (
	"math/rand"
	"time"
)

// BackoffFullJitter returns a delay in [0, min(cap, base*2^attempt)].
// Reference retries use base 200ms cap 3s (D4); SQS visibility retries are
// owned by the consumer in block 5 with base 1s cap 30s.
func BackoffFullJitter(attempt int, base, cap time.Duration, r *rand.Rand) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	ceiling := base
	for i := 0; i < attempt; i++ {
		ceiling *= 2
		if ceiling >= cap {
			ceiling = cap
			break
		}
	}
	if ceiling > cap {
		ceiling = cap
	}
	if ceiling <= 0 {
		return 0
	}
	return time.Duration(r.Int63n(int64(ceiling) + 1))
}
