-- +goose Up
CREATE TABLE inbox (
    consumer_name TEXT NOT NULL,
    message_id TEXT NOT NULL,
    payload_hash TEXT NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    concluded_at TIMESTAMPTZ,
    CONSTRAINT inbox_consumer_message_unique PRIMARY KEY (consumer_name, message_id)
);

CREATE TABLE outbox (
    event_id UUID PRIMARY KEY,
    aggregate_id UUID NOT NULL,
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    attempts INT NOT NULL DEFAULT 0 CONSTRAINT outbox_attempts_non_negative CHECK (attempts >= 0),
    next_send_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ,
    lease_owner TEXT,
    lease_expires_at TIMESTAMPTZ
);
CREATE INDEX outbox_pending_send
    ON outbox (next_send_at)
    WHERE published_at IS NULL;

CREATE TABLE work_items (
    transaction_id UUID PRIMARY KEY REFERENCES wager_transactions (id),
    kind TEXT NOT NULL CONSTRAINT work_kind CHECK (kind IN ('initial', 'reference')),
    ref_attempts INT NOT NULL DEFAULT 0 CONSTRAINT work_ref_attempts_non_negative CHECK (ref_attempts >= 0),
    infra_attempts INT NOT NULL DEFAULT 0 CONSTRAINT work_infra_attempts_non_negative CHECK (infra_attempts >= 0),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_owner TEXT,
    lease_expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX work_items_due
    ON work_items (next_attempt_at);

-- +goose Down
DROP TABLE IF EXISTS work_items;
DROP TABLE IF EXISTS outbox;
DROP TABLE IF EXISTS inbox;
