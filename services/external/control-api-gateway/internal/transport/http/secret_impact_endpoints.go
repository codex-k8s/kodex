package httptransport

import (
	"net/http"
	"strconv"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

func (server *Server) GetRuntimeSecretImpact(w http.ResponseWriter, r *http.Request, ref generated.SecretRef, revision generated.RuntimeSecretRevision, p generated.GetRuntimeSecretImpactParams) {
	setRuntimeSecretHeaders(w)
	if !opaqueHTTPReference.MatchString(ref) || revision < 1 || revision > maximumSafeJSONInteger || !validHTTPPage(p.PageSize, p.PageToken) || !validSearchText(stringValue(p.Query), 0, 200) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.GetRuntimeSecretImpact(r.Context(), &controlplanev1.GetRuntimeSecretImpactRequest{SecretRef: ref, Revision: revision, Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || response.GetSecretRef() != ref || response.GetTargetRevision() != revision || response.GetSecretVersion() < 1 || response.GetSecretVersion() > maximumSafeJSONInteger ||
		len(response.GetConsumers()) > 100 || response.GetTotal() < int64(len(response.GetConsumers())) || response.GetTotal() > maximumSafeJSONInteger || len(response.GetPage().GetNextPageToken()) > 512 {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result := generated.RuntimeSecretImpact{SecretRef: ref, SecretVersion: response.GetSecretVersion(), TargetRevision: revision, Total: response.GetTotal(),
		NextPageToken: response.GetPage().GetNextPageToken(), Consumers: []generated.RuntimeSecretImpactConsumer{}}
	seen := make(map[string]bool)
	for _, input := range response.GetConsumers() {
		item, ok := secretImpactConsumerView(input, revision)
		key := item.EnvironmentVersionRef
		if item.Consumer != nil {
			key = item.Consumer.BindingRef
		}
		if !ok || seen[key] {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		seen[key] = true
		result.Consumers = append(result.Consumers, item)
	}
	w.Header().Set("ETag", "\""+strconv.FormatInt(result.SecretVersion, 10)+"\"")
	writeJSON(w, http.StatusOK, result)
}

func secretImpactConsumerView(input *controlplanev1.RuntimeSecretImpactConsumer, target int64) (generated.RuntimeSecretImpactConsumer, bool) {
	if input == nil || !opaqueHTTPReference.MatchString(input.GetEnvironmentRef()) || !opaqueHTTPReference.MatchString(input.GetEnvironmentVersionRef()) ||
		input.GetEnvironmentVersion() < 1 || input.GetEnvironmentVersion() > maximumSafeJSONInteger || len(input.GetSecretRevisions()) < 1 || len(input.GetSecretRevisions()) > 128 {
		return generated.RuntimeSecretImpactConsumer{}, false
	}
	seen := make(map[int64]bool)
	for _, revision := range input.GetSecretRevisions() {
		if revision < 1 || revision > maximumSafeJSONInteger || revision == target || seen[revision] {
			return generated.RuntimeSecretImpactConsumer{}, false
		}
		seen[revision] = true
	}
	consumer := input.GetConsumer()
	if !opaqueHTTPReference.MatchString(consumer.GetProjectRef()) || consumer.GetVersionRef() != input.GetEnvironmentVersionRef() {
		return generated.RuntimeSecretImpactConsumer{}, false
	}
	result := generated.RuntimeSecretImpactConsumer{EnvironmentRef: input.GetEnvironmentRef(), EnvironmentVersion: input.GetEnvironmentVersion(), EnvironmentVersionRef: input.GetEnvironmentVersionRef(),
		ProjectRef: consumer.GetProjectRef(), SecretRevisions: append([]int64{}, input.GetSecretRevisions()...)}
	if consumer.GetAgentRef() == "" {
		return result, consumer.GetAgentVersion() == 0 && consumer.GetBindingRef() == "" && consumer.GetBindingVersion() == 0
	}
	item := generated.RuntimeEnvironmentConsumer{AgentRef: consumer.GetAgentRef(), AgentVersion: consumer.GetAgentVersion(), BindingRef: consumer.GetBindingRef(), BindingVersion: consumer.GetBindingVersion(), VersionRef: consumer.GetVersionRef(), ProjectRef: consumer.GetProjectRef()}
	result.Consumer = &item
	return result, validEnvironmentConsumer(item)
}

func (server *Server) RebindRuntimeSecret(w http.ResponseWriter, r *http.Request, ref generated.SecretRef, revision generated.RuntimeSecretRevision, p generated.RebindRuntimeSecretParams) {
	setRuntimeSecretHeaders(w)
	body, ok := decodeJSON[generated.RuntimeSecretRebindInput](w, r)
	if !ok {
		return
	}
	if !opaqueHTTPReference.MatchString(ref) || revision < 1 || revision > maximumSafeJSONInteger || len(body.Selections) < 1 || len(body.Selections) > 32 {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireVersionedMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	input := &controlplanev1.RebindRuntimeSecretRequest{Mutation: mutation, SecretRef: ref, Revision: revision}
	selected := make(map[string]generated.RuntimeSecretRebindSelection)
	agents := make(map[string]string)
	for _, selection := range body.Selections {
		_, duplicate := selected[selection.EnvironmentRef]
		if duplicate || !opaqueHTTPReference.MatchString(selection.EnvironmentRef) || !opaqueHTTPReference.MatchString(selection.SourceVersionRef) || selection.ExpectedEnvironmentVersion < 1 || selection.ExpectedEnvironmentVersion > maximumSafeJSONInteger || selection.Consumers == nil || len(selection.Consumers) > 100 {
			writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
			return
		}
		selected[selection.EnvironmentRef] = selection
		item := &controlplanev1.RuntimeSecretRebindSelection{EnvironmentRef: selection.EnvironmentRef, ExpectedEnvironmentVersion: selection.ExpectedEnvironmentVersion, SourceVersionRef: selection.SourceVersionRef}
		for _, consumer := range selection.Consumers {
			if !validEnvironmentConsumer(consumer) || consumer.VersionRef != selection.SourceVersionRef || agents[consumer.AgentRef] != "" || len(agents) >= 100 {
				writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
				return
			}
			agents[consumer.AgentRef] = selection.EnvironmentRef
			item.Consumers = append(item.Consumers, &controlplanev1.RuntimeEnvironmentConsumer{AgentRef: consumer.AgentRef, AgentVersion: consumer.AgentVersion, BindingRef: consumer.BindingRef, BindingVersion: consumer.BindingVersion, VersionRef: consumer.VersionRef, ProjectRef: consumer.ProjectRef})
		}
		input.Selections = append(input.Selections, item)
	}
	response, err := server.control.Command.RebindRuntimeSecret(r.Context(), input)
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	result, ok := secretRebindView(response, ref, revision, selected, agents)
	if !ok {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func secretRebindView(response *controlplanev1.RebindRuntimeSecretResponse, ref string, revision int64, selected map[string]generated.RuntimeSecretRebindSelection, agents map[string]string) (generated.RuntimeSecretRebindResult, bool) {
	result := generated.RuntimeSecretRebindResult{Environments: []generated.RuntimeSecretReboundEnvironment{}, Bindings: []generated.AgentRuntimeEnvironmentBinding{}}
	if response == nil || len(response.GetEnvironments()) != len(selected) || len(response.GetBindings()) != len(agents) {
		return result, false
	}
	versions := make(map[string]string)
	consumers := make(map[string]generated.RuntimeEnvironmentConsumer)
	for _, environment := range response.GetEnvironments() {
		selection, exists := selected[environment.GetRef()]
		current := environment.GetCurrentVersion()
		if !exists || versions[environment.GetRef()] != "" || environment.GetVersion() <= selection.ExpectedEnvironmentVersion || environment.GetVersion() > maximumSafeJSONInteger ||
			!opaqueHTTPReference.MatchString(environment.GetProjectRef()) || !opaqueHTTPReference.MatchString(current.GetRef()) || current.GetRef() == selection.SourceVersionRef || !validManagedDigest(current.GetDigest()) || len(current.GetSecretDescriptors()) > 128 {
			return result, false
		}
		matched := false
		for _, descriptor := range current.GetSecretDescriptors() {
			if descriptor.GetSecretRef() == ref {
				if descriptor.GetRevision() != revision {
					return result, false
				}
				matched = true
			}
		}
		if !matched {
			return result, false
		}
		for _, consumer := range selection.Consumers {
			if consumer.ProjectRef != environment.GetProjectRef() {
				return result, false
			}
			consumers[consumer.AgentRef] = consumer
		}
		versions[environment.GetRef()] = current.GetRef()
		result.Environments = append(result.Environments, generated.RuntimeSecretReboundEnvironment{EnvironmentRef: environment.GetRef(), EnvironmentVersion: environment.GetVersion(), ProjectRef: environment.GetProjectRef(), VersionRef: current.GetRef(), Digest: current.GetDigest()})
	}
	for _, binding := range response.GetBindings() {
		previous, exists := consumers[binding.GetAgentRef()]
		version := versions[binding.GetEnvironmentRef()]
		if !exists || binding.GetEnvironmentRef() != agents[binding.GetAgentRef()] || version == "" || binding.GetVersionRef() != version || binding.GetRef() != previous.BindingRef ||
			binding.GetVersion() <= previous.BindingVersion || binding.GetVersion() > maximumSafeJSONInteger || !validManagedDigest(binding.GetDigest()) {
			return result, false
		}
		delete(consumers, binding.GetAgentRef())
		result.Bindings = append(result.Bindings, generated.AgentRuntimeEnvironmentBinding{Ref: binding.GetRef(), Version: binding.GetVersion(), AgentRef: binding.GetAgentRef(), EnvironmentRef: binding.GetEnvironmentRef(), VersionRef: &version, Digest: binding.GetDigest()})
	}
	return result, true
}
