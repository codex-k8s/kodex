package httptransport

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/proto"
)

func (server *Server) GetRuntimeEnvironmentDraft(w http.ResponseWriter, r *http.Request, ref generated.RuntimeEnvironmentDraftRef) {
	response, err := server.control.Query.GetRuntimeEnvironmentDraft(r.Context(), &controlplanev1.GetRuntimeEnvironmentDraftRequest{DraftRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeEnvironmentDraft(w, http.StatusOK, response.GetDraft())
}

func (server *Server) CreateRuntimeEnvironmentDraft(w http.ResponseWriter, r *http.Request, projectRef generated.ProjectRef, p generated.CreateRuntimeEnvironmentDraftParams) {
	r, ok := withProjectReference(w, r, projectRef)
	if !ok {
		return
	}
	body, ok := decodeJSON[generated.RuntimeEnvironmentDraftCreateInput](w, r)
	if !ok {
		return
	}
	expected := int64(0)
	if body.ExpectedEnvironmentVersion != nil {
		expected = *body.ExpectedEnvironmentVersion
	}
	if (body.EnvironmentRef == nil) != (body.ExpectedEnvironmentVersion == nil) || expected < 0 || expected > maximumSafeJSONInteger ||
		body.EnvironmentRef != nil && (stringValue(body.EnvironmentRef) == "" || expected < 1) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	specification, ok := environmentDraftSpecificationInput(w, body.Specification)
	if !ok {
		return
	}
	mutation, ok := requireMutation(w, p.IdempotencyKey, "")
	if !ok {
		return
	}
	response, err := server.control.Command.CreateRuntimeEnvironmentDraft(r.Context(), &controlplanev1.CreateRuntimeEnvironmentDraftRequest{
		Mutation: mutation, ProjectRef: projectRef, EnvironmentRef: stringValue(body.EnvironmentRef), ExpectedEnvironmentVersion: expected, Specification: specification,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeEnvironmentDraft(w, http.StatusCreated, response.GetDraft())
}

func (server *Server) SaveRuntimeEnvironmentDraft(w http.ResponseWriter, r *http.Request, ref generated.RuntimeEnvironmentDraftRef, p generated.SaveRuntimeEnvironmentDraftParams) {
	body, ok := decodeJSON[generated.RuntimeEnvironmentDraftSpecification](w, r)
	if !ok {
		return
	}
	specification, ok := environmentDraftSpecificationInput(w, body)
	if !ok {
		return
	}
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.SaveRuntimeEnvironmentDraft(r.Context(), &controlplanev1.SaveRuntimeEnvironmentDraftRequest{Mutation: mutation, DraftRef: ref, Specification: specification})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeEnvironmentDraft(w, http.StatusOK, response.GetDraft())
}

func (server *Server) ValidateRuntimeEnvironmentDraft(w http.ResponseWriter, r *http.Request, ref generated.RuntimeEnvironmentDraftRef, p generated.ValidateRuntimeEnvironmentDraftParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.ValidateRuntimeEnvironmentDraft(r.Context(), &controlplanev1.ValidateRuntimeEnvironmentDraftRequest{Mutation: mutation, DraftRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeEnvironmentDraft(w, http.StatusOK, response.GetDraft())
}

func (server *Server) PublishRuntimeEnvironmentDraft(w http.ResponseWriter, r *http.Request, ref generated.RuntimeEnvironmentDraftRef, p generated.PublishRuntimeEnvironmentDraftParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.PublishRuntimeEnvironmentDraft(r.Context(), &controlplanev1.PublishRuntimeEnvironmentDraftRequest{Mutation: mutation, DraftRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response.GetEnvironment().GetRef() == "" || response.GetEnvironment().GetVersion() < 1 || response.GetEnvironment().GetRef() != response.GetDraft().GetPublishedEnvironmentRef() || response.GetDraft().GetState() != "PUBLISHED" {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeEnvironmentDraft(w, http.StatusOK, response.GetDraft())
}

func (server *Server) DiscardRuntimeEnvironmentDraft(w http.ResponseWriter, r *http.Request, ref generated.RuntimeEnvironmentDraftRef, p generated.DiscardRuntimeEnvironmentDraftParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.DiscardRuntimeEnvironmentDraft(r.Context(), &controlplanev1.DiscardRuntimeEnvironmentDraftRequest{Mutation: mutation, DraftRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeEnvironmentDraft(w, http.StatusOK, response.GetDraft())
}

func environmentDraftSpecificationInput(w http.ResponseWriter, input generated.RuntimeEnvironmentDraftSpecification) (*controlplanev1.RuntimeEnvironmentDraftSpecification, bool) {
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > 256<<10 || len(input.Tools) > 128 || len(input.Values) > 128 || len(input.SecretBindings) > 128 {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return nil, false
	}
	bindings, ok := runtimeSecretBindings(w, input.SecretBindings)
	if !ok {
		return nil, false
	}
	result := &controlplanev1.RuntimeEnvironmentDraftSpecification{Name: input.Name, Description: input.Description, ImageArtifactRef: input.ImageArtifactRef,
		Values: runtimeEnvironmentValues(input.Values), SecretBindings: bindings, Tools: runtimeEnvironmentTools(input.Tools)}
	if input.Policy != nil {
		result.Policy = runtimeEnvironmentPolicyInput(*input.Policy)
	}
	return result, true
}

func writeEnvironmentDraft(w http.ResponseWriter, statusCode int, input *controlplanev1.RuntimeEnvironmentDraft) {
	if input == nil || input.GetSpecification() == nil || input.GetRef() == "" || input.GetProjectRef() == "" ||
		input.GetVersion() < 1 || input.GetVersion() > maximumSafeJSONInteger || input.GetExpectedEnvironmentVersion() < 0 ||
		input.GetExpectedEnvironmentVersion() > maximumSafeJSONInteger || len(input.GetDiagnostics()) > 64 {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	switch input.GetState() {
	case "DRAFT", "VALID", "INVALID", "PUBLISHED", "DISCARDED":
	default:
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	if input.GetValidationDigest() != "" && !validManagedDigest(input.GetValidationDigest()) ||
		(input.GetState() == "VALID" || input.GetState() == "PUBLISHED") && !validManagedDigest(input.GetValidationDigest()) ||
		input.GetState() == "PUBLISHED" && input.GetPublishedEnvironmentRef() == "" ||
		(input.GetEnvironmentRef() == "") != (input.GetExpectedEnvironmentVersion() == 0) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	spec := input.GetSpecification()
	if len(spec.GetValues()) > 128 || len(spec.GetSecretBindings()) > 128 || len(spec.GetTools()) > 128 {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result := generated.RuntimeEnvironmentDraft{
		Ref: input.GetRef(), Version: input.GetVersion(), ProjectRef: input.GetProjectRef(), ExpectedEnvironmentVersion: input.GetExpectedEnvironmentVersion(),
		State: generated.RuntimeEnvironmentDraftState(input.GetState()), Diagnostics: append([]string{}, input.GetDiagnostics()...),
		Specification: generated.RuntimeEnvironmentDraftSpecification{Name: spec.GetName(), Description: spec.GetDescription(), ImageArtifactRef: spec.GetImageArtifactRef(),
			Values: []generated.RuntimeEnvironmentValue{}, SecretBindings: []generated.RuntimeSecretBinding{}, Tools: []generated.RuntimeEnvironmentTool{}},
	}
	for _, value := range spec.GetValues() {
		if value == nil {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		result.Specification.Values = append(result.Specification.Values, generated.RuntimeEnvironmentValue{Name: value.GetName(), Value: value.GetValue()})
	}
	for _, binding := range spec.GetSecretBindings() {
		if binding == nil || binding.GetRevision() < 0 || binding.GetRevision() > maximumSafeJSONInteger {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		revision := binding.GetRevision()
		result.Specification.SecretBindings = append(result.Specification.SecretBindings, generated.RuntimeSecretBinding{Name: binding.GetName(), SecretRef: binding.GetSecretRef(), Revision: &revision})
	}
	for _, tool := range spec.GetTools() {
		if tool == nil {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		result.Specification.Tools = append(result.Specification.Tools, generated.RuntimeEnvironmentTool{Name: tool.GetName(), Command: tool.GetCommand(), Description: tool.GetDescription(), UsageHint: tool.GetUsageHint()})
	}
	policy, ok := environmentDraftPolicyView(spec.GetPolicy())
	if !ok {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result.Specification.Policy = policy
	if value := input.GetEnvironmentRef(); value != "" {
		result.EnvironmentRef = &value
	}
	if value := input.GetValidationDigest(); value != "" {
		result.ValidationDigest = &value
	}
	if value := input.GetPublishedEnvironmentRef(); value != "" {
		result.PublishedEnvironmentRef = &value
	}
	w.Header().Set("ETag", "\""+strconv.FormatInt(input.GetVersion(), 10)+"\"")
	writeJSON(w, statusCode, result)
}

func environmentDraftPolicyView(input *controlplanev1.RuntimeEnvironmentPolicyInput) (*generated.RuntimeEnvironmentPolicyInput, bool) {
	if input == nil {
		return nil, true
	}
	resources := input.GetResources()
	if resources == nil {
		return nil, false
	}
	// Отсутствующая policy в сохранённом черновике пока возвращается владельцем как нулевая структура.
	if proto.Equal(resources, &controlplanev1.RuntimeResourcePolicy{}) && len(input.GetVolumes()) == 0 && len(input.GetNetworkDestinations()) == 0 &&
		(input.GetKubernetesAccess() == controlplanev1.RuntimeKubernetesAccessKind_RUNTIME_KUBERNETES_ACCESS_KIND_NONE || input.GetKubernetesAccess() == controlplanev1.RuntimeKubernetesAccessKind_RUNTIME_KUBERNETES_ACCESS_KIND_UNSPECIFIED) {
		return nil, true
	}
	if resources.GetCpuRequestMilli() < 100 || resources.GetCpuRequestMilli() > 8000 || resources.GetCpuLimitMilli() < 100 || resources.GetCpuLimitMilli() > 16000 ||
		resources.GetMemoryRequestMib() < 128 || resources.GetMemoryRequestMib() > 32768 || resources.GetMemoryLimitMib() < 128 || resources.GetMemoryLimitMib() > 65536 ||
		resources.GetEphemeralStorageRequestMib() < 256 || resources.GetEphemeralStorageRequestMib() > 20480 || resources.GetEphemeralStorageLimitMib() < 256 || resources.GetEphemeralStorageLimitMib() > 102400 ||
		len(input.GetVolumes()) > 16 || len(input.GetNetworkDestinations()) < 3 || len(input.GetNetworkDestinations()) > 4 {
		return nil, false
	}
	result := &generated.RuntimeEnvironmentPolicyInput{Resources: generated.RuntimeResourcePolicy{
		CpuRequestMilli: resources.GetCpuRequestMilli(), CpuLimitMilli: resources.GetCpuLimitMilli(), MemoryRequestMib: resources.GetMemoryRequestMib(), MemoryLimitMib: resources.GetMemoryLimitMib(),
		EphemeralStorageRequestMib: resources.GetEphemeralStorageRequestMib(), EphemeralStorageLimitMib: resources.GetEphemeralStorageLimitMib(),
	}, Volumes: []generated.RuntimeVolumeInput{}, NetworkDestinations: []generated.RuntimeNetworkDestination{}}
	switch input.GetKubernetesAccess() {
	case controlplanev1.RuntimeKubernetesAccessKind_RUNTIME_KUBERNETES_ACCESS_KIND_NONE, controlplanev1.RuntimeKubernetesAccessKind_RUNTIME_KUBERNETES_ACCESS_KIND_READ_OWN_EXECUTION:
		result.KubernetesAccess = generated.RuntimeKubernetesAccessKind(strings.TrimPrefix(input.GetKubernetesAccess().String(), "RUNTIME_KUBERNETES_ACCESS_KIND_"))
	default:
		return nil, false
	}
	for _, volume := range input.GetVolumes() {
		if volume == nil || (volume.GetKind() != controlplanev1.RuntimeVolumeKind_RUNTIME_VOLUME_KIND_EPHEMERAL_DISK && volume.GetKind() != controlplanev1.RuntimeVolumeKind_RUNTIME_VOLUME_KIND_EPHEMERAL_MEMORY) {
			return nil, false
		}
		result.Volumes = append(result.Volumes, generated.RuntimeVolumeInput{Name: volume.GetName(), Kind: generated.RuntimeVolumeKind(strings.TrimPrefix(volume.GetKind().String(), "RUNTIME_VOLUME_KIND_")), SizeMib: volume.GetSizeMib()})
	}
	for _, destination := range input.GetNetworkDestinations() {
		switch destination {
		case controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_DNS, controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_RUNTIME_CALLBACK,
			controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_PROVIDER_PROXY, controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_KUBERNETES_API:
			result.NetworkDestinations = append(result.NetworkDestinations, generated.RuntimeNetworkDestination(strings.TrimPrefix(destination.String(), "RUNTIME_NETWORK_DESTINATION_")))
		default:
			return nil, false
		}
	}
	return result, true
}
