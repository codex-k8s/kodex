// Package proofsigner задаёт client port authority-proof signer.
package proofsigner

import (
	"context"

	authoritytype "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/authority"
)

// State фиксирует проверенную served identity signer.
type State struct {
	PolicyRevision   uint64
	PolicyDigest     string
	SignerGeneration uint64
	PublicThumbprint string
}

// Signer подписывает canonical claims ключом, independently verified при startup.
type Signer interface {
	Sign(context.Context, authoritytype.ProofClaims) (string, string, error)
	Check(context.Context) (State, error)
}
