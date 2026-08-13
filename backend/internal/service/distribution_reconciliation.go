package service

import (
	"context"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

var (
	ErrInvalidDistributionReconciliation = infraerrors.BadRequest(
		"INVALID_DISTRIBUTION_RECONCILIATION",
		"invalid distribution reconciliation request",
	)
	ErrDistributionReconciliationBusy = infraerrors.Conflict(
		"DISTRIBUTION_RECONCILIATION_BUSY",
		"distribution reconciliation is already running",
	)
	ErrDistributionReconciliationIssueNotRepairable = infraerrors.Conflict(
		"DISTRIBUTION_RECONCILIATION_ISSUE_NOT_REPAIRABLE",
		"distribution reconciliation issue requires manual handling",
	)
)

type DistributionReconciliationRun struct {
	ID              int64      `json:"id"`
	TriggerType     string     `json:"trigger_type"`
	Status          string     `json:"status"`
	IssueCount      int        `json:"issue_count"`
	ResolvedCount   int        `json:"resolved_count"`
	ErrorMessage    string     `json:"error_message"`
	StartedByUserID *int64     `json:"started_by_user_id,omitempty"`
	StartedAt       time.Time  `json:"started_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
}

type DistributionReconciliationIssue struct {
	ID                  int64          `json:"id"`
	Fingerprint         string         `json:"fingerprint"`
	IssueType           string         `json:"issue_type"`
	Severity            string         `json:"severity"`
	Status              string         `json:"status"`
	ChannelOrgID        *int64         `json:"channel_org_id,omitempty"`
	OrganizationName    string         `json:"organization_name"`
	BillingUsageEntryID *int64         `json:"billing_usage_entry_id,omitempty"`
	CommissionID        *int64         `json:"commission_id,omitempty"`
	ExpectedAmount      float64        `json:"expected_amount"`
	ActualAmount        float64        `json:"actual_amount"`
	Details             map[string]any `json:"details"`
	OccurrenceCount     int            `json:"occurrence_count"`
	Repairable          bool           `json:"repairable"`
	LastDetectedRunID   *int64         `json:"last_detected_run_id,omitempty"`
	ResolutionAction    string         `json:"resolution_action"`
	ResolutionNote      string         `json:"resolution_note"`
	ResolvedByUserID    *int64         `json:"resolved_by_user_id,omitempty"`
	FirstDetectedAt     time.Time      `json:"first_detected_at"`
	LastDetectedAt      time.Time      `json:"last_detected_at"`
	ResolvedAt          *time.Time     `json:"resolved_at,omitempty"`
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
}

type DistributionReconciliationIssueAction struct {
	ID             int64     `json:"id"`
	IssueID        int64     `json:"issue_id"`
	RunID          *int64    `json:"run_id,omitempty"`
	Action         string    `json:"action"`
	PreviousStatus string    `json:"previous_status"`
	NewStatus      string    `json:"new_status"`
	Note           string    `json:"note"`
	OperatorUserID *int64    `json:"operator_user_id,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type DistributionReconciliationSummary struct {
	LatestRun       *DistributionReconciliationRun `json:"latest_run,omitempty"`
	OpenCount       int64                          `json:"open_count"`
	CriticalCount   int64                          `json:"critical_count"`
	WarningCount    int64                          `json:"warning_count"`
	IgnoredCount    int64                          `json:"ignored_count"`
	ResolvedCount   int64                          `json:"resolved_count"`
	RepairableCount int64                          `json:"repairable_count"`
}

type DistributionReconciliationFilter struct {
	ChannelOrgID int64
	IssueType    string
	Severity     string
	Status       string
}

func (f DistributionReconciliationFilter) normalized() DistributionReconciliationFilter {
	if f.ChannelOrgID < 0 {
		f.ChannelOrgID = 0
	}
	f.IssueType = normalizeDistributionReconciliationIssueType(f.IssueType)
	f.Severity = normalizeDistributionReconciliationSeverity(f.Severity)
	f.Status = normalizeDistributionReconciliationStatus(f.Status)
	return f
}

type DistributionReconciliationRunInput struct {
	TriggerType     string
	StartedByUserID *int64
}

type DistributionReconciliationResolutionInput struct {
	Action         string
	Note           string
	OperatorUserID *int64
}

type DistributionReconciliationIssueActionInput struct {
	RunID          *int64
	Action         string
	PreviousStatus string
	NewStatus      string
	Note           string
	OperatorUserID *int64
}

type DistributionReconciliationRepository interface {
	RunReconciliation(ctx context.Context, input DistributionReconciliationRunInput) (*DistributionReconciliationRun, error)
	ListReconciliationIssues(ctx context.Context, filter DistributionReconciliationFilter, params pagination.PaginationParams) ([]DistributionReconciliationIssue, *pagination.PaginationResult, error)
	GetReconciliationIssue(ctx context.Context, issueID int64) (*DistributionReconciliationIssue, error)
	ListReconciliationIssueActions(ctx context.Context, issueID int64) ([]DistributionReconciliationIssueAction, error)
	RecordReconciliationIssueAction(ctx context.Context, issueID int64, input DistributionReconciliationIssueActionInput) error
	GetReconciliationSummary(ctx context.Context) (*DistributionReconciliationSummary, error)
	ResolveReconciliationIssue(ctx context.Context, issueID int64, input DistributionReconciliationResolutionInput) (*DistributionReconciliationIssue, error)
}

type DistributionReconciliationExecutor interface {
	RunReconciliation(ctx context.Context, input DistributionReconciliationRunInput) (*DistributionReconciliationRun, error)
}

func (s *DistributionAdminService) SetReconciliationRepository(repo DistributionReconciliationRepository) {
	if s == nil {
		return
	}
	s.reconciliationRepo = repo
}

func (s *DistributionAdminService) RunReconciliation(ctx context.Context, input DistributionReconciliationRunInput) (*DistributionReconciliationRun, error) {
	if s == nil || s.reconciliationRepo == nil {
		return nil, ErrInvalidDistributionReconciliation
	}
	input.TriggerType = normalizeDistributionReconciliationTrigger(input.TriggerType)
	return s.reconciliationRepo.RunReconciliation(ctx, input)
}

func (s *DistributionAdminService) ListReconciliationIssues(ctx context.Context, filter DistributionReconciliationFilter, params pagination.PaginationParams) ([]DistributionReconciliationIssue, *pagination.PaginationResult, error) {
	if s == nil || s.reconciliationRepo == nil {
		return nil, nil, ErrInvalidDistributionReconciliation
	}
	return s.reconciliationRepo.ListReconciliationIssues(ctx, filter.normalized(), params)
}

func (s *DistributionAdminService) GetReconciliationSummary(ctx context.Context) (*DistributionReconciliationSummary, error) {
	if s == nil || s.reconciliationRepo == nil {
		return nil, ErrInvalidDistributionReconciliation
	}
	return s.reconciliationRepo.GetReconciliationSummary(ctx)
}

func (s *DistributionAdminService) ListReconciliationIssueActions(ctx context.Context, issueID int64) ([]DistributionReconciliationIssueAction, error) {
	if s == nil || s.reconciliationRepo == nil || issueID <= 0 {
		return nil, ErrInvalidDistributionReconciliation
	}
	if _, err := s.reconciliationRepo.GetReconciliationIssue(ctx, issueID); err != nil {
		return nil, err
	}
	return s.reconciliationRepo.ListReconciliationIssueActions(ctx, issueID)
}

func (s *DistributionAdminService) ResolveReconciliationIssue(ctx context.Context, issueID int64, input DistributionReconciliationResolutionInput) (*DistributionReconciliationIssue, error) {
	if s == nil || s.reconciliationRepo == nil || issueID <= 0 {
		return nil, ErrInvalidDistributionReconciliation
	}
	input.Action = strings.ToLower(strings.TrimSpace(input.Action))
	input.Note = strings.TrimSpace(input.Note)
	if input.Action == "recheck" {
		before, err := s.reconciliationRepo.GetReconciliationIssue(ctx, issueID)
		if err != nil {
			return nil, err
		}
		run, err := s.reconciliationRepo.RunReconciliation(ctx, DistributionReconciliationRunInput{
			TriggerType:     "manual",
			StartedByUserID: input.OperatorUserID,
		})
		if err != nil {
			return nil, err
		}
		after, err := s.reconciliationRepo.GetReconciliationIssue(ctx, issueID)
		if err != nil {
			return nil, err
		}
		if err := s.reconciliationRepo.RecordReconciliationIssueAction(ctx, issueID, DistributionReconciliationIssueActionInput{
			RunID:          &run.ID,
			Action:         input.Action,
			PreviousStatus: before.Status,
			NewStatus:      after.Status,
			Note:           input.Note,
			OperatorUserID: input.OperatorUserID,
		}); err != nil {
			return nil, err
		}
		return after, nil
	}
	if input.Action != "repair" && input.Action != "dismiss" && input.Action != "reopen" {
		return nil, ErrInvalidDistributionReconciliation
	}
	if input.Action == "dismiss" && input.Note == "" {
		return nil, ErrInvalidDistributionReconciliation
	}
	return s.reconciliationRepo.ResolveReconciliationIssue(ctx, issueID, input)
}

func normalizeDistributionReconciliationTrigger(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "scheduled") {
		return "scheduled"
	}
	return "manual"
}

func normalizeDistributionReconciliationIssueType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "usage_wallet_transaction_missing",
		"usage_wallet_amount_mismatch",
		"usage_commission_reserve_mismatch",
		"usage_commission_ledger_mismatch",
		"wallet_state_mismatch",
		"wallet_commission_reserved_mismatch",
		"wallet_total_consumed_mismatch",
		"commission_wallet_action_missing",
		"commission_reversal_wallet_action_missing",
		"commission_balance_audit_missing",
		"commission_reversal_balance_audit_missing":
		return value
	default:
		return ""
	}
}

func normalizeDistributionReconciliationSeverity(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "critical", "warning", "info":
		return value
	default:
		return ""
	}
}

func normalizeDistributionReconciliationStatus(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "open", "resolved", "ignored":
		return value
	default:
		return ""
	}
}
