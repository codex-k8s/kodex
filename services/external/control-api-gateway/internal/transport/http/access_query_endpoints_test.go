package httptransport

import (
	"encoding/json"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/proto"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestPublicAccessQueryScopesAcrossEndpoints(t *testing.T) {
	for _, target := range []string{`{"kind":"ORGANIZATION"}`, `{"kind":"PROJECT","projectRef":"prj_fixture"}`, `{"kind":"RESOURCE_KIND","projectRef":"prj_fixture","resourceKind":"ROLE_IMAGE"}`, `{"kind":"RESOURCE_INSTANCE","projectRef":"prj_fixture","resourceKind":"PROJECT","resourceRef":"prj_fixture"}`} {
		var decoded generated.AccessScope
		if err := json.Unmarshal([]byte(target), &decoded); err != nil {
			t.Fatal(err)
		}
		for _, operation := range []string{"query", "explain", "simulate"} {
			t.Run(operation+"/"+string(decoded.Kind), func(t *testing.T) {
				permission := "image.source.view"
				decision := &cp.EffectiveAccessDecision{PermissionKey: permission, Decision: cp.AccessDecision_ACCESS_DECISION_ALLOWED, Target: protoAccessScope(decoded)}
				var response proto.Message
				var body string
				switch operation {
				case "query":
					response = &cp.QueryEffectiveAccessResponse{Decisions: []*cp.EffectiveAccessDecision{decision}}
					body = `{"target":` + target + `,"permissionKeys":["image.source.view","image.source.manage"]}`
				case "explain":
					response = &cp.ExplainAccessResponse{Result: decision}
					body = `{"target":` + target + `,"permissionKey":"image.source.view"}`
				case "simulate":
					response = &cp.SimulateAccessResponse{Current: decision, Simulated: decision}
					body = `{"target":` + target + `,"subjectRef":"sub_fixture","permissionKey":"image.source.view","role":{"permissionKeys":["image.source.view"],"allowedScopes":["ORGANIZATION"]},"binding":{"subjectKind":"USER","subjectRef":"sub_fixture","scope":{"kind":"ORGANIZATION"},"conditions":{"requireOwner":false}}}`
				}
				client := &catalogRPCRecorder{response: response}
				handler := generated.Handler(&Server{control: &controlplaneclient.Client{Access: cp.NewAccessServiceClient(client)}})
				path := "/api/v1/administration/access/effective-access/" + operation

				result := httptest.NewRecorder()
				request := httptest.NewRequest("POST", path, strings.NewReader(body))
				request.Header.Set("Content-Type", "application/json")
				handler.ServeHTTP(result, request)
				if result.Code != 200 {
					t.Fatalf("public %s status=%d", operation, result.Code)
				}
				var actual *cp.AccessScope
				switch request := client.request.(type) {
				case *cp.QueryEffectiveAccessRequest:
					actual = request.Target
					if request.SubjectRef != "" || !reflect.DeepEqual(request.PermissionKeys, []string{"image.source.view", "image.source.manage"}) {
						t.Fatal("query authority or permissions changed")
					}
				case *cp.ExplainAccessRequest:
					actual = request.Target
					if request.SubjectRef != "" || request.PermissionKey != permission {
						t.Fatal("explain input changed")
					}
				case *cp.SimulateAccessRequest:
					actual = request.Target
					if request.SubjectRef != "sub_fixture" || request.Binding.SubjectRef != "sub_fixture" {
						t.Fatal("simulation subject or draft changed")
					}
				default:
					t.Fatal("unexpected access RPC")
				}
				expected := &cp.AccessScope{Kind: cp.AccessScopeKind(cp.AccessScopeKind_value["ACCESS_SCOPE_KIND_"+string(decoded.Kind)])}
				if decoded.ProjectRef != nil {
					expected.ProjectRef = *decoded.ProjectRef
				}
				if decoded.ResourceRef != nil {
					expected.ResourceRef = *decoded.ResourceRef
				}
				if decoded.ResourceKind != nil {
					expected.ResourceKind = cp.AccessResourceKind(cp.AccessResourceKind_value["ACCESS_RESOURCE_KIND_"+string(*decoded.ResourceKind)])
				}
				if !proto.Equal(actual, expected) {
					t.Fatal("public scope changed at BFF mapping")
				}
				var payload map[string]any
				if err := json.Unmarshal(result.Body.Bytes(), &payload); err != nil {
					t.Fatal(err)
				}
				var projected map[string]any
				if operation == "query" {
					projected = payload["items"].([]any)[0].(map[string]any)
				} else if operation == "explain" {
					projected = payload["result"].(map[string]any)
				} else {
					projected = payload["current"].(map[string]any)
				}
				var expectedJSON map[string]any
				if err := json.Unmarshal([]byte(target), &expectedJSON); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(projected["target"], expectedJSON) {
					t.Fatal("public decision target changed")
				}
				client.request = nil
				invalid := strings.TrimSuffix(body, "}") + `,"actorRef":"forged"}`
				result = httptest.NewRecorder()
				handler.ServeHTTP(result, httptest.NewRequest("POST", path, strings.NewReader(invalid)))
				if result.Code != 400 || client.request != nil {
					t.Fatal("strict decoder accepted caller actor")
				}
			})
		}
	}
}
