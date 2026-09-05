package reconciliation

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/repository/receipt"
)

const CycleTimeout = 5 * time.Second

type Observer interface{ Reconciliation(string) }

type Service struct {
	Repository receipt.ReconciliationRepository
	Authority  receipt.EffectAuthority
	Barrier    func(context.Context) error
	Observer   Observer
	Interval   time.Duration
	Batch      int
}

func (s *Service) Run(ctx context.Context) error {
	if s.Repository == nil || s.Authority == nil || s.Barrier == nil || s.Interval < 5*time.Second || s.Interval > 5*time.Minute || s.Batch < 1 || s.Batch > 64 {
		return errs.Invalid
	}
	ticker := time.NewTicker(s.Interval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return nil
		}
		cycle, cancel := context.WithTimeout(ctx, CycleTimeout)
		if err := s.Barrier(cycle); err == nil && cycle.Err() == nil {
			if err := s.Cycle(cycle); err != nil {
				s.observe("error")
			}
		} else {
			s.observe("barrier")
		}
		cancel()
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (s *Service) Cycle(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, CycleTimeout)
	defer cancel()
	pending, err := s.Repository.Pending(ctx, s.Batch)
	if err != nil {
		return err
	}
	if len(pending) > s.Batch {
		return errs.Unavailable
	}
	for _, p := range pending {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		claimed, err := s.Repository.ClaimReconciliation(ctx, p, s.Interval)
		if err != nil {
			return err
		}
		if !claimed {
			continue
		}
		if !p.Valid() {
			s.observe("invalid")
			continue
		}
		d, err := s.Authority.Reconcile(ctx, p.Owner, "")
		if err != nil {
			s.failure(err)
			continue
		}
		if !d.ValidFor(p, time.Now()) {
			s.observe("invalid")
			continue
		}
		// Выбор решения не заменяет свежую авторизацию непосредственно перед commit.
		fresh, err := s.Authority.Reconcile(ctx, p.Owner, d.Ref)
		if err != nil {
			s.failure(err)
			continue
		}
		if fresh != d || !fresh.ValidFor(p, time.Now()) {
			s.observe("invalid")
			continue
		}
		changed, err := s.Repository.CommitReconciliation(ctx, p, fresh)
		if err != nil {
			s.failure(err)
			continue
		}
		if changed {
			s.observe("committed")
		} else {
			s.observe("replay")
		}
	}
	return nil
}

func (s *Service) observe(outcome string) {
	if s.Observer != nil {
		s.Observer.Reconciliation(outcome)
	}
}

func (s *Service) failure(err error) {
	switch {
	case errors.Is(err, errs.NotFound):
		s.observe("none")
	case errors.Is(err, errs.Denied):
		s.observe("denied")
	default:
		s.observe("error")
	}
}
