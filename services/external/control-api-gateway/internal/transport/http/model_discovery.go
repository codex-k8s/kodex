package httptransport

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

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
	minimumContext := int64(0)
	if p.MinimumContextTokens != nil {
		minimumContext = *p.MinimumContextTokens
	}
	valid := minimumContext >= 0 && minimumContext <= 1000000000000 && slices.Contains([]string{"", "SUPPORTED", "UNSUPPORTED", "UNKNOWN"}, stringValue(p.Tools)) && slices.Contains([]string{"", "SUPPORTED", "UNSUPPORTED", "UNKNOWN"}, stringValue(p.Reasoning)) && slices.Contains([]string{"", "NONE", "IMPLICIT", "EXPLICIT", "BOTH", "UNKNOWN"}, stringValue(p.Cache)) && slices.Contains([]string{"", "PROVIDER", "FAMILY"}, stringValue(p.GroupBy))
	if p.Tools != nil && stringValue(p.Tools) == "" || p.Reasoning != nil && stringValue(p.Reasoning) == "" || p.Cache != nil && stringValue(p.Cache) == "" || p.GroupBy != nil && stringValue(p.GroupBy) == "" {
		valid = false
	}
	for _, filter := range []string{stringValue(p.ProviderId), stringValue(p.Family), stringValue(p.InputModality), stringValue(p.OutputModality)} {
		if len(filter) > 128 || strings.ContainsAny(filter, "\x00\r\n\t") {
			valid = false
		}
	}
	if !valid {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.ListModelDiscovery(r.Context(), &cp.ListModelDiscoveryRequest{Query: stringValue(p.Query), ProviderId: stringValue(p.ProviderId), Page: page(p.PageSize, p.PageToken), Family: stringValue(p.Family), MinimumContextTokens: minimumContext, InputModality: stringValue(p.InputModality), OutputModality: stringValue(p.OutputModality), Tools: stringValue(p.Tools), Reasoning: stringValue(p.Reasoning), Cache: stringValue(p.Cache), GroupBy: stringValue(p.GroupBy)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if !validModelDiscoveryResponse(response) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	expectedGroup := stringValue(p.GroupBy)
	if expectedGroup == "" {
		expectedGroup = "PROVIDER"
	}
	if response.GroupBy != expectedGroup {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	for _, model := range response.Models {
		if stringValue(p.ProviderId) != "" && model.ProviderId != stringValue(p.ProviderId) || stringValue(p.Family) != "" && model.Family != stringValue(p.Family) || minimumContext > 0 && (!model.ContextKnown || model.ContextTokens < minimumContext) || stringValue(p.Tools) != "" && model.Tools != stringValue(p.Tools) || stringValue(p.Reasoning) != "" && model.Reasoning != stringValue(p.Reasoning) || !discoveryModalityMatches(model.InputModalities, stringValue(p.InputModality)) || !discoveryModalityMatches(model.OutputModalities, stringValue(p.OutputModality)) {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		cache := stringValue(p.Cache)
		if cache == "IMPLICIT" && model.CacheImplicit != "SUPPORTED" || cache == "EXPLICIT" && model.CacheExplicit != "SUPPORTED" || slices.Contains([]string{"BOTH", "NONE", "UNKNOWN"}, cache) && model.Cache != cache {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
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

// discoveryModalityMatches checks the owner result against exact or broad-media public filters; missing metadata cannot satisfy a requested modality.
// discoveryModalityMatches сверяет ответ владельца с точным либо broad-media публичным фильтром; отсутствующие metadata не удовлетворяют заданной модальности.
func discoveryModalityMatches(values []string, wanted string) bool {
	if wanted == "" {
		return true
	}
	for _, value := range values {
		if value == wanted || !strings.Contains(wanted, "/") && strings.HasPrefix(value, wanted+"/") {
			return true
		}
	}
	return false
}

// validModelDiscoveryResponse rejects malformed statuses, identities, missing provenance and impossible bounded pages; raw endpoints and SDK metadata never cross this public projection.
// validModelDiscoveryResponse отклоняет неверные статусы, identities, отсутствие provenance и невозможные страницы; raw endpoints и SDK metadata не пересекают публичную проекцию.
func validModelDiscoveryResponse(response *cp.ListModelDiscoveryResponse) bool {
	if response == nil || response.Source == nil || response.Page == nil || len(response.Models) > 100 || response.Total < int64(len(response.Models)) || response.Total > 50000 || len(response.Page.NextPageToken) > 512 {
		return false
	}
	if !slices.Contains([]string{"PROVIDER", "FAMILY"}, response.GroupBy) {
		return false
	}
	source := response.Source
	if source.Schema != "kodex.model-provider-discovery/v1" || source.Source != "models.dev" || source.License != "MIT" || source.UpstreamSchema != "models.dev/experimental-v2.0" || len(source.SourceRevision) != 40 || strings.Trim(source.SourceRevision, "0123456789abcdef") != "" || !validManagedDigest(source.SchemaSha256) || !validManagedDigest(source.UpstreamSha256) || !validManagedDigest(source.SnapshotSha256) || !validManagedDigest(source.OverlaySha256) || !validManagedDigest(source.CatalogSha256) || source.SourceUrl != "https://models.dev/_experimental_v2.0_api.json" || source.ModelCount < 1 || source.ModelCount > 50000 {
		return false
	}
	if _, err := time.Parse(time.RFC3339Nano, source.RetrievedAt); err != nil || !strings.HasSuffix(source.RetrievedAt, "Z") || source.ProjectionSchema != "urn:kodex:contract:model-provider-discovery:v1" || !validManagedDigest(source.ProjectionSchemaSha256) || source.UpstreamSchemaValidation != "NOT_PERFORMED" {
		return false
	}
	seen := map[string]bool{}
	for _, model := range response.Models {
		if model == nil || !boundedModelText(model.ProviderId, 128) || !boundedModelText(model.ProviderName, 512) || !boundedModelText(model.ModelId, 512) || !boundedModelText(model.Name, 512) || model.ContextTokens < 0 || model.ContextTokens > 1000000000000 || model.OutputTokens < 0 || model.OutputTokens > 1000000000000 || len(model.ReasoningEfforts) > 16 || len(model.InputModalities) > 64 || len(model.OutputModalities) > 64 || !slices.Contains([]string{"KNOWN", "CUSTOM", "REMOVED"}, model.Known) || !slices.Contains([]string{"BUNDLED", "UNSUPPORTED", "UNKNOWN"}, model.Adapter) || !slices.Contains([]string{"AVAILABLE", "UNAVAILABLE", "UNKNOWN"}, model.Account) || !slices.Contains([]string{"UNKNOWN", "UNAVAILABLE"}, model.Allowed) || model.Tested != "NOT_RUN" {
			return false
		}
		identity := model.ProviderId + "\x00" + model.ModelId
		states := []string{"SUPPORTED", "UNSUPPORTED", "UNKNOWN"}
		if !boundedModelText(model.Family, 128) || !slices.Contains(states, model.Tools) || !slices.Contains(states, model.Reasoning) || !slices.Contains(states, model.CacheImplicit) || !slices.Contains(states, model.CacheExplicit) || !slices.Contains([]string{"NONE", "IMPLICIT", "EXPLICIT", "BOTH", "UNKNOWN"}, model.Cache) || !model.ContextKnown && model.ContextTokens != 0 || !model.OutputKnown && model.OutputTokens != 0 {
			return false
		}
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
