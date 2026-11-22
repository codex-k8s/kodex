package platform

import (
	"context"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ManagedKubernetesCaptureRepository owns the fixed artifact-retention task path and historical
// source eligibility. No method accepts caller-assigned artifact bytes, actors or native outcomes.
// ManagedKubernetesCaptureRepository владеет fixed artifact-retention task path и historical source
// eligibility. Методы не принимают caller-assigned artifact bytes, actors либо native outcomes.
type ManagedKubernetesCaptureRepository interface {
	// FinalizeManagedKubernetesCapture reads the exact sealed source and adopts owner-derived artifacts.
	// FinalizeManagedKubernetesCapture читает exact sealed source и принимает owner-derived artifacts.
	FinalizeManagedKubernetesCapture(context.Context, value.Principal, value.ManagedKubernetesCaptureTaskBinding) (entity.ManagedKubernetesCaptureReceipt, error)
	// GetManagedKubernetesCapture returns metadata for the original task attempt after lost ACK.
	// GetManagedKubernetesCapture возвращает metadata original task attempt после lost ACK.
	GetManagedKubernetesCapture(context.Context, value.Principal, value.ManagedKubernetesCaptureTaskBinding) (entity.ManagedKubernetesCaptureReceipt, error)
	// RenewManagedKubernetesCapture extends only the current artifact attempt within its hard deadline.
	// RenewManagedKubernetesCapture продлевает только current artifact attempt до hard deadline.
	RenewManagedKubernetesCapture(context.Context, value.Principal, value.ManagedKubernetesCaptureTaskBinding) (time.Time, error)
	// CheckManagedKubernetesCaptureReadiness verifies producer dependencies without a worker startup cycle.
	// CheckManagedKubernetesCaptureReadiness проверяет producer dependencies без worker startup cycle.
	CheckManagedKubernetesCaptureReadiness(context.Context, value.Principal) (uint32, error)
}
