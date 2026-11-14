package integration

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"go.yaml.in/yaml/v3"
)

// lokiFixtureInput defines independent bounded inputs for every advertised Loki operation without any live endpoint or credential.
// lokiFixtureInput задаёт independent bounded inputs для каждой advertised Loki operation без live endpoint либо credential.
func lokiFixtureInput(t *testing.T, operation string) map[string]any {
	t.Helper()
	switch operation {
	case "loki.connection.read", "loki.ruler.group.list":
		return map[string]any{}
	case "loki.ruler.group.read", "loki.ruler.group.delete":
		return map[string]any{"group": "fixture-group"}
	case "loki.ruler.group.replace":
		return map[string]any{"group": "fixture-group", "alert": "FixtureAlert", "query": `sum(rate({job="fixture"}[5m])) > 1`, "for_seconds": 60, "severity": "warning"}
	case "loki.logs.query":
		return map[string]any{"query": `{job="fixture"} | json | level="error"`, "start": "2026-11-14T00:00:00Z", "end": "2026-11-14T00:01:00Z"}
	case "loki.query.instant", "loki.query.range":
		input := observabilityFixtureInput(t, strings.TrimPrefix(operation, "loki."))
		input["query"] = `sum(rate({job="fixture"}[5m]))`
		return input
	case "loki.labels.list", "loki.label.values", "loki.series.list":
		input := observabilityFixtureInput(t, strings.TrimPrefix(operation, "loki."))
		input["match"] = `{job="fixture"}`
		return input
	}
	t.Fatal("advertised Loki operation lacks scenario")
	return nil
}

// lokiFixture stores only synthetic rule YAML and forced failure modes; wire assertions are independent from consumer helpers and reject every unregistered path/body/header.
// lokiFixture хранит только synthetic rule YAML и forced failure modes; wire assertions независимы от consumer helpers и отвергают unregistered path/body/header.
type lokiFixture struct {
	t                   *testing.T
	operation, scenario string
	effects, reads      int
	stored              []byte
	beforeEffect        func()
}

// roundTrip serves exact-version Loki shapes, real-key/invalid-key authentication and authoritative YAML readback; no request can leave the synthetic selected tenant/namespace.
// roundTrip обслуживает exact-version Loki shapes, real-key/invalid-key authentication и authoritative YAML readback; request не выходит за synthetic selected tenant/namespace.
func (f *lokiFixture) roundTrip(r *http.Request) (*http.Response, error) {
	f.t.Helper()
	if r.URL.Host != "loki.example.invalid" || r.URL.Scheme != "https" || r.Header.Get("X-Scope-OrgID") != "fixture-tenant" || r.Header.Get("THANOS-TENANT") != "" {
		f.t.Fatal("Loki selected target/tenant mismatch")
	}
	status, body, mime := 200, "", "application/json"
	if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer kodex-invalid-") {
		if r.Method != "GET" || r.URL.Path != "/loki/api/v1/labels" {
			f.t.Fatal("Loki invalid-key probe escaped fixed READ")
		}
		status = 401
		body = `{"error":"must-drop"}`
		if f.scenario == "open-auth" {
			status = 200
			body = `{"status":"success","data":[]}`
		}
		return &http.Response{Request: r, StatusCode: status, Header: http.Header{"Content-Type": {mime}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	}
	if r.Header.Get("Authorization") != "Bearer synthetic-observability-token" {
		f.t.Fatal("Loki selected credential mismatch")
	}
	if r.Method != "GET" {
		f.effects++
		if r.GetBody != nil || r.Header.Get("Idempotency-Key") != "" {
			f.t.Fatal("Loki mutation became replayable")
		}
		switch f.scenario {
		case "transport-loss":
			return nil, errors.New("synthetic transport loss")
		case "server-loss":
			return &http.Response{Request: r, StatusCode: 503, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("must-drop"))}, nil
		case "malformed":
			return &http.Response{Request: r, StatusCode: 202, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{"))}, nil
		}
		switch r.Method + " " + r.URL.Path {
		case "POST /loki/api/v1/rules/fixture-rules":
			if r.Header.Get("Content-Type") != "application/yaml" {
				f.t.Fatal("Loki lost native YAML media type")
			}
			raw, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(raw))
			var group map[string]any
			if yaml.Unmarshal(raw, &group) != nil || len(group) != 3 || group["name"] != "fixture-group" || group["interval"] != "1m" {
				f.t.Fatal("Loki group replacement escaped closed fields")
			}
			rules, ok := group["rules"].([]any)
			if !ok || len(rules) != 1 {
				f.t.Fatal("Loki replacement is not one explicit rule")
			}
			rule, ok := rules[0].(map[string]any)
			if !ok || len(rule) != 4 || rule["alert"] != "FixtureAlert" || rule["expr"] != `sum(rate({job="fixture"}[5m])) > 1` || rule["for"] != "60s" {
				f.t.Fatal("Loki replacement changed fixed rule")
			}
			f.stored = raw
		case "DELETE /loki/api/v1/rules/fixture-rules/fixture-group":
			if r.ContentLength > 0 {
				f.t.Fatal("Loki delete gained a body")
			}
		default:
			f.t.Fatal("Loki effect escaped namespace/operation registry")
		}
		status = 202
		body = `{"status":"success"}`
	} else {
		f.reads++
		switch r.URL.Path {
		case "/loki/api/v1/status/buildinfo":
			body = `{"version":"3.6.21","buildUser":"must-drop"}`
			if f.scenario == "unknown-version" {
				body = `{"version":"3.7.10"}`
			}
		case "/loki/api/v1/labels", "/loki/api/v1/label/job/values", "/loki/api/v1/series":
			if r.URL.Query().Get("start") == "" || r.URL.Query().Get("end") == "" {
				f.t.Fatal("Loki metadata lacks explicit time window")
			}
			body = `{"status":"success","data":["job"]}`
			if r.URL.Path == "/loki/api/v1/series" {
				if r.URL.Query().Get("match[]") != `{job="fixture"}` {
					f.t.Fatal("Loki metadata selector mismatch")
				}
				body = `{"status":"success","data":[{"job":"fixture"}]}`
			}
		case "/loki/api/v1/query", "/loki/api/v1/query_range":
			if r.URL.Query().Get("limit") != "100" || r.URL.Query().Get("direction") != "forward" || r.URL.Query().Has("timeout") || r.URL.Query().Has("lookback_delta") {
				f.t.Fatal("Loki query gained unsupported or unbounded parameters")
			}
			if deadline, ok := r.Context().Deadline(); !ok || time.Until(deadline) > 5*time.Second {
				f.t.Fatal("Loki query lacks bounded cancellation")
			}
			if f.operation == "loki.logs.query" {
				if r.URL.Query().Get("query") != `{job="fixture"} | json | level="error"` {
					f.t.Fatal("Loki pipeline rewritten")
				}
				body = `{"status":"success","data":{"resultType":"streams","result":[{"stream":{"job":"fixture","Authorization":"must-drop"},"values":[["1794614400000000000","Synthetic line",{"secret":"must-drop"}]]}],"stats":{"secret":"must-drop"}}}`
			} else {
				if r.URL.Query().Get("query") != `sum(rate({job="fixture"}[5m]))` {
					f.t.Fatal("Loki metric query rewritten")
				}
				body = `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"job":"fixture"},"value":[1794614400,"1"]}]}}`
				if r.URL.Path == "/loki/api/v1/query_range" {
					if r.URL.Query().Get("step") != "60" {
						f.t.Fatal("Loki metric step mismatch")
					}
					body = `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"job":"fixture"},"values":[[1794614400,"1"]]}]}}`
				}
			}
		case "/loki/api/v1/rules/fixture-rules", "/loki/api/v1/rules/fixture-rules/fixture-group":
			mime = "application/yaml"
			body = "name: fixture-group\ninterval: 1m\nrules:\n- alert: FixtureAlert\n  expr: 'sum(rate({job=\"fixture\"}[5m])) > 1'\n  for: 1m\n  labels:\n    severity: warning\n  annotations:\n    secret: must-drop\n"
			if len(f.stored) > 0 {
				body = string(f.stored)
			}
			if r.URL.Path == "/loki/api/v1/rules/fixture-rules" {
				var value any
				_ = yaml.Unmarshal([]byte(body), &value)
				raw, _ := yaml.Marshal(map[string]any{"fixture-rules": []any{value}})
				body = string(raw)
			}
			if f.scenario == "foreign-namespace" {
				body = "other-namespace: []\n"
			}
			if f.scenario == "yaml-alias" {
				body = "name: &a fixture-group\nrules: *a\n"
			}
			if f.scenario == "yaml-secret" {
				body = "name: fixture-group\nsecret: \"synthetic-observability-\\u0074oken\"\n"
			}
			if f.effects > 0 && f.scenario == "readback-denied" {
				status = 403
			}
		default:
			f.t.Fatal("Loki READ escaped closed registry")
		}
		if f.beforeEffect != nil && r.URL.Path == "/loki/api/v1/rules/fixture-rules" {
			f.beforeEffect()
			f.beforeEffect = nil
		}
	}
	validateDocumentedMetricsWire(f.t, "loki", r, status, body, mime)
	return &http.Response{Request: r, StatusCode: status, Header: http.Header{"Content-Type": {mime}}, Body: io.NopCloser(strings.NewReader(body))}, nil
}

// testLokiCapabilities executes every shipped Loki capability through real owner claim/package/profile validation and checks typed output, exact effect count and receipt pins.
// testLokiCapabilities выполняет каждую shipped Loki capability через real owner claim/package/profile validation, проверяя typed output, effect count и receipt pins.
func testLokiCapabilities(t *testing.T, key string) {
	t.Helper()
	for _, capability := range testAdapter(t).definitions[key].Spec.Capabilities {
		t.Run(capability.Operation, func(t *testing.T) {
			adapter := testAdapter(t)
			fixture := &lokiFixture{t: t, operation: capability.Operation}
			adapter.nativeHTTPClient = &http.Client{Transport: roundTripFunc(fixture.roundTrip)}
			request := observabilityInvocation(t, adapter, key, capability.Operation, lokiFixtureInput(t, capability.Operation))
			request.PersonalCredentialAdmission = true
			request.ResourcePermissionProfile = integrationpackage.CredentialResourcePermissionProfile(adapter.definitions[key], capability)
			if key == "loki-ruler-namespace" {
				installQualificationFixture(t, adapter, qualificationFixture(qualificationLokiRuler, request.Credential.ContentSHA256))
			}
			result, err := adapter.Execute(t.Context(), request)
			effects := 0
			if capability.Risk != "READ" {
				effects = 1
			}
			if err != nil || fixture.effects != effects || result.Receipt.InputDigest != request.InputDigest || result.Receipt.EffectKey != request.EffectKey || strings.Contains(result.Summary, "must-drop") {
				t.Fatalf("Loki typed consumer failed: %v effects=%d", err, fixture.effects)
			}
		})
	}
}

// TestLokiClosedConsumer covers full query/log/metadata and both read-only/qualified ruler namespace profiles without using live endpoints or keys.
// TestLokiClosedConsumer покрывает все query/log/metadata и read-only/qualified ruler namespace profiles без live endpoints либо keys.
func TestLokiClosedConsumer(t *testing.T) {
	for _, key := range []string{"loki", "loki-ruler-namespace-read", "loki-ruler-namespace"} {
		t.Run(key, func(t *testing.T) { testLokiCapabilities(t, key) })
	}
}

// TestLokiMutationUnknownAndWithdrawal preserves UNKNOWN for lost effects and refuses withdrawn/foreign/invalid YAML qualification before any mutation; recovery never resends the effect.
// TestLokiMutationUnknownAndWithdrawal сохраняет UNKNOWN при lost effects и отвергает withdrawn/foreign/invalid YAML qualification до mutation; recovery не повторяет effect.
func TestLokiMutationUnknownAndWithdrawal(t *testing.T) {
	for _, operation := range []string{"loki.ruler.group.replace", "loki.ruler.group.delete"} {
		for _, scenario := range []string{"transport-loss", "server-loss", "malformed", "readback-denied", "withdrawn", "foreign-namespace", "yaml-alias", "yaml-secret", "missing-proof", "unknown-version", "open-auth"} {
			if operation == "loki.ruler.group.delete" && scenario == "readback-denied" {
				continue
			}
			t.Run(operation+"/"+scenario, func(t *testing.T) {
				adapter := testAdapter(t)
				fixture := &lokiFixture{t: t, operation: operation, scenario: scenario}
				adapter.nativeHTTPClient = &http.Client{Transport: roundTripFunc(fixture.roundTrip)}
				request := observabilityInvocation(t, adapter, "loki-ruler-namespace", operation, lokiFixtureInput(t, operation))
				if scenario != "missing-proof" {
					path := installQualificationFixture(t, adapter, qualificationFixture(qualificationLokiRuler, request.Credential.ContentSHA256))
					if scenario == "withdrawn" {
						fixture.beforeEffect = func() { _ = os.Remove(path) }
					}
				}
				_, err := adapter.Execute(context.Background(), request)
				unknown := scenario == "transport-loss" || scenario == "server-loss" || scenario == "malformed" || scenario == "readback-denied"
				if err == nil || IsUnknownOutcome(err) != unknown || fixture.effects != map[bool]int{true: 1, false: 0}[unknown] {
					t.Fatalf("Loki failure boundary: %v effects=%d", err, fixture.effects)
				}
				if unknown {
					if _, err = adapter.Recover(t.Context(), request); !IsUnknownOutcome(err) || fixture.effects != 1 {
						t.Fatal("Loki recovery replayed effect")
					}
				}
			})
		}
	}
}
