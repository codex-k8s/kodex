package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// kubernetesCaptureServiceFixture observes production-map forwarding without pretending to own SQL.
// kubernetesCaptureServiceFixture проверяет production-map forwarding без имитации owner SQL.
type kubernetesCaptureServiceFixture struct {
	*managedKubernetesRepositoryFixture
	forwarded int
}

// FinalizeManagedKubernetesCapture records one artifact-only forwarding without creating output.
// FinalizeManagedKubernetesCapture фиксирует artifact-only forwarding без создания output.
func (fixture *kubernetesCaptureServiceFixture) FinalizeManagedKubernetesCapture(context.Context, value.Principal, value.ManagedKubernetesCaptureTaskBinding) (entity.ManagedKubernetesCaptureReceipt, error) {
	fixture.forwarded++
	return entity.ManagedKubernetesCaptureReceipt{}, nil
}

// GetManagedKubernetesCapture records metadata forwarding without a body or grant.
// GetManagedKubernetesCapture фиксирует metadata forwarding без body либо grant.
func (fixture *kubernetesCaptureServiceFixture) GetManagedKubernetesCapture(context.Context, value.Principal, value.ManagedKubernetesCaptureTaskBinding) (entity.ManagedKubernetesCaptureReceipt, error) {
	fixture.forwarded++
	return entity.ManagedKubernetesCaptureReceipt{}, nil
}

// RenewManagedKubernetesCapture records forwarding without asserting a renewed real lease.
// RenewManagedKubernetesCapture фиксирует forwarding без утверждения renewed real lease.
func (fixture *kubernetesCaptureServiceFixture) RenewManagedKubernetesCapture(context.Context, value.Principal, value.ManagedKubernetesCaptureTaskBinding) (time.Time, error) {
	fixture.forwarded++
	return time.Time{}, nil
}

// CheckManagedKubernetesCaptureReadiness keeps this fixture unready despite valid routing.
// CheckManagedKubernetesCaptureReadiness сохраняет fixture unready при корректном routing.
func (fixture *kubernetesCaptureServiceFixture) CheckManagedKubernetesCaptureReadiness(context.Context, value.Principal) (uint32, error) {
	fixture.forwarded++
	return 0, nil
}

// TestKubernetesCaptureServiceUsesFixedProductionMap verifies registry-to-domain forwarding and
// denies technical peers from unrelated purposes before repository use. It proves no issuer or SQL.
// TestKubernetesCaptureServiceUsesFixedProductionMap проверяет registry-to-domain forwarding и
// запрещает peers другого purpose до repository. Не доказывает issuer либо SQL.
func TestKubernetesCaptureServiceUsesFixedProductionMap(t *testing.T) {
	_, base, p, _ := managedKubernetesServiceFixture(t, "unused")
	fixture := &kubernetesCaptureServiceFixture{managedKubernetesRepositoryFixture: base}
	service, err := New(fixture)
	if err != nil {
		t.Fatal(err)
	}
	p.CallerWorkload = "artifact-retention"
	binding := value.ManagedKubernetesCaptureTaskBinding{CaptureRef: "koc_original_capture", LeaseRef: "kca_original_attempt", Fence: strings.Repeat("fixture-private-", 4), Generation: 1, WorkloadInstance: "capture-worker", SnapshotDigest: strings.Repeat("a", 64)}
	for operation, method := range controlplaneclient.ArtifactRetentionCaptureOperations() {
		p.Permission = operation
		switch method {
		case cp.ManagedKubernetesOwnerService_FinalizeManagedKubernetesCapture_FullMethodName:
			_, err = service.FinalizeManagedKubernetesCapture(t.Context(), p, binding)
		case cp.ManagedKubernetesOwnerService_GetManagedKubernetesCapture_FullMethodName:
			_, err = service.GetManagedKubernetesCapture(t.Context(), p, binding)
		case cp.ManagedKubernetesOwnerService_RenewManagedKubernetesCapture_FullMethodName:
			_, err = service.RenewManagedKubernetesCapture(t.Context(), p, binding)
		case cp.ManagedKubernetesOwnerService_CheckManagedKubernetesCaptureReadiness_FullMethodName:
			_, err = service.CheckManagedKubernetesCaptureReadiness(t.Context(), p)
		default:
			t.Fatal("unexpected capture operation")
		}
		if err != nil {
			t.Fatal("production capture operation refused by service", err)
		}
	}
	if fixture.forwarded != 4 {
		t.Fatal("capture methods did not reach domain ports")
	}
	for _, caller := range []string{"control-api-gateway", "integration-gateway", "secret-broker", "runtime-controller"} {
		p.CallerWorkload = caller
		for operation, method := range controlplaneclient.ArtifactRetentionCaptureOperations() {
			if method == cp.ManagedKubernetesOwnerService_GetManagedKubernetesCapture_FullMethodName {
				p.Permission = operation
			}
		}
		if _, err := service.GetManagedKubernetesCapture(t.Context(), p, binding); !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("unrelated workload entered capture purpose", err)
		}
	}
	p.CallerWorkload = "artifact-retention"
	binding.Fence = ""
	if _, err := service.GetManagedKubernetesCapture(t.Context(), p, binding); !errors.Is(err, errs.ErrInvalid) || fixture.forwarded != 4 {
		t.Fatal("malformed capture binding reached repository", err)
	}
}
