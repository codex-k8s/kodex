package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// grafanaFixtureSilenceID is a synthetic provider UUID with no live resource correspondence.
// grafanaFixtureSilenceID задаёт synthetic provider UUID без соответствия live resource.
const grafanaFixtureSilenceID = "10000000-0000-4000-8000-000000000001"

// grafanaFixtureInput assigns a bounded independent scenario to each advertised operation; unknown operations fail the exhaustive catalog check.
// grafanaFixtureInput назначает bounded independent scenario каждой advertised operation; неизвестные операции ломают exhaustive catalog check.
func grafanaFixtureInput(t *testing.T, operation string) map[string]any {
	t.Helper()
	input := map[string]any{}
	switch operation {
	case "grafana.connection.read", "grafana.dashboard.read", "grafana.datasource.read", "grafana.rule.list":
	case "grafana.datasource.query":
		return observabilityFixtureInput(t, "query.range")
	case "grafana.rule.create", "grafana.rule.update":
		input = map[string]any{"title": "Fixture rule", "folder_uid": "fixture-folder", "rule_group": "fixture-group", "query": "up", "comparator": "gt", "threshold": "0.5", "for_seconds": 60, "paused": false}
		if operation == "grafana.rule.update" {
			input["rule_uid"] = "fixture-rule"
		}
	case "grafana.rule.read", "grafana.rule.delete":
		input["rule_uid"] = "fixture-rule"
	case "grafana.annotation.list":
		input["start"] = "2026-11-14T00:00:00Z"
		input["end"] = "2026-11-14T00:01:00Z"
	case "grafana.annotation.read", "grafana.annotation.delete":
		input["annotation_id"] = "17"
	case "grafana.annotation.create", "grafana.annotation.update":
		input["text"] = "Synthetic annotation"
		input["time"] = "2026-11-14T00:00:00Z"
		input["time_end"] = "2026-11-14T00:01:00Z"
		if operation == "grafana.annotation.create" {
			input["dashboard_uid"] = "fixture-dashboard"
		} else {
			input["annotation_id"] = "17"
		}
	case "grafana.silence.list":
		input["matcher_name"] = "alertname"
		input["matcher_value"] = "Fixture"
	case "grafana.silence.read", "grafana.silence.delete":
		input["silence_id"] = grafanaFixtureSilenceID
	case "grafana.silence.create", "grafana.silence.update":
		input["matcher_name"] = "alertname"
		input["matcher_value"] = "Fixture"
		input["comment"] = "Synthetic silence"
		input["start"] = "2026-11-14T00:00:00Z"
		input["end"] = "2026-11-14T00:01:00Z"
		if operation == "grafana.silence.update" {
			input["silence_id"] = grafanaFixtureSilenceID
		}
	default:
		t.Fatal("advertised Grafana operation lacks scenario")
	}
	return input
}

// grafanaFixturePermissions returns an independent current-token metadata profile including authentic scopeless general-silence actions, without returning any live credential.
// grafanaFixturePermissions возвращает independent current-token metadata profile с настоящими scopeless general-silence actions без выдачи live credential.
func grafanaFixturePermissions() string {
	return `{"alert.rules:create":["folders:*"],"alert.rules:write":["folders:*"],"alert.rules:delete":["folders:*"],"annotations:create":["dashboards:*"],"annotations:write":["dashboards:*"],"annotations:delete":["dashboards:*"],"alert.instances:create":[""],"alert.instances:write":[""],"alert.instances:read":[""]}`
}

// grafanaFixture keeps only synthetic remote state and one forced failure; it validates actual methods, paths, bodies and replay behavior independently of the adapter implementation.
// grafanaFixture хранит только synthetic remote state и один forced failure; actual methods, paths, bodies и replay behavior проверяются независимо от adapter implementation.
type grafanaFixture struct {
	t              *testing.T
	effects, reads int
	mutated        bool
	scenario       string
	operation      string
	rulePayload    []byte
	beforeEffect   func()
}

// roundTrip serves exact-version documented provider shapes and safe state readback, refusing every unexpected target/header/path instead of acting as an arbitrary HTTP fixture.
// roundTrip обслуживает exact-version documented provider shapes и safe state readback, закрывая неожиданные target/header/path вместо arbitrary HTTP fixture.
func (f *grafanaFixture) roundTrip(r *http.Request) (*http.Response, error) {
	f.t.Helper()
	status := 200
	body := ""
	if r.URL.Scheme != "https" || r.URL.Host != "grafana.example.invalid" || r.Header.Get("THANOS-TENANT") != "" || r.Header.Get("X-Scope-OrgID") != "" {
		f.t.Fatal("Grafana native boundary mismatch")
	}
	if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer kodex-invalid-") {
		if r.Method != "GET" || r.URL.Path != "/api/user" {
			f.t.Fatal("Grafana invalid-key probe escaped authenticated path")
		}
		return &http.Response{Request: r, StatusCode: 401, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"message":"must-drop"}`))}, nil
	}
	if r.Header.Get("Authorization") != "Bearer synthetic-observability-token" {
		f.t.Fatal("Grafana selected credential mismatch")
	}
	if r.Method != http.MethodGet {
		f.effects++
		if r.GetBody != nil || r.Header.Get("Idempotency-Key") != "" {
			f.t.Fatal("Grafana mutation permits unproven replay")
		}
		if f.scenario == "native-conflict" {
			return &http.Response{Request: r, StatusCode: 409, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"message":"version conflict"}`))}, nil
		}
		if f.scenario == "transport-loss" {
			return nil, errors.New("synthetic transport loss")
		}
		if f.scenario == "server-loss" {
			return &http.Response{Request: r, StatusCode: 503, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"message":"must-drop"}`))}, nil
		}
		if f.scenario == "malformed" {
			return &http.Response{Request: r, StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{`))}, nil
		}
		var payload map[string]json.RawMessage
		if r.Body != nil {
			raw, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(raw))
			if len(raw) > 0 && json.Unmarshal(raw, &payload) != nil {
				f.t.Fatal("Grafana mutation body is not bounded JSON")
			}
		}
		switch r.Method + " " + r.URL.Path {
		case "POST /api/v1/provisioning/alert-rules", "PUT /api/v1/provisioning/alert-rules/fixture-rule":
			if len(payload) != 11 || string(payload["orgID"]) != "1" || string(payload["folderUID"]) != `"fixture-folder"` || string(payload["condition"]) != `"B"` || string(payload["for"]) != `"60s"` || r.Header.Get("X-Disable-Provenance") != "" {
				f.t.Fatal("rule write lost closed model or manager")
			}
			var data []struct {
				RefID         string         `json:"refId"`
				DatasourceUID string         `json:"datasourceUid"`
				Model         map[string]any `json:"model"`
			}
			if json.Unmarshal(payload["data"], &data) != nil || len(data) != 2 || data[0].RefID != "A" || data[0].DatasourceUID != "fixture-prometheus" || data[0].Model["expr"] != "up" || data[0].Model["instant"] != true || data[1].RefID != "B" || data[1].DatasourceUID != "__expr__" || data[1].Model["type"] != "classic_conditions" {
				f.t.Fatal("rule query escaped fixed datasource/condition")
			}
			var uid string
			_ = json.Unmarshal(payload["uid"], &uid)
			if r.Method == http.MethodPost && (!strings.HasPrefix(uid, "kodex-") || len(uid) != 38) || r.Method == http.MethodPut && uid != "fixture-rule" {
				f.t.Fatal("rule UID not effect-bound")
			}
			payload["provenance"] = json.RawMessage(`"api"`)
			f.rulePayload, _ = json.Marshal(payload)
			body = string(f.rulePayload)
			f.mutated = true
			if r.Method == http.MethodPost {
				status = 201
			}
		case "DELETE /api/v1/provisioning/alert-rules/fixture-rule":
			if len(payload) != 0 {
				f.t.Fatal("rule delete gained input body")
			}
			status = 204
			body = ""
			f.mutated = true
		case "POST /api/annotations", "PATCH /api/annotations/17":
			expected := `{"text":"Synthetic annotation","time":1794614400000,"timeEnd":1794614460000}`
			if r.Method == http.MethodPost {
				expected = `{"text":"Synthetic annotation","time":1794614400000,"timeEnd":1794614460000,"dashboardUID":"fixture-dashboard"}`
			}
			var want map[string]any
			_ = json.Unmarshal([]byte(expected), &want)
			encoded, _ := json.Marshal(payload)
			var got map[string]any
			_ = json.Unmarshal(encoded, &got)
			wantRaw, _ := json.Marshal(want)
			gotRaw, _ := json.Marshal(got)
			if string(wantRaw) != string(gotRaw) {
				f.t.Fatal("Grafana annotation body differs from fixed contract")
			}
			body = `{"message":"Annotation patched"}`
			if r.Method == http.MethodPost {
				body = `{"message":"Annotation added","id":17}`
			}
			f.mutated = true
		case "DELETE /api/annotations/17":
			if len(payload) != 0 {
				f.t.Fatal("annotation deletion gained a body")
			}
			body = `{"message":"Annotation deleted"}`
			f.mutated = true
		case "POST /api/alertmanager/grafana/api/v2/silences":
			var values struct {
				ID       string `json:"id"`
				Matchers []struct {
					Name    string `json:"name"`
					Value   string `json:"value"`
					IsRegex bool   `json:"isRegex"`
					IsEqual bool   `json:"isEqual"`
				} `json:"matchers"`
				StartsAt  string `json:"startsAt"`
				EndsAt    string `json:"endsAt"`
				CreatedBy string `json:"createdBy"`
				Comment   string `json:"comment"`
			}
			encoded, _ := json.Marshal(payload)
			_ = json.Unmarshal(encoded, &values)
			wantFields := 5
			if f.operation == "grafana.silence.update" {
				wantFields = 6
				if values.ID != grafanaFixtureSilenceID {
					f.t.Fatal("silence update lost exact target")
				}
			}
			if len(payload) != wantFields || len(values.Matchers) != 1 || values.Matchers[0].Name != "alertname" || values.Matchers[0].Value != "Fixture" || values.Matchers[0].IsRegex || !values.Matchers[0].IsEqual || values.StartsAt != "2026-11-14T00:00:00Z" || values.EndsAt != "2026-11-14T00:01:00Z" || values.CreatedBy != "Kodex" || values.Comment != "Synthetic silence" {
				f.t.Fatal("Grafana silence body differs from fixed typed contract")
			}
			status = 202
			body = `{"silenceID":"` + grafanaFixtureSilenceID + `"}`
			f.mutated = true
		case "DELETE /api/alertmanager/grafana/api/v2/silence/" + grafanaFixtureSilenceID:
			body = `{"message":"silence deleted"}`
			f.mutated = true
		default:
			f.t.Fatal("Grafana mutation escaped finite registry")
		}
	} else {
		f.reads++
		switch r.URL.Path {
		case "/api/health":
			body = `{"version":"13.2.0","database":"ok","commit":"must-drop"}`
		case "/api/user":
			body = `{"uid":"fixture-user","orgId":1,"email":"must-drop","login":"must-drop"}`
			if f.scenario == "foreign-org" {
				body = `{"uid":"fixture-user","orgId":2}`
			}
		case "/api/access-control/user/permissions":
			if r.URL.Query().Encode() != (url.Values{"reloadcache": {"true"}}).Encode() {
				f.t.Fatal("permissions cache was not refreshed")
			}
			body = grafanaFixturePermissions()
			if f.scenario == "readonly" {
				body = `{}`
			}
			if f.scenario == "ambiguous-permissions" {
				body = `{"annotations:create":["dashboards:*"],"Annotations:Create":[]}`
			}
		case "/api/dashboards/uid/fixture-dashboard":
			body = `{"dashboard":{"uid":"fixture-dashboard","title":"Fixture dashboard","version":3,"links":["must-drop"],"templating":{"secret":"must-drop"}},"meta":{"provisioned":true,"provisionedExternalId":"must-drop"}}`
		case "/api/datasources/uid/fixture-prometheus":
			body = `{"uid":"fixture-prometheus","orgId":1,"type":"prometheus","name":"Fixture datasource","url":"https://must-drop.invalid","jsonData":{"authorization":"must-drop"},"secureJsonFields":{"apiKey":true}}`
		case "/api/datasources/proxy/uid/fixture-prometheus/api/v1/query_range":
			if r.URL.Query().Get("query") != "up" || r.URL.Query().Get("timeout") != "5s" || r.URL.Query().Get("lookback_delta") != "5m" || r.URL.Query().Get("step") != "60" {
				f.t.Fatal("Grafana proxy lost bounded PromQL parameters")
			}
			body = `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"job":"fixture"},"values":[[1794614400,"1"]]}]}}`
		case "/api/annotations", "/api/annotations/17":
			text := "Before"
			if f.mutated {
				text = "Synthetic annotation"
			}
			body = `{"id":17,"dashboardUID":"fixture-dashboard","time":1794614400000,"timeEnd":1794614460000,"text":"` + text + `","email":"must-drop","data":{"secret":"must-drop"}}`
			if f.scenario == "foreign-dashboard" {
				body = `{"id":17,"dashboardUID":"foreign-dashboard","time":1794614400000,"timeEnd":1794614460000,"text":"Other"}`
			}
			if r.URL.Path == "/api/annotations" {
				if r.URL.Query().Get("limit") != "50" || r.URL.Query().Get("from") != "1794614400000" || r.URL.Query().Get("to") != "1794614460000" {
					f.t.Fatal("annotation list lost fixed bounds")
				}
				body = "[" + body + "]"
			}
			if f.mutated && f.scenario == "readback-denied" {
				status = 403
			}
		case "/api/v1/provisioning/alert-rules", "/api/v1/provisioning/alert-rules/fixture-rule":
			body = `{"uid":"fixture-rule","orgID":1,"title":"Fixture rule","folderUID":"fixture-folder","ruleGroup":"fixture-group","provenance":"file","isPaused":false,"data":[{"model":{"secret":"must-drop"}}]}`
			if strings.HasPrefix(f.operation, "grafana.rule.") && f.operation != "grafana.rule.list" && f.operation != "grafana.rule.read" && f.scenario != "file-managed" {
				body = strings.Replace(body, `"provenance":"file"`, `"provenance":"api"`, 1)
			}
			if f.mutated && len(f.rulePayload) > 0 {
				body = string(f.rulePayload)
			}
			if f.mutated && f.scenario == "readback-denied" {
				status = 403
			}
			if r.URL.Path == "/api/v1/provisioning/alert-rules" {
				body = "[" + body + "]"
			}
		case "/api/alertmanager/grafana/api/v2/silences", "/api/alertmanager/grafana/api/v2/silence/" + grafanaFixtureSilenceID:
			comment := "Before"
			if f.mutated {
				comment = "Synthetic silence"
			}
			body = `{"id":"` + grafanaFixtureSilenceID + `","matchers":[{"name":"alertname","value":"Fixture","isRegex":false,"isEqual":true}],"startsAt":"2026-11-14T00:00:00Z","endsAt":"2026-11-14T00:01:00Z","comment":"` + comment + `","createdBy":"must-drop","status":{"state":"active"}}`
			if r.URL.Path == "/api/alertmanager/grafana/api/v2/silences" {
				if r.URL.Query().Get("filter") != `alertname="Fixture"` {
					f.t.Fatal("silence list accepted arbitrary filter")
				}
				body = "[" + body + "]"
			}
			if f.mutated && f.scenario == "readback-denied" {
				status = 403
			}
		default:
			if strings.HasPrefix(r.URL.Path, "/api/v1/provisioning/alert-rules/kodex-") && f.mutated && len(f.rulePayload) > 0 {
				body = string(f.rulePayload)
				if f.scenario == "readback-denied" {
					status = 403
				}
			} else {
				f.t.Fatal("Grafana read escaped finite registry")
			}
		}
	}
	if f.beforeEffect != nil && r.URL.Path == "/api/access-control/user/permissions" {
		f.beforeEffect()
		f.beforeEffect = nil
	}
	validateDocumentedMetricsWire(f.t, "grafana", r, status, body)
	return &http.Response{Request: r, StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
}

// testGrafanaCapabilities exercises each shipped capability through the actual generic owner-claim validation and native consumer, including personal profiles, exact effects and safe receipts.
// testGrafanaCapabilities проверяет каждую shipped capability через actual generic owner-claim validation и native consumer, включая personal profiles, exact effects и safe receipts.
func testGrafanaCapabilities(t *testing.T, key string) {
	t.Helper()
	adapter := testAdapter(t)
	for _, capability := range adapter.definitions[key].Spec.Capabilities {
		t.Run(capability.Operation, func(t *testing.T) {
			adapter := testAdapter(t)
			fixture := &grafanaFixture{t: t, operation: capability.Operation}
			adapter.nativeHTTPClient = &http.Client{Transport: roundTripFunc(fixture.roundTrip)}
			input := grafanaFixtureInput(t, capability.Operation)
			if key == "grafana-dashboard-annotations" && capability.Operation == "grafana.annotation.create" {
				delete(input, "dashboard_uid")
			}
			request := observabilityInvocation(t, adapter, key, capability.Operation, input)
			if kind := grafanaQualificationKind(key); kind != "" {
				installQualificationFixture(t, adapter, qualificationFixture(kind, request.Credential.ContentSHA256))
			}
			request.PersonalCredentialAdmission = true
			request.ResourcePermissionProfile = integrationpackage.CredentialResourcePermissionProfile(adapter.definitions[key], capability)
			result, err := adapter.Execute(t.Context(), request)
			effects := 0
			if capability.Risk != "READ" {
				effects = 1
			}
			if err != nil || fixture.effects != effects || result.Receipt.EffectKey != request.EffectKey || result.Receipt.InputDigest != request.InputDigest || result.Receipt.ResponseDigest == "" || strings.Contains(result.Summary, "must-drop") {
				t.Fatalf("Grafana typed consumer failed: %v, effects=%d", err, fixture.effects)
			}
		})
	}
}

// TestGrafanaClosedConsumer covers all dashboard/organization operations and ensures file-managed rule reads do not imply writable provenance.
// TestGrafanaClosedConsumer покрывает все dashboard/organization operations и не позволяет file-managed rule reads означать writable provenance.
func TestGrafanaClosedConsumer(t *testing.T) {
	for _, key := range []string{"grafana-dashboard", "grafana-organization", "grafana-dashboard-annotations", "grafana-organization-rule-delete"} {
		t.Run(key, func(t *testing.T) { testGrafanaCapabilities(t, key) })
	}
}

// TestGrafanaMutationNeverReplays preserves UNKNOWN after lost/malformed effects or denied readback and never reissues a mutation through Recover.
// TestGrafanaMutationNeverReplays сохраняет UNKNOWN после lost/malformed effects либо denied readback и не повторяет mutation через Recover.
func TestGrafanaMutationNeverReplays(t *testing.T) {
	for _, operation := range []string{"grafana.annotation.create", "grafana.annotation.update", "grafana.annotation.delete", "grafana.silence.create", "grafana.silence.update", "grafana.silence.delete", "grafana.rule.create", "grafana.rule.update"} {
		for _, scenario := range []string{"transport-loss", "server-loss", "malformed", "readback-denied"} {
			if strings.HasSuffix(operation, ".delete") && scenario == "readback-denied" {
				continue
			}
			t.Run(operation+"/"+scenario, func(t *testing.T) {
				adapter := testAdapter(t)
				fixture := &grafanaFixture{t: t, scenario: scenario, operation: operation}
				adapter.nativeHTTPClient = &http.Client{Transport: roundTripFunc(fixture.roundTrip)}
				request := observabilityInvocation(t, adapter, "grafana-organization", operation, grafanaFixtureInput(t, operation))
				if _, err := adapter.Execute(context.Background(), request); !IsUnknownOutcome(err) || fixture.effects != 1 {
					t.Fatal("Grafana ambiguous effect became repeatable/known", err)
				}
				if _, err := adapter.Recover(t.Context(), request); !IsUnknownOutcome(err) || fixture.effects != 1 {
					t.Fatal("Grafana Recover replayed effect", err)
				}
			})
		}
	}
}

// TestGrafanaBoundariesRejectBeforeEffect tests current organization, dashboard READ membership, profile mismatch and known read-only permissions without permitting any external change.
// TestGrafanaBoundariesRejectBeforeEffect проверяет current organization, dashboard READ membership, profile mismatch и known read-only permissions без external change.
func TestGrafanaBoundariesRejectBeforeEffect(t *testing.T) {
	for _, scenario := range []string{"foreign-org", "foreign-dashboard", "readonly", "profile"} {
		t.Run(scenario, func(t *testing.T) {
			adapter := testAdapter(t)
			fixture := &grafanaFixture{t: t, scenario: scenario, operation: "grafana.annotation.update"}
			adapter.nativeHTTPClient = &http.Client{Transport: roundTripFunc(fixture.roundTrip)}
			key, operation := "grafana-organization", "grafana.annotation.update"
			if scenario == "foreign-dashboard" {
				key, operation = "grafana-dashboard", "grafana.annotation.read"
			}
			request := observabilityInvocation(t, adapter, key, operation, grafanaFixtureInput(t, operation))
			if scenario == "profile" {
				request.PersonalCredentialAdmission = true
				request.ResourcePermissionProfile = "SENTRY_PROJECT_MUTATION_V1"
			}
			if _, err := adapter.Execute(t.Context(), request); err == nil || fixture.effects != 0 {
				t.Fatal("Grafana scope/permission failure caused an effect", err)
			}
		})
	}
}

// TestGrafanaPersonalMetadataPreflight proves current scope metadata crosses the existing private verification path without converting unknown or read-only credentials into reusable WRITE proof.
// TestGrafanaPersonalMetadataPreflight доказывает прохождение current scope metadata через существующий private verification path без превращения unknown либо read-only credentials в reusable WRITE proof.
func TestGrafanaPersonalMetadataPreflight(t *testing.T) {
	for _, scenario := range []string{"writable", "readonly", "ambiguous-permissions"} {
		t.Run(scenario, func(t *testing.T) {
			adapter := testAdapter(t)
			fixture := &grafanaFixture{t: t, scenario: scenario}
			adapter.nativeHTTPClient = &http.Client{Transport: roundTripFunc(fixture.roundTrip)}
			request := observabilityInvocation(t, adapter, "grafana-organization", "grafana.connection.read", map[string]any{})
			request.CapabilityKey, request.Operation, request.GrantRef, request.GrantVersion = "", "", "", 0
			request.PersonalObservationProfiles, request.PersonalDeniedWriteCapabilityKeys = integrationpackage.CredentialObservationRequests(adapter.definitions["grafana-organization"])
			result, err := adapter.VerifyPersonalCredential(t.Context(), request)
			if err != nil || fixture.effects != 0 {
				t.Fatal("Grafana personal metadata probe failed or mutated provider", err)
			}
			if (scenario == "ambiguous-permissions") != (len(result.ObservedPermissionProfiles) == 0) || scenario == "readonly" && len(result.DeniedCapabilityKeys) != 8 || scenario != "readonly" && len(result.DeniedCapabilityKeys) != 0 {
				t.Fatal("unknown/current provider metadata became false WRITE evidence")
			}
			for _, key := range result.ConfirmedCapabilityKeys {
				if key != "grafana.connection.read" {
					t.Fatal("personal metadata globally confirmed WRITE")
				}
			}
		})
	}
}

// TestGrafanaNativeRuleProvenanceAndVersion preserves file/Git read-only behavior and the native optimistic-lock failure; a conflicting write is never silently retried or represented as an accepted rule update.
// TestGrafanaNativeRuleProvenanceAndVersion сохраняет file/Git read-only behavior и native optimistic-lock failure; конфликтная запись не повторяется и не выдаётся за accepted rule update.
func TestGrafanaNativeRuleProvenanceAndVersion(t *testing.T) {
	for _, scenario := range []string{"file-managed", "native-conflict"} {
		t.Run(scenario, func(t *testing.T) {
			adapter := testAdapter(t)
			fixture := &grafanaFixture{t: t, scenario: scenario, operation: "grafana.rule.update"}
			adapter.nativeHTTPClient = &http.Client{Transport: roundTripFunc(fixture.roundTrip)}
			request := observabilityInvocation(t, adapter, "grafana-organization", "grafana.rule.update", grafanaFixtureInput(t, "grafana.rule.update"))
			_, err := adapter.Execute(t.Context(), request)
			wanted := 0
			if scenario == "native-conflict" {
				wanted = 1
			}
			if err == nil || IsUnknownOutcome(err) || fixture.effects != wanted {
				t.Fatal("native file/version boundary failed", err, fixture.effects)
			}
		})
	}
}

// TestGrafanaQualifiedMutationWithdrawal requires exact operator evidence at send time and refuses missing/withdrawn or foreign-key profiles without performing a mutation or substituting a broader organization connection.
// TestGrafanaQualifiedMutationWithdrawal требует exact operator evidence при отправке и отвергает missing/withdrawn/foreign-key profile без mutation либо подстановки более широкой organization connection.
func TestGrafanaQualifiedMutationWithdrawal(t *testing.T) {
	for _, key := range []string{"grafana-dashboard-annotations", "grafana-organization-rule-delete"} {
		for _, scenario := range []string{"missing", "withdrawn", "foreign-key"} {
			t.Run(key+"/"+scenario, func(t *testing.T) {
				adapter := testAdapter(t)
				operation := "grafana.annotation.update"
				if key == "grafana-organization-rule-delete" {
					operation = "grafana.rule.delete"
				}
				fixture := &grafanaFixture{t: t, operation: operation}
				adapter.nativeHTTPClient = &http.Client{Transport: roundTripFunc(fixture.roundTrip)}
				request := observabilityInvocation(t, adapter, key, operation, grafanaFixtureInput(t, operation))
				if scenario != "missing" {
					proof := qualificationFixture(grafanaQualificationKind(key), request.Credential.ContentSHA256)
					if scenario == "foreign-key" {
						proof.CredentialSHA256 = strings.Repeat("9", 64)
					}
					path := installQualificationFixture(t, adapter, proof)
					if scenario == "withdrawn" {
						fixture.beforeEffect = func() { _ = os.Remove(path) }
					}
				}
				_, err := adapter.Execute(t.Context(), request)
				var safe *SafeError
				if !errors.As(err, &safe) || safe.Code != providerCapabilityUnsupportedCode || safe.HealthSummary != operatorQualificationRequiredSummary || fixture.effects != 0 {
					t.Fatal("unqualified Grafana mutation escaped before-send boundary", err, fixture.effects)
				}
			})
		}
	}
}
