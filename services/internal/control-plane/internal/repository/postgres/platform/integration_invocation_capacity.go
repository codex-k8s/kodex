package platform

import (
	"context"
	_ "embed"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// queryManagedIntegrationDeliveryContract checks the installed schema independently of optional target configuration.
// queryManagedIntegrationDeliveryContract проверяет installed schema независимо от optional target configuration.
//
//go:embed sql/integration_invocation__delivery_contract.sql
var queryManagedIntegrationDeliveryContract string

// ClaimIntegrationInvocationsWithCapacity commits only leases with matching pre-reserved purpose slots.
// It reuses canonical disjoint selectors in one transaction; unsupported SSH_TARGET fails before any write.
// ClaimIntegrationInvocationsWithCapacity фиксирует лишь leases с matching pre-reserved purpose slots.
// Использует canonical disjoint selectors в одной транзакции; unsupported SSH_TARGET закрыт до writes.
func (repository *Repository) ClaimIntegrationInvocationsWithCapacity(ctx context.Context, p value.Principal, instance string, limit int32, capacity value.IntegrationInvocationDeliveryCapacity) ([]map[string]any, error) {
	if p.CallerWorkload != "integration-gateway" || p.Permission != "platform.runtime.integrations.claim" || p.ProjectRef != "" {
		return nil, errs.ErrForbidden
	}
	if !managedkubernetes.ValidLocator(instance) || !capacity.Valid(limit) {
		return nil, errs.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// Purpose readiness is checked before any owner lease write; it never remints actor authority.
	// Purpose readiness проверяется до любой записи owner lease; actor authority не переназначается.
	if capacity.SSH != 0 {
		ready, err := repository.sshExecutionDeliveryReady(ctx)
		if err != nil {
			return nil, err
		}
		if !ready {
			return nil, errs.ErrUnavailable
		}
	}
	if capacity.Ordinary+capacity.Kubernetes+capacity.SSH == 0 {
		return []map[string]any{}, nil
	}
	current, tx, route, err := repository.beginIntegrationClaimTransaction(ctx, p)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	claims := make([]map[string]any, 0, limit)
	// Source/cursor precede Connection locks. Ordinary connections are disjoint from the fixed BI packages.
	// Source/cursor предшествуют Connection locks. Ordinary connections отделены от fixed BI packages.
	if capacity.Kubernetes != 0 {
		selected, err := repository.claimManagedKubernetesInvocationsTx(ctx, tx, current, p, instance, int32(capacity.Kubernetes), route)
		if err != nil {
			return nil, err
		}
		claims = append(claims, selected...)
	}
	if capacity.SSH != 0 {
		selected, err := repository.claimManagedSSHInvocationsTx(ctx, tx, current, p, instance, int32(capacity.SSH), route)
		if err != nil {
			return nil, err
		}
		claims = append(claims, selected...)
	}
	if capacity.Ordinary != 0 {
		selected, err := repository.claimOrdinaryIntegrationInvocationsTx(ctx, tx, current, p, instance, int32(capacity.Ordinary), route, false)
		if err != nil {
			return nil, err
		}
		claims = append(claims, selected...)
	}
	if tx.Commit(ctx) != nil {
		return nil, errs.ErrUnavailable
	}
	return claims, nil
}

// ManagedIntegrationInvocationDeliveryContract reports code/schema support, never optional purpose availability.
// Exact revision one requires the actual capacity, original binding, finalization and metadata readback schema.
// ManagedIntegrationInvocationDeliveryContract сообщает code/schema support, не optional purpose availability.
// Exact revision one требует actual capacity/original binding/finalization/metadata readback schema.
func (repository *Repository) ManagedIntegrationInvocationDeliveryContract(ctx context.Context, p value.Principal) (uint32, error) {
	broker := p.CallerWorkload == "secret-broker" && p.Permission == "platform.runtime.kubernetes.readiness"
	gateway := p.CallerWorkload == "integration-gateway" && p.Permission == "platform.runtime.integrations.delivery.check"
	if p.ProjectRef != "" || !broker && !gateway {
		return 0, errs.ErrForbidden
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := repository.resolveScope(ctx, p); err != nil {
		return 0, err
	}
	var ready bool
	if repository.pool.QueryRow(ctx, queryManagedIntegrationDeliveryContract).Scan(&ready) != nil {
		return 0, errs.ErrUnavailable
	}
	if !ready {
		return 0, nil
	}
	return 1, nil
}
