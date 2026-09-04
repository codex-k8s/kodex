package httptransport

import (
	"net/http"
	"strconv"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

func (server *Server) GetRuntimeEnvironmentImpact(w http.ResponseWriter, r *http.Request, environmentRef generated.RuntimeEnvironmentRef, versionRef generated.RuntimeEnvironmentVersionRef, p generated.GetRuntimeEnvironmentImpactParams) {
	if !opaqueHTTPReference.MatchString(environmentRef) || !opaqueHTTPReference.MatchString(versionRef) || !validHTTPPage(p.PageSize, p.PageToken) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.GetRuntimeEnvironmentImpact(r.Context(), &controlplanev1.GetRuntimeEnvironmentImpactRequest{EnvironmentRef: environmentRef, VersionRef: versionRef, Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || response.GetEnvironmentRef() != environmentRef || response.GetTargetVersionRef() != versionRef ||
		response.GetEnvironmentVersion() < 1 || response.GetEnvironmentVersion() > maximumSafeJSONInteger || !validManagedDigest(response.GetTargetDigest()) ||
		response.GetTotal() < int64(len(response.GetConsumers())) || response.GetTotal() > maximumSafeJSONInteger || len(response.GetConsumers()) > 100 || len(response.GetPage().GetNextPageToken()) > 512 {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result := generated.RuntimeEnvironmentImpact{EnvironmentRef: environmentRef, EnvironmentVersion: response.GetEnvironmentVersion(), TargetVersionRef: versionRef,
		TargetDigest: response.GetTargetDigest(), Total: response.GetTotal(), NextPageToken: response.GetPage().GetNextPageToken(), Consumers: []generated.RuntimeEnvironmentConsumer{}}
	seen := make(map[string]bool)
	for _, consumer := range response.GetConsumers() {
		item := generated.RuntimeEnvironmentConsumer{AgentRef: consumer.GetAgentRef(), AgentVersion: consumer.GetAgentVersion(), BindingRef: consumer.GetBindingRef(),
			BindingVersion: consumer.GetBindingVersion(), VersionRef: consumer.GetVersionRef(), ProjectRef: consumer.GetProjectRef()}
		if !validEnvironmentConsumer(item) || seen[item.AgentRef] {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		seen[item.AgentRef] = true
		result.Consumers = append(result.Consumers, item)
	}
	w.Header().Set("ETag", "\""+strconv.FormatInt(result.EnvironmentVersion, 10)+"\"")
	writeJSON(w, http.StatusOK, result)
}

func (server *Server) RebindRuntimeEnvironment(w http.ResponseWriter, r *http.Request, environmentRef generated.RuntimeEnvironmentRef, versionRef generated.RuntimeEnvironmentVersionRef, p generated.RebindRuntimeEnvironmentParams) {
	body, ok := decodeJSON[generated.RuntimeEnvironmentRebindInput](w, r)
	if !ok {
		return
	}
	if !opaqueHTTPReference.MatchString(environmentRef) || !opaqueHTTPReference.MatchString(versionRef) || len(body.Consumers) < 1 || len(body.Consumers) > 100 {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireVersionedMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	input := &controlplanev1.RebindRuntimeEnvironmentRequest{Mutation: mutation, EnvironmentRef: environmentRef, VersionRef: versionRef}
	selected := make(map[string]generated.RuntimeEnvironmentConsumer, len(body.Consumers))
	for _, item := range body.Consumers {
		_, duplicate := selected[item.AgentRef]
		if !validEnvironmentConsumer(item) || duplicate {
			writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
			return
		}
		selected[item.AgentRef] = item
		input.Consumers = append(input.Consumers, &controlplanev1.RuntimeEnvironmentConsumer{AgentRef: item.AgentRef, AgentVersion: item.AgentVersion, BindingRef: item.BindingRef,
			BindingVersion: item.BindingVersion, VersionRef: item.VersionRef, ProjectRef: item.ProjectRef})
	}
	response, err := server.control.Command.RebindRuntimeEnvironment(r.Context(), input)
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || len(response.GetBindings()) != len(selected) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result := generated.RuntimeEnvironmentRebindResult{Bindings: []generated.AgentRuntimeEnvironmentBinding{}}
	for _, binding := range response.GetBindings() {
		previous, exists := selected[binding.GetAgentRef()]
		if !exists || binding.GetRef() != previous.BindingRef || binding.GetEnvironmentRef() != environmentRef || binding.GetVersionRef() != versionRef ||
			binding.GetVersion() <= previous.BindingVersion || binding.GetVersion() > maximumSafeJSONInteger || !validManagedDigest(binding.GetDigest()) {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		delete(selected, binding.GetAgentRef())
		result.Bindings = append(result.Bindings, generated.AgentRuntimeEnvironmentBinding{Ref: binding.GetRef(), Version: binding.GetVersion(), AgentRef: binding.GetAgentRef(),
			EnvironmentRef: environmentRef, VersionRef: &versionRef, Digest: binding.GetDigest()})
	}
	writeJSON(w, http.StatusOK, result)
}

func validEnvironmentConsumer(item generated.RuntimeEnvironmentConsumer) bool {
	return opaqueHTTPReference.MatchString(item.AgentRef) && opaqueHTTPReference.MatchString(item.BindingRef) && opaqueHTTPReference.MatchString(item.VersionRef) && opaqueHTTPReference.MatchString(item.ProjectRef) &&
		item.AgentVersion >= 1 && item.AgentVersion <= maximumSafeJSONInteger && item.BindingVersion >= 1 && item.BindingVersion <= maximumSafeJSONInteger
}
