package app

import (
	"context"
	"errors"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integration"
)

// closeKubernetesActivity closes only the original owner graph. A lost acknowledgement permits one strict terminal metadata read; nil proves neither target process termination nor broker transport join.
// closeKubernetesActivity закрывает только original owner graph. Lost acknowledgement допускает один strict terminal metadata read; nil не доказывает target process termination либо broker transport join.
func closeKubernetesActivity(ctx context.Context, binding *sb.ManagedKubernetesInvocationBinding, owner cp.ManagedKubernetesOwnerServiceClient) error {
	_, err := integration.CancelKubernetesInvocation(ctx, binding, owner)
	if err == nil {
		return nil
	}
	var unavailable *integration.SafeError
	if !errors.As(err, &unavailable) || unavailable.Code != "INTEGRATION_UNAVAILABLE" || ctx.Err() != nil {
		return err
	}
	receipt, readErr := integration.ReadKubernetesInvocation(ctx, binding, owner)
	if readErr != nil {
		return errors.Join(err, readErr)
	}
	if receipt.State == cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_RUNNING {
		return errKubernetesActivity
	}
	return nil
}

// cleanupKubernetesActivity owns one independent total cleanup budget after business cancellation. A stream's mandatory closure callback calls closeKubernetesActivity before broker cancellation; all steps consume the same remaining deadline. Only an explicit broker join permits nil for a stream.
// cleanupKubernetesActivity владеет одним independent total cleanup budget после business cancellation. Обязательный closure callback stream вызывает closeKubernetesActivity до broker cancellation; все шаги расходуют один оставшийся deadline. Для stream nil допускается лишь после explicit broker join.
func cleanupKubernetesActivity(ctx context.Context, binding *sb.ManagedKubernetesInvocationBinding, owner cp.ManagedKubernetesOwnerServiceClient, stream *integration.KubernetesStreamConsumer, budget time.Duration) error {
	if ctx == nil || binding == nil || owner == nil || budget <= 0 || budget > time.Minute {
		return errKubernetesActivity
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), budget)
	defer cancel()
	if stream == nil {
		return closeKubernetesActivity(cleanup, binding, owner)
	}
	result, err := stream.Cancel(cleanup)
	if err != nil {
		return err
	}
	if !result.TransportJoined {
		return errKubernetesActivity
	}
	return nil
}
