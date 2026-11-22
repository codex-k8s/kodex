package kubernetesoperation

import (
	"bytes"
	"context"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// ResourceOperations joins owner planning, private native preparation and exact effect execution.
// It handles direct resource plans; expanded phases require the separate composite consumer.
// ResourceOperations соединяет owner planning, private native preparation и exact effect execution.
// Он обрабатывает direct resource plans; expanded phases требуют отдельного composite consumer.
type ResourceOperations struct {
	owner    targetrepo.ResourcePlanOwner
	native   targetrepo.ResourcePreparer
	executor *ResourceExecutor
}

// NewResourceOperations requires the full owner path and a private provider factory, with no fallback.
// NewResourceOperations требует полный owner path и private provider factory без fallback.
func NewResourceOperations(owner targetrepo.ResourcePlanOwner, native targetrepo.ResourcePreparer) (*ResourceOperations, error) {
	executor, err := NewResourceExecutor(owner, native)
	if err != nil || native == nil {
		return nil, targetrepo.ErrOperationUnavailable
	}
	return &ResourceOperations{owner: owner, native: native, executor: executor}, nil
}

// Execute resolves only server-owned input. Existing history is read before loading any credential;
// ATTEMPTING/terminal children use owner readback and never repeat discovery or native execution.
// New observations are pinned by Prepare before Begin, and every abandoned preparation is closed.
// Execute разрешает только server-owned input. Existing history читается до загрузки credentials;
// ATTEMPTING/terminal children используют owner readback без повторного discovery/native execution.
// Новые observations закрепляет Prepare до Begin; каждая abandoned preparation закрывается.
func (service *ResourceOperations) Execute(ctx context.Context, binding managedkubernetes.Binding) (targetrepo.ResourceExecution, error) {
	var empty targetrepo.ResourceExecution
	if service == nil || ctx == nil || ctx.Err() != nil || binding.Validate() != nil || binding.InvocationRef == "" {
		return empty, targetrepo.ErrOperationUnavailable
	}
	snapshot, err := service.resolve(ctx, binding)
	if err != nil {
		return empty, err
	}
	registered, known := integrationpackage.KubernetesOperationByKey(snapshot.Operation)
	metadata := integrationpackage.KubernetesMetadataRead(snapshot.Operation)
	if !known || (!metadata && (registered.Execution != integrationpackage.KubernetesResource || snapshot.Resource == nil)) || (metadata && (snapshot.Resource != nil || snapshot.Discovery != nil)) {
		return empty, targetrepo.ErrOperationUnavailable
	}
	var prepared targetrepo.ResourcePreparation
	defer func() {
		if prepared != nil {
			prepared.Close()
		}
	}()
	var candidates []managedkubernetes.ChildCandidate
	if snapshot.PlanRef == "" {
		prepared, err = service.native.PrepareKubernetesResource(ctx, snapshot)
		if err != nil || prepared == nil {
			return empty, targetrepo.ErrOperationUnavailable
		}
		pin, ready := prepared.Binding()
		if !ready || (!metadata && pin.Observation.Validate() != nil) || (metadata && pin.Observation != (managedkubernetes.ResourceObservation{})) || pin.Operation != snapshot.Operation || pin.InputDigest != resourceResultDigest(snapshot.NormalizedInput) || pin.TargetDigest != snapshot.Target.MetadataDigest() || pin.SubjectDigest != snapshot.SubjectDigest {
			return empty, targetrepo.ErrOperationUnavailable
		}
		if !metadata {
			observed := pin.Observation
			candidates = []managedkubernetes.ChildCandidate{{ObservationState: observed.State, Resource: observed.Resource, ObservedResourceDigest: observed.ObservedResourceDigest, RegisteredSchemaDigest: observed.RegisteredSchemaDigest, NamespaceUID: observed.NamespaceUID}}
		}
		// A gateway renewal can extend the same binding during discovery, but cannot change its input.
		// Gateway renewal может продлить тот же binding во время discovery, но не меняет его input.
		current, resolveErr := service.resolve(ctx, binding)
		originalPin, valid := streamOwnerReadPin(snapshot)
		currentPin, currentValid := streamOwnerReadPin(current)
		if resolveErr != nil || !valid || !currentValid || originalPin != currentPin {
			return empty, targetrepo.ErrOperationUnavailable
		}
		snapshot = current
		if snapshot.PlanRef != "" {
			candidates = nil
		}
	}
	planContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	plan, err := service.owner.PrepareKubernetesChildren(planContext, binding, snapshot, candidates)
	planErr := planContext.Err()
	cancel()
	if err != nil || planErr != nil || len(plan.Children) != 1 {
		return empty, targetrepo.ErrOperationUnavailable
	}
	child := plan.Children[0]
	if child.Operation != snapshot.Operation || !bytes.Equal(child.NormalizedInput, snapshot.NormalizedInput) {
		return empty, targetrepo.ErrOperationUnavailable
	}
	if _, valid := resourcePlanChild(plan, child.Ref); !valid {
		return empty, targetrepo.ErrOperationUnavailable
	}
	switch child.State {
	case "ATTEMPTING", "SUCCEEDED", "FAILED", "UNKNOWN", "CANCELLED":
		if prepared != nil {
			prepared.Close()
			prepared = nil
		}
	case "READY", "WAITING_OWNER":
		if prepared == nil {
			prepared, err = service.native.PrepareKubernetesResource(ctx, snapshot)
			if err != nil || prepared == nil {
				return empty, targetrepo.ErrOperationUnavailable
			}
		}
	default:
		return empty, targetrepo.ErrOperationUnavailable
	}
	owned := prepared
	prepared = nil
	return service.executor.Execute(ctx, binding, snapshot, plan, child.Ref, owned)
}

// resolve bounds each authoritative read and rejects malformed or stale snapshots before private access.
// resolve ограничивает authoritative read и отклоняет malformed/stale snapshots до private access.
func (service *ResourceOperations) resolve(ctx context.Context, binding managedkubernetes.Binding) (value.KubernetesOperationSnapshot, error) {
	read, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	snapshot, err := service.owner.ResolveKubernetesOperation(read, binding)
	_, valid := streamOwnerReadPin(snapshot)
	if err != nil || read.Err() != nil || !valid || !snapshot.ExpiresAt.After(time.Now()) {
		return value.KubernetesOperationSnapshot{}, targetrepo.ErrOperationUnavailable
	}
	return snapshot, nil
}
