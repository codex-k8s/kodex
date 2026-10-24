package httptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// secretRevisionPath names revision two of the synthetic runtime secret for local impact and
// consumer-rebinding HTTP tests.
// secretRevisionPath задаёт маршрут второй ревизии синтетического runtime secret для локальных
// HTTP-тестов влияния и перепривязки потребителей.
const secretRevisionPath = "/api/v1/runtime-secrets/sec_fixture01/revisions/2"

// secretRebindBody provides one synthetic environment selection with source-version, scope and
// agent/binding version pins for secret rebinding.
// secretRebindBody задаёт выбор одного синтетического окружения с исходной версией, областью и
// pins версий агента и привязки для перепривязки секрета.
const secretRebindBody = `{"selections":[{"environmentRef":"env_fixture01","expectedEnvironmentVersion":4,"sourceVersionRef":"ever_fixture01","scopeKind":"PROJECT","organizationRef":"org_fixture01","projectRef":"prj_fixture01","consumers":[{"agentRef":"agt_fixture01","agentVersion":5,"bindingRef":"bind_fixture01","bindingVersion":2,"versionRef":"ever_fixture01","projectRef":"prj_fixture01","scopeKind":"PROJECT","organizationRef":"org_fixture01"}]}]}`

// secretImpactRecorder captures a cloned secret-impact or rebind request and supports injected
// owner failures and response corruption.
// secretImpactRecorder сохраняет копию запроса влияния либо перепривязки секрета и поддерживает
// заданные ошибки владельца и повреждение ответа.
type secretImpactRecorder struct {
	grpc.ClientConnInterface
	request proto.Message
	failure error
	corrupt func(proto.Message)
}

// Invoke returns typed synthetic impact or rebind data after recording the request, including
// private environment values to test receipt filtering.
// Invoke возвращает типизированные синтетические данные влияния либо перепривязки после записи
// запроса, включая внутренние значения окружения для проверки фильтрации квитанции.
func (client *secretImpactRecorder) Invoke(_ context.Context, method string, request, response any, _ ...grpc.CallOption) error {
	client.request = proto.Clone(request.(proto.Message))
	if client.failure != nil {
		return client.failure
	}
	var result proto.Message
	switch method {
	case controlplanev1.PlatformQueryService_GetRuntimeSecretImpact_FullMethodName:
		result = &controlplanev1.GetRuntimeSecretImpactResponse{SecretRef: "sec_fixture01", SecretVersion: 3, TargetRevision: 2, Total: 2, Page: &controlplanev1.PageInfo{NextPageToken: "cursor-fixture"},
			Consumers: []*controlplanev1.RuntimeSecretImpactConsumer{
				{EnvironmentRef: "env_fixture01", EnvironmentVersion: 4, EnvironmentVersionRef: "ever_fixture01", ScopeKind: controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT, OrganizationRef: "org_fixture01", ProjectRef: "prj_fixture01", SecretRevisions: []int64{1}, Consumer: &controlplanev1.RuntimeEnvironmentConsumer{AgentRef: "agt_fixture01", AgentVersion: 5, BindingRef: "bind_fixture01", BindingVersion: 2, VersionRef: "ever_fixture01", ProjectRef: "prj_fixture01", ScopeKind: controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT, OrganizationRef: "org_fixture01"}},
				{EnvironmentRef: "env_fixture02", EnvironmentVersion: 1, EnvironmentVersionRef: "ever_unbound01", ScopeKind: controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT, OrganizationRef: "org_fixture01", ProjectRef: "prj_fixture01", SecretRevisions: []int64{1}, Consumer: &controlplanev1.RuntimeEnvironmentConsumer{VersionRef: "ever_unbound01", ProjectRef: "prj_fixture01", ScopeKind: controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT, OrganizationRef: "org_fixture01"}},
			}}
	case controlplanev1.PlatformCommandService_RebindRuntimeSecret_FullMethodName:
		result = &controlplanev1.RebindRuntimeSecretResponse{Environments: []*controlplanev1.RuntimeEnvironmentSet{{Ref: "env_fixture01", Version: 5, ProjectRef: "prj_fixture01", ScopeKind: controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT, OrganizationRef: "org_fixture01", CurrentVersion: &controlplanev1.RuntimeEnvironmentVersion{
			Ref: "ever_fixture02", Digest: strings.Repeat("b", 64), Values: []*controlplanev1.RuntimeEnvironmentValue{{Name: "PRIVATE_ENV", Value: "private-value-not-for-receipt"}}, SecretDescriptors: []*controlplanev1.RuntimeSecretDescriptor{{SecretRef: "sec_fixture01", Revision: 2}}}}},
			Bindings: []*controlplanev1.AgentRuntimeEnvironmentBinding{{Ref: "bind_fixture01", Version: 3, AgentRef: "agt_fixture01", EnvironmentRef: "env_fixture01", VersionRef: "ever_fixture02", Digest: strings.Repeat("c", 64)}}}
	default:
		return status.Error(codes.Unimplemented, "unexpected test method")
	}
	if client.corrupt != nil {
		client.corrupt(result)
	}
	proto.Merge(response.(proto.Message), result)
	return nil
}

// secretImpactHandler connects the generated HTTP router to query and command clients backed by
// the local secret-impact recorder.
// secretImpactHandler соединяет generated HTTP-маршрутизатор с query- и command-клиентами
// локальной заглушки записи влияния секрета.
func secretImpactHandler(client *secretImpactRecorder) http.Handler {
	return generated.Handler(&Server{control: &controlplaneclient.Client{Query: controlplanev1.NewPlatformQueryServiceClient(client), Command: controlplanev1.NewPlatformCommandServiceClient(client)}})
}

// TestSecretImpactExactMappingIncludesUnboundEnvironment checks exact secret revision and
// pagination mapping, preserving both bound and unbound environments, ETag and noncacheable impact
// metadata.
// TestSecretImpactExactMappingIncludesUnboundEnvironment проверяет точное отображение ревизии
// секрета и пагинации, сохранение привязанных и непривязанных окружений, ETag и некэшируемых
// метаданных влияния.
func TestSecretImpactExactMappingIncludesUnboundEnvironment(t *testing.T) {
	client := &secretImpactRecorder{}
	w := httptest.NewRecorder()
	secretImpactHandler(client).ServeHTTP(w, managedTestRequest(http.MethodGet, secretRevisionPath+"/impact?pageSize=7&pageToken=prior-fixture", ""))
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	input := client.request.(*controlplanev1.GetRuntimeSecretImpactRequest)
	if input.SecretRef != "sec_fixture01" || input.Revision != 2 || input.GetPage().PageSize != 7 || input.GetPage().PageToken != "prior-fixture" {
		t.Fatal(input)
	}
	var result generated.RuntimeSecretImpact
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Consumers) != 2 || result.Consumers[0].Consumer == nil || result.Consumers[1].Consumer != nil || result.Consumers[1].ProjectRef != "prj_fixture01" || result.NextPageToken != "cursor-fixture" || w.Header().Get("ETag") != `"3"` || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("incomplete impact: %s", w.Body.String())
	}
}

// TestSecretRebindExactMappingAndSafeReceipt checks secret and environment version fencing, source
// consumer pins and idempotency with new version receipts that omit private environment values.
// TestSecretRebindExactMappingAndSafeReceipt проверяет ограничения версий секрета и окружения,
// pins исходных потребителей и идемпотентность с квитанциями новых версий без внутренних значений
// окружения.
func TestSecretRebindExactMappingAndSafeReceipt(t *testing.T) {
	client := &secretImpactRecorder{}
	w := httptest.NewRecorder()
	secretImpactHandler(client).ServeHTTP(w, managedTestRequest(http.MethodPost, secretRevisionPath+"/consumer-bindings", secretRebindBody))
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	input := client.request.(*controlplanev1.RebindRuntimeSecretRequest)
	if input.SecretRef != "sec_fixture01" || input.Revision != 2 || input.GetMutation().GetExpectedVersion() != 3 || input.GetMutation().GetIdempotencyKey() != "managed-fixture-01" || len(input.Selections) != 1 {
		t.Fatal(input)
	}
	selection := input.Selections[0]
	if selection.EnvironmentRef != "env_fixture01" || selection.ExpectedEnvironmentVersion != 4 || selection.SourceVersionRef != "ever_fixture01" || len(selection.Consumers) != 1 || selection.Consumers[0].VersionRef != "ever_fixture01" {
		t.Fatal(selection)
	}
	var result generated.RuntimeSecretRebindResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Environments) != 1 || result.Environments[0].VersionRef != "ever_fixture02" || len(result.Bindings) != 1 || stringValue(result.Bindings[0].VersionRef) != "ever_fixture02" || strings.Contains(w.Body.String(), "private-value") || strings.Contains(w.Body.String(), "PRIVATE_ENV") {
		t.Fatalf("invalid receipt: %s", w.Body.String())
	}
}

// TestSecretRebindAllowsPublicationWithoutAgentBindings checks successful publication of a
// selected environment with an explicitly empty consumer list and no returned agent bindings.
// TestSecretRebindAllowsPublicationWithoutAgentBindings проверяет успешную публикацию выбранного
// окружения с явно пустым списком потребителей и отсутствием возвращённых привязок агентов.
func TestSecretRebindAllowsPublicationWithoutAgentBindings(t *testing.T) {
	client := &secretImpactRecorder{corrupt: func(m proto.Message) { m.(*controlplanev1.RebindRuntimeSecretResponse).Bindings = nil }}
	w := httptest.NewRecorder()
	body := `{"selections":[{"environmentRef":"env_fixture01","expectedEnvironmentVersion":4,"sourceVersionRef":"ever_fixture01","scopeKind":"PROJECT","organizationRef":"org_fixture01","projectRef":"prj_fixture01","consumers":[]}]}`
	secretImpactHandler(client).ServeHTTP(w, managedTestRequest(http.MethodPost, secretRevisionPath+"/consumer-bindings", body))
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

// TestSecretImpactRejectInvalidInputBeforeRPC checks revision and page bounds, forged authority,
// empty or repeated selections, missing consumers and mismatched source versions before owner RPC.
// TestSecretImpactRejectInvalidInputBeforeRPC проверяет границы ревизии и страницы, поддельные
// полномочия, пустой либо повторный выбор, отсутствие потребителей и несовпадение исходных версий
// до RPC владельца.
func TestSecretImpactRejectInvalidInputBeforeRPC(t *testing.T) {
	for _, tc := range []struct{ name, method, path, body string }{
		{"zero-revision", "GET", strings.Replace(secretRevisionPath, "/2", "/0", 1) + "/impact", ""},
		{"unsafe-revision", "GET", strings.Replace(secretRevisionPath, "/2", "/9007199254740992", 1) + "/impact", ""},
		{"overflow-page", "GET", secretRevisionPath + "/impact?pageSize=4294967297", ""},
		{"empty", "POST", secretRevisionPath + "/consumer-bindings", `{"selections":[]}`},
		{"missing-consumers", "POST", secretRevisionPath + "/consumer-bindings", `{"selections":[{"environmentRef":"env_fixture01","expectedEnvironmentVersion":4,"sourceVersionRef":"ever_fixture01"}]}`},
		{"authority", "POST", secretRevisionPath + "/consumer-bindings", strings.TrimSuffix(secretRebindBody, "}") + `,"organizationId":"forged"}`},
		{"wrong-source", "POST", secretRevisionPath + "/consumer-bindings", strings.Replace(secretRebindBody, `"versionRef":"ever_fixture01"`, `"versionRef":"ever_other01"`, 1)},
		{"unsafe-occ", "POST", secretRevisionPath + "/consumer-bindings", strings.Replace(secretRebindBody, `"expectedEnvironmentVersion":4`, `"expectedEnvironmentVersion":9007199254740992`, 1)},
		{"duplicate-environment", "POST", secretRevisionPath + "/consumer-bindings", strings.TrimSuffix(secretRebindBody, "]}") + "," + strings.TrimPrefix(secretRebindBody, `{"selections":[`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &secretImpactRecorder{}
			w := httptest.NewRecorder()
			secretImpactHandler(client).ServeHTTP(w, managedTestRequest(tc.method, tc.path, tc.body))
			if w.Code != 400 || client.request != nil {
				t.Fatalf("status=%d called=%t", w.Code, client.request != nil)
			}
		})
	}
}

// TestSecretImpactRejectUpstreamMismatch checks rejection of foreign secret or target, partial
// consumer identity, wrong secret descriptors and incomplete or unselected rebind results.
// TestSecretImpactRejectUpstreamMismatch проверяет отказ чужим секрету либо цели, частичной
// identity потребителя, неверным дескрипторам секрета и неполным либо невыбранным результатам
// перепривязки.
func TestSecretImpactRejectUpstreamMismatch(t *testing.T) {
	for _, tc := range []struct {
		name, method, suffix, body string
		corrupt                    func(proto.Message)
	}{
		{"wrong-secret", "GET", "/impact", "", func(m proto.Message) { m.(*controlplanev1.GetRuntimeSecretImpactResponse).SecretRef = "sec_other01" }},
		{"wrong-target", "GET", "/impact", "", func(m proto.Message) { m.(*controlplanev1.GetRuntimeSecretImpactResponse).TargetRevision = 3 }},
		{"partial-consumer", "GET", "/impact", "", func(m proto.Message) {
			m.(*controlplanev1.GetRuntimeSecretImpactResponse).Consumers[1].Consumer.BindingRef = "bind_hidden01"
		}},
		{"wrong-descriptor", "POST", "/consumer-bindings", secretRebindBody, func(m proto.Message) {
			m.(*controlplanev1.RebindRuntimeSecretResponse).Environments[0].CurrentVersion.SecretDescriptors[0].Revision = 1
		}},
		{"unselected-environment", "POST", "/consumer-bindings", secretRebindBody, func(m proto.Message) {
			m.(*controlplanev1.RebindRuntimeSecretResponse).Environments[0].Ref = "env_other01"
		}},
		{"incomplete-bindings", "POST", "/consumer-bindings", secretRebindBody, func(m proto.Message) { m.(*controlplanev1.RebindRuntimeSecretResponse).Bindings = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &secretImpactRecorder{corrupt: tc.corrupt}
			w := httptest.NewRecorder()
			secretImpactHandler(client).ServeHTTP(w, managedTestRequest(tc.method, secretRevisionPath+tc.suffix, tc.body))
			if w.Code != 502 {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

// TestSecretImpactAuthorityFailureNeverBecomesSuccess checks concrete owner failure mapping on
// both impact reads and rebind commands without exposing private provider diagnostic text.
// TestSecretImpactAuthorityFailureNeverBecomesSuccess проверяет конкретное отображение ошибок
// владельца при чтении влияния и командах перепривязки без раскрытия внутренней диагностики
// провайдера.
func TestSecretImpactAuthorityFailureNeverBecomesSuccess(t *testing.T) {
	for _, code := range []codes.Code{codes.PermissionDenied, codes.Unauthenticated, codes.NotFound, codes.Aborted, codes.Unavailable} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			client := &secretImpactRecorder{failure: status.Error(code, "private-provider-detail")}
			w := httptest.NewRecorder()
			suffix, body := "/impact", ""
			if method == http.MethodPost {
				suffix, body = "/consumer-bindings", secretRebindBody
			}
			secretImpactHandler(client).ServeHTTP(w, managedTestRequest(method, secretRevisionPath+suffix, body))
			want := map[codes.Code]int{codes.PermissionDenied: 403, codes.NotFound: 404, codes.Aborted: 412, codes.Unauthenticated: 401, codes.Unavailable: 503}[code]
			if w.Code != want || strings.Contains(w.Body.String(), "private-provider-detail") {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		}
	}
}
