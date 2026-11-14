package integrationpackage

import (
	"errors"
	"strings"
)

// CredentialResourcePermissionObservabilityReadV1 requires a current typed resource read under the exact selected endpoint; authentication rejection is not tenant-isolation proof.
// CredentialResourcePermissionObservabilityReadV1 требует текущего typed resource read точного endpoint; отказ authentication не доказывает tenant isolation.
const CredentialResourcePermissionObservabilityReadV1 = "OBSERVABILITY_TYPED_RESOURCE_READ_V1"

// observabilityCredentialContract closes the supported provider operation registry and keeps whole-endpoint authority explicit in the owner-derived resource scope.
// observabilityCredentialContract замыкает реестр операций провайдеров и сохраняет явную whole-endpoint authority в owner-derived resource scope.
func observabilityCredentialContract(key, operation string) (CredentialResourcePermissionContract, bool) {
	if key == "loki" || key == "loki-ruler-namespace" || key == "loki-ruler-namespace-read" {
		return lokiCredentialContract(key, operation)
	}
	if strings.HasPrefix(key, "grafana-") {
		return grafanaCredentialContract(key, operation)
	}
	if key != "prometheus" && key != "thanos" {
		return CredentialResourcePermissionContract{}, false
	}
	switch strings.TrimPrefix(operation, key+".") {
	case "connection.read", "query.instant", "query.range", "labels.list", "label.values", "series.list", "rules.list", "alerts.list":
		if !strings.HasPrefix(operation, key+".") {
			return CredentialResourcePermissionContract{}, false
		}
		return CredentialResourcePermissionContract{Profile: CredentialResourcePermissionObservabilityReadV1, Operation: operation, ResourceKind: "HTTPS_RESOURCE", Risk: "READ", ApprovalPolicy: "NONE", ReadCapabilities: []string{key + ".connection.read"}}, true
	default:
		return CredentialResourcePermissionContract{}, false
	}
}

// validateObservabilityConfiguration prevents mutable package constraints from silently narrowing the description while expanding endpoint authority or tenant-header syntax.
// validateObservabilityConfiguration не позволяет изменяемым package constraints скрыто расширить endpoint authority либо синтаксис tenant header.
func validateObservabilityConfiguration(definition Package, configuration map[string]string) error {
	if definition.Metadata.Key == "loki" || definition.Metadata.Key == "loki-ruler-namespace" || definition.Metadata.Key == "loki-ruler-namespace-read" {
		return validateLokiConfiguration(definition, configuration)
	}
	if strings.HasPrefix(definition.Metadata.Key, "grafana-") {
		return validateGrafanaConfiguration(definition, configuration)
	}
	if (definition.Metadata.Key != "prometheus" && definition.Metadata.Key != "thanos") || configuration["scope_mode"] != "WHOLE_ENDPOINT" || definition.Metadata.Key == "thanos" && !ValidSentrySlug(configuration["tenant_id"]) {
		return errors.New("observability configuration is invalid")
	}
	return nil
}
