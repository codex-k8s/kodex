package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// identityReadinessRepository keeps the existing core optional ports and an explicit independent OUTPUT capability; it performs no SQL or resource action.
// identityReadinessRepository сохраняет existing core optional ports и независимую OUTPUT capability без SQL либо resource action.
type identityReadinessRepository struct {
	*managedKubernetesRepositoryFixture
	platformrepo.ManagedKubernetesCleanupRepository
	platformrepo.ManagedKubernetesInvocationCompletionRepository
	platformrepo.ManagedKubernetesInvocationCancellationRepository
	platformrepo.ManagedKubernetesInputRepository
	platformrepo.KubernetesStreamInputRepository
	platformrepo.KubernetesFileInputRepository
	platformrepo.KubernetesFileCleanupRepository
	ready                                         bool
	coreErr, errorDelivery                        error
	delivery, output, input, file                 uint32
	coreCalls, outputCalls, inputCalls, fileCalls int
}

// ManagedIntegrationInvocationDeliveryContract models only installed owner delivery support, preserving infrastructure errors.
// ManagedIntegrationInvocationDeliveryContract моделирует лишь installed owner delivery support с сохранением infrastructure errors.
func (repository *identityReadinessRepository) ManagedIntegrationInvocationDeliveryContract(context.Context, value.Principal) (uint32, error) {
	return repository.delivery, repository.errorDelivery
}

// CheckManagedKubernetesOwnerReadiness represents the unchanged repository operator/schema/source predicate and counts all actual core reads.
// CheckManagedKubernetesOwnerReadiness представляет unchanged repository operator/schema/source predicate и считает actual core reads.
func (repository *identityReadinessRepository) CheckManagedKubernetesOwnerReadiness(context.Context, value.Principal) (bool, error) {
	repository.coreCalls++
	return repository.ready, repository.coreErr
}

// ManagedKubernetesOutputContract is deliberately independent from core health readiness; absent OUTPUT remains revision zero.
// ManagedKubernetesOutputContract намеренно независим от core health readiness; отсутствующий OUTPUT остаётся revision0.
func (repository *identityReadinessRepository) ManagedKubernetesOutputContract(context.Context, value.Principal) (uint32, error) {
	repository.outputCalls++
	return repository.output, nil
}

// ManagedKubernetesInputContract models the independent private INPUT compatibility gate.
// ManagedKubernetesInputContract моделирует отдельную проверку совместимости закрытого INPUT.
func (repository *identityReadinessRepository) ManagedKubernetesInputContract(context.Context, value.Principal) (uint32, error) {
	repository.inputCalls++
	return repository.input, nil
}

// ManagedKubernetesFileInputContract models the independent FILE input compatibility gate.
// ManagedKubernetesFileInputContract моделирует отдельную проверку совместимости FILE input.
func (repository *identityReadinessRepository) ManagedKubernetesFileInputContract(context.Context, value.Principal) (uint32, error) {
	repository.fileCalls++
	return repository.file, nil
}

// TestKubernetesIdentityHealthReadinessPreservesCore proves actual domain entrypoints preserve exact broker/tenant authority, optional core ports, delivery support and every core false/error without requiring absent OUTPUT. Full execution requires exact OUTPUT, INPUT and FILE input revisions.
// TestKubernetesIdentityHealthReadinessPreservesCore проверяет actual domain entrypoints: exact broker/tenant authority, optional core ports, delivery support и каждый core false/error сохраняются без требования отсутствующего OUTPUT. Полное исполнение требует точных ревизий OUTPUT, INPUT и FILE input.
func TestKubernetesIdentityHealthReadinessPreservesCore(t *testing.T) {
	_, base, p, _ := managedKubernetesServiceFixture(t, "platform.runtime.kubernetes.readiness")
	repository := &identityReadinessRepository{managedKubernetesRepositoryFixture: base, ready: true, delivery: 1}
	service, err := New(repository)
	if err != nil {
		t.Fatal(err)
	}
	if ready, err := service.CheckManagedKubernetesIdentityHealthReadiness(t.Context(), p); err != nil || !ready || repository.coreCalls != 1 || repository.outputCalls != 0 || repository.inputCalls != 0 || repository.fileCalls != 0 {
		t.Fatal("identity health lost core checks or depended on OUTPUT", err)
	}
	if ready, err := service.CheckManagedKubernetesOwnerReadiness(t.Context(), p); err != nil || ready {
		t.Fatal("absent OUTPUT became execution readiness", err)
	}
	repository.output = managedkubernetes.OutputContractRevision
	if ready, err := service.CheckManagedKubernetesOwnerReadiness(t.Context(), p); err != nil || ready {
		t.Fatal("OUTPUT alone became execution readiness", err)
	}
	repository.input = managedkubernetes.InputContractRevision
	if ready, err := service.CheckManagedKubernetesOwnerReadiness(t.Context(), p); err != nil || ready {
		t.Fatal("missing FILE input became execution readiness", err)
	}
	repository.file = managedkubernetes.FileInputContractRevision
	if ready, err := service.CheckManagedKubernetesOwnerReadiness(t.Context(), p); err != nil || !ready {
		t.Fatal("qualified execution conjunction was rejected", err)
	}
	repository.ready = false
	if ready, err := service.CheckManagedKubernetesIdentityHealthReadiness(t.Context(), p); err != nil || ready {
		t.Fatal("core false was weakened by health profile", err)
	}
	repository.coreErr = errs.ErrUnavailable
	if _, err := service.CheckManagedKubernetesIdentityHealthReadiness(t.Context(), p); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("core infrastructure failure lost", err)
	}
	repository.coreErr = nil
	repository.ready = true
	for _, delivery := range []uint32{0, 2} {
		repository.delivery = delivery
		before := repository.coreCalls
		if _, err := service.CheckManagedKubernetesIdentityHealthReadiness(t.Context(), p); !errors.Is(err, errs.ErrUnavailable) || repository.coreCalls != before {
			t.Fatal("missing/future delivery support reached core owner", err)
		}
	}
	repository.delivery = 1
	for _, mutate := range []func(*value.Principal){func(p *value.Principal) {
		p.CallerWorkload = "integration-gateway"
		p.Permission = "platform.runtime.integrations.delivery.check"
	}, func(p *value.Principal) { p.ProjectRef = "project_foreign" }, func(p *value.Principal) { p.Permission = "platform.runtime.kubernetes.resolve" }} {
		changed := p
		mutate(&changed)
		before := repository.coreCalls
		if _, err := service.CheckManagedKubernetesIdentityHealthReadiness(t.Context(), changed); !errors.Is(err, errs.ErrForbidden) || repository.coreCalls != before {
			t.Fatal("wrong authority reached identity readiness owner", err)
		}
	}
	serviceWithoutPorts, err := New(base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := serviceWithoutPorts.CheckManagedKubernetesIdentityHealthReadiness(t.Context(), p); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("missing core ports became healthy", err)
	}
}
