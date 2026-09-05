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

const draftSpecBody = `{"name":"TYPE_Черновик","description":"i18n:исходный текст","imageArtifactRef":"","tools":[],"values":[{"name":"TEST","value":"TYPE_не преобразовывать"}],"secretBindings":[]}`

type environmentDraftRecorder struct {
	grpc.ClientConnInterface
	method   string
	request  proto.Message
	failure  error
	empty    bool
	bindings []*controlplanev1.RuntimeSecretBinding
	mutate   func(*controlplanev1.RuntimeEnvironmentDraft)
}

func (client *environmentDraftRecorder) Invoke(_ context.Context, method string, request, response any, _ ...grpc.CallOption) error {
	client.method, client.request = method, proto.Clone(request.(proto.Message))
	if client.failure != nil {
		return client.failure
	}
	if client.empty {
		return nil
	}
	draft := &controlplanev1.RuntimeEnvironmentDraft{Ref: "renvd_fixture01", ProjectRef: "prj_fixture01", Version: 4, State: "DRAFT",
		Specification: &controlplanev1.RuntimeEnvironmentDraftSpecification{Name: "TYPE_Черновик", Description: "i18n:исходный текст",
			Values:         []*controlplanev1.RuntimeEnvironmentValue{{Name: "TEST", Value: "TYPE_не преобразовывать"}},
			SecretBindings: client.bindings,
		}}
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
	if strings.HasSuffix(method, "/PublishRuntimeEnvironmentDraft") {
		draft.State, draft.PublishedEnvironmentRef = "PUBLISHED", "renv_fixture01"
		draft.ValidationDigest = strings.Repeat("a", 64)
		target.Set(target.Descriptor().Fields().ByName("environment"), protoreflect.ValueOfMessage((&controlplanev1.RuntimeEnvironmentSet{Ref: draft.PublishedEnvironmentRef, Version: 2}).ProtoReflect()))
	}
	if client.mutate != nil {
		client.mutate(draft)
	}
	target.Set(target.Descriptor().Fields().ByName("draft"), protoreflect.ValueOfMessage(draft.ProtoReflect()))
	return nil
}

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

func TestEnvironmentPublishedSecretDescriptorPreservesPin(t *testing.T) {
	for _, revision := range []int64{0, -1, 7, maximumSafeJSONInteger, maximumSafeJSONInteger + 1} {
		client := &catalogRPCRecorder{response: &controlplanev1.GetRuntimeEnvironmentSetResponse{Environment: &controlplanev1.RuntimeEnvironmentSet{
			Ref: "renv_fixture01", Version: 3, CurrentVersion: &controlplanev1.RuntimeEnvironmentVersion{Ref: "renvv_fixture01", SecretDescriptors: []*controlplanev1.RuntimeSecretDescriptor{{
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
		if strings.Contains(w.Body.String(), "internal-only-namespace") || strings.Contains(w.Body.String(), `"namespace"`) {
			t.Fatal("private namespace leaked")
		}
	}
}

func draftTestHandler(client *environmentDraftRecorder) http.Handler {
	return generated.Handler(&Server{control: &controlplaneclient.Client{
		Query: controlplanev1.NewPlatformQueryServiceClient(client), Command: controlplanev1.NewPlatformCommandServiceClient(client),
	}})
}

func TestEnvironmentDraftRoutesKeepSeparateTypedLifecycle(t *testing.T) {
	for _, test := range []struct {
		method, path, body, rpc string
		code                    int
	}{
		{http.MethodPost, "/api/v1/projects/prj_fixture01/runtime-environment-drafts", `{"specification":` + draftSpecBody + `}`, "CreateRuntimeEnvironmentDraft", http.StatusCreated},
		{http.MethodGet, "/api/v1/runtime-environment-drafts/renvd_fixture01", "", "GetRuntimeEnvironmentDraft", http.StatusOK},
		{http.MethodPut, "/api/v1/runtime-environment-drafts/renvd_fixture01", draftSpecBody, "SaveRuntimeEnvironmentDraft", http.StatusOK},
		{http.MethodPost, "/api/v1/runtime-environment-drafts/renvd_fixture01/validation", "", "ValidateRuntimeEnvironmentDraft", http.StatusOK},
		{http.MethodPost, "/api/v1/runtime-environment-drafts/renvd_fixture01/publication", "", "PublishRuntimeEnvironmentDraft", http.StatusOK},
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
			if spec, ok := client.request.(interface {
				GetSpecification() *controlplanev1.RuntimeEnvironmentDraftSpecification
			}); ok &&
				(spec.GetSpecification().GetName() != "TYPE_Черновик" || spec.GetSpecification().GetValues()[0].GetValue() != "TYPE_не преобразовывать") {
				t.Fatal("draft specification changed before RPC")
			}
			var result generated.RuntimeEnvironmentDraft
			if json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Version != 4 || result.Specification.Name != "TYPE_Черновик" ||
				result.Specification.Description != "i18n:исходный текст" || result.Specification.Values[0].Value != "TYPE_не преобразовывать" ||
				result.Specification.Tools == nil || result.Specification.SecretBindings == nil || result.Diagnostics == nil ||
				response.Header().Get("ETag") != `"4"` || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("draft output changed source or shape: %s", response.Body.String())
			}
		})
	}
}

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

func TestEnvironmentDraftRejectsForeignReceipt(t *testing.T) {
	for _, route := range []struct{ method, path, body string }{
		{"GET", "/api/v1/runtime-environment-drafts/renvd_fixture01", ""},
		{"PUT", "/api/v1/runtime-environment-drafts/renvd_fixture01", draftSpecBody},
		{"POST", "/api/v1/runtime-environment-drafts/renvd_fixture01/validation", ""},
		{"POST", "/api/v1/runtime-environment-drafts/renvd_fixture01/publication", ""},
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
	}
	view, ok := environmentDraftPolicyView(input)
	if !ok || view == nil || view.Volumes[0].Kind != "EPHEMERAL_DISK" || view.KubernetesAccess != "NONE" || view.NetworkDestinations[0] != "DNS" ||
		!proto.Equal(input, runtimeEnvironmentPolicyInput(*view)) {
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
