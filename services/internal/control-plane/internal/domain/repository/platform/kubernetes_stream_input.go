package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// KubernetesStreamInputRepository reuses the existing execution stream/frame lifecycle under current original-root eligibility; it creates no independent task or authority graph.
// KubernetesStreamInputRepository использует existing execution stream/frame lifecycle с current original-root eligibility; независимый task либо authority graph не создаётся.
type KubernetesStreamInputRepository interface {
	// StopKubernetesStream closes the exact original tool graph under current root visibility/OCC, even when its former execution grant is no longer usable; known external facts remain immutable.
	// StopKubernetesStream закрывает exact original tool graph под current root visibility/OCC, даже если прежний execution grant уже недопустим; known external facts остаются immutable.
	StopKubernetesStream(context.Context, value.Principal, value.Mutation, command.KubernetesStreamInputStop) (entity.KubernetesStreamInputView, error)

	// ListKubernetesStreamInputs applies common current root visibility to every bounded page; the owner separately enforces action-specific live write authority.
	// ListKubernetesStreamInputs применяет common current root visibility на каждой bounded page; live write authority owner отдельно проверяет по action.
	ListKubernetesStreamInputs(context.Context, value.Principal, query.KubernetesStreamInputList) (entity.KubernetesStreamInputPage, error)
	// GetKubernetesStreamInput reads safe exact metadata without recovering private bytes or replacing a submission.
	// GetKubernetesStreamInput читает safe exact metadata без восстановления private bytes либо замены submission.
	GetKubernetesStreamInput(context.Context, value.Principal, query.KubernetesStreamInputRead) (entity.KubernetesStreamInputReadback, error)
	// SubmitKubernetesStreamInput derives sequence and private storage under owner OCC/idempotency, before one private claim and the broker's native admission.
	// SubmitKubernetesStreamInput назначает sequence и private storage под owner OCC/idempotency до одного private claim и broker native admission.
	SubmitKubernetesStreamInput(context.Context, value.Principal, value.Mutation, command.KubernetesStreamInputSubmit) (entity.KubernetesStreamInputReadback, error)
}
