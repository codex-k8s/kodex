package httptransport

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/encoding/protojson"
)

// ListModelDiscovery validates a bounded informational search and exact owner response, preserving ProtoJSON int64 strings and explicit false/empty values without implying execution eligibility.
// ListModelDiscovery проверяет ограниченный справочный поиск и точный ответ владельца, сохраняя ProtoJSON int64 strings и явные false/empty values без утверждения допуска исполнения.
func (server *Server) ListModelDiscovery(w http.ResponseWriter, r *http.Request, p generated.ListModelDiscoveryParams) {
	r, ok := catalogRequest(w, r, nil, p.Query, p.PageSize, p.PageToken)
	if !ok {
		return
	}
	if len(stringValue(p.ProviderId)) > 128 {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.ListModelDiscovery(r.Context(), &cp.ListModelDiscoveryRequest{Query: stringValue(p.Query), ProviderId: stringValue(p.ProviderId), Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if !validModelDiscoveryResponse(response) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	raw, err := (protojson.MarshalOptions{EmitUnpopulated: true}).Marshal(response)
	var view map[string]any
	if err != nil || json.Unmarshal(raw, &view) != nil {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	view["items"], view["nextPageToken"] = view["models"], response.Page.NextPageToken
	delete(view, "models")
	delete(view, "page")
	writeJSON(w, http.StatusOK, view)
}

// validModelDiscoveryResponse rejects malformed statuses, identities, missing provenance and impossible bounded pages; raw endpoints and SDK metadata never cross this public projection.
// validModelDiscoveryResponse отклоняет неверные статусы, identities, отсутствие provenance и невозможные страницы; raw endpoints и SDK metadata не пересекают публичную проекцию.
func validModelDiscoveryResponse(response *cp.ListModelDiscoveryResponse) bool {
	if response == nil || response.Source == nil || response.Page == nil || len(response.Models) > 100 || response.Total < int64(len(response.Models)) || response.Total > 50000 || len(response.Page.NextPageToken) > 512 {
		return false
	}
	source := response.Source
	if source.Schema != "kodex.model-provider-discovery/v1" || source.Source != "models.dev" || source.License != "MIT" || source.UpstreamSchema != "models.dev/experimental-v2.0" || len(source.SourceRevision) != 40 || strings.Trim(source.SourceRevision, "0123456789abcdef") != "" || !validManagedDigest(source.SchemaSha256) || !validManagedDigest(source.UpstreamSha256) || !validManagedDigest(source.SnapshotSha256) || !validManagedDigest(source.OverlaySha256) || !validManagedDigest(source.CatalogSha256) || source.SourceUrl != "https://models.dev/_experimental_v2.0_api.json" || source.ModelCount < 1 || source.ModelCount > 50000 {
		return false
	}
	seen := map[string]bool{}
	for _, model := range response.Models {
		if model == nil || !boundedModelText(model.ProviderId, 128) || !boundedModelText(model.ProviderName, 512) || !boundedModelText(model.ModelId, 512) || !boundedModelText(model.Name, 512) || model.ContextTokens < 0 || model.ContextTokens > 1000000000000 || model.OutputTokens < 0 || model.OutputTokens > 1000000000000 || len(model.ReasoningEfforts) > 16 || len(model.InputModalities) > 64 || len(model.OutputModalities) > 64 || !slices.Contains([]string{"KNOWN", "CUSTOM", "REMOVED"}, model.Known) || !slices.Contains([]string{"BUNDLED", "UNSUPPORTED", "UNKNOWN"}, model.Adapter) || !slices.Contains([]string{"AVAILABLE", "UNAVAILABLE", "UNKNOWN"}, model.Account) || !slices.Contains([]string{"UNKNOWN", "UNAVAILABLE"}, model.Allowed) || model.Tested != "NOT_RUN" {
			return false
		}
		identity := model.ProviderId + "\x00" + model.ModelId
		if !boundedModelText(model.Type, 64) || !boundedModelText(model.UpstreamStatus, 32) {
			return false
		}
		for _, effort := range model.ReasoningEfforts {
			if !boundedModelText(effort, 32) {
				return false
			}
		}
		for _, modality := range append(slices.Clone(model.InputModalities), model.OutputModalities...) {
			if !boundedModelText(modality, 128) {
				return false
			}
		}
		if seen[identity] {
			return false
		}
		seen[identity] = true
	}
	return true
}
