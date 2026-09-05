package secretdrafts

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// KeyGuard хранит принятый manifest и счётчик использований на проверяющей
// стороне. Его состояние обязано переживать замену Pod и смену replica.
type KeyGuard interface {
	Observe(context.Context, value.DraftKeyManifest) error
	Reserve(context.Context, value.DraftEncryptionKey) error
	CheckCurrent(context.Context, value.DraftEncryptionKey) error
}
