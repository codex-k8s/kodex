package platform

import (
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// ValidateIntegrationCredentialPolicyChange accepts explicit modes only and requires an idle,
// disabled Connection before changing selection. A same-policy retry is not a configuration change;
// the owner must still check authority and OCC before calling this function.
// ValidateIntegrationCredentialPolicyChange принимает только явные режимы и требует неактивное
// отключённое подключение перед сменой выбора. Повтор того же режима не меняет настройку;
// владелец всё равно проверяет полномочия и OCC до вызова функции.
func ValidateIntegrationCredentialPolicyChange(current, requested entity.IntegrationCredentialPolicy, enabled bool, state string, effects, grants, roots int64) error {
	valid := func(policy entity.IntegrationCredentialPolicy) bool {
		return policy == entity.IntegrationCredentialProjectShared || policy == entity.IntegrationCredentialInitiatorRequired
	}
	if !valid(current) || !valid(requested) || effects < 0 || grants < 0 || roots < 0 {
		return errs.ErrInvalid
	}
	if current == requested {
		return nil
	}
	if enabled || state != "DISABLED" || effects != 0 || grants != 0 || roots != 0 {
		return errs.ErrConflict
	}
	return nil
}
