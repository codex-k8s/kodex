package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/legacy"
)

// observabilityInvocation builds exact synthetic owner claim pins and whole-endpoint scope without loading any live configuration or key.
// observabilityInvocation строит точные synthetic owner claim pins и whole-endpoint scope без чтения live configuration либо key.
func observabilityInvocation(t *testing.T, adapter *Adapter, key, operation string, input map[string]any) Request {
	t.Helper()
	definition := adapter.definitions[key]
	request := invocationRequest(t, adapter.definitions["jira"], "jira.project.read", map[string]any{}, testCredential(t, adapter, "synthetic-observability-token"))
	capability, ok := definition.Capability(operation)
	if !ok {
		t.Fatal("missing observability capability")
	}
	configuration := map[string]string{"base_url": "https://" + key + ".example.invalid", "scope_mode": "WHOLE_ENDPOINT"}
	if strings.HasPrefix(key, "loki") {
		configuration["base_url"] = "https://loki.example.invalid"
		configuration["tenant_id"] = "fixture-tenant"
		if key != "loki" {
			configuration["rule_namespace"] = "fixture-rules"
			configuration["scope_mode"] = "API_MANAGED_NAMESPACE"
			if key == "loki-ruler-namespace-read" {
				configuration["scope_mode"] = "RULER_NAMESPACE_READ"
			}
		}
	}
	if key == "thanos" {
		configuration["tenant_id"] = "fixture-tenant"
	}
	if strings.HasPrefix(key, "grafana-") {
		configuration = map[string]string{"base_url": "https://grafana.example.invalid", "organization_id": "1", "datasource_uid": "fixture-prometheus", "scope_mode": "WHOLE_ORGANIZATION"}
		if key == "grafana-dashboard" {
			configuration["scope_mode"] = "DASHBOARD_READ"
			configuration["dashboard_uid"] = "fixture-dashboard"
		}
		if key == "grafana-dashboard-annotations" {
			delete(configuration, "datasource_uid")
			configuration["scope_mode"] = "DASHBOARD_ANNOTATIONS"
			configuration["dashboard_uid"] = "fixture-dashboard"
		}
		if key == "grafana-organization-rule-delete" {
			delete(configuration, "datasource_uid")
			configuration["scope_mode"] = "API_ONLY_ORGANIZATION_RULE_DELETE"
		}

	}

	request.DefinitionPackage, _ = json.Marshal(definition)
	request.DefinitionKey, request.DefinitionVersion, request.DefinitionDigest = key, definition.Metadata.Version, definition.Digest
	request.CapabilityKey, request.Operation, request.Risk, request.ApprovalPolicy = operation, operation, capability.Risk, capability.ApprovalPolicy
	request.ResourceKind = capability.ResourceScope.Kind
	request.ResourceScope, _ = capability.ResourceScopeValues(configuration)
	raw, _ := json.Marshal(request.ResourceScope)
	digest := sha256.Sum256(raw)
	request.ResourceScopeDigest = hex.EncodeToString(digest[:])
	raw, _ = json.Marshal(input)
	canonical, err := capability.ValidateInput(raw)
	if err != nil {
		t.Fatal(err)
	}
	digest = sha256.Sum256(canonical)
	request.InputDigest = hex.EncodeToString(digest[:])
	request.Input = input
	request.Configuration = map[string]any{}
	for k, v := range configuration {
		request.Configuration[k] = v
	}
	return request
}

// observabilityFixtureInput assigns one bounded scenario to every closed operation; unknown names fail coverage instead of being silently skipped.
// observabilityFixtureInput назначает один ограниченный scenario каждой closed operation; неизвестные имена ломают покрытие вместо молчаливого пропуска.
func observabilityFixtureInput(t *testing.T, operation string) map[string]any {
	t.Helper()
	input := map[string]any{}
	switch operation {
	case "connection.read", "rules.list", "alerts.list":
	case "query.instant":
		input["query"] = "up"
		input["time"] = "2026-11-14T00:01:00Z"
	case "query.range":
		input["query"] = "up"
		input["start"] = "2026-11-14T00:00:00Z"
		input["end"] = "2026-11-14T00:01:00Z"
		input["step_seconds"] = 60
	case "labels.list", "label.values", "series.list":
		input["match"] = "up{job=\"fixture\"}"
		input["start"] = "2026-11-14T00:00:00Z"
		input["end"] = "2026-11-14T00:01:00Z"
		if operation == "label.values" {
			input["label"] = "job"
		}
	default:
		t.Fatal("advertised observability operation lacks scenario")
	}
	return input
}

// observabilityFixtureResponse independently declares documented request methods/paths/query and provider responses, including unprojected sensitive metadata.
// observabilityFixtureResponse независимо задаёт documented methods/paths/query и provider responses, включая sensitive metadata, которые нельзя выдавать.
func observabilityFixtureResponse(t *testing.T, key, operation string, r *http.Request) string {
	t.Helper()
	expected := url.Values{}
	path := ""
	switch operation {
	case "query.instant":
		path = "/api/v1/query"
		expected = url.Values{"query": {"up"}, "time": {"2026-11-14T00:01:00Z"}, "timeout": {"5s"}, "lookback_delta": {"5m"}, "limit": {"50"}}
	case "query.range":
		path = "/api/v1/query_range"
		expected = url.Values{"query": {"up"}, "start": {"2026-11-14T00:00:00Z"}, "end": {"2026-11-14T00:01:00Z"}, "step": {"60"}, "timeout": {"5s"}, "lookback_delta": {"5m"}, "limit": {"50"}}
	case "labels.list", "label.values", "series.list":
		path = map[string]string{"labels.list": "/api/v1/labels", "label.values": "/api/v1/label/job/values", "series.list": "/api/v1/series"}[operation]
		expected = url.Values{"match[]": {"up{job=\"fixture\"}"}, "start": {"2026-11-14T00:00:00Z"}, "end": {"2026-11-14T00:01:00Z"}}
	case "rules.list":
		path = "/api/v1/rules"
	case "alerts.list":
		path = "/api/v1/alerts"
	default:
		t.Fatal("unknown fixture operation")
	}
	if key == "thanos" {
		expected.Del("limit")
		if operation != "rules.list" && operation != "alerts.list" {
			expected.Set("partial_response", "false")
		}
		if strings.HasPrefix(operation, "query.") {
			expected.Set("dedup", "true")
		}
	}
	if r.Method != http.MethodGet || r.URL.Path != path || r.URL.Query().Encode() != expected.Encode() || r.Body != nil && r.ContentLength != 0 {
		t.Error("provider request differs from independent versioned fixture")
	}
	data := map[string]string{
		"query.instant": `{"resultType":"vector","result":[{"metric":{"__name__":"up","job":"fixture","a":"lower","A":"upper","api_token":"must-redact"},"value":[1794614460,"1"],"diagnostic":"must-drop"}]}`,
		"query.range":   `{"resultType":"matrix","result":[{"metric":{"__name__":"up","job":"fixture"},"values":[[1794614400,"1"],[1794614460,"2"]]}]}`,
		"labels.list":   `["__name__","job"]`, "label.values": `["fixture"]`, "series.list": `[{"__name__":"up","job":"fixture"}]`,
		"rules.list":  `{"groups":[{"name":"fixture","file":"must-drop","rules":[{"name":"recorded_up","type":"recording","health":"ok","labels":{},"query":"must-drop","lastError":"must-drop"}]}]}`,
		"alerts.list": `{"alerts":[{"labels":{"alertname":"Fixture"},"state":"firing","value":"1","annotations":{"secret":"must-drop"},"generatorURL":"https://must-drop.invalid"}]}`,
	}[operation]
	return `{"status":"success","data":` + data + `}`
}

// observabilityValidateWire checks real consumer requests and provider responses against a separate pinned-version OpenAPI subset; fixture parameter assertions remain independently stricter.
// observabilityValidateWire проверяет реальные consumer requests и provider responses по отдельному pinned-version OpenAPI subset; независимые fixture assertions дополнительно ограничивают параметры.
func observabilityValidateWire(t *testing.T, r *http.Request, status int, body string) {
	validateDocumentedMetricsWire(t, "prometheus-compatible", r, status, body)
}

// validateDocumentedMetricsWire checks only repository-owned finite provider documents with references disabled.
// validateDocumentedMetricsWire проверяет только repo-owned finite provider documents с отключёнными внешними references.
func validateDocumentedMetricsWire(t *testing.T, key string, r *http.Request, status int, body string, contentTypes ...string) {
	t.Helper()
	_, document := documentedProviderContract(t, key)
	document.Servers = openapi3.Servers{{URL: r.URL.Scheme + "://" + r.URL.Host}}
	router, err := legacy.NewRouter(document)
	if err != nil {
		t.Fatal(err)
	}
	route, params, err := router.FindRoute(r)
	if err != nil {
		t.Fatal(err)
	}
	input := &openapi3filter.RequestValidationInput{Request: r, PathParams: params, Route: route, Options: &openapi3filter.Options{AuthenticationFunc: func(context.Context, *openapi3filter.AuthenticationInput) error { return nil }}}
	if err := openapi3filter.ValidateRequest(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	contentType := "application/json"
	if len(contentTypes) > 0 {
		contentType = contentTypes[0]
	}
	output := &openapi3filter.ResponseValidationInput{RequestValidationInput: input, Status: status, Header: http.Header{"Content-Type": {contentType}}}
	output.SetBodyBytes([]byte(body))
	if err := openapi3filter.ValidateResponse(t.Context(), output); err != nil {
		t.Fatal(err)
	}
}

// testObservabilityCapabilities runs every advertised operation through package/configuration/credential validation and the exact native HTTP consumer, then validates response and receipt pins.
// testObservabilityCapabilities исполняет все advertised operations через package/configuration/credential validation и exact native HTTP consumer, проверяя проекцию и receipt pins.
func testObservabilityCapabilities(t *testing.T, key string) {
	t.Helper()
	for _, capability := range testAdapter(t).definitions[key].Spec.Capabilities {
		t.Run(capability.Operation, func(t *testing.T) {
			adapter := testAdapter(t)
			operation := strings.TrimPrefix(capability.Operation, key+".")
			calls, probes := 0, 0
			adapter.nativeHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Scheme != "https" || r.URL.Host != key+".example.invalid" || r.Header.Get("X-Scope-OrgID") != "" {
					t.Error("native transport target/header crossed configured boundary")
				}
				if (key == "thanos") != (r.Header.Get("THANOS-TENANT") == "fixture-tenant") {
					t.Error("tenant routing context mismatch")
				}
				auth := r.Header.Get("Authorization")
				status := 200
				body := ""
				if strings.HasPrefix(auth, "Bearer kodex-invalid-") {
					probes++
					status = 401
					body = `{"error":"must-drop"}`
				} else if auth != "Bearer synthetic-observability-token" {
					t.Error("selected credential not used")
				}
				if r.URL.Path == "/api/v1/status/buildinfo" {
					if r.Method != "GET" || len(r.URL.Query()) != 0 {
						t.Error("health path differs from documented contract")
					}
					if status == 200 {
						version := "3.14.0"
						if key == "thanos" {
							version = "0.42.5"
						}
						body = `{"status":"success","data":{"version":"` + version + `","revision":"must-drop"}}`
					}
				} else {
					if status != 200 {
						t.Error("invalid-key probe reached data query")
					}
					body = observabilityFixtureResponse(t, key, operation, r)
				}
				observabilityValidateWire(t, r, status, body)
				return &http.Response{Request: r, StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			request := observabilityInvocation(t, adapter, key, capability.Operation, observabilityFixtureInput(t, operation))
			request.PersonalCredentialAdmission = true
			request.ResourcePermissionProfile = "OBSERVABILITY_TYPED_RESOURCE_READ_V1"
			result, err := adapter.Execute(context.Background(), request)
			wantCalls := 3
			if operation == "connection.read" {
				wantCalls = 2
			}
			if err != nil || calls != wantCalls || probes != 1 || result.Receipt.EffectKey != request.EffectKey || result.Receipt.InputDigest != request.InputDigest || result.Receipt.ResponseDigest == "" || strings.Contains(result.Summary, "must-drop") || strings.Contains(result.Summary, "must-redact") {
				t.Fatalf("closed observability consumer failed: %v, calls=%d", err, calls)
			}
			if operation == "query.instant" && (!strings.Contains(result.Summary, "REDACTED") || !strings.Contains(result.Summary, "upper") || !strings.Contains(result.Summary, "lower")) {
				t.Fatal("case-sensitive labels or redaction lost")
			}
		})
	}
}

// TestObservabilityClosedConsumer qualifies both endpoint contracts without live requests, arbitrary URLs or credential fallback.
// TestObservabilityClosedConsumer квалифицирует оба endpoint contracts без live requests, arbitrary URLs и credential fallback.
func TestObservabilityClosedConsumer(t *testing.T) {
	for _, key := range []string{"prometheus", "thanos"} {
		t.Run(key, func(t *testing.T) { testObservabilityCapabilities(t, key) })
	}
}

// TestObservabilityHealthNeverInventsIsolation rejects unsupported versions and open or indeterminate authentication boundaries before any data query is issued.
// TestObservabilityHealthNeverInventsIsolation закрывает unsupported versions и открытые/неопределённые authentication boundaries до любого data query.
func TestObservabilityHealthNeverInventsIsolation(t *testing.T) {
	for _, scenario := range []string{"open-auth", "probe-500", "wrong-version", "good"} {
		t.Run(scenario, func(t *testing.T) {
			adapter := testAdapter(t)
			queries := 0
			adapter.nativeHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				status := 200
				version := "3.14.0"
				if scenario == "wrong-version" {
					version = "3.15.1"
				}
				if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer kodex-invalid-") {
					if scenario == "probe-500" {
						status = 500
					} else if scenario != "open-auth" {
						status = 403
					}
				}
				if r.URL.Path != "/api/v1/status/buildinfo" {
					queries++
				}
				return &http.Response{Request: r, StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"status":"success","data":{"version":"` + version + `"}}`))}, nil
			})}
			request := observabilityInvocation(t, adapter, "prometheus", "prometheus.connection.read", map[string]any{})
			result, err := adapter.Execute(context.Background(), request)
			if (err == nil) != (scenario == "good") || queries != 0 {
				t.Fatal("health accepted an unqualified authentication/version boundary", err)
			}
			if err == nil && (!strings.Contains(result.Summary, `UNKNOWN`) || !strings.Contains(result.Summary, `WHOLE_ENDPOINT`)) {
				t.Fatal("health invented tenant isolation")
			}
		})
	}
}

// TestObservabilityProjectionRejectsAmbiguity tests partial/error envelopes, case aliases, repeated keys, invalid native histograms and secret-bearing metadata projections without a transport dependency.
// TestObservabilityProjectionRejectsAmbiguity проверяет partial/error envelopes, case aliases, повторы, invalid histograms и sensitive metadata projections без transport dependency.
func TestObservabilityProjectionRejectsAmbiguity(t *testing.T) {
	for _, data := range []string{
		`{"status":"success","status":"error","data":{}}`,
		`{"STATUS":"success","data":{"resultType":"vector","result":[]}}`,
		`{"status":"success","warnings":["partial"],"data":{"resultType":"vector","result":[]}}`,
		`{"status":"success","data":{"resultType":"vector","ResultType":"vector","result":[]}}`,
		`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"job":"a","job":"b"},"value":[1,"1"]}]}}`,
		`{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":["1","1"]}]}}`,
		`{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"histogram":[1,{"count":"1","sum":"2","buckets":[[4,"0","1","1"]]}]}]}}`,
	} {
		if _, err := observabilityProjection("query.instant", []byte(data)); err == nil {
			t.Fatal("ambiguous/partial metrics response accepted")
		}
	}
	valid := `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"a":"one","A":"two"},"histogram":[1,{"count":"1","sum":"2","buckets":[[0,"-Inf","+Inf","1"]],"secret":"must-drop"}]}]}}`
	projection, err := observabilityProjection("query.instant", []byte(valid))
	raw, _ := json.Marshal(projection)
	if err != nil || strings.Contains(string(raw), "must-drop") {
		t.Fatal("native histogram compatibility/projection failed", err)
	}
}

// TestObservabilityPromQLASTBounds exercises semantic time modifiers and cumulative/nested histories that cannot be bounded by HTTP range or response-size checks.
// TestObservabilityPromQLASTBounds проверяет semantic time modifiers и суммарные/nested histories, которые нельзя ограничить HTTP range либо размером ответа.
func TestObservabilityPromQLASTBounds(t *testing.T) {
	for _, value := range []string{"up", "sum(rate(http_requests_total[5m]))", "up offset 1h", "rate(up[1h] offset 1h)", "sum_over_time(up[30m:1m])"} {
		if err := validateObservabilityPromQL(value, false); err != nil {
			t.Fatalf("bounded PromQL rejected: %s", value)
		}
	}
	for _, value := range []string{"rate(up[100y])", "up offset 100y", "up offset -100y", "up @ 1", "up @ start()", "sum_over_time(up[1h:])", "sum_over_time(up[24h:1m])", "sum_over_time(rate(up[23h])[1h:1m] offset 1h)", "sum_over_time(sum_over_time(up[5m:1m])[5m:1m])", "sum(", "up +"} {
		if err := validateObservabilityPromQL(value, false); err == nil {
			t.Fatalf("unbounded/malformed PromQL accepted: %s", value)
		}
	}
	if validateObservabilityPromQL(`up{job="fixture"}`, true) != nil || validateObservabilityPromQL(`rate(up[5m])`, true) == nil {
		t.Fatal("metadata selector accepted expression or rejected bounded selector")
	}
}

// TestObservabilityAdmissionAndBudgetsCloseBeforeQuery verifies exact personal profile, missing native transport and semantic input budgets; a healthy provider never repairs an invalid scope or profile.
// TestObservabilityAdmissionAndBudgetsCloseBeforeQuery проверяет exact personal profile, missing native transport и semantic input budgets; здоровый provider не исправляет invalid scope либо profile.
func TestObservabilityAdmissionAndBudgetsCloseBeforeQuery(t *testing.T) {
	for _, scenario := range []string{"profile", "native", "large-range", "large-history", "large-steps", "arbitrary-url"} {
		t.Run(scenario, func(t *testing.T) {
			adapter := testAdapter(t)
			queries := 0
			adapter.nativeHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/api/v1/status/buildinfo" {
					queries++
				}
				status := 200
				if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer kodex-invalid-") {
					status = 401
				}
				return &http.Response{Request: r, StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"status":"success","data":{"version":"3.14.0"}}`))}, nil
			})}
			input := observabilityFixtureInput(t, "query.range")
			switch scenario {
			case "large-range":
				input["end"] = "2026-11-16T00:00:00Z"
			case "large-history":
				input["query"] = "rate(up[100y])"
			case "large-steps":
				input["end"] = "2026-11-14T01:00:00Z"
				input["step_seconds"] = 1
			case "arbitrary-url":
				input["query"] = "https://untrusted.example.invalid/api"
			}
			request := observabilityInvocation(t, adapter, "prometheus", "prometheus.query.range", input)
			if scenario == "profile" {
				request.PersonalCredentialAdmission = true
				request.ResourcePermissionProfile = "SENTRY_PROJECT_OBJECT_READ_V1"
			}
			if scenario == "native" {
				adapter.nativeHTTPClient = nil
			}
			if _, err := adapter.Execute(t.Context(), request); err == nil || queries != 0 {
				t.Fatal("invalid query/profile/transport reached data endpoint", err)
			}
		})
	}
}

// TestObservabilityPersonalHealthChecksExactSelectedKey tests the existing personal verification consumer and confirms only the inputless health capability, without inventing token identity or tenant evidence.
// TestObservabilityPersonalHealthChecksExactSelectedKey проверяет существующий personal verification consumer и подтверждает только inputless health capability без выдуманной token identity либо tenant evidence.
func TestObservabilityPersonalHealthChecksExactSelectedKey(t *testing.T) {
	for _, key := range []string{"prometheus", "thanos"} {
		t.Run(key, func(t *testing.T) {
			adapter := testAdapter(t)
			calls := 0
			adapter.nativeHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				status := 200
				version := "3.14.0"
				if key == "thanos" {
					version = "0.42.5"
				}
				if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer kodex-invalid-") {
					status = 401
				} else if r.Header.Get("Authorization") != "Bearer synthetic-observability-token" {
					t.Error("personal verification changed selected key")
				}
				if r.Method != "GET" || r.URL.Path != "/api/v1/status/buildinfo" {
					t.Error("personal health expanded to effect or arbitrary endpoint")
				}
				return &http.Response{Request: r, StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"status":"success","data":{"version":"` + version + `"}}`))}, nil
			})}
			request := observabilityInvocation(t, adapter, key, key+".connection.read", map[string]any{})
			request.CapabilityKey, request.Operation, request.GrantRef, request.GrantVersion = "", "", "", 0
			result, err := adapter.VerifyPersonalCredential(t.Context(), request)
			if err != nil || calls != 2 || len(result.ConfirmedCapabilityKeys) != 1 || result.ConfirmedCapabilityKeys[0] != key+".connection.read" || len(result.ObservedPermissionProfiles) != 0 || len(result.DeniedCapabilityKeys) != 0 {
				t.Fatal("personal endpoint verification invented extra permission evidence", err)
			}
		})
	}
}
