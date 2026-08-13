package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type distributionReconciliationRepositoryStub struct {
	runResult *DistributionReconciliationRun
	runInput  DistributionReconciliationRunInput
	runErr    error

	issues   []*DistributionReconciliationIssue
	getCalls int

	listFilter DistributionReconciliationFilter
	listParams pagination.PaginationParams

	actionIssueID int64
	actionInput   DistributionReconciliationIssueActionInput
	actionErr     error

	resolveIssueID int64
	resolveInput   DistributionReconciliationResolutionInput
	resolveResult  *DistributionReconciliationIssue
	resolveErr     error
}

func (s *distributionReconciliationRepositoryStub) RunReconciliation(_ context.Context, input DistributionReconciliationRunInput) (*DistributionReconciliationRun, error) {
	s.runInput = input
	return s.runResult, s.runErr
}

func (s *distributionReconciliationRepositoryStub) ListReconciliationIssues(_ context.Context, filter DistributionReconciliationFilter, params pagination.PaginationParams) ([]DistributionReconciliationIssue, *pagination.PaginationResult, error) {
	s.listFilter = filter
	s.listParams = params
	return nil, &pagination.PaginationResult{Page: params.Page, PageSize: params.PageSize}, nil
}

func (s *distributionReconciliationRepositoryStub) GetReconciliationIssue(_ context.Context, _ int64) (*DistributionReconciliationIssue, error) {
	if len(s.issues) == 0 {
		return nil, ErrInvalidDistributionReconciliation
	}
	index := s.getCalls
	if index >= len(s.issues) {
		index = len(s.issues) - 1
	}
	s.getCalls++
	return s.issues[index], nil
}

func (s *distributionReconciliationRepositoryStub) ListReconciliationIssueActions(context.Context, int64) ([]DistributionReconciliationIssueAction, error) {
	return nil, nil
}

func (s *distributionReconciliationRepositoryStub) RecordReconciliationIssueAction(_ context.Context, issueID int64, input DistributionReconciliationIssueActionInput) error {
	s.actionIssueID = issueID
	s.actionInput = input
	return s.actionErr
}

func (s *distributionReconciliationRepositoryStub) GetReconciliationSummary(context.Context) (*DistributionReconciliationSummary, error) {
	return &DistributionReconciliationSummary{}, nil
}

func (s *distributionReconciliationRepositoryStub) ResolveReconciliationIssue(_ context.Context, issueID int64, input DistributionReconciliationResolutionInput) (*DistributionReconciliationIssue, error) {
	s.resolveIssueID = issueID
	s.resolveInput = input
	return s.resolveResult, s.resolveErr
}

func TestDistributionAdminServiceResolveReconciliationIssue_RecheckRecordsRunAudit(t *testing.T) {
	operatorUserID := int64(42)
	repo := &distributionReconciliationRepositoryStub{
		runResult: &DistributionReconciliationRun{ID: 91, Status: "completed"},
		issues: []*DistributionReconciliationIssue{
			{ID: 7, Status: "open"},
			{ID: 7, Status: "resolved"},
		},
	}
	svc := &DistributionAdminService{}
	svc.SetReconciliationRepository(repo)

	issue, err := svc.ResolveReconciliationIssue(context.Background(), 7, DistributionReconciliationResolutionInput{
		Action:         " ReCheck ",
		Note:           " verified ",
		OperatorUserID: &operatorUserID,
	})

	require.NoError(t, err)
	require.Equal(t, "resolved", issue.Status)
	require.Equal(t, "manual", repo.runInput.TriggerType)
	require.Equal(t, &operatorUserID, repo.runInput.StartedByUserID)
	require.Equal(t, int64(7), repo.actionIssueID)
	require.Equal(t, int64(91), *repo.actionInput.RunID)
	require.Equal(t, "recheck", repo.actionInput.Action)
	require.Equal(t, "open", repo.actionInput.PreviousStatus)
	require.Equal(t, "resolved", repo.actionInput.NewStatus)
	require.Equal(t, "verified", repo.actionInput.Note)
	require.Equal(t, &operatorUserID, repo.actionInput.OperatorUserID)
}

func TestDistributionAdminServiceResolveReconciliationIssue_DismissRequiresAuditNote(t *testing.T) {
	repo := &distributionReconciliationRepositoryStub{}
	svc := &DistributionAdminService{}
	svc.SetReconciliationRepository(repo)

	issue, err := svc.ResolveReconciliationIssue(context.Background(), 7, DistributionReconciliationResolutionInput{
		Action: "dismiss",
		Note:   "   ",
	})

	require.Nil(t, issue)
	require.ErrorIs(t, err, ErrInvalidDistributionReconciliation)
	require.Zero(t, repo.resolveIssueID)
}

func TestDistributionAdminServiceListReconciliationIssues_NormalizesFilter(t *testing.T) {
	repo := &distributionReconciliationRepositoryStub{}
	svc := &DistributionAdminService{}
	svc.SetReconciliationRepository(repo)
	params := pagination.PaginationParams{Page: 2, PageSize: 25}

	_, _, err := svc.ListReconciliationIssues(context.Background(), DistributionReconciliationFilter{
		ChannelOrgID: -1,
		IssueType:    " WALLET_STATE_MISMATCH ",
		Severity:     " CRITICAL ",
		Status:       " OPEN ",
	}, params)

	require.NoError(t, err)
	require.Equal(t, DistributionReconciliationFilter{
		IssueType: "wallet_state_mismatch",
		Severity:  "critical",
		Status:    "open",
	}, repo.listFilter)
	require.Equal(t, params, repo.listParams)
}

func TestDistributionAdminServiceResolveReconciliationIssue_RecheckStopsOnRunFailure(t *testing.T) {
	runErr := errors.New("scan failed")
	repo := &distributionReconciliationRepositoryStub{
		runErr: runErr,
		issues: []*DistributionReconciliationIssue{{ID: 7, Status: "open"}},
	}
	svc := &DistributionAdminService{}
	svc.SetReconciliationRepository(repo)

	issue, err := svc.ResolveReconciliationIssue(context.Background(), 7, DistributionReconciliationResolutionInput{Action: "recheck"})

	require.Nil(t, issue)
	require.ErrorIs(t, err, runErr)
	require.Zero(t, repo.actionIssueID)
}
