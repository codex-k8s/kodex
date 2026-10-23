package callback

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// assistantCatalogInputDiagnosticClient adds an optional revision-one authorization-recording failure to nested in-memory search and terminal-projection stubs.
// assistantCatalogInputDiagnosticClient добавляет необязательную ошибку записи авторизации первой ревизии к вложенным подстановкам поиска и итоговой проекции в памяти.
type assistantCatalogInputDiagnosticClient struct {
	*assistantSearchRecoveryClient
	authorizationFailure bool
}

// RecordRunToolCall rejects revision one when authorization failure is enabled; otherwise preserves the inherited terminal-recording behavior.
// RecordRunToolCall отклоняет первую ревизию при включённой ошибке авторизации, иначе сохраняет унаследованное поведение итоговой записи.
func (client *assistantCatalogInputDiagnosticClient) RecordRunToolCall(ctx context.Context, request *controlplanev1.RecordRunToolCallRequest, options ...grpc.CallOption) (*controlplanev1.RecordRunToolCallResponse, error) {
	if client.authorizationFailure && request.GetRevision() == 1 {
		return nil, status.Error(codes.PermissionDenied, "PRIVATE_AUTHORIZATION_ERROR")
	}
	return client.assistantSearchRecoveryClient.RecordRunToolCall(ctx, request, options...)
}

// newAssistantCatalogInputDiagnosticClient assembles fresh nested activity/search stubs around the supplied catalog response, keeping call counts isolated per test.
// newAssistantCatalogInputDiagnosticClient собирает новые вложенные подстановки активности и поиска вокруг переданного ответа каталога, изолируя счётчики вызовов между тестами.
func newAssistantCatalogInputDiagnosticClient(response *controlplanev1.AssistantConfigurationCatalogResponse) *assistantCatalogInputDiagnosticClient {
	return &assistantCatalogInputDiagnosticClient{assistantSearchRecoveryClient: &assistantSearchRecoveryClient{
		assistantSearchDiagnosticClient: &assistantSearchDiagnosticClient{
			assistantFreshCatalogMCPClient: &assistantFreshCatalogMCPClient{},
			response:                       &controlplanev1.SearchAssistantResourcesResponse{AssistantConfigurationCatalog: response},
		},
	}}
}

// assistantCatalogDiagnosticWire executes a local get_configuration_catalog MCP call and requires its structured error response, returning safe fields plus captured wire/log text.
// The helper uses httptest and stubs only; it opens no server listener or real owner connection.
// assistantCatalogDiagnosticWire выполняет локальный вызов MCP get_configuration_catalog и требует структурированный ответ ошибки, возвращая безопасные поля и записанные ответ с логом.
// Используются только httptest и подстановки; серверный слушатель или настоящее соединение с владельцем не открываются.
func assistantCatalogDiagnosticWire(t *testing.T, input runtimecontract.RunnerInput, arguments map[string]any, client *assistantCatalogInputDiagnosticClient) (map[string]any, string, string) {
	t.Helper()
	var logs bytes.Buffer
	server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}, logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	params, err := json.Marshal(map[string]any{"name": "get_configuration_catalog", "arguments": arguments})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	server.callTool(recorder, httptest.NewRequest("POST", "/mcp", nil), mcpRequest{ID: json.RawMessage(`"catalog-input"`), Params: params}, input)
	var wire struct {
		Result struct {
			IsError    bool           `json:"isError"`
			Structured map[string]any `json:"structuredContent"`
		} `json:"result"`
	}
	if json.Unmarshal(recorder.Body.Bytes(), &wire) != nil || !wire.Result.IsError {
		t.Fatal("catalog failure did not reach the native MCP error result")
	}
	return wire.Result.Structured, recorder.Body.String(), logs.String()
}

// assertAssistantCatalogDiagnosticPrivacy rejects known private canaries and execution/context pins in captured MCP/log text; its fixture requires a non-nil assistant context.
// This bounded assertion is not a general-purpose secret detector.
// assertAssistantCatalogDiagnosticPrivacy отклоняет известные частные контрольные значения и привязки выполнения и контекста в записанных MCP-ответе и логе; образец требует непустой контекст помощника.
// Эта ограниченная проверка не является универсальным детектором секретов.
func assertAssistantCatalogDiagnosticPrivacy(t *testing.T, input runtimecontract.RunnerInput, wire, logs string) {
	t.Helper()
	for _, private := range []string{"PRIVATE_SENTINEL", "PRIVATE_OWNER_ERROR", "PRIVATE_PROJECTION_ERROR", "PRIVATE_AUTHORIZATION_ERROR", input.LeaseFence, input.ProviderCredentialSHA256, input.Instructions, input.Task, input.AssistantContext.EntityRef} {
		if private != "" && (strings.Contains(wire, private) || strings.Contains(logs, private)) {
			t.Fatal("catalog failure disclosed private input, execution pins or upstream content")
		}
	}
}

// TestAssistantCatalogLocalInputWireRecoveryIsProjectionBound checks malformed workflow selectors in both scopes keep closed local classes, zero search RPCs and failed activity receipts.
// After a terminal recording failure the same input must lose retry guidance and remain TOOL_UNAVAILABLE.
// TestAssistantCatalogLocalInputWireRecoveryIsProjectionBound проверяет, что некорректные селекторы процесса в обеих областях сохраняют закрытые локальные классы, отсутствие поисковых RPC и квитанции неуспешной активности.
// После ошибки итоговой записи тот же ввод должен лишиться подсказки повтора и остаться TOOL_UNAVAILABLE.
func TestAssistantCatalogLocalInputWireRecoveryIsProjectionBound(t *testing.T) {
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeSystem, runtimecontract.AssistantScopeProject} {
		for _, test := range []struct {
			name   string
			class  string
			mutate func(map[string]any, map[string]any)
		}{
			{"unknown top field", assistantCatalogShapeInvalid, func(a, _ map[string]any) { a["PRIVATE_SENTINEL"] = "PRIVATE_SENTINEL" }},
			{"selector null", assistantCatalogShapeInvalid, func(a, _ map[string]any) { a["assistant_configuration_catalog"] = nil }},
			{"selector text", assistantCatalogShapeInvalid, func(a, _ map[string]any) { a["assistant_configuration_catalog"] = "PRIVATE_SENTINEL" }},
			{"selector array", assistantCatalogShapeInvalid, func(a, _ map[string]any) { a["assistant_configuration_catalog"] = []any{} }},
			{"unknown selector field", assistantCatalogShapeInvalid, func(_, s map[string]any) { s["expected_digest"] = "PRIVATE_SENTINEL" }},
			{"missing kind", assistantCatalogSelectorInvalid, func(_, s map[string]any) { delete(s, "kind") }},
			{"kind type", assistantCatalogSelectorInvalid, func(_, s map[string]any) { s["kind"] = 7 }},
			{"unknown kind", assistantCatalogSelectorInvalid, func(_, s map[string]any) { s["kind"] = "PRIVATE_SENTINEL" }},
			{"missing assistant", assistantCatalogSelectorInvalid, func(_, s map[string]any) { delete(s, "assistant_ref") }},
			{"assistant type", assistantCatalogSelectorInvalid, func(_, s map[string]any) { s["assistant_ref"] = 7 }},
			{"assistant format", assistantCatalogSelectorInvalid, func(_, s map[string]any) { s["assistant_ref"] = "invalid/ref" }},
			{"missing entity kind", assistantCatalogSelectorInvalid, func(_, s map[string]any) { delete(s, "entity_kind") }},
			{"missing entity ref", assistantCatalogSelectorInvalid, func(_, s map[string]any) { delete(s, "entity_ref") }},
			{"missing entity pair", assistantCatalogSelectorInvalid, func(_, s map[string]any) { delete(s, "entity_kind"); delete(s, "entity_ref") }},
			{"entity kind type", assistantCatalogSelectorInvalid, func(_, s map[string]any) { s["entity_kind"] = 7 }},
			{"entity kind mismatch", assistantCatalogSelectorInvalid, func(_, s map[string]any) { s["entity_kind"] = "AGENT" }},
			{"entity ref format", assistantCatalogSelectorInvalid, func(_, s map[string]any) { s["entity_ref"] = "invalid/ref" }},
			{"query type", assistantCatalogSelectorInvalid, func(_, s map[string]any) { s["query"] = 7 }},
			{"query bound", assistantCatalogSelectorInvalid, func(_, s map[string]any) { s["query"] = strings.Repeat("я", 81) }},
			{"query not allowed", assistantCatalogSelectorInvalid, func(_, s map[string]any) { s["query"] = "PRIVATE_SENTINEL" }},
			{"offset type", assistantCatalogSelectorInvalid, func(_, s map[string]any) { s["offset"] = "PRIVATE_SENTINEL" }},
			{"offset fraction", assistantCatalogSelectorInvalid, func(_, s map[string]any) { s["offset"] = 0.5 }},
			{"offset bound", assistantCatalogSelectorInvalid, func(_, s map[string]any) { s["offset"] = 10001 }},
			{"offset negative", assistantCatalogSelectorInvalid, func(_, s map[string]any) { s["offset"] = -1 }},
			{"offset not allowed", assistantCatalogSelectorInvalid, func(_, s map[string]any) { s["offset"] = 1 }},
			{"account mixed", assistantCatalogSelectorInvalid, func(_, s map[string]any) { s["account_ref"] = "acc_owned123" }},
			{"profile mixed", assistantCatalogSelectorInvalid, func(_, s map[string]any) { s["runtime_profile_ref"] = "profile-owned123" }},
			{"agent query mixed", assistantCatalogShapeInvalid, func(a, _ map[string]any) { a["agent_query"] = "" }},
			{"agent offset mixed", assistantCatalogShapeInvalid, func(a, _ map[string]any) { a["agent_offset"] = 0 }},
			{"definition query mixed", assistantCatalogShapeInvalid, func(a, _ map[string]any) { a["definition_query"] = "" }},
			{"definition offset mixed", assistantCatalogShapeInvalid, func(a, _ map[string]any) { a["definition_offset"] = 0 }},
			{"operation selection mixed", assistantCatalogShapeInvalid, func(a, _ map[string]any) { a["operation_types"] = []any{"UPDATE_WORKFLOW"} }},
			{"page offset negative", assistantCatalogPageInvalid, func(_, s map[string]any) { s["configuration_offset_bytes"] = -1 }},
			{"page offset fraction", assistantCatalogPageInvalid, func(_, s map[string]any) { s["configuration_offset_bytes"] = 0.5 }},
			{"page offset type", assistantCatalogPageInvalid, func(_, s map[string]any) { s["configuration_offset_bytes"] = "PRIVATE_SENTINEL" }},
			{"page offset bound", assistantCatalogPageInvalid, func(_, s map[string]any) {
				s["configuration_offset_bytes"] = maximumAssistantCurrentConfigurationBytes + 1
			}},
			{"maximum below bound", assistantCatalogPageInvalid, func(_, s map[string]any) { s["maximum_bytes"] = 3 }},
			{"maximum above bound", assistantCatalogPageInvalid, func(_, s map[string]any) { s["maximum_bytes"] = 16385 }},
			{"maximum fraction", assistantCatalogPageInvalid, func(_, s map[string]any) { s["maximum_bytes"] = 4.5 }},
			{"maximum type", assistantCatalogPageInvalid, func(_, s map[string]any) { s["maximum_bytes"] = "PRIVATE_SENTINEL" }},
			{"digest type", assistantCatalogPageInvalid, func(_, s map[string]any) { s["configuration_sha256"] = 7 }},
			{"digest uppercase", assistantCatalogPageInvalid, func(_, s map[string]any) { s["configuration_sha256"] = strings.Repeat("A", 64) }},
			{"digest empty", assistantCatalogPageInvalid, func(_, s map[string]any) { s["configuration_sha256"] = "" }},
			{"continuation missing digest", assistantCatalogPageInvalid, func(_, s map[string]any) { s["configuration_offset_bytes"] = 1 }},
		} {
			t.Run(string(scope)+"/"+test.name, func(t *testing.T) {
				input, arguments, response := assistantWorkflowConfigurationFixture(t)
				input.AssistantScope = scope
				test.mutate(arguments, arguments["assistant_configuration_catalog"].(map[string]any))
				client := newAssistantCatalogInputDiagnosticClient(response)
				structured, wire, logs := assistantCatalogDiagnosticWire(t, input, arguments, client)
				if structured["error_code"] != assistantCatalogInputInvalidCode || structured["retryable"] != true || structured["guidance"] != assistantCatalogReadInputGuidance || client.calls != 0 ||
					client.projection.GetState() != controlplanev1.RunToolCallState_RUN_TOOL_CALL_STATE_FAILED || client.projection.GetSafeResult() != "TOOL_UNAVAILABLE" ||
					!strings.Contains(logs, `"failure_class":"`+test.class+`"`) {
					t.Fatal("local catalog input did not retain its closed classification, zero RPC count or FAILED activity receipt")
				}
				assertAssistantCatalogDiagnosticPrivacy(t, input, wire, logs)
				client.terminalFailure = true
				structured, wire, logs = assistantCatalogDiagnosticWire(t, input, arguments, client)
				if structured["error_code"] != "TOOL_UNAVAILABLE" || structured["retryable"] != false || structured["guidance"] != nil || client.calls != 0 {
					t.Fatal("failed terminal projection made malformed input recoverable")
				}
				assertAssistantCatalogDiagnosticPrivacy(t, input, wire, logs)
			})
		}
	}
}

// TestAssistantCatalogIndexAndDefinitionInputRecovery checks invalid index/definition filters and a missing MODELS account yield local corrective guidance without searching the owner.
// TestAssistantCatalogIndexAndDefinitionInputRecovery проверяет локальную подсказку исправления без поиска у владельца для некорректных фильтров индекса и определений, а также отсутствующего аккаунта MODELS.
func TestAssistantCatalogIndexAndDefinitionInputRecovery(t *testing.T) {
	for _, arguments := range []map[string]any{
		{"agent_query": 7}, {"agent_query": strings.Repeat("я", 81)}, {"agent_offset": -1}, {"agent_offset": 129}, {"agent_offset": 0.5}, {"agent_offset": "PRIVATE_SENTINEL"},
		{"definition_offset": 0}, {"definition_query": 7}, {"definition_query": strings.Repeat("я", 81)},
		{"definition_query": "", "definition_offset": -1}, {"definition_query": "", "definition_offset": 10001}, {"definition_query": "", "definition_offset": 0.5}, {"definition_query": "", "definition_offset": "PRIVATE_SENTINEL"},
	} {
		input, _, response := assistantWorkflowConfigurationFixture(t)
		client := newAssistantCatalogInputDiagnosticClient(response)
		structured, wire, logs := assistantCatalogDiagnosticWire(t, input, arguments, client)
		if structured["error_code"] != assistantCatalogInputInvalidCode || structured["retryable"] != true || client.calls != 0 {
			t.Fatal("index or definition input lost local recovery or reached owner search")
		}
		assertAssistantCatalogDiagnosticPrivacy(t, input, wire, logs)
	}
	input, arguments, response := assistantFreshCatalogFixture(runtimecontract.AssistantScopeProject, "MODELS")
	delete(arguments["assistant_configuration_catalog"].(map[string]any), "account_ref")
	client := newAssistantCatalogInputDiagnosticClient(response.AssistantConfigurationCatalog)
	structured, _, _ := assistantCatalogDiagnosticWire(t, input, arguments, client)
	if structured["error_code"] != assistantCatalogInputInvalidCode || client.calls != 0 {
		t.Fatal("missing required MODELS account was not local input")
	}
}

// TestAssistantCatalogInputRecoveryDoesNotClassifyAuthorityOrSnapshotFailures checks absent trusted context/lease, foreign identity and malformed or stale owner snapshots never become retryable input errors.
// Expected search counts distinguish local refusal from failures discovered after one owner RPC in both scopes.
// TestAssistantCatalogInputRecoveryDoesNotClassifyAuthorityOrSnapshotFailures проверяет, что отсутствие доверенного контекста или аренды, чужая идентичность, некорректный либо устаревший снимок владельца не становятся исправимыми ошибками ввода.
// Ожидаемые счётчики поиска различают локальный отказ и ошибки после одного RPC владельца в обеих областях.
func TestAssistantCatalogInputRecoveryDoesNotClassifyAuthorityOrSnapshotFailures(t *testing.T) {
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeSystem, runtimecontract.AssistantScopeProject} {
		for _, test := range []struct {
			name   string
			calls  int
			mutate func(*runtimecontract.RunnerInput, map[string]any, *controlplanev1.AssistantConfigurationCatalogResponse)
		}{
			{"missing organization", 0, func(i *runtimecontract.RunnerInput, _ map[string]any, _ *controlplanev1.AssistantConfigurationCatalogResponse) {
				i.OrganizationRef = ""
			}},
			{"missing lease", 0, func(i *runtimecontract.RunnerInput, _ map[string]any, _ *controlplanev1.AssistantConfigurationCatalogResponse) {
				i.LeaseRef = ""
			}},
			{"missing fence", 0, func(i *runtimecontract.RunnerInput, _ map[string]any, _ *controlplanev1.AssistantConfigurationCatalogResponse) {
				i.LeaseFence = ""
			}},
			{"missing generation", 0, func(i *runtimecontract.RunnerInput, _ map[string]any, _ *controlplanev1.AssistantConfigurationCatalogResponse) {
				i.LeaseGeneration = 0
			}},
			{"wrong context", 0, func(i *runtimecontract.RunnerInput, _ map[string]any, _ *controlplanev1.AssistantConfigurationCatalogResponse) {
				i.AssistantContext.EntityKind = "PROJECT"
			}},
			{"missing context version", 0, func(i *runtimecontract.RunnerInput, _ map[string]any, _ *controlplanev1.AssistantConfigurationCatalogResponse) {
				i.AssistantContext.EntityVersion = nil
			}},
			{"revoked operation", 0, func(i *runtimecontract.RunnerInput, _ map[string]any, _ *controlplanev1.AssistantConfigurationCatalogResponse) {
				i.AssistantContext.AllowedOperations = []string{}
			}},
			{"revoked operation with malformed page", 0, func(i *runtimecontract.RunnerInput, s map[string]any, _ *controlplanev1.AssistantConfigurationCatalogResponse) {
				i.AssistantContext.AllowedOperations = []string{}
				s["maximum_bytes"] = 3
			}},
			{"foreign helper", 0, func(_ *runtimecontract.RunnerInput, s map[string]any, _ *controlplanev1.AssistantConfigurationCatalogResponse) {
				s["assistant_ref"] = "agt_foreign123"
			}},
			{"foreign entity", 0, func(_ *runtimecontract.RunnerInput, s map[string]any, _ *controlplanev1.AssistantConfigurationCatalogResponse) {
				s["entity_ref"] = "wfl_foreign123"
			}},
			{"stale snapshot", 1, func(_ *runtimecontract.RunnerInput, _ map[string]any, r *controlplanev1.AssistantConfigurationCatalogResponse) {
				r.WorkflowConfiguration.Version++
			}},
			{"foreign project", 1, func(_ *runtimecontract.RunnerInput, _ map[string]any, r *controlplanev1.AssistantConfigurationCatalogResponse) {
				r.ProjectRef = "prj_foreign123"
			}},
			{"foreign response helper", 1, func(_ *runtimecontract.RunnerInput, _ map[string]any, r *controlplanev1.AssistantConfigurationCatalogResponse) {
				r.AssistantRef = "agt_foreign123"
			}},
			{"unknown proto", 1, func(_ *runtimecontract.RunnerInput, _ map[string]any, r *controlplanev1.AssistantConfigurationCatalogResponse) {
				r.WorkflowConfiguration.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01})
			}},
			{"mixed snapshot", 1, func(_ *runtimecontract.RunnerInput, _ map[string]any, r *controlplanev1.AssistantConfigurationCatalogResponse) {
				r.AgentConfiguration = &controlplanev1.AssistantAgentConfiguration{}
			}},
			{"wrong source digest", 1, func(_ *runtimecontract.RunnerInput, _ map[string]any, r *controlplanev1.AssistantConfigurationCatalogResponse) {
				r.WorkflowConfiguration.ConfigurationSha256 = strings.Repeat("a", 64)
			}},
			{"wrong expected digest", 1, func(_ *runtimecontract.RunnerInput, s map[string]any, _ *controlplanev1.AssistantConfigurationCatalogResponse) {
				s["configuration_sha256"] = strings.Repeat("c", 64)
			}},
			{"offset beyond EOF", 1, func(_ *runtimecontract.RunnerInput, s map[string]any, r *controlplanev1.AssistantConfigurationCatalogResponse) {
				s["configuration_offset_bytes"] = len(r.WorkflowConfiguration.ConfigurationJson) + 1
				s["configuration_sha256"] = r.WorkflowConfiguration.ConfigurationSha256
			}},
		} {
			t.Run(string(scope)+"/"+test.name, func(t *testing.T) {
				input, arguments, response := assistantWorkflowConfigurationFixture(t)
				input.AssistantScope = scope
				test.mutate(&input, arguments["assistant_configuration_catalog"].(map[string]any), response)
				client := newAssistantCatalogInputDiagnosticClient(response)
				structured, wire, logs := assistantCatalogDiagnosticWire(t, input, arguments, client)
				if structured["error_code"] != "TOOL_UNAVAILABLE" || structured["retryable"] != false || structured["guidance"] != nil || client.calls != test.calls {
					t.Fatal("authority or snapshot failure was treated as recoverable caller input")
				}
				assertAssistantCatalogDiagnosticPrivacy(t, input, wire, logs)
			})
		}
	}
}

// TestAssistantCatalogInputMarkerDoesNotTrustRemoteTextOrUnregisteredClass checks forged error text, unknown local wrappers and owner status codes cannot acquire local retry classification.
// An authorization-recording refusal also prevents recovery and search, even when the selector itself is malformed.
// TestAssistantCatalogInputMarkerDoesNotTrustRemoteTextOrUnregisteredClass проверяет, что подставной текст ошибки, неизвестные локальные оболочки и коды владельца не получают локальную классификацию повтора.
// Отказ записи авторизации также запрещает восстановление и поиск, даже если сам селектор некорректен.
func TestAssistantCatalogInputMarkerDoesNotTrustRemoteTextOrUnregisteredClass(t *testing.T) {
	for _, err := range []error{errors.New(assistantCatalogInputFailureMessage), errAssistantConfigurationPage,
		status.Error(codes.InvalidArgument, assistantCatalogInputFailureMessage),
		status.Error(codes.Unknown, assistantCatalogShapeInvalid),
		&assistantCatalogInputError{class: "PRIVATE_SENTINEL"}, (*assistantCatalogInputError)(nil)} {
		if assistantCatalogRecoveryGuidance(err) != "" || assistantCatalogFailureClass(err) != "" {
			t.Fatal("unregistered error or remote text acquired local catalog input classification")
		}
	}
	for _, code := range []codes.Code{codes.InvalidArgument, codes.PermissionDenied, codes.Unavailable, codes.Unknown} {
		input, arguments, response := assistantWorkflowConfigurationFixture(t)
		client := newAssistantCatalogInputDiagnosticClient(response)
		client.failure = status.Error(code, "PRIVATE_OWNER_ERROR "+assistantCatalogInputFailureMessage)
		structured, wire, logs := assistantCatalogDiagnosticWire(t, input, arguments, client)
		if structured["error_code"] != "TOOL_UNAVAILABLE" || structured["retryable"] != false || structured["guidance"] != nil || client.calls != 1 {
			t.Fatal("owner RPC error was classified by code or text as local input")
		}
		assertAssistantCatalogDiagnosticPrivacy(t, input, wire, logs)
	}
	input, arguments, response := assistantWorkflowConfigurationFixture(t)
	arguments["assistant_configuration_catalog"].(map[string]any)["maximum_bytes"] = 3
	client := newAssistantCatalogInputDiagnosticClient(response)
	client.authorizationFailure = true
	server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}, logger: slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))}
	params, _ := json.Marshal(map[string]any{"name": "get_configuration_catalog", "arguments": arguments})
	recorder := httptest.NewRecorder()
	server.callTool(recorder, httptest.NewRequest("POST", "/mcp", nil), mcpRequest{ID: json.RawMessage(`"catalog-authorization"`), Params: params}, input)
	if !strings.Contains(recorder.Body.String(), "Tool authorization unavailable") || strings.Contains(recorder.Body.String(), assistantCatalogInputInvalidCode) || client.calls != 0 {
		t.Fatal("authorization failure allowed local recovery or owner read")
	}
}

// TestAssistantCatalogAgentAndCurrentInputKeepExactOwnership checks ten agent/current-read cases in both scopes separate fixable page/query syntax from foreign identities, revoked context and missing runtime revision.
// Every case rejects before search and preserves the correct retry/privacy boundary.
// TestAssistantCatalogAgentAndCurrentInputKeepExactOwnership проверяет десять случаев чтения сотрудника и текущей конфигурации в обеих областях, отделяя исправимый синтаксис страницы и запроса от чужой идентичности, отозванного контекста и отсутствующей ревизии выполнения.
// Каждый случай отклоняется до поиска с сохранением правильной границы повтора и приватности.
func TestAssistantCatalogAgentAndCurrentInputKeepExactOwnership(t *testing.T) {
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeSystem, runtimecontract.AssistantScopeProject} {
		for _, scenario := range []string{"agent page", "agent query", "agent missing entity", "agent foreign helper", "agent foreign entity", "agent revoked", "current query", "current missing revision", "current foreign helper", "page on current"} {
			t.Run(string(scope)+"/"+scenario, func(t *testing.T) {
				input, arguments, response := assistantAgentConfigurationFixture(t)
				input.AssistantScope = scope
				if strings.HasPrefix(scenario, "current") || scenario == "page on current" {
					var owner *controlplanev1.SearchAssistantResourcesResponse
					input, arguments, owner = assistantOwnCurrentFixture(scope)
					response = owner.AssistantConfigurationCatalog
				}
				selector := arguments["assistant_configuration_catalog"].(map[string]any)
				recoverable := true
				switch scenario {
				case "agent page":
					selector["maximum_bytes"] = 3
				case "agent query", "current query":
					selector["query"] = "PRIVATE_SENTINEL"
				case "agent missing entity":
					delete(selector, "entity_kind")
					delete(selector, "entity_ref")
				case "agent foreign helper", "current foreign helper":
					selector["assistant_ref"] = "agt_foreign123"
					recoverable = false
				case "agent foreign entity":
					selector["entity_ref"] = "agt_foreign123"
					recoverable = false
				case "agent revoked":
					input.AssistantContext.AllowedOperations = []string{}
					recoverable = false
				case "current missing revision":
					input.RuntimeRevisionDigest = ""
					recoverable = false
				case "page on current":
					selector["maximum_bytes"] = 4096
				}
				client := newAssistantCatalogInputDiagnosticClient(response)
				structured, wire, logs := assistantCatalogDiagnosticWire(t, input, arguments, client)
				want := "TOOL_UNAVAILABLE"
				if recoverable {
					want = assistantCatalogInputInvalidCode
				}
				if structured["error_code"] != want || structured["retryable"] != recoverable || client.calls != 0 || !recoverable && structured["guidance"] != nil {
					t.Fatal("agent/current input classification changed exact own-resource or runtime-revision boundary")
				}
				if input.AssistantContext != nil {
					assertAssistantCatalogDiagnosticPrivacy(t, input, wire, logs)
				}
			})
		}
	}
}

// TestAssistantCatalogInputRecoveryPreservesFullConfigurationReads reassembles successful workflow/agent configuration pages through EOF in both scopes, checking repeated exact lease pins and final successful activity.
// TestAssistantCatalogInputRecoveryPreservesFullConfigurationReads собирает успешные страницы конфигурации процесса и сотрудника до конца в обеих областях, проверяя повторные точные привязки аренды и итоговую успешную активность.
func TestAssistantCatalogInputRecoveryPreservesFullConfigurationReads(t *testing.T) {
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeSystem, runtimecontract.AssistantScopeProject} {
		for _, kind := range []string{"WORKFLOW_CONFIGURATION", "AGENT_CONFIGURATION"} {
			t.Run(string(scope)+"/"+kind, func(t *testing.T) {
				input, arguments, response := assistantWorkflowConfigurationFixture(t)
				key, raw := "workflow_configuration", response.WorkflowConfiguration.ConfigurationJson
				if kind == "AGENT_CONFIGURATION" {
					input, arguments, response = assistantAgentConfigurationFixture(t)
					key, raw = "agent_configuration", response.AgentConfiguration.ConfigurationJson
				}
				input.AssistantScope = scope
				arguments["operation_types"] = []any{}
				client := newAssistantCatalogInputDiagnosticClient(response)
				server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
				readAssistantConfigurationMCP(t, input, arguments, server, key, raw)
				if client.calls < 2 || client.request.GetLeaseRef() != input.LeaseRef || client.request.GetFence() != input.LeaseFence || client.request.GetGeneration() != input.LeaseGeneration ||
					client.projection.GetState() != controlplanev1.RunToolCallState_RUN_TOOL_CALL_STATE_SUCCEEDED {
					t.Fatal("local diagnostics changed successful EOF read, exact lease or activity result")
				}
			})
		}
	}
}

// TestAssistantWorkflowUpdateSchemaExplainsDependencyRetentionWithoutNewFields checks workflow-update guidance retains dependency edges, parallelism, result schema and defaults without exposing editable dependsOn/draft fields.
// TestAssistantWorkflowUpdateSchemaExplainsDependencyRetentionWithoutNewFields проверяет сохранение в подсказке изменения процесса зависимостей, параллельности, схемы результата и значений по умолчанию без объявления редактируемых полей dependsOn или draft.
func TestAssistantWorkflowUpdateSchemaExplainsDependencyRetentionWithoutNewFields(t *testing.T) {
	schema := workflowUpdateInputSchema("wfl_fixture123")
	steps := schema["properties"].(map[string]any)["steps"].(map[string]any)
	guidance, ok := steps["description"].(string)
	step := steps["items"].(map[string]any)
	properties := step["properties"].(map[string]any)
	if !ok || !strings.Contains(guidance, "count, order, keys and parallelism") || !strings.Contains(guidance, "numeric parallelGroup") || !strings.Contains(guidance, "draft.Steps[].DependsOn") ||
		!strings.Contains(guidance, "retain ALL original edges") || !strings.Contains(guidance, "waits for ALL peers") || !strings.Contains(guidance, "After.draft") || !strings.Contains(guidance, "Omit key for new steps") ||
		!strings.Contains(guidance, "owner must verify") || !strings.Contains(guidance, "returns plan locators") || !strings.Contains(guidance, "After Apply, read full WORKFLOW_CONFIGURATION") ||
		!strings.Contains(guidance, "ResultSchema is retained") || !strings.Contains(guidance, "input defaults are preserved by input key") || step["additionalProperties"] != false || properties["dependsOn"] != nil || properties["draft"] != nil {
		t.Fatal("workflow update guidance changed editable fields or omitted dependency retention conditions")
	}
}
