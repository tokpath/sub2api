//go:build integration

package repository

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type distributionCommissionSettlementFixture struct {
	userID         int64
	channelOrgID   int64
	memberID       int64
	commissionID   int64
	amount         float64
	initialBalance float64
	initialPrepaid float64
	initialReserve float64
}

func TestDistributionCommissionSettlement_RejectsFrozenBeforeMaturity(t *testing.T) {
	ctx := context.Background()
	repo := &distributionCommissionRepository{db: integrationDB}
	fixture := createDistributionCommissionSettlementFixture(t, "frozen", "balance", time.Now().UTC().Add(time.Hour))

	out, err := repo.SettleToBalance(ctx, fixture.commissionID, service.DistributionCommissionSettlementInput{SettlementMethod: "balance"})
	require.Nil(t, out)
	require.ErrorIs(t, err, service.ErrDistributionCommissionNotAvailable)

	assertDistributionCommissionSettlementState(t, fixture, "frozen", fixture.initialBalance, fixture.initialPrepaid, fixture.initialReserve, 0, 0, 0)
}

func TestDistributionCommissionSettlement_RollsBackWalletWhenBeneficiaryCannotBeCredited(t *testing.T) {
	ctx := context.Background()
	repo := &distributionCommissionRepository{db: integrationDB}
	fixture := createDistributionCommissionSettlementFixture(t, "available", "balance", time.Time{})

	_, err := integrationDB.ExecContext(ctx, `UPDATE users SET deleted_at = NOW() WHERE id = $1`, fixture.userID)
	require.NoError(t, err)

	out, err := repo.SettleToBalance(ctx, fixture.commissionID, service.DistributionCommissionSettlementInput{SettlementMethod: "balance"})
	require.Nil(t, out)
	require.ErrorIs(t, err, service.ErrUserNotFound)

	assertDistributionCommissionSettlementState(t, fixture, "available", fixture.initialBalance, fixture.initialPrepaid, fixture.initialReserve, 0, 0, 0)
}

func TestDistributionCommissionSettlement_ConcurrentRequestsCreditExactlyOnce(t *testing.T) {
	ctx := context.Background()
	repo := &distributionCommissionRepository{db: integrationDB}
	fixture := createDistributionCommissionSettlementFixture(t, "frozen", "balance", time.Now().UTC().Add(-time.Minute))
	settledByUserID := fixture.userID

	results := runConcurrentCommissionMutations(20, func() (*service.DistributionCommissionLedger, error) {
		return repo.SettleToBalance(ctx, fixture.commissionID, service.DistributionCommissionSettlementInput{
			SettlementMethod:      "balance",
			SettlementReferenceNo: "concurrent-settlement",
			SettledByUserID:       &settledByUserID,
		})
	})
	for _, result := range results {
		require.NoError(t, result.err)
		require.NotNil(t, result.ledger)
		require.Equal(t, "settled", result.ledger.Status)
	}

	assertDistributionCommissionSettlementState(
		t,
		fixture,
		"settled",
		fixture.initialBalance+fixture.amount,
		fixture.initialPrepaid-fixture.amount,
		0,
		1,
		1,
		0,
	)
}

func TestDistributionCommissionSettlement_AppliesWalletTransitionByMethod(t *testing.T) {
	testCases := []struct {
		name                 string
		originalMethod       string
		settlementMethod     string
		expectedPrepaid      float64
		expectedTransactions []string
	}{
		{
			name:                 "settle reserved commission",
			originalMethod:       "manual",
			settlementMethod:     "manual",
			expectedPrepaid:      90,
			expectedTransactions: []string{"commission_settle"},
		},
		{
			name:                 "release reserve for offline settlement",
			originalMethod:       "manual",
			settlementMethod:     "offline",
			expectedPrepaid:      100,
			expectedTransactions: []string{"commission_release"},
		},
		{
			name:                 "deduct unreserved offline commission when changed to manual",
			originalMethod:       "offline",
			settlementMethod:     "manual",
			expectedPrepaid:      90,
			expectedTransactions: []string{"commission_deduct"},
		},
		{
			name:                 "keep wallet unchanged for offline settlement",
			originalMethod:       "offline",
			settlementMethod:     "offline",
			expectedPrepaid:      100,
			expectedTransactions: []string{},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			repo := &distributionCommissionRepository{db: integrationDB}
			fixture := createDistributionCommissionSettlementFixture(t, "available", testCase.originalMethod, time.Time{})

			settled, err := repo.Settle(ctx, fixture.commissionID, service.DistributionCommissionSettlementInput{
				SettlementMethod: testCase.settlementMethod,
			})
			require.NoError(t, err)
			require.Equal(t, "settled", settled.Status)
			require.Equal(t, testCase.settlementMethod, settled.SettlementMethod)

			assertDistributionCommissionSettlementState(
				t,
				fixture,
				"settled",
				fixture.initialBalance,
				testCase.expectedPrepaid,
				0,
				len(testCase.expectedTransactions),
				0,
				0,
			)
			assertDistributionCommissionWalletTransactionTypes(t, fixture.commissionID, testCase.expectedTransactions)
		})
	}
}

func TestDistributionCommissionCancellation_ConcurrentRequestsReleaseReserveExactlyOnce(t *testing.T) {
	ctx := context.Background()
	repo := &distributionCommissionRepository{db: integrationDB}
	fixture := createDistributionCommissionSettlementFixture(t, "frozen", "manual", time.Now().UTC().Add(time.Hour))

	results := runConcurrentCommissionMutations(20, func() (*service.DistributionCommissionLedger, error) {
		return repo.Reverse(ctx, fixture.commissionID)
	})
	for _, result := range results {
		require.NoError(t, result.err)
		require.NotNil(t, result.ledger)
		require.Equal(t, fixture.commissionID, result.ledger.ID)
		require.Equal(t, "cancelled", result.ledger.Status)
	}

	assertDistributionCommissionSettlementState(
		t,
		fixture,
		"cancelled",
		fixture.initialBalance,
		fixture.initialPrepaid,
		0,
		1,
		0,
		0,
	)
	assertDistributionCommissionWalletTransactionTypes(t, fixture.commissionID, []string{"commission_release"})
}

func TestDistributionCommissionReversal_ConcurrentRequestsReverseExactlyOnce(t *testing.T) {
	ctx := context.Background()
	repo := &distributionCommissionRepository{db: integrationDB}
	fixture := createDistributionCommissionSettlementFixture(t, "available", "balance", time.Time{})
	settledByUserID := fixture.userID

	settled, err := repo.SettleToBalance(ctx, fixture.commissionID, service.DistributionCommissionSettlementInput{
		SettlementMethod: "balance",
		SettledByUserID:  &settledByUserID,
	})
	require.NoError(t, err)
	require.Equal(t, "settled", settled.Status)

	results := runConcurrentCommissionMutations(20, func() (*service.DistributionCommissionLedger, error) {
		return repo.ReverseBalanceSettlement(ctx, fixture.commissionID)
	})
	var reversalID int64
	for _, result := range results {
		require.NoError(t, result.err)
		require.NotNil(t, result.ledger)
		require.Equal(t, "reversed", result.ledger.Status)
		if reversalID == 0 {
			reversalID = result.ledger.ID
		}
		require.Equal(t, reversalID, result.ledger.ID)
	}

	assertDistributionCommissionSettlementState(
		t,
		fixture,
		"settled",
		fixture.initialBalance,
		fixture.initialPrepaid,
		0,
		2,
		2,
		1,
	)

	var reversalAmount float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT amount
		FROM commission_ledger
		WHERE reversed_from_id = $1
	`, fixture.commissionID).Scan(&reversalAmount))
	require.InDelta(t, -fixture.amount, reversalAmount, 0.00000001)
}

type distributionCommissionMutationResult struct {
	ledger *service.DistributionCommissionLedger
	err    error
}

func runConcurrentCommissionMutations(
	count int,
	mutate func() (*service.DistributionCommissionLedger, error),
) []distributionCommissionMutationResult {
	start := make(chan struct{})
	results := make([]distributionCommissionMutationResult, count)
	var waitGroup sync.WaitGroup
	waitGroup.Add(count)
	for index := 0; index < count; index++ {
		go func(resultIndex int) {
			defer waitGroup.Done()
			<-start
			results[resultIndex].ledger, results[resultIndex].err = mutate()
		}(index)
	}
	close(start)
	waitGroup.Wait()
	return results
}

func createDistributionCommissionSettlementFixture(
	t *testing.T,
	status string,
	settlementMethod string,
	frozenUntil time.Time,
) distributionCommissionSettlementFixture {
	t.Helper()
	ctx := context.Background()
	client := testEntClient(t)
	const (
		amount         = 10.0
		initialBalance = 50.0
		initialPrepaid = 100.0
	)
	initialReserve := 0.0
	if distributionCommissionRequiresReserve(settlementMethod) {
		initialReserve = amount
	}

	user := mustCreateUser(t, client, &service.User{
		Email:        "commission-settlement-" + uuid.NewString() + "@example.com",
		PasswordHash: "hash",
		Balance:      initialBalance,
	})

	var channelOrgID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO channel_organizations (type, name, status, config)
		VALUES ('reseller', $1, 'active', '{}'::jsonb)
		RETURNING id
	`, "settlement-org-"+uuid.NewString()).Scan(&channelOrgID))

	var memberID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO channel_members (channel_org_id, user_id, role_type, commission_rate, status)
		VALUES ($1, $2, 'agent', 0.1, 'active')
		RETURNING id
	`, channelOrgID, user.ID).Scan(&memberID))

	_, err := integrationDB.ExecContext(ctx, `
		INSERT INTO channel_wallets (
			channel_org_id,
			prepaid_balance,
			commission_reserved,
			total_recharged,
			status
		)
		VALUES ($1, $2, $3, $2, 'active')
	`, channelOrgID, initialPrepaid, initialReserve)
	require.NoError(t, err)

	var nullableFrozenUntil any
	if !frozenUntil.IsZero() {
		nullableFrozenUntil = frozenUntil
	}
	var commissionID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO commission_ledger (
			channel_org_id,
			member_id,
			user_id,
			commission_type,
			base_amount,
			rate,
			amount,
			status,
			settlement_method,
			frozen_until
		)
		VALUES ($1, $2, $3, 'direct', 100, 0.1, $4, $5, $6, $7)
		RETURNING id
	`, channelOrgID, memberID, user.ID, amount, status, settlementMethod, nullableFrozenUntil).Scan(&commissionID))

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		queries := []struct {
			query string
			args  []any
		}{
			{"DELETE FROM channel_wallet_transactions WHERE channel_org_id = $1", []any{channelOrgID}},
			{"DELETE FROM redeem_codes WHERE notes IN ($1, $2)", []any{
				"distribution commission settlement #" + strconv.FormatInt(commissionID, 10),
				"distribution commission reversal #" + strconv.FormatInt(commissionID, 10),
			}},
			{"DELETE FROM commission_ledger WHERE channel_org_id = $1", []any{channelOrgID}},
			{"DELETE FROM channel_alert_events WHERE channel_org_id = $1", []any{channelOrgID}},
			{"DELETE FROM channel_organizations WHERE id = $1", []any{channelOrgID}},
			{"DELETE FROM users WHERE id = $1", []any{user.ID}},
		}
		for _, query := range queries {
			_, cleanupErr := integrationDB.ExecContext(cleanupCtx, query.query, query.args...)
			require.NoError(t, cleanupErr)
		}
	})

	return distributionCommissionSettlementFixture{
		userID:         user.ID,
		channelOrgID:   channelOrgID,
		memberID:       memberID,
		commissionID:   commissionID,
		amount:         amount,
		initialBalance: initialBalance,
		initialPrepaid: initialPrepaid,
		initialReserve: initialReserve,
	}
}

func assertDistributionCommissionSettlementState(
	t *testing.T,
	fixture distributionCommissionSettlementFixture,
	expectedStatus string,
	expectedBalance float64,
	expectedPrepaid float64,
	expectedReserve float64,
	expectedWalletTransactions int,
	expectedRedeemRecords int,
	expectedReversals int,
) {
	t.Helper()
	ctx := context.Background()

	var status string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT status FROM commission_ledger WHERE id = $1`, fixture.commissionID).Scan(&status))
	require.Equal(t, expectedStatus, status)

	var balance float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance FROM users WHERE id = $1`, fixture.userID).Scan(&balance))
	require.InDelta(t, expectedBalance, balance, 0.00000001)

	var prepaidBalance, commissionReserved float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT prepaid_balance, commission_reserved
		FROM channel_wallets
		WHERE channel_org_id = $1
	`, fixture.channelOrgID).Scan(&prepaidBalance, &commissionReserved))
	require.InDelta(t, expectedPrepaid, prepaidBalance, 0.00000001)
	require.InDelta(t, expectedReserve, commissionReserved, 0.00000001)

	var walletTransactions int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM channel_wallet_transactions
		WHERE commission_id = $1
	`, fixture.commissionID).Scan(&walletTransactions))
	require.Equal(t, expectedWalletTransactions, walletTransactions)

	var redeemRecords int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM redeem_codes
		WHERE notes IN ($1, $2)
	`,
		"distribution commission settlement #"+strconv.FormatInt(fixture.commissionID, 10),
		"distribution commission reversal #"+strconv.FormatInt(fixture.commissionID, 10),
	).Scan(&redeemRecords))
	require.Equal(t, expectedRedeemRecords, redeemRecords)

	var reversals int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM commission_ledger
		WHERE reversed_from_id = $1
	`, fixture.commissionID).Scan(&reversals))
	require.Equal(t, expectedReversals, reversals)
}

func assertDistributionCommissionWalletTransactionTypes(t *testing.T, commissionID int64, expected []string) {
	t.Helper()
	rows, err := integrationDB.QueryContext(context.Background(), `
		SELECT transaction_type
		FROM channel_wallet_transactions
		WHERE commission_id = $1
		ORDER BY id ASC
	`, commissionID)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()

	actual := make([]string, 0, len(expected))
	for rows.Next() {
		var transactionType string
		require.NoError(t, rows.Scan(&transactionType))
		actual = append(actual, transactionType)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, expected, actual)
}
