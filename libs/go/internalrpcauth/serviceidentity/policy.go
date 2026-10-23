package serviceidentity

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
)

// Policy is the target-owned, versioned document of exact service RPC grants;
// application revision does not authorize transport, and file delivery remains
// the responsibility of the target policy owner.
// RU: Policy — версионированный документ target с точными допусками service
// RPC; revision приложения не даёт транспортных прав, а доставкой файла
// управляет владелец политики target.
type Policy struct {
	Version        int       `json:"version"`
	TargetSPIFFEID string    `json:"target_spiffe_id"`
	Bindings       []Binding `json:"bindings"`
}

// FromPolicy decodes the complete document for the expected target, rejects
// unknown or duplicate keys and invalid bindings, then returns its exact-byte
// digest for release evidence rather than a cross-target revision check.
// RU: FromPolicy декодирует полный документ ожидаемого target, отклоняет
// неизвестные/повторные ключи и недопустимые bindings, затем возвращает digest
// точных байтов для release evidence, а не сверки ревизий разных target.
func FromPolicy(raw []byte, expectedTarget string, revocations RevocationBoundary) (*Authorizer, string, error) {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return nil, "", errors.New("service policy size rejected")
	}
	var policy Policy
	if err := internalrpcauth.DecodeCanonicalJSON(raw, &policy); err != nil {
		return nil, "", errors.New("service policy document rejected")
	}
	if policy.Version != 1 || policy.TargetSPIFFEID != expectedTarget {
		return nil, "", errors.New("service policy target rejected")
	}
	authorizer, err := New(expectedTarget, policy.Bindings, revocations)
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(raw)
	return authorizer, hex.EncodeToString(digest[:]), nil
}
