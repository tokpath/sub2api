package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// settleCommissionAtomic is the only write path for commission settlement.
// The commission row, channel wallet, wallet transaction, user balance payout,
// redeem audit record and final ledger state are committed together.
func (r *distributionCommissionRepository) settleCommissionAtomic(
	ctx context.Context,
	commissionID int64,
	input service.DistributionCommissionSettlementInput,
) (_ *service.DistributionCommissionLedger, err error) {
	if r == nil || r.db == nil || commissionID <= 0 {
		return nil, service.ErrInvalidDistributionCommission
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin atomic distribution commission settlement: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := thawDistributionCommissionByID(ctx, tx, commissionID); err != nil {
		return nil, err
	}
	original, err := getDistributionCommissionByIDForUpdate(ctx, tx, commissionID)
	if err != nil {
		return nil, err
	}
	if original == nil {
		return nil, service.ErrInvalidDistributionCommission
	}

	status := strings.ToLower(strings.TrimSpace(original.Status))
	if status == "settled" {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit idempotent distribution commission settlement: %w", err)
		}
		return original, nil
	}
	if status != "available" {
		return nil, service.ErrDistributionCommissionNotAvailable
	}

	settlementMethod := normalizeDistributionCommissionSettlementMethod(input.SettlementMethod, original.SettlementMethod)
	input.SettlementMethod = settlementMethod
	if err := applyDistributionCommissionSettlementWallet(ctx, tx, original, input); err != nil {
		return nil, err
	}
	if distributionCommissionSettlementPaysBalance(settlementMethod) {
		if err := updateDistributionCommissionUserBalance(ctx, tx, original.UserID, original.Amount); err != nil {
			return nil, err
		}
		if err := insertDistributionBalanceRedeemRecord(ctx, tx, original.UserID, original.Amount, commissionID); err != nil {
			return nil, err
		}
	}

	rows, err := tx.QueryContext(ctx, `
UPDATE commission_ledger
SET status = 'settled',
    settlement_method = $2,
    settlement_reference_no = $3,
    settlement_note = $4,
    settled_by_user_id = $5,
    settled_at = NOW(),
    frozen_until = NULL,
    updated_at = NOW()
WHERE id = $1
  AND status = 'available'
RETURNING id,
          channel_org_id,
          member_id,
          user_id,
          usage_log_id,
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
          updated_at`,
		commissionID,
		settlementMethod,
		strings.TrimSpace(input.SettlementReferenceNo),
		strings.TrimSpace(input.SettlementNote),
		nullableInt64Arg(input.SettledByUserID),
	)
	if err != nil {
		return nil, fmt.Errorf("settle distribution commission atomically: %w", err)
	}
	ledger, err := scanDistributionCommissionLedger(rows)
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit atomic distribution commission settlement: %w", err)
	}
	return ledger, nil
}

// reverseCommissionAtomic serializes every cancellation/reversal on the
// original commission row. Settled commissions get one immutable negative
// ledger entry; unsettled commissions transition to cancelled in place.
func (r *distributionCommissionRepository) reverseCommissionAtomic(
	ctx context.Context,
	commissionID int64,
) (_ *service.DistributionCommissionLedger, err error) {
	if r == nil || r.db == nil || commissionID <= 0 {
		return nil, service.ErrInvalidDistributionCommission
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin atomic distribution commission reversal: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	original, err := getDistributionCommissionByIDForUpdate(ctx, tx, commissionID)
	if err != nil {
		return nil, err
	}
	if original == nil {
		return nil, service.ErrInvalidDistributionCommission
	}

	status := strings.ToLower(strings.TrimSpace(original.Status))
	if status == "cancelled" {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit idempotent distribution commission cancellation: %w", err)
		}
		return original, nil
	}
	if status == "settled" {
		existing, err := getDistributionCommissionReversal(ctx, tx, commissionID)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			if err := tx.Commit(); err != nil {
				return nil, fmt.Errorf("commit idempotent distribution commission reversal: %w", err)
			}
			return existing, nil
		}

		if err := applyDistributionCommissionReversalWallet(ctx, tx, original); err != nil {
			return nil, err
		}
		if distributionCommissionSettlementPaysBalance(original.SettlementMethod) {
			if err := updateDistributionCommissionUserBalance(ctx, tx, original.UserID, -original.Amount); err != nil {
				return nil, err
			}
			if err := insertDistributionBalanceRedeemRecord(ctx, tx, original.UserID, -original.Amount, commissionID); err != nil {
				return nil, err
			}
		}

		reversal, err := insertDistributionCommissionReversal(ctx, tx, commissionID)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit atomic distribution commission reversal: %w", err)
		}
		return reversal, nil
	}

	switch status {
	case "pending", "frozen", "available":
		if err := applyDistributionCommissionReversalWallet(ctx, tx, original); err != nil {
			return nil, err
		}
	default:
		return nil, service.ErrDistributionCommissionNotReversible
	}

	rows, err := tx.QueryContext(ctx, `
UPDATE commission_ledger
SET status = 'cancelled',
    updated_at = NOW()
WHERE id = $1
  AND status IN ('pending', 'frozen', 'available')
RETURNING id,
          channel_org_id,
          member_id,
          user_id,
          usage_log_id,
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
          updated_at`, commissionID)
	if err != nil {
		return nil, fmt.Errorf("cancel distribution commission atomically: %w", err)
	}
	ledger, err := scanDistributionCommissionLedger(rows)
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit atomic distribution commission cancellation: %w", err)
	}
	return ledger, nil
}

func applyDistributionCommissionSettlementWallet(
	ctx context.Context,
	tx *sql.Tx,
	original *service.DistributionCommissionLedger,
	input service.DistributionCommissionSettlementInput,
) error {
	if original == nil || original.ChannelOrgID <= 0 || original.Amount <= 0 {
		return service.ErrInvalidDistributionCommission
	}
	org, err := getDistributionOrganizationByID(ctx, tx, original.ChannelOrgID)
	if err != nil {
		return err
	}
	if org == nil {
		return service.ErrInvalidDistributionOrganization
	}
	if !distributionOrganizationUsesWallet(org.Type) {
		return nil
	}

	current, err := getDistributionWalletByChannelOrgID(ctx, tx, original.ChannelOrgID, true)
	if err != nil {
		return err
	}
	state := &distributionWalletMutationState{
		prepaidBalance:  current.PrepaidBalance,
		reservedBalance: current.CommissionReserved,
		totalRecharged:  current.TotalRecharged,
		totalConsumed:   current.TotalConsumed,
		referenceNo:     input.SettlementReferenceNo,
		note:            distributionCommissionSettlementWalletNote(original.ID, input.SettlementNote),
		operatorUserID:  input.SettledByUserID,
		commissionID:    &original.ID,
	}
	wasReserved := distributionCommissionRequiresReserve(original.SettlementMethod)
	needsDeduction := distributionCommissionRequiresReserve(input.SettlementMethod)
	switch {
	case wasReserved && needsDeduction:
		if current.PrepaidBalance < original.Amount {
			return service.ErrDistributionWalletInsufficientBalance
		}
		if current.CommissionReserved < original.Amount {
			return service.ErrDistributionWalletInsufficientReserved
		}
		state.transactionType = "commission_settle"
		state.prepaidBalance -= original.Amount
		state.reservedBalance -= original.Amount
	case wasReserved && !needsDeduction:
		if current.CommissionReserved < original.Amount {
			return service.ErrDistributionWalletInsufficientReserved
		}
		state.transactionType = "commission_release"
		state.reservedBalance -= original.Amount
	case !wasReserved && needsDeduction:
		if current.PrepaidBalance-current.CommissionReserved < original.Amount {
			return service.ErrDistributionWalletInsufficientBalance
		}
		state.transactionType = "commission_deduct"
		state.prepaidBalance -= original.Amount
	default:
		return nil
	}
	return commitDistributionCommissionWalletMutation(ctx, tx, org, original.Amount, current, state)
}

func applyDistributionCommissionReversalWallet(
	ctx context.Context,
	tx *sql.Tx,
	original *service.DistributionCommissionLedger,
) error {
	if original == nil || original.ChannelOrgID <= 0 || original.Amount <= 0 {
		return service.ErrInvalidDistributionCommission
	}
	org, err := getDistributionOrganizationByID(ctx, tx, original.ChannelOrgID)
	if err != nil {
		return err
	}
	if org == nil {
		return service.ErrInvalidDistributionOrganization
	}
	if !distributionOrganizationUsesWallet(org.Type) {
		return nil
	}

	current, err := getDistributionWalletByChannelOrgID(ctx, tx, original.ChannelOrgID, true)
	if err != nil {
		return err
	}
	state := &distributionWalletMutationState{
		prepaidBalance:  current.PrepaidBalance,
		reservedBalance: current.CommissionReserved,
		totalRecharged:  current.TotalRecharged,
		totalConsumed:   current.TotalConsumed,
		note:            fmt.Sprintf("distribution commission reversal #%d", original.ID),
		commissionID:    &original.ID,
	}
	if strings.EqualFold(strings.TrimSpace(original.Status), "settled") {
		if !distributionCommissionRequiresReserve(original.SettlementMethod) {
			return nil
		}
		state.transactionType = "commission_refund"
		state.prepaidBalance += original.Amount
	} else {
		if !distributionCommissionRequiresReserve(original.SettlementMethod) {
			return nil
		}
		if current.CommissionReserved < original.Amount {
			return service.ErrDistributionWalletInsufficientReserved
		}
		state.transactionType = "commission_release"
		state.reservedBalance -= original.Amount
	}
	return commitDistributionCommissionWalletMutation(ctx, tx, org, original.Amount, current, state)
}

func commitDistributionCommissionWalletMutation(
	ctx context.Context,
	tx *sql.Tx,
	org *service.DistributionOrganization,
	amount float64,
	current *service.DistributionWallet,
	state *distributionWalletMutationState,
) error {
	state.status = resolveDistributionWalletMutationStatus(current, state, org)
	updated, err := updateDistributionWalletState(ctx, tx, current.ChannelOrgID, state)
	if err != nil {
		return err
	}
	if err := syncDistributionAlertEvents(ctx, tx, org, updated); err != nil {
		return err
	}
	return insertDistributionWalletTransaction(ctx, tx, current.ChannelOrgID, amount, current, updated, state)
}

func getDistributionCommissionReversal(ctx context.Context, tx *sql.Tx, commissionID int64) (*service.DistributionCommissionLedger, error) {
	var exists bool
	if err := tx.QueryRowContext(ctx, `
SELECT EXISTS(
    SELECT 1
    FROM commission_ledger
    WHERE reversed_from_id = $1
      AND status = 'reversed'
)`, commissionID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check distribution commission reversal: %w", err)
	}
	if !exists {
		return nil, nil
	}
	rows, err := tx.QueryContext(ctx, `
SELECT id,
       channel_org_id,
       member_id,
       user_id,
       usage_log_id,
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
FROM commission_ledger
WHERE reversed_from_id = $1
  AND status = 'reversed'
ORDER BY id ASC
LIMIT 1`, commissionID)
	if err != nil {
		return nil, fmt.Errorf("get distribution commission reversal: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanDistributionCommissionLedger(rows)
}

func insertDistributionCommissionReversal(ctx context.Context, tx *sql.Tx, commissionID int64) (*service.DistributionCommissionLedger, error) {
	rows, err := tx.QueryContext(ctx, `
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
SELECT channel_org_id,
       member_id,
       user_id,
       usage_log_id,
       NULL::BIGINT,
       request_id,
       api_key_id,
       'commission-reversal:' || id::text,
       commission_type,
       base_amount,
       rate,
       -amount,
       'reversed',
       settlement_method,
       settlement_reference_no,
       settlement_note,
       frozen_until,
       NOW(),
       settled_by_user_id,
       id,
       NOW(),
       NOW()
FROM commission_ledger
WHERE id = $1
  AND status = 'settled'
RETURNING id,
          channel_org_id,
          member_id,
          user_id,
          usage_log_id,
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
          updated_at`, commissionID)
	if err != nil {
		return nil, fmt.Errorf("insert distribution commission reversal: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanDistributionCommissionLedger(rows)
}

func updateDistributionCommissionUserBalance(ctx context.Context, tx *sql.Tx, userID int64, amount float64) error {
	result, err := tx.ExecContext(ctx, `
UPDATE users
SET balance = balance + $2,
    updated_at = NOW()
WHERE id = $1
  AND deleted_at IS NULL`, userID, amount)
	if err != nil {
		return fmt.Errorf("update distribution commission user balance: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return service.ErrUserNotFound
	}
	return nil
}

func normalizeDistributionCommissionSettlementMethod(candidate string, fallback string) string {
	for _, value := range []string{candidate, fallback} {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "balance", "auto", "manual", "offline":
			return strings.ToLower(strings.TrimSpace(value))
		}
	}
	return "manual"
}

func distributionCommissionSettlementPaysBalance(method string) bool {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "balance", "auto":
		return true
	default:
		return false
	}
}

func distributionCommissionSettlementWalletNote(commissionID int64, note string) string {
	note = strings.TrimSpace(note)
	if note == "" {
		return fmt.Sprintf("distribution commission settlement #%d", commissionID)
	}
	return fmt.Sprintf("distribution commission settlement #%d: %s", commissionID, note)
}
