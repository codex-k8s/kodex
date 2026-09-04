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

const secretRevisionPath = "/api/v1/runtime-secrets/sec_fixture01/revisions/2"
const secretRebindBody = `{"selections":[{"environmentRef":"env_fixture01","expectedEnvironmentVersion":4,"sourceVersionRef":"ever_fixture01","consumers":[{"agentRef":"agt_fixture01","agentVersion":5,"bindingRef":"bind_fixture01","bindingVersion":2,"versionRef":"ever_fixture01","projectRef":"prj_fixture01"}]}]}`

type secretImpactRecorder struct {
	grpc.ClientConnInterface
	request proto.Message
	failure error
	corrupt func(proto.Message)
}

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
				{EnvironmentRef: "env_fixture01", EnvironmentVersion: 4, EnvironmentVersionRef: "ever_fixture01", SecretRevisions: []int64{1}, Consumer: &controlplanev1.RuntimeEnvironmentConsumer{AgentRef: "agt_fixture01", AgentVersion: 5, BindingRef: "bind_fixture01", BindingVersion: 2, VersionRef: "ever_fixture01", ProjectRef: "prj_fixture01"}},
				{EnvironmentRef: "env_fixture02", EnvironmentVersion: 1, EnvironmentVersionRef: "ever_unbound01", SecretRevisions: []int64{1}, Consumer: &controlplanev1.RuntimeEnvironmentConsumer{VersionRef: "ever_unbound01", ProjectRef: "prj_fixture01"}},
			}}
	case controlplanev1.PlatformCommandService_RebindRuntimeSecret_FullMethodName:
		result = &controlplanev1.RebindRuntimeSecretResponse{Environments: []*controlplanev1.RuntimeEnvironmentSet{{Ref: "env_fixture01", Version: 5, ProjectRef: "prj_fixture01", CurrentVersion: &controlplanev1.RuntimeEnvironmentVersion{
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

func secretImpactHandler(client *secretImpactRecorder) http.Handler {
	return generated.Handler(&Server{control: &controlplaneclient.Client{Query: controlplanev1.NewPlatformQueryServiceClient(client), Command: controlplanev1.NewPlatformCommandServiceClient(client)}})
}

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

func TestSecretRebindAllowsPublicationWithoutAgentBindings(t *testing.T) {
	client := &secretImpactRecorder{corrupt: func(m proto.Message) { m.(*controlplanev1.RebindRuntimeSecretResponse).Bindings = nil }}
	w := httptest.NewRecorder()
	body := `{"selections":[{"environmentRef":"env_fixture01","expectedEnvironmentVersion":4,"sourceVersionRef":"ever_fixture01","consumers":[]}]}`
	secretImpactHandler(client).ServeHTTP(w, managedTestRequest(http.MethodPost, secretRevisionPath+"/consumer-bindings", body))
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

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
