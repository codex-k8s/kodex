package serviceidentity

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
)

// Policy является отдельной конфигурацией target. Revision приложения не
// участвует в допуске; доставка файла принадлежит владельцу target policy.
type Policy struct {
	Version        int       `json:"version"`
	TargetSPIFFEID string    `json:"target_spiffe_id"`
	Bindings       []Binding `json:"bindings"`
}

// FromPolicy принимает только полный документ выбранного target. Неизвестные
// поля, дубликаты ключей, wildcard/повторные bindings закрыто отклоняются.
// Digest используется в release evidence, но не обязан совпадать у соседей.
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
