package app

import (
	"context"
	"errors"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integration"
)

// settleKubernetesActivity asks the existing owner to aggregate accepted child facts, never supplying a proposed outcome or replaying a provider effect. A RUNNING reply is usable only when the Complete RPC positively proves a known incomplete plan; uncertain completion permits one metadata read, not another Complete.
// settleKubernetesActivity просит существующего владельца собрать принятые результаты children без предложенного outcome либо повтора provider effect. RUNNING пригоден только после положительного Complete о known incomplete plan; uncertain completion допускает один metadata read, а не повтор Complete.
func settleKubernetesActivity(ctx context.Context, binding *sb.ManagedKubernetesInvocationBinding, owner cp.ManagedKubernetesOwnerServiceClient) (*cp.ManagedKubernetesInvocationReceipt, error) {
	return settleKubernetesActivityWithPlan(ctx, binding, owner, nil)
}

// settleKubernetesActivityWithPlan preserves the original plan across capture-only polling and
// rejects a changed metadata read before issuing another owner Complete. No external effect is replayed.
// settleKubernetesActivityWithPlan сохраняет original plan при capture-only polling и отклоняет
// changed metadata read до следующего owner Complete. External effect не повторяется.
func settleKubernetesActivityWithPlan(ctx context.Context, binding *sb.ManagedKubernetesInvocationBinding, owner cp.ManagedKubernetesOwnerServiceClient, expected *cp.ManagedKubernetesInvocationReceipt) (*cp.ManagedKubernetesInvocationReceipt, error) {
	observed, err := integration.ReadKubernetesInvocation(ctx, binding, owner)
	if err != nil {
		return nil, err
	}
	if expected != nil && (observed.PlanRef != expected.PlanRef || observed.PlanRevision != expected.PlanRevision || observed.PlanDigest != expected.PlanDigest || observed.SnapshotDigest != expected.SnapshotDigest) {
		return nil, errKubernetesActivity
	}
	if observed.State != cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_RUNNING {
		return observed, nil
	}
	completed, err := integration.CompleteKubernetesInvocation(ctx, binding, observed, owner)
	if err == nil {
		return completed, nil
	}
	var unavailable *integration.SafeError
	if !errors.As(err, &unavailable) || unavailable.Code != "INTEGRATION_UNAVAILABLE" || ctx.Err() != nil {
		return nil, err
	}
	// Readback must retain the same immutable plan, and cannot turn a live state into permission to advance after a lost acknowledgement.
	// Readback сохраняет тот же immutable plan и не превращает live state в разрешение advance после потерянного acknowledgement.
	recovered, readErr := integration.ReadKubernetesInvocation(ctx, binding, owner)
	if readErr != nil {
		return nil, errors.Join(err, readErr)
	}
	if recovered.PlanRef != observed.PlanRef || recovered.PlanRevision != observed.PlanRevision || recovered.PlanDigest != observed.PlanDigest || recovered.SnapshotDigest != observed.SnapshotDigest || recovered.State == cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_RUNNING && recovered.CompletionPendingReason != cp.ManagedKubernetesCompletionPendingReason_MANAGED_KUBERNETES_COMPLETION_PENDING_REASON_CAPTURE_PENDING {
		return nil, errKubernetesActivity
	}
	return recovered, nil
}
