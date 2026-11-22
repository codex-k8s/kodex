package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// KubernetesCaptureHistoryRepository applies original-root current visibility to every bounded result.
// KubernetesCaptureHistoryRepository применяет original-root current visibility к каждому bounded result.
type KubernetesCaptureHistoryRepository interface {
	// ListKubernetesCaptures discovers retained results independently of current integration permission.
	// ListKubernetesCaptures обнаруживает сохранённые результаты независимо от current integration permission.
	ListKubernetesCaptures(context.Context, value.Principal, query.KubernetesCaptureList) (entity.KubernetesCapturePage, error)
}
