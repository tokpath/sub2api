-- Link every usage-derived financial mutation to one durable billing event.
-- Historical rows remain valid; new writes populate the nullable linkage fields.

ALTER TABLE billing_usage_entries
    ADD COLUMN IF NOT EXISTS request_id VARCHAR(255),
    ADD COLUMN IF NOT EXISTS request_fingerprint VARCHAR(64),
    ADD COLUMN IF NOT EXISTS channel_org_id BIGINT REFERENCES channel_organizations(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS channel_usage_delta DECIMAL(20,8) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS commission_reserved_delta DECIMAL(20,8) NOT NULL DEFAULT 0;

CREATE UNIQUE INDEX IF NOT EXISTS idx_billing_usage_entries_request_api_key_unique
    ON billing_usage_entries(request_id, api_key_id)
    WHERE request_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_billing_usage_entries_channel_org_id
    ON billing_usage_entries(channel_org_id)
    WHERE channel_org_id IS NOT NULL;

ALTER TABLE channel_wallet_transactions
    ADD COLUMN IF NOT EXISTS billing_usage_entry_id BIGINT REFERENCES billing_usage_entries(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS usage_log_id BIGINT REFERENCES usage_logs(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS request_id VARCHAR(255),
    ADD COLUMN IF NOT EXISTS api_key_id BIGINT REFERENCES api_keys(id) ON DELETE RESTRICT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_channel_wallet_transactions_usage_event_type_unique
    ON channel_wallet_transactions(billing_usage_entry_id, transaction_type)
    WHERE billing_usage_entry_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_channel_wallet_transactions_usage_log_id
    ON channel_wallet_transactions(usage_log_id)
    WHERE usage_log_id IS NOT NULL;

ALTER TABLE commission_ledger
    ADD COLUMN IF NOT EXISTS billing_usage_entry_id BIGINT REFERENCES billing_usage_entries(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS request_id VARCHAR(255),
    ADD COLUMN IF NOT EXISTS api_key_id BIGINT REFERENCES api_keys(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS dedup_key VARCHAR(160) NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_commission_ledger_usage_event_member_type_unique
    ON commission_ledger(billing_usage_entry_id, member_id, commission_type)
    WHERE billing_usage_entry_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_commission_ledger_dedup_key_unique
    ON commission_ledger(dedup_key)
    WHERE dedup_key <> '';

CREATE INDEX IF NOT EXISTS idx_commission_ledger_billing_usage_entry_id
    ON commission_ledger(billing_usage_entry_id)
    WHERE billing_usage_entry_id IS NOT NULL;
