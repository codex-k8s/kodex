package integration

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// grafanaJSON sends only fixed typed native API calls; every non-GET remains a single non-replayable effect.
// grafanaJSON отправляет только fixed typed native API calls; каждый non-GET остаётся однократным effect без replay.
func (adapter *Adapter) grafanaJSON(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, method, path string, query url.Values, body any, probe bool) (*providerResponse, error) {
	if adapter.nativeHTTPClient == nil {
		return nil, &SafeError{Code: providerUnavailableCode}
	}
	if kind := grafanaQualificationKind(request.DefinitionKey); kind != "" {
		profile, err := adapter.operatorQualificationFor(request, configuration, kind)
		if err != nil {
			return nil, err
		}
		expiry, _ := time.Parse(time.RFC3339, profile.ValidUntil)
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, expiry)
		defer cancel()
	}
	return adapter.callProviderResponse(ctx, providerCall{BaseURL: configuration["base_url"], Method: method, Path: path, Query: query, Body: body, AuthScheme: "BEARER", Credential: request.Credential, Capability: capability, Client: adapter.nativeHTTPClient, permissionEffect: request.permissionEffect, invalidCredentialProbe: probe})
}

// grafanaHealth checks the exact supported build, current token's organization and signed identity, and invalid-key rejection on the same authenticated path without projecting user data.
// grafanaHealth проверяет exact supported build, organization и signed identity текущего токена и invalid-key rejection на том же authenticated path без выдачи user data.
func (adapter *Adapter) grafanaHealth(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string) (map[string]string, error) {
	// The authenticated probe is a registered inputless READ prerequisite even when the outer invocation is a gated mutation.
	// Authenticated probe остаётся registered inputless READ prerequisite, даже если внешний invocation является gated mutation.
	readCapability, exists := adapter.definitions[request.DefinitionKey].Capability("grafana.connection.read")
	if !exists || readCapability.Risk != "READ" || readCapability.ApprovalPolicy != "NONE" {
		return nil, &SafeError{Code: providerCapabilityUnsupportedCode}
	}
	capability = readCapability
	response, err := adapter.grafanaJSON(ctx, request, capability, configuration, http.MethodGet, "/api/health", nil, nil, false)
	if err != nil {
		return nil, err
	}
	var health struct {
		Version  string `json:"version"`
		Database string `json:"database"`
	}
	if decodeTypedProviderJSON(response.Body, &health) != nil {
		return nil, &SafeError{Code: providerResponseInvalidCode}
	}
	if health.Version != "13.2.0" {
		return nil, &SafeError{Code: providerCapabilityUnsupportedCode}
	}
	if health.Database != "ok" {
		return nil, &SafeError{Code: providerUnavailableCode}
	}
	response, err = adapter.grafanaJSON(ctx, request, capability, configuration, http.MethodGet, "/api/user", nil, nil, false)
	if err != nil {
		return nil, err
	}
	var identity struct {
		OrgID int64  `json:"orgId"`
		UID   string `json:"uid"`
	}
	if decodeTypedProviderJSON(response.Body, &identity) != nil || strconv.FormatInt(identity.OrgID, 10) != configuration["organization_id"] || identity.UID == "" || identity.UID == "0" || len(identity.UID) > 200 || strings.Contains(strings.ToLower(identity.UID), "anonymous") || strings.ContainsAny(identity.UID, "\r\n\x00") {
		return nil, &SafeError{Code: providerResponseInvalidCode}
	}
	_, err = adapter.grafanaJSON(ctx, request, capability, configuration, http.MethodGet, "/api/user", nil, nil, true)
	var safe *SafeError
	if !errors.As(err, &safe) || (safe.AuthenticationHTTPStatus != http.StatusUnauthorized && safe.AuthenticationHTTPStatus != http.StatusForbidden) {
		if err != nil && (!errors.As(err, &safe) || safe.Code == providerUnavailableCode) {
			return nil, err
		}
		return nil, &SafeError{Code: providerConfigurationInvalidCode}
	}
	return map[string]string{"provider": "grafana", "version": health.Version, "organization_id": configuration["organization_id"], "scope_mode": configuration["scope_mode"], "authentication_rejection": "OBSERVED"}, nil
}

// grafanaPermissions reloads current action/scope metadata from the same selected token; unknown or oversized documents never become sufficient WRITE evidence.
// grafanaPermissions повторно загружает текущие action/scope metadata того же selected token; неизвестные либо слишком большие документы не становятся достаточным WRITE evidence.
func (adapter *Adapter) grafanaPermissions(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string) (map[string][]string, error) {
	response, err := adapter.grafanaJSON(ctx, request, capability, configuration, http.MethodGet, "/api/access-control/user/permissions", url.Values{"reloadcache": {"true"}}, nil, false)
	if err != nil {
		return nil, err
	}
	var permissions map[string][]string
	if integrationpackage.ValidateCredentialPermissionJSON(response.Body) != nil || decodeProviderJSON(response.Body, &permissions) != nil || permissions == nil || len(permissions) > 500 {
		return nil, &SafeError{Code: providerResponseInvalidCode}
	}
	total := 0
	for action, scopes := range permissions {
		if action == "" || len(action) > 100 || strings.ContainsAny(action, "\r\n\x00") || scopes == nil || len(scopes) > 500 {
			return nil, &SafeError{Code: providerResponseInvalidCode}
		}
		seen := map[string]bool{}
		for _, scope := range scopes {
			total++
			if len(scope) > 200 || strings.ContainsAny(scope, "\r\n\x00") || seen[scope] || total > 2000 {
				return nil, &SafeError{Code: providerResponseInvalidCode}
			}
			seen[scope] = true
		}
	}
	return permissions, nil
}

// grafanaWriteAction maps only registered mutations to exact provider actions; no request field or raw permission string can select a new command.
// grafanaWriteAction сопоставляет только registered mutations с exact provider actions; request field либо raw permission string не выбирает новую команду.
func grafanaWriteAction(operation string) string {
	switch operation {
	case "grafana.rule.create":
		return "alert.rules:create"
	case "grafana.rule.update":
		return "alert.rules:write"
	case "grafana.rule.delete":
		return "alert.rules:delete"
	case "grafana.annotation.create":
		return "annotations:create"
	case "grafana.annotation.update":
		return "annotations:write"
	case "grafana.annotation.delete":
		return "annotations:delete"
	case "grafana.silence.create":
		return "alert.instances:create"
	case "grafana.silence.update":
		return "alert.instances:write"
	case "grafana.silence.delete":
		return "alert.instances:write"
	}
	return ""
}

// grafanaPermissionScope permits only current exact/wildcard matches from the finite provider scope grammar; folder inheritance is independently enforced by Grafana at effect time.
// grafanaPermissionScope допускает только текущие exact/wildcard matches конечной provider scope grammar; folder inheritance независимо проверяет Grafana при effect.
func grafanaPermissionScope(permissions map[string][]string, action string, targets ...string) bool {
	for _, scope := range permissions[action] {
		if scope == "*" {
			return true
		}
		for _, target := range targets {
			if scope == target {
				return true
			}
			if strings.HasSuffix(scope, ":*") && strings.HasPrefix(target, strings.TrimSuffix(scope, "*")) {
				return true
			}
		}
	}
	return false
}

// observeGrafanaPersonalPermissions records a conditional current-metadata profile and known-deny actions after authenticated health; it never confirms reusable WRITE capabilities.
// observeGrafanaPersonalPermissions фиксирует conditional current-metadata profile и known-deny actions после authenticated health; reusable WRITE capabilities не подтверждаются.
func (adapter *Adapter) observeGrafanaPersonalPermissions(ctx context.Context, request Request, definition integrationpackage.Package, configuration map[string]string, result *CredentialProbeResult) error {
	health, exists := definition.CapabilityByOperation(definition.Spec.HealthCheck.Operation)
	if !exists {
		return &SafeError{Code: providerCapabilityUnsupportedCode}
	}
	permissions, err := adapter.grafanaPermissions(ctx, request, health, configuration)
	if err != nil {
		var safe *SafeError
		if errors.As(err, &safe) && safe.AuthenticationHTTPStatus == http.StatusUnauthorized {
			return err
		}
		return nil
	}
	result.ObservedPermissionProfiles = []string{integrationpackage.CredentialObservationGrafanaPermissionsV1}
	for _, operation := range request.PersonalDeniedWriteCapabilityKeys {
		if len(permissions[grafanaWriteAction(operation)]) == 0 || strings.HasPrefix(operation, "grafana.silence.") && !grafanaSilencePermissions(permissions, operation) {
			result.DeniedCapabilityKeys = append(result.DeniedCapabilityKeys, operation)
		}
	}
	return nil
}

// executeGrafana separates dashboard reads from explicit whole-organization effects, rechecking current provider permissions before each mutation and preserving UNKNOWN after ambiguous effects.
// executeGrafana отделяет dashboard reads от явных whole-organization effects, повторно проверяет current provider permissions перед mutation и сохраняет UNKNOWN после ambiguous effects.
func (adapter *Adapter) executeGrafana(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string) (Result, error) {
	contract, exists := integrationpackage.CredentialResourcePermissionContractFor(request.DefinitionKey, request.CapabilityKey)
	if !exists || contract.Operation != request.Operation || contract.Risk != request.Risk || contract.ApprovalPolicy != request.ApprovalPolicy || request.PersonalCredentialAdmission && request.ResourcePermissionProfile != contract.Profile {
		return Result{}, &SafeError{Code: providerCapabilityUnsupportedCode}
	}
	health, err := adapter.grafanaHealth(ctx, request, capability, configuration)
	if err != nil {
		return Result{}, err
	}
	var permissions map[string][]string
	if capability.Risk != "READ" {
		if request.DefinitionKey != "grafana-organization" && request.DefinitionKey != "grafana-dashboard-annotations" && request.DefinitionKey != "grafana-organization-rule-delete" {
			return Result{}, &SafeError{Code: providerCapabilityUnsupportedCode}
		}
		permissions, err = adapter.grafanaPermissions(ctx, request, capability, configuration)
		if err != nil {
			return Result{}, err
		}
		if len(permissions[grafanaWriteAction(request.Operation)]) == 0 {
			return Result{}, &SafeError{Code: providerRequestRejectedCode}
		}
	}
	var projection any = health
	reference := "grafana-organization:" + configuration["organization_id"]
	switch request.Operation {
	case "grafana.connection.read":
		if request.DefinitionKey == "grafana-dashboard" || request.DefinitionKey == "grafana-dashboard-annotations" {
			if _, err := adapter.grafanaDashboard(ctx, request, capability, configuration, configuration["dashboard_uid"]); err != nil {
				return Result{}, err
			}
		}
		if configuration["datasource_uid"] != "" {
			if _, err := adapter.grafanaDatasource(ctx, request, capability, configuration); err != nil {
				return Result{}, err
			}
		}
	case "grafana.dashboard.read":
		projection, err = adapter.grafanaDashboard(ctx, request, capability, configuration, configuration["dashboard_uid"])
	case "grafana.datasource.read":
		projection, err = adapter.grafanaDatasource(ctx, request, capability, configuration)
	case "grafana.datasource.query":
		projection, err = adapter.grafanaDatasourceQuery(ctx, request, capability, configuration)
	default:
		if strings.HasPrefix(request.Operation, "grafana.annotation.") {
			projection, reference, err = adapter.grafanaAnnotations(ctx, request, capability, configuration, permissions)
		} else if strings.HasPrefix(request.Operation, "grafana.silence.") {
			projection, reference, err = adapter.grafanaSilences(ctx, request, capability, configuration, permissions)
		} else if capability.Risk != "READ" {
			projection, reference, err = adapter.grafanaRuleMutation(ctx, request, capability, configuration, permissions)
		} else {
			projection, reference, err = adapter.grafanaRulesRead(ctx, request, capability, configuration)
		}
	}
	if err != nil {
		return Result{}, err
	}
	raw, err := json.Marshal(projection)
	if err != nil || len(raw) > 65536 {
		return Result{}, &SafeError{Code: providerResponseInvalidCode}
	}
	return providerResult(request, reference, map[string]string{"text": string(raw)})
}

// grafanaQualificationKind binds optional risky storage modes to exact shipped definitions, never to caller supplied capability/profile names.
// grafanaQualificationKind связывает дополнительные risky storage modes с exact shipped definitions, а не caller supplied capability/profile names.
func grafanaQualificationKind(key string) string {
	switch key {
	case "grafana-dashboard-annotations":
		return qualificationGrafanaAnnotations
	case "grafana-organization-rule-delete":
		return qualificationGrafanaRuleDelete
	}
	return ""
}
