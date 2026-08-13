package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func distributionReconciliationIssueRows(issueID int64, status string, lastRunID any, resolvedAt any) *sqlmock.Rows {
	detectedAt := time.Date(2026, 8, 13, 6, 0, 0, 0, time.UTC)
	return sqlmock.NewRows([]string{
		"id",
		"fingerprint",
		"issue_type",
		"severity",
		"status",
		"channel_org_id",
		"organization_name",
		"billing_usage_entry_id",
		"commission_id",
		"expected_amount",
		"actual_amount",
		"details",
		"occurrence_count",
		"repairable",
		"last_detected_run_id",
		"resolution_action",
		"resolution_note",
		"resolved_by_user_id",
		"first_detected_at",
		"last_detected_at",
		"resolved_at",
		"created_at",
		"updated_at",
	}).AddRow(
		issueID,
		"wallet:88:state",
		"wallet_state_mismatch",
		"critical",
		status,
		int64(88),
		"Independent Agent",
		nil,
		nil,
		100.0,
		90.0,
		[]byte(`{"latest_transaction_id":101}`),
		2,
		false,
		lastRunID,
		"",
		"",
		nil,
		detectedAt,
		detectedAt,
		resolvedAt,
		detectedAt,
		detectedAt,
	)
}

func TestBuildDistributionReconciliationWhere_UsesStablePlaceholders(t *testing.T) {
	whereSQL, args := buildDistributionReconciliationWhere(service.DistributionReconciliationFilter{
		ChannelOrgID: 88,
		IssueType:    "wallet_state_mismatch",
		Severity:     "critical",
		Status:       "open",
	})

	require.Equal(t, "WHERE 1=1 AND issue.channel_org_id = $1 AND issue.issue_type = $2 AND issue.severity = $3 AND issue.status = $4", whereSQL)
	require.Equal(t, []any{int64(88), "wallet_state_mismatch", "critical", "open"}, args)
}

func TestDistributionWalletRepositoryResolveReconciliationIssue_DismissesWithAtomicAudit(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &distributionWalletRepository{db: db}
	operatorUserID := int64(42)
	resolvedAt := time.Date(2026, 8, 13, 6, 5, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery("FROM distribution_reconciliation_issues issue").
		WithArgs(int64(7)).
		WillReturnRows(distributionReconciliationIssueRows(7, "open", int64(11), nil))
	mock.ExpectExec("UPDATE distribution_reconciliation_issues").
		WithArgs(int64(7), "ignored", "dismiss", "verified externally", operatorUserID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO distribution_reconciliation_issue_actions").
		WithArgs(int64(7), int64(11), "dismiss", "open", "ignored", "verified externally", operatorUserID).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	mock.ExpectQuery("FROM distribution_reconciliation_issues issue").
		WithArgs(int64(7)).
		WillReturnRows(distributionReconciliationIssueRows(7, "ignored", int64(11), resolvedAt))

	issue, err := repo.ResolveReconciliationIssue(context.Background(), 7, service.DistributionReconciliationResolutionInput{
		Action:         "dismiss",
		Note:           "verified externally",
		OperatorUserID: &operatorUserID,
	})

	require.NoError(t, err)
	require.Equal(t, "ignored", issue.Status)
	require.NotNil(t, issue.ResolvedAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDistributionWalletRepositoryResolveReconciliationIssue_RejectsRepairAfterResolution(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &distributionWalletRepository{db: db}
	resolvedAt := time.Date(2026, 8, 13, 6, 5, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery("FROM distribution_reconciliation_issues issue").
		WithArgs(int64(7)).
		WillReturnRows(distributionReconciliationIssueRows(7, "resolved", int64(11), resolvedAt))
	mock.ExpectRollback()

	issue, err := repo.ResolveReconciliationIssue(context.Background(), 7, service.DistributionReconciliationResolutionInput{Action: "repair"})

	require.Nil(t, issue)
	require.ErrorIs(t, err, service.ErrDistributionReconciliationIssueNotRepairable)
	require.NoError(t, mock.ExpectationsWereMet())
}
