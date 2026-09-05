package httptransport

import (
	"net/http"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

func (server *Server) ListManagedConfigurations(w http.ResponseWriter, r *http.Request, p generated.ListManagedConfigurationsParams) {
	r, ok := catalogRequest(w, r, p.ProjectRef, p.Query, p.PageSize, p.PageToken)
	if !ok {
		return
	}
	kind := controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_UNSPECIFIED
	if p.Kind != nil {
		value, known := controlplanev1.ManagedConfigurationKind_value["MANAGED_CONFIGURATION_KIND_"+string(*p.Kind)]
		if !known || value == 0 {
			writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
			return
		}
		kind = controlplanev1.ManagedConfigurationKind(value)
	}
	result, err := server.control.Query.ListManagedConfigurations(r.Context(), &controlplanev1.ListManagedConfigurationsRequest{
		ProjectRef: stringValue(p.ProjectRef), Kind: kind, Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken),
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if result == nil || result.GetTotal() < int64(len(result.GetConfigurations())) || result.GetTotal() > maximumSafeJSONInteger || len(result.GetConfigurations()) > 100 {
		writeLocalProblem(w, http.StatusBadGateway, "INTERNAL", false)
		return
	}
	output := generated.ManagedConfigurationPage{Items: make([]generated.ManagedConfigurationSummary, 0, len(result.GetConfigurations())), Total: result.GetTotal(), NextPageToken: optionalManagedString(result.GetPage().GetNextPageToken())}
	for _, value := range result.GetConfigurations() {
		item, err := managedConfigurationSummaryView(value)
		if err != nil {
			writeLocalProblem(w, http.StatusBadGateway, "INTERNAL", false)
			return
		}
		output.Items = append(output.Items, item)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, output)
}

func managedConfigurationSummaryView(value *controlplanev1.ManagedConfigurationSet) (generated.ManagedConfigurationSummary, error) {
	metadata, err := managedConfigurationMetadataView(value)
	if err != nil {
		return generated.ManagedConfigurationSummary{}, err
	}
	result := generated.ManagedConfigurationSummary{
		Ref: metadata.Ref, Version: metadata.Version, ProjectRef: metadata.ProjectRef, Name: metadata.Name,
		Kind: generated.ManagedConfigurationSummaryKind(metadata.Kind), ManagedBy: generated.ManagedConfigurationSummaryManagedBy(metadata.ManagedBy),
		Source: metadata.Source, SourceRevision: metadata.SourceRevision, UpdatedAt: metadata.UpdatedAt,
	}
	if revision := value.GetCurrentRevision(); revision != nil {
		if revision.GetRef() == "" || !validManagedVersion(revision.GetRevision()) || !validManagedDigest(revision.GetDigest()) {
			return generated.ManagedConfigurationSummary{}, errManagedConfigurationShape
		}
		switch revision.GetState() {
		case controlplanev1.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_DRAFT,
			controlplanev1.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_VALID,
			controlplanev1.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_INVALID,
			controlplanev1.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_PUBLISHED,
			controlplanev1.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_SUPERSEDED,
			controlplanev1.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_DISCARDED:
		default:
			return generated.ManagedConfigurationSummary{}, errManagedConfigurationShape
		}
		result.CurrentRevision = &struct {
			Digest   string                                                    `json:"digest"`
			Ref      generated.OpaqueRef                                       `json:"ref"`
			Revision int64                                                     `json:"revision"`
			State    generated.ManagedConfigurationSummaryCurrentRevisionState `json:"state"`
		}{Digest: revision.GetDigest(), Ref: revision.GetRef(), Revision: revision.GetRevision(), State: generated.ManagedConfigurationSummaryCurrentRevisionState(strings.TrimPrefix(revision.GetState().String(), "MANAGED_CONFIGURATION_STATE_"))}
	}
	return result, nil
}
