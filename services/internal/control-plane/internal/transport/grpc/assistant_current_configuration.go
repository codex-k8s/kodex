package grpc

import (
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func castAssistantCurrentConfiguration(value entity.AssistantCurrentConfiguration) (*controlplanev1.AssistantCurrentConfiguration, error) {
	environment := value.Environment
	result := &controlplanev1.AssistantCurrentConfiguration{AgentVersion: value.AgentVersion,
		Configuration: castAgentRuntimeConfiguration(value.Configuration), PublishedOverlay: castConfigOverlay(&value.PublishedOverlay),
		EnvironmentBinding: &controlplanev1.AgentRuntimeEnvironmentBinding{Ref: value.EnvironmentBinding.Ref, Version: value.EnvironmentBinding.Version,
			AgentRef: value.EnvironmentBinding.AgentRef, EnvironmentRef: value.EnvironmentBinding.EnvironmentRef, VersionRef: value.EnvironmentBinding.VersionRef, Digest: value.EnvironmentBinding.Digest},
		EnvironmentRef: value.EnvironmentRef, EnvironmentVersion: value.EnvironmentVersion,
		EnvironmentVersionRef: environment.Ref, EnvironmentRevision: environment.Revision, EnvironmentDigest: environment.Digest,
		Image: &controlplanev1.RuntimeEnvironmentImage{ArtifactRef: environment.Image.ArtifactRef, RecipeRef: environment.Image.RecipeRef,
			RecipeGeneration: environment.Image.RecipeGeneration, Reference: environment.Image.Reference, Digest: environment.Image.Digest},
		Policy: castRuntimeEnvironmentPolicy(environment.Policy), InstructionTemplateRef: value.InstructionTemplateRef,
		InstructionTemplateDigest: value.InstructionTemplateDigest, PublishedInstructions: value.PublishedInstructions,
		SystemCoreRevision: value.SystemCoreRevision, SystemCoreInstructions: value.SystemCoreInstructions,
		OwnerInstructions: value.OwnerInstructions, OwnerInstructionsRevision: value.OwnerInstructionsRevision}
	for _, tool := range environment.Tools {
		result.Tools = append(result.Tools, &controlplanev1.RuntimeEnvironmentTool{Name: tool.Name, Command: tool.Command, Description: tool.Description, UsageHint: tool.UsageHint})
	}
	for _, item := range environment.Values {
		result.Values = append(result.Values, &controlplanev1.RuntimeEnvironmentValue{Name: item.Name, Value: item.Value})
	}
	for _, item := range value.SecretBindings {
		result.SecretBindings = append(result.SecretBindings, &controlplanev1.RuntimeSecretBinding{Name: item.Name, SecretRef: item.SecretRef, Revision: item.Revision})
	}
	for _, item := range value.TemplateVariables {
		reason, err := castTemplateAvailabilityReason(item.Reason, item.Available)
		if err != nil {
			return nil, err
		}
		variable := &controlplanev1.TemplateVariable{Name: item.Name, ValueType: item.Type, Description: item.Description,
			Example: item.Example, Source: item.Source, Collection: item.Collection, ItemValueType: item.ItemType,
			RangeExample: item.RangeExample, Available: item.Available, Reason: reason}
		for _, field := range item.ItemFields {
			variable.ItemFields = append(variable.ItemFields, &controlplanev1.TemplateVariableField{Name: field.Name, ValueType: field.Type, Description: field.Description})
		}
		result.TemplateVariables = append(result.TemplateVariables, variable)
	}
	return result, nil
}
