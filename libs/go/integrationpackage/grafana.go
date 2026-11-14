package integrationpackage

import (
	"errors"
	"strconv"
)

const (
	// CredentialResourcePermissionGrafanaWriteV1 requires current organization/permissions and exact typed object resolution before one approved effect.
	// CredentialResourcePermissionGrafanaWriteV1 требует текущие organization/permissions и exact typed object resolution до одного approved effect.
	CredentialResourcePermissionGrafanaWriteV1 = "GRAFANA_TYPED_WRITE_V1"
	// CredentialObservationGrafanaPermissionsV1 records current action/scope metadata without claiming a reusable provider WRITE permission.
	// CredentialObservationGrafanaPermissionsV1 фиксирует текущие action/scope metadata без утверждения reusable provider WRITE permission.
	CredentialObservationGrafanaPermissionsV1 = "GRAFANA_CURRENT_PERMISSIONS_V1"
)

// grafanaCredentialContract separates narrow dashboard reads from explicit organization-wide writes; no annotation pre-read is treated as atomic dashboard ownership proof.
// grafanaCredentialContract отделяет narrow dashboard reads от явных organization-wide writes; annotation pre-read не считается atomic dashboard ownership proof.
func grafanaCredentialContract(key, operation string) (CredentialResourcePermissionContract, bool) {
	contract := CredentialResourcePermissionContract{Profile: CredentialResourcePermissionObservabilityReadV1, Operation: operation, ResourceKind: "HTTPS_RESOURCE", Risk: "READ", ApprovalPolicy: "NONE", ReadCapabilities: []string{"grafana.connection.read"}}
	if key != "grafana-dashboard" && key != "grafana-organization" {
		return CredentialResourcePermissionContract{}, false
	}
	switch operation {
	case "grafana.connection.read", "grafana.datasource.read", "grafana.datasource.query", "grafana.annotation.list", "grafana.annotation.read":
		return contract, true
	case "grafana.dashboard.read":
		if key == "grafana-dashboard" {
			return contract, true
		}
	case "grafana.silence.list", "grafana.silence.read", "grafana.rule.list", "grafana.rule.read":
		if key == "grafana-organization" {
			return contract, true
		}
	case "grafana.annotation.create", "grafana.annotation.update", "grafana.silence.create", "grafana.silence.update", "grafana.annotation.delete", "grafana.silence.delete":
		if key == "grafana-organization" {
			contract.Profile, contract.Risk, contract.ApprovalPolicy, contract.RequiredObservationProfile = CredentialResourcePermissionGrafanaWriteV1, "WRITE", "HUMAN_EACH_EFFECT", CredentialObservationGrafanaPermissionsV1
			if operation == "grafana.annotation.delete" || operation == "grafana.silence.delete" {
				contract.Risk = "DESTRUCTIVE"
			}
			return contract, true
		}
	}
	return CredentialResourcePermissionContract{}, false
}

// validateGrafanaConfiguration pins current organization and literal object coordinates independently of mutable package field constraints; dashboard scope can never become organization scope by an extra input.
// validateGrafanaConfiguration закрепляет текущую organization и literal object coordinates независимо от mutable package constraints; dashboard scope не превращается в organization scope дополнительным input.
func validateGrafanaConfiguration(definition Package, configuration map[string]string) error {
	organization, err := strconv.ParseInt(configuration["organization_id"], 10, 64)
	if err != nil || organization < 1 || strconv.FormatInt(organization, 10) != configuration["organization_id"] || !ValidSentrySlug(configuration["datasource_uid"]) {
		return errors.New("Grafana configuration is invalid")
	}
	if definition.Metadata.Key == "grafana-dashboard" && configuration["scope_mode"] == "DASHBOARD_READ" && ValidSentrySlug(configuration["dashboard_uid"]) {
		return nil
	}
	if definition.Metadata.Key == "grafana-organization" && configuration["scope_mode"] == "WHOLE_ORGANIZATION" {
		return nil
	}
	return errors.New("Grafana resource scope is invalid")
}
