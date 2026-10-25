package httptransport

import (
	"fmt"
	"net/http/httptest"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestWorkflowGateTimeoutHTTPBoundary proves the real HTTP routes reject integer wraparound before
// any RPC and preserve exact valid values and absence in their generated requests.
// TestWorkflowGateTimeoutHTTPBoundary проверяет отказ реальных HTTP-маршрутов при переполнении до
// любого RPC и сохранение точных допустимых значений либо отсутствия в generated-запросе.
func TestWorkflowGateTimeoutHTTPBoundary(t *testing.T) {
	for _, route := range []struct{ method, path string }{{"POST", "/api/v1/projects/prj_fixture01/workflows"}, {"PATCH", "/api/v1/workflows/wfl_fixture01"}} {
		for _, raw := range []string{"", "1", "604800", "0", "-1", "604801", "4294967297", "-4294967295", "1.5"} {
			for _, gated := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/%s/%t", route.method, raw, gated), func(t *testing.T) {
					field := ""
					if raw != "" {
						field = `,"approvalTimeoutSeconds":` + raw
					}
					body := fmt.Sprintf(`{"name":"Процесс","purpose":"Согласование","coordinatorAgentRef":"agt_fixture01","steps":[{"position":1,"name":"Результат","purpose":"Проверить","agentRef":"agt_fixture01","parallel":false,"parallelGroup":0,"timeoutSeconds":600,"expectedResult":"Ответ","humanGate":%t,"gateDecisions":["APPROVE"],"requiredCapabilityKeys":[]%s}]}`, gated, field)
					client := &catalogRPCRecorder{failure: status.Error(codes.Unavailable, "fixture unavailable")}
					handler := generated.Handler(&Server{control: &controlplaneclient.Client{Command: cp.NewPlatformCommandServiceClient(client)}})
					writer := httptest.NewRecorder()
					handler.ServeHTTP(writer, managedTestRequest(route.method, route.path, body))
					valid := raw == "" || gated && (raw == "1" || raw == "604800")
					if !valid {
						if writer.Code != 400 || client.request != nil {
							t.Fatal("invalid gate wait reached RPC or escaped HTTP validation")
						}
						return
					}
					var draft *cp.WorkflowVersion
					switch request := client.request.(type) {
					case *cp.CreateWorkflowRequest:
						draft = request.Draft
					case *cp.UpdateWorkflowDraftRequest:
						draft = request.Draft
					default:
						t.Fatal("valid wait did not reach typed owner RPC")
					}
					if len(draft.Steps) != 1 {
						t.Fatal("workflow step missing")
					}
					actual := draft.Steps[0].ApprovalTimeoutSeconds
					if raw == "" {
						if actual != nil {
							t.Fatal("absent policy gained a default")
						}
					} else if actual == nil || fmt.Sprint(*actual) != raw {
						t.Fatal("valid wait changed in transport")
					}
				})
			}
		}
	}
}
