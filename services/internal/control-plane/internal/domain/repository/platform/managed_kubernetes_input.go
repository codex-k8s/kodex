package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ManagedKubernetesInputRepository owns one-attempt staged delivery on the original stream graph.
// A missing port stays unavailable; no generic artifact or TOKEN materialization can replace it.
// ManagedKubernetesInputRepository владеет one-attempt staged delivery исходного stream graph.
// Отсутствующий port недоступен; generic artifact/TOKEN materialization его не заменяет.
type ManagedKubernetesInputRepository interface {
	// ClaimManagedKubernetesInputFrame commits the attempt before reading exclusive private bytes.
	// ClaimManagedKubernetesInputFrame фиксирует attempt до чтения exclusive private bytes.
	ClaimManagedKubernetesInputFrame(context.Context, value.Principal, value.ManagedKubernetesBinding, string) (entity.ManagedKubernetesInputFrameAdmission, error)
}
