package app

import (
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/codex-k8s/kodex/libs/go/oidcverifier"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// oidcIdentityPolicyConfigurer is the bootstrap-only deployment-intent port; configuring a policy alone grants no human access.
// oidcIdentityPolicyConfigurer — bootstrap-only deployment-intent port; настройка policy сама не даёт human access.
type oidcIdentityPolicyConfigurer interface {
	ConfigureStandardOIDCIdentity(value.OIDCIdentityPolicy) error
}

// configureOIDCIdentityPolicy reads one bounded mounted policy document only for explicitly selected platform mode and verifies its exact issuer.
// Unknown JSON fields, trailing content, mode/file ambiguity and incomplete documents fail before bootstrap can mutate persistent state.
// configureOIDCIdentityPolicy читает один bounded mounted policy document лишь для явно выбранного platform mode и проверяет точный issuer.
// Неизвестные JSON fields, trailing content, неоднозначные mode/file и неполные документы отклоняются до изменения persistent state bootstrap.
func configureOIDCIdentityPolicy(owner oidcIdentityPolicyConfigurer, config Config) error {
	mode, valid := oidcverifier.ParseAuthorityMode(config.OIDCAuthorityMode)
	if !valid || owner == nil {
		return errors.New("OIDC identity policy configuration is invalid")
	}
	if mode == oidcverifier.ClaimsAuthorityMode {
		if config.OIDCIdentityPolicyFile != "" {
			return errors.New("OIDC identity policy is unavailable in claims mode")
		}
		return nil
	}
	file, err := os.Open(config.OIDCIdentityPolicyFile)
	if err != nil {
		return errors.New("OIDC identity policy cannot be opened")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 16384 {
		return errors.New("OIDC identity policy file is invalid")
	}
	decoder := json.NewDecoder(io.LimitReader(file, 16385))
	decoder.DisallowUnknownFields()
	var policy value.OIDCIdentityPolicy
	if decoder.Decode(&policy) != nil || decoder.Decode(new(any)) != io.EOF || policy.Validate() != nil || policy.Issuer != config.OIDCIssuer {
		return errors.New("OIDC identity policy document is invalid")
	}
	return owner.ConfigureStandardOIDCIdentity(policy)
}
