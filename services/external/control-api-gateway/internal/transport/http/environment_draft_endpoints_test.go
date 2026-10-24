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
	"google.golang.org/protobuf/reflect/protoreflect"
)

// draftSpecBody contains synthetic draft text deliberately resembling enum/localization markers,
// empty collections and one plain environment value to verify exact content preservation rather
// than accidental normalization.
// draftSpecBody содержит синтетический draft текст, намеренно похожий на enum/localization
// маркеры, пустые коллекции и одно обычное environment значение для проверки точного сохранения
// без случайной нормализации.
const draftSpecBody = `{"name":"TYPE_Черновик","description":"i18n:исходный текст","imageArtifactRef":"","tools":[],"values":[{"name":"TEST","value":"TYPE_не преобразовывать"}],"secretBindings":[]}`

// environmentDraftRecorder captures cloned typed RPC requests and builds controlled
// project/organization draft responses, with independent draft/environment mutation hooks for
// negative publication tests. It persists no environment state.
// environmentDraftRecorder сохраняет копии typed RPC requests и собирает контролируемые
// project/organization draft ответы с отдельными draft/environment mutation hooks для
// отрицательных publication тестов. Состояние среды не сохраняется.
type environmentDraftRecorder struct {
	grpc.ClientConnInterface
	method            string
	request           proto.Message
	failure           error
	empty             bool
	bindings          []*controlplanev1.RuntimeSecretBinding
	mutate            func(*controlplanev1.RuntimeEnvironmentDraft)
	organization      bool
	mutateEnvironment func(*controlplanev1.RuntimeEnvironmentSet)
}

// Invoke records method/input, applies configured failure/empty behavior and fills the generated
// response with synthetic draft plus publication plan/environment when needed. Mutation hooks
// corrupt only the requested test projection.
// Invoke сохраняет method/input, применяет заданные failure/empty и заполняет generated ответ
// синтетическим draft и при необходимости publication plan/environment. Mutation hooks повреждают
// только нужную тестовую проекцию.
func (client *environmentDraftRecorder) Invoke(_ context.Context, method string, request, response any, _ ...grpc.CallOption) error {
	client.method, client.request = method, proto.Clone(request.(proto.Message))
	if client.failure != nil {
		return client.failure
	}
	if client.empty {
		return nil
	}
	draft := &controlplanev1.RuntimeEnvironmentDraft{ScopeKind: controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT, OrganizationRef: "org_fixture01", Ref: "renvd_fixture01", ProjectRef: "prj_fixture01", Version: 4, State: "DRAFT",
		Specification: &controlplanev1.RuntimeEnvironmentDraftSpecification{Name: "TYPE_Черновик", Description: "i18n:исходный текст",
			Values:         []*controlplanev1.RuntimeEnvironmentValue{{Name: "TEST", Value: "TYPE_не преобразовывать"}},
			SecretBindings: client.bindings,
		}}
	if client.organization {
		draft.ScopeKind = controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION
		draft.ProjectRef = ""
	}
	if strings.HasSuffix(method, "/ValidateRuntimeEnvironmentDraft") {
		draft.State, draft.Diagnostics = "INVALID", []string{"ENVIRONMENT_VALIDATION_FAILED"}
	}
	if strings.HasSuffix(method, "/DiscardRuntimeEnvironmentDraft") {
		draft.State = "DISCARDED"
	}
	target := response.(proto.Message).ProtoReflect()
	if create, ok := request.(*controlplanev1.CreateRuntimeEnvironmentDraftRequest); ok {
		draft.EnvironmentRef, draft.ExpectedEnvironmentVersion = create.GetEnvironmentRef(), create.GetExpectedEnvironmentVersion()
	}
	if create, ok := request.(*controlplanev1.CreateOrganizationRuntimeEnvironmentDraftRequest); ok {
		draft.EnvironmentRef, draft.ExpectedEnvironmentVersion = create.GetEnvironmentRef(), create.GetExpectedEnvironmentVersion()
	}
	if strings.HasSuffix(method, "/PublishRuntimeEnvironmentDraft") {
		draft.State, draft.PublishedEnvironmentRef = "PUBLISHED", "renv_fixture01"
		draft.ValidationDigest = strings.Repeat("a", 64)
		plan := revisionImpactFixture()
		plan.Version, plan.State, plan.PublishedRevisionRef = 2, controlplanev1.RevisionImpactState_REVISION_IMPACT_STATE_APPLIED, "renvv_published01"
		target.Set(target.Descriptor().Fields().ByName("plan"), protoreflect.ValueOfMessage(plan.ProtoReflect()))
		environment := &controlplanev1.RuntimeEnvironmentSet{ScopeKind: draft.ScopeKind, OrganizationRef: draft.OrganizationRef, Ref: draft.PublishedEnvironmentRef, ProjectRef: draft.ProjectRef, Version: 2, CurrentVersion: &controlplanev1.RuntimeEnvironmentVersion{Ref: plan.PublishedRevisionRef, Version: 1, Revision: 1, Digest: plan.TargetDigest}}
		if client.mutateEnvironment != nil {
			client.mutateEnvironment(environment)
		}
		target.Set(target.Descriptor().Fields().ByName("environment"), protoreflect.ValueOfMessage(environment.ProtoReflect()))
	}
	if client.mutate != nil {
		client.mutate(draft)
	}
	target.Set(target.Descriptor().Fields().ByName("draft"), protoreflect.ValueOfMessage(draft.ProtoReflect()))
	return nil
}

// TestEnvironmentSecretRevisionSurvivesHTTPRoundTrip round-trips zero, explicit and maximum-safe
// secret revision pins through draft reads and save/create routes, proving edits to unrelated
// fields do not replace the pinned revision.
// TestEnvironmentSecretRevisionSurvivesHTTPRoundTrip переносит нулевой, явный и максимально
// безопасный secret revision pin через чтение draft и save/create маршруты, доказывая сохранение
// pin при изменении других полей.
func TestEnvironmentSecretRevisionSurvivesHTTPRoundTrip(t *testing.T) {
	for _, revision := range []int64{0, 7, maximumSafeJSONInteger} {
		client := &environmentDraftRecorder{bindings: []*controlplanev1.RuntimeSecretBinding{{Name: "API_TOKEN", SecretRef: "sec_fixture01", Revision: revision}}}
		read := httptest.NewRecorder()
		draftTestHandler(client).ServeHTTP(read, managedTestRequest("GET", "/api/v1/runtime-environment-drafts/renvd_fixture01", ""))
		var draft generated.RuntimeEnvironmentDraft
		if read.Code != 200 || json.Unmarshal(read.Body.Bytes(), &draft) != nil || len(draft.Specification.SecretBindings) != 1 || draft.Specification.SecretBindings[0].Revision == nil || *draft.Specification.SecretBindings[0].Revision != revision {
			t.Fatalf("draft lost exact Secret pin: %d", read.Code)
		}
		body, err := json.Marshal(draft.Specification)
		if err != nil {
			t.Fatal(err)
		}
		for _, create := range []bool{false, true} {
			path, method, content := "/api/v1/runtime-environment-drafts/renvd_fixture01", "PUT", string(body)
			if create {
				path, method, content = "/api/v1/projects/prj_fixture01/runtime-environment-drafts", "POST", `{"specification":`+content+`}`
			}
			write := httptest.NewRecorder()
			draftTestHandler(client).ServeHTTP(write, managedTestRequest(method, path, content))
			request, ok := client.request.(interface {
				GetSpecification() *controlplanev1.RuntimeEnvironmentDraftSpecification
			})
			if write.Code != 200 && write.Code != 201 || !ok || request.GetSpecification().GetSecretBindings()[0].GetRevision() != revision {
				t.Fatal("saving another field replaced exact Secret revision")
			}
		}
	}
}

// TestEnvironmentSecretRevisionRejectsInvalidNumbersBeforeRPC rejects negative, unsafe and
// fractional input revisions on draft/environment create/save/version routes before RPC, and
// requires 502 for corrupt owner revision metadata.
// TestEnvironmentSecretRevisionRejectsInvalidNumbersBeforeRPC отклоняет отрицательные,
// небезопасные и дробные revisions до RPC на draft/environment create/save/version маршрутах и
// требует 502 для неверных metadata revision владельца.
func TestEnvironmentSecretRevisionRejectsInvalidNumbersBeforeRPC(t *testing.T) {
	for _, revision := range []string{"-1", "9007199254740992", "1.5"} {
		spec := strings.Replace(draftSpecBody, `"secretBindings":[]`, `"secretBindings":[{"name":"API_TOKEN","secretRef":"sec_fixture01","revision":`+revision+`}]`, 1)
		for _, route := range []struct{ method, path, body string }{
			{"PUT", "/api/v1/runtime-environment-drafts/renvd_fixture01", spec},
			{"POST", "/api/v1/projects/prj_fixture01/runtime-environment-drafts", `{"specification":` + spec + `}`},
			{"POST", "/api/v1/projects/prj_fixture01/runtime-environments", spec},
			{"POST", "/api/v1/runtime-environments/renv_fixture01/versions", spec},
		} {
			client := &environmentDraftRecorder{}
			w := httptest.NewRecorder()
			draftTestHandler(client).ServeHTTP(w, managedTestRequest(route.method, route.path, route.body))
			if w.Code != 400 || client.request != nil {
				t.Fatalf("bad pin reached owner: %s %s status=%d", route.method, route.path, w.Code)
			}
		}
	}
	for _, revision := range []int64{-1, maximumSafeJSONInteger + 1} {
		client := &environmentDraftRecorder{bindings: []*controlplanev1.RuntimeSecretBinding{{Name: "API_TOKEN", SecretRef: "sec_fixture01", Revision: revision}}}
		w := httptest.NewRecorder()
		draftTestHandler(client).ServeHTTP(w, managedTestRequest("GET", "/api/v1/runtime-environment-drafts/renvd_fixture01", ""))
		if w.Code != 502 {
			t.Fatal("corrupt owner revision returned")
		}
	}
}

// TestEnvironmentPublishedSecretDescriptorPreservesPin checks positive safe published secret
// revisions survive projection while zero/negative/unsafe pins fail, and verifies explicit false
// readiness with private namespace metadata omitted.
// TestEnvironmentPublishedSecretDescriptorPreservesPin проверяет сохранение положительных
// безопасных published secret revisions, отказ для zero/negative/unsafe pins и явную false
// готовность без приватных namespace metadata.
func TestEnvironmentPublishedSecretDescriptorPreservesPin(t *testing.T) {
	for _, revision := range []int64{0, -1, 7, maximumSafeJSONInteger, maximumSafeJSONInteger + 1} {
		client := &catalogRPCRecorder{response: &controlplanev1.GetRuntimeEnvironmentSetResponse{Environment: &controlplanev1.RuntimeEnvironmentSet{
			ScopeKind: controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT, OrganizationRef: "org_fixture01", ProjectRef: "prj_fixture01", Ref: "renv_fixture01", Version: 3, CurrentVersion: &controlplanev1.RuntimeEnvironmentVersion{Ref: "renvv_fixture01", SecretDescriptors: []*controlplanev1.RuntimeSecretDescriptor{{
				Name: "API_TOKEN", SecretRef: "sec_fixture01", Revision: revision, Namespace: "internal-only-namespace", SecretName: "secret-fixture", SecretKey: "value", SecretUid: "uid-fixture", SecretResourceVersion: "9", ContentSha256: strings.Repeat("a", 64),
			}}},
		}}}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, managedTestRequest("GET", "/api/v1/runtime-environments/renv_fixture01", ""))
		if revision < 1 || revision > maximumSafeJSONInteger {
			if w.Code != 502 {
				t.Fatalf("invalid descriptor revision accepted: %d", w.Code)
			}
			continue
		}
		var result generated.RuntimeEnvironmentSet
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.CurrentVersion.SecretDescriptors) != 1 || result.CurrentVersion.SecretDescriptors[0].Revision != revision {
			t.Fatalf("published Secret pin lost: %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), `"ready":false`) {
			t.Fatal("false published environment readiness was omitted")
		}
		if strings.Contains(w.Body.String(), "internal-only-namespace") || strings.Contains(w.Body.String(), `"namespace"`) {
			t.Fatal("private namespace leaked")
		}
	}
}

// draftTestHandler wires the generated HTTP router to typed query/command clients backed by the
// synthetic draft recorder, without an external RPC connection.
// draftTestHandler связывает generated HTTP router с typed query/command клиентами синтетического
// draft recorder без внешнего RPC соединения.
func draftTestHandler(client *environmentDraftRecorder) http.Handler {
	return generated.Handler(&Server{control: &controlplaneclient.Client{
		Query: controlplanev1.NewPlatformQueryServiceClient(client), Command: controlplanev1.NewPlatformCommandServiceClient(client),
	}})
}

// TestEnvironmentDraftRoutesKeepSeparateTypedLifecycle checks
// create/read/save/validate/publish/discard routes use their dedicated RPCs, retain
// mutation/version/plan selectors and exact user text, and return typed arrays, draft ETag and
// no-store consistently.
// TestEnvironmentDraftRoutesKeepSeparateTypedLifecycle проверяет отдельные RPC
// create/read/save/validate/publish/discard, сохранение mutation/version/plan selectors и точного
// пользовательского текста и согласованные typed массивы, draft ETag и no-store.
func TestEnvironmentDraftRoutesKeepSeparateTypedLifecycle(t *testing.T) {
	for _, test := range []struct {
		method, path, body, rpc string
		code                    int
	}{
		{http.MethodPost, "/api/v1/projects/prj_fixture01/runtime-environment-drafts", `{"specification":` + draftSpecBody + `}`, "CreateRuntimeEnvironmentDraft", http.StatusCreated},
		{http.MethodGet, "/api/v1/runtime-environment-drafts/renvd_fixture01", "", "GetRuntimeEnvironmentDraft", http.StatusOK},
		{http.MethodPut, "/api/v1/runtime-environment-drafts/renvd_fixture01", draftSpecBody, "SaveRuntimeEnvironmentDraft", http.StatusOK},
		{http.MethodPost, "/api/v1/runtime-environment-drafts/renvd_fixture01/validation", "", "ValidateRuntimeEnvironmentDraft", http.StatusOK},
		{http.MethodPost, "/api/v1/runtime-environment-drafts/renvd_fixture01/publication", revisionImpactPublishBody, "PublishRuntimeEnvironmentDraft", http.StatusOK},
		{http.MethodDelete, "/api/v1/runtime-environment-drafts/renvd_fixture01", "", "DiscardRuntimeEnvironmentDraft", http.StatusOK},
	} {
		t.Run(test.rpc, func(t *testing.T) {
			client := &environmentDraftRecorder{}
			response := httptest.NewRecorder()
			draftTestHandler(client).ServeHTTP(response, managedTestRequest(test.method, test.path, test.body))
			if response.Code != test.code || !strings.HasSuffix(client.method, "/"+test.rpc) {
				t.Fatalf("incorrect lifecycle: status=%d rpc=%s body=%s", response.Code, client.method, response.Body.String())
			}
			if mutation, ok := client.request.(interface {
				GetMutation() *controlplanev1.MutationContext
			}); ok {
				expected := int64(3)
				if test.rpc == "CreateRuntimeEnvironmentDraft" {
					expected = 0
				}
				if mutation.GetMutation().GetExpectedVersion() != expected || mutation.GetMutation().GetIdempotencyKey() != "managed-fixture-01" {
					t.Fatal("draft mutation metadata changed")
				}
			}
			if create, ok := client.request.(*controlplanev1.CreateRuntimeEnvironmentDraftRequest); ok && create.GetProjectRef() != "prj_fixture01" {
				t.Fatal("draft project changed")
			}
			if publish, ok := client.request.(*controlplanev1.PublishRuntimeEnvironmentDraftRequest); ok && (publish.GetPlanRef() != "rip_fixture01" || len(publish.GetSelectedItemRefs()) != 0) {
				t.Fatal("publication changed the selected immutable plan")
			}
			if spec, ok := client.request.(interface {
				GetSpecification() *controlplanev1.RuntimeEnvironmentDraftSpecification
			}); ok &&
				(spec.GetSpecification().GetName() != "TYPE_Черновик" || spec.GetSpecification().GetValues()[0].GetValue() != "TYPE_не преобразовывать") {
				t.Fatal("draft specification changed before RPC")
			}
			var result generated.RuntimeEnvironmentDraft
			raw := response.Body.Bytes()
			if test.rpc == "PublishRuntimeEnvironmentDraft" {
				var envelope struct {
					Draft json.RawMessage `json:"draft"`
				}
				if json.Unmarshal(raw, &envelope) != nil {
					t.Fatal("invalid publication envelope")
				}
				raw = envelope.Draft
			}
			if json.Unmarshal(raw, &result) != nil || result.Version != 4 || result.Specification.Name != "TYPE_Черновик" ||
				result.Specification.Description != "i18n:исходный текст" || result.Specification.Values[0].Value != "TYPE_не преобразовывать" ||
				result.Specification.Tools == nil || result.Specification.SecretBindings == nil || result.Diagnostics == nil ||
				response.Header().Get("ETag") != `"4"` || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("draft output changed source or shape: %s", response.Body.String())
			}
		})
	}
}

// TestEnvironmentDraftPinsExistingEnvironmentVersion requires existing environment ref and
// expected version to travel together to create, and rejects missing or zero counterpart fields
// before owner RPC.
// TestEnvironmentDraftPinsExistingEnvironmentVersion требует совместной передачи existing
// environment ref и expected version в create и отклоняет отсутствие либо нулевое парное поле до
// RPC владельца.
func TestEnvironmentDraftPinsExistingEnvironmentVersion(t *testing.T) {
	client := &environmentDraftRecorder{}
	response := httptest.NewRecorder()
	draftTestHandler(client).ServeHTTP(response, managedTestRequest(http.MethodPost, "/api/v1/projects/prj_fixture01/runtime-environment-drafts",
		`{"environmentRef":"renv_fixture01","expectedEnvironmentVersion":7,"specification":`+draftSpecBody+`}`))
	input, ok := client.request.(*controlplanev1.CreateRuntimeEnvironmentDraftRequest)
	if !ok || input.GetExpectedEnvironmentVersion() != 7 || input.GetEnvironmentRef() != "renv_fixture01" {
		t.Fatalf("environment version was not pinned: %s", response.Body.String())
	}
	for _, prefix := range []string{`"environmentRef":"renv_fixture01",`, `"expectedEnvironmentVersion":7,`, `"environmentRef":"renv_fixture01","expectedEnvironmentVersion":0,`} {
		client := &environmentDraftRecorder{}
		response := httptest.NewRecorder()
		draftTestHandler(client).ServeHTTP(response, managedTestRequest(http.MethodPost, "/api/v1/projects/prj_fixture01/runtime-environment-drafts", `{`+prefix+`"specification":`+draftSpecBody+`}`))
		if response.Code != http.StatusBadRequest || client.request != nil {
			t.Fatal("unversioned environment draft reached RPC")
		}
	}
}

// TestEnvironmentDraftRejectsForeignReceipt rejects a foreign draft identity across all
// existing-draft lifecycle routes and mismatched project/environment/version pins in create
// responses, without exposing the foreign receipt.
// TestEnvironmentDraftRejectsForeignReceipt отклоняет чужую draft identity на всех lifecycle
// маршрутах существующего черновика и несовпадающие project/environment/version pins create ответа
// без раскрытия чужого receipt.
func TestEnvironmentDraftRejectsForeignReceipt(t *testing.T) {
	for _, route := range []struct{ method, path, body string }{
		{"GET", "/api/v1/runtime-environment-drafts/renvd_fixture01", ""},
		{"PUT", "/api/v1/runtime-environment-drafts/renvd_fixture01", draftSpecBody},
		{"POST", "/api/v1/runtime-environment-drafts/renvd_fixture01/validation", ""},
		{"POST", "/api/v1/runtime-environment-drafts/renvd_fixture01/publication", revisionImpactPublishBody},
		{"DELETE", "/api/v1/runtime-environment-drafts/renvd_fixture01", ""},
	} {
		client := &environmentDraftRecorder{mutate: func(d *controlplanev1.RuntimeEnvironmentDraft) { d.Ref = "renvd_other01" }}
		w := httptest.NewRecorder()
		draftTestHandler(client).ServeHTTP(w, managedTestRequest(route.method, route.path, route.body))
		if w.Code != 502 || strings.Contains(w.Body.String(), "renvd_other01") {
			t.Fatalf("foreign receipt accepted: %s %s status=%d", route.method, route.path, w.Code)
		}
	}
	for _, mutate := range []func(*controlplanev1.RuntimeEnvironmentDraft){
		func(d *controlplanev1.RuntimeEnvironmentDraft) { d.ProjectRef = "prj_other01" },
		func(d *controlplanev1.RuntimeEnvironmentDraft) { d.EnvironmentRef = "renv_other01" },
		func(d *controlplanev1.RuntimeEnvironmentDraft) { d.ExpectedEnvironmentVersion = 8 },
	} {
		client := &environmentDraftRecorder{mutate: mutate}
		w := httptest.NewRecorder()
		draftTestHandler(client).ServeHTTP(w, managedTestRequest("POST", "/api/v1/projects/prj_fixture01/runtime-environment-drafts", `{"environmentRef":"renv_fixture01","expectedEnvironmentVersion":7,"specification":`+draftSpecBody+`}`))
		if w.Code != 502 {
			t.Fatal("create receipt lost project/environment/version fence")
		}
	}
}

// TestEnvironmentDraftPreservesAuthorityFailures checks permission, not-found and stale-version
// owner failures preserve their HTTP status and an empty successful RPC response becomes
// invalid-upstream 502.
// TestEnvironmentDraftPreservesAuthorityFailures проверяет сохранение HTTP статусов permission,
// not-found и stale-version отказов владельца и преобразование пустого успешного RPC ответа в
// invalid-upstream 502.
func TestEnvironmentDraftPreservesAuthorityFailures(t *testing.T) {
	for _, test := range []struct {
		failure error
		empty   bool
		code    int
	}{
		{status.Error(codes.PermissionDenied, "denied"), false, http.StatusForbidden},
		{status.Error(codes.NotFound, "missing"), false, http.StatusNotFound},
		{status.Error(codes.Aborted, "stale"), false, http.StatusPreconditionFailed},
		{nil, true, http.StatusBadGateway},
	} {
		response := httptest.NewRecorder()
		draftTestHandler(&environmentDraftRecorder{failure: test.failure, empty: test.empty}).ServeHTTP(response,
			managedTestRequest(http.MethodGet, "/api/v1/runtime-environment-drafts/renvd_fixture01", ""))
		if response.Code != test.code {
			t.Fatalf("authority result changed: %d", response.Code)
		}
	}
}

// TestEnvironmentDraftPolicyKeepsTypedResourceAndNetworkSettings round-trips exact
// resource/ephemeral-volume/network policy through typed input/output, rejects unknown
// destinations and keeps an owner-zero unset policy absent rather than inventing a published
// policy.
// TestEnvironmentDraftPolicyKeepsTypedResourceAndNetworkSettings переносит точные
// resource/ephemeral-volume/network policy через typed input/output, отклоняет неизвестные
// destinations и сохраняет нулевую unset policy владельца отсутствующей без выдуманной published
// policy.
func TestEnvironmentDraftPolicyKeepsTypedResourceAndNetworkSettings(t *testing.T) {
	input := &controlplanev1.RuntimeEnvironmentPolicyInput{
		Resources: &controlplanev1.RuntimeResourcePolicy{CpuRequestMilli: 1000, CpuLimitMilli: 2000, MemoryRequestMib: 1024, MemoryLimitMib: 4096,
			EphemeralStorageRequestMib: 1024, EphemeralStorageLimitMib: 2048},
		Volumes: []*controlplanev1.RuntimeVolumeInput{{Name: "scratch", Kind: controlplanev1.RuntimeVolumeKind_RUNTIME_VOLUME_KIND_EPHEMERAL_DISK, SizeMib: 256}},
		NetworkDestinations: []controlplanev1.RuntimeNetworkDestination{
			controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_DNS,
			controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_RUNTIME_CALLBACK,
			controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_PROVIDER_PROXY,
		}, KubernetesAccess: controlplanev1.RuntimeKubernetesAccessKind_RUNTIME_KUBERNETES_ACCESS_KIND_NONE,
		WebAccess: &controlplanev1.RuntimeWebAccess{Mode: controlplanev1.RuntimeWebAccessMode_RUNTIME_WEB_ACCESS_MODE_NONE},
	}
	view, ok := environmentDraftPolicyView(input)
	if !ok || view == nil {
		t.Fatal("valid draft policy was rejected")
	}
	roundTrip, roundTripOK := runtimeEnvironmentPolicyInput(*view)
	if view.Volumes[0].Kind != "EPHEMERAL_DISK" || view.KubernetesAccess != "NONE" || view.NetworkDestinations[0] != "DNS" ||
		!roundTripOK || !proto.Equal(input, roundTrip) {
		t.Fatal("draft policy was not preserved by typed round trip")
	}
	input.NetworkDestinations[0] = controlplanev1.RuntimeNetworkDestination(999)
	if _, ok := environmentDraftPolicyView(input); ok {
		t.Fatal("unknown network destination accepted")
	}
	if view, ok := environmentDraftPolicyView(&controlplanev1.RuntimeEnvironmentPolicyInput{Resources: &controlplanev1.RuntimeResourcePolicy{}, KubernetesAccess: controlplanev1.RuntimeKubernetesAccessKind_RUNTIME_KUBERNETES_ACCESS_KIND_NONE}); !ok || view != nil {
		t.Fatal("unset draft policy became a published policy")
	}
}

// TestEnvironmentDraftRejectsUnknownPolicyEnumBeforeRPC rejects protobuf-prefixed strings supplied
// where public Kubernetes/network enum names are required, proving malformed policy input never
// reaches the owner.
// TestEnvironmentDraftRejectsUnknownPolicyEnumBeforeRPC отклоняет protobuf-prefixed строки вместо
// публичных Kubernetes/network enum имён, доказывая отсутствие запроса владельцу при неверном
// policy входе.
func TestEnvironmentDraftRejectsUnknownPolicyEnumBeforeRPC(t *testing.T) {
	for _, body := range []string{
		`{"name":"draft","description":"","imageArtifactRef":"","tools":[],"values":[],"secretBindings":[],"policy":{"resources":{"cpuRequestMilli":1000,"cpuLimitMilli":2000,"memoryRequestMib":1024,"memoryLimitMib":4096,"ephemeralStorageRequestMib":1024,"ephemeralStorageLimitMib":2048},"volumes":[],"networkDestinations":["DNS","RUNTIME_CALLBACK","PROVIDER_PROXY"],"kubernetesAccess":"RUNTIME_KUBERNETES_ACCESS_KIND_NONE"}}`,
		`{"name":"draft","description":"","imageArtifactRef":"","tools":[],"values":[],"secretBindings":[],"policy":{"resources":{"cpuRequestMilli":1000,"cpuLimitMilli":2000,"memoryRequestMib":1024,"memoryLimitMib":4096,"ephemeralStorageRequestMib":1024,"ephemeralStorageLimitMib":2048},"volumes":[],"networkDestinations":["RUNTIME_NETWORK_DESTINATION_DNS","RUNTIME_CALLBACK","PROVIDER_PROXY"],"kubernetesAccess":"NONE"}}`,
	} {
		client := &environmentDraftRecorder{}
		response := httptest.NewRecorder()
		draftTestHandler(client).ServeHTTP(response, managedTestRequest(http.MethodPost, "/api/v1/projects/prj_fixture01/runtime-environment-drafts", `{"specification":`+body+`}`))
		if response.Code != http.StatusBadRequest || client.request != nil {
			t.Fatalf("unknown policy enum reached RPC: status=%d", response.Code)
		}
	}
}

// TestRuntimeEnvironmentReadbackUsesOpenAPIEnums checks nested Kubernetes/volume/network/web
// policy enum values normalize to public OpenAPI names and no protobuf enum prefixes leak into
// JSON.
// TestRuntimeEnvironmentReadbackUsesOpenAPIEnums проверяет нормализацию вложенных
// Kubernetes/volume/network/web enum в публичные OpenAPI имена и отсутствие protobuf префиксов в
// JSON.
func TestRuntimeEnvironmentReadbackUsesOpenAPIEnums(t *testing.T) {
	policy := &controlplanev1.RuntimeEnvironmentPolicy{
		Resources: &controlplanev1.RuntimeResourcePolicy{},
		Volumes:   []*controlplanev1.RuntimeVolume{{Name: "scratch", Kind: controlplanev1.RuntimeVolumeKind_RUNTIME_VOLUME_KIND_EPHEMERAL_DISK}},
		Network: &controlplanev1.RuntimeNetworkPolicy{
			Egress: []*controlplanev1.RuntimeNetworkEgress{{
				Destination: controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_DNS,
				Protocol:    controlplanev1.RuntimeNetworkProtocol_RUNTIME_NETWORK_PROTOCOL_TCP,
			}},
			WebAccess: &controlplanev1.RuntimeWebAccess{Mode: controlplanev1.RuntimeWebAccessMode_RUNTIME_WEB_ACCESS_MODE_NONE},
		},
		KubernetesAccess: &controlplanev1.RuntimeKubernetesAccessProfile{Kind: controlplanev1.RuntimeKubernetesAccessKind_RUNTIME_KUBERNETES_ACCESS_KIND_NONE},
	}
	value, err := messageMap(&controlplanev1.GetRuntimeEnvironmentSetResponse{Environment: &controlplanev1.RuntimeEnvironmentSet{CurrentVersion: &controlplanev1.RuntimeEnvironmentVersion{Policy: policy}}})
	if err != nil {
		t.Fatal(err)
	}
	environment := value["environment"].(map[string]any)
	version := environment["currentVersion"].(map[string]any)
	policyView := version["policy"].(map[string]any)
	network := policyView["network"].(map[string]any)
	egress := network["egress"].([]any)[0].(map[string]any)
	if policyView["kubernetesAccess"].(map[string]any)["kind"] != "NONE" ||
		policyView["volumes"].([]any)[0].(map[string]any)["kind"] != "EPHEMERAL_DISK" ||
		egress["destination"] != "DNS" ||
		egress["protocol"] != "TCP" ||
		network["webAccess"].(map[string]any)["mode"] != "NONE" {
		t.Fatal("runtime environment enum was not normalized to OpenAPI")
	}
	encoded, _ := json.Marshal(value)
	for _, forbidden := range []string{"RUNTIME_VOLUME_KIND_", "RUNTIME_NETWORK_DESTINATION_", "RUNTIME_NETWORK_PROTOCOL_", "RUNTIME_WEB_ACCESS_MODE_", "RUNTIME_KUBERNETES_ACCESS_KIND_"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("Proto enum prefix leaked to HTTP readback: %s", forbidden)
		}
	}
}

// TestEnvironmentDraftMutationRequiresVersionBeforeRPC removes If-Match from draft save and
// requires failure before the synthetic owner records any request, protecting the versioned
// mutation boundary.
// TestEnvironmentDraftMutationRequiresVersionBeforeRPC удаляет If-Match из draft save и требует
// отказа до сохранения любого запроса синтетическим владельцем, защищая versioned mutation
// boundary.
func TestEnvironmentDraftMutationRequiresVersionBeforeRPC(t *testing.T) {
	client := &environmentDraftRecorder{}
	response := httptest.NewRecorder()
	request := managedTestRequest(http.MethodPut, "/api/v1/runtime-environment-drafts/renvd_fixture01", draftSpecBody)
	request.Header.Del("If-Match")
	draftTestHandler(client).ServeHTTP(response, request)
	if response.Code < 400 || client.request != nil {
		t.Fatal("unversioned draft mutation reached RPC")
	}
}

// TestOrganizationEnvironmentDraftUsesCanonicalOwnerAndSeparateVersionPins checks organization
// create uses its dedicated RPC with create mutation version separate from optional environment
// version, preserves text and proves authority fields are absent from the request contract.
// TestOrganizationEnvironmentDraftUsesCanonicalOwnerAndSeparateVersionPins проверяет отдельный RPC
// организационного create с версией create mutation отдельно от optional environment version,
// сохранение текста и отсутствие authority полей в контракте запроса.
func TestOrganizationEnvironmentDraftUsesCanonicalOwnerAndSeparateVersionPins(t *testing.T) {
	for _, existing := range []bool{false, true} {
		client := &environmentDraftRecorder{organization: true}
		body := `{"specification":` + draftSpecBody + `}`
		if existing {
			body = `{"environmentRef":"renv_fixture01","expectedEnvironmentVersion":7,"specification":` + draftSpecBody + `}`
		}
		w := httptest.NewRecorder()
		draftTestHandler(client).ServeHTTP(w, managedTestRequest(http.MethodPost, "/api/v1/organization/runtime-environment-drafts", body))
		input, ok := client.request.(*controlplanev1.CreateOrganizationRuntimeEnvironmentDraftRequest)
		if w.Code != http.StatusCreated || !ok || client.method != controlplanev1.PlatformCommandService_CreateOrganizationRuntimeEnvironmentDraft_FullMethodName || input.Mutation.GetExpectedVersion() != 0 || input.Mutation.IdempotencyKey != "managed-fixture-01" {
			t.Fatalf("organization draft used another owner/version path: status=%d request=%v body=%s", w.Code, client.request, w.Body.String())
		}
		if existing && (input.EnvironmentRef != "renv_fixture01" || input.ExpectedEnvironmentVersion != 7) || !existing && (input.EnvironmentRef != "" || input.ExpectedEnvironmentVersion != 0) {
			t.Fatal("draft OCC and environment pin were mixed")
		}
		if input.Specification.Name != "TYPE_Черновик" || input.Specification.Values[0].Value != "TYPE_не преобразовывать" {
			t.Fatal("organization draft altered caller specification")
		}
		for _, field := range []protoreflect.Name{"project_ref", "organization_ref", "scope_kind", "actor_ref"} {
			if input.ProtoReflect().Descriptor().Fields().ByName(field) != nil {
				t.Fatalf("organization authority became payload: %s", field)
			}
		}
		var result generated.RuntimeEnvironmentDraft
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.ScopeKind != generated.RuntimeResourceScopeKindORGANIZATION || result.OrganizationRef != "org_fixture01" || result.ProjectRef != "" || w.Header().Get("ETag") != `"4"` || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("organization scope/readback lost: status=%d body=%s", w.Code, w.Body.String())
		}
	}
}

// TestEnvironmentDraftReadbackRejectsUnspecifiedAndMalformedScope requires draft reads to reject
// missing/unknown scope, absent organization, project without locator and organization carrying a
// project, without leaking the draft identifier.
// TestEnvironmentDraftReadbackRejectsUnspecifiedAndMalformedScope требует отказа draft чтения для
// missing/unknown scope, отсутствующей организации, проекта без locator и организации с проектом
// без утечки draft identifier.
func TestEnvironmentDraftReadbackRejectsUnspecifiedAndMalformedScope(t *testing.T) {
	for name, mutate := range map[string]func(*controlplanev1.RuntimeEnvironmentDraft){
		"missing scope":           func(d *controlplanev1.RuntimeEnvironmentDraft) { d.ScopeKind = 0 },
		"unknown scope":           func(d *controlplanev1.RuntimeEnvironmentDraft) { d.ScopeKind = 99 },
		"missing organization":    func(d *controlplanev1.RuntimeEnvironmentDraft) { d.OrganizationRef = "" },
		"project without locator": func(d *controlplanev1.RuntimeEnvironmentDraft) { d.ProjectRef = "" },
		"org carrying project": func(d *controlplanev1.RuntimeEnvironmentDraft) {
			d.ScopeKind = controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION
		},
	} {
		t.Run(name, func(t *testing.T) {
			client := &environmentDraftRecorder{mutate: mutate}
			w := httptest.NewRecorder()
			draftTestHandler(client).ServeHTTP(w, managedTestRequest(http.MethodGet, "/api/v1/runtime-environment-drafts/renvd_fixture01", ""))
			if w.Code != http.StatusBadGateway || strings.Contains(w.Body.String(), "renvd_fixture01") {
				t.Fatalf("malformed scope escaped environment draft readback: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

// TestOrganizationEnvironmentDraftRejectsProjectAndMalformedOwnerResponse rejects malformed or
// project-scoped organization-create receipts and caller-injected project/organization/scope
// fields before RPC, keeping authority assignment with the owner.
// TestOrganizationEnvironmentDraftRejectsProjectAndMalformedOwnerResponse отклоняет неверные и
// проектные receipts организационного create и подставленные caller project/organization/scope до
// RPC, оставляя назначение полномочий владельцу.
func TestOrganizationEnvironmentDraftRejectsProjectAndMalformedOwnerResponse(t *testing.T) {
	for name, mutate := range map[string]func(*controlplanev1.RuntimeEnvironmentDraft){
		"missing scope":        func(d *controlplanev1.RuntimeEnvironmentDraft) { d.ScopeKind = 0 },
		"unknown scope":        func(d *controlplanev1.RuntimeEnvironmentDraft) { d.ScopeKind = 99 },
		"missing organization": func(d *controlplanev1.RuntimeEnvironmentDraft) { d.OrganizationRef = "" },
		"org carrying project": func(d *controlplanev1.RuntimeEnvironmentDraft) { d.ProjectRef = "prj_fixture01" },
		"valid project tuple": func(d *controlplanev1.RuntimeEnvironmentDraft) {
			d.ScopeKind = controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT
			d.ProjectRef = "prj_fixture01"
		},
	} {
		t.Run(name, func(t *testing.T) {
			client := &environmentDraftRecorder{organization: true, mutate: mutate}
			w := httptest.NewRecorder()
			draftTestHandler(client).ServeHTTP(w, managedTestRequest(http.MethodPost, "/api/v1/organization/runtime-environment-drafts", `{"specification":`+draftSpecBody+`}`))
			if w.Code != http.StatusBadGateway || strings.Contains(w.Body.String(), "renvd_fixture01") {
				t.Fatalf("organization creation exposed project receipt: %d %s", w.Code, w.Body.String())
			}
		})
	}
	for _, authority := range []string{`"projectRef":"prj_fixture01"`, `"organizationRef":"org_other01"`, `"scopeKind":"PROJECT"`} {
		client := &environmentDraftRecorder{organization: true}
		w := httptest.NewRecorder()
		draftTestHandler(client).ServeHTTP(w, managedTestRequest(http.MethodPost, "/api/v1/organization/runtime-environment-drafts", `{`+authority+`,"specification":`+draftSpecBody+`}`))
		if w.Code != http.StatusBadRequest || client.request != nil {
			t.Fatal("caller scope authority reached organization owner")
		}
	}
}

// TestEnvironmentPublicationRejectsCrossOrganizationAndMissingScope checks valid project and
// organization publication remains usable while missing/unknown or foreign organization scope in
// the returned environment rejects the complete publication envelope.
// TestEnvironmentPublicationRejectsCrossOrganizationAndMissingScope проверяет штатную публикацию
// проекта и организации и отказ всему publication envelope при missing/unknown либо чужой
// organization области возвращённой среды.
func TestEnvironmentPublicationRejectsCrossOrganizationAndMissingScope(t *testing.T) {
	for _, organization := range []bool{false, true} {
		control := &environmentDraftRecorder{organization: organization}
		positive := httptest.NewRecorder()
		draftTestHandler(control).ServeHTTP(positive, managedTestRequest(http.MethodPost, "/api/v1/runtime-environment-drafts/renvd_fixture01/publication", revisionImpactPublishBody))
		if positive.Code != http.StatusOK || !strings.Contains(positive.Body.String(), `"organizationRef":"org_fixture01"`) {
			t.Fatalf("exact scoped publication must remain usable: status=%d body=%s", positive.Code, positive.Body.String())
		}
		for name, mutate := range map[string]func(*controlplanev1.RuntimeEnvironmentSet){
			"missing scope":        func(e *controlplanev1.RuntimeEnvironmentSet) { e.ScopeKind = 0 },
			"unknown scope":        func(e *controlplanev1.RuntimeEnvironmentSet) { e.ScopeKind = 99 },
			"missing organization": func(e *controlplanev1.RuntimeEnvironmentSet) { e.OrganizationRef = "" },
			"foreign organization": func(e *controlplanev1.RuntimeEnvironmentSet) { e.OrganizationRef = "org_other01" },
		} {
			t.Run(map[bool]string{false: "project/", true: "organization/"}[organization]+name, func(t *testing.T) {
				client := &environmentDraftRecorder{organization: organization, mutateEnvironment: mutate}
				w := httptest.NewRecorder()
				draftTestHandler(client).ServeHTTP(w, managedTestRequest(http.MethodPost, "/api/v1/runtime-environment-drafts/renvd_fixture01/publication", revisionImpactPublishBody))
				if w.Code != http.StatusBadGateway || strings.Contains(w.Body.String(), "renv_fixture01") || strings.Contains(w.Body.String(), "org_other01") {
					t.Fatalf("environment publication crossed scope/organization: status=%d body=%s", w.Code, w.Body.String())
				}
			})
		}
	}
}
