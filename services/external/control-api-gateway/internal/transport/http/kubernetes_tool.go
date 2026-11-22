package httptransport

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// kubernetesToolClientAvailable rejects absent typed composition without a generic execution fallback.
// kubernetesToolClientAvailable закрыто отклоняет отсутствие клиента без обхода через общее исполнение.
func (server *Server) kubernetesToolClientAvailable(w http.ResponseWriter) bool {
	if server.control == nil || server.control.KubernetesTools == nil {
		writeLocalProblem(w, http.StatusServiceUnavailable, "UNAVAILABLE", false)
		return false
	}
	return true
}

// kubernetesToolTerminal recognizes closed owner states without asserting native join or remote termination.
// kubernetesToolTerminal распознаёт закрытые состояния владельца без утверждения native join или remote termination.
func kubernetesToolTerminal(state generated.KubernetesToolViewState) bool {
	switch state {
	case "SUCCEEDED", "FAILED", "REJECTED", "CANCELLED", "UNKNOWN_OUTCOME":
		return true
	default:
		return false
	}
}

// kubernetesToolPublicView projects only registered safe metadata and exact public integers.
// kubernetesToolPublicView выдаёт только зарегистрированные безопасные метаданные и точные публичные числа.
func kubernetesToolPublicView(v *cp.ManagedKubernetesToolView) (generated.KubernetesToolView, bool) {
	var out generated.KubernetesToolView
	if v == nil || len(v.ProtoReflect().GetUnknown()) != 0 {
		return out, false
	}
	_, known := integrationpackage.KubernetesOperationByKey(v.Operation)
	name, stateKnown := cp.ManagedKubernetesToolState_name[int32(v.State)]
	if !known || !stateKnown || v.State == cp.ManagedKubernetesToolState_MANAGED_KUBERNETES_TOOL_STATE_UNSPECIFIED || !validManagedVersion(v.Version) || !validManagedVersion(v.TargetRevision) || !managedkubernetes.ValidLocator(v.TargetRef) || len(v.TargetRef) > 96 || !validKubernetesInputHTTPRef(v.InvocationRef, "inv_") || !validKubernetesInputHTTPRef(v.RunRef, "run_") || !validKubernetesInputHTTPRef(v.NodeRef, "nod_") || !validKubernetesInputHTTPRef(v.ConnectionRef, "int_") {
		return out, false
	}
	reason := "NONE"
	switch v.SafeReasonCode {
	case cp.ManagedKubernetesToolSafeReasonCode_MANAGED_KUBERNETES_TOOL_SAFE_REASON_CODE_NONE:
	case cp.ManagedKubernetesToolSafeReasonCode_MANAGED_KUBERNETES_TOOL_SAFE_REASON_CODE_PORT_FORWARD_PROFILE_UNSUPPORTED:
		if v.Operation != "kubernetes.port-forward" || v.State != cp.ManagedKubernetesToolState_MANAGED_KUBERNETES_TOOL_STATE_FAILED {
			return out, false
		}
		reason = managedkubernetes.ReasonPortForwardProfileUnsupported
	default:
		return out, false
	}
	out = generated.KubernetesToolView{InvocationRef: v.InvocationRef, RunRef: v.RunRef, NodeRef: v.NodeRef, ConnectionRef: v.ConnectionRef, TargetRef: v.TargetRef, TargetRevision: v.TargetRevision, Operation: v.Operation, State: generated.KubernetesToolViewState(strings.TrimPrefix(name, "MANAGED_KUBERNETES_TOOL_STATE_")), Version: v.Version, WaitingForGate: v.WaitingForGate, SafeReasonCode: generated.KubernetesToolViewSafeReasonCode(reason)}
	if len(v.CaptureStatuses) > 256 {
		return out, false
	}
	out.CaptureStatuses = make([]generated.KubernetesCaptureSummary, 0, len(v.CaptureStatuses))
	seenCaptures := map[string]bool{}
	for _, capture := range v.CaptureStatuses {
		summary, valid := kubernetesCapturePublicSummary(capture)
		if !valid || seenCaptures[summary.CaptureRef] {
			return out, false
		}
		seenCaptures[summary.CaptureRef] = true
		out.CaptureStatuses = append(out.CaptureStatuses, summary)
	}
	return out, !(kubernetesToolTerminal(out.State) && out.WaitingForGate)
}

// ListManagedKubernetesTools checks a bounded exact-node owner response before disclosing pending invocations.
// ListManagedKubernetesTools проверяет ограниченный ответ точного узла перед выдачей ожидающих вызовов.
func (server *Server) ListManagedKubernetesTools(w http.ResponseWriter, r *http.Request, runRef generated.RunRef, nodeRef generated.OpaqueRef, params generated.ListManagedKubernetesToolsParams) {
	size := 20
	if params.PageSize != nil {
		size = *params.PageSize
	}
	token := ""
	if params.PageToken != nil {
		token = *params.PageToken
	}
	if !validKubernetesInputHTTPRef(runRef, "run_") || !validKubernetesInputHTTPRef(nodeRef, "nod_") || size < 1 || size > 50 || token != "" && !validKubernetesInputHTTPRef(token, "inv_") {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if !server.kubernetesToolClientAvailable(w) {
		return
	}
	response, err := server.control.KubernetesTools.ListManagedKubernetesTools(r.Context(), &cp.ListManagedKubernetesToolsRequest{RunRef: runRef, NodeRef: nodeRef, Page: &cp.PageRequest{PageSize: int32(size), PageToken: token}})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || response.Page == nil || len(response.Page.ProtoReflect().GetUnknown()) != 0 || len(response.Items) > size || response.Page.NextPageToken != "" && !validKubernetesInputHTTPRef(response.Page.NextPageToken, "inv_") {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result := generated.KubernetesToolPage{Items: make([]generated.KubernetesToolView, 0, len(response.Items)), NextPageToken: response.Page.NextPageToken}
	seen := make(map[string]bool, len(response.Items))
	for _, item := range response.Items {
		view, valid := kubernetesToolPublicView(item)
		if !valid || view.RunRef != runRef || view.NodeRef != nodeRef || seen[view.InvocationRef] {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		seen[view.InvocationRef] = true
		result.Items = append(result.Items, view)
	}
	writeJSON(w, http.StatusOK, result)
}

// GetManagedKubernetesTool reads exact safe metadata; the response cannot substitute a different invocation.
// GetManagedKubernetesTool читает безопасные метаданные и не допускает подмены вызова в ответе.
func (server *Server) GetManagedKubernetesTool(w http.ResponseWriter, r *http.Request, inv generated.IntegrationInvocationRef) {
	if !validKubernetesInputHTTPRef(inv, "inv_") {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if !server.kubernetesToolClientAvailable(w) {
		return
	}
	response, err := server.control.KubernetesTools.GetManagedKubernetesTool(r.Context(), &cp.GetManagedKubernetesToolRequest{InvocationRef: inv})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	view, valid := kubernetesToolPublicView(response.View)
	if !valid || view.InvocationRef != inv {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(view.Version, 10)))
	writeJSON(w, http.StatusOK, view)
}

// StopManagedKubernetesTool accepts only empty closure intent with exact invocation OCC, never private native authority.
// StopManagedKubernetesTool принимает только пустой запрос закрытия с точной версией вызова, без приватных полномочий исполнения.
func (server *Server) StopManagedKubernetesTool(w http.ResponseWriter, r *http.Request, inv generated.IntegrationInvocationRef, params generated.StopManagedKubernetesToolParams) {
	if !validKubernetesInputHTTPRef(inv, "inv_") || !validKubernetesInputHTTPKey(params.IdempotencyKey) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireMutation(w, params.IdempotencyKey, params.IfMatch)
	if !ok {
		return
	}
	if mutation.ExpectedVersion == nil || !validManagedVersion(*mutation.ExpectedVersion) || params.IfMatch != strconv.Quote(strconv.FormatInt(*mutation.ExpectedVersion, 10)) || r.Body == nil {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1025))
	defer clear(raw)
	var body struct{}
	if err != nil || len(raw) > 1024 || internalrpcauth.DecodeExactJSON(raw, &body) != nil {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if !server.kubernetesToolClientAvailable(w) {
		return
	}
	response, err := server.control.KubernetesTools.StopManagedKubernetesTool(r.Context(), &cp.StopManagedKubernetesToolRequest{InvocationRef: inv, Mutation: mutation})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	view, valid := kubernetesToolPublicView(response.View)
	if !valid || view.InvocationRef != inv || !kubernetesToolTerminal(view.State) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(view.Version, 10)))
	writeJSON(w, http.StatusOK, view)
}
