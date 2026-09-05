package httptransport

import (
	"net/http"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

func (server *Server) GetAgentRuntimeConfiguration(writer http.ResponseWriter, request *http.Request, agentRef generated.AgentRef) {
	if !opaqueHTTPReference.MatchString(agentRef) {
		writeLocalProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.GetAgentRuntimeConfiguration(request.Context(), &controlplanev1.GetAgentRuntimeConfigurationRequest{AgentRef: agentRef})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeAgentRuntimeConfiguration(writer, http.StatusOK, response.GetRuntimeConfiguration(), agentRef)
}

func (server *Server) ListAgentRuntimeConfigurationVersions(writer http.ResponseWriter, request *http.Request, agentRef generated.AgentRef, parameters generated.ListAgentRuntimeConfigurationVersionsParams) {
	response, err := server.control.Query.ListAgentRuntimeConfigurationVersions(request.Context(), &controlplanev1.ListAgentRuntimeConfigurationVersionsRequest{
		AgentRef: agentRef, Page: page(parameters.PageSize, parameters.PageToken),
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeMessage(writer, http.StatusOK, response, "", "configurations")
}

func (server *Server) PublishAgentRuntimeConfiguration(writer http.ResponseWriter, request *http.Request, agentRef generated.AgentRef, parameters generated.PublishAgentRuntimeConfigurationParams) {
	body, ok := decodeJSON[generated.AgentRuntimeConfigurationInput](writer, request)
	if !ok {
		return
	}
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, parameters.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.PublishAgentRuntimeConfiguration(request.Context(), &controlplanev1.PublishAgentRuntimeConfigurationRequest{
		Mutation: mutation, AgentRef: agentRef, RuntimeProfileRef: body.RuntimeProfileRef, Model: body.Model,
		ProviderPolicyMode: string(body.ProviderPolicyMode), ProviderAccounts: providerAccountCandidates(body.ProviderAccounts),
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeAgentRuntimeConfiguration(writer, http.StatusOK, response.GetRuntimeConfiguration(), agentRef)
}

func (server *Server) CreateConfigOverlayDraft(writer http.ResponseWriter, request *http.Request, agentRef generated.AgentRef, parameters generated.CreateConfigOverlayDraftParams) {
	body, ok := decodeJSON[generated.ConfigOverlayDraftInput](writer, request)
	if !ok {
		return
	}
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, parameters.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.CreateConfigOverlayDraft(request.Context(), &controlplanev1.CreateConfigOverlayDraftRequest{Mutation: mutation, AgentRef: agentRef, Content: body.Content})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeAgentRuntimeConfiguration(writer, http.StatusCreated, response.GetRuntimeConfiguration(), agentRef)
}

func (server *Server) ValidateConfigOverlayDraft(writer http.ResponseWriter, request *http.Request, agentRef generated.AgentRef, parameters generated.ValidateConfigOverlayDraftParams) {
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, parameters.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.ValidateConfigOverlayDraft(request.Context(), &controlplanev1.ValidateConfigOverlayDraftRequest{Mutation: mutation, AgentRef: agentRef})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeAgentRuntimeConfiguration(writer, http.StatusOK, response.GetRuntimeConfiguration(), agentRef)
}

func (server *Server) PublishConfigOverlayDraft(writer http.ResponseWriter, request *http.Request, agentRef generated.AgentRef, parameters generated.PublishConfigOverlayDraftParams) {
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, parameters.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.PublishConfigOverlayDraft(request.Context(), &controlplanev1.PublishConfigOverlayDraftRequest{Mutation: mutation, AgentRef: agentRef})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeAgentRuntimeConfiguration(writer, http.StatusOK, response.GetRuntimeConfiguration(), agentRef)
}

func (server *Server) RollbackConfigOverlay(writer http.ResponseWriter, request *http.Request, agentRef generated.AgentRef, parameters generated.RollbackConfigOverlayParams) {
	body, ok := decodeJSON[generated.ConfigOverlayRollbackInput](writer, request)
	if !ok {
		return
	}
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, parameters.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.RollbackConfigOverlay(request.Context(), &controlplanev1.RollbackConfigOverlayRequest{Mutation: mutation, AgentRef: agentRef, PublishedOverlayRef: body.PublishedOverlayRef})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeAgentRuntimeConfiguration(writer, http.StatusOK, response.GetRuntimeConfiguration(), agentRef)
}

func (server *Server) BindAgentRuntimeEnvironment(writer http.ResponseWriter, request *http.Request, agentRef generated.AgentRef, parameters generated.BindAgentRuntimeEnvironmentParams) {
	body, ok := decodeJSON[generated.RuntimeEnvironmentBindingInput](writer, request)
	if !ok {
		return
	}
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, parameters.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.BindAgentRuntimeEnvironment(request.Context(), &controlplanev1.BindAgentRuntimeEnvironmentRequest{Mutation: mutation, AgentRef: agentRef, EnvironmentRef: body.EnvironmentRef})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeAgentRuntimeConfiguration(writer, http.StatusOK, response.GetRuntimeConfiguration(), agentRef)
}

func writeAgentRuntimeConfiguration(writer http.ResponseWriter, statusCode int, view *controlplanev1.AgentRuntimeConfigurationView, agentRef string) {
	if view == nil || !validManagedVersion(view.GetAgentVersion()) || view.GetConfiguration().GetAgentRef() != agentRef || len(view.GetSkillBindings())+len(view.GetMemoryBindings()) > 128 {
		writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	seenRefs, seenResources := make(map[string]bool), make(map[string]bool)
	for _, bindings := range [][]*controlplanev1.AgentContextBinding{view.GetSkillBindings(), view.GetMemoryBindings()} {
		for _, binding := range bindings {
			if !validAgentContextBinding(binding, agentRef) || seenRefs[binding.GetRef()] || seenResources[binding.GetResourceRef()] {
				writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
				return
			}
			seenRefs[binding.GetRef()], seenResources[binding.GetResourceRef()] = true, true
		}
	}
	writeMessage(writer, statusCode, view, "", "")
}

func (server *Server) ListRuntimeEnvironmentSets(writer http.ResponseWriter, request *http.Request, projectRef generated.ProjectRef, parameters generated.ListRuntimeEnvironmentSetsParams) {
	request, ok := withProjectReference(writer, request, projectRef)
	if !ok {
		return
	}
	response, err := server.control.Query.ListRuntimeEnvironmentSets(request.Context(), &controlplanev1.ListRuntimeEnvironmentSetsRequest{
		ProjectRef: projectRef, Query: stringValue(parameters.Query), Page: page(parameters.PageSize, parameters.PageToken),
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeMessage(writer, http.StatusOK, response, "", "environments")
}

func (server *Server) GetRuntimeEnvironmentSet(writer http.ResponseWriter, request *http.Request, environmentRef generated.RuntimeEnvironmentRef) {
	response, err := server.control.Query.GetRuntimeEnvironmentSet(request.Context(), &controlplanev1.GetRuntimeEnvironmentSetRequest{EnvironmentRef: environmentRef})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeMessage(writer, http.StatusOK, response, "environment", "")
}

func (server *Server) ListRuntimeEnvironmentVersions(writer http.ResponseWriter, request *http.Request, environmentRef generated.RuntimeEnvironmentRef, parameters generated.ListRuntimeEnvironmentVersionsParams) {
	response, err := server.control.Query.ListRuntimeEnvironmentVersions(request.Context(), &controlplanev1.ListRuntimeEnvironmentVersionsRequest{
		EnvironmentRef: environmentRef, Page: page(parameters.PageSize, parameters.PageToken),
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeMessage(writer, http.StatusOK, response, "", "versions")
}

func (server *Server) CreateRuntimeEnvironmentSet(writer http.ResponseWriter, request *http.Request, projectRef generated.ProjectRef, parameters generated.CreateRuntimeEnvironmentSetParams) {
	request, ok := withProjectReference(writer, request, projectRef)
	if !ok {
		return
	}
	body, ok := decodeJSON[generated.RuntimeEnvironmentInput](writer, request)
	if !ok {
		return
	}
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, "")
	if !ok {
		return
	}
	response, err := server.control.Command.CreateRuntimeEnvironmentSet(request.Context(), &controlplanev1.CreateRuntimeEnvironmentSetRequest{
		Mutation: mutation, ProjectRef: projectRef, Name: body.Name, Description: body.Description,
		ImageArtifactRef: body.ImageArtifactRef, Values: runtimeEnvironmentValues(body.Values),
		SecretBindings: runtimeSecretBindings(body.SecretBindings), Tools: runtimeEnvironmentTools(body.Tools),
		Policy: runtimeEnvironmentPolicyInput(body.Policy),
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeMessage(writer, http.StatusCreated, response, "environment", "")
}

func (server *Server) PublishRuntimeEnvironmentVersion(writer http.ResponseWriter, request *http.Request, environmentRef generated.RuntimeEnvironmentRef, parameters generated.PublishRuntimeEnvironmentVersionParams) {
	body, ok := decodeJSON[generated.RuntimeEnvironmentInput](writer, request)
	if !ok {
		return
	}
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, parameters.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.PublishRuntimeEnvironmentVersion(request.Context(), &controlplanev1.PublishRuntimeEnvironmentVersionRequest{
		Mutation: mutation, EnvironmentRef: environmentRef, Name: body.Name, Description: body.Description,
		ImageArtifactRef: body.ImageArtifactRef, Values: runtimeEnvironmentValues(body.Values),
		SecretBindings: runtimeSecretBindings(body.SecretBindings), Tools: runtimeEnvironmentTools(body.Tools),
		Policy: runtimeEnvironmentPolicyInput(body.Policy),
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeMessage(writer, http.StatusOK, response, "environment", "")
}

func (server *Server) RollbackRuntimeEnvironment(writer http.ResponseWriter, request *http.Request, environmentRef generated.RuntimeEnvironmentRef, parameters generated.RollbackRuntimeEnvironmentParams) {
	body, ok := decodeJSON[generated.RuntimeEnvironmentRollbackInput](writer, request)
	if !ok {
		return
	}
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, parameters.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.RollbackRuntimeEnvironment(request.Context(), &controlplanev1.RollbackRuntimeEnvironmentRequest{Mutation: mutation, EnvironmentRef: environmentRef, PublishedVersionRef: body.PublishedVersionRef})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeMessage(writer, http.StatusOK, response, "environment", "")
}

func (server *Server) ListTemplateVariables(writer http.ResponseWriter, request *http.Request, projectRef generated.ProjectRef, parameters generated.ListTemplateVariablesParams) {
	request, ok := withProjectReference(writer, request, projectRef)
	if !ok {
		return
	}
	response, err := server.control.Query.ListTemplateVariables(request.Context(), &controlplanev1.ListTemplateVariablesRequest{
		ProjectRef: projectRef, Query: stringValue(parameters.Query), Page: page(parameters.PageSize, parameters.PageToken),
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeMessage(writer, http.StatusOK, response, "", "variables")
}

func providerAccountCandidates(input []generated.ProviderAccountCandidate) []*controlplanev1.ProviderAccountCandidate {
	result := make([]*controlplanev1.ProviderAccountCandidate, 0, len(input))
	for _, item := range input {
		result = append(result, &controlplanev1.ProviderAccountCandidate{AccountRef: item.AccountRef, Weight: int32(item.Weight)})
	}
	return result
}

func runtimeEnvironmentValues(input []generated.RuntimeEnvironmentValue) []*controlplanev1.RuntimeEnvironmentValue {
	result := make([]*controlplanev1.RuntimeEnvironmentValue, 0, len(input))
	for _, item := range input {
		result = append(result, &controlplanev1.RuntimeEnvironmentValue{Name: item.Name, Value: item.Value})
	}
	return result
}

func runtimeSecretBindings(input []generated.RuntimeSecretBinding) []*controlplanev1.RuntimeSecretBinding {
	result := make([]*controlplanev1.RuntimeSecretBinding, 0, len(input))
	for _, item := range input {
		result = append(result, &controlplanev1.RuntimeSecretBinding{Name: item.Name, SecretRef: item.SecretRef})
	}
	return result
}

func runtimeEnvironmentTools(input []generated.RuntimeEnvironmentTool) []*controlplanev1.RuntimeEnvironmentTool {
	result := make([]*controlplanev1.RuntimeEnvironmentTool, 0, len(input))
	for _, item := range input {
		result = append(result, &controlplanev1.RuntimeEnvironmentTool{Name: item.Name, Command: item.Command, Description: item.Description, UsageHint: item.UsageHint})
	}
	return result
}

func runtimeEnvironmentPolicyInput(input generated.RuntimeEnvironmentPolicyInput) *controlplanev1.RuntimeEnvironmentPolicyInput {
	result := &controlplanev1.RuntimeEnvironmentPolicyInput{Resources: &controlplanev1.RuntimeResourcePolicy{
		CpuRequestMilli: input.Resources.CpuRequestMilli, CpuLimitMilli: input.Resources.CpuLimitMilli,
		MemoryRequestMib: input.Resources.MemoryRequestMib, MemoryLimitMib: input.Resources.MemoryLimitMib,
		EphemeralStorageRequestMib: input.Resources.EphemeralStorageRequestMib,
		EphemeralStorageLimitMib:   input.Resources.EphemeralStorageLimitMib,
	}, KubernetesAccess: runtimeKubernetesAccessKind(string(input.KubernetesAccess))}
	for _, volume := range input.Volumes {
		result.Volumes = append(result.Volumes, &controlplanev1.RuntimeVolumeInput{
			Name: volume.Name, Kind: runtimeVolumeKind(string(volume.Kind)), SizeMib: volume.SizeMib,
		})
	}
	for _, destination := range input.NetworkDestinations {
		result.NetworkDestinations = append(result.NetworkDestinations, runtimeNetworkDestination(string(destination)))
	}
	return result
}

func runtimeVolumeKind(value string) controlplanev1.RuntimeVolumeKind {
	switch value {
	case "EPHEMERAL_DISK":
		return controlplanev1.RuntimeVolumeKind_RUNTIME_VOLUME_KIND_EPHEMERAL_DISK
	case "EPHEMERAL_MEMORY":
		return controlplanev1.RuntimeVolumeKind_RUNTIME_VOLUME_KIND_EPHEMERAL_MEMORY
	default:
		return controlplanev1.RuntimeVolumeKind_RUNTIME_VOLUME_KIND_UNSPECIFIED
	}
}

func runtimeNetworkDestination(value string) controlplanev1.RuntimeNetworkDestination {
	switch value {
	case "DNS":
		return controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_DNS
	case "RUNTIME_CALLBACK":
		return controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_RUNTIME_CALLBACK
	case "PROVIDER_PROXY":
		return controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_PROVIDER_PROXY
	case "KUBERNETES_API":
		return controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_KUBERNETES_API
	default:
		return controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_UNSPECIFIED
	}
}

func runtimeKubernetesAccessKind(value string) controlplanev1.RuntimeKubernetesAccessKind {
	switch value {
	case "NONE":
		return controlplanev1.RuntimeKubernetesAccessKind_RUNTIME_KUBERNETES_ACCESS_KIND_NONE
	case "READ_OWN_EXECUTION":
		return controlplanev1.RuntimeKubernetesAccessKind_RUNTIME_KUBERNETES_ACCESS_KIND_READ_OWN_EXECUTION
	default:
		return controlplanev1.RuntimeKubernetesAccessKind_RUNTIME_KUBERNETES_ACCESS_KIND_UNSPECIFIED
	}
}
