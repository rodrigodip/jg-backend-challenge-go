-- +goose Up
CREATE TABLE wallets (
    id UUID PRIMARY KEY,
    player_id UUID NOT NULL,
    currency CHAR(3) NOT NULL,
    balance_minor BIGINT NOT NULL CONSTRAINT wallets_balance_non_negative CHECK (balance_minor >= 0),
    version BIGINT NOT NULL CONSTRAINT wallets_version_min CHECK (version >= 1),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT wallets_player_currency_unique UNIQUE (player_id, currency)
);

-- +goose Down
DROP TABLE IF EXISTS wallets;
