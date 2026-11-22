package platform

import (
	"context"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/access"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// requireAccessUntil uses the same authoritative target, subject, bindings and allow-only evaluator as requireAccess, additionally returning the current permission's disclosure deadline. A zero deadline means one currently winning alternative has no end; it is not a transferable grant or a promise against later revocation.
// requireAccessUntil использует те же authoritative target, subject, bindings и allow-only evaluator, что requireAccess, дополнительно возвращая disclosure deadline текущего permission. Zero deadline означает отсутствие конца у одной currently winning alternative; это не transferable grant либо обещание против позднего revoke.
func (repository *Repository) requireAccessUntil(ctx context.Context, tx pgx.Tx, current scope, permission string, target any) (time.Time, error) {
	var resolved resolvedAccessTarget
	switch value := target.(type) {
	case entity.AccessScope:
		if value.Kind == "RESOURCE_KIND" {
			if err := access.ValidateScope(value); err != nil {
				return time.Time{}, errs.ErrInvalid
			}
			resolved.scope = value
		} else {
			var err error
			resolved, err = repository.resolveAccessTarget(ctx, tx, current.organizationID, value)
			if err != nil {
				return time.Time{}, err
			}
		}
	case resolvedAccessTarget:
		resolved = value
	default:
		return time.Time{}, errs.ErrInvalid
	}
	if resolved.scope.ResourceKind == "ARTIFACT" && resolved.resourceID != "" {
		allowed, err := kubernetesCaptureArtifactAccess(ctx, tx, current, permission, resolved.resourceID)
		if err != nil {
			return time.Time{}, err
		}
		if !allowed {
			return time.Time{}, errs.ErrNotFound
		}
	}
	if current.authorityProjectID != "" && resolved.projectID != "" && current.authorityProjectID != resolved.projectID {
		return time.Time{}, errs.ErrNotFound
	}
	subject, err := repository.resolveAccessSubject(ctx, tx, current.organizationID, current.actorRef)
	if err != nil {
		return time.Time{}, err
	}
	bindings, err := repository.loadAccessBindings(ctx, tx, current.organizationID, subject)
	if err != nil {
		return time.Time{}, err
	}
	evaluated := time.Now()
	decision := access.Evaluate(subject.AccessSubject, permission, resolved.scope, resolved.ownerSubjectRef, bindings, evaluated.UTC())
	if !decision.Allowed {
		return time.Time{}, errs.ErrNotFound
	}
	return accessDisclosureDecisionUntil(decision, bindings, evaluated)
}

// accessDisclosureDecisionUntil computes the latest end among currently winning alternatives only. Future-starting bindings never extend a present decision; each required permission must be checked separately and callers take the earliest nonzero bound with their own lease/window.
// accessDisclosureDecisionUntil вычисляет наиболее поздний конец лишь currently winning alternatives. Future-starting bindings не продлевают present decision; каждый required permission проверяется отдельно, caller выбирает earliest nonzero bound вместе со своей lease/window.
func accessDisclosureDecisionUntil(decision entity.EffectiveAccessDecision, bindings []entity.AccessBinding, evaluated time.Time) (time.Time, error) {
	if !decision.Allowed {
		return time.Time{}, errs.ErrNotFound
	}
	winning := make(map[string]bool)
	for _, step := range decision.Explanation {
		winning[step.BindingRef] = false
	}
	if len(winning) == 0 || evaluated.IsZero() {
		return time.Time{}, errs.ErrUnavailable
	}
	var latest time.Time
	unbounded := false
	for _, binding := range bindings {
		if _, ok := winning[binding.Ref]; !ok {
			continue
		}
		winning[binding.Ref] = true
		if binding.Conditions.ValidFrom != nil && evaluated.Before(*binding.Conditions.ValidFrom) {
			return time.Time{}, errs.ErrUnavailable
		}
		if binding.Conditions.ValidUntil == nil {
			unbounded = true
			continue
		}
		if !evaluated.Before(*binding.Conditions.ValidUntil) {
			return time.Time{}, errs.ErrUnavailable
		}
		// Reattach the duration to the evaluation's monotonic clock; the stored wall timestamp never renews the allowance after a clock adjustment.
		// Привязываем duration к monotonic clock evaluation; stored wall timestamp не продлевает allowance после clock adjustment.
		until := evaluated.Add(binding.Conditions.ValidUntil.Sub(evaluated))
		if until.After(latest) {
			latest = until
		}
	}
	for _, found := range winning {
		if !found {
			return time.Time{}, errs.ErrUnavailable
		}
	}
	if unbounded {
		return time.Time{}, nil
	}
	if latest.IsZero() {
		return time.Time{}, errs.ErrUnavailable
	}
	return latest, nil
}

// earlierAccessDeadline preserves an earlier finite allowance across fresh alternatives;
// a restored unbounded binding never renews a command that already waited past its first bound.
// earlierAccessDeadline сохраняет ранний finite allowance при fresh alternatives;
// восстановленный unbounded binding не продлевает command после истечения первого bound.
func earlierAccessDeadline(first, next time.Time) time.Time {
	if first.IsZero() || !next.IsZero() && next.Before(first) {
		return next
	}
	return first
}

// accessDeadlineExpired uses the evaluator's monotonic deadline; zero is unbounded.
// accessDeadlineExpired использует monotonic deadline evaluator; zero означает unbounded.
func accessDeadlineExpired(until time.Time) bool {
	return !until.IsZero() && !time.Now().Before(until)
}

// nullableAccessDeadline retains an unbounded permission as SQL NULL without inventing a finite lease.
// nullableAccessDeadline хранит unbounded permission как SQL NULL, не выдумывая finite lease.
func nullableAccessDeadline(until time.Time) any {
	if until.IsZero() {
		return nil
	}
	return until
}
