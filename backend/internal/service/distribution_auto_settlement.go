package service

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

const (
	distributionAutoSettlementDefaultBatchSize   = 100
	distributionAutoSettlementDefaultTaskTimeout = 30 * time.Second
	distributionReconciliationDefaultTaskTimeout = 2 * time.Minute
)

type DistributionAutoSettlementRepository interface {
	ListAutoSettleCommissionIDs(ctx context.Context, limit int) ([]int64, error)
}

type DistributionAutoSettlementExecutor interface {
	SettleCommission(ctx context.Context, commissionID int64, input DistributionCommissionSettlementInput) (*DistributionCommissionLedger, error)
}

type DistributionAutoSettlementService struct {
	repo        DistributionAutoSettlementRepository
	executor    DistributionAutoSettlementExecutor
	interval    time.Duration
	batchSize   int
	taskTimeout time.Duration
	reconciler  DistributionReconciliationExecutor

	reconciliationInterval time.Duration
	reconciliationTimeout  time.Duration
	lastReconciliationAt   time.Time

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
	running  int32
}

func NewDistributionAutoSettlementService(
	repo DistributionAutoSettlementRepository,
	executor DistributionAutoSettlementExecutor,
	interval time.Duration,
) *DistributionAutoSettlementService {
	return &DistributionAutoSettlementService{
		repo:                  repo,
		executor:              executor,
		interval:              interval,
		batchSize:             distributionAutoSettlementDefaultBatchSize,
		taskTimeout:           distributionAutoSettlementDefaultTaskTimeout,
		reconciliationTimeout: distributionReconciliationDefaultTaskTimeout,
		stopCh:                make(chan struct{}),
	}
}

func (s *DistributionAutoSettlementService) SetReconciliationExecutor(executor DistributionReconciliationExecutor, interval time.Duration) {
	if s == nil {
		return
	}
	s.reconciler = executor
	s.reconciliationInterval = interval
}

func (s *DistributionAutoSettlementService) Start() {
	if s == nil || s.repo == nil || s.executor == nil || s.interval <= 0 {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		s.runOnce()
		for {
			select {
			case <-ticker.C:
				s.runOnce()
			case <-s.stopCh:
				return
			}
		}
	}()
}

func (s *DistributionAutoSettlementService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

func (s *DistributionAutoSettlementService) runOnce() {
	if s == nil || s.repo == nil || s.executor == nil {
		return
	}
	if !atomic.CompareAndSwapInt32(&s.running, 0, 1) {
		return
	}
	defer atomic.StoreInt32(&s.running, 0)
	defer s.runReconciliationIfDue()

	ctx, cancel := context.WithTimeout(context.Background(), s.taskTimeout)
	defer cancel()

	ids, err := s.repo.ListAutoSettleCommissionIDs(ctx, s.batchSize)
	if err != nil {
		logger.LegacyPrintf("service.distribution_auto_settlement", "[DistributionAutoSettlement] list auto-settle commissions failed: %v", err)
		return
	}
	for _, commissionID := range ids {
		if ctx.Err() != nil {
			return
		}
		if _, err := s.executor.SettleCommission(ctx, commissionID, DistributionCommissionSettlementInput{SettlementMethod: "auto"}); err != nil {
			logger.LegacyPrintf("service.distribution_auto_settlement", "[DistributionAutoSettlement] settle commission failed: commission_id=%d err=%v", commissionID, err)
		}
	}
}

func (s *DistributionAutoSettlementService) runReconciliationIfDue() {
	if s == nil || s.reconciler == nil || s.reconciliationInterval <= 0 {
		return
	}
	now := time.Now()
	if !s.lastReconciliationAt.IsZero() && now.Sub(s.lastReconciliationAt) < s.reconciliationInterval {
		return
	}
	s.lastReconciliationAt = now
	ctx, cancel := context.WithTimeout(context.Background(), s.reconciliationTimeout)
	defer cancel()
	if _, err := s.reconciler.RunReconciliation(ctx, DistributionReconciliationRunInput{TriggerType: "scheduled"}); err != nil {
		logger.LegacyPrintf("service.distribution_reconciliation", "[DistributionReconciliation] scheduled run failed: %v", err)
	}
}
