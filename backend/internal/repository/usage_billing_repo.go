package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type usageBillingRepository struct {
	db *sql.DB
}

func NewUsageBillingRepository(_ *dbent.Client, sqlDB *sql.DB) service.UsageBillingRepository {
	return &usageBillingRepository{db: sqlDB}
}

func (r *usageBillingRepository) Apply(ctx context.Context, cmd *service.UsageBillingCommand) (_ *service.UsageBillingApplyResult, err error) {
	if cmd == nil {
		return &service.UsageBillingApplyResult{}, nil
	}
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing repository db is nil")
	}

	cmd.Normalize()
	if cmd.RequestID == "" {
		return nil, service.ErrUsageBillingRequestIDRequired
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	applied, err := r.claimUsageBillingKey(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}
	if !applied {
		return &service.UsageBillingApplyResult{Applied: false}, nil
	}

	result := &service.UsageBillingApplyResult{Applied: true}
	usageLog, err := persistUsageBillingLog(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}
	if usageLog != nil {
		result.UsageLogID = usageLog.ID
		entryID, err := insertBillingUsageEntry(ctx, tx, cmd, usageLog.ID)
		if err != nil {
			return nil, err
		}
		result.BillingUsageEntryID = entryID
	}

	if !cmd.SkipCoreBilling {
		if err := r.applyUsageBillingEffects(ctx, tx, cmd, result); err != nil {
			return nil, err
		}
	}
	if err := applyDistributionUsageAccounting(ctx, tx, cmd, result); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	if cmd.UsageLog != nil && usageLog != nil {
		cmd.UsageLog.ID = usageLog.ID
		cmd.UsageLog.CreatedAt = usageLog.CreatedAt
		cmd.UsageLog.RateMultiplier = usageLog.RateMultiplier
	}
	return result, nil
}

func (r *usageBillingRepository) claimUsageBillingKey(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) (bool, error) {
	return r.claimUsageBillingRequest(ctx, tx, cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint)
}

func (r *usageBillingRepository) claimUsageBillingRequest(ctx context.Context, tx *sql.Tx, requestID string, apiKeyID int64, requestFingerprint string) (bool, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO usage_billing_dedup (request_id, api_key_id, request_fingerprint)
		VALUES ($1, $2, $3)
		ON CONFLICT (request_id, api_key_id) DO NOTHING
		RETURNING id
	`, requestID, apiKeyID, requestFingerprint).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		var existingFingerprint string
		if err := tx.QueryRowContext(ctx, `
			SELECT request_fingerprint
			FROM usage_billing_dedup
			WHERE request_id = $1 AND api_key_id = $2
		`, requestID, apiKeyID).Scan(&existingFingerprint); err != nil {
			return false, err
		}
		if strings.TrimSpace(existingFingerprint) != strings.TrimSpace(requestFingerprint) {
			return false, service.ErrUsageBillingRequestConflict
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var archivedFingerprint string
	err = tx.QueryRowContext(ctx, `
		SELECT request_fingerprint
		FROM usage_billing_dedup_archive
		WHERE request_id = $1 AND api_key_id = $2
	`, requestID, apiKeyID).Scan(&archivedFingerprint)
	if err == nil {
		if strings.TrimSpace(archivedFingerprint) != strings.TrimSpace(requestFingerprint) {
			return false, service.ErrUsageBillingRequestConflict
		}
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	return true, nil
}

func (r *usageBillingRepository) ReserveBatchImageBalance(ctx context.Context, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	return r.applyBatchImageBalanceHold(ctx, cmd, reserveUsageBillingBatchImageBalance)
}

func (r *usageBillingRepository) CaptureBatchImageBalance(ctx context.Context, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	return r.applyBatchImageBalanceHold(ctx, cmd, captureUsageBillingBatchImageBalance)
}

func (r *usageBillingRepository) ReleaseBatchImageBalance(ctx context.Context, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	return r.applyBatchImageBalanceHold(ctx, cmd, releaseUsageBillingBatchImageBalance)
}

func (r *usageBillingRepository) applyBatchImageBalanceHold(
	ctx context.Context,
	cmd *service.BatchImageBalanceHoldCommand,
	apply func(context.Context, *sql.Tx, *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error),
) (_ *service.BatchImageBalanceHoldResult, err error) {
	if cmd == nil {
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing repository db is nil")
	}
	cmd.Normalize()
	if cmd.RequestID == "" {
		return nil, service.ErrUsageBillingRequestIDRequired
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	applied, err := r.claimUsageBillingRequest(ctx, tx, cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint)
	if err != nil {
		return nil, err
	}
	if !applied {
		return &service.BatchImageBalanceHoldResult{Applied: false}, nil
	}

	result, err := apply(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}
	if result == nil {
		result = &service.BatchImageBalanceHoldResult{}
	}
	result.Applied = true

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return result, nil
}

func (r *usageBillingRepository) applyUsageBillingEffects(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand, result *service.UsageBillingApplyResult) error {
	if cmd.SubscriptionCost > 0 && cmd.SubscriptionID != nil {
		if err := incrementUsageBillingSubscription(ctx, tx, *cmd.SubscriptionID, cmd.SubscriptionCost); err != nil {
			return err
		}
	}

	if cmd.BalanceCost > 0 {
		newBalance, sufficient, err := deductUsageBillingBalance(ctx, tx, cmd.UserID, cmd.BalanceCost)
		if err != nil {
			return err
		}
		result.NewBalance = &newBalance
		result.BalanceOverdrafted = !sufficient
	}

	if cmd.APIKeyQuotaCost > 0 {
		exhausted, err := incrementUsageBillingAPIKeyQuota(ctx, tx, cmd.APIKeyID, cmd.APIKeyQuotaCost)
		if err != nil {
			return err
		}
		result.APIKeyQuotaExhausted = exhausted
	}

	if cmd.APIKeyRateLimitCost > 0 {
		if err := incrementUsageBillingAPIKeyRateLimit(ctx, tx, cmd.APIKeyID, cmd.APIKeyRateLimitCost); err != nil {
			return err
		}
	}

	if cmd.AccountQuotaCost > 0 && (strings.EqualFold(cmd.AccountType, service.AccountTypeAPIKey) || strings.EqualFold(cmd.AccountType, service.AccountTypeBedrock)) {
		quotaState, err := incrementUsageBillingAccountQuota(ctx, tx, cmd.AccountID, cmd.AccountQuotaCost)
		if err != nil {
			return err
		}
		result.QuotaState = quotaState
	}

	return nil
}

func persistUsageBillingLog(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) (*service.UsageLog, error) {
	if cmd == nil || cmd.UsageLog == nil {
		return nil, nil
	}
	usageLog := *cmd.UsageLog
	usageLog.RequestID = cmd.RequestID
	usageLog.APIKeyID = cmd.APIKeyID
	usageLog.UserID = cmd.UserID
	usageLog.AccountID = cmd.AccountID
	repo := newUsageLogRepositoryWithSQL(nil, tx)
	if _, err := repo.createSingle(ctx, tx, &usageLog); err != nil {
		return nil, fmt.Errorf("persist usage log in billing transaction: %w", err)
	}
	if usageLog.ID <= 0 {
		return nil, errors.New("persisted usage log has no id")
	}
	return &usageLog, nil
}

func insertBillingUsageEntry(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand, usageLogID int64) (int64, error) {
	if cmd == nil || usageLogID <= 0 {
		return 0, nil
	}
	var channelOrgID any
	if cmd.DistributionAccounting != nil && cmd.DistributionAccounting.ChannelOrgID > 0 {
		channelOrgID = cmd.DistributionAccounting.ChannelOrgID
	}
	userDelta := cmd.BalanceCost + cmd.SubscriptionCost
	if cmd.SkipCoreBilling {
		userDelta = 0
	}
	var entryID int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO billing_usage_entries (
			usage_log_id,
			user_id,
			api_key_id,
			subscription_id,
			billing_type,
			applied,
			delta_usd,
			request_id,
			request_fingerprint,
			channel_org_id,
			channel_usage_delta,
			commission_reserved_delta,
			created_at
		)
		VALUES ($1, $2, $3, $4, $5, TRUE, $6, $7, $8, $9, 0, 0, NOW())
		RETURNING id
	`,
		usageLogID,
		cmd.UserID,
		cmd.APIKeyID,
		nullableInt64Arg(cmd.SubscriptionID),
		cmd.BillingType,
		userDelta,
		cmd.RequestID,
		cmd.RequestFingerprint,
		channelOrgID,
	).Scan(&entryID)
	if err != nil {
		return 0, fmt.Errorf("insert billing usage entry: %w", err)
	}
	return entryID, nil
}

func applyDistributionUsageAccounting(
	ctx context.Context,
	tx *sql.Tx,
	cmd *service.UsageBillingCommand,
	result *service.UsageBillingApplyResult,
) error {
	if cmd == nil || cmd.DistributionAccounting == nil || cmd.DistributionAccounting.ChannelOrgID <= 0 {
		return nil
	}
	if result == nil || result.UsageLogID <= 0 || result.BillingUsageEntryID <= 0 {
		return errors.New("distribution usage accounting requires a persisted billing event")
	}
	plan := cmd.DistributionAccounting
	org, err := getDistributionOrganizationByID(ctx, tx, plan.ChannelOrgID)
	if err != nil {
		return err
	}
	if org == nil {
		return service.ErrInvalidDistributionOrganization
	}
	if !strings.EqualFold(strings.TrimSpace(org.Status), "active") {
		return service.ErrDistributionChannelSuspended
	}
	consumptionAmount := plan.ConsumptionAmount
	if consumptionAmount < 0 {
		return service.ErrInvalidDistributionWalletTransaction
	}

	reservedAmount, err := insertDistributionUsageCommissions(ctx, tx, cmd, result, plan)
	if err != nil {
		return err
	}
	if !distributionOrganizationUsesWallet(org.Type) {
		return updateDistributionBillingEventTotals(ctx, tx, result.BillingUsageEntryID, consumptionAmount, 0)
	}

	if consumptionAmount <= 0 && reservedAmount <= 0 {
		return nil
	}
	current, err := getDistributionWalletByChannelOrgID(ctx, tx, plan.ChannelOrgID, true)
	if err != nil {
		return err
	}
	if current == nil {
		return service.ErrInvalidDistributionWallet
	}
	if service.NormalizeDistributionWalletStatus(current.Status) != "active" {
		return service.ErrDistributionChannelSuspended
	}
	consumptionLimit := plan.ConsumptionLimit
	if consumptionLimit <= 0 {
		consumptionLimit = distributionWalletConsumptionLimitFromConfig(org.Config)
	}
	if consumptionLimit > 0 && current.TotalConsumed+consumptionAmount > consumptionLimit {
		return service.ErrDistributionChannelConsumptionLimitExceeded
	}
	if current.PrepaidBalance-current.CommissionReserved < consumptionAmount+reservedAmount {
		return service.ErrDistributionWalletInsufficientBalance
	}

	intermediate := *current
	intermediate.PrepaidBalance -= consumptionAmount
	intermediate.TotalConsumed += consumptionAmount
	finalState := distributionWalletMutationState{
		prepaidBalance:  intermediate.PrepaidBalance,
		reservedBalance: intermediate.CommissionReserved + reservedAmount,
		totalRecharged:  intermediate.TotalRecharged,
		totalConsumed:   intermediate.TotalConsumed,
	}
	finalState.status = resolveDistributionWalletMutationStatus(current, &finalState, org)
	updated, err := updateDistributionWalletState(ctx, tx, plan.ChannelOrgID, &finalState)
	if err != nil {
		return err
	}

	entryID := result.BillingUsageEntryID
	usageLogID := result.UsageLogID
	apiKeyID := cmd.APIKeyID
	if consumptionAmount > 0 {
		state := distributionWalletMutationState{
			transactionType:     "consume",
			billingUsageEntryID: &entryID,
			usageLogID:          &usageLogID,
			requestID:           cmd.RequestID,
			apiKeyID:            &apiKeyID,
		}
		if err := insertDistributionWalletTransaction(ctx, tx, plan.ChannelOrgID, consumptionAmount, current, &intermediate, &state); err != nil {
			return err
		}
	}
	if reservedAmount > 0 {
		state := distributionWalletMutationState{
			transactionType:     "commission_reserve",
			billingUsageEntryID: &entryID,
			usageLogID:          &usageLogID,
			requestID:           cmd.RequestID,
			apiKeyID:            &apiKeyID,
		}
		if err := insertDistributionWalletTransaction(ctx, tx, plan.ChannelOrgID, reservedAmount, &intermediate, updated, &state); err != nil {
			return err
		}
	}
	if err := syncDistributionAlertEvents(ctx, tx, org, updated); err != nil {
		return err
	}
	return updateDistributionBillingEventTotals(ctx, tx, result.BillingUsageEntryID, consumptionAmount, reservedAmount)
}

func updateDistributionBillingEventTotals(
	ctx context.Context,
	tx *sql.Tx,
	billingUsageEntryID int64,
	consumptionAmount float64,
	reservedAmount float64,
) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE billing_usage_entries
		SET channel_usage_delta = $2,
			commission_reserved_delta = $3
		WHERE id = $1
	`, billingUsageEntryID, consumptionAmount, reservedAmount); err != nil {
		return fmt.Errorf("update distribution billing event totals: %w", err)
	}
	return nil
}

func insertDistributionUsageCommissions(
	ctx context.Context,
	tx *sql.Tx,
	cmd *service.UsageBillingCommand,
	result *service.UsageBillingApplyResult,
	plan *service.DistributionUsageAccountingPlan,
) (float64, error) {
	if plan == nil || len(plan.Commissions) == 0 {
		return 0, nil
	}
	reservedAmount := 0.0
	usageLogID := result.UsageLogID
	entryID := result.BillingUsageEntryID
	apiKeyID := cmd.APIKeyID
	for _, commission := range plan.Commissions {
		if commission.ChannelOrgID != plan.ChannelOrgID || commission.MemberID <= 0 || commission.UserID <= 0 || commission.BaseAmount <= 0 || commission.Rate <= 0 || commission.Amount <= 0 {
			return 0, service.ErrInvalidDistributionCommission
		}
		dedupKey := strings.TrimSpace(commission.DedupKey)
		if dedupKey == "" {
			dedupKey = usageCommissionDedupKey(cmd.RequestID, cmd.APIKeyID, commission.MemberID, commission.CommissionType)
		}
		var insertedID int64
		err := tx.QueryRowContext(ctx, `
			INSERT INTO commission_ledger (
				channel_org_id,
				member_id,
				user_id,
				usage_log_id,
				billing_usage_entry_id,
				request_id,
				api_key_id,
				dedup_key,
				commission_type,
				base_amount,
				rate,
				amount,
				status,
				settlement_method,
				settlement_reference_no,
				settlement_note,
				frozen_until,
				settled_at,
				settled_by_user_id,
				reversed_from_id,
				created_at,
				updated_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, NOW(), NOW())
			ON CONFLICT (dedup_key) WHERE dedup_key <> '' DO NOTHING
			RETURNING id
		`,
			plan.ChannelOrgID,
			commission.MemberID,
			commission.UserID,
			usageLogID,
			entryID,
			cmd.RequestID,
			apiKeyID,
			dedupKey,
			commission.CommissionType,
			commission.BaseAmount,
			commission.Rate,
			commission.Amount,
			commission.Status,
			commission.SettlementMethod,
			commission.SettlementReferenceNo,
			commission.SettlementNote,
			nullableTimeArg(commission.FrozenUntil),
			nullableTimeArg(commission.SettledAt),
			nullableInt64Arg(commission.SettledByUserID),
			nullableInt64Arg(commission.ReversedFromID),
		).Scan(&insertedID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("insert distribution usage commission: %w", err)
		}
		if insertedID > 0 && commission.Amount > 0 && distributionCommissionRequiresReserve(commission.SettlementMethod) {
			reservedAmount += commission.Amount
		}
	}
	return reservedAmount, nil
}

func usageCommissionDedupKey(requestID string, apiKeyID, memberID int64, commissionType string) string {
	raw := fmt.Sprintf("%s|%d|%d|%s", strings.TrimSpace(requestID), apiKeyID, memberID, strings.TrimSpace(commissionType))
	sum := sha256.Sum256([]byte(raw))
	return "usage:" + hex.EncodeToString(sum[:])
}

func distributionOrganizationUsesWallet(orgType string) bool {
	switch strings.ToLower(strings.TrimSpace(orgType)) {
	case "reseller", "oem":
		return true
	default:
		return false
	}
}

func distributionCommissionRequiresReserve(settlementMethod string) bool {
	switch strings.ToLower(strings.TrimSpace(settlementMethod)) {
	case "balance", "auto", "manual":
		return true
	default:
		return false
	}
}

func incrementUsageBillingSubscription(ctx context.Context, tx *sql.Tx, subscriptionID int64, costUSD float64) error {
	const updateSQL = `
		UPDATE user_subscriptions us
		SET
			daily_usage_usd = us.daily_usage_usd + $1,
			weekly_usage_usd = us.weekly_usage_usd + $1,
			monthly_usage_usd = us.monthly_usage_usd + $1,
			updated_at = NOW()
		FROM groups g
		WHERE us.id = $2
			AND us.deleted_at IS NULL
			AND us.group_id = g.id
			AND g.deleted_at IS NULL
	`
	res, err := tx.ExecContext(ctx, updateSQL, costUSD, subscriptionID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected > 0 {
		return nil
	}
	return service.ErrSubscriptionNotFound
}

func deductUsageBillingBalance(ctx context.Context, tx *sql.Tx, userID int64, amount float64) (float64, bool, error) {
	var newBalance float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance - $1,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL AND balance >= $1
		RETURNING balance
	`, amount, userID).Scan(&newBalance)
	if err == nil {
		return newBalance, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}

	err = tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance - $1,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
		RETURNING balance
	`, amount, userID).Scan(&newBalance)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, service.ErrUserNotFound
	}
	if err != nil {
		return 0, false, err
	}
	return newBalance, false, nil
}

func reserveUsageBillingBatchImageBalance(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	if cmd.HoldAmount <= 0 {
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	var balance, frozen float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance - $1,
			frozen_balance = COALESCE(frozen_balance, 0) + $1,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL AND balance >= $1
		RETURNING balance, frozen_balance
	`, cmd.HoldAmount, cmd.UserID).Scan(&balance, &frozen)
	if err == nil {
		return &service.BatchImageBalanceHoldResult{NewBalance: &balance, FrozenBalance: &frozen}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if exists, existsErr := userExistsForBilling(ctx, tx, cmd.UserID); existsErr != nil {
		return nil, existsErr
	} else if !exists {
		return nil, service.ErrUserNotFound
	}
	return nil, service.ErrBatchImageInsufficientBalance
}

func captureUsageBillingBatchImageBalance(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	if cmd.HoldAmount <= 0 && cmd.ActualAmount <= 0 {
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	if cmd.ActualAmount-cmd.HoldAmount > 0.00000001 {
		return nil, service.ErrBatchImageSettlementCostExceedsHold
	}
	var balance, frozen float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance
				+ CASE WHEN $1 > $2 THEN $1 - $2 ELSE 0 END
				- CASE WHEN $2 > $1 THEN $2 - $1 ELSE 0 END,
			frozen_balance = COALESCE(frozen_balance, 0) - $1,
			updated_at = NOW()
		WHERE id = $3 AND deleted_at IS NULL AND COALESCE(frozen_balance, 0) >= $1
		RETURNING balance, frozen_balance
	`, cmd.HoldAmount, cmd.ActualAmount, cmd.UserID).Scan(&balance, &frozen)
	if err == nil {
		return &service.BatchImageBalanceHoldResult{NewBalance: &balance, FrozenBalance: &frozen}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if exists, existsErr := userExistsForBilling(ctx, tx, cmd.UserID); existsErr != nil {
		return nil, existsErr
	} else if !exists {
		return nil, service.ErrUserNotFound
	}
	return nil, errors.New("batch image frozen balance is insufficient")
}

func releaseUsageBillingBatchImageBalance(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	if cmd.HoldAmount <= 0 {
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	// 释放前校验该 job 确实预留过 hold（hold request id 已被 claim），
	// 防止从未成功冻结的 job 触发"幻影释放"，从其他用户的冻结资金池中凭空生成余额。
	held, heldErr := batchImageHoldClaimExists(ctx, tx, service.BatchImageHoldRequestID(cmd.BatchID), cmd.APIKeyID)
	if heldErr != nil {
		return nil, heldErr
	}
	if !held {
		logger.LegacyPrintf("repository.usage_billing", "[BatchImage] release skipped, hold was never reserved: batch=%s", cmd.BatchID)
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	var balance, frozen float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance + $1,
			frozen_balance = COALESCE(frozen_balance, 0) - $1,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL AND COALESCE(frozen_balance, 0) >= $1
		RETURNING balance, frozen_balance
	`, cmd.HoldAmount, cmd.UserID).Scan(&balance, &frozen)
	if err == nil {
		return &service.BatchImageBalanceHoldResult{NewBalance: &balance, FrozenBalance: &frozen}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if exists, existsErr := userExistsForBilling(ctx, tx, cmd.UserID); existsErr != nil {
		return nil, existsErr
	} else if !exists {
		return nil, service.ErrUserNotFound
	}
	return nil, errors.New("batch image frozen balance is insufficient")
}

// batchImageHoldClaimExists 检查 hold request id 是否已在 dedup（或归档）表中被 claim，
// 即该 batch 的冻结操作确实成功提交过。
func batchImageHoldClaimExists(ctx context.Context, tx *sql.Tx, holdRequestID string, apiKeyID int64) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `
		SELECT 1
		FROM usage_billing_dedup
		WHERE request_id = $1 AND api_key_id = $2
	`, holdRequestID, apiKeyID).Scan(&exists)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	err = tx.QueryRowContext(ctx, `
		SELECT 1
		FROM usage_billing_dedup_archive
		WHERE request_id = $1 AND api_key_id = $2
	`, holdRequestID, apiKeyID).Scan(&exists)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func userExistsForBilling(ctx context.Context, tx *sql.Tx, userID int64) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `
		SELECT 1
		FROM users
		WHERE id = $1 AND deleted_at IS NULL
	`, userID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func incrementUsageBillingAPIKeyQuota(ctx context.Context, tx *sql.Tx, apiKeyID int64, amount float64) (bool, error) {
	var exhausted bool
	err := tx.QueryRowContext(ctx, `
		UPDATE api_keys
		SET quota_used = quota_used + $1,
			status = CASE
				WHEN quota > 0
					AND status = $3
					AND quota_used < quota
					AND quota_used + $1 >= quota
				THEN $4
				ELSE status
			END,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
		RETURNING quota > 0 AND quota_used >= quota AND quota_used - $1 < quota
	`, amount, apiKeyID, service.StatusAPIKeyActive, service.StatusAPIKeyQuotaExhausted).Scan(&exhausted)
	if errors.Is(err, sql.ErrNoRows) {
		return false, service.ErrAPIKeyNotFound
	}
	if err != nil {
		return false, err
	}
	return exhausted, nil
}

func incrementUsageBillingAPIKeyRateLimit(ctx context.Context, tx *sql.Tx, apiKeyID int64, cost float64) error {
	res, err := tx.ExecContext(ctx, `
		UPDATE api_keys SET
			usage_5h = CASE WHEN window_5h_start IS NOT NULL AND window_5h_start + INTERVAL '5 hours' <= NOW() THEN $1 ELSE usage_5h + $1 END,
			usage_1d = CASE WHEN window_1d_start IS NOT NULL AND window_1d_start + INTERVAL '24 hours' <= NOW() THEN $1 ELSE usage_1d + $1 END,
			usage_7d = CASE WHEN window_7d_start IS NOT NULL AND window_7d_start + INTERVAL '7 days' <= NOW() THEN $1 ELSE usage_7d + $1 END,
			window_5h_start = CASE WHEN window_5h_start IS NULL OR window_5h_start + INTERVAL '5 hours' <= NOW() THEN NOW() ELSE window_5h_start END,
			window_1d_start = CASE WHEN window_1d_start IS NULL OR window_1d_start + INTERVAL '24 hours' <= NOW() THEN date_trunc('day', NOW()) ELSE window_1d_start END,
			window_7d_start = CASE WHEN window_7d_start IS NULL OR window_7d_start + INTERVAL '7 days' <= NOW() THEN date_trunc('day', NOW()) ELSE window_7d_start END,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
	`, cost, apiKeyID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrAPIKeyNotFound
	}
	return nil
}

func incrementUsageBillingAccountQuota(ctx context.Context, tx *sql.Tx, accountID int64, amount float64) (*service.AccountQuotaState, error) {
	rows, err := tx.QueryContext(ctx,
		`UPDATE accounts SET extra = (
			COALESCE(extra, '{}'::jsonb)
			|| jsonb_build_object('quota_used', COALESCE((extra->>'quota_used')::numeric, 0) + $1)
			|| CASE WHEN COALESCE((extra->>'quota_daily_limit')::numeric, 0) > 0 THEN
				jsonb_build_object(
					'quota_daily_used',
					CASE WHEN `+dailyExpiredExpr+`
					THEN $1
					ELSE COALESCE((extra->>'quota_daily_used')::numeric, 0) + $1 END,
					'quota_daily_start',
					CASE WHEN `+dailyExpiredExpr+`
					THEN `+nowUTC+`
					ELSE COALESCE(extra->>'quota_daily_start', `+nowUTC+`) END
				)
				|| CASE WHEN `+dailyExpiredExpr+` AND `+nextDailyResetAtExpr+` IS NOT NULL
				   THEN jsonb_build_object('quota_daily_reset_at', `+nextDailyResetAtExpr+`)
				   ELSE '{}'::jsonb END
			ELSE '{}'::jsonb END
			|| CASE WHEN COALESCE((extra->>'quota_weekly_limit')::numeric, 0) > 0 THEN
				jsonb_build_object(
					'quota_weekly_used',
					CASE WHEN `+weeklyExpiredExpr+`
					THEN $1
					ELSE COALESCE((extra->>'quota_weekly_used')::numeric, 0) + $1 END,
					'quota_weekly_start',
					CASE WHEN `+weeklyExpiredExpr+`
					THEN `+nowUTC+`
					ELSE COALESCE(extra->>'quota_weekly_start', `+nowUTC+`) END
				)
				|| CASE WHEN `+weeklyExpiredExpr+` AND `+nextWeeklyResetAtExpr+` IS NOT NULL
				   THEN jsonb_build_object('quota_weekly_reset_at', `+nextWeeklyResetAtExpr+`)
				   ELSE '{}'::jsonb END
			ELSE '{}'::jsonb END
		), updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
		RETURNING
			COALESCE((extra->>'quota_used')::numeric, 0),
			COALESCE((extra->>'quota_limit')::numeric, 0),
			COALESCE((extra->>'quota_daily_used')::numeric, 0),
			COALESCE((extra->>'quota_daily_limit')::numeric, 0),
			COALESCE((extra->>'quota_weekly_used')::numeric, 0),
			COALESCE((extra->>'quota_weekly_limit')::numeric, 0)`,
		amount, accountID)
	if err != nil {
		return nil, err
	}

	var state service.AccountQuotaState
	if rows.Next() {
		if err := rows.Scan(
			&state.TotalUsed, &state.TotalLimit,
			&state.DailyUsed, &state.DailyLimit,
			&state.WeeklyUsed, &state.WeeklyLimit,
		); err != nil {
			_ = rows.Close()
			return nil, err
		}
	} else {
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
		return nil, service.ErrAccountNotFound
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	// 必须在执行下一条 SQL 前显式关闭 rows：pq 驱动在同一连接上
	// 不允许前一条查询的结果集未耗尽时启动新查询，否则会返回
	// "unexpected Parse response" 错误。
	if err := rows.Close(); err != nil {
		return nil, err
	}
	// 任意维度额度在本次递增中从"未超"跨越到"已超"时，必须刷新调度快照，
	// 否则 Redis 中缓存的 Account 仍显示旧的 used 值，后续请求会继续选中本账号，
	// 最终观察到 daily_used / weekly_used 大幅超过配置的 limit。
	// 对于日/周额度，即使本次触发了周期重置（pre=0、post=amount），
	// 判定式 (post-amount) < limit 同样成立，逻辑与总额度保持一致。
	crossedTotal := state.TotalLimit > 0 && state.TotalUsed >= state.TotalLimit && (state.TotalUsed-amount) < state.TotalLimit
	crossedDaily := state.DailyLimit > 0 && state.DailyUsed >= state.DailyLimit && (state.DailyUsed-amount) < state.DailyLimit
	crossedWeekly := state.WeeklyLimit > 0 && state.WeeklyUsed >= state.WeeklyLimit && (state.WeeklyUsed-amount) < state.WeeklyLimit
	if crossedTotal || crossedDaily || crossedWeekly {
		if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountChanged, &accountID, nil, nil); err != nil {
			logger.LegacyPrintf("repository.usage_billing", "[SchedulerOutbox] enqueue quota exceeded failed: account=%d err=%v", accountID, err)
			return nil, err
		}
	}
	return &state, nil
}
