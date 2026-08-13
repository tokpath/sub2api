-- Persist distribution financial reconciliation runs and actionable differences.
-- Fingerprints keep one auditable lifecycle per invariant instead of creating
-- duplicate rows on every scheduled scan.

CREATE TABLE IF NOT EXISTS distribution_reconciliation_runs (
    id BIGSERIAL PRIMARY KEY,
    trigger_type VARCHAR(20) NOT NULL,
    status VARCHAR(20) NOT NULL,
    issue_count INTEGER NOT NULL DEFAULT 0,
    resolved_count INTEGER NOT NULL DEFAULT 0,
    error_message TEXT NOT NULL DEFAULT '',
    started_by_user_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ NULL,
    CONSTRAINT distribution_reconciliation_runs_trigger_check CHECK (
        trigger_type IN ('manual', 'scheduled')
    ),
    CONSTRAINT distribution_reconciliation_runs_status_check CHECK (
        status IN ('running', 'completed', 'failed')
    )
);

CREATE INDEX IF NOT EXISTS idx_distribution_reconciliation_runs_started_at
    ON distribution_reconciliation_runs(started_at DESC);

CREATE INDEX IF NOT EXISTS idx_distribution_reconciliation_runs_status
    ON distribution_reconciliation_runs(status, started_at DESC);

CREATE TABLE IF NOT EXISTS distribution_reconciliation_issues (
    id BIGSERIAL PRIMARY KEY,
    fingerprint VARCHAR(200) NOT NULL,
    issue_type VARCHAR(80) NOT NULL,
    severity VARCHAR(20) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'open',
    channel_org_id BIGINT NULL REFERENCES channel_organizations(id) ON DELETE SET NULL,
    billing_usage_entry_id BIGINT NULL REFERENCES billing_usage_entries(id) ON DELETE SET NULL,
    commission_id BIGINT NULL REFERENCES commission_ledger(id) ON DELETE SET NULL,
    expected_amount DECIMAL(20,8) NOT NULL DEFAULT 0,
    actual_amount DECIMAL(20,8) NOT NULL DEFAULT 0,
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    repairable BOOLEAN NOT NULL DEFAULT FALSE,
    occurrence_count INTEGER NOT NULL DEFAULT 1,
    first_detected_run_id BIGINT NULL REFERENCES distribution_reconciliation_runs(id) ON DELETE SET NULL,
    last_detected_run_id BIGINT NULL REFERENCES distribution_reconciliation_runs(id) ON DELETE SET NULL,
    resolution_action VARCHAR(32) NOT NULL DEFAULT '',
    resolution_note TEXT NOT NULL DEFAULT '',
    resolved_by_user_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    first_detected_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_detected_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT distribution_reconciliation_issues_type_check CHECK (
        issue_type IN (
            'usage_wallet_transaction_missing',
            'usage_wallet_amount_mismatch',
            'usage_commission_reserve_mismatch',
            'usage_commission_ledger_mismatch',
            'wallet_state_mismatch',
            'wallet_commission_reserved_mismatch',
            'wallet_total_consumed_mismatch',
            'commission_wallet_action_missing',
            'commission_reversal_wallet_action_missing',
            'commission_balance_audit_missing',
            'commission_reversal_balance_audit_missing'
        )
    ),
    CONSTRAINT distribution_reconciliation_issues_severity_check CHECK (
        severity IN ('critical', 'warning', 'info')
    ),
    CONSTRAINT distribution_reconciliation_issues_status_check CHECK (
        status IN ('open', 'resolved', 'ignored')
    ),
    CONSTRAINT distribution_reconciliation_issues_resolution_check CHECK (
        resolution_action IN ('', 'repair', 'dismiss', 'reopen', 'auto_resolved')
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_distribution_reconciliation_issues_fingerprint
    ON distribution_reconciliation_issues(fingerprint);

CREATE INDEX IF NOT EXISTS idx_distribution_reconciliation_issues_status_severity
    ON distribution_reconciliation_issues(status, severity, last_detected_at DESC);

CREATE INDEX IF NOT EXISTS idx_distribution_reconciliation_issues_channel_org
    ON distribution_reconciliation_issues(channel_org_id, status, last_detected_at DESC);

CREATE INDEX IF NOT EXISTS idx_distribution_reconciliation_issues_last_run
    ON distribution_reconciliation_issues(last_detected_run_id);

CREATE TABLE IF NOT EXISTS distribution_reconciliation_issue_actions (
    id BIGSERIAL PRIMARY KEY,
    issue_id BIGINT NOT NULL REFERENCES distribution_reconciliation_issues(id) ON DELETE RESTRICT,
    run_id BIGINT NULL REFERENCES distribution_reconciliation_runs(id) ON DELETE SET NULL,
    action VARCHAR(32) NOT NULL,
    previous_status VARCHAR(20) NOT NULL,
    new_status VARCHAR(20) NOT NULL,
    note TEXT NOT NULL DEFAULT '',
    operator_user_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT distribution_reconciliation_issue_actions_action_check CHECK (
        action IN ('repair', 'dismiss', 'reopen', 'recheck')
    ),
    CONSTRAINT distribution_reconciliation_issue_actions_previous_status_check CHECK (
        previous_status IN ('open', 'resolved', 'ignored')
    ),
    CONSTRAINT distribution_reconciliation_issue_actions_new_status_check CHECK (
        new_status IN ('open', 'resolved', 'ignored')
    )
);

CREATE INDEX IF NOT EXISTS idx_distribution_reconciliation_issue_actions_issue
    ON distribution_reconciliation_issue_actions(issue_id, created_at DESC);
