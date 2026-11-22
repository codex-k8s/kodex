package httptransport

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"net/http"
)

// ListManagedKubernetesCaptures validates the entire safe owner page before disclosing retained history.
// Exact run/node and unique capture refs are required; integration authority and task receipts are absent.
// ListManagedKubernetesCaptures проверяет полную safe owner page до выдачи сохранённой истории.
// Обязательны exact run/node и unique capture refs; integration authority и task receipts отсутствуют.
func (server *Server) ListManagedKubernetesCaptures(w http.ResponseWriter, r *http.Request, runRef generated.RunRef, nodeRef generated.OpaqueRef, params generated.ListManagedKubernetesCapturesParams) {
	size := 20
	if params.PageSize != nil {
		size = *params.PageSize
	}
	token := ""
	if params.PageToken != nil {
		token = *params.PageToken
	}
	if !validKubernetesInputHTTPRef(runRef, "run_") || !validKubernetesInputHTTPRef(nodeRef, "nod_") || size < 1 || size > 50 || token != "" && !validKubernetesInputHTTPRef(token, "koc_") {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if !server.kubernetesCaptureClientAvailable(w) {
		return
	}
	response, err := server.control.KubernetesCaptures.ListManagedKubernetesCaptures(r.Context(), &cp.ListManagedKubernetesCapturesRequest{RunRef: runRef, NodeRef: nodeRef, Page: &cp.PageRequest{PageSize: int32(size), PageToken: token}})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || response.Page == nil || len(response.Page.ProtoReflect().GetUnknown()) != 0 || len(response.Items) > size || response.Page.NextPageToken != "" && !validKubernetesInputHTTPRef(response.Page.NextPageToken, "koc_") {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result := generated.KubernetesCapturePage{Items: make([]generated.KubernetesCaptureStatus, 0, len(response.Items)), NextPageToken: response.Page.NextPageToken}
	seen := map[string]bool{}
	previous := token
	for _, item := range response.Items {
		readback, valid := kubernetesCapturePublicReadback(item, nil)
		if !valid || readback.View.RunRef != runRef || readback.View.NodeRef != nodeRef || seen[readback.View.Summary.CaptureRef] || readback.View.Summary.CaptureRef <= previous {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		seen[readback.View.Summary.CaptureRef] = true
		previous = readback.View.Summary.CaptureRef
		result.Items = append(result.Items, readback.View)
	}
	if response.Page.NextPageToken != "" && (len(result.Items) != size || response.Page.NextPageToken != previous) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
