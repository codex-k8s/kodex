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

// lokiHTTP selects only the pinned native transport and server-bound tenant; qualified ruler calls reread operator evidence immediately before each request and cannot outlive its expiry.
// lokiHTTP выбирает только pinned native transport и server-bound tenant; qualified ruler calls перечитывают operator evidence перед каждым request и не переживают его expiry.
func (adapter *Adapter) lokiHTTP(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, method, path string, query url.Values, body []byte, probe bool) (*providerResponse, error) {
	if adapter.nativeHTTPClient == nil {
		return nil, &SafeError{Code: providerUnavailableCode}
	}
	if request.DefinitionKey == "loki-ruler-namespace" {
		profile, err := adapter.operatorQualificationFor(request, configuration, qualificationLokiRuler)
		if err != nil {
			return nil, err
		}
		expiry, _ := time.Parse(time.RFC3339, profile.ValidUntil)
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, expiry)
		defer cancel()
	}
	return adapter.callProviderResponse(ctx, providerCall{BaseURL: configuration["base_url"], Method: method, Path: path, Query: query, lokiRulerYAML: body, AuthScheme: "BEARER", Credential: request.Credential, Capability: capability, Client: adapter.nativeHTTPClient, permissionEffect: request.permissionEffect, invalidCredentialProbe: probe, tenantHeader: "X-Scope-OrgID", tenantID: configuration["tenant_id"]})
}

// lokiHealth checks the exact runtime build plus real-key and invalid-key authentication on a bounded labels GET; operator tenant evidence remains an attestation, never a runtime-observed isolation claim.
// lokiHealth проверяет exact runtime build и real-key/invalid-key authentication на bounded labels GET; operator tenant evidence остаётся attestation, не runtime-observed isolation claim.
func (adapter *Adapter) lokiHealth(ctx context.Context, request Request, configuration map[string]string) (map[string]string, error) {
	capability, exists := adapter.definitions[request.DefinitionKey].Capability("loki.connection.read")
	if !exists || capability.Risk != "READ" {
		return nil, &SafeError{Code: providerCapabilityUnsupportedCode}
	}
	response, err := adapter.lokiHTTP(ctx, request, capability, configuration, http.MethodGet, "/loki/api/v1/status/buildinfo", nil, nil, false)
	if err != nil {
		return nil, err
	}
	var version struct {
		Version string `json:"version"`
	}
	if grafanaDecode(response.Body, &version) != nil {
		return nil, &SafeError{Code: providerResponseInvalidCode}
	}
	if version.Version != "3.6.21" {
		return nil, &SafeError{Code: providerCapabilityUnsupportedCode}
	}
	now := time.Now().UTC()
	query := url.Values{"start": {now.Add(-time.Minute).Format(time.RFC3339Nano)}, "end": {now.Format(time.RFC3339Nano)}}
	response, err = adapter.lokiHTTP(ctx, request, capability, configuration, http.MethodGet, "/loki/api/v1/labels", query, nil, false)
	if err != nil {
		return nil, err
	}
	if _, err = observabilityProjection("labels.list", response.Body); err != nil {
		return nil, err
	}
	_, err = adapter.lokiHTTP(ctx, request, capability, configuration, http.MethodGet, "/loki/api/v1/labels", query, nil, true)
	var safe *SafeError
	if !errors.As(err, &safe) || safe.AuthenticationHTTPStatus != http.StatusUnauthorized && safe.AuthenticationHTTPStatus != http.StatusForbidden {
		if err != nil && (!errors.As(err, &safe) || safe.Code == providerUnavailableCode) {
			return nil, err
		}
		return nil, &SafeError{Code: providerConfigurationInvalidCode}
	}
	if request.DefinitionKey != "loki" {
		probeRequest := request
		probeRequest.Operation = "loki.ruler.group.list"
		if _, _, err := adapter.lokiRuler(ctx, probeRequest, capability, configuration); err != nil {
			return nil, err
		}
	}
	health := map[string]string{"provider": "loki", "version": version.Version, "scope_mode": configuration["scope_mode"], "authentication_rejection": "OBSERVED", "tenant_isolation": "UNKNOWN", "server_cost_limits": "UNKNOWN"}
	if request.DefinitionKey == "loki-ruler-namespace" {
		health["operator_qualification"] = "OPERATOR_ATTESTED"
	}
	return health, nil
}

// lokiQuery derives fixed native endpoints from the finite operation registry, preserving original validated LogQL and explicit UTC range/step budgets; the HTTP deadline is not presented as a server computation-cost proof.
// lokiQuery выводит fixed native endpoints из конечного registry, сохраняя исходный validated LogQL и explicit UTC range/step budgets; HTTP deadline не выдаётся за server computation-cost proof.
func lokiQuery(operation string, input map[string]any) (string, url.Values, error) {
	query := url.Values{}
	path := ""
	kind := "metric"
	switch operation {
	case "loki.query.instant":
		path = "/loki/api/v1/query"
	case "loki.query.range", "loki.logs.query":
		path = "/loki/api/v1/query_range"
		if operation == "loki.logs.query" {
			kind = "logs"
		}
	case "loki.labels.list":
		path = "/loki/api/v1/labels"
	case "loki.label.values":
		label, _ := input["label"].(string)
		if !observabilityLabelName(label) {
			return "", nil, &SafeError{Code: providerRequestRejectedCode}
		}
		path = "/loki/api/v1/label/" + label + "/values"
	case "loki.series.list":
		path = "/loki/api/v1/series"
	default:
		return "", nil, &SafeError{Code: providerCapabilityUnsupportedCode}
	}
	if strings.HasPrefix(operation, "loki.query.") || operation == "loki.logs.query" {
		value, _ := input["query"].(string)
		if _, err := validateObservabilityLogQL(value, kind); err != nil {
			return "", nil, err
		}
		query.Set("query", value)
		query.Set("limit", "100")
		query.Set("direction", "forward")
	}
	if operation == "loki.query.instant" {
		instant, err := observabilityTime(input, "time")
		if err != nil {
			return "", nil, err
		}
		query.Set("time", instant.Format(time.RFC3339Nano))
		return path, query, nil
	}
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
	if operation == "loki.query.range" {
		step := observabilityStep(input["step_seconds"])
		if step < 1 || step > 3600 || end.Sub(start)/time.Second/time.Duration(step)+1 > 1000 {
			return "", nil, &SafeError{Code: providerRequestRejectedCode}
		}
		query.Set("step", strconv.Itoa(step))
	}
	if !strings.HasPrefix(operation, "loki.query.") && operation != "loki.logs.query" {
		selector, _ := input["match"].(string)
		if _, err = validateObservabilityLogQL(selector, "selector"); err != nil {
			return "", nil, err
		}
		if operation == "loki.series.list" {
			query.Set("match[]", selector)
		} else {
			query.Set("query", selector)
		}
	}
	return path, query, nil
}

// lokiStream is a typed bounded log result; optional structured metadata and execution statistics are deliberately absent from the projection.
// lokiStream задаёт typed bounded log result; optional structured metadata и execution statistics отсутствуют в projection.
type lokiStream struct {
	Stream map[string]string   `json:"stream"`
	Values [][]json.RawMessage `json:"values"`
}

// lokiLogsProjection checks explicit stream/time/text budgets and preserves raw log text only as untrusted data, without promoting its contents into instructions or authority.
// lokiLogsProjection проверяет explicit stream/time/text budgets и сохраняет log text только как недоверенные данные, не превращая содержимое в инструкции либо authority.
func lokiLogsProjection(raw []byte, input map[string]any) (any, error) {
	invalid := &SafeError{Code: providerResponseInvalidCode}
	var data struct {
		ResultType string       `json:"resultType"`
		Result     []lokiStream `json:"result"`
	}
	if observabilityData(raw, &data) != nil || data.ResultType != "streams" || data.Result == nil || len(data.Result) > 50 {
		return nil, invalid
	}
	start, err := observabilityTime(input, "start")
	if err != nil {
		return nil, err
	}
	end, err := observabilityTime(input, "end")
	if err != nil {
		return nil, err
	}
	count := 0
	for i := range data.Result {
		stream := &data.Result[i]
		if !observabilityLabels(stream.Stream) || stream.Values == nil {
			return nil, invalid
		}
		var previous int64
		for j, pair := range stream.Values {
			if len(pair) < 2 || len(pair) > 3 {
				return nil, invalid
			}
			var timestamp, line string
			if observabilityDecode(pair[0], &timestamp) != nil || observabilityDecode(pair[1], &line) != nil || line == "" || len(line) > 4096 || strings.ContainsRune(line, '\x00') {
				return nil, invalid
			}
			ns, err := strconv.ParseInt(timestamp, 10, 64)
			if err != nil || strconv.FormatInt(ns, 10) != timestamp || ns < start.UnixNano() || ns > end.UnixNano() || previous > ns {
				return nil, invalid
			}
			previous = ns
			stream.Values[j] = pair[:2]
			count++
			if count > 100 {
				return nil, invalid
			}
		}
	}
	return map[string]any{"result_type": "streams", "result": data.Result, "limit_reached": count == 100, "content_trust": "UNTRUSTED"}, nil
}

// executeLoki enforces the owner-derived profile before health and dispatches only closed query/ruler consumers; all effects remain single-attempt and generic recovery never resends them.
// executeLoki проверяет owner-derived profile до health и вызывает только closed query/ruler consumers; effects остаются single-attempt, generic recovery их не повторяет.
func (adapter *Adapter) executeLoki(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string) (Result, error) {
	contract, exists := integrationpackage.CredentialResourcePermissionContractFor(request.DefinitionKey, request.CapabilityKey)
	if !exists || contract.Operation != request.Operation || contract.Risk != request.Risk || contract.ApprovalPolicy != request.ApprovalPolicy || request.PersonalCredentialAdmission && request.ResourcePermissionProfile != contract.Profile {
		return Result{}, &SafeError{Code: providerCapabilityUnsupportedCode}
	}
	health, err := adapter.lokiHealth(ctx, request, configuration)
	if err != nil {
		return Result{}, err
	}
	var projection any = health
	reference := "loki-endpoint"
	if strings.HasPrefix(request.Operation, "loki.ruler.") {
		projection, reference, err = adapter.lokiRuler(ctx, request, capability, configuration)
	} else if request.Operation != "loki.connection.read" {
		path, query, e := lokiQuery(request.Operation, request.Input)
		if e != nil {
			return Result{}, e
		}
		var response *providerResponse
		queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		response, err = adapter.lokiHTTP(queryCtx, request, capability, configuration, http.MethodGet, path, query, nil, false)
		cancel()
		if err == nil {
			if request.Operation == "loki.logs.query" {
				projection, err = lokiLogsProjection(response.Body, request.Input)
			} else {
				projection, err = observabilityProjection(strings.TrimPrefix(request.Operation, "loki."), response.Body)
			}
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
