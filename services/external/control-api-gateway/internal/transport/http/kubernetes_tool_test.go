package httptransport

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/proto"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// toolHTTPViewFixture provides safe synthetic metadata without a native stream or private execution material.
// toolHTTPViewFixture задаёт безопасные синтетические метаданные без потока и приватных полномочий исполнения.
func toolHTTPViewFixture() *cp.ManagedKubernetesToolView {
	return &cp.ManagedKubernetesToolView{InvocationRef: "inv_synthetic", RunRef: "run_synthetic", NodeRef: "nod_synthetic", ConnectionRef: "int_synthetic", TargetRef: "target_synthetic", TargetRevision: 1, Operation: "kubernetes.exec", State: cp.ManagedKubernetesToolState_MANAGED_KUBERNETES_TOOL_STATE_RUNNING, Version: 4, WaitingForGate: true, SafeReasonCode: cp.ManagedKubernetesToolSafeReasonCode_MANAGED_KUBERNETES_TOOL_SAFE_REASON_CODE_NONE}
}

// TestKubernetesToolHTTPNoStreamReason exposes the closed owner code through real List/Get routes and rejects unknown upstream enums without rendering raw diagnostics.
// TestKubernetesToolHTTPNoStreamReason выдаёт закрытый owner code через actual List/Get routes, отклоняя unknown upstream enums без raw diagnostics.
func TestKubernetesToolHTTPNoStreamReason(t *testing.T) {
	view := toolHTTPViewFixture()
	view.Operation = "kubernetes.port-forward"
	view.State = cp.ManagedKubernetesToolState_MANAGED_KUBERNETES_TOOL_STATE_FAILED
	view.WaitingForGate = false
	view.SafeReasonCode = cp.ManagedKubernetesToolSafeReasonCode_MANAGED_KUBERNETES_TOOL_SAFE_REASON_CODE_PORT_FORWARD_PROFILE_UNSUPPORTED
	for _, get := range []bool{false, true} {
		route := "/api/v1/runs/run_synthetic/nodes/nod_synthetic/kubernetes-tools"
		var response proto.Message = &cp.ListManagedKubernetesToolsResponse{Items: []*cp.ManagedKubernetesToolView{view}, Page: &cp.PageInfo{}}
		if get {
			route = "/api/v1/integration-invocations/inv_synthetic/kubernetes-tool"
			response = &cp.GetManagedKubernetesToolResponse{View: view}
		}
		r := &catalogRPCRecorder{response: response}
		w := httptest.NewRecorder()
		toolHTTPHandler(r).ServeHTTP(w, inputHTTPRequest(http.MethodGet, route, ""))
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"safeReasonCode":"KUBERNETES_PORT_FORWARD_PROFILE_UNSUPPORTED"`) || strings.Contains(w.Body.String(), "streamRef") {
			t.Fatal("no-stream reason not readable", w.Code)
		}
	}
	for _, mode := range []string{"unspecified", "unknown", "operation", "state"} {
		v := proto.Clone(view).(*cp.ManagedKubernetesToolView)
		switch mode {
		case "unspecified":
			v.SafeReasonCode = 0
		case "unknown":
			v.SafeReasonCode = 99
		case "operation":
			v.Operation = "kubernetes.exec"
		case "state":
			v.State = cp.ManagedKubernetesToolState_MANAGED_KUBERNETES_TOOL_STATE_RUNNING
		}
		if _, valid := kubernetesToolPublicView(v); valid {
			t.Fatal("unknown or foreign reason disclosed", mode)
		}
	}
}

// toolHTTPHandler uses the actual generated HTTP router and typed client; persistence and authentication remain separate tests.
// toolHTTPHandler использует настоящий generated router и typed client; хранение и аутентификация проверяются отдельно.
func toolHTTPHandler(recorder *catalogRPCRecorder) http.Handler {
	return generated.Handler(&Server{control: &controlplaneclient.Client{KubernetesTools: cp.NewManagedKubernetesToolServiceClient(recorder)}})
}

// TestKubernetesToolHTTPActualRoutes preserves exact-node discovery and safe metadata through the generated router.
// TestKubernetesToolHTTPActualRoutes сохраняет выбор точного узла и безопасные метаданные через generated router.
func TestKubernetesToolHTTPActualRoutes(t *testing.T) {
	view := toolHTTPViewFixture()
	for _, item := range []struct {
		path, method string
		response     proto.Message
	}{
		{"/api/v1/runs/run_synthetic/nodes/nod_synthetic/kubernetes-tools", cp.ManagedKubernetesToolService_ListManagedKubernetesTools_FullMethodName, &cp.ListManagedKubernetesToolsResponse{Items: []*cp.ManagedKubernetesToolView{view}, Page: &cp.PageInfo{}}},
		{"/api/v1/integration-invocations/inv_synthetic/kubernetes-tool", cp.ManagedKubernetesToolService_GetManagedKubernetesTool_FullMethodName, &cp.GetManagedKubernetesToolResponse{View: view}},
	} {
		recorder := &catalogRPCRecorder{response: item.response}
		w := httptest.NewRecorder()
		toolHTTPHandler(recorder).ServeHTTP(w, inputHTTPRequest(http.MethodGet, item.path, ""))
		if w.Code != 200 || recorder.method != item.method || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), `"waitingForGate":true`) {
			t.Fatal("pending tool route lost metadata", w.Code)
		}
	}
}

// TestKubernetesToolHTTPStopRejectsAuthorityPayload rejects caller-supplied authority, malformed closure and forged terminal responses.
// TestKubernetesToolHTTPStopRejectsAuthorityPayload отклоняет полномочия в payload, неверное закрытие и подменённые terminal ответы.
func TestKubernetesToolHTTPStopRejectsAuthorityPayload(t *testing.T) {
	const path = "/api/v1/integration-invocations/inv_synthetic/kubernetes-tool/stop"
	view := toolHTTPViewFixture()
	view.State = cp.ManagedKubernetesToolState_MANAGED_KUBERNETES_TOOL_STATE_CANCELLED
	view.WaitingForGate = false
	recorder := &catalogRPCRecorder{response: &cp.StopManagedKubernetesToolResponse{View: view}}
	w := httptest.NewRecorder()
	toolHTTPHandler(recorder).ServeHTTP(w, inputHTTPRequest(http.MethodPost, path, "{}"))
	request, ok := recorder.request.(*cp.StopManagedKubernetesToolRequest)
	if w.Code != 200 || !ok || request.InvocationRef != view.InvocationRef || request.Mutation.GetExpectedVersion() != 3 || request.Mutation.IdempotencyKey != "public-input-key" || recorder.method != cp.ManagedKubernetesToolService_StopManagedKubernetesTool_FullMethodName || w.Header().Get("ETag") != `"4"` {
		t.Fatal("tool Stop lost exact owner mutation", w.Code)
	}
	for _, body := range []string{"null", "[]", `{"actorRef":"usr_foreign"}`, `{"fence":"fake"}`, `{"runRef":"run_foreign"}`, "{}{}", strings.Repeat(" ", 1025)} {
		recorder := &catalogRPCRecorder{}
		w := httptest.NewRecorder()
		toolHTTPHandler(recorder).ServeHTTP(w, inputHTTPRequest(http.MethodPost, path, body))
		if w.Code != 400 || recorder.request != nil {
			t.Fatal("invalid Stop body reached owner", w.Code)
		}
	}
	for _, mutate := range []func(*cp.ManagedKubernetesToolView){func(v *cp.ManagedKubernetesToolView) { v.InvocationRef = "inv_foreignxx" }, func(v *cp.ManagedKubernetesToolView) {
		v.State = cp.ManagedKubernetesToolState_MANAGED_KUBERNETES_TOOL_STATE_RUNNING
	}, func(v *cp.ManagedKubernetesToolView) { v.WaitingForGate = true }, func(v *cp.ManagedKubernetesToolView) { v.Version = 1 << 53 }} {
		altered := proto.Clone(view).(*cp.ManagedKubernetesToolView)
		mutate(altered)
		recorder := &catalogRPCRecorder{response: &cp.StopManagedKubernetesToolResponse{View: altered}}
		w := httptest.NewRecorder()
		toolHTTPHandler(recorder).ServeHTTP(w, inputHTTPRequest(http.MethodPost, path, "{}"))
		if w.Code != 502 {
			t.Fatal("invalid Stop readback accepted", w.Code)
		}
	}
}
