//go:build integration

package repository

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDistributionReconciliation_DetectsAndRepairsWalletTotalConsumed(t *testing.T) {
	ctx := context.Background()
	repo := &distributionWalletRepository{db: integrationDB}

	var channelOrgID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
INSERT INTO channel_organizations (type, name, status, config)
VALUES ('reseller', $1, 'active', '{}'::jsonb)
RETURNING id`, "reconciliation-wallet-"+uuid.NewString()).Scan(&channelOrgID))

	_, err := integrationDB.ExecContext(ctx, `
INSERT INTO channel_wallets (
    channel_org_id,
    prepaid_balance,
    commission_reserved,
    total_recharged,
    total_consumed,
    status
)
VALUES ($1, 90, 0, 100, 5, 'active')`, channelOrgID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
INSERT INTO channel_wallet_transactions (
    channel_org_id,
    transaction_type,
    amount,
    prepaid_balance_before,
    prepaid_balance_after,
    commission_reserved_before,
    commission_reserved_after
)
VALUES ($1, 'consume', 10, 100, 90, 0, 0)`, channelOrgID)
	require.NoError(t, err)

	t.Cleanup(func() {
		cleanupDistributionReconciliationFixture(t, channelOrgID)
	})

	run, err := repo.RunReconciliation(ctx, service.DistributionReconciliationRunInput{TriggerType: "manual"})
	require.NoError(t, err)
	require.Equal(t, "completed", run.Status)

	issues, _, err := repo.ListReconciliationIssues(ctx, service.DistributionReconciliationFilter{
		ChannelOrgID: channelOrgID,
		Status:       "open",
	}, pagination.PaginationParams{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Len(t, issues, 1)
	require.Equal(t, "wallet_total_consumed_mismatch", issues[0].IssueType)
	require.True(t, issues[0].Repairable)
	require.InDelta(t, 10, issues[0].ExpectedAmount, 0.00000001)
	require.InDelta(t, 5, issues[0].ActualAmount, 0.00000001)

	resolved, err := repo.ResolveReconciliationIssue(ctx, issues[0].ID, service.DistributionReconciliationResolutionInput{
		Action: "repair",
		Note:   "integration repair",
	})
	require.NoError(t, err)
	require.Equal(t, "resolved", resolved.Status)

	var totalConsumed float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT total_consumed
FROM channel_wallets
WHERE channel_org_id = $1`, channelOrgID).Scan(&totalConsumed))
	require.InDelta(t, 10, totalConsumed, 0.00000001)

	actions, err := repo.ListReconciliationIssueActions(ctx, issues[0].ID)
	require.NoError(t, err)
	require.Len(t, actions, 1)
	require.Equal(t, "repair", actions[0].Action)
	require.Equal(t, "open", actions[0].PreviousStatus)
	require.Equal(t, "resolved", actions[0].NewStatus)
	require.Equal(t, run.ID, *actions[0].RunID)

	_, err = repo.RunReconciliation(ctx, service.DistributionReconciliationRunInput{TriggerType: "manual"})
	require.NoError(t, err)
	issue, err := repo.GetReconciliationIssue(ctx, issues[0].ID)
	require.NoError(t, err)
	require.Equal(t, "resolved", issue.Status)
}

func TestDistributionReconciliation_DetectsBalanceAuditLossAndKeepsDismissal(t *testing.T) {
	ctx := context.Background()
	commissionRepo := &distributionCommissionRepository{db: integrationDB}
	reconciliationRepo := &distributionWalletRepository{db: integrationDB}
	fixture := createDistributionCommissionSettlementFixture(t, "available", "balance", time.Time{})
	t.Cleanup(func() {
		cleanupDistributionReconciliationIssues(t, fixture.channelOrgID)
	})

	settled, err := commissionRepo.SettleToBalance(ctx, fixture.commissionID, service.DistributionCommissionSettlementInput{SettlementMethod: "balance"})
	require.NoError(t, err)
	require.Equal(t, "settled", settled.Status)

	_, err = reconciliationRepo.RunReconciliation(ctx, service.DistributionReconciliationRunInput{TriggerType: "manual"})
	require.NoError(t, err)
	issues, _, err := reconciliationRepo.ListReconciliationIssues(ctx, service.DistributionReconciliationFilter{
		ChannelOrgID: fixture.channelOrgID,
		Status:       "open",
	}, pagination.PaginationParams{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Empty(t, issues)

	_, err = integrationDB.ExecContext(ctx, `
DELETE FROM redeem_codes
WHERE notes = $1`, "distribution commission settlement #"+fmtInt64(fixture.commissionID))
	require.NoError(t, err)

	_, err = reconciliationRepo.RunReconciliation(ctx, service.DistributionReconciliationRunInput{TriggerType: "manual"})
	require.NoError(t, err)
	issues, _, err = reconciliationRepo.ListReconciliationIssues(ctx, service.DistributionReconciliationFilter{
		ChannelOrgID: fixture.channelOrgID,
		Status:       "open",
	}, pagination.PaginationParams{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Len(t, issues, 1)
	require.Equal(t, "commission_balance_audit_missing", issues[0].IssueType)
	require.False(t, issues[0].Repairable)

	dismissed, err := reconciliationRepo.ResolveReconciliationIssue(ctx, issues[0].ID, service.DistributionReconciliationResolutionInput{
		Action: "dismiss",
		Note:   "confirmed external payout",
	})
	require.NoError(t, err)
	require.Equal(t, "ignored", dismissed.Status)

	_, err = reconciliationRepo.RunReconciliation(ctx, service.DistributionReconciliationRunInput{TriggerType: "manual"})
	require.NoError(t, err)
	ignored, err := reconciliationRepo.GetReconciliationIssue(ctx, issues[0].ID)
	require.NoError(t, err)
	require.Equal(t, "ignored", ignored.Status)
	require.Greater(t, ignored.OccurrenceCount, issues[0].OccurrenceCount)
}

func TestDistributionReconciliation_DoesNotReportOfflineCommissionAsUsageLedgerLoss(t *testing.T) {
	ctx := context.Background()
	usageRepo := NewUsageBillingRepository(testEntClient(t), integrationDB)
	commissionRepo := &distributionCommissionRepository{db: integrationDB}
	reconciliationRepo := &distributionWalletRepository{db: integrationDB}
	consumer, apiKey, account, channelOrgID, memberID := createAtomicDistributionUsageFixture(t, 100)
	t.Cleanup(func() {
		cleanupDistributionReconciliationIssues(t, channelOrgID)
	})

	command := atomicDistributionUsageCommand(
		"reconciliation-offline-"+uuid.NewString(),
		consumer.ID,
		apiKey.ID,
		account.ID,
		channelOrgID,
		memberID,
	)
	applied, err := usageRepo.Apply(ctx, command)
	require.NoError(t, err)
	require.True(t, applied.Applied)

	var commissionID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
UPDATE commission_ledger
SET status = 'available',
    frozen_until = NULL
WHERE billing_usage_entry_id = $1
RETURNING id`, applied.BillingUsageEntryID).Scan(&commissionID))
	settled, err := commissionRepo.Settle(ctx, commissionID, service.DistributionCommissionSettlementInput{
		SettlementMethod: "offline",
	})
	require.NoError(t, err)
	require.Equal(t, "settled", settled.Status)
	require.Equal(t, "offline", settled.SettlementMethod)

	_, err = reconciliationRepo.RunReconciliation(ctx, service.DistributionReconciliationRunInput{TriggerType: "manual"})
	require.NoError(t, err)
	issues, _, err := reconciliationRepo.ListReconciliationIssues(ctx, service.DistributionReconciliationFilter{
		ChannelOrgID: channelOrgID,
		Status:       "open",
	}, pagination.PaginationParams{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Empty(t, issues)
}

func TestDistributionReconciliation_FullUsageSettlementReversalLifecycleIsClean(t *testing.T) {
	ctx := context.Background()
	usageRepo := NewUsageBillingRepository(testEntClient(t), integrationDB)
	commissionRepo := &distributionCommissionRepository{db: integrationDB}
	reconciliationRepo := &distributionWalletRepository{db: integrationDB}
	consumer, apiKey, account, channelOrgID, memberID := createAtomicDistributionUsageFixture(t, 100)
	var commissionID int64
	t.Cleanup(func() {
		cleanupDistributionReconciliationIssues(t, channelOrgID)
		_, err := integrationDB.ExecContext(context.Background(), `
DELETE FROM redeem_codes
WHERE notes IN ($1, $2)`,
			"distribution commission settlement #"+fmtInt64(commissionID),
			"distribution commission reversal #"+fmtInt64(commissionID),
		)
		require.NoError(t, err)
	})

	var beneficiaryUserID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT user_id
FROM channel_members
WHERE id = $1`, memberID).Scan(&beneficiaryUserID))
	command := atomicDistributionUsageCommand(
		"reconciliation-lifecycle-"+uuid.NewString(),
		consumer.ID,
		apiKey.ID,
		account.ID,
		channelOrgID,
		memberID,
	)
	command.DistributionAccounting.Commissions[0].UserID = beneficiaryUserID
	applied, err := usageRepo.Apply(ctx, command)
	require.NoError(t, err)
	require.True(t, applied.Applied)

	require.NoError(t, integrationDB.QueryRowContext(ctx, `
UPDATE commission_ledger
SET status = 'available',
    frozen_until = NULL
WHERE billing_usage_entry_id = $1
RETURNING id`, applied.BillingUsageEntryID).Scan(&commissionID))

	settled, err := commissionRepo.SettleToBalance(ctx, commissionID, service.DistributionCommissionSettlementInput{
		SettlementMethod: "balance",
	})
	require.NoError(t, err)
	require.Equal(t, "settled", settled.Status)

	reversed, err := commissionRepo.ReverseBalanceSettlement(ctx, commissionID)
	require.NoError(t, err)
	require.Equal(t, "reversed", reversed.Status)

	run, err := reconciliationRepo.RunReconciliation(ctx, service.DistributionReconciliationRunInput{TriggerType: "manual"})
	require.NoError(t, err)
	require.Equal(t, "completed", run.Status)
	issues, _, err := reconciliationRepo.ListReconciliationIssues(ctx, service.DistributionReconciliationFilter{
		ChannelOrgID: channelOrgID,
		Status:       "open",
	}, pagination.PaginationParams{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Empty(t, issues)

	var prepaidBalance, commissionReserved, beneficiaryBalance float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT prepaid_balance, commission_reserved
FROM channel_wallets
WHERE channel_org_id = $1`, channelOrgID).Scan(&prepaidBalance, &commissionReserved))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT balance
FROM users
WHERE id = $1`, beneficiaryUserID).Scan(&beneficiaryBalance))
	require.InDelta(t, 98, prepaidBalance, 0.00000001)
	require.Zero(t, commissionReserved)
	require.Zero(t, beneficiaryBalance)
}

func cleanupDistributionReconciliationFixture(t *testing.T, channelOrgID int64) {
	t.Helper()
	cleanupDistributionReconciliationIssues(t, channelOrgID)
	ctx := context.Background()
	_, err := integrationDB.ExecContext(ctx, `DELETE FROM channel_wallet_transactions WHERE channel_org_id = $1`, channelOrgID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `DELETE FROM channel_alert_events WHERE channel_org_id = $1`, channelOrgID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `DELETE FROM channel_organizations WHERE id = $1`, channelOrgID)
	require.NoError(t, err)
}

func cleanupDistributionReconciliationIssues(t *testing.T, channelOrgID int64) {
	t.Helper()
	ctx := context.Background()
	_, err := integrationDB.ExecContext(ctx, `
DELETE FROM distribution_reconciliation_issue_actions
WHERE issue_id IN (
    SELECT id
    FROM distribution_reconciliation_issues
    WHERE channel_org_id = $1
)`, channelOrgID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
DELETE FROM distribution_reconciliation_issues
WHERE channel_org_id = $1`, channelOrgID)
	require.NoError(t, err)
}

func fmtInt64(value int64) string {
	return strconv.FormatInt(value, 10)
}
