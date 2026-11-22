package app

import (
	"context"
	"errors"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integration"
)

// rejectKubernetesPortForwardPreflight asks the existing owner to finalize its independently derived no-effect profile failure.
// Lost completion ACK permits one exact metadata read, never another dispatch or completion mutation.
// rejectKubernetesPortForwardPreflight просит existing owner завершить independently derived no-effect profile failure.
// Lost completion ACK допускает один exact metadata read, не новый dispatch либо completion mutation.
func rejectKubernetesPortForwardPreflight(ctx context.Context, binding *sb.ManagedKubernetesInvocationBinding, owner cp.ManagedKubernetesOwnerServiceClient) error {
	observed, err := integration.ReadKubernetesInvocation(ctx, binding, owner)
	if err != nil {
		return err
	}
	if observed.State == cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_FAILED && observed.CompletionRecorded {
		return nil
	}
	_, err = integration.RejectKubernetesPortForwardPreflight(ctx, binding, observed, owner)
	if err == nil {
		return nil
	}
	var safe *integration.SafeError
	if !errors.As(err, &safe) || safe.Code != "INTEGRATION_UNAVAILABLE" || ctx.Err() != nil {
		return err
	}
	final, readErr := integration.ReadKubernetesInvocation(ctx, binding, owner)
	if readErr != nil {
		return errors.Join(err, readErr)
	}
	if final.State != cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_FAILED || !final.CompletionRecorded || final.PlanRef != observed.PlanRef || final.PlanRevision != observed.PlanRevision || final.PlanDigest != observed.PlanDigest || final.SnapshotDigest != observed.SnapshotDigest {
		return errKubernetesActivity
	}
	return nil
}
