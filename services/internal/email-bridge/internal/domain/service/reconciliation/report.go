package reconciliation

import (
	"context"
	"time"

	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/repository/receipt"
)

func (s *Service) ReportCycle(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, CycleTimeout)
	defer cancel()
	if s.Reports == nil {
		return errs.Invalid
	}
	pending, err := s.Reports.PendingReports(ctx, s.Batch)
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
		if !p.Valid() || p.Source.Binding.Lease.ExpiresAt.Add(receipt.ReportGrace).After(time.Now()) {
			s.observe("invalid")
			continue
		}
		claimed, err := s.Reports.ClaimReport(ctx, p, s.Interval)
		if err != nil {
			return err
		}
		if !claimed {
			continue
		}
		request := p.Report(true)
		owner, err := s.Authority.Report(ctx, request)
		if err != nil {
			s.failure(err)
			continue
		}
		want := request.Receipt
		want.Ref, want.Version = owner.Ref, owner.Version
		if owner.Ref == "" || owner.Version < 1 || !owner.Same(want) {
			s.observe("invalid")
			continue
		}
		if err := s.Repository.Remember(ctx, p.Scope, p.Record, owner, p.Source.Binding.Lease.ExpiresAt.Add(receipt.ReportGrace)); err != nil {
			s.failure(err)
			continue
		}
		if err := s.Reports.AcknowledgeReport(ctx, p); err != nil {
			s.failure(err)
			continue
		}
		s.observe("reported")
	}
	return nil
}
