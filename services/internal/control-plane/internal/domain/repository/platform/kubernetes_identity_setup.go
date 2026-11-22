package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// KubernetesIdentitySetupRepository owns setup grants and atomic identity metadata attachment.
// No method receives material; every addressed command resolves current owner before OCC/replay.
// KubernetesIdentitySetupRepository владеет setup grants и atomic identity metadata attachment.
// Методы не получают material; addressed command разрешает current owner до OCC/replay.
type KubernetesIdentitySetupRepository interface {
	// BeginKubernetesIdentitySetup admits only the current self user or an explicitly selected service.
	// BeginKubernetesIdentitySetup допускает current self user либо явно выбранный service.
	BeginKubernetesIdentitySetup(context.Context, value.Principal, value.Mutation, command.KubernetesIdentitySetupBegin) (entity.KubernetesIdentitySetupAdmission, error)
	// GetKubernetesIdentitySetup returns safe metadata after fresh actor/Connection visibility checks.
	// GetKubernetesIdentitySetup возвращает safe metadata после fresh actor/Connection visibility checks.
	GetKubernetesIdentitySetup(context.Context, value.Principal, command.KubernetesIdentitySetupRead) (entity.KubernetesIdentitySetupView, error)
	// CancelKubernetesIdentitySetup closes all setup attempts without pretending to undo a remote write.
	// CancelKubernetesIdentitySetup закрывает setup attempts без утверждения об отмене remote write.
	CancelKubernetesIdentitySetup(context.Context, value.Principal, value.Mutation, command.KubernetesIdentitySetupCancel) (entity.KubernetesIdentitySetupView, error)
	// ResolveKubernetesIdentityUpload returns broker-private pins but never external write credit.
	// ResolveKubernetesIdentityUpload возвращает broker-private pins, не external write credit.
	ResolveKubernetesIdentityUpload(context.Context, value.Principal, command.KubernetesIdentityUploadResolve) (entity.KubernetesIdentityUploadSnapshot, error)
	// ClaimKubernetesIdentityUpload durably limits each setup to one material write and bounded observe attempts.
	// ClaimKubernetesIdentityUpload устойчиво ограничивает setup одной material write и bounded observe attempts.
	ClaimKubernetesIdentityUpload(context.Context, value.Principal, command.KubernetesIdentityUploadClaim) (entity.KubernetesIdentityUploadAdmission, error)
	// CompleteKubernetesIdentityUpload retains UNKNOWN or binds current material; late evidence cannot revive grants.
	// CompleteKubernetesIdentityUpload сохраняет UNKNOWN либо привязывает current material; late evidence не восстанавливает grants.
	CompleteKubernetesIdentityUpload(context.Context, value.Principal, command.KubernetesIdentityUploadComplete) (entity.KubernetesIdentitySetupView, error)
}
