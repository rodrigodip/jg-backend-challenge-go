// Package wagering implements the transactional wager use-case: hybrid
// accept (inline or 202), financial idempotency via canonical hashing,
// reference/work-table resumption, ledger cursors and reconciliation.
package wagering

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// BusinessHashInput carries the ten business fields hashed for financial
// idempotency. Transport metadata (idempotency key, message id, timestamps)
// is excluded so HTTP and SQS produce identical hashes for the same business.
type BusinessHashInput struct {
	ProviderID            string
	ExternalTransactionID string
	PlayerID              string
	WalletID              string
	RoundID               string
	GameID                string
	Kind                  string
	AmountText            string // raw external "25.00", never normalized
	Currency              string
	ReferenceExternalID   string // empty when absent (field omitted)
}

// CanonicalJSON renders v with keys sorted and no whitespace (RFC 8785
// profile restricted to our string-only business payload). Both channels
// share this function, so equal business always yields equal bytes.
func CanonicalJSON(v any) ([]byte, error) {
	return canonicalValue(v)
}

func canonicalValue(v any) ([]byte, error) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var buf bytes.Buffer
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			kb, err := canonicalString(k)
			if err != nil {
				return nil, err
			}
			buf.Write(kb)
			buf.WriteByte(':')
			vb, err := canonicalValue(t[k])
			if err != nil {
				return nil, err
			}
			buf.Write(vb)
		}
		buf.WriteByte('}')
		return buf.Bytes(), nil
	case string:
		return canonicalString(t)
	default:
		// No floats or numbers may carry money; other scalars are rejected
		// by construction of BusinessHashInput (all strings).
		return nil, &json.UnsupportedTypeError{}
	}
}

// canonicalString JSON-encodes one string without HTML escaping and without
// surrounding whitespace, matching RFC 8785 string output for our alphabet.
func canonicalString(s string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return nil, err
	}
	out := buf.Bytes()
	// Encoder appends a newline; strip it.
	return bytes.TrimRight(out, "\n"), nil
}

// PayloadHash returns hex(SHA-256(canonical JSON)) of the business fields.
func PayloadHash(in BusinessHashInput) (string, error) {
	doc := map[string]any{
		"providerId":            in.ProviderID,
		"externalTransactionId": in.ExternalTransactionID,
		"playerId":              in.PlayerID,
		"walletId":              in.WalletID,
		"roundId":               in.RoundID,
		"gameId":                in.GameID,
		"kind":                  in.Kind,
		"money": map[string]any{
			"amount":   in.AmountText,
			"currency": in.Currency,
		},
	}
	if in.ReferenceExternalID != "" {
		doc["referenceExternalTransactionId"] = in.ReferenceExternalID
	}
	raw, err := CanonicalJSON(doc)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
