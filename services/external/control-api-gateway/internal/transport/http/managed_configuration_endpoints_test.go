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

const managedFixtureContent = "TYPE_остается исходным текстом\ni18n:это не ключ перевода"

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

type managedRPCRecorder struct {
	grpc.ClientConnInterface
	method        string
	request       proto.Message
	failure       error
	revisionState controlplanev1.ManagedConfigurationState
}

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

func managedTestHandler(client *managedRPCRecorder) http.Handler {
	return generated.Handler(&Server{control: &controlplaneclient.Client{
		Query:   controlplanev1.NewPlatformQueryServiceClient(client),
		Command: controlplanev1.NewPlatformCommandServiceClient(client),
	}})
}

func managedTestRequest(method, path, body string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "managed-fixture-01")
	request.Header.Set("X-CSRF-Token", "fixture-csrf-token")
	request.Header.Set("If-Match", "\"3\"")
	return request
}

func TestManagedConfigurationRoutesCallExactTypedRPC(t *testing.T) {
	t.Parallel()
	draftBody := "{\"configurationRef\":\"mcfg_fixture01\",\"projectRef\":\"prj_fixture01\",\"name\":\"Название\",\"contentFormat\":\"TEXT\",\"content\":\"TYPE_source\"}"
	rebindBody := "{\"impactDigest\":\"" + strings.Repeat("b", 64) + "\",\"consumers\":[{\"kind\":\"AGENT\",\"ref\":\"agt_fixture01\",\"revisionRef\":\"mrev_fixture00\",\"version\":4}]}"
	cases := []struct {
		name, method, path, body, rpc string
		code                          int
	}{
		{"CreatePromptTemplateDraft", http.MethodPost, "/api/v1/prompt-template-configurations/drafts", draftBody, "CreatePromptTemplateDraft", http.StatusCreated},
		{"ValidatePromptTemplateDraft", http.MethodPost, "/api/v1/prompt-template-configurations/mcfg_fixture01/revisions/mrev_fixture01/validation", "", "ValidatePromptTemplateDraft", http.StatusOK},
		{"PublishPromptTemplateDraft", http.MethodPost, "/api/v1/prompt-template-configurations/mcfg_fixture01/revisions/mrev_fixture01/publication", "", "PublishPromptTemplateDraft", http.StatusOK},
		{"RebindPromptTemplateConsumers", http.MethodPost, "/api/v1/prompt-template-configurations/mcfg_fixture01/revisions/mrev_fixture01/consumer-bindings", rebindBody, "RebindPromptTemplateConsumers", http.StatusOK},
		{"CreateRoleImageRevisionDraft", http.MethodPost, "/api/v1/role-image-configurations/drafts", draftBody, "CreateRoleImageRevisionDraft", http.StatusCreated},
		{"ValidateRoleImageRevisionDraft", http.MethodPost, "/api/v1/role-image-configurations/mcfg_fixture01/revisions/mrev_fixture01/validation", "", "ValidateRoleImageRevisionDraft", http.StatusOK},
		{"PublishRoleImageRevisionDraft", http.MethodPost, "/api/v1/role-image-configurations/mcfg_fixture01/revisions/mrev_fixture01/publication", "", "PublishRoleImageRevisionDraft", http.StatusOK},
		{"RebindRoleImageConsumers", http.MethodPost, "/api/v1/role-image-configurations/mcfg_fixture01/revisions/mrev_fixture01/consumer-bindings", rebindBody, "RebindRoleImageConsumers", http.StatusOK},
		{"CreateIntegrationDefinitionDraft", http.MethodPost, "/api/v1/integration-definition-configurations/drafts", draftBody, "CreateIntegrationDefinitionDraft", http.StatusCreated},
		{"ValidateIntegrationDefinitionDraft", http.MethodPost, "/api/v1/integration-definition-configurations/mcfg_fixture01/revisions/mrev_fixture01/validation", "", "ValidateIntegrationDefinitionDraft", http.StatusOK},
		{"PublishIntegrationDefinitionDraft", http.MethodPost, "/api/v1/integration-definition-configurations/mcfg_fixture01/revisions/mrev_fixture01/publication", "", "PublishIntegrationDefinitionDraft", http.StatusOK},
		{"RebindIntegrationDefinitionConsumers", http.MethodPost, "/api/v1/integration-definition-configurations/mcfg_fixture01/revisions/mrev_fixture01/consumer-bindings", rebindBody, "RebindIntegrationDefinitionConsumers", http.StatusOK},
		{"CreateSystemSTTConfigurationDraft", http.MethodPost, "/api/v1/system-stt-configurations/drafts", draftBody, "CreateSystemSTTConfigurationDraft", http.StatusCreated},
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
				if body.GetProjectRef() != "prj_fixture01" || body.GetContent() != "TYPE_source" {
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

func TestManagedConfigurationPropagatesOwnerDenial(t *testing.T) {
	t.Parallel()
	client := &managedRPCRecorder{failure: status.Error(codes.PermissionDenied, "denied")}
	response := httptest.NewRecorder()
	managedTestHandler(client).ServeHTTP(response, managedTestRequest(http.MethodGet, "/api/v1/managed-configurations/mcfg_fixture01/revisions", ""))
	if response.Code != http.StatusForbidden {
		t.Fatalf("owner denial status=%d", response.Code)
	}
}

func TestManagedConfigurationRejectsDuplicateConsumerBindings(t *testing.T) {
	t.Parallel()
	consumer := generated.ManagedConfigurationConsumer{Kind: "AGENT", Ref: "agt_fixture01", RevisionRef: "mrev_fixture00", Version: 2}
	recorder := httptest.NewRecorder()
	if _, ok := managedConsumerInput(recorder, generated.ManagedConfigurationRebindInput{ImpactDigest: strings.Repeat("a", 64), Consumers: []generated.ManagedConfigurationConsumer{consumer, consumer}}); ok {
		t.Fatal("duplicate consumer accepted")
	}
}
