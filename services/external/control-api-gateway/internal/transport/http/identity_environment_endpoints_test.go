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

type identityEnvironmentRecorder struct {
	grpc.ClientConnInterface
	method  string
	request proto.Message
	failure error
	corrupt func(proto.Message)
}

func (client *identityEnvironmentRecorder) Invoke(_ context.Context, method string, request, response any, _ ...grpc.CallOption) error {
	client.method, client.request = method, proto.Clone(request.(proto.Message))
	if client.failure != nil {
		return client.failure
	}
	identity := &controlplanev1.InteractionIdentity{Ref: "iid_fixture01", Version: 4, ConnectionRef: "conn_fixture01", ConnectionVersion: 3,
		ExternalTeamRef: "team-fixture", ExternalChannelRef: "channel-fixture", ExternalUserDigest: strings.Repeat("a", 64), SubjectRef: "sub_fixture01", State: "ACTIVE"}
	var output proto.Message
	switch {
	case strings.HasSuffix(method, "/ListInteractionIdentities"):
		output = &controlplanev1.ListInteractionIdentitiesResponse{Identities: []*controlplanev1.InteractionIdentity{identity}, Page: &controlplanev1.PageInfo{NextPageToken: "next-fixture"}}
	case strings.HasSuffix(method, "/BindInteractionIdentity"):
		output = &controlplanev1.BindInteractionIdentityResponse{Identity: identity}
	case strings.HasSuffix(method, "/RevokeInteractionIdentity"):
		identity.State = "REVOKED"
		output = &controlplanev1.RevokeInteractionIdentityResponse{Identity: identity}
	case strings.HasSuffix(method, "/GetRuntimeEnvironmentImpact"):
		output = &controlplanev1.GetRuntimeEnvironmentImpactResponse{EnvironmentRef: "env_fixture01", EnvironmentVersion: 3, TargetVersionRef: "ever_fixture02", TargetDigest: strings.Repeat("b", 64),
			Consumers: []*controlplanev1.RuntimeEnvironmentConsumer{{AgentRef: "agt_fixture01", AgentVersion: 5, BindingRef: "bind_fixture01", BindingVersion: 2, VersionRef: "ever_fixture01", ProjectRef: "prj_fixture01"}}, Total: 1, Page: &controlplanev1.PageInfo{NextPageToken: "next-fixture"}}
	case strings.HasSuffix(method, "/RebindRuntimeEnvironment"):
		output = &controlplanev1.RebindRuntimeEnvironmentResponse{Bindings: []*controlplanev1.AgentRuntimeEnvironmentBinding{{Ref: "bind_fixture01", Version: 3, AgentRef: "agt_fixture01", EnvironmentRef: "env_fixture01", VersionRef: "ever_fixture02", Digest: strings.Repeat("b", 64)}}}
	default:
		return status.Error(codes.Unimplemented, "unexpected test method")
	}
	if client.corrupt != nil {
		client.corrupt(output)
	}
	proto.Merge(response.(proto.Message), output)
	return nil
}

func identityEnvironmentHandler(client *identityEnvironmentRecorder) http.Handler {
	return generated.Handler(&Server{control: &controlplaneclient.Client{Query: controlplanev1.NewPlatformQueryServiceClient(client), Command: controlplanev1.NewPlatformCommandServiceClient(client)}})
}

func identityBindBody() string {
	return `{"externalTeamRef":"team-fixture","externalChannelRef":"channel-fixture","externalUserDigest":"` + strings.Repeat("a", 64) + `","subjectRef":"sub_fixture01"}`
}

const environmentRebindBody = `{"consumers":[{"agentRef":"agt_fixture01","agentVersion":5,"bindingRef":"bind_fixture01","bindingVersion":2,"versionRef":"ever_fixture01","projectRef":"prj_fixture01"}]}`
const identityCollectionPath = "/api/v1/integration-connections/conn_fixture01/interaction-identities"
const environmentVersionPath = "/api/v1/runtime-environments/env_fixture01/versions/ever_fixture02"

func TestIdentityEnvironmentExactMapping(t *testing.T) {
	for _, tc := range []struct {
		method, path, body, rpc string
		code                    int
	}{
		{http.MethodGet, identityCollectionPath + "?pageSize=7&pageToken=cursor-fixture", "", "ListInteractionIdentities", 200},
		{http.MethodPost, identityCollectionPath, identityBindBody(), "BindInteractionIdentity", 201},
		{http.MethodDelete, "/api/v1/interaction-identities/iid_fixture01", "", "RevokeInteractionIdentity", 200},
		{http.MethodGet, environmentVersionPath + "/impact?pageSize=7&pageToken=cursor-fixture", "", "GetRuntimeEnvironmentImpact", 200},
		{http.MethodPost, environmentVersionPath + "/consumer-bindings", environmentRebindBody, "RebindRuntimeEnvironment", 200},
	} {
		t.Run(tc.rpc, func(t *testing.T) {
			client := &identityEnvironmentRecorder{}
			w := httptest.NewRecorder()
			identityEnvironmentHandler(client).ServeHTTP(w, managedTestRequest(tc.method, tc.path, tc.body))
			if w.Code != tc.code || !strings.HasSuffix(client.method, "/"+tc.rpc) {
				t.Fatalf("status=%d rpc=%s body=%s", w.Code, client.method, w.Body.String())
			}
			var mutation *controlplanev1.MutationContext
			var pagination *controlplanev1.PageRequest
			switch input := client.request.(type) {
			case *controlplanev1.ListInteractionIdentitiesRequest:
				if input.ConnectionRef != "conn_fixture01" {
					t.Fatal(input)
				}
				pagination = input.Page
			case *controlplanev1.BindInteractionIdentityRequest:
				if input.ConnectionRef != "conn_fixture01" || input.SubjectRef != "sub_fixture01" || input.ExternalUserDigest != strings.Repeat("a", 64) {
					t.Fatal(input)
				}
				mutation = input.Mutation
			case *controlplanev1.RevokeInteractionIdentityRequest:
				if input.IdentityRef != "iid_fixture01" {
					t.Fatal(input)
				}
				mutation = input.Mutation
			case *controlplanev1.GetRuntimeEnvironmentImpactRequest:
				if input.EnvironmentRef != "env_fixture01" || input.VersionRef != "ever_fixture02" {
					t.Fatal(input)
				}
				pagination = input.Page
				if w.Header().Get("ETag") != `"3"` {
					t.Fatal(w.Header())
				}
			case *controlplanev1.RebindRuntimeEnvironmentRequest:
				mutation = input.Mutation
				if input.EnvironmentRef != "env_fixture01" || input.VersionRef != "ever_fixture02" || len(input.Consumers) != 1 || input.Consumers[0].VersionRef != "ever_fixture01" || input.Consumers[0].BindingVersion != 2 || input.Consumers[0].AgentVersion != 5 {
					t.Fatal(input)
				}
				var result generated.RuntimeEnvironmentRebindResult
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || len(result.Bindings) != 1 || stringValue(result.Bindings[0].VersionRef) != "ever_fixture02" {
					t.Fatalf("bad bindings: %s", w.Body.String())
				}
			}
			if mutation != nil && (mutation.GetExpectedVersion() != 3 || mutation.IdempotencyKey != "managed-fixture-01") {
				t.Fatal(mutation)
			}
			if pagination != nil && (pagination.PageSize != 7 || pagination.PageToken != "cursor-fixture") {
				t.Fatal(pagination)
			}
		})
	}
}

func TestIdentityEnvironmentRejectInvalidInputBeforeRPC(t *testing.T) {
	for _, tc := range []struct{ name, path, body, header, value string }{
		{"actor", identityCollectionPath, strings.TrimSuffix(identityBindBody(), "}") + `,"actorRef":"forged"}`, "", ""},
		{"digest", identityCollectionPath, strings.Replace(identityBindBody(), strings.Repeat("a", 64), "bad", 1), "", ""},
		{"control", identityCollectionPath, strings.Replace(identityBindBody(), "team-fixture", `team\nfixture`, 1), "", ""},
		{"missing-occ", identityCollectionPath, identityBindBody(), "If-Match", ""},
		{"weak-occ", identityCollectionPath, identityBindBody(), "If-Match", `W/"3"`},
		{"unsafe-occ", identityCollectionPath, identityBindBody(), "If-Match", `"9007199254740992"`},
		{"noncanonical-occ", identityCollectionPath, identityBindBody(), "If-Match", `"03"`},
		{"short-idempotency", identityCollectionPath, identityBindBody(), "Idempotency-Key", "short"},
		{"missing-idempotency", identityCollectionPath, identityBindBody(), "Idempotency-Key", ""},
		{"empty-selection", environmentVersionPath + "/consumer-bindings", `{"consumers":[]}`, "", ""},
		{"unsafe-version", environmentVersionPath + "/consumer-bindings", strings.Replace(environmentRebindBody, `"agentVersion":5`, `"agentVersion":9007199254740992`, 1), "", ""},
		{"duplicate-selection", environmentVersionPath + "/consumer-bindings", strings.TrimSuffix(environmentRebindBody, "]}") + "," + strings.TrimPrefix(environmentRebindBody, `{"consumers":[`), "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &identityEnvironmentRecorder{}
			w := httptest.NewRecorder()
			r := managedTestRequest(http.MethodPost, tc.path, tc.body)
			if tc.header != "" {
				r.Header.Del(tc.header)
				if tc.value != "" {
					r.Header.Set(tc.header, tc.value)
				}
			}
			identityEnvironmentHandler(client).ServeHTTP(w, r)
			if w.Code != 400 || client.request != nil {
				t.Fatalf("status=%d called=%v body=%s", w.Code, client.request != nil, w.Body.String())
			}
		})
	}
}

func TestIdentityEnvironmentPageBoundsBeforeRPC(t *testing.T) {
	for _, path := range []string{identityCollectionPath, environmentVersionPath + "/impact"} {
		for _, query := range []string{"pageSize=0", "pageSize=101", "pageSize=4294967297", "pageToken=" + strings.Repeat("a", 513)} {
			client := &identityEnvironmentRecorder{}
			w := httptest.NewRecorder()
			identityEnvironmentHandler(client).ServeHTTP(w, managedTestRequest(http.MethodGet, path+"?"+query, ""))
			if w.Code != 400 || client.request != nil {
				t.Fatalf("page validation status=%d called=%t", w.Code, client.request != nil)
			}
		}
	}
}

func TestIdentityEnvironmentAuthorityErrorsRemainClosed(t *testing.T) {
	for _, code := range []codes.Code{codes.PermissionDenied, codes.NotFound, codes.Aborted, codes.Unauthenticated, codes.Unavailable} {
		t.Run(code.String(), func(t *testing.T) {
			client := &identityEnvironmentRecorder{failure: status.Error(code, "private upstream detail")}
			w := httptest.NewRecorder()
			identityEnvironmentHandler(client).ServeHTTP(w, managedTestRequest(http.MethodPost, identityCollectionPath, identityBindBody()))
			want := map[codes.Code]int{codes.PermissionDenied: 403, codes.NotFound: 404, codes.Aborted: 412, codes.Unauthenticated: 401, codes.Unavailable: 503}[code]
			if w.Code != want || strings.Contains(w.Body.String(), "private upstream detail") {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestIdentityEnvironmentRejectCorruptUpstream(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body string
		corrupt                  func(proto.Message)
	}{
		{"wrong-connection", "GET", identityCollectionPath, "", func(m proto.Message) {
			m.(*controlplanev1.ListInteractionIdentitiesResponse).Identities[0].ConnectionRef = "conn_other01"
		}},
		{"unknown-state", "POST", identityCollectionPath, identityBindBody(), func(m proto.Message) { m.(*controlplanev1.BindInteractionIdentityResponse).Identity.State = "UNKNOWN" }},
		{"wrong-target", "GET", environmentVersionPath + "/impact", "", func(m proto.Message) {
			m.(*controlplanev1.GetRuntimeEnvironmentImpactResponse).TargetVersionRef = "ever_other01"
		}},
		{"nil-consumer", "GET", environmentVersionPath + "/impact", "", func(m proto.Message) { m.(*controlplanev1.GetRuntimeEnvironmentImpactResponse).Consumers[0] = nil }},
		{"unsafe-version", "GET", environmentVersionPath + "/impact", "", func(m proto.Message) {
			m.(*controlplanev1.GetRuntimeEnvironmentImpactResponse).EnvironmentVersion = maximumSafeJSONInteger + 1
		}},
		{"unselected-agent", "POST", environmentVersionPath + "/consumer-bindings", environmentRebindBody, func(m proto.Message) {
			m.(*controlplanev1.RebindRuntimeEnvironmentResponse).Bindings[0].AgentRef = "agt_other01"
		}},
		{"missing-binding", "POST", environmentVersionPath + "/consumer-bindings", environmentRebindBody, func(m proto.Message) { m.(*controlplanev1.RebindRuntimeEnvironmentResponse).Bindings = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &identityEnvironmentRecorder{corrupt: tc.corrupt}
			w := httptest.NewRecorder()
			identityEnvironmentHandler(client).ServeHTTP(w, managedTestRequest(tc.method, tc.path, tc.body))
			if w.Code != 502 {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}
