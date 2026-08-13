package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const distributionReconciliationAdvisoryLock = "distribution:financial-reconciliation"

var distributionReconciliationCandidateQueries = []string{
	`WITH wallet_actual AS (
    SELECT billing_usage_entry_id,
           COUNT(*) FILTER (WHERE transaction_type = 'consume') AS transaction_count,
           COALESCE(SUM(amount) FILTER (WHERE transaction_type = 'consume'), 0) AS actual_amount
    FROM channel_wallet_transactions
    WHERE billing_usage_entry_id IS NOT NULL
    GROUP BY billing_usage_entry_id
), candidates AS (
    SELECT 'usage-wallet:' || b.id::text || ':consume' AS fingerprint,
           CASE WHEN COALESCE(a.transaction_count, 0) = 0
                THEN 'usage_wallet_transaction_missing'
                ELSE 'usage_wallet_amount_mismatch'
           END AS issue_type,
           'critical' AS severity,
           b.channel_org_id,
           b.id AS billing_usage_entry_id,
           NULL::BIGINT AS commission_id,
           b.channel_usage_delta AS expected_amount,
           COALESCE(a.actual_amount, 0) AS actual_amount,
           jsonb_build_object(
               'request_id', b.request_id,
               'usage_log_id', b.usage_log_id,
               'transaction_count', COALESCE(a.transaction_count, 0)
           ) AS details,
           FALSE AS repairable
    FROM billing_usage_entries b
    JOIN channel_organizations o ON o.id = b.channel_org_id AND o.type IN ('reseller', 'oem')
    LEFT JOIN wallet_actual a ON a.billing_usage_entry_id = b.id
    WHERE b.applied = TRUE
      AND (
          (b.channel_usage_delta <> 0 AND COALESCE(a.transaction_count, 0) <> 1)
          OR (b.channel_usage_delta = 0 AND COALESCE(a.transaction_count, 0) <> 0)
          OR b.channel_usage_delta <> COALESCE(a.actual_amount, 0)
      )
)`,
	`WITH reserve_actual AS (
    SELECT billing_usage_entry_id,
           COUNT(*) FILTER (WHERE transaction_type = 'commission_reserve') AS transaction_count,
           COALESCE(SUM(amount) FILTER (WHERE transaction_type = 'commission_reserve'), 0) AS actual_amount
    FROM channel_wallet_transactions
    WHERE billing_usage_entry_id IS NOT NULL
    GROUP BY billing_usage_entry_id
), candidates AS (
    SELECT 'usage-wallet:' || b.id::text || ':commission-reserve' AS fingerprint,
           'usage_commission_reserve_mismatch' AS issue_type,
           'critical' AS severity,
           b.channel_org_id,
           b.id AS billing_usage_entry_id,
           NULL::BIGINT AS commission_id,
           b.commission_reserved_delta AS expected_amount,
           COALESCE(a.actual_amount, 0) AS actual_amount,
           jsonb_build_object(
               'request_id', b.request_id,
               'usage_log_id', b.usage_log_id,
               'transaction_count', COALESCE(a.transaction_count, 0)
           ) AS details,
           FALSE AS repairable
    FROM billing_usage_entries b
    JOIN channel_organizations o ON o.id = b.channel_org_id AND o.type IN ('reseller', 'oem')
    LEFT JOIN reserve_actual a ON a.billing_usage_entry_id = b.id
    WHERE b.applied = TRUE
      AND (
          (b.commission_reserved_delta <> 0 AND COALESCE(a.transaction_count, 0) <> 1)
          OR (b.commission_reserved_delta = 0 AND COALESCE(a.transaction_count, 0) <> 0)
          OR b.commission_reserved_delta <> COALESCE(a.actual_amount, 0)
      )
)`,
	`WITH ledger_actual AS (
    SELECT billing_usage_entry_id,
           COUNT(*) AS commission_count,
           COALESCE(SUM(amount), 0) AS actual_amount
    FROM commission_ledger
    WHERE billing_usage_entry_id IS NOT NULL
      AND reversed_from_id IS NULL
    GROUP BY billing_usage_entry_id
), candidates AS (
    SELECT 'usage-ledger:' || b.id::text || ':commission' AS fingerprint,
           'usage_commission_ledger_mismatch' AS issue_type,
           'critical' AS severity,
           b.channel_org_id,
           b.id AS billing_usage_entry_id,
           NULL::BIGINT AS commission_id,
           b.commission_reserved_delta AS expected_amount,
           COALESCE(a.actual_amount, 0) AS actual_amount,
           jsonb_build_object(
               'request_id', b.request_id,
               'usage_log_id', b.usage_log_id,
               'commission_count', COALESCE(a.commission_count, 0)
           ) AS details,
           FALSE AS repairable
    FROM billing_usage_entries b
    JOIN channel_organizations o ON o.id = b.channel_org_id AND o.type IN ('reseller', 'oem')
    LEFT JOIN ledger_actual a ON a.billing_usage_entry_id = b.id
    WHERE b.applied = TRUE
      AND b.commission_reserved_delta <> 0
      AND b.commission_reserved_delta <> COALESCE(a.actual_amount, 0)
)`,
	`WITH latest_transaction AS (
    SELECT DISTINCT ON (channel_org_id)
           channel_org_id,
           id AS transaction_id,
           prepaid_balance_after,
           commission_reserved_after
    FROM channel_wallet_transactions
    ORDER BY channel_org_id, id DESC
), candidates AS (
    SELECT 'wallet:' || w.channel_org_id::text || ':state' AS fingerprint,
           'wallet_state_mismatch' AS issue_type,
           'critical' AS severity,
           w.channel_org_id,
           NULL::BIGINT AS billing_usage_entry_id,
           NULL::BIGINT AS commission_id,
           CASE WHEN w.prepaid_balance <> latest.prepaid_balance_after
                THEN latest.prepaid_balance_after
                ELSE latest.commission_reserved_after
           END AS expected_amount,
           CASE WHEN w.prepaid_balance <> latest.prepaid_balance_after
                THEN w.prepaid_balance
                ELSE w.commission_reserved
           END AS actual_amount,
           jsonb_build_object(
               'latest_transaction_id', latest.transaction_id,
               'expected_prepaid_balance', latest.prepaid_balance_after,
               'actual_prepaid_balance', w.prepaid_balance,
               'expected_commission_reserved', latest.commission_reserved_after,
               'actual_commission_reserved', w.commission_reserved
           ) AS details,
           FALSE AS repairable
    FROM channel_wallets w
    JOIN latest_transaction latest ON latest.channel_org_id = w.channel_org_id
    WHERE w.prepaid_balance <> latest.prepaid_balance_after
       OR w.commission_reserved <> latest.commission_reserved_after
)`,
	`WITH outstanding_commissions AS (
    SELECT channel_org_id,
           COUNT(*) AS commission_count,
           COALESCE(SUM(amount), 0) AS expected_amount
    FROM commission_ledger
    WHERE reversed_from_id IS NULL
      AND status IN ('pending', 'frozen', 'available')
      AND settlement_method IN ('balance', 'auto', 'manual')
    GROUP BY channel_org_id
), candidates AS (
    SELECT 'wallet:' || w.channel_org_id::text || ':commission-reserved' AS fingerprint,
           'wallet_commission_reserved_mismatch' AS issue_type,
           'critical' AS severity,
           w.channel_org_id,
           NULL::BIGINT AS billing_usage_entry_id,
           NULL::BIGINT AS commission_id,
           COALESCE(c.expected_amount, 0) AS expected_amount,
           w.commission_reserved AS actual_amount,
           jsonb_build_object('commission_count', COALESCE(c.commission_count, 0)) AS details,
           FALSE AS repairable
    FROM channel_wallets w
    JOIN channel_organizations o ON o.id = w.channel_org_id AND o.type IN ('reseller', 'oem')
    LEFT JOIN outstanding_commissions c ON c.channel_org_id = w.channel_org_id
    WHERE w.commission_reserved <> COALESCE(c.expected_amount, 0)
)`,
	`WITH consumption_ledger AS (
    SELECT channel_org_id,
           COUNT(*) AS transaction_count,
           COALESCE(SUM(amount), 0) AS total_consumed
    FROM channel_wallet_transactions
    WHERE transaction_type = 'consume'
    GROUP BY channel_org_id
), candidates AS (
    SELECT 'wallet:' || w.channel_org_id::text || ':total-consumed' AS fingerprint,
           'wallet_total_consumed_mismatch' AS issue_type,
           'warning' AS severity,
           w.channel_org_id,
           NULL::BIGINT AS billing_usage_entry_id,
           NULL::BIGINT AS commission_id,
           COALESCE(ledger.total_consumed, 0) AS expected_amount,
           w.total_consumed AS actual_amount,
           jsonb_build_object('transaction_count', COALESCE(ledger.transaction_count, 0)) AS details,
           COALESCE(ledger.transaction_count, 0) > 0 AS repairable
    FROM channel_wallets w
    LEFT JOIN consumption_ledger ledger ON ledger.channel_org_id = w.channel_org_id
    WHERE w.total_consumed <> COALESCE(ledger.total_consumed, 0)
)`,
	`WITH action_actual AS (
    SELECT commission_id,
           COUNT(*) FILTER (WHERE transaction_type IN ('commission_settle', 'commission_deduct')) AS transaction_count,
           COALESCE(SUM(amount) FILTER (WHERE transaction_type IN ('commission_settle', 'commission_deduct')), 0) AS actual_amount
    FROM channel_wallet_transactions
    WHERE commission_id IS NOT NULL
    GROUP BY commission_id
), candidates AS (
    SELECT 'commission:' || c.id::text || ':settlement-wallet' AS fingerprint,
           'commission_wallet_action_missing' AS issue_type,
           'critical' AS severity,
           c.channel_org_id,
           c.billing_usage_entry_id,
           c.id AS commission_id,
           c.amount AS expected_amount,
           COALESCE(a.actual_amount, 0) AS actual_amount,
           jsonb_build_object(
               'settlement_method', c.settlement_method,
               'transaction_count', COALESCE(a.transaction_count, 0)
           ) AS details,
           FALSE AS repairable
    FROM commission_ledger c
    JOIN channel_organizations o ON o.id = c.channel_org_id AND o.type IN ('reseller', 'oem')
    LEFT JOIN action_actual a ON a.commission_id = c.id
    WHERE c.reversed_from_id IS NULL
      AND c.status = 'settled'
      AND c.settlement_method IN ('balance', 'auto', 'manual')
      AND (
          COALESCE(a.transaction_count, 0) <> 1
          OR c.amount <> COALESCE(a.actual_amount, 0)
      )
)`,
	`WITH reversal_actual AS (
    SELECT commission_id,
           COUNT(*) FILTER (WHERE transaction_type = 'commission_refund') AS transaction_count,
           COALESCE(SUM(amount) FILTER (WHERE transaction_type = 'commission_refund'), 0) AS actual_amount
    FROM channel_wallet_transactions
    WHERE commission_id IS NOT NULL
    GROUP BY commission_id
), candidates AS (
    SELECT 'commission:' || source.id::text || ':reversal-wallet' AS fingerprint,
           'commission_reversal_wallet_action_missing' AS issue_type,
           'critical' AS severity,
           source.channel_org_id,
           source.billing_usage_entry_id,
           source.id AS commission_id,
           source.amount AS expected_amount,
           COALESCE(a.actual_amount, 0) AS actual_amount,
           jsonb_build_object(
               'settlement_method', source.settlement_method,
               'reversal_id', reversal.id,
               'transaction_count', COALESCE(a.transaction_count, 0)
           ) AS details,
           FALSE AS repairable
    FROM commission_ledger source
    JOIN commission_ledger reversal ON reversal.reversed_from_id = source.id AND reversal.status = 'reversed'
    JOIN channel_organizations o ON o.id = source.channel_org_id AND o.type IN ('reseller', 'oem')
    LEFT JOIN reversal_actual a ON a.commission_id = source.id
    WHERE source.status = 'settled'
      AND source.settlement_method IN ('balance', 'auto', 'manual')
      AND (
          COALESCE(a.transaction_count, 0) <> 1
          OR source.amount <> COALESCE(a.actual_amount, 0)
      )
)`,
	`WITH balance_audit AS (
    SELECT notes,
           COUNT(*) AS audit_count,
           COALESCE(SUM(value), 0) AS actual_amount
    FROM redeem_codes
    WHERE type = 'distribution_balance'
      AND notes LIKE 'distribution commission settlement #%'
    GROUP BY notes
), candidates AS (
    SELECT 'commission:' || c.id::text || ':settlement-balance-audit' AS fingerprint,
           'commission_balance_audit_missing' AS issue_type,
           'critical' AS severity,
           c.channel_org_id,
           c.billing_usage_entry_id,
           c.id AS commission_id,
           c.amount AS expected_amount,
           COALESCE(a.actual_amount, 0) AS actual_amount,
           jsonb_build_object(
               'settlement_method', c.settlement_method,
               'user_id', c.user_id,
               'audit_count', COALESCE(a.audit_count, 0)
           ) AS details,
           FALSE AS repairable
    FROM commission_ledger c
    LEFT JOIN balance_audit a ON a.notes = 'distribution commission settlement #' || c.id::text
    WHERE c.reversed_from_id IS NULL
      AND c.status = 'settled'
      AND c.settlement_method IN ('balance', 'auto')
      AND (
          COALESCE(a.audit_count, 0) <> 1
          OR c.amount <> COALESCE(a.actual_amount, 0)
      )
)`,
	`WITH reversal_audit AS (
    SELECT notes,
           COUNT(*) AS audit_count,
           COALESCE(SUM(value), 0) AS actual_amount
    FROM redeem_codes
    WHERE type = 'distribution_balance'
      AND notes LIKE 'distribution commission reversal #%'
    GROUP BY notes
), candidates AS (
    SELECT 'commission:' || source.id::text || ':reversal-balance-audit' AS fingerprint,
           'commission_reversal_balance_audit_missing' AS issue_type,
           'critical' AS severity,
           source.channel_org_id,
           source.billing_usage_entry_id,
           source.id AS commission_id,
           -source.amount AS expected_amount,
           COALESCE(a.actual_amount, 0) AS actual_amount,
           jsonb_build_object(
               'settlement_method', source.settlement_method,
               'user_id', source.user_id,
               'reversal_id', reversal.id,
               'audit_count', COALESCE(a.audit_count, 0)
           ) AS details,
           FALSE AS repairable
    FROM commission_ledger source
    JOIN commission_ledger reversal ON reversal.reversed_from_id = source.id AND reversal.status = 'reversed'
    LEFT JOIN reversal_audit a ON a.notes = 'distribution commission reversal #' || source.id::text
    WHERE source.settlement_method IN ('balance', 'auto')
      AND (
          COALESCE(a.audit_count, 0) <> 1
          OR -source.amount <> COALESCE(a.actual_amount, 0)
      )
)`,
}

const distributionReconciliationCandidateUpsertSQL = `
INSERT INTO distribution_reconciliation_issues (
    fingerprint,
    issue_type,
    severity,
    status,
    channel_org_id,
    billing_usage_entry_id,
    commission_id,
    expected_amount,
    actual_amount,
    details,
    repairable,
    occurrence_count,
    first_detected_run_id,
    last_detected_run_id,
    first_detected_at,
    last_detected_at,
    created_at,
    updated_at
)
SELECT fingerprint,
       issue_type,
       severity,
       'open',
       channel_org_id,
       billing_usage_entry_id,
       commission_id,
       expected_amount,
       actual_amount,
       details,
       repairable,
       1,
       $1,
       $1,
       NOW(),
       NOW(),
       NOW(),
       NOW()
FROM candidates
ON CONFLICT (fingerprint) DO UPDATE
SET issue_type = EXCLUDED.issue_type,
    severity = EXCLUDED.severity,
    status = CASE
        WHEN distribution_reconciliation_issues.status = 'resolved' THEN 'open'
        ELSE distribution_reconciliation_issues.status
    END,
    channel_org_id = EXCLUDED.channel_org_id,
    billing_usage_entry_id = EXCLUDED.billing_usage_entry_id,
    commission_id = EXCLUDED.commission_id,
    expected_amount = EXCLUDED.expected_amount,
    actual_amount = EXCLUDED.actual_amount,
    details = EXCLUDED.details,
    repairable = EXCLUDED.repairable,
    occurrence_count = distribution_reconciliation_issues.occurrence_count + 1,
    last_detected_run_id = EXCLUDED.last_detected_run_id,
    last_detected_at = NOW(),
    resolution_action = CASE
        WHEN distribution_reconciliation_issues.status = 'resolved' THEN ''
        ELSE distribution_reconciliation_issues.resolution_action
    END,
    resolution_note = CASE
        WHEN distribution_reconciliation_issues.status = 'resolved' THEN ''
        ELSE distribution_reconciliation_issues.resolution_note
    END,
    resolved_by_user_id = CASE
        WHEN distribution_reconciliation_issues.status = 'resolved' THEN NULL
        ELSE distribution_reconciliation_issues.resolved_by_user_id
    END,
    resolved_at = CASE
        WHEN distribution_reconciliation_issues.status = 'resolved' THEN NULL
        ELSE distribution_reconciliation_issues.resolved_at
    END,
    updated_at = NOW()`

func (r *distributionWalletRepository) RunReconciliation(ctx context.Context, input service.DistributionReconciliationRunInput) (_ *service.DistributionReconciliationRun, err error) {
	if r == nil || r.db == nil {
		return nil, service.ErrInvalidDistributionReconciliation
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, fmt.Errorf("begin distribution reconciliation: %w", err)
	}
	started := false
	defer func() {
		if err == nil {
			return
		}
		_ = tx.Rollback()
		if started {
			recordDistributionReconciliationFailure(r.db, input, err)
		}
	}()

	var locked bool
	if err = tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtext($1))`, distributionReconciliationAdvisoryLock).Scan(&locked); err != nil {
		return nil, fmt.Errorf("lock distribution reconciliation: %w", err)
	}
	if !locked {
		return nil, service.ErrDistributionReconciliationBusy
	}
	if input.TriggerType == "scheduled" {
		recent, recentErr := queryDistributionReconciliationRun(ctx, tx, `
SELECT id,
       trigger_type,
       status,
       issue_count,
       resolved_count,
       error_message,
       started_by_user_id,
       started_at,
       completed_at
FROM distribution_reconciliation_runs
WHERE trigger_type = 'scheduled'
  AND status = 'completed'
  AND started_at >= NOW() - INTERVAL '9 minutes'
ORDER BY started_at DESC, id DESC
LIMIT 1`)
		if recentErr != nil {
			return nil, recentErr
		}
		if recent != nil {
			if err = tx.Commit(); err != nil {
				return nil, fmt.Errorf("commit skipped distribution reconciliation: %w", err)
			}
			return recent, nil
		}
	}

	var run service.DistributionReconciliationRun
	var startedByUserID sql.NullInt64
	var completedAt sql.NullTime
	err = tx.QueryRowContext(ctx, `
INSERT INTO distribution_reconciliation_runs (
    trigger_type,
    status,
    started_by_user_id,
    started_at
)
VALUES ($1, 'running', $2, NOW())
RETURNING id,
          trigger_type,
          status,
          issue_count,
          resolved_count,
          error_message,
          started_by_user_id,
          started_at,
          completed_at`,
		input.TriggerType,
		nullableInt64Arg(input.StartedByUserID),
	).Scan(
		&run.ID,
		&run.TriggerType,
		&run.Status,
		&run.IssueCount,
		&run.ResolvedCount,
		&run.ErrorMessage,
		&startedByUserID,
		&run.StartedAt,
		&completedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create distribution reconciliation run: %w", err)
	}
	started = true
	if startedByUserID.Valid {
		run.StartedByUserID = &startedByUserID.Int64
	}
	if completedAt.Valid {
		run.CompletedAt = &completedAt.Time
	}

	issueCount := int64(0)
	for _, candidateQuery := range distributionReconciliationCandidateQueries {
		result, queryErr := tx.ExecContext(ctx, candidateQuery+distributionReconciliationCandidateUpsertSQL, run.ID)
		if queryErr != nil {
			return nil, fmt.Errorf("detect distribution reconciliation differences: %w", queryErr)
		}
		affected, queryErr := result.RowsAffected()
		if queryErr != nil {
			return nil, fmt.Errorf("count distribution reconciliation differences: %w", queryErr)
		}
		issueCount += affected
	}

	resolvedResult, err := tx.ExecContext(ctx, `
UPDATE distribution_reconciliation_issues
SET status = 'resolved',
    resolution_action = 'auto_resolved',
    resolution_note = 'Difference was not detected by the latest reconciliation run',
    resolved_by_user_id = NULL,
    resolved_at = NOW(),
    updated_at = NOW()
WHERE status = 'open'
  AND last_detected_run_id IS DISTINCT FROM $1`, run.ID)
	if err != nil {
		return nil, fmt.Errorf("resolve stale distribution reconciliation issues: %w", err)
	}
	resolvedCount, err := resolvedResult.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("count resolved distribution reconciliation issues: %w", err)
	}

	completedAt = sql.NullTime{}
	err = tx.QueryRowContext(ctx, `
UPDATE distribution_reconciliation_runs
SET status = 'completed',
    issue_count = $2,
    resolved_count = $3,
    completed_at = NOW()
WHERE id = $1
RETURNING status,
          issue_count,
          resolved_count,
          completed_at`, run.ID, issueCount, resolvedCount).Scan(
		&run.Status,
		&run.IssueCount,
		&run.ResolvedCount,
		&completedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("complete distribution reconciliation run: %w", err)
	}
	if completedAt.Valid {
		run.CompletedAt = &completedAt.Time
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit distribution reconciliation: %w", err)
	}
	return &run, nil
}

func recordDistributionReconciliationFailure(db *sql.DB, input service.DistributionReconciliationRunInput, runErr error) {
	if db == nil || runErr == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	message := runErr.Error()
	if len(message) > 4000 {
		message = message[:4000]
	}
	_, _ = db.ExecContext(ctx, `
INSERT INTO distribution_reconciliation_runs (
    trigger_type,
    status,
    error_message,
    started_by_user_id,
    started_at,
    completed_at
)
VALUES ($1, 'failed', $2, $3, NOW(), NOW())`,
		input.TriggerType,
		message,
		nullableInt64Arg(input.StartedByUserID),
	)
}

func (r *distributionWalletRepository) ListReconciliationIssues(ctx context.Context, filter service.DistributionReconciliationFilter, params pagination.PaginationParams) ([]service.DistributionReconciliationIssue, *pagination.PaginationResult, error) {
	if r == nil || r.db == nil {
		return nil, nil, service.ErrInvalidDistributionReconciliation
	}
	whereSQL, args := buildDistributionReconciliationWhere(filter)
	var total int64
	if err := r.db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM distribution_reconciliation_issues issue
`+whereSQL, args...).Scan(&total); err != nil {
		return nil, nil, fmt.Errorf("count distribution reconciliation issues: %w", err)
	}

	args = append(args, params.Limit(), params.Offset())
	rows, err := r.db.QueryContext(ctx, distributionReconciliationIssueSelectSQL+`
`+whereSQL+`
ORDER BY CASE issue.status WHEN 'open' THEN 0 WHEN 'ignored' THEN 1 ELSE 2 END,
         CASE issue.severity WHEN 'critical' THEN 0 WHEN 'warning' THEN 1 ELSE 2 END,
         issue.last_detected_at DESC,
         issue.id DESC
LIMIT $`+fmt.Sprintf("%d", len(args)-1)+` OFFSET $`+fmt.Sprintf("%d", len(args)), args...)
	if err != nil {
		return nil, nil, fmt.Errorf("list distribution reconciliation issues: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items, err := scanDistributionReconciliationIssues(rows)
	if err != nil {
		return nil, nil, err
	}
	return items, paginationResultFromTotal(total, params), nil
}

func buildDistributionReconciliationWhere(filter service.DistributionReconciliationFilter) (string, []any) {
	whereSQL := "WHERE 1=1"
	args := make([]any, 0, 4)
	if filter.ChannelOrgID > 0 {
		args = append(args, filter.ChannelOrgID)
		whereSQL += fmt.Sprintf(" AND issue.channel_org_id = $%d", len(args))
	}
	if filter.IssueType != "" {
		args = append(args, filter.IssueType)
		whereSQL += fmt.Sprintf(" AND issue.issue_type = $%d", len(args))
	}
	if filter.Severity != "" {
		args = append(args, filter.Severity)
		whereSQL += fmt.Sprintf(" AND issue.severity = $%d", len(args))
	}
	if filter.Status != "" {
		args = append(args, filter.Status)
		whereSQL += fmt.Sprintf(" AND issue.status = $%d", len(args))
	}
	return whereSQL, args
}

func (r *distributionWalletRepository) GetReconciliationIssue(ctx context.Context, issueID int64) (*service.DistributionReconciliationIssue, error) {
	if r == nil || r.db == nil || issueID <= 0 {
		return nil, service.ErrInvalidDistributionReconciliation
	}
	rows, err := r.db.QueryContext(ctx, distributionReconciliationIssueSelectSQL+`
WHERE issue.id = $1`, issueID)
	if err != nil {
		return nil, fmt.Errorf("get distribution reconciliation issue: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items, err := scanDistributionReconciliationIssues(rows)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, service.ErrInvalidDistributionReconciliation
	}
	return &items[0], nil
}

func (r *distributionWalletRepository) ListReconciliationIssueActions(ctx context.Context, issueID int64) ([]service.DistributionReconciliationIssueAction, error) {
	if r == nil || r.db == nil || issueID <= 0 {
		return nil, service.ErrInvalidDistributionReconciliation
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT id,
       issue_id,
       run_id,
       action,
       previous_status,
       new_status,
       note,
       operator_user_id,
       created_at
FROM distribution_reconciliation_issue_actions
WHERE issue_id = $1
ORDER BY created_at DESC, id DESC
LIMIT 100`, issueID)
	if err != nil {
		return nil, fmt.Errorf("list distribution reconciliation issue actions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]service.DistributionReconciliationIssueAction, 0)
	for rows.Next() {
		var (
			item           service.DistributionReconciliationIssueAction
			runID          sql.NullInt64
			operatorUserID sql.NullInt64
		)
		if err := rows.Scan(
			&item.ID,
			&item.IssueID,
			&runID,
			&item.Action,
			&item.PreviousStatus,
			&item.NewStatus,
			&item.Note,
			&operatorUserID,
			&item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan distribution reconciliation issue action: %w", err)
		}
		if operatorUserID.Valid {
			item.OperatorUserID = &operatorUserID.Int64
		}
		if runID.Valid {
			item.RunID = &runID.Int64
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate distribution reconciliation issue actions: %w", err)
	}
	return items, nil
}

func (r *distributionWalletRepository) RecordReconciliationIssueAction(ctx context.Context, issueID int64, input service.DistributionReconciliationIssueActionInput) error {
	if r == nil || r.db == nil || issueID <= 0 {
		return service.ErrInvalidDistributionReconciliation
	}
	return insertDistributionReconciliationIssueAction(ctx, r.db, issueID, input)
}

func (r *distributionWalletRepository) GetReconciliationSummary(ctx context.Context) (*service.DistributionReconciliationSummary, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrInvalidDistributionReconciliation
	}
	summary := &service.DistributionReconciliationSummary{}
	if err := r.db.QueryRowContext(ctx, `
SELECT COUNT(*) FILTER (WHERE status = 'open'),
       COUNT(*) FILTER (WHERE status = 'open' AND severity = 'critical'),
       COUNT(*) FILTER (WHERE status = 'open' AND severity = 'warning'),
       COUNT(*) FILTER (WHERE status = 'ignored'),
       COUNT(*) FILTER (WHERE status = 'resolved'),
       COUNT(*) FILTER (WHERE status = 'open' AND repairable = TRUE)
FROM distribution_reconciliation_issues`).Scan(
		&summary.OpenCount,
		&summary.CriticalCount,
		&summary.WarningCount,
		&summary.IgnoredCount,
		&summary.ResolvedCount,
		&summary.RepairableCount,
	); err != nil {
		return nil, fmt.Errorf("summarize distribution reconciliation issues: %w", err)
	}

	latest, err := queryDistributionReconciliationRun(ctx, r.db, `
SELECT id,
       trigger_type,
       status,
       issue_count,
       resolved_count,
       error_message,
       started_by_user_id,
       started_at,
       completed_at
FROM distribution_reconciliation_runs
ORDER BY started_at DESC, id DESC
LIMIT 1`)
	if err != nil {
		return nil, err
	}
	summary.LatestRun = latest
	return summary, nil
}

func (r *distributionWalletRepository) ResolveReconciliationIssue(ctx context.Context, issueID int64, input service.DistributionReconciliationResolutionInput) (_ *service.DistributionReconciliationIssue, err error) {
	if r == nil || r.db == nil || issueID <= 0 {
		return nil, service.ErrInvalidDistributionReconciliation
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin distribution reconciliation resolution: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	issue, err := getDistributionReconciliationIssueForUpdate(ctx, tx, issueID)
	if err != nil {
		return nil, err
	}
	switch input.Action {
	case "repair":
		if issue.Status != "open" || !issue.Repairable || issue.IssueType != "wallet_total_consumed_mismatch" || issue.ChannelOrgID == nil {
			return nil, service.ErrDistributionReconciliationIssueNotRepairable
		}
		if err := repairDistributionWalletTotalConsumed(ctx, tx, *issue.ChannelOrgID); err != nil {
			return nil, err
		}
		if input.Note == "" {
			input.Note = "Rebuilt total consumed from immutable wallet transactions"
		}
	case "dismiss":
		if issue.Status != "open" {
			return nil, service.ErrInvalidDistributionReconciliation
		}
	case "reopen":
		if issue.Status == "open" {
			return nil, service.ErrInvalidDistributionReconciliation
		}
	default:
		return nil, service.ErrInvalidDistributionReconciliation
	}

	status := "resolved"
	resolutionAction := input.Action
	resolvedAtSQL := "NOW()"
	resolvedBy := nullableInt64Arg(input.OperatorUserID)
	if input.Action == "dismiss" {
		status = "ignored"
	}
	if input.Action == "reopen" {
		status = "open"
		resolutionAction = ""
		input.Note = ""
		resolvedAtSQL = "NULL"
		resolvedBy = nil
	}

	query := `
UPDATE distribution_reconciliation_issues
SET status = $2,
    resolution_action = $3,
    resolution_note = $4,
    resolved_by_user_id = $5,
    resolved_at = ` + resolvedAtSQL + `,
    updated_at = NOW()
WHERE id = $1`
	if _, err := tx.ExecContext(ctx, query, issueID, status, resolutionAction, input.Note, resolvedBy); err != nil {
		return nil, fmt.Errorf("resolve distribution reconciliation issue: %w", err)
	}
	if err := insertDistributionReconciliationIssueAction(ctx, tx, issueID, service.DistributionReconciliationIssueActionInput{
		RunID:          issue.LastDetectedRunID,
		Action:         input.Action,
		PreviousStatus: issue.Status,
		NewStatus:      status,
		Note:           input.Note,
		OperatorUserID: input.OperatorUserID,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit distribution reconciliation resolution: %w", err)
	}
	return r.GetReconciliationIssue(ctx, issueID)
}

func insertDistributionReconciliationIssueAction(ctx context.Context, execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, issueID int64, input service.DistributionReconciliationIssueActionInput) error {
	if _, err := execer.ExecContext(ctx, `
INSERT INTO distribution_reconciliation_issue_actions (
    issue_id,
    run_id,
    action,
    previous_status,
    new_status,
    note,
    operator_user_id,
    created_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())`,
		issueID,
		nullableInt64Arg(input.RunID),
		input.Action,
		input.PreviousStatus,
		input.NewStatus,
		input.Note,
		nullableInt64Arg(input.OperatorUserID),
	); err != nil {
		return fmt.Errorf("record distribution reconciliation issue action: %w", err)
	}
	return nil
}

func repairDistributionWalletTotalConsumed(ctx context.Context, tx *sql.Tx, channelOrgID int64) error {
	current, err := getDistributionWalletByChannelOrgID(ctx, tx, channelOrgID, true)
	if err != nil {
		return err
	}
	if current == nil {
		return service.ErrInvalidDistributionWallet
	}
	var expected float64
	var transactionCount int64
	if err := tx.QueryRowContext(ctx, `
SELECT COALESCE(SUM(amount), 0)::double precision,
       COUNT(*)
FROM channel_wallet_transactions
WHERE channel_org_id = $1
  AND transaction_type = 'consume'`, channelOrgID).Scan(&expected, &transactionCount); err != nil {
		return fmt.Errorf("rebuild distribution wallet total consumed: %w", err)
	}
	if transactionCount == 0 {
		return service.ErrDistributionReconciliationIssueNotRepairable
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE channel_wallets
SET total_consumed = $2,
    updated_at = NOW()
WHERE channel_org_id = $1`, channelOrgID, expected); err != nil {
		return fmt.Errorf("repair distribution wallet total consumed: %w", err)
	}
	current.TotalConsumed = expected
	org, err := getDistributionOrganizationByID(ctx, tx, channelOrgID)
	if err != nil {
		return err
	}
	return syncDistributionAlertEvents(ctx, tx, org, current)
}

func getDistributionReconciliationIssueForUpdate(ctx context.Context, tx *sql.Tx, issueID int64) (*service.DistributionReconciliationIssue, error) {
	rows, err := tx.QueryContext(ctx, distributionReconciliationIssueSelectSQL+`
WHERE issue.id = $1
FOR UPDATE OF issue`, issueID)
	if err != nil {
		return nil, fmt.Errorf("lock distribution reconciliation issue: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items, err := scanDistributionReconciliationIssues(rows)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, service.ErrInvalidDistributionReconciliation
	}
	return &items[0], nil
}

const distributionReconciliationIssueSelectSQL = `
SELECT issue.id,
       issue.fingerprint,
       issue.issue_type,
       issue.severity,
       issue.status,
       issue.channel_org_id,
       COALESCE(org.name, ''),
       issue.billing_usage_entry_id,
       issue.commission_id,
       issue.expected_amount,
       issue.actual_amount,
       issue.details,
       issue.occurrence_count,
       issue.repairable,
       issue.last_detected_run_id,
       issue.resolution_action,
       issue.resolution_note,
       issue.resolved_by_user_id,
       issue.first_detected_at,
       issue.last_detected_at,
       issue.resolved_at,
       issue.created_at,
       issue.updated_at
FROM distribution_reconciliation_issues issue
LEFT JOIN channel_organizations org ON org.id = issue.channel_org_id`

func scanDistributionReconciliationIssues(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]service.DistributionReconciliationIssue, error) {
	items := make([]service.DistributionReconciliationIssue, 0)
	for rows.Next() {
		var (
			item                service.DistributionReconciliationIssue
			channelOrgID        sql.NullInt64
			billingUsageEntryID sql.NullInt64
			commissionID        sql.NullInt64
			lastDetectedRunID   sql.NullInt64
			resolvedByUserID    sql.NullInt64
			resolvedAt          sql.NullTime
			detailsJSON         []byte
		)
		if err := rows.Scan(
			&item.ID,
			&item.Fingerprint,
			&item.IssueType,
			&item.Severity,
			&item.Status,
			&channelOrgID,
			&item.OrganizationName,
			&billingUsageEntryID,
			&commissionID,
			&item.ExpectedAmount,
			&item.ActualAmount,
			&detailsJSON,
			&item.OccurrenceCount,
			&item.Repairable,
			&lastDetectedRunID,
			&item.ResolutionAction,
			&item.ResolutionNote,
			&resolvedByUserID,
			&item.FirstDetectedAt,
			&item.LastDetectedAt,
			&resolvedAt,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan distribution reconciliation issue: %w", err)
		}
		item.Details = map[string]any{}
		if len(detailsJSON) > 0 {
			if err := json.Unmarshal(detailsJSON, &item.Details); err != nil {
				return nil, fmt.Errorf("decode distribution reconciliation issue details: %w", err)
			}
		}
		if channelOrgID.Valid {
			item.ChannelOrgID = &channelOrgID.Int64
		}
		if billingUsageEntryID.Valid {
			item.BillingUsageEntryID = &billingUsageEntryID.Int64
		}
		if commissionID.Valid {
			item.CommissionID = &commissionID.Int64
		}
		if lastDetectedRunID.Valid {
			item.LastDetectedRunID = &lastDetectedRunID.Int64
		}
		if resolvedByUserID.Valid {
			item.ResolvedByUserID = &resolvedByUserID.Int64
		}
		if resolvedAt.Valid {
			item.ResolvedAt = &resolvedAt.Time
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func queryDistributionReconciliationRun(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, query string, args ...any) (*service.DistributionReconciliationRun, error) {
	var (
		run             service.DistributionReconciliationRun
		startedByUserID sql.NullInt64
		completedAt     sql.NullTime
	)
	if err := queryer.QueryRowContext(ctx, query, args...).Scan(
		&run.ID,
		&run.TriggerType,
		&run.Status,
		&run.IssueCount,
		&run.ResolvedCount,
		&run.ErrorMessage,
		&startedByUserID,
		&run.StartedAt,
		&completedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get distribution reconciliation run: %w", err)
	}
	if startedByUserID.Valid {
		run.StartedByUserID = &startedByUserID.Int64
	}
	if completedAt.Valid {
		run.CompletedAt = &completedAt.Time
	}
	return &run, nil
}

var _ service.DistributionReconciliationRepository = (*distributionWalletRepository)(nil)
