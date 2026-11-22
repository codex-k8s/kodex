package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ManagedKubernetesInputReadinessRepository proves actual installed INPUT owner compatibility.
// Missing optional support is revision zero and cannot advertise a functional execution path.
// ManagedKubernetesInputReadinessRepository подтверждает actual installed INPUT owner compatibility.
// Отсутствующая optional support имеет revision0, не объявляет functional execution path.
type ManagedKubernetesInputReadinessRepository interface {
	// ManagedKubernetesInputContract checks the same repository/storage/schema used by private claims.
	// ManagedKubernetesInputContract проверяет repository/storage/schema реальных private claims.
	ManagedKubernetesInputContract(context.Context, value.Principal) (uint32, error)
}

// ManagedKubernetesFileReadinessRepository proves FILE source provenance, preparation and range IO support.
// It cannot be substituted by raw public stdin, source metadata or identity-only health readiness.
// ManagedKubernetesFileReadinessRepository подтверждает FILE source provenance, preparation и range IO.
// Raw public stdin, source metadata либо identity-only health readiness не являются заменой.
type ManagedKubernetesFileReadinessRepository interface {
	// ManagedKubernetesFileInputContract checks the actual source and prepared-content owner path.
	// ManagedKubernetesFileInputContract проверяет actual source и prepared-content owner path.
	ManagedKubernetesFileInputContract(context.Context, value.Principal) (uint32, error)
}
