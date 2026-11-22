package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// KubernetesPortForwardConnectionRepository extends the existing stream/INPUT ledger; it adds no task queue, caller-assigned incarnation or independent lease.
// KubernetesPortForwardConnectionRepository расширяет existing stream/INPUT ledger без task queue, caller-assigned incarnation либо independent lease.
type KubernetesPortForwardConnectionRepository interface {
	// OpenKubernetesPortForwardConnection atomically assigns one owner incarnation and reserves its exact OPEN in the existing protected INPUT producer.
	// OpenKubernetesPortForwardConnection атомарно назначает owner incarnation и резервирует exact OPEN в existing protected INPUT producer.
	OpenKubernetesPortForwardConnection(context.Context, value.Principal, value.Mutation, command.KubernetesPortForwardConnectionOpen) (entity.KubernetesPortForwardConnectionReadback, error)
	// GetKubernetesPortForwardConnection reads one exact owner receipt, including an uncertain Open ACK, without resubmitting bytes.
	// GetKubernetesPortForwardConnection читает exact owner receipt, включая uncertain Open ACK, без повторной передачи bytes.
	GetKubernetesPortForwardConnection(context.Context, value.Principal, query.KubernetesPortForwardConnectionRead) (entity.KubernetesPortForwardConnectionReadback, error)
	// ListKubernetesPortForwardConnections applies current original-root visibility to every bounded stream-scoped page.
	// ListKubernetesPortForwardConnections применяет current original-root visibility к каждой bounded stream-scoped page.
	ListKubernetesPortForwardConnections(context.Context, value.Principal, query.KubernetesPortForwardConnectionList) (entity.KubernetesPortForwardConnectionPage, error)
	// CloseKubernetesPortForwardConnection uses a normal exact pair receipt while eligible; lost authority permits only original parent closure/cleanup without new native bytes.
	// CloseKubernetesPortForwardConnection использует normal exact pair receipt при eligibility; lost authority допускает лишь original parent closure/cleanup без новых native bytes.
	CloseKubernetesPortForwardConnection(context.Context, value.Principal, value.Mutation, command.KubernetesPortForwardConnectionClose) (entity.KubernetesPortForwardConnectionReadback, error)
}
