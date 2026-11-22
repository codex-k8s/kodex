package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ManagedKubernetesOutputReadinessRepository proves the exact output storage/publication compatibility.
// Default/missing support is revision zero and never makes the Kubernetes purpose ready.
// ManagedKubernetesOutputReadinessRepository доказывает exact output storage/publication compatibility.
// Default/missing support имеет revision0 и не делает Kubernetes purpose готовым.
type ManagedKubernetesOutputReadinessRepository interface {
	// ManagedKubernetesOutputContract checks actual schema, storage and publication consumer readiness.
	// ManagedKubernetesOutputContract проверяет actual schema/storage/publication consumer readiness.
	ManagedKubernetesOutputContract(context.Context, value.Principal) (uint32, error)
}
