package snapshot

import (
	"encoding/json"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
)

// credentialSignerKey defines the shared public credential-signer field layout.
// Each trust document derives its own distinct named record from this layout;
// issuer, audience, purpose and chain validation remain document-specific.
// credentialSignerKey задаёт общую форму полей открытого подписанта учётных данных.
// Каждый документ доверия определяет от неё собственную отдельную именованную
// запись; подписант, аудитория, назначение и цепочка проверяются по документу.
type credentialSignerKey struct {
	Status     string          `json:"status"`
	Generation uint64          `json:"credential_signer_generation"`
	KeyID      string          `json:"kid"`
	PublicJWK  json.RawMessage `json:"public_jwk"`
	Thumbprint string          `json:"jwk_thumbprint_sha256"`
	NotBefore  int64           `json:"not_before"`
	NotAfter   int64           `json:"not_after"`
}

// validReadbackSignerMetadata checks the shared public-key identity, fingerprint,
// positive generation, closed lifecycle status and half-open validity interval
// after key parsing. It does not verify outer signatures, purpose, audience,
// duplicate key IDs, CURRENT cardinality or durable trust activation.
// validReadbackSignerMetadata проверяет общие личность открытого ключа, отпечаток,
// положительное поколение, закрытый набор статусов и полуоткрытый срок после разбора
// ключа. Подпись внешнего документа, назначение, аудитория, повторы ID, число
// CURRENT и устойчивая активация доверия остаются у вызывающих путей.
func validReadbackSignerMetadata(
	key internalrpcauth.ES256Key,
	expectedKeyID, expectedThumbprint string,
	generation uint64,
	status string,
	notBefore, notAfter int64,
	now time.Time,
) bool {
	thumbprint, thumbprintErr := internalrpcauth.PublicJWKThumbprintSHA256(key)
	if thumbprintErr != nil ||
		key.KeyID != expectedKeyID ||
		thumbprint != expectedThumbprint ||
		generation == 0 ||
		(status != "CURRENT" && status != "NEXT" && status != "PREVIOUS") ||
		now.Before(time.Unix(notBefore, 0)) ||
		!now.Before(time.Unix(notAfter, 0)) {
		return false
	}
	return true
}
