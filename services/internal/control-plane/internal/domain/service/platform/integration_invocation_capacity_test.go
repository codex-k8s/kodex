package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// integrationCapacityFixture captures the exact availability counts without issuing synthetic authority.
// integrationCapacityFixture сохраняет exact availability counts без synthetic authority.
type integrationCapacityFixture struct {
	*managedKubernetesRepositoryFixture
	seen  value.IntegrationInvocationDeliveryCapacity
	calls int
}

// ClaimIntegrationInvocationsWithCapacity records bounded forwarding without claiming a resource or lease.
// ClaimIntegrationInvocationsWithCapacity фиксирует bounded forwarding без resource либо lease claim.
func (fixture *integrationCapacityFixture) ClaimIntegrationInvocationsWithCapacity(_ context.Context, _ value.Principal, _ string, _ int32, capacity value.IntegrationInvocationDeliveryCapacity) ([]map[string]any, error) {
	fixture.calls++
	fixture.seen = capacity
	return nil, nil
}

// TestIntegrationInvocationCapacityNeverDowngrades prevents missing/invalid support from becoming an ordinary claim.
// TestIntegrationInvocationCapacityNeverDowngrades запрещает missing/invalid support превращаться в ordinary claim.
func TestIntegrationInvocationCapacityNeverDowngrades(t *testing.T) {
	service, base, p, _ := managedKubernetesServiceFixture(t, "unused")
	p.CallerWorkload = "integration-gateway"
	p.Permission = "platform.runtime.integrations.claim"
	capacity := value.IntegrationInvocationDeliveryCapacity{Ordinary: 1, Kubernetes: 1}
	if _, err := service.ClaimIntegrationInvocationsWithCapacity(t.Context(), p, "gateway-test", 2, capacity); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("missing capacity owner downgraded to legacy", err)
	}
	fixture := &integrationCapacityFixture{managedKubernetesRepositoryFixture: base}
	service, err := New(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.ClaimIntegrationInvocationsWithCapacity(t.Context(), p, "gateway-test", 2, capacity); err != nil || fixture.calls != 1 || fixture.seen != capacity {
		t.Fatal("capacity projection changed", err)
	}
	capacity.Kubernetes = 4
	if _, err = service.ClaimIntegrationInvocationsWithCapacity(t.Context(), p, "gateway-test", 2, capacity); !errors.Is(err, errs.ErrInvalid) || fixture.calls != 1 {
		t.Fatal("overcommit reached owner", err)
	}
	p.Permission = "platform.runtime.kubernetes.claim"
	if _, err = service.ClaimIntegrationInvocationsWithCapacity(t.Context(), p, "gateway-test", 8, capacity); !errors.Is(err, errs.ErrForbidden) || fixture.calls != 1 {
		t.Fatal("dedicated method became a second mixed claim", err)
	}
}
