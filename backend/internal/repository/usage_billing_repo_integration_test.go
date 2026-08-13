//go:build integration

package repository

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestUsageBillingRepositoryApply_DeduplicatesBalanceBilling(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)

	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-user-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Balance:      100,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-usage-billing-" + uuid.NewString(),
		Name:   "billing",
		Quota:  1,
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name: "usage-billing-account-" + uuid.NewString(),
		Type: service.AccountTypeAPIKey,
	})

	requestID := uuid.NewString()
	cmd := &service.UsageBillingCommand{
		RequestID:           requestID,
		APIKeyID:            apiKey.ID,
		UserID:              user.ID,
		AccountID:           account.ID,
		AccountType:         service.AccountTypeAPIKey,
		BalanceCost:         1.25,
		APIKeyQuotaCost:     1.25,
		APIKeyRateLimitCost: 1.25,
	}

	result1, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.NotNil(t, result1)
	require.True(t, result1.Applied)
	require.True(t, result1.APIKeyQuotaExhausted)

	result2, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.NotNil(t, result2)
	require.False(t, result2.Applied)

	var balance float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id = $1", user.ID).Scan(&balance))
	require.InDelta(t, 98.75, balance, 0.000001)

	var quotaUsed float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT quota_used FROM api_keys WHERE id = $1", apiKey.ID).Scan(&quotaUsed))
	require.InDelta(t, 1.25, quotaUsed, 0.000001)

	var usage5h float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT usage_5h FROM api_keys WHERE id = $1", apiKey.ID).Scan(&usage5h))
	require.InDelta(t, 1.25, usage5h, 0.000001)

	var status string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT status FROM api_keys WHERE id = $1", apiKey.ID).Scan(&status))
	require.Equal(t, service.StatusAPIKeyQuotaExhausted, status)

	var dedupCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id = $1 AND api_key_id = $2", requestID, apiKey.ID).Scan(&dedupCount))
	require.Equal(t, 1, dedupCount)
}

func TestUsageBillingRepositoryApply_DeduplicatesSubscriptionBilling(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)

	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-sub-user-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
	})
	group := mustCreateGroup(t, client, &service.Group{
		Name:             "usage-billing-group-" + uuid.NewString(),
		Platform:         service.PlatformAnthropic,
		SubscriptionType: service.SubscriptionTypeSubscription,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID:  user.ID,
		GroupID: &group.ID,
		Key:     "sk-usage-billing-sub-" + uuid.NewString(),
		Name:    "billing-sub",
	})
	subscription := mustCreateSubscription(t, client, &service.UserSubscription{
		UserID:  user.ID,
		GroupID: group.ID,
	})

	requestID := uuid.NewString()
	cmd := &service.UsageBillingCommand{
		RequestID:        requestID,
		APIKeyID:         apiKey.ID,
		UserID:           user.ID,
		AccountID:        0,
		SubscriptionID:   &subscription.ID,
		SubscriptionCost: 2.5,
	}

	result1, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.True(t, result1.Applied)

	result2, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.False(t, result2.Applied)

	var dailyUsage float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT daily_usage_usd FROM user_subscriptions WHERE id = $1", subscription.ID).Scan(&dailyUsage))
	require.InDelta(t, 2.5, dailyUsage, 0.000001)
}

func TestUsageBillingRepositoryApply_RequestFingerprintConflict(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)

	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-conflict-user-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Balance:      100,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-usage-billing-conflict-" + uuid.NewString(),
		Name:   "billing-conflict",
	})

	requestID := uuid.NewString()
	_, err := repo.Apply(ctx, &service.UsageBillingCommand{
		RequestID:   requestID,
		APIKeyID:    apiKey.ID,
		UserID:      user.ID,
		BalanceCost: 1.25,
	})
	require.NoError(t, err)

	_, err = repo.Apply(ctx, &service.UsageBillingCommand{
		RequestID:   requestID,
		APIKeyID:    apiKey.ID,
		UserID:      user.ID,
		BalanceCost: 2.50,
	})
	require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
}

func TestUsageBillingRepositoryApply_UpdatesAccountQuota(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)

	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-account-user-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-usage-billing-account-" + uuid.NewString(),
		Name:   "billing-account",
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name: "usage-billing-account-quota-" + uuid.NewString(),
		Type: service.AccountTypeAPIKey,
		Extra: map[string]any{
			"quota_limit": 100.0,
		},
	})

	_, err := repo.Apply(ctx, &service.UsageBillingCommand{
		RequestID:        uuid.NewString(),
		APIKeyID:         apiKey.ID,
		UserID:           user.ID,
		AccountID:        account.ID,
		AccountType:      service.AccountTypeAPIKey,
		AccountQuotaCost: 3.5,
	})
	require.NoError(t, err)

	var quotaUsed float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COALESCE((extra->>'quota_used')::numeric, 0) FROM accounts WHERE id = $1", account.ID).Scan(&quotaUsed))
	require.InDelta(t, 3.5, quotaUsed, 0.000001)
}

func TestUsageBillingRepositoryApply_EnqueuesSchedulerOutboxOnQuotaCrossing(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)

	newFixture := func(t *testing.T, extra map[string]any) (int64, int64) {
		t.Helper()
		user := mustCreateUser(t, client, &service.User{
			Email:        fmt.Sprintf("usage-billing-outbox-user-%d-%s@example.com", time.Now().UnixNano(), uuid.NewString()),
			PasswordHash: "hash",
		})
		apiKey := mustCreateApiKey(t, client, &service.APIKey{
			UserID: user.ID,
			Key:    "sk-usage-billing-outbox-" + uuid.NewString(),
			Name:   "billing-outbox",
		})
		account := mustCreateAccount(t, client, &service.Account{
			Name:  "usage-billing-outbox-" + uuid.NewString(),
			Type:  service.AccountTypeAPIKey,
			Extra: extra,
		})
		return apiKey.ID, account.ID
	}

	outboxCountFor := func(t *testing.T, accountID int64) int {
		t.Helper()
		var count int
		require.NoError(t, integrationDB.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM scheduler_outbox WHERE event_type = $1 AND account_id = $2",
			service.SchedulerOutboxEventAccountChanged, accountID,
		).Scan(&count))
		return count
	}

	t.Run("daily_first_crossing_enqueues", func(t *testing.T) {
		apiKeyID, accountID := newFixture(t, map[string]any{
			"quota_daily_limit": 10.0,
		})
		// 第一次低于日限额：不应入队 outbox
		_, err := repo.Apply(ctx, &service.UsageBillingCommand{
			RequestID:        uuid.NewString(),
			APIKeyID:         apiKeyID,
			AccountID:        accountID,
			AccountType:      service.AccountTypeAPIKey,
			AccountQuotaCost: 4,
		})
		require.NoError(t, err)
		require.Equal(t, 0, outboxCountFor(t, accountID), "below limit should not enqueue")

		// 第二次跨越日限额：应入队一次 outbox
		_, err = repo.Apply(ctx, &service.UsageBillingCommand{
			RequestID:        uuid.NewString(),
			APIKeyID:         apiKeyID,
			AccountID:        accountID,
			AccountType:      service.AccountTypeAPIKey,
			AccountQuotaCost: 8,
		})
		require.NoError(t, err)
		require.Equal(t, 1, outboxCountFor(t, accountID), "crossing daily limit should enqueue once")

		// 再次递增（已超）：不应重复入队
		_, err = repo.Apply(ctx, &service.UsageBillingCommand{
			RequestID:        uuid.NewString(),
			APIKeyID:         apiKeyID,
			AccountID:        accountID,
			AccountType:      service.AccountTypeAPIKey,
			AccountQuotaCost: 2,
		})
		require.NoError(t, err)
		require.Equal(t, 1, outboxCountFor(t, accountID), "subsequent increments beyond limit should not re-enqueue")
	})

	t.Run("weekly_first_crossing_enqueues", func(t *testing.T) {
		apiKeyID, accountID := newFixture(t, map[string]any{
			"quota_weekly_limit": 10.0,
		})
		_, err := repo.Apply(ctx, &service.UsageBillingCommand{
			RequestID:        uuid.NewString(),
			APIKeyID:         apiKeyID,
			AccountID:        accountID,
			AccountType:      service.AccountTypeAPIKey,
			AccountQuotaCost: 15, // 单次即跨越
		})
		require.NoError(t, err)
		require.Equal(t, 1, outboxCountFor(t, accountID), "single-shot crossing weekly limit should enqueue once")
	})
}

func TestUsageBillingRepositoryApply_AtomicallyPersistsDistributionUsage(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)
	consumer, apiKey, account, orgID, memberID := createAtomicDistributionUsageFixture(t, 100)

	requestID := "atomic-usage-" + uuid.NewString()
	cmd := atomicDistributionUsageCommand(requestID, consumer.ID, apiKey.ID, account.ID, orgID, memberID)
	first, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.True(t, first.Applied)
	require.Positive(t, first.UsageLogID)
	require.Positive(t, first.BillingUsageEntryID)
	require.Equal(t, first.UsageLogID, cmd.UsageLog.ID)

	second, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.False(t, second.Applied)

	var balance float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id = $1", consumer.ID).Scan(&balance))
	require.InDelta(t, 98.5, balance, 0.00000001)

	var usageLogCount, billingEventCount, walletTransactionCount, commissionCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_logs WHERE request_id = $1 AND api_key_id = $2", requestID, apiKey.ID).Scan(&usageLogCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM billing_usage_entries WHERE request_id = $1 AND api_key_id = $2", requestID, apiKey.ID).Scan(&billingEventCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM channel_wallet_transactions WHERE billing_usage_entry_id = $1", first.BillingUsageEntryID).Scan(&walletTransactionCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM commission_ledger WHERE billing_usage_entry_id = $1", first.BillingUsageEntryID).Scan(&commissionCount))
	require.Equal(t, 1, usageLogCount)
	require.Equal(t, 1, billingEventCount)
	require.Equal(t, 2, walletTransactionCount)
	require.Equal(t, 1, commissionCount)

	var prepaidBalance, commissionReserved, totalConsumed float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT prepaid_balance, commission_reserved, total_consumed
		FROM channel_wallets
		WHERE channel_org_id = $1
	`, orgID).Scan(&prepaidBalance, &commissionReserved, &totalConsumed))
	require.InDelta(t, 98, prepaidBalance, 0.00000001)
	require.InDelta(t, 0.2, commissionReserved, 0.00000001)
	require.InDelta(t, 2, totalConsumed, 0.00000001)

	var channelUsageDelta, commissionReservedDelta float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT channel_usage_delta, commission_reserved_delta
		FROM billing_usage_entries
		WHERE id = $1
	`, first.BillingUsageEntryID).Scan(&channelUsageDelta, &commissionReservedDelta))
	require.InDelta(t, 2, channelUsageDelta, 0.00000001)
	require.InDelta(t, 0.2, commissionReservedDelta, 0.00000001)
}

func TestUsageBillingRepositoryApply_ConcurrentRetriesApplyDistributionUsageExactlyOnce(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)
	consumer, apiKey, account, orgID, memberID := createAtomicDistributionUsageFixture(t, 100)

	const requestCount = 20
	requestID := "atomic-concurrent-" + uuid.NewString()
	type applyResult struct {
		result *service.UsageBillingApplyResult
		err    error
	}
	results := make([]applyResult, requestCount)
	start := make(chan struct{})
	var waitGroup sync.WaitGroup
	waitGroup.Add(requestCount)
	for index := 0; index < requestCount; index++ {
		go func(resultIndex int) {
			defer waitGroup.Done()
			<-start
			results[resultIndex].result, results[resultIndex].err = repo.Apply(
				ctx,
				atomicDistributionUsageCommand(requestID, consumer.ID, apiKey.ID, account.ID, orgID, memberID),
			)
		}(index)
	}
	close(start)
	waitGroup.Wait()

	appliedCount := 0
	for _, result := range results {
		require.NoError(t, result.err)
		require.NotNil(t, result.result)
		if result.result.Applied {
			appliedCount++
		}
	}
	require.Equal(t, 1, appliedCount)

	var balance float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id = $1", consumer.ID).Scan(&balance))
	require.InDelta(t, 98.5, balance, 0.00000001)

	var usageLogCount, billingEventCount, walletTransactionCount, commissionCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_logs WHERE request_id = $1 AND api_key_id = $2", requestID, apiKey.ID).Scan(&usageLogCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM billing_usage_entries WHERE request_id = $1 AND api_key_id = $2", requestID, apiKey.ID).Scan(&billingEventCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM channel_wallet_transactions WHERE request_id = $1 AND api_key_id = $2", requestID, apiKey.ID).Scan(&walletTransactionCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM commission_ledger WHERE request_id = $1 AND api_key_id = $2", requestID, apiKey.ID).Scan(&commissionCount))
	require.Equal(t, 1, usageLogCount)
	require.Equal(t, 1, billingEventCount)
	require.Equal(t, 2, walletTransactionCount)
	require.Equal(t, 1, commissionCount)

	var prepaidBalance, commissionReserved, totalConsumed float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT prepaid_balance, commission_reserved, total_consumed
		FROM channel_wallets
		WHERE channel_org_id = $1
	`, orgID).Scan(&prepaidBalance, &commissionReserved, &totalConsumed))
	require.InDelta(t, 98, prepaidBalance, 0.00000001)
	require.InDelta(t, 0.2, commissionReserved, 0.00000001)
	require.InDelta(t, 2, totalConsumed, 0.00000001)
}

func TestUsageBillingRepositoryApply_RollsBackAllEffectsWhenDistributionWalletIsInsufficient(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)
	consumer, apiKey, account, orgID, memberID := createAtomicDistributionUsageFixture(t, 1)

	requestID := "atomic-rollback-" + uuid.NewString()
	cmd := atomicDistributionUsageCommand(requestID, consumer.ID, apiKey.ID, account.ID, orgID, memberID)
	_, err := repo.Apply(ctx, cmd)
	require.ErrorIs(t, err, service.ErrDistributionWalletInsufficientBalance)

	var balance float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id = $1", consumer.ID).Scan(&balance))
	require.InDelta(t, 100, balance, 0.00000001)

	for table, condition := range map[string]string{
		"usage_billing_dedup":   "request_id = $1 AND api_key_id = $2",
		"usage_logs":            "request_id = $1 AND api_key_id = $2",
		"billing_usage_entries": "request_id = $1 AND api_key_id = $2",
		"commission_ledger":     "request_id = $1 AND api_key_id = $2",
	} {
		var count int
		require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE "+condition, requestID, apiKey.ID).Scan(&count))
		require.Zero(t, count, "%s must roll back", table)
	}

	var prepaidBalance, commissionReserved, totalConsumed float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT prepaid_balance, commission_reserved, total_consumed
		FROM channel_wallets
		WHERE channel_org_id = $1
	`, orgID).Scan(&prepaidBalance, &commissionReserved, &totalConsumed))
	require.InDelta(t, 1, prepaidBalance, 0.00000001)
	require.Zero(t, commissionReserved)
	require.Zero(t, totalConsumed)
}

func createAtomicDistributionUsageFixture(
	t *testing.T,
	prepaidBalance float64,
) (*service.User, *service.APIKey, *service.Account, int64, int64) {
	t.Helper()
	ctx := context.Background()
	client := testEntClient(t)
	consumer := mustCreateUser(t, client, &service.User{
		Email:        "atomic-consumer-" + uuid.NewString() + "@example.com",
		PasswordHash: "hash",
		Balance:      100,
	})
	memberUser := mustCreateUser(t, client, &service.User{
		Email:        "atomic-member-" + uuid.NewString() + "@example.com",
		PasswordHash: "hash",
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: consumer.ID,
		Key:    "sk-atomic-usage-" + uuid.NewString(),
		Name:   "atomic-usage",
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name: "atomic-usage-account-" + uuid.NewString(),
		Type: service.AccountTypeAPIKey,
	})

	var orgID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO channel_organizations (type, name, status, config)
		VALUES ('reseller', $1, 'active', '{"consumption_limit": 1000}'::jsonb)
		RETURNING id
	`, "atomic-org-"+uuid.NewString()).Scan(&orgID))
	var memberID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO channel_members (channel_org_id, user_id, role_type, commission_rate, status)
		VALUES ($1, $2, 'agent', 0.1, 'active')
		RETURNING id
	`, orgID, memberUser.ID).Scan(&memberID))
	_, err := integrationDB.ExecContext(ctx, `
		INSERT INTO user_attributions (user_id, channel_org_id, referrer_member_id, bound_source, bound_by)
		VALUES ($1, $2, $3, 'registration', 'system')
	`, consumer.ID, orgID, memberID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO channel_wallets (channel_org_id, prepaid_balance, total_recharged, status)
		VALUES ($1, $2, $2, 'active')
	`, orgID, prepaidBalance)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupStatements := []struct {
			query string
			args  []any
		}{
			{"DELETE FROM channel_wallet_transactions WHERE channel_org_id = $1", []any{orgID}},
			{"DELETE FROM commission_ledger WHERE channel_org_id = $1", []any{orgID}},
			{"DELETE FROM billing_usage_entries WHERE api_key_id = $1", []any{apiKey.ID}},
			{"DELETE FROM usage_logs WHERE api_key_id = $1", []any{apiKey.ID}},
			{"DELETE FROM user_attributions WHERE channel_org_id = $1", []any{orgID}},
			{"DELETE FROM channel_organizations WHERE id = $1", []any{orgID}},
			{"DELETE FROM usage_billing_dedup WHERE api_key_id = $1", []any{apiKey.ID}},
			{"DELETE FROM api_keys WHERE id = $1", []any{apiKey.ID}},
			{"DELETE FROM accounts WHERE id = $1", []any{account.ID}},
			{"DELETE FROM users WHERE id IN ($1, $2)", []any{consumer.ID, memberUser.ID}},
		}
		for _, statement := range cleanupStatements {
			_, cleanupErr := integrationDB.ExecContext(context.Background(), statement.query, statement.args...)
			require.NoError(t, cleanupErr)
		}
	})
	return consumer, apiKey, account, orgID, memberID
}

func atomicDistributionUsageCommand(
	requestID string,
	userID int64,
	apiKeyID int64,
	accountID int64,
	orgID int64,
	memberID int64,
) *service.UsageBillingCommand {
	return &service.UsageBillingCommand{
		RequestID:   requestID,
		APIKeyID:    apiKeyID,
		UserID:      userID,
		AccountID:   accountID,
		AccountType: service.AccountTypeAPIKey,
		BalanceCost: 1.5,
		UsageLog: &service.UsageLog{
			UserID:         userID,
			APIKeyID:       apiKeyID,
			AccountID:      accountID,
			RequestID:      requestID,
			Model:          "gpt-5.1",
			InputTokens:    10,
			OutputTokens:   5,
			TotalCost:      1.5,
			ActualCost:     1.5,
			RateMultiplier: 1,
			BillingType:    service.BillingTypeBalance,
		},
		DistributionAccounting: &service.DistributionUsageAccountingPlan{
			ChannelOrgID:      orgID,
			ConsumptionAmount: 2,
			ConsumptionLimit:  1000,
			Commissions: []service.DistributionCommissionInput{{
				ChannelOrgID:     orgID,
				MemberID:         memberID,
				UserID:           userID,
				CommissionType:   "direct",
				BaseAmount:       2,
				Rate:             0.1,
				Amount:           0.2,
				Status:           "frozen",
				SettlementMethod: "balance",
			}},
		},
	}
}

func TestDashboardAggregationRepositoryCleanupUsageBillingDedup_BatchDeletesOldRows(t *testing.T) {
	ctx := context.Background()
	repo := newDashboardAggregationRepositoryWithSQL(integrationDB)

	oldRequestID := "dedup-old-" + uuid.NewString()
	newRequestID := "dedup-new-" + uuid.NewString()
	oldCreatedAt := time.Now().UTC().AddDate(0, 0, -400)
	newCreatedAt := time.Now().UTC().Add(-time.Hour)

	_, err := integrationDB.ExecContext(ctx, `
		INSERT INTO usage_billing_dedup (request_id, api_key_id, request_fingerprint, created_at)
		VALUES ($1, 1, $2, $3), ($4, 1, $5, $6)
	`,
		oldRequestID, strings.Repeat("a", 64), oldCreatedAt,
		newRequestID, strings.Repeat("b", 64), newCreatedAt,
	)
	require.NoError(t, err)

	require.NoError(t, repo.CleanupUsageBillingDedup(ctx, time.Now().UTC().AddDate(0, 0, -365)))

	var oldCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id = $1", oldRequestID).Scan(&oldCount))
	require.Equal(t, 0, oldCount)

	var newCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id = $1", newRequestID).Scan(&newCount))
	require.Equal(t, 1, newCount)

	var archivedCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_billing_dedup_archive WHERE request_id = $1", oldRequestID).Scan(&archivedCount))
	require.Equal(t, 1, archivedCount)
}

func TestUsageBillingRepositoryApply_DeduplicatesAgainstArchivedKey(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)
	aggRepo := newDashboardAggregationRepositoryWithSQL(integrationDB)

	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-archive-user-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Balance:      100,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-usage-billing-archive-" + uuid.NewString(),
		Name:   "billing-archive",
	})

	requestID := uuid.NewString()
	cmd := &service.UsageBillingCommand{
		RequestID:   requestID,
		APIKeyID:    apiKey.ID,
		UserID:      user.ID,
		BalanceCost: 1.25,
	}

	result1, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.True(t, result1.Applied)

	_, err = integrationDB.ExecContext(ctx, `
		UPDATE usage_billing_dedup
		SET created_at = $1
		WHERE request_id = $2 AND api_key_id = $3
	`, time.Now().UTC().AddDate(0, 0, -400), requestID, apiKey.ID)
	require.NoError(t, err)
	require.NoError(t, aggRepo.CleanupUsageBillingDedup(ctx, time.Now().UTC().AddDate(0, 0, -365)))

	result2, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.False(t, result2.Applied)

	var balance float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id = $1", user.ID).Scan(&balance))
	require.InDelta(t, 98.75, balance, 0.000001)
}
