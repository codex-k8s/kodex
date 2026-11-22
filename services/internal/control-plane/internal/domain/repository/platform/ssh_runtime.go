package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// SSHRuntimeReader resolves exact execution ownership and fresh original actor visibility before disclosure.
// SSHRuntimeReader проверяет точное выполнение и текущую видимость исходного инициатора до выдачи.
type SSHRuntimeReader interface {
	ReadSSHRuntimeInvocation(context.Context, value.Principal, query.SSHRuntimeRead) (entity.SSHRuntimeResult, error)
}
