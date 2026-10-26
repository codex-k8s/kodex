package app

import (
	"context"
	"errors"
)

// readinessDependency observes one existing infrastructure dependency under the
// readiness worker's shared timeout without assigning request authority.
// readinessDependency проверяет существующую инфраструктурную зависимость
// с общим timeout worker и не назначает полномочия запросу.
type readinessDependency interface {
	Check(context.Context) error
}

// signingKeyInitialization exposes only the first verified snapshot milestone;
// later token eligibility and refresh degradation remain verifier responsibilities.
// signingKeyInitialization предоставляет только факт первого проверенного снимка;
// дальнейший допуск токенов и деградация обновления остаются в verifier.
type signingKeyInitialization interface {
	SigningKeysInitialized() bool
}

// initialSigningKeysReason identifies the closed startup-only readiness barrier.
// initialSigningKeysReason обозначает закрытый барьер readiness только при старте.
const initialSigningKeysReason = "signing_keys_starting"

// errInitialSigningKeys prevents serving readiness before the first trusted
// snapshot without exposing issuer configuration or causing another fetch.
// errInitialSigningKeys запрещает готовность до первого доверенного снимка
// без раскрытия настроек issuer и дополнительного обращения к нему.
var errInitialSigningKeys = errors.New("OIDC signing-key initialization is pending")
