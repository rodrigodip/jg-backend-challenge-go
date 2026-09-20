-- +goose Up
CREATE TABLE wallet_ledger_entries (
    id UUID PRIMARY KEY,
    wallet_id UUID NOT NULL REFERENCES wallets (id),
    transaction_id UUID NOT NULL REFERENCES wager_transactions (id),
    direction TEXT NOT NULL CONSTRAINT ledger_direction CHECK (direction IN ('DEBIT', 'CREDIT')),
    amount_minor BIGINT NOT NULL CONSTRAINT ledger_amount_positive CHECK (amount_minor > 0),
    currency CHAR(3) NOT NULL,
    balance_before_minor BIGINT NOT NULL CONSTRAINT ledger_before_non_negative CHECK (balance_before_minor >= 0),
    balance_after_minor BIGINT NOT NULL CONSTRAINT ledger_after_non_negative CHECK (balance_after_minor >= 0),
    wallet_version BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ledger_wallet_tx_unique UNIQUE (wallet_id, transaction_id),
    CONSTRAINT ledger_balance_math CHECK (
        (direction = 'DEBIT' AND balance_after_minor = balance_before_minor - amount_minor)
        OR
        (direction = 'CREDIT' AND balance_after_minor = balance_before_minor + amount_minor)
    )
);

-- Append-only enforcement at the database level, independent of app roles.
-- +goose StatementBegin
CREATE FUNCTION prevent_ledger_write() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'wallet_ledger_entries is append-only';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER wallet_ledger_entries_no_update_delete
    BEFORE UPDATE OR DELETE ON wallet_ledger_entries
    FOR EACH ROW EXECUTE FUNCTION prevent_ledger_write();
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS wallet_ledger_entries_no_update_delete ON wallet_ledger_entries;
DROP FUNCTION IF EXISTS prevent_ledger_write();
DROP TABLE IF EXISTS wallet_ledger_entries;
