package reconciliation

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/repository/receipt"
)

// CycleTimeout bounds each polling/reconciliation cycle independently of the worker's configured polling interval.
// CycleTimeout ограничивает каждый цикл опроса и сверки независимо от заданного интервала опроса worker.
const CycleTimeout = 5 * time.Second

// Observer records technical reconciliation outcomes rather than mailbox contents or decision identifiers.
// Observer учитывает технические исходы сверки, не содержимое ящика или идентификаторы решений.
type Observer interface {
	// Reconciliation observes one of the service's fixed outcome strings; adapters must retain bounded metric labels.
	// Reconciliation наблюдает одну из фиксированных строк исхода сервиса; адаптер обязан сохранять ограниченные метки метрик.
	Reconciliation(string)
}

// Service runs owner-report recovery and unknown-effect reconciliation without a mail transport or resend capability.
// Dependencies and cancellation belong to the composition root; the service does not create its own root context.
// Service восстанавливает отчёты владельцу и сверяет неопределённые эффекты без почтового транспорта или возможности повторной отправки.
// Зависимости и отмена принадлежат composition root; сервис не создаёт собственный корневой контекст.
type Service struct {
	Repository receipt.ReconciliationRepository
	Reports    receipt.ReportRepository
	Authority  receipt.EffectAuthority
	Barrier    func(context.Context) error
	Observer   Observer
	Interval   time.Duration
	Batch      int
}

// Run polls reconciliation behind the startup barrier and returns nil after cooperative cancellation and cycle completion.
// Run опрашивает сверку за startup barrier и возвращает nil после совместной отмены и завершения текущего цикла.
func (s *Service) Run(ctx context.Context) error {
	return s.run(ctx, s.Cycle)
}

// RunReports requires a report journal, then uses the same bounded worker loop for report recovery.
// RunReports требует журнал отчётов, затем использует тот же ограниченный цикл worker для их восстановления.
func (s *Service) RunReports(ctx context.Context) error {
	if s.Reports == nil {
		return errs.Invalid
	}
	return s.run(ctx, s.ReportCycle)
}

// run validates dependencies and polling bounds, checks the barrier on every cycle, and observes failures without terminating polling.
// Cancellation waits on context-aware dependencies already in progress; it does not forcibly interrupt a non-cooperative callback.
// run проверяет зависимости и границы опроса, проходит barrier в каждом цикле и учитывает ошибки без завершения опроса.
// Отмена ожидает уже исполняющиеся зависимости, поддерживающие контекст; несотрудничающий callback принудительно не прерывается.
func (s *Service) run(ctx context.Context, cycleWork func(context.Context) error) error {
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
			if err := cycleWork(cycle); err != nil {
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

// Cycle claims a bounded pending batch and resolves each conclusive owner decision twice, including an exact fresh recheck before commit.
// Per-item failures preserve earlier progress and remain observable; batch/claim failures terminate the cycle without resending mail.
// Cycle захватывает ограниченную пачку и дважды разрешает каждое определённое решение владельца, включая точную свежую проверку до commit.
// Ошибки отдельных элементов сохраняют прежний прогресс и учитываются; сбои пачки или claim завершают цикл без повторной отправки писем.
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

// observe forwards a service-owned outcome when an observer is configured and otherwise performs no action.
// observe передаёт заданный сервисом исход настроенному observer, а при его отсутствии ничего не делает.
func (s *Service) observe(outcome string) {
	if s.Observer != nil {
		s.Observer.Reconciliation(outcome)
	}
}

// failure maps wrapped not-found, denied and all other failures to fixed none/denied/error outcomes without exposing details.
// failure отображает обёрнутые not-found, denied и прочие ошибки в фиксированные none/denied/error без раскрытия подробностей.
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
