package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// KubernetesResourceMaterialRepository owns actor-local artifacts without receiving private bytes
// on this public port. Every operation resolves current owner/Connection/identity before OCC/replay.
// KubernetesResourceMaterialRepository владеет actor-local artifacts без приёма private bytes
// через public port. Каждая operation разрешает current owner/Connection/identity до OCC/replay.
type KubernetesResourceMaterialRepository interface {
	// BeginKubernetesResourceMaterial issues one bounded exact-destination upload admission.
	// BeginKubernetesResourceMaterial выдаёт один bounded exact-destination upload admission.
	BeginKubernetesResourceMaterial(context.Context, value.Principal, value.Mutation, command.KubernetesResourceMaterialBegin) (entity.KubernetesResourceMaterialAdmission, error)
	// GetKubernetesResourceMaterial freshly reads safe metadata without recovering private upload authority.
	// GetKubernetesResourceMaterial свежо читает safe metadata без восстановления private upload authority.
	GetKubernetesResourceMaterial(context.Context, value.Principal, command.KubernetesResourceMaterialRead) (entity.KubernetesResourceMaterialView, error)
	// CancelKubernetesResourceMaterial closes future use while preserving immutable prior effect facts.
	// CancelKubernetesResourceMaterial закрывает future use с сохранением immutable prior effect facts.
	CancelKubernetesResourceMaterial(context.Context, value.Principal, value.Mutation, command.KubernetesResourceMaterialCancel) (entity.KubernetesResourceMaterialView, error)
}
