// Package readbackrow shares only the fixed intent column layout between
// publisher/readback PostgreSQL adapters, not their distinct validation policies.
// Пакет readbackrow разделяет лишь фиксированный layout колонок intent между
// PostgreSQL-адаптерами publisher и readback, но не их разные политики проверки.
package readbackrow

import "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"

// Destinations returns eighteen ordered pointers into a nonnil caller-owned
// intent and public-JWK buffer slot for positional scanning. It does not scan,
// validate JSON, copy key bytes or decide whether partially decoded data is valid.
// Destinations возвращает восемнадцать упорядоченных указателей на ненулевой
// intent вызывающего и слот буфера публичного JWK для позиционного Scan. Чтение,
// проверка JSON, копирование ключа и допуск частичных данных здесь не выполняются.
func Destinations(intent *model.ReadbackIntent, publicJWK *[]byte) []any {
	return []any{
		&intent.IntentID,
		&intent.Kind,
		&intent.IntentRevision,
		&intent.IntentDigestSHA256,
		&intent.WorkloadID,
		&intent.WorkloadSPIFFEID,
		&intent.Role,
		&intent.WorkloadGeneration,
		&intent.CredentialGeneration,
		&intent.MaterialGeneration,
		&intent.PossessionKeyID,
		&intent.PossessionKeyGeneration,
		publicJWK,
		&intent.PossessionKeyThumbprint,
		&intent.SourceRevision,
		&intent.ServedStateDigestSHA256,
		&intent.Status,
		&intent.ExpiresAt,
	}
}
