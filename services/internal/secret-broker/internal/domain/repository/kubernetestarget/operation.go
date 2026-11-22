package kubernetestarget

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// ErrOperationUnavailable hides private owner/transport diagnostics and grants no fallback authority.
// ErrOperationUnavailable скрывает private owner/transport diagnostics без fallback authority.
var ErrOperationUnavailable = errors.New("managed Kubernetes operation is unavailable")

// OperationResolver reads the exact current owner graph without granting an external effect.
// OperationResolver читает exact current owner graph без разрешения external effect.
type OperationResolver interface {
	// ResolveKubernetesOperation preserves the gateway claim and cannot choose another identity or target.
	// ResolveKubernetesOperation сохраняет gateway claim и не выбирает другую identity либо target.
	ResolveKubernetesOperation(context.Context, managedkubernetes.Binding) (value.KubernetesOperationSnapshot, error)
}

// OperationStreamResolver checks an existing ACTIVE stream inside its original owner graph.
// OperationStreamResolver проверяет существующий ACTIVE stream внутри исходного owner graph.
type OperationStreamResolver interface {
	// ResolveKubernetesStream performs a fresh read without issuing frame credit or opening transport.
	// ResolveKubernetesStream выполняет fresh read без frame credit либо открытия transport.
	ResolveKubernetesStream(context.Context, managedkubernetes.Binding, string) (value.KubernetesOperationSnapshot, error)
}

// OperationExecution returns either admitted stream metadata or a resource/gate/reconciliation result.
// Neither branch derives authority from caller-selected operation names or credentials.
// OperationExecution возвращает admitted stream metadata либо resource/gate/reconciliation result.
// Ни одна ветвь не выводит authority из выбранных caller operation names либо credentials.
type OperationExecution struct {
	Effect ResourceExecution
	Stream *value.KubernetesStreamGrant
}
