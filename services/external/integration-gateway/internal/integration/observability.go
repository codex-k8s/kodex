package integration

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// observabilityEnvelope separates bounded successful data from warnings, infos and provider diagnostics; partial results never become a complete receipt.
// observabilityEnvelope отделяет ограниченные успешные данные от warnings, infos и diagnostics; частичный результат не становится полной квитанцией.
type observabilityEnvelope struct {
	Status    string          `json:"status"`
	Data      json.RawMessage `json:"data"`
	Warnings  []string        `json:"warnings"`
	Infos     []string        `json:"infos"`
	Error     string          `json:"error"`
	ErrorType string          `json:"errorType"`
}

// observabilityGET uses only the mandatory operator-pinned native transport and a finite server-selected tenant header; neither missing transport nor rejection can fall back to direct/core HTTP.
// observabilityGET использует только обязательный operator-pinned native transport и конечный server-selected tenant header; fallback к direct/core HTTP отсутствует.
func (adapter *Adapter) observabilityGET(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, path string, query url.Values, probe bool) (*providerResponse, error) {
	if adapter.nativeHTTPClient == nil {
		return nil, &SafeError{Code: providerUnavailableCode}
	}
	call := providerCall{BaseURL: configuration["base_url"], Method: http.MethodGet, Path: path, Query: query, AuthScheme: "BEARER", Credential: request.Credential, Capability: capability, Client: adapter.nativeHTTPClient, invalidCredentialProbe: probe}
	if request.DefinitionKey == "thanos" {
		call.tenantHeader, call.tenantID = "THANOS-TENANT", configuration["tenant_id"]
	}
	return adapter.callProviderResponse(ctx, call)
}

// observabilityData rejects duplicate exact keys before decoding while allowing distinct case-sensitive metric labels, and refuses all warning/error envelopes without forwarding diagnostics.
// observabilityData отклоняет точные повторы ключей до decode, сохраняя регистрозависимые metric labels, и закрывает warning/error envelopes без выдачи diagnostics.
func observabilityData(raw []byte, target any) error {
	var envelope observabilityEnvelope
	if integrationpackage.ValidateProviderJSON(raw) != nil || observabilityDecode(raw, &envelope) != nil || envelope.Status != "success" || len(envelope.Warnings) != 0 || len(envelope.Infos) != 0 || envelope.Error != "" || envelope.ErrorType != "" || len(envelope.Data) == 0 || string(envelope.Data) == "null" || observabilityDecode(envelope.Data, target) != nil {
		return &SafeError{Code: providerResponseInvalidCode}
	}
	return nil
}

// observabilityHealth proves exact version compatibility and observed rejection of a fresh invalid credential under the same path; it makes no user-identity, permission or tenant-isolation claim.
// observabilityHealth доказывает совместимость точной версии и наблюдённый отказ свежего неверного credential на том же path; identity, permissions и tenant isolation не утверждаются.
func (adapter *Adapter) observabilityHealth(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string) (map[string]string, error) {
	const path = "/api/v1/status/buildinfo"
	response, err := adapter.observabilityGET(ctx, request, capability, configuration, path, nil, false)
	if err != nil {
		return nil, err
	}
	var info struct {
		Version string `json:"version"`
	}
	if err := observabilityData(response.Body, &info); err != nil {
		return nil, err
	}
	expected := "3.14.0"
	if request.DefinitionKey == "thanos" {
		expected = "0.42.5"
	}
	if info.Version != expected {
		return nil, &SafeError{Code: providerCapabilityUnsupportedCode}
	}
	_, err = adapter.observabilityGET(ctx, request, capability, configuration, path, nil, true)
	var safe *SafeError
	if !errors.As(err, &safe) || (safe.AuthenticationHTTPStatus != http.StatusUnauthorized && safe.AuthenticationHTTPStatus != http.StatusForbidden) {
		if err != nil && (!errors.As(err, &safe) || safe.Code == providerUnavailableCode) {
			return nil, err
		}
		return nil, &SafeError{Code: providerConfigurationInvalidCode}
	}
	return map[string]string{"provider": request.DefinitionKey, "version": info.Version, "scope_mode": "WHOLE_ENDPOINT", "authentication_rejection": "OBSERVED", "tenant_isolation": "UNKNOWN"}, nil
}

// observabilityTime accepts an explicit bounded UTC instant only, avoiding provider-relative now defaults and ambiguous floating-point timestamp inputs.
// observabilityTime принимает только явный ограниченный момент UTC, исключая provider-relative now и неоднозначные floating-point timestamps.
func observabilityTime(input map[string]any, key string) (time.Time, error) {
	value, ok := input[key].(string)
	instant, err := time.Parse(time.RFC3339Nano, value)
	if !ok || err != nil || !strings.HasSuffix(value, "Z") || instant.Year() < 2000 || instant.Year() > 2200 {
		return time.Time{}, &SafeError{Code: providerRequestRejectedCode}
	}
	return instant, nil
}

// observabilityQuery derives only the finite Prometheus-compatible parameters and enforces a 24-hour, 1000-step budget before any query request.
// observabilityQuery выводит только конечные Prometheus-compatible параметры и проверяет бюджет 24 часа/1000 шагов до query request.
func observabilityQuery(operation string, input map[string]any) (string, url.Values, error) {
	query := url.Values{}
	var path string
	switch operation {
	case "query.instant", "query.range":
		value, _ := input["query"].(string)
		if value == "" || len(value) > 4096 || validateObservabilityPromQL(value, false) != nil {
			return "", nil, &SafeError{Code: providerRequestRejectedCode}
		}
		query.Set("query", value)
		query.Set("timeout", "5s")
		query.Set("lookback_delta", "5m")
		query.Set("limit", "50")
		path = "/api/v1/query"
		if operation == "query.instant" {
			instant, err := observabilityTime(input, "time")
			if err != nil {
				return "", nil, err
			}
			query.Set("time", instant.Format(time.RFC3339Nano))
		} else {
			path = "/api/v1/query_range"
		}
	case "labels.list":
		path = "/api/v1/labels"
	case "label.values":
		label, _ := input["label"].(string)
		if !observabilityLabelName(label) {
			return "", nil, &SafeError{Code: providerRequestRejectedCode}
		}
		path = "/api/v1/label/" + label + "/values"
	case "series.list":
		path = "/api/v1/series"
	case "rules.list":
		path = "/api/v1/rules"
	case "alerts.list":
		path = "/api/v1/alerts"
	default:
		return "", nil, &SafeError{Code: providerCapabilityUnsupportedCode}
	}
	if operation == "query.range" || operation == "labels.list" || operation == "label.values" || operation == "series.list" {
		start, err := observabilityTime(input, "start")
		if err != nil {
			return "", nil, err
		}
		end, err := observabilityTime(input, "end")
		if err != nil {
			return "", nil, err
		}
		if end.Before(start) || end.Sub(start) > 24*time.Hour {
			return "", nil, &SafeError{Code: providerRequestRejectedCode}
		}
		query.Set("start", start.Format(time.RFC3339Nano))
		query.Set("end", end.Format(time.RFC3339Nano))
		if operation == "query.range" {
			step := observabilityStep(input["step_seconds"])
			if step < 1 || step > 3600 || end.Sub(start)/time.Second/time.Duration(step)+1 > 1000 {
				return "", nil, &SafeError{Code: providerRequestRejectedCode}
			}
			query.Set("step", strconv.Itoa(step))
		} else {
			selector, _ := input["match"].(string)
			if selector == "" || len(selector) > 2048 || validateObservabilityPromQL(selector, true) != nil {
				return "", nil, &SafeError{Code: providerRequestRejectedCode}
			}
			query.Set("match[]", selector)
		}
	}
	return path, query, nil
}

// observabilityLabelName admits the finite legacy label-name grammar for endpoint segments without permitting path/query injection.
// observabilityLabelName допускает конечную legacy-грамматику label name для endpoint segment без path/query injection.
func observabilityLabelName(value string) bool {
	if len(value) < 1 || len(value) > 100 {
		return false
	}
	for i, c := range value {
		if !(c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// observabilityStep accepts only an exact integral scalar, so a recovered float or malformed number cannot silently shrink a query's declared step.
// observabilityStep принимает только точный целый scalar, чтобы recovered float или повреждённое число не изменили заявленный шаг запроса.
func observabilityStep(raw any) int {
	switch value := raw.(type) {
	case int:
		return value
	case int64:
		if value >= 1 && value <= 3600 {
			return int(value)
		}
	case float64:
		if value >= 1 && value <= 3600 && value == math.Trunc(value) {
			return int(value)
		}
	case json.Number:
		parsed, err := strconv.Atoi(string(value))
		if err == nil {
			return parsed
		}
	}
	return 0
}

// observabilityLabels validates bounded case-sensitive label maps and redacts values on known credential-bearing label names; it is not a general secret detector.
// observabilityLabels проверяет ограниченные регистрозависимые labels и скрывает значения известных credential-bearing имён; универсальным поиском секретов не является.
func observabilityLabels(labels map[string]string) bool {
	if labels == nil || len(labels) > 30 {
		return false
	}
	for key, value := range labels {
		if key == "" || len(key) > 100 || len(value) > 1024 || strings.ContainsAny(key+value, "\r\n\x00") {
			return false
		}
		lower := strings.ToLower(key)
		for _, sensitive := range []string{"authorization", "cookie", "password", "secret", "token", "dsn", "connection_string"} {
			if strings.Contains(lower, sensitive) {
				labels[key] = "[REDACTED]"
				break
			}
		}
	}
	return true
}

// executeObservability consumes one closed endpoint-scoped metrics operation with exact personal profile and bounded projections; no provider URL or raw config enters the dispatcher.
// executeObservability исполняет одну закрытую endpoint-scoped metrics operation с точным personal profile и ограниченной проекцией; provider URL и raw config в dispatcher не поступают.
func (adapter *Adapter) executeObservability(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string) (Result, error) {
	if request.DefinitionKey == "loki" || request.DefinitionKey == "loki-ruler-namespace" || request.DefinitionKey == "loki-ruler-namespace-read" {
		return adapter.executeLoki(ctx, request, capability, configuration)
	}
	if strings.HasPrefix(request.DefinitionKey, "grafana-") {
		return adapter.executeGrafana(ctx, request, capability, configuration)
	}
	contract, exists := integrationpackage.CredentialResourcePermissionContractFor(request.DefinitionKey, request.CapabilityKey)
	if !exists || contract.Operation != request.Operation || request.Risk != "READ" || request.ApprovalPolicy != "NONE" || configuration["scope_mode"] != "WHOLE_ENDPOINT" || request.PersonalCredentialAdmission && request.ResourcePermissionProfile != contract.Profile {
		return Result{}, &SafeError{Code: providerCapabilityUnsupportedCode}
	}
	health, err := adapter.observabilityHealth(ctx, request, capability, configuration)
	if err != nil {
		return Result{}, err
	}
	var projection any = health
	operation := strings.TrimPrefix(request.Operation, request.DefinitionKey+".")
	if operation != "connection.read" {
		path, query, err := observabilityQuery(operation, request.Input)
		if err != nil {
			return Result{}, err
		}
		if request.DefinitionKey == "thanos" {
			if operation != "rules.list" && operation != "alerts.list" {
				query.Set("partial_response", "false")
			}
			query.Del("limit")
			if strings.HasPrefix(operation, "query.") {
				query.Set("dedup", "true")
			}
		}
		response, err := adapter.observabilityGET(ctx, request, capability, configuration, path, query, false)
		if err != nil {
			return Result{}, err
		}
		projection, err = observabilityProjection(operation, response.Body)
		if err != nil {
			return Result{}, err
		}
	}
	raw, err := json.Marshal(projection)
	if err != nil || len(raw) > 65536 {
		return Result{}, &SafeError{Code: providerResponseInvalidCode}
	}
	return providerResult(request, request.DefinitionKey+"-endpoint", map[string]string{"text": string(raw)})
}

// observabilityNumber validates one string-encoded sample value, preserving native NaN/Inf semantics without accepting diagnostic strings.
// observabilityNumber проверяет строковое sample value, сохраняя native NaN/Inf semantics без допуска diagnostic strings.
func observabilityNumber(value string) bool {
	_, err := strconv.ParseFloat(value, 64)
	return value != "" && len(value) <= 100 && err == nil
}

// observabilityTimestamp checks a finite positive JSON number without replacing its original decimal bytes in the resulting projection.
// observabilityTimestamp проверяет конечный положительный JSON number, сохраняя исходные decimal bytes в результирующей проекции.
func observabilityTimestamp(raw json.RawMessage) bool {
	if len(raw) == 0 || raw[0] == '"' || string(raw) == "null" {
		return false
	}
	var number json.Number
	if observabilityDecode(raw, &number) != nil {
		return false
	}
	value, err := strconv.ParseFloat(string(number), 64)
	return err == nil && value >= 0 && !math.IsInf(value, 0) && !math.IsNaN(value)
}
