package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// KubernetesToolRepository owns public visibility and closure of one existing invocation, including pre-stream gates.
// KubernetesToolRepository владеет проверкой видимости и закрытием существующего вызова, включая Gate до создания потока.
type KubernetesToolRepository interface {
	// ListKubernetesTools checks original-root eligibility for every item before disclosure.
	// ListKubernetesTools проверяет права исходного инициатора на каждый элемент до выдачи.
	ListKubernetesTools(context.Context, value.Principal, query.KubernetesToolList) (entity.KubernetesToolPage, error)
	// GetKubernetesTool returns current metadata without recovering execution credit or private bytes.
	// GetKubernetesTool возвращает текущие метаданные без восстановления прав исполнения или приватных данных.
	GetKubernetesTool(context.Context, value.Principal, string) (entity.KubernetesToolView, error)
	// StopKubernetesTool atomically closes only the selected invocation after current visibility, idempotency and OCC checks.
	// StopKubernetesTool атомарно закрывает выбранный вызов после проверки видимости, идемпотентности и версии.
	StopKubernetesTool(context.Context, value.Principal, value.Mutation, string) (entity.KubernetesToolView, error)
}
