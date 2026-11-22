package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// KubernetesRuntimeReader resolves exact execution ownership and fresh original actor visibility before disclosure.
// KubernetesRuntimeReader проверяет точное выполнение и текущую видимость исходного инициатора до выдачи.
type KubernetesRuntimeReader interface {
	ReadKubernetesRuntimeInvocation(context.Context, value.Principal, query.KubernetesRuntimeRead) (entity.KubernetesRuntimeResult, error)
}
