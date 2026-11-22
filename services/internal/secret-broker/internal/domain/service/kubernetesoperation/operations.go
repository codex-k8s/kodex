package kubernetesoperation

import (
	"context"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
)

// Operations selects the registered execution family from a fresh owner read, never a gateway selector.
// Each selected consumer repeats its own current authority and immutable input checks before effects.
// Operations выбирает registered execution family по fresh owner read, не gateway selector.
// Выбранный consumer повторяет current authority и immutable input checks до effects.
type Operations struct {
	owner     targetrepo.OperationResolver
	resources *ResourceOperations
	streams   *StreamOperations
}

// NewOperations requires both existing consumers; an absent route never falls back to another engine.
// NewOperations требует оба existing consumers; отсутствующий route не заменяется другим engine.
func NewOperations(owner targetrepo.OperationResolver, resources *ResourceOperations, streams *StreamOperations) (*Operations, error) {
	if owner == nil || resources == nil || streams == nil {
		return nil, targetrepo.ErrOperationUnavailable
	}
	return &Operations{owner: owner, resources: resources, streams: streams}, nil
}

// Execute routes one exact binding after a bounded current read, without touching private target material.
// Execute направляет exact binding после bounded current read без обращения к private target material.
func (operations *Operations) Execute(ctx context.Context, binding managedkubernetes.Binding) (targetrepo.OperationExecution, error) {
	if operations == nil || ctx == nil || binding.Validate() != nil || binding.InvocationRef == "" {
		return targetrepo.OperationExecution{}, targetrepo.ErrOperationUnavailable
	}
	read, cancel := context.WithTimeout(ctx, outputOwnerTimeout)
	snapshot, err := operations.owner.ResolveKubernetesOperation(read, binding)
	readErr := read.Err()
	cancel()
	_, valid := streamOwnerReadPin(snapshot)
	registered, known := integrationpackage.KubernetesOperationByKey(snapshot.Operation)
	if err != nil || readErr != nil || !valid || !known || !snapshot.ExpiresAt.After(time.Now()) {
		return targetrepo.OperationExecution{}, targetrepo.ErrOperationUnavailable
	}
	switch registered.Execution {
	case integrationpackage.KubernetesResource:
		result, err := operations.resources.Execute(ctx, binding)
		return targetrepo.OperationExecution{Effect: result}, err
	case integrationpackage.KubernetesStream:
		return operations.streams.Execute(ctx, binding)
	default:
		if integrationpackage.KubernetesMetadataRead(snapshot.Operation) {
			result, err := operations.resources.Execute(ctx, binding)
			return targetrepo.OperationExecution{Effect: result}, err
		}
		return targetrepo.OperationExecution{}, targetrepo.ErrOperationUnavailable
	}
}
