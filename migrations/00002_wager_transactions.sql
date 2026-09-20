-- +goose Up
CREATE TABLE wager_transactions (
    id UUID PRIMARY KEY,
    origin TEXT NOT NULL CONSTRAINT tx_origin CHECK (origin IN ('INTERNAL', 'EXTERNAL')),
    provider_id TEXT,
    external_transaction_id TEXT,
    idempotency_key TEXT,
    payload_hash TEXT,
    wallet_id UUID NOT NULL REFERENCES wallets (id),
    player_id UUID NOT NULL,
    round_id TEXT,
    game_id TEXT,
    kind TEXT NOT NULL CONSTRAINT tx_kind CHECK (kind IN ('OPENING', 'BET', 'WIN', 'LOSS', 'REFUND', 'ROLLBACK')),
    amount_minor BIGINT NOT NULL,
    currency CHAR(3) NOT NULL,
    reference_external_transaction_id TEXT,
    reference_transaction_id UUID REFERENCES wager_transactions (id),
    state TEXT NOT NULL CONSTRAINT tx_state CHECK (state IN ('PENDING', 'PENDING_REFERENCE', 'PROCESSED', 'REJECTED', 'FAILED')),
    failure_code TEXT,
    result_balance_minor BIGINT,
    result_currency CHAR(3),
    reversed_by_kind TEXT,
    reversed_by_transaction_id UUID REFERENCES wager_transactions (id),
    wallet_version_observed BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT tx_origin_fields CHECK (
        (origin = 'INTERNAL'
            AND provider_id IS NULL
            AND external_transaction_id IS NULL
            AND idempotency_key IS NULL
            AND payload_hash IS NULL
            AND round_id IS NULL
            AND game_id IS NULL
            AND reference_external_transaction_id IS NULL
            AND kind = 'OPENING')
        OR
        (origin = 'EXTERNAL'
            AND provider_id IS NOT NULL
            AND external_transaction_id IS NOT NULL
            AND idempotency_key IS NOT NULL
            AND payload_hash IS NOT NULL
            AND round_id IS NOT NULL
            AND game_id IS NOT NULL
            AND kind <> 'OPENING')
    )
);

-- Financial idempotency: one key and one external id per provider.
CREATE UNIQUE INDEX wager_transactions_provider_key_unique
    ON wager_transactions (provider_id, idempotency_key)
    WHERE origin = 'EXTERNAL';
CREATE UNIQUE INDEX wager_transactions_provider_external_unique
    ON wager_transactions (provider_id, external_transaction_id)
    WHERE origin = 'EXTERNAL';
-- At most one internal OPENING credit per wallet.
CREATE UNIQUE INDEX wager_transactions_single_opening
    ON wager_transactions (wallet_id)
    WHERE origin = 'INTERNAL';
-- Immediate re-evaluation of dependents when a reference is processed.
CREATE INDEX wager_transactions_reference_lookup
    ON wager_transactions (provider_id, reference_external_transaction_id)
    WHERE reference_external_transaction_id IS NOT NULL;
-- Worker claim scans.
CREATE INDEX wager_transactions_pending_state
    ON wager_transactions (state)
    WHERE state IN ('PENDING', 'PENDING_REFERENCE');

-- +goose Down
DROP TABLE IF EXISTS wager_transactions;
