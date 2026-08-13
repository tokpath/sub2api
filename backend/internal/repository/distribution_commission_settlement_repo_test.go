package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func distributionCommissionLedgerRows(
	id int64,
	channelOrgID int64,
	memberID int64,
	userID int64,
	amount float64,
	status string,
	settlementMethod string,
	settlementReferenceNo string,
	settlementNote string,
	frozenUntil any,
	settledAt any,
	settledByUserID any,
	reversedFromID any,
	createdAt time.Time,
	updatedAt time.Time,
) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id",
		"channel_org_id",
		"member_id",
		"user_id",
		"usage_log_id",
		"commission_type",
		"base_amount",
		"rate",
		"amount",
		"status",
		"settlement_method",
		"settlement_reference_no",
		"settlement_note",
		"frozen_until",
		"settled_at",
		"settled_by_user_id",
		"reversed_from_id",
		"created_at",
		"updated_at",
	}).AddRow(
		id,
		channelOrgID,
		memberID,
		userID,
		nil,
		"direct",
		amount*10,
		0.1,
		amount,
		status,
		settlementMethod,
		settlementReferenceNo,
		settlementNote,
		frozenUntil,
		settledAt,
		settledByUserID,
		reversedFromID,
		createdAt,
		updatedAt,
	)
}

func TestDistributionCommissionRepositorySettle_RejectsUnmaturedFrozenCommission(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &distributionCommissionRepository{db: db}
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE commission_ledger").
		WithArgs(int64(1001)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("FROM commission_ledger WHERE id = \\$1 FOR UPDATE").
		WithArgs(int64(1001)).
		WillReturnRows(distributionCommissionLedgerRows(
			1001, 88, 42, 7, 10, "frozen", "manual", "", "",
			now.Add(time.Hour), nil, nil, nil, now, now,
		))
	mock.ExpectRollback()

	out, err := repo.Settle(context.Background(), 1001, service.DistributionCommissionSettlementInput{SettlementMethod: "manual"})
	require.Nil(t, out)
	require.ErrorIs(t, err, service.ErrDistributionCommissionNotAvailable)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDistributionCommissionRepositorySettle_IsIdempotentWhenAlreadySettled(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &distributionCommissionRepository{db: db}
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE commission_ledger").
		WithArgs(int64(1001)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("FROM commission_ledger WHERE id = \\$1 FOR UPDATE").
		WithArgs(int64(1001)).
		WillReturnRows(distributionCommissionLedgerRows(
			1001, 88, 42, 7, 10, "settled", "manual", "PAY-1", "done",
			nil, now, int64(9), nil, now, now,
		))
	mock.ExpectCommit()

	out, err := repo.Settle(context.Background(), 1001, service.DistributionCommissionSettlementInput{SettlementMethod: "offline"})
	require.NoError(t, err)
	require.NotNil(t, out)
	require.Equal(t, "settled", out.Status)
	require.Equal(t, "manual", out.SettlementMethod)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDistributionCommissionRepositorySettleToBalance_CommitsLedgerAndBalanceAudit(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &distributionCommissionRepository{db: db}
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	settledByUserID := int64(9)

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE commission_ledger").
		WithArgs(int64(1001)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("FROM commission_ledger WHERE id = \\$1 FOR UPDATE").
		WithArgs(int64(1001)).
		WillReturnRows(distributionCommissionLedgerRows(
			1001, 88, 42, 7, 10, "available", "balance", "", "",
			nil, nil, nil, nil, now, now,
		))
	mock.ExpectQuery("FROM channel_organizations").
		WithArgs(int64(88)).
		WillReturnRows(distributionOrganizationRows(88, "Platform", "platform", "active", `{}`, now, now))
	mock.ExpectExec("UPDATE users").
		WithArgs(int64(7), 10.0).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO redeem_codes").
		WithArgs(sqlmock.AnyArg(), service.RedeemTypeDistributionBalance, 10.0, int64(7), "distribution commission settlement #1001").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("UPDATE commission_ledger").
		WithArgs(int64(1001), "balance", "PAY-1", "approved", settledByUserID).
		WillReturnRows(distributionCommissionLedgerRows(
			1001, 88, 42, 7, 10, "settled", "balance", "PAY-1", "approved",
			nil, now, settledByUserID, nil, now, now,
		))
	mock.ExpectCommit()

	out, err := repo.SettleToBalance(context.Background(), 1001, service.DistributionCommissionSettlementInput{
		SettlementMethod:      "balance",
		SettlementReferenceNo: "PAY-1",
		SettlementNote:        "approved",
		SettledByUserID:       &settledByUserID,
	})
	require.NoError(t, err)
	require.NotNil(t, out)
	require.Equal(t, "settled", out.Status)
	require.Equal(t, "PAY-1", out.SettlementReferenceNo)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDistributionCommissionRepositorySettleToBalance_RollsBackWalletWhenBalanceCreditFails(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &distributionCommissionRepository{db: db}
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	settledByUserID := int64(9)

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE commission_ledger").
		WithArgs(int64(1001)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("FROM commission_ledger WHERE id = \\$1 FOR UPDATE").
		WithArgs(int64(1001)).
		WillReturnRows(distributionCommissionLedgerRows(
			1001, 88, 42, 7, 10, "available", "balance", "", "",
			nil, nil, nil, nil, now, now,
		))
	mock.ExpectQuery("FROM channel_organizations").
		WithArgs(int64(88)).
		WillReturnRows(distributionOrganizationRows(88, "Reseller", "reseller", "active", `{}`, now, now))
	mock.ExpectQuery("FROM channel_wallets w").
		WithArgs(int64(88)).
		WillReturnRows(distributionWalletRows(88, "Reseller", "reseller", 100, 10, 100, 0, 0, "active", now, now))
	mock.ExpectQuery("UPDATE channel_wallets").
		WithArgs(int64(88), 90.0, 0.0, 100.0, 0.0, "active").
		WillReturnRows(distributionWalletRows(88, "Reseller", "reseller", 90, 0, 100, 0, 0, "active", now, now))
	mock.ExpectQuery("FROM channel_alert_events").
		WithArgs(int64(88)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "alert_type", "severity", "status", "details"}))
	mock.ExpectQuery("INSERT INTO channel_wallet_transactions").
		WithArgs(
			int64(88),
			"commission_settle",
			10.0,
			100.0,
			90.0,
			10.0,
			0.0,
			"PAY-1",
			"distribution commission settlement #1001",
			settledByUserID,
			int64(1001),
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)))
	mock.ExpectExec("UPDATE users").
		WithArgs(int64(7), 10.0).
		WillReturnError(errors.New("credit failed"))
	mock.ExpectRollback()

	out, err := repo.SettleToBalance(context.Background(), 1001, service.DistributionCommissionSettlementInput{
		SettlementMethod:      "balance",
		SettlementReferenceNo: "PAY-1",
		SettledByUserID:       &settledByUserID,
	})
	require.Nil(t, out)
	require.ErrorContains(t, err, "credit failed")
	require.NoError(t, mock.ExpectationsWereMet())
}
