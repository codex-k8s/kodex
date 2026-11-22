package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// KubernetesStreamOutputReader retains current original-actor eligibility through exact object verification and final disclosure.
// KubernetesStreamOutputReader сохраняет current original-actor eligibility при проверке exact object и итоговой выдаче.
type KubernetesStreamOutputReader interface {
	ReadKubernetesStreamOutput(context.Context, value.Principal, query.KubernetesStreamOutputRead) (entity.KubernetesStreamOutput, error)
}
