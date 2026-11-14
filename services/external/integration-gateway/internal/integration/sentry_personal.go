package integration

import (
	"context"
	"errors"
	"net/http"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// observeSentryPersonalScopes records only requested current-token metadata after project health; unknown metadata leaves WRITE unconfirmed, while an observed read-only token produces an explicit launch denial.
// observeSentryPersonalScopes фиксирует только запрошенные metadata текущего токена после проверки проекта; неизвестный формат оставляет WRITE неподтверждённым, наблюдённый read-only token закрывает запуск записи.
func (adapter *Adapter) observeSentryPersonalScopes(ctx context.Context, request Request, definition integrationpackage.Package, configuration map[string]string, result *CredentialProbeResult) error {
	health, exists := definition.CapabilityByOperation(definition.Spec.HealthCheck.Operation)
	if !exists || definition.Spec.Adapter != "SENTRY" {
		return &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
	metadata, err := adapter.sentryToken(ctx, request, health, configuration)
	if err != nil {
		var safe *SafeError
		if errors.As(err, &safe) && safe.Code == "INTEGRATION_AUTH_REJECTED" && safe.AuthenticationHTTPStatus != http.StatusForbidden {
			return err
		}
		return nil
	}
	result.ObservedPermissionProfiles = []string{integrationpackage.CredentialObservationSentryScopesV1}
	for _, key := range request.PersonalDeniedWriteCapabilityKeys {
		if !sentryScopeAllows(metadata, key) {
			result.DeniedCapabilityKeys = append(result.DeniedCapabilityKeys, key)
		}
	}
	return nil
}
