package httptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// managedFixtureContent is synthetic Cyrillic source text with enum- and translation-like prefixes
// used to detect unintended response localization. It is not a real user document or executable
// configuration.
// managedFixtureContent является синтетическим кириллическим текстом с похожими на enum и ключ
// перевода префиксами для выявления нежелательной локализации ответа. Это не реальный документ
// пользователя или исполняемая конфигурация.
const managedFixtureContent = "TYPE_остается исходным текстом\ni18n:это не ключ перевода"

// managedFixture creates fresh typed configuration and revision messages with fixed time, source,
// digest, and version pins for HTTP tests. Fixture metadata describes no real published or authorized
// resource.
// managedFixture создаёт новые типизированные сообщения конфигурации и ревизии с фиксированными
// временем, исходником, дайджестом и версиями для HTTP-тестов. Метаданные не описывают реальный
// опубликованный или разрешённый ресурс.
func managedFixture() (*controlplanev1.ManagedConfigurationSet, *controlplanev1.ManagedConfigurationRevision) {
	now := timestamppb.New(time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC))
	revision := &controlplanev1.ManagedConfigurationRevision{
		Ref: "mrev_fixture01", Revision: 2, State: controlplanev1.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_VALID,
		ContentFormat: "TEXT", Content: managedFixtureContent, Digest: strings.Repeat("a", 64), CreatedAt: now,
	}
	configuration := &controlplanev1.ManagedConfigurationSet{
		Ref: "mcfg_fixture01", Version: 3, Name: "TYPE_Название", Kind: controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_PROMPT_TEMPLATE,
		ManagedBy: controlplanev1.ManagedConfigurationOwner_MANAGED_CONFIGURATION_OWNER_UI, UpdatedAt: now,
	}
	return configuration, revision
}

// managedRPCRecorder is an in-process unary client fixture that captures a cloned request and returns
// configurable failure or synthetic projections. It does not provide a real transport, session
// authentication, owner transaction, or general stream implementation.
// managedRPCRecorder является внутрипроцессной unary-подменой клиента, сохраняющей копию запроса и
// возвращающей заданную ошибку или синтетические проекции. Реальные транспорт, аутентификация сессии,
// транзакция владельца и общая реализация потоков отсутствуют.
type managedRPCRecorder struct {
	grpc.ClientConnInterface
	method        string
	request       proto.Message
	failure       error
	revisionState controlplanev1.ManagedConfigurationState
}

// Invoke captures the method and a deep Proto copy before returning the configured failure or
// constructing a method-specific fixture response. The default fills the generated
// configuration/revision fields by reflection; these synthetic replies are not evidence of real owner
// lifecycle transitions.
// Invoke сохраняет метод и глубокую Proto-копию до возврата заданной ошибки или создания
// соответствующего методу ответа. По умолчанию сгенерированные поля конфигурации и ревизии заполняются
// рефлексией; синтетические ответы не доказывают реальные переходы владельца.
func (client *managedRPCRecorder) Invoke(_ context.Context, method string, request, response any, _ ...grpc.CallOption) error {
	client.method, client.request = method, proto.Clone(request.(proto.Message))
	if client.failure != nil {
		return client.failure
	}
	configuration, revision := managedFixture()
	if client.revisionState != 0 {
		revision.State = client.revisionState
	}
	var output proto.Message
	switch {
	case strings.HasSuffix(method, "/PublishPromptTemplateDraft"):
		plan := revisionImpactFixture()
		plan.Kind = controlplanev1.RevisionImpactKind_REVISION_IMPACT_KIND_PROMPT_TEMPLATE
		plan.SourceRef, plan.SourceVersion, plan.DraftRef, plan.DraftVersion = configuration.Ref, configuration.Version, revision.Ref, revision.Revision
		plan.State, plan.Version, plan.PublishedRevisionRef = controlplanev1.RevisionImpactState_REVISION_IMPACT_STATE_APPLIED, 2, revision.Ref
		configuration.Version++
		revision.State = controlplanev1.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_PUBLISHED
		output = &controlplanev1.PublishPromptTemplateDraftResponse{Configuration: configuration, Revision: revision, Plan: plan}
	case strings.HasSuffix(method, "/DetachGitManagedConfiguration"):
		output = &controlplanev1.DetachGitManagedConfigurationResponse{Configuration: configuration}
	case strings.HasSuffix(method, "/ListManagedConfigurationHistory"):
		output = &controlplanev1.ListManagedConfigurationHistoryResponse{Configuration: configuration, Revisions: []*controlplanev1.ManagedConfigurationRevision{revision}, Total: 2, Page: &controlplanev1.PageInfo{NextPageToken: "next-page-2"}}
	case strings.HasSuffix(method, "/GetManagedConfigurationImpact"):
		output = &controlplanev1.GetManagedConfigurationImpactResponse{Impact: &controlplanev1.ManagedConfigurationImpact{ConfigurationRef: configuration.Ref, TargetRevisionRef: revision.Ref, Digest: strings.Repeat("b", 64)}}
	case strings.HasSuffix(method, "/GetSystemSTTConfiguration"):
		output = &controlplanev1.GetSystemSTTConfigurationResponse{Configuration: &controlplanev1.SystemSTTConfiguration{ConfigurationRef: configuration.Ref, RevisionRef: revision.Ref, Revision: 2, Digest: revision.Digest, ProviderAccountRef: "pacc_fixture01", Model: "gpt-transcribe", Language: "ru", PermissionKey: "platform.stt.use",
			Parameters: &controlplanev1.SystemSTTParameters{}, MaximumAudioBytes: 10 << 20, MaximumAudioDurationMilliseconds: 120000, ProviderTimeoutMilliseconds: 15000}}
	default:
		target := response.(proto.Message).ProtoReflect()
		target.Set(target.Descriptor().Fields().ByName("configuration"), protoreflect.ValueOfMessage(configuration.ProtoReflect()))
		target.Set(target.Descriptor().Fields().ByName("revision"), protoreflect.ValueOfMessage(revision.ProtoReflect()))
		return nil
	}
	proto.Merge(response.(proto.Message), output)
	return nil
}

// managedTestHandler connects the generated HTTP routes to typed query and command clients backed by
// the supplied recorder. It intentionally has no live SSO, network connection, or owner service.
// managedTestHandler соединяет сгенерированные HTTP-маршруты с типизированными query и command
// клиентами заданного recorder. Реальных SSO, сетевого подключения и сервиса владельца намеренно нет.
func managedTestHandler(client *managedRPCRecorder) http.Handler {
	return generated.Handler(&Server{control: &controlplaneclient.Client{
		Query:   controlplanev1.NewPlatformQueryServiceClient(client),
		Command: controlplanev1.NewPlatformCommandServiceClient(client),
	}})
}

// managedTestRequest creates an in-memory JSON request with fixed synthetic idempotency, CSRF, and
// version-three If-Match headers. The fixture headers are test data, not authenticated owner
// credentials.
// managedTestRequest создаёт JSON-запрос в памяти с фиксированными синтетическими заголовками
// идемпотентности, CSRF и If-Match версии3. Заголовки являются тестовыми данными, а не удостоверенными
// реквизитами владельца.
func managedTestRequest(method, path, body string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "managed-fixture-01")
	request.Header.Set("X-CSRF-Token", "fixture-csrf-token")
	request.Header.Set("If-Match", "\"3\"")
	return request
}

// TestManagedConfigurationRoutesCallExactTypedRPC covers twenty selected routes, expected RPC suffixes
// and statuses, mutation and resource pins, project-versus-organization input, unchanged source text,
// pagination, and STT zero-value output. It uses generated typed clients and a recorder, not live
// authorization or a complete proof of every method's owner boundary.
// TestManagedConfigurationRoutesCallExactTypedRPC проверяет20 выбранных маршрутов, ожидаемые суффиксы
// RPC и статусы, привязки изменения и ресурса, проектный и общий вход, сохранность текста, пагинацию и
// нулевые поля STT. Используются сгенерированные клиенты и recorder, а не реальная авторизация или
// полное доказательство каждой границы владельца.
func TestManagedConfigurationRoutesCallExactTypedRPC(t *testing.T) {
	t.Parallel()
	draftBody := "{\"configurationRef\":\"mcfg_fixture01\",\"projectRef\":\"prj_fixture01\",\"name\":\"Название\",\"contentFormat\":\"TEXT\",\"content\":\"TYPE_source\"}"
	organizationDraftBody := "{\"configurationRef\":\"mcfg_fixture01\",\"name\":\"Название\",\"contentFormat\":\"TEXT\",\"content\":\"TYPE_source\"}"
	rebindBody := "{\"impactDigest\":\"" + strings.Repeat("b", 64) + "\",\"consumers\":[{\"kind\":\"AGENT\",\"ref\":\"agt_fixture01\",\"revisionRef\":\"mrev_fixture00\",\"version\":4}]}"
	cases := []struct {
		name, method, path, body, rpc string
		code                          int
	}{
		{"CreatePromptTemplateDraft", http.MethodPost, "/api/v1/prompt-template-configurations/drafts", draftBody, "CreatePromptTemplateDraft", http.StatusCreated},
		{"ValidatePromptTemplateDraft", http.MethodPost, "/api/v1/prompt-template-configurations/mcfg_fixture01/revisions/mrev_fixture01/validation", "", "ValidatePromptTemplateDraft", http.StatusOK},
		{"PublishPromptTemplateDraft", http.MethodPost, "/api/v1/prompt-template-configurations/mcfg_fixture01/revisions/mrev_fixture01/publication", revisionImpactPublishBody, "PublishPromptTemplateDraft", http.StatusOK},
		{"RebindPromptTemplateConsumers", http.MethodPost, "/api/v1/prompt-template-configurations/mcfg_fixture01/revisions/mrev_fixture01/consumer-bindings", rebindBody, "RebindPromptTemplateConsumers", http.StatusOK},
		{"CreateRoleImageRevisionDraft", http.MethodPost, "/api/v1/role-image-configurations/drafts", draftBody, "CreateRoleImageRevisionDraft", http.StatusCreated},
		{"ValidateRoleImageRevisionDraft", http.MethodPost, "/api/v1/role-image-configurations/mcfg_fixture01/revisions/mrev_fixture01/validation", "", "ValidateRoleImageRevisionDraft", http.StatusOK},
		{"PublishRoleImageRevisionDraft", http.MethodPost, "/api/v1/role-image-configurations/mcfg_fixture01/revisions/mrev_fixture01/publication", "", "PublishRoleImageRevisionDraft", http.StatusOK},
		{"CreateIntegrationDefinitionDraft", http.MethodPost, "/api/v1/integration-definition-configurations/drafts", organizationDraftBody, "CreateIntegrationDefinitionDraft", http.StatusCreated},
		{"ValidateIntegrationDefinitionDraft", http.MethodPost, "/api/v1/integration-definition-configurations/mcfg_fixture01/revisions/mrev_fixture01/validation", "", "ValidateIntegrationDefinitionDraft", http.StatusOK},
		{"PublishIntegrationDefinitionDraft", http.MethodPost, "/api/v1/integration-definition-configurations/mcfg_fixture01/revisions/mrev_fixture01/publication", "", "PublishIntegrationDefinitionDraft", http.StatusOK},
		{"RebindIntegrationDefinitionConsumers", http.MethodPost, "/api/v1/integration-definition-configurations/mcfg_fixture01/revisions/mrev_fixture01/consumer-bindings", rebindBody, "RebindIntegrationDefinitionConsumers", http.StatusOK},
		{"CreateSystemSTTConfigurationDraft", http.MethodPost, "/api/v1/system-stt-configurations/drafts", organizationDraftBody, "CreateSystemSTTConfigurationDraft", http.StatusCreated},
		{"ValidateSystemSTTConfigurationDraft", http.MethodPost, "/api/v1/system-stt-configurations/mcfg_fixture01/revisions/mrev_fixture01/validation", "", "ValidateSystemSTTConfigurationDraft", http.StatusOK},
		{"PublishSystemSTTConfigurationDraft", http.MethodPost, "/api/v1/system-stt-configurations/mcfg_fixture01/revisions/mrev_fixture01/publication", "", "PublishSystemSTTConfigurationDraft", http.StatusOK},
		{"RebindSystemSTTConsumers", http.MethodPost, "/api/v1/system-stt-configurations/mcfg_fixture01/revisions/mrev_fixture01/consumer-bindings", rebindBody, "RebindSystemSTTConsumers", http.StatusOK},
		{"history", http.MethodGet, "/api/v1/managed-configurations/mcfg_fixture01/revisions?pageSize=20&pageToken=next-page", "", "ListManagedConfigurationHistory", http.StatusOK},
		{"impact", http.MethodGet, "/api/v1/managed-configurations/mcfg_fixture01/revisions/mrev_fixture01/impact", "", "GetManagedConfigurationImpact", http.StatusOK},
		{"detach", http.MethodPost, "/api/v1/managed-configurations/mcfg_fixture01/detachment", "", "DetachGitManagedConfiguration", http.StatusOK},
		{"copy", http.MethodPost, "/api/v1/managed-configurations/mcfg_fixture01/copies", "{\"name\":\"Копия\"}", "CopyGitManagedConfiguration", http.StatusCreated},
		{"stt", http.MethodGet, "/api/v1/system-stt-configuration", "", "GetSystemSTTConfiguration", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &managedRPCRecorder{}
			response := httptest.NewRecorder()
			managedTestHandler(client).ServeHTTP(response, managedTestRequest(tc.method, tc.path, tc.body))
			if response.Code != tc.code || !strings.HasSuffix(client.method, "/"+tc.rpc) {
				t.Fatalf("route status=%d method=%s body=%s", response.Code, client.method, response.Body.String())
			}
			fields := client.request.ProtoReflect()
			if field := fields.Descriptor().Fields().ByName("mutation"); field != nil {
				mutation := fields.Get(field).Message().Interface().(*controlplanev1.MutationContext)
				if mutation.GetIdempotencyKey() != "managed-fixture-01" || mutation.GetExpectedVersion() != 3 {
					t.Fatal("mutation metadata changed")
				}
			}
			if field := fields.Descriptor().Fields().ByName("configuration_ref"); field != nil && fields.Get(field).String() != "mcfg_fixture01" {
				t.Fatal("configuration reference changed")
			}
			if field := fields.Descriptor().Fields().ByName("revision_ref"); field != nil && fields.Get(field).String() != "mrev_fixture01" {
				t.Fatal("revision reference changed")
			}
			if strings.HasPrefix(tc.rpc, "Create") {
				body := client.request.(interface {
					GetProjectRef() string
					GetContent() string
				})
				wantProject := "prj_fixture01"
				if tc.rpc == "CreateIntegrationDefinitionDraft" || tc.rpc == "CreateSystemSTTConfigurationDraft" {
					wantProject = ""
				}
				if body.GetProjectRef() != wantProject || body.GetContent() != "TYPE_source" {
					t.Fatal("draft input changed")
				}
			}
			if tc.rpc == "ListManagedConfigurationHistory" {
				request := client.request.(*controlplanev1.ListManagedConfigurationHistoryRequest)
				if request.GetPage().GetPageSize() != 20 || request.GetPage().GetPageToken() != "next-page" {
					t.Fatal("history pagination changed")
				}
				var history generated.ManagedConfigurationHistory
				if json.Unmarshal(response.Body.Bytes(), &history) != nil || history.Configuration.Ref != "mcfg_fixture01" || len(history.Items) != 1 || history.NextPageToken == nil || *history.NextPageToken != "next-page-2" {
					t.Fatal("history response changed")
				}
			}
			if tc.rpc == "GetSystemSTTConfiguration" {
				if !strings.Contains(response.Body.String(), "\"ready\":false") || !strings.Contains(response.Body.String(), "\"readinessBlockers\":[]") {
					t.Fatal("STT zero values missing")
				}
			}
		})
	}
}

// TestManagedConfigurationViewPreservesSourceAndRejectsInvalidProducerShape checks unchanged
// source/name, nonnil diagnostics, and ETag output, then rejects unknown state, unsafe revision,
// malformed digest, and missing creation time. It validates producer shape rather than cryptographic
// content provenance.
// TestManagedConfigurationViewPreservesSourceAndRejectsInvalidProducerShape проверяет неизменность
// исходника и имени, ненулевую диагностику и ETag, затем отказ для неизвестного состояния,
// небезопасной ревизии, неверного дайджеста и отсутствующего времени. Проверяется форма производителя,
// а не криптографическое происхождение содержимого.
func TestManagedConfigurationViewPreservesSourceAndRejectsInvalidProducerShape(t *testing.T) {
	t.Parallel()
	configuration, revision := managedFixture()
	recorder := &localizingRecorder{httptest.NewRecorder()}
	writeManagedResult(recorder, http.StatusOK, &controlplanev1.CreatePromptTemplateDraftResponse{Configuration: configuration, Revision: revision})
	var output generated.ManagedConfigurationResult
	if json.Unmarshal(recorder.Body.Bytes(), &output) != nil || output.Revision.Content != managedFixtureContent || output.Configuration.Name != "TYPE_Название" || output.Revision.ValidationDiagnostics == nil || recorder.Header().Get("ETag") != "\"3\"" {
		t.Fatalf("source or response changed: %s", recorder.Body.String())
	}
	for _, change := range []func(*controlplanev1.ManagedConfigurationRevision){
		func(r *controlplanev1.ManagedConfigurationRevision) {
			r.State = controlplanev1.ManagedConfigurationState(999)
		},
		func(r *controlplanev1.ManagedConfigurationRevision) { r.Revision = maximumSafeJSONInteger + 1 },
		func(r *controlplanev1.ManagedConfigurationRevision) { r.Digest = "invalid" },
		func(r *controlplanev1.ManagedConfigurationRevision) { r.CreatedAt = nil },
	} {
		bad := proto.Clone(revision).(*controlplanev1.ManagedConfigurationRevision)
		change(bad)
		if _, err := managedRevisionView(bad); err == nil {
			t.Fatal("invalid producer response accepted")
		}
	}
}

// TestManagedConfigurationRejectsCallerAuthorityAndMissingOCCBeforeRPC supplies three invalid draft
// fixtures with If-Match removed and asserts rejection before any recorded RPC. The combined invalid
// inputs demonstrate early rejection but do not isolate each individual draft guard.
// TestManagedConfigurationRejectsCallerAuthorityAndMissingOCCBeforeRPC передаёт три неверных черновика
// с удалённым If-Match и проверяет отказ до зарегистрированного RPC. Совместные неверные признаки
// доказывают ранний отказ, но не изолируют каждую отдельную проверку черновика.
func TestManagedConfigurationRejectsCallerAuthorityAndMissingOCCBeforeRPC(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		"{\"name\":\"x\",\"contentFormat\":\"TEXT\",\"content\":\"x\",\"managedBy\":\"GIT\"}",
		"{\"name\":\"x\",\"contentFormat\":\"TEXT\",\"content\":\"x\",\"configurationRef\":\"mcfg_fixture01\"}",
		"{\"name\":\"x\",\"contentFormat\":\"UNKNOWN\",\"content\":\"x\"}",
	} {
		client := &managedRPCRecorder{}
		request := managedTestRequest(http.MethodPost, "/api/v1/prompt-template-configurations/drafts", body)
		request.Header.Del("If-Match")
		response := httptest.NewRecorder()
		managedTestHandler(client).ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest || client.method != "" {
			t.Fatal("invalid draft reached RPC")
		}
	}
}

// TestManagedConfigurationCreateRejectsInvalidProjectScopeBeforeRPC checks four scope cases: missing
// project for prompt/image and forbidden project for integration/STT. Every case must return the local
// invalid-request response without reaching the RPC recorder.
// TestManagedConfigurationCreateRejectsInvalidProjectScopeBeforeRPC проверяет четыре случая:
// отсутствие проекта у промпта и образа и запрещённый проект у интеграции и STT. Каждый случай даёт
// локальный отказ неверного запроса без обращения к recorder RPC.
func TestManagedConfigurationCreateRejectsInvalidProjectScopeBeforeRPC(t *testing.T) {
	t.Parallel()
	cases := []struct {
		path, body string
	}{
		{"/api/v1/prompt-template-configurations/drafts", `{"name":"x","contentFormat":"TEXT","content":"x"}`},
		{"/api/v1/role-image-configurations/drafts", `{"name":"x","contentFormat":"JSON","content":"{}"}`},
		{"/api/v1/integration-definition-configurations/drafts", `{"projectRef":"prj_fixture01","name":"x","contentFormat":"YAML","content":"kind: IntegrationDefinition"}`},
		{"/api/v1/system-stt-configurations/drafts", `{"projectRef":"prj_fixture01","name":"x","contentFormat":"JSON","content":"{}"}`},
	}
	for _, tc := range cases {
		client := &managedRPCRecorder{}
		response := httptest.NewRecorder()
		managedTestHandler(client).ServeHTTP(response, managedTestRequest(http.MethodPost, tc.path, tc.body))
		if response.Code != http.StatusBadRequest || client.method != "" || !strings.Contains(response.Body.String(), `"code":"INVALID_REQUEST"`) {
			t.Fatalf("project scope was not rejected locally: path=%s status=%d method=%s body=%s", tc.path, response.Code, client.method, response.Body.String())
		}
	}
}

// TestManagedConfigurationPropagatesOwnerDenial verifies that a synthetic PermissionDenied from the
// configuration-history owner path becomes HTTP403. It is error-mapping evidence, not a real SSO or
// owner eligibility test.
// TestManagedConfigurationPropagatesOwnerDenial проверяет преобразование синтетического
// PermissionDenied пути истории конфигурации в HTTP403. Это доказательство преобразования ошибки, а не
// реальная проверка SSO или допустимости владельца.
func TestManagedConfigurationPropagatesOwnerDenial(t *testing.T) {
	t.Parallel()
	client := &managedRPCRecorder{failure: status.Error(codes.PermissionDenied, "denied")}
	response := httptest.NewRecorder()
	managedTestHandler(client).ServeHTTP(response, managedTestRequest(http.MethodGet, "/api/v1/managed-configurations/mcfg_fixture01/revisions", ""))
	if response.Code != http.StatusForbidden {
		t.Fatalf("owner denial status=%d", response.Code)
	}
}

// TestManagedConfigurationRejectsDuplicateConsumerBindings calls the shared consumer validator with
// two identical kind/reference selections and requires rejection. It directly tests duplicate
// detection, not dispatch through an HTTP route or a live owner.
// TestManagedConfigurationRejectsDuplicateConsumerBindings вызывает общий validator потребителей с
// двумя одинаковыми парами вид/ссылка и требует отказ. Проверяется непосредственно обнаружение
// повторов, а не HTTP-маршрут или реальный владелец.
func TestManagedConfigurationRejectsDuplicateConsumerBindings(t *testing.T) {
	t.Parallel()
	var consumer generated.ManagedConfigurationConsumerInput
	if err := consumer.FromManagedConfigurationConsumerMatch(generated.ManagedConfigurationConsumerMatch{Kind: "AGENT", Ref: "agt_fixture01", RevisionRef: "mrev_fixture00", Version: 2}); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	if _, ok := managedConsumerInput(recorder, generated.ManagedConfigurationRebindInput{ImpactDigest: strings.Repeat("a", 64), Consumers: []generated.ManagedConfigurationConsumerInput{consumer, consumer}}); ok {
		t.Fatal("duplicate consumer accepted")
	}
}
