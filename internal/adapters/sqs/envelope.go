// Package sqs holds the SQS consumer, the outbox publisher and queue
// helpers (block 5).
//
// The consumer shares the wagering.Service use-case with HTTP, so financial
// guarantees (idempotency, replay, ledger) are identical on both channels.
// Transport identity is the SQS messageId, recorded durably in the inbox in
// the same commit as the treatment; the queue message is deleted only after
// that commit.
//
// Broker limits (documented for 5.3, verified against MiniStack 1.5.13):
// the emulator accepts any credentials, including bogus ones, serves every
// queue from the same default account and enforces no queue policy or role
// separation. Per-role keypairs (consumer, publisher) are still wired
// end-to-end so the topology matches production, but correctness never
// depends on the broker: the consumer revalidates every domain rule,
// dedupes by inbox hash and preserves money on redelivery.
package sqs

import (
	"encoding/json"
	"errors"
	"strings"
)

// OperationData is the business content of one wager message. Field names
// mirror the HTTP contract so the same operation hashes identically on both
// channels (double-uniqueness handler, block 3.2).
type OperationData struct {
	ProviderID            string `json:"providerId"`
	ExternalTransactionID string `json:"externalTransactionId"`
	PlayerID              string `json:"playerId"`
	WalletID              string `json:"walletId"`
	RoundID               string `json:"roundId"`
	GameID                string `json:"gameId"`
	Kind                  string `json:"kind"`
	Amount                string `json:"amount"`
	Currency              string `json:"currency"`
	ReferenceExternalID   string `json:"referenceExternalTransactionId,omitempty"`
	IdempotencyKey        string `json:"idempotencyKey"`
}

// Envelope is the SQS message body.
type Envelope struct {
	Data          OperationData `json:"data"`
	CorrelationID string        `json:"correlationId,omitempty"`
}

// errPermanent marks envelopes that can never process, however often they
// are retried: malformed JSON, missing business content or a missing
// idempotency key. The consumer routes them to the DLQ instead of burning
// redrive budgets.
var errPermanent = errors.New("permanent envelope error")

// PermanentError reports whether err is a never-retryable envelope failure.
func PermanentError(err error) bool { return errors.Is(err, errPermanent) }

func permanent(msg string) error { return &envelopeError{msg: msg} }

type envelopeError struct{ msg string }

func (e *envelopeError) Error() string        { return "permanent envelope: " + e.msg }
func (e *envelopeError) Is(target error) bool { return target == errPermanent }

// ParseEnvelope decodes and validates the transport envelope. Business
// validation (amounts, wallets, references) stays in the domain via Submit;
// only structural defects that retry cannot fix are permanent here.
func ParseEnvelope(body []byte) (Envelope, error) {
	var env Envelope
	dec := json.NewDecoder(strings.NewReader(string(body)))
	if err := dec.Decode(&env); err != nil {
		return Envelope{}, permanent("invalid JSON: " + err.Error())
	}
	d := env.Data
	if d.ProviderID == "" || d.ExternalTransactionID == "" {
		return Envelope{}, permanent("providerId and externalTransactionId are required")
	}
	if d.IdempotencyKey == "" {
		return Envelope{}, permanent("idempotencyKey is required")
	}
	if d.Kind == "" {
		return Envelope{}, permanent("kind is required")
	}
	return env, nil
}
