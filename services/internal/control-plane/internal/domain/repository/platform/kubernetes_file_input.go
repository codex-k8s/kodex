package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// KubernetesFileInputRepository keeps source materialization inside the existing owner graph.
// Public methods read original-root metadata; private preparation derives the artifact from the
// frozen invocation/catalog/child and cannot accept caller bytes or a different source selector.
// KubernetesFileInputRepository оставляет materialization источника внутри existing owner graph.
// Public methods читают original-root metadata; private preparation выводит artifact из frozen
// invocation/catalog/child, не принимает caller bytes либо иной source selector.
type KubernetesFileInputRepository interface {
	// ListKubernetesFileInputs discovers at most256 exact-invocation source records after current root visibility.
	// ListKubernetesFileInputs обнаруживает до256 exact-invocation source records после current root visibility.
	ListKubernetesFileInputs(context.Context, value.Principal, string) ([]entity.KubernetesFileInputView, error)
	// GetKubernetesFileInput reads one original-invocation source without issuing fresh execution rights.
	// GetKubernetesFileInput читает original-invocation source без выдачи fresh execution rights.
	GetKubernetesFileInput(context.Context, value.Principal, string, string) (entity.KubernetesFileInputView, error)
	// PrepareManagedKubernetesFileInput owns one prepared-content intent before any native Begin.
	// PrepareManagedKubernetesFileInput владеет одним prepared-content intent до любого native Begin.
	PrepareManagedKubernetesFileInput(context.Context, value.Principal, value.ManagedKubernetesBinding, string) (entity.ManagedKubernetesFilePreparation, error)
}
