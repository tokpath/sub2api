-- Make every commission settlement wallet mutation traceable and idempotent.
-- Historical wallet transactions remain valid with a NULL commission_id.

ALTER TABLE channel_wallet_transactions
    ADD COLUMN IF NOT EXISTS commission_id BIGINT REFERENCES commission_ledger(id) ON DELETE RESTRICT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_channel_wallet_transactions_commission_action_unique
    ON channel_wallet_transactions(commission_id, transaction_type)
    WHERE commission_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_channel_wallet_transactions_commission_id
    ON channel_wallet_transactions(commission_id)
    WHERE commission_id IS NOT NULL;
