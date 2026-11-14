package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/legacy"
)

// sentryInvocation builds synthetic exact definition/input/scope pins and never loads live configuration or credentials.
// sentryInvocation строит synthetic exact definition/input/scope pins, не загружая живые конфигурацию и credentials.
func sentryInvocation(t *testing.T, adapter *Adapter, key, operation string, input map[string]any) Request {
	t.Helper()
	definition := adapter.definitions[key]
	credential := testCredential(t, adapter, "synthetic-sentry-token")
	request := invocationRequest(t, adapter.definitions["jira"], "jira.project.read", map[string]any{}, credential)
	capability, exists := definition.Capability(operation)
	if !exists {
		t.Fatal("missing Sentry operation")
	}
	origin := "https://sentry.io"
	if strings.HasPrefix(key, "sentry-self-hosted") {
		origin = "https://sentry.example.invalid"
	}
	configuration := map[string]string{"base_url": origin, "organization_slug": "fixture-org", "project_slug": "fixture-project"}
	if strings.HasSuffix(key, "-organization") {
		delete(configuration, "project_slug")
	}
	request.DefinitionPackage, _ = json.Marshal(definition)
	request.DefinitionKey, request.DefinitionVersion, request.DefinitionDigest = key, definition.Metadata.Version, definition.Digest
	request.CapabilityKey, request.Operation, request.Risk, request.ApprovalPolicy = capability.Key, capability.Operation, capability.Risk, capability.ApprovalPolicy
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

// sentryInput supplies one bounded synthetic input per advertised operation; unknown operations fail the fixture instead of disappearing from coverage.
// sentryInput задаёт ограниченный synthetic input каждой объявленной операции; неизвестная операция ломает fixture вместо исчезновения из покрытия.
func sentryInput(t *testing.T, operation string) map[string]any {
	t.Helper()
	switch operation {
	case "sentry.organization.read":
		return map[string]any{}
	case "sentry.organization.project.list", "sentry.organization.release.list", "sentry.organization.alert.list", "sentry.organization.detector.list":
		return map[string]any{"limit": 2}
	case "sentry.organization.release.read":
		return map[string]any{"version": "fixture@1"}
	case "sentry.organization.release.create":
		return map[string]any{"version": "fixture@1", "project_slug": "fixture-project", "status": "open", "ref": "abcdef"}
	case "sentry.organization.release.update":
		return map[string]any{"version": "fixture@1", "status": "open", "ref": "abcdef"}
	case "sentry.organization.alert.read", "sentry.organization.alert.delete":
		return map[string]any{"alert_id": "31"}
	case "sentry.organization.detector.read":
		return map[string]any{"detector_id": "41"}
	case "sentry.organization.alert.create", "sentry.organization.alert.update":
		result := map[string]any{"name": "Synthetic alert", "enabled": true, "detector_id": "41", "trigger": "first_seen_event", "notification_target": "team:9", "frequency_minutes": 30}
		if operation == "sentry.organization.alert.update" {
			result["alert_id"] = "31"
		}
		return result
	case "sentry.project.read":
		return map[string]any{}
	case "sentry.issue.list":
		return map[string]any{"limit": 2, "query": "is:unresolved"}
	case "sentry.issue.event.list", "sentry.issue.comment.list":
		return map[string]any{"issue_id": "17", "limit": 2}
	case "sentry.issue.comment.create":
		return map[string]any{"issue_id": "17", "text": "Synthetic comment"}
	case "sentry.issue.assignment.update":
		return map[string]any{"issue_id": "17", "assignee": "user:7"}
	case "sentry.release.list", "sentry.alert.list":
		return map[string]any{"limit": 2}
	case "sentry.release.read":
		return map[string]any{"version": "fixture@1"}
	case "sentry.alert.read":
		return map[string]any{"alert_id": "31"}
	case "sentry.issue.read":
		return map[string]any{"issue_id": "17"}
	case "sentry.event.read":
		return map[string]any{"event_id": strings.Repeat("a", 32)}
	case "sentry.issue.status.update":
		return map[string]any{"issue_id": "17", "status": "resolved"}
	default:
		t.Fatal("Sentry operation lacks a consumer fixture")
		return nil
	}
}

// sentryWireFixture validates actual adapter HTTP requests and successful responses against the independently declared provider subset, including the single status-only PUT payload.
// sentryWireFixture проверяет реальные HTTP-запросы адаптера и успешные ответы по отдельному provider subset, включая единственный status-only PUT payload.
func sentryWireFixture(t *testing.T, request *http.Request) *http.Response {
	t.Helper()
	if request.Header.Get("Authorization") != "Bearer synthetic-sentry-token" {
		t.Fatal("Sentry credential/query boundary changed")
	}
	if request.Method == http.MethodPut && (request.GetBody != nil || request.Header.Get("Idempotency-Key") != "") {
		t.Fatal("Sentry PUT permits unproven replay")
	}
	_, document := documentedProviderContract(t, "sentry")
	document.Servers = openapi3.Servers{{URL: request.URL.Scheme + "://" + request.URL.Host}}
	router, err := legacy.NewRouter(document)
	if err != nil {
		t.Fatal(err)
	}
	var requestBody []byte
	if request.Body != nil {
		requestBody, _ = io.ReadAll(request.Body)
		request.Body = io.NopCloser(bytes.NewReader(requestBody))
	}
	route, params, err := router.FindRoute(request)
	if err != nil {
		t.Fatal(err)
	}
	input := &openapi3filter.RequestValidationInput{Request: request, PathParams: params, Route: route, Options: &openapi3filter.Options{AuthenticationFunc: func(_ context.Context, _ *openapi3filter.AuthenticationInput) error { return nil }}}
	if err := openapi3filter.ValidateRequest(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	request.Body = io.NopCloser(bytes.NewReader(requestBody))
	project := `{"id":"3","slug":"fixture-project","name":"Synthetic project","organization":{"id":"1","slug":"fixture-org"}}`
	status := "unresolved"
	if request.Method == http.MethodPut && request.URL.Path == "/api/0/organizations/fixture-org/issues/17/" {
		var payload map[string]string
		if json.Unmarshal(requestBody, &payload) != nil || len(payload) != 1 || payload["status"] != "resolved" && payload["status"] != "unresolved" && payload["status"] != "ignored" && payload["assignedTo"] != "user:7" {
			t.Fatal("Sentry mutation changed fields")
		}
		if payload["status"] != "" {
			status = payload["status"]
		}
	}
	issue := `{"id":"17","title":"Synthetic error","status":"` + status + `","project":{"id":"3","slug":"fixture-project"}}`
	body := project
	header := http.Header{"Content-Type": []string{"application/json"}}
	switch request.URL.Path {
	case "/api/0/":
		body = `{"auth":{"scopes":["project:read","event:read","event:write","org:read","project:releases","alerts:write"]},"user":{"id":"7"}}`
	case "/api/0/projects/fixture-org/fixture-project/":
	case "/api/0/projects/fixture-org/fixture-project/issues/":
		body = "[" + issue + "]"
		header.Set("Link", "<"+request.URL.Scheme+"://"+request.URL.Host+request.URL.Path+`?cursor=0:2:0>; rel="next"; results="true"; cursor="0:2:0"`)
	case "/api/0/organizations/fixture-org/issues/17/":
		body = issue
		if strings.Contains(string(requestBody), "assignedTo") {
			body = strings.TrimSuffix(issue, "}") + `,"assignedTo":{"id":"7","type":"user"}}`
		}
	case "/api/0/organizations/fixture-org/issues/17/comments/":
		body = `{"id":"21","data":{"text":"Synthetic comment"},"user":{"id":"7"}}`
		if request.Method == http.MethodGet {
			body = "[" + body + "]"
		}
	case "/api/0/organizations/fixture-org/issues/17/events/":
		body = `[{"eventID":"` + strings.Repeat("a", 32) + `","projectID":"3","title":"Synthetic event","platform":"go"}]`
	case "/api/0/projects/fixture-org/fixture-project/releases/":
		body = `[{"version":"fixture@1","status":"open","projects":[{"id":"3","slug":"fixture-project"}]}]`
	case "/api/0/projects/fixture-org/fixture-project/releases/fixture@1/":
		body = `{"version":"fixture@1","status":"open","projects":[{"id":"3","slug":"fixture-project"}]}`
	case "/api/0/organizations/fixture-org/":
		body = `{"id":"1","slug":"fixture-org","name":"Synthetic organization"}`
	case "/api/0/organizations/fixture-org/projects/":
		body = `[{"id":"3","slug":"fixture-project","name":"Synthetic project"}]`
	case "/api/0/organizations/fixture-org/releases/":
		body = `{"version":"fixture@1","status":"open","ref":"abcdef","projects":[{"id":3,"slug":"fixture-project"},{"id":4,"slug":"other-project"}]}`
		if request.Method == http.MethodGet {
			body = "[" + body + "]"
		}
	case "/api/0/organizations/fixture-org/releases/fixture@1/":
		body = `{"version":"fixture@1","status":"open","ref":"abcdef","projects":[{"id":3,"slug":"fixture-project"},{"id":4,"slug":"other-project"}]}`
	case "/api/0/organizations/fixture-org/detectors/":
		body = `[{"id":"41","name":"Synthetic detector","type":"error","projectId":"3"}]`
	case "/api/0/organizations/fixture-org/detectors/41/":
		body = `{"id":"41","name":"Synthetic detector","type":"error","projectId":"3"}`
	case "/api/0/organizations/fixture-org/workflows/":
		body = `[{"id":"31","name":"Synthetic alert","enabled":true,"organizationId":"1"}]`
		if request.Method == http.MethodPost {
			body = sentryWorkflowFixture()
		}
	case "/api/0/organizations/fixture-org/workflows/31/":
		body = `{"id":"31","name":"Synthetic alert","enabled":true,"organizationId":"1"}`
		if request.Method == http.MethodPut {
			body = sentryWorkflowFixture()
		}
		if request.Method == http.MethodDelete {
			body = ""
		}
	case "/api/0/organizations/fixture-org/workflows/31/project-scope/":
		body = `{"projectIds":["3"],"includesAllProjects":false}`
	case "/api/0/projects/fixture-org/fixture-project/events/" + strings.Repeat("a", 32) + "/":
		body = `{"eventID":"` + strings.Repeat("a", 32) + `","projectID":"3","title":"Synthetic event","platform":"go","request":{"headers":[["Secret","not-projected"]]}}`
	default:
		t.Fatal("Sentry request escaped fixed routes")
	}
	responseStatus := 200
	if request.Method == http.MethodDelete {
		responseStatus = 204
	}
	if request.Method == http.MethodPost {
		responseStatus = 201
	}
	validation := &openapi3filter.ResponseValidationInput{RequestValidationInput: input, Status: responseStatus, Header: header, Body: io.NopCloser(strings.NewReader(body))}
	if err := openapi3filter.ValidateResponse(t.Context(), validation); err != nil {
		t.Fatal(err)
	}
	return &http.Response{StatusCode: responseStatus, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: request}
}

// testSentryCapabilities exercises every shipped operation through its selected Cloud/native transport, validates receipts and runs scoped approval with the same immutable input.
// testSentryCapabilities выполняет каждую shipped operation через выбранный Cloud/native transport, проверяет receipts и scoped approval с тем же immutable input.
func testSentryCapabilities(t *testing.T, key string) {
	t.Helper()
	adapter := testAdapter(t)
	for _, capability := range adapter.definitions[key].Spec.Capabilities {
		t.Run(capability.Key, func(t *testing.T) {
			adapter := testAdapter(t)
			request := sentryInvocation(t, adapter, key, capability.Key, sentryInput(t, capability.Key))
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(wire *http.Request) (*http.Response, error) { calls++; return sentryWireFixture(t, wire), nil })}
			denied := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("Sentry used the wrong transport")
				return nil, errors.New("wrong transport")
			})}
			adapter.providerHTTPClient, adapter.openAPIHTTPClient, adapter.nativeHTTPClient = denied, denied, denied
			if key == "sentry" || key == "sentry-organization" {
				adapter.openAPIHTTPClient = client
			} else {
				adapter.nativeHTTPClient = client
			}
			request.PersonalCredentialAdmission = true
			contract, _ := integrationpackage.CredentialResourcePermissionContractFor(key, capability.Key)
			request.ResourcePermissionProfile = contract.Profile
			result, err := adapter.Execute(t.Context(), request)
			if err != nil || calls == 0 || result.Receipt.EffectKey != request.EffectKey || result.Receipt.InputDigest != request.InputDigest || strings.Contains(result.Summary, "not-projected") {
				t.Fatalf("Sentry consumer failed: %v calls=%d", err, calls)
			}
			if capability.AllowsApprovalPolicy("HUMAN_SCOPED") {
				request.ApprovalPolicy = "HUMAN_SCOPED"
				if _, err := adapter.Execute(t.Context(), request); err != nil {
					t.Fatal("scoped approval rejected", err)
				}
			}
		})
	}
}

// TestSentryClosedConsumer runs both separately selected origins across the entire first provider vertical, without network access.
// TestSentryClosedConsumer выполняет всю первую provider vertical с обоими отдельно выбранными origin без сетевого доступа.
func TestSentryClosedConsumer(t *testing.T) {
	for _, key := range []string{"sentry", "sentry-self-hosted", "sentry-organization", "sentry-self-hosted-organization"} {
		t.Run(key, func(t *testing.T) { testSentryCapabilities(t, key) })
	}
}

// TestSentryRejectsForeignProjectBeforeMutation proves that issue IDs never grant cross-project writes, and transport loss after PUT remains UNKNOWN with no replay or recovery mutation.
// TestSentryRejectsForeignProjectBeforeMutation доказывает запрет межпроектной записи по issue ID; потеря ответа PUT остаётся UNKNOWN без повтора или recovery mutation.
func TestSentryRejectsForeignProjectBeforeMutation(t *testing.T) {
	for _, scenario := range []string{"foreign-project", "readonly-token", "unknown-put", "malformed-put", "malformed-same-state-put"} {
		t.Run(scenario, func(t *testing.T) {
			adapter := testAdapter(t)
			input := sentryInput(t, "sentry.issue.status.update")
			if scenario == "malformed-same-state-put" {
				input["status"] = "unresolved"
			}
			request := sentryInvocation(t, adapter, "sentry", "sentry.issue.status.update", input)
			puts := 0
			adapter.openAPIHTTPClient = &http.Client{Transport: roundTripFunc(func(wire *http.Request) (*http.Response, error) {
				if wire.Method == http.MethodPut {
					puts++
					if scenario == "unknown-put" {
						return nil, errors.New("synthetic lost response")
					}
				}
				response := sentryWireFixture(t, wire)
				if scenario == "foreign-project" && strings.Contains(wire.URL.Path, "/organizations/") {
					response.Body = io.NopCloser(strings.NewReader(`{"id":"17","title":"Foreign","status":"unresolved","project":{"id":"4","slug":"foreign"}}`))
				}
				if scenario == "readonly-token" && wire.URL.Path == "/api/0/" {
					response.Body = io.NopCloser(strings.NewReader(`{"auth":{"scopes":["event:read"]}}`))
				}
				if strings.HasPrefix(scenario, "malformed") && wire.Method == http.MethodPut {
					response.Body = io.NopCloser(strings.NewReader(`{"id":"17"}`))
				}
				return response, nil
			})}
			_, err := adapter.Execute(t.Context(), request)
			unknown := scenario == "unknown-put" || strings.HasPrefix(scenario, "malformed")
			if err == nil || IsUnknownOutcome(err) != unknown || unknown && puts != 1 || !unknown && puts != 0 {
				t.Fatalf("boundary failed: %v puts=%d", err, puts)
			}
			before := puts
			_, err = adapter.Recover(t.Context(), request)
			if !IsUnknownOutcome(err) || puts != before {
				t.Fatal("Sentry recovery repeated a mutation")
			}
		})
	}
}

// TestSentryPersonalScopePreflight distinguishes current read-only tokens, supported write metadata and ambiguous scope payloads without emitting a WRITE proof.
// TestSentryPersonalScopePreflight различает текущий read-only token, поддержанные write metadata и неоднозначные scopes без выпуска WRITE proof.
func TestSentryPersonalScopePreflight(t *testing.T) {
	for _, scenario := range []string{"writable", "readonly", "ambiguous"} {
		t.Run(scenario, func(t *testing.T) {
			adapter := testAdapter(t)
			request := sentryInvocation(t, adapter, "sentry", "sentry.project.read", map[string]any{})
			request.CapabilityKey, request.Operation, request.GrantRef, request.GrantVersion = "", "", "", 0
			request.PersonalObservationProfiles, request.PersonalDeniedWriteCapabilityKeys = integrationpackage.CredentialObservationRequests(adapter.definitions["sentry"])
			adapter.openAPIHTTPClient = &http.Client{Transport: roundTripFunc(func(wire *http.Request) (*http.Response, error) {
				if wire.Method != http.MethodGet {
					t.Fatal("scope preflight mutated provider")
				}
				response := sentryWireFixture(t, wire)
				if wire.URL.Path == "/api/0/" {
					if scenario == "readonly" {
						response.Body = io.NopCloser(strings.NewReader(`{"auth":{"scopes":["event:read"]}}`))
					}
					if scenario == "ambiguous" {
						response.Body = io.NopCloser(strings.NewReader(`{"auth":{"scopes":["event:write"],"Scopes":[]}}`))
					}
				}
				return response, nil
			})}
			result, err := adapter.VerifyPersonalCredential(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "readonly" && len(result.DeniedCapabilityKeys) != 3 || scenario != "readonly" && len(result.DeniedCapabilityKeys) != 0 || scenario == "ambiguous" && len(result.ObservedPermissionProfiles) != 0 || scenario != "ambiguous" && len(result.ObservedPermissionProfiles) != 1 {
				t.Fatal("unknown scopes became permission proof")
			}
			for _, key := range result.ConfirmedCapabilityKeys {
				if key == "sentry.issue.status.update" {
					t.Fatal("metadata produced a global WRITE proof")
				}
			}
		})
	}
}

// sentryWorkflowFixture is an independent fixed provider response for the supported notification profile; it never reflects arbitrary request configuration back as proof.
// sentryWorkflowFixture задаёт независимый фиксированный provider response поддержанного notification profile; произвольная request configuration не отражается как proof.
func sentryWorkflowFixture() string {
	return `{"id":"31","organizationId":"1","name":"Synthetic alert","enabled":true,"detectorIds":["41"],"config":{"frequency":30},"triggers":{"logicType":"any-short","conditions":[{"type":"first_seen_event","comparison":true,"conditionResult":true}]},"actionFilters":[{"logicType":"all","conditions":[],"actions":[{"type":"email","data":{"fallthrough_type":"NoOne"},"config":{"target_type":"team","target_identifier":"9"}}]}]}`
}

// TestSentryProjectAlertScopeHidesShared checks the same complete project eligibility on list/detail and refuses foreign/shared/all-project workflow metadata without widening the project grant.
// TestSentryProjectAlertScopeHidesShared проверяет одинаковый полный project eligibility для list/detail и скрывает foreign/shared/all-project metadata без расширения project grant.
func TestSentryProjectAlertScopeHidesShared(t *testing.T) {
	for _, operation := range []string{"sentry.alert.list", "sentry.alert.read"} {
		for _, scope := range []string{`{"projectIds":["4"],"includesAllProjects":false}`, `{"projectIds":["3","4"],"includesAllProjects":false}`, `{"projectIds":[],"includesAllProjects":true}`} {
			t.Run(operation+scope, func(t *testing.T) {
				adapter := testAdapter(t)
				request := sentryInvocation(t, adapter, "sentry", operation, sentryInput(t, operation))
				details := 0
				adapter.openAPIHTTPClient = &http.Client{Transport: roundTripFunc(func(wire *http.Request) (*http.Response, error) {
					if wire.URL.Path == "/api/0/organizations/fixture-org/workflows/31/" {
						details++
					}
					response := sentryWireFixture(t, wire)
					if strings.HasSuffix(wire.URL.Path, "/project-scope/") {
						response.Body = io.NopCloser(strings.NewReader(scope))
					}
					return response, nil
				})}
				result, err := adapter.Execute(t.Context(), request)
				if operation == "sentry.alert.read" {
					if err == nil || details != 0 {
						t.Fatal("hidden workflow detail was fetched or returned")
					}
					return
				}
				if err != nil || strings.Contains(result.Summary, "Synthetic alert") || strings.Contains(result.Summary, `\"31\"`) {
					t.Fatal("shared workflow metadata leaked from project list", err)
				}
			})
		}
	}
}

// TestSentryOrganizationMutationNeverReplays keeps each shared-object effect on exactly one HTTP mutation and preserves UNKNOWN across response loss, malformed acknowledgements and attempted recovery.
// TestSentryOrganizationMutationNeverReplays сохраняет каждый shared-object effect как одну HTTP mutation и UNKNOWN после потери ответа, malformed acknowledgement и попытки recovery.
func TestSentryOrganizationMutationNeverReplays(t *testing.T) {
	operations := []string{"sentry.organization.release.create", "sentry.organization.release.update", "sentry.organization.alert.create", "sentry.organization.alert.update", "sentry.organization.alert.delete"}
	for _, operation := range operations {
		for _, scenario := range []string{"lost", "malformed", "scope-denied"} {
			t.Run(operation+scenario, func(t *testing.T) {
				adapter := testAdapter(t)
				request := sentryInvocation(t, adapter, "sentry-organization", operation, sentryInput(t, operation))
				effects := 0
				adapter.openAPIHTTPClient = &http.Client{Transport: roundTripFunc(func(wire *http.Request) (*http.Response, error) {
					mutation := wire.Method != http.MethodGet && wire.Method != http.MethodHead
					if mutation {
						effects++
						if scenario == "lost" {
							return nil, errors.New("synthetic lost organization effect response")
						}
					}
					response := sentryWireFixture(t, wire)
					if scenario == "scope-denied" && wire.URL.Path == "/api/0/" {
						response.Body = io.NopCloser(strings.NewReader(`{"auth":{"scopes":["event:read"]},"user":{"id":"7"}}`))
					}
					if mutation && scenario == "malformed" {
						response.Body = io.NopCloser(strings.NewReader(`{"id":"31"}`))
					}
					return response, nil
				})}
				_, err := adapter.Execute(t.Context(), request)
				unknown := scenario != "scope-denied"
				if err == nil || IsUnknownOutcome(err) != unknown || unknown && effects != 1 || !unknown && effects != 0 {
					t.Fatalf("organization effect boundary failed: %v effects=%d", err, effects)
				}
				before := effects
				_, err = adapter.Recover(t.Context(), request)
				if !IsUnknownOutcome(err) || effects != before {
					t.Fatal("organization recovery repeated shared mutation")
				}
			})
		}
	}
}

// TestSentryWrongScopeAndProfileCloseBeforeHTTP rejects project-to-organization relabeling, a foreign mandatory profile and an unavailable native route without exposing a credential to a fallback transport.
// TestSentryWrongScopeAndProfileCloseBeforeHTTP отклоняет project-to-organization relabeling, чужой mandatory profile и недоступный native route без передачи credential запасному transport.
func TestSentryWrongScopeAndProfileCloseBeforeHTTP(t *testing.T) {
	for _, scenario := range []string{"project-relabel", "wrong-profile", "native-unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			adapter := testAdapter(t)
			key := "sentry-organization"
			if scenario == "native-unavailable" {
				key = "sentry-self-hosted-organization"
			}
			request := sentryInvocation(t, adapter, key, "sentry.organization.release.update", sentryInput(t, "sentry.organization.release.update"))
			calls := 0
			denied := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return nil, errors.New("forbidden synthetic fallback")
			})}
			adapter.providerHTTPClient, adapter.openAPIHTTPClient, adapter.nativeHTTPClient = denied, denied, nil
			if scenario == "project-relabel" {
				definition := adapter.definitions["sentry"]
				request.DefinitionKey, request.DefinitionVersion, request.DefinitionDigest = "sentry", definition.Metadata.Version, definition.Digest
				request.DefinitionPackage, _ = json.Marshal(definition)
			}
			if scenario == "wrong-profile" {
				request.PersonalCredentialAdmission = true
				request.ResourcePermissionProfile = integrationpackage.CredentialResourcePermissionSentryStatusV1
			}
			if _, err := adapter.Execute(t.Context(), request); err == nil || calls != 0 {
				t.Fatal("invalid scope/profile reached HTTP")
			}
		})
	}
}
