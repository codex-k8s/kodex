package platform

import (
	"context"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
)

// TestKubernetesDeliveryOwnerComponent proves the common contract revision without any optional target/source.
// Normal purpose readiness remains false, and the real production operation map reaches the owner query.
// TestKubernetesDeliveryOwnerComponent доказывает common contract revision без optional target/source.
// Normal purpose readiness остаётся false; production operation map достигает owner query.
func TestKubernetesDeliveryOwnerComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	pool := newIsolatedAssistantComponentPool(t, ctx)
	defer pool.Close()
	repository := newSyntheticRuntimeComponentRepository(t, ctx, pool)
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	_, _, gateway := resolveIntegrationEffectTestPrincipals(t, ctx, repository)
	gateway.Permission = ""
	for operation, method := range controlplaneclient.IntegrationGatewayOperations() {
		if method == cp.ManagedKubernetesOwnerService_CheckManagedKubernetesOwnerReadiness_FullMethodName {
			gateway.Permission = operation
		}
	}
	revision, err := service.CheckManagedKubernetesDeliveryContract(ctx, gateway)
	if err != nil || revision != 1 {
		t.Fatal("common delivery depended on optional target", revision, err)
	}
	broker := gateway
	broker.CallerWorkload = "secret-broker"
	broker.Permission = "platform.runtime.kubernetes.readiness"
	ready, err := service.CheckManagedKubernetesOwnerReadiness(ctx, broker)
	if err != nil || ready {
		t.Fatal("missing optional target became purpose readiness", ready, err)
	}
}
