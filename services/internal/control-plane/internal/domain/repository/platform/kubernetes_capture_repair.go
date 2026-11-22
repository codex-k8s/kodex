package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// KubernetesCaptureRepairRepository owns original-root visibility, upload permission, OCC,
// idempotency and the existing fenced capture task. It grants no native execution or payload replay.
// KubernetesCaptureRepairRepository владеет original-root visibility, upload permission, OCC,
// idempotency и existing fenced capture task. Native execution либо payload replay не выдаёт.
type KubernetesCaptureRepairRepository interface {
	// GetKubernetesCaptureStatus reads current original-owner metadata and optional original-key receipt.
	// GetKubernetesCaptureStatus читает current original-owner metadata и optional original-key receipt.
	GetKubernetesCaptureStatus(context.Context, value.Principal, query.KubernetesCaptureStatusRead) (entity.KubernetesCaptureStatusReadback, error)
	// RepairKubernetesCapture opens only the existing bounded artifact task after upload permission and OCC.
	// RepairKubernetesCapture открывает лишь existing bounded artifact task после upload permission и OCC.
	RepairKubernetesCapture(context.Context, value.Principal, value.Mutation, command.KubernetesCaptureRepair) (entity.KubernetesCaptureStatusReadback, error)
}

// KubernetesStagingCleanupRepository repairs only the exhausted immutable storage cleanup cycle.
// Native tasks, frame receipts and total attempt counters remain owned by their existing graph.
// KubernetesStagingCleanupRepository восстанавливает только exhausted immutable storage cleanup cycle.
// Native tasks, frame receipts и total attempt counters остаются в существующем owner graph.
type KubernetesStagingCleanupRepository interface {
	// RepairKubernetesStagingCleanup requires original root visibility, upload authority and exact OCC.
	// RepairKubernetesStagingCleanup требует original root visibility, upload authority и exact OCC.
	RepairKubernetesStagingCleanup(context.Context, value.Principal, value.Mutation, command.KubernetesCaptureRepair) (entity.KubernetesCaptureStatusReadback, error)
}
