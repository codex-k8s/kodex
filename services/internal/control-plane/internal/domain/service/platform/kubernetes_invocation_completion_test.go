package platform

import (
	"context"
	"errors"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// kubernetesInvocationGatewayFixture records service forwarding without modeling owner SQL authority.
// It intentionally issues no lease or result and receives operation names only from the production map.
// kubernetesInvocationGatewayFixture фиксирует service forwarding, не моделируя owner SQL authority.
// Не выдаёт lease/result и получает operation names лишь из production map.
type kubernetesInvocationGatewayFixture struct {
	*managedKubernetesRepositoryFixture
	forwarded   int
	revision    uint32
	deliveryErr error
}

// ClaimManagedKubernetesInvocations keeps the legacy port shape without creating fixture work.
// It is never the mixed runtime consumer path under test.
// ClaimManagedKubernetesInvocations сохраняет legacy port shape без создания fixture work.
// Не является mixed runtime consumer path этой проверки.
func (*kubernetesInvocationGatewayFixture) ClaimManagedKubernetesInvocations(context.Context, value.Principal, string, int32) ([]map[string]any, error) {
	return nil, errs.ErrUnavailable
}

// RenewManagedKubernetesInvocation records protected forwarding without renewing real work.
// RenewManagedKubernetesInvocation фиксирует protected forwarding без real work renewal.
func (repository *kubernetesInvocationGatewayFixture) RenewManagedKubernetesInvocation(context.Context, value.Principal, value.ManagedKubernetesBinding) (entity.ManagedKubernetesInvocationLease, error) {
	repository.forwarded++
	return entity.ManagedKubernetesInvocationLease{}, nil
}

// GetManagedKubernetesInvocation records metadata-only forwarding without returning target data.
// GetManagedKubernetesInvocation фиксирует metadata-only forwarding без target data.
func (repository *kubernetesInvocationGatewayFixture) GetManagedKubernetesInvocation(context.Context, value.Principal, value.ManagedKubernetesBinding) (entity.ManagedKubernetesInvocationReceipt, error) {
	repository.forwarded++
	return entity.ManagedKubernetesInvocationReceipt{}, nil
}

// CompleteManagedKubernetesInvocation records grammar admission without asserting graph finalization.
// CompleteManagedKubernetesInvocation фиксирует grammar admission без утверждения graph finalization.
func (repository *kubernetesInvocationGatewayFixture) CompleteManagedKubernetesInvocation(context.Context, value.Principal, value.ManagedKubernetesBinding, command.ManagedKubernetesCompleteInvocation) (entity.ManagedKubernetesInvocationReceipt, error) {
	repository.forwarded++
	return entity.ManagedKubernetesInvocationReceipt{}, nil
}

// CancelManagedKubernetesInvocation records only exact graph-stop forwarding without inventing remote outcomes.
// CancelManagedKubernetesInvocation фиксирует лишь exact graph-stop forwarding без выдуманного remote outcome.
func (repository *kubernetesInvocationGatewayFixture) CancelManagedKubernetesInvocation(context.Context, value.Principal, value.ManagedKubernetesBinding) (entity.ManagedKubernetesInvocationReceipt, error) {
	repository.forwarded++
	return entity.ManagedKubernetesInvocationReceipt{}, nil
}

// TestKubernetesInvocationGatewayUsesProductionOperations prevents fixtures from inventing operation names.
// Wrong callers, health variants and partial plan tuples stop before repository forwarding.
// TestKubernetesInvocationGatewayUsesProductionOperations запрещает fixtures выдумывать operation names.
// Wrong callers, health variants и partial plan tuples отклоняются до repository forwarding.
func TestKubernetesInvocationGatewayUsesProductionOperations(t *testing.T) {
	_, base, p, binding := managedKubernetesServiceFixture(t, "unused")
	repository := &kubernetesInvocationGatewayFixture{managedKubernetesRepositoryFixture: base}
	service, err := New(repository)
	if err != nil {
		t.Fatal("create gateway owner service")
	}
	p.CallerWorkload = "integration-gateway"
	for _, method := range []string{cp.ManagedKubernetesOwnerService_RenewManagedKubernetesInvocation_FullMethodName, cp.ManagedKubernetesOwnerService_GetManagedKubernetesInvocation_FullMethodName, cp.ManagedKubernetesOwnerService_CompleteManagedKubernetesInvocation_FullMethodName, cp.ManagedKubernetesOwnerService_CancelManagedKubernetesInvocation_FullMethodName} {
		p.Permission = ""
		for operation, registered := range controlplaneclient.IntegrationGatewayOperations() {
			if registered == method {
				if p.Permission != "" {
					t.Fatal("duplicate gateway operation")
				}
				p.Permission = operation
			}
		}
		if p.Permission == "" {
			t.Fatal("purpose method missing from production gateway map")
		}
		switch method {
		case cp.ManagedKubernetesOwnerService_RenewManagedKubernetesInvocation_FullMethodName:
			_, err = service.RenewManagedKubernetesInvocation(t.Context(), p, binding)
		case cp.ManagedKubernetesOwnerService_GetManagedKubernetesInvocation_FullMethodName:
			_, err = service.GetManagedKubernetesInvocation(t.Context(), p, binding)
		case cp.ManagedKubernetesOwnerService_CancelManagedKubernetesInvocation_FullMethodName:
			_, err = service.CancelManagedKubernetesInvocation(t.Context(), p, binding)
		default:
			_, err = service.CompleteManagedKubernetesInvocation(t.Context(), p, binding, command.ManagedKubernetesCompleteInvocation{})
		}
		if err != nil {
			t.Fatal("production operation refused by domain service", method, err)
		}
	}
	if repository.forwarded != 4 {
		t.Fatal("production registry did not reach every domain port")
	}
	p.Permission = "platform.runtime.kubernetes.read"
	p.CallerWorkload = "secret-broker"
	if _, err := service.GetManagedKubernetesInvocation(t.Context(), p, binding); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("broker assumed gateway completion authority")
	}
	p.CallerWorkload = "integration-gateway"
	binding.InvocationRef = ""
	binding.ConnectionTestRef = "ict_health"
	if _, err := service.GetManagedKubernetesInvocation(t.Context(), p, binding); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("health binding became invocation readback")
	}
	if repository.forwarded != 4 {
		t.Fatal("invalid variant reached owner")
	}
}

// ManagedIntegrationInvocationDeliveryContract returns only a synthetic code/schema capability fact.
// It deliberately does not provide optional Kubernetes target readiness or runtime authority.
// ManagedIntegrationInvocationDeliveryContract возвращает лишь synthetic code/schema capability fact.
// Не выдаёт optional Kubernetes target readiness либо runtime authority.
func (repository *kubernetesInvocationGatewayFixture) ManagedIntegrationInvocationDeliveryContract(context.Context, value.Principal) (uint32, error) {
	return repository.revision, repository.deliveryErr
}

// TestKubernetesDeliveryContractIsIndependentAndFailClosed exercises production operation mapping.
// Unknown revisions and missing support stay zero, while infrastructure errors are not readiness.
// TestKubernetesDeliveryContractIsIndependentAndFailClosed проверяет production operation mapping.
// Unknown revisions/missing support остаются zero; infrastructure errors не становятся readiness.
func TestKubernetesDeliveryContractIsIndependentAndFailClosed(t *testing.T) {
	_, base, p, _ := managedKubernetesServiceFixture(t, "unused")
	fixture := &kubernetesInvocationGatewayFixture{managedKubernetesRepositoryFixture: base, revision: 1}
	service, err := New(fixture)
	if err != nil {
		t.Fatal("create delivery contract fixture")
	}
	p.CallerWorkload = "integration-gateway"
	p.Permission = ""
	for operation, method := range controlplaneclient.IntegrationGatewayOperations() {
		if method == cp.ManagedKubernetesOwnerService_CheckManagedKubernetesOwnerReadiness_FullMethodName {
			if p.Permission != "" {
				t.Fatal("duplicate common delivery operation")
			}
			p.Permission = operation
		}
	}
	if p.Permission == "" {
		t.Fatal("common delivery probe missing from production gateway map")
	}
	if revision, err := service.CheckManagedKubernetesDeliveryContract(t.Context(), p); err != nil || revision != 1 {
		t.Fatal("common contract required an optional target", err)
	}
	if _, err := service.CheckManagedKubernetesOwnerReadiness(t.Context(), p); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("gateway common probe assumed broker target readiness authority")
	}
	fixture.revision = 2
	if revision, err := service.CheckManagedKubernetesDeliveryContract(t.Context(), p); err != nil || revision != 0 {
		t.Fatal("unknown contract revision became supported")
	}
	fixture.deliveryErr = errs.ErrUnavailable
	if _, err := service.CheckManagedKubernetesDeliveryContract(t.Context(), p); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("infrastructure failure became missing feature readiness")
	}
	unsupported, err := New(base)
	if err != nil {
		t.Fatal("create unsupported delivery fixture")
	}
	if revision, err := unsupported.CheckManagedKubernetesDeliveryContract(t.Context(), p); err != nil || revision != 0 {
		t.Fatal("missing delivery port became supported")
	}
}
