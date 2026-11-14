package integrationpackage

import "errors"

// CredentialResourcePermissionLokiRulerMutationV1 requires an exact operator-qualified API-only namespace and current key before a single EACH-gated whole-group replacement or deletion.
// CredentialResourcePermissionLokiRulerMutationV1 требует exact operator-qualified API-only namespace и текущий key до одного EACH-gated whole-group replacement либо deletion.
const CredentialResourcePermissionLokiRulerMutationV1 = "LOKI_RULER_MUTATION_V1"

// lokiCredentialContract restricts reads to whole-endpoint or explicit namespace contracts; no tenant header or raw resource name can enable a ruler mutation.
// lokiCredentialContract ограничивает reads whole-endpoint либо explicit namespace contract; tenant header и raw resource name не включают ruler mutation.
func lokiCredentialContract(key, operation string) (CredentialResourcePermissionContract, bool) {
	contract := CredentialResourcePermissionContract{Profile: CredentialResourcePermissionObservabilityReadV1, Operation: operation, ResourceKind: "HTTPS_RESOURCE", Risk: "READ", ApprovalPolicy: "NONE", ReadCapabilities: []string{"loki.connection.read"}}
	if key == "loki" {
		switch operation {
		case "loki.connection.read", "loki.query.instant", "loki.query.range", "loki.logs.query", "loki.labels.list", "loki.label.values", "loki.series.list":
			return contract, true
		}
	}
	if key == "loki-ruler-namespace" || key == "loki-ruler-namespace-read" {
		switch operation {
		case "loki.connection.read", "loki.ruler.group.list", "loki.ruler.group.read":
			return contract, true
		case "loki.ruler.group.replace", "loki.ruler.group.delete":
			if key != "loki-ruler-namespace" {
				return CredentialResourcePermissionContract{}, false
			}
			contract.Profile = CredentialResourcePermissionLokiRulerMutationV1
			contract.Risk = "WRITE"
			contract.ApprovalPolicy = "HUMAN_EACH_EFFECT"
			if operation == "loki.ruler.group.delete" {
				contract.Risk = "DESTRUCTIVE"
			}
			return contract, true
		}
	}
	return CredentialResourcePermissionContract{}, false
}

// validateLokiConfiguration keeps tenant routing literal and prevents widening an explicit API-only namespace into a caller-selected endpoint/path.
// validateLokiConfiguration сохраняет literal tenant routing и запрещает расширять explicit API-only namespace до caller-selected endpoint/path.
func validateLokiConfiguration(definition Package, configuration map[string]string) error {
	if !ValidSentrySlug(configuration["tenant_id"]) {
		return errors.New("Loki tenant routing is invalid")
	}
	if definition.Metadata.Key == "loki" && configuration["scope_mode"] == "WHOLE_ENDPOINT" {
		return nil
	}
	if definition.Metadata.Key == "loki-ruler-namespace-read" && configuration["scope_mode"] == "RULER_NAMESPACE_READ" && ValidSentrySlug(configuration["rule_namespace"]) {
		return nil
	}
	if definition.Metadata.Key == "loki-ruler-namespace" && configuration["scope_mode"] == "API_MANAGED_NAMESPACE" && ValidSentrySlug(configuration["rule_namespace"]) {
		return nil
	}
	return errors.New("Loki resource scope is invalid")
}
