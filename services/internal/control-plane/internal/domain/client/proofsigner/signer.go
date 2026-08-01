// Package proofsigner задаёт клиентский порт подписания доказательств полномочий.
package proofsigner

import (
	"context"

	authoritytype "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/authority"
)

// State фиксирует проверенную обслуживаемую идентичность подписывающего компонента.
type State struct {
	TrustRevision    uint64
	TrustDigest      string
	SignerGeneration uint64
	PublicThumbprint string
}

// Signer подписывает канонические запросы ключом, независимо проверенным при запуске.
type Signer interface {
	Sign(context.Context, authoritytype.ProofClaims) (string, string, State, error)
	Check(context.Context) (State, error)
}
