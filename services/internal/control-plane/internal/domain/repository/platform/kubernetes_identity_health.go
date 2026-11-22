package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// KubernetesIdentityHealthRepository owns authentication observations outside the external effect graph.
// Only the selected identity is tested; an authentication fact never proves global Kubernetes RBAC.
// KubernetesIdentityHealthRepository владеет authentication observations вне external effect graph.
// Проверяется только выбранная identity; authentication fact не доказывает global Kubernetes RBAC.
type KubernetesIdentityHealthRepository interface {
	// BeginManagedKubernetesHealthProbe consumes one exact current test claim before returning private pins.
	// BeginManagedKubernetesHealthProbe расходует exact current test claim до выдачи private pins.
	BeginManagedKubernetesHealthProbe(context.Context, value.Principal, value.ManagedKubernetesBinding) (entity.KubernetesIdentityHealthAdmission, error)
	// CompleteManagedKubernetesHealthProbe records exact evidence and conditionally verifies the current binding.
	// CompleteManagedKubernetesHealthProbe сохраняет exact evidence и проверяет только current binding.
	CompleteManagedKubernetesHealthProbe(context.Context, value.Principal, value.ManagedKubernetesBinding, command.KubernetesIdentityHealthCompletion) (entity.KubernetesIdentityHealthReceipt, error)
}
