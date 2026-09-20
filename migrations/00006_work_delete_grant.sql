-- +goose Up
-- Finished work items leave the queue via DELETE once their transaction is
-- terminal. The app role otherwise stays append-only on financial tables.
GRANT DELETE ON work_items TO app;

-- +goose Down
REVOKE DELETE ON work_items FROM app;
