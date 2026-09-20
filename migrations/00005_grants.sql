-- +goose Up
-- Least-privilege grants for the app role. Roles themselves are created
-- outside migrations (postgres init script / operator), so no secrets live here.
GRANT CONNECT ON DATABASE wallet TO app;
GRANT USAGE ON SCHEMA public TO app;
GRANT SELECT, INSERT, UPDATE ON wallets TO app;
GRANT SELECT, INSERT, UPDATE ON wager_transactions TO app;
GRANT SELECT, INSERT ON wallet_ledger_entries TO app;
GRANT SELECT, INSERT, UPDATE ON inbox TO app;
GRANT SELECT, INSERT, UPDATE ON outbox TO app;
GRANT SELECT, INSERT, UPDATE ON work_items TO app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO app;

-- +goose Down
REVOKE ALL ON work_items FROM app;
REVOKE ALL ON outbox FROM app;
REVOKE ALL ON inbox FROM app;
REVOKE ALL ON wallet_ledger_entries FROM app;
REVOKE ALL ON wager_transactions FROM app;
REVOKE ALL ON wallets FROM app;
