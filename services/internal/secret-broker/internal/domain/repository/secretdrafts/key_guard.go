package secretdrafts

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// KeyGuard requires verifier-owned durable manifest/high-watermark and encryption-use state shared across
// replicas; it never transports key bytes.
// KeyGuard требует устойчивые verifier-owned manifest/high-watermark и счётчик шифрований, общие для
// replicas; материал ключей через него не передаётся.
type KeyGuard interface {
	// Observe validates and records an accepted manifest without allowing revision/current-key rollback or
	// dropping retained keys.
	// Observe проверяет и фиксирует принятый manifest без отката revision/current-key и удаления retained
	// keys.
	Observe(context.Context, value.DraftKeyManifest) error
	// Reserve durably consumes one encryption allowance for the exact current key before sealing; a later
	// failure does not refund it.
	// Reserve устойчиво расходует одно разрешённое шифрование точного current key до sealing; последующий сбой
	// не возвращает расход.
	Reserve(context.Context, value.DraftEncryptionKey) error
	// CheckCurrent verifies the current key and remaining durable write budget without consuming an encryption
	// allowance.
	// CheckCurrent проверяет current key и остаток устойчивого write budget без расхода разрешённого
	// шифрования.
	CheckCurrent(context.Context, value.DraftEncryptionKey) error
}
