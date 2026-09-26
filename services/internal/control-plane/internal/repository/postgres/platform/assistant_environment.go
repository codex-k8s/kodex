package platform

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

var assistantEnvironmentTextFields = []string{"name", "description", "imageArtifactRef"}
var assistantEnvironmentEditableFields = []string{"name", "description", "imageArtifactRef", "publicValues", "secretBindings", "tools", "policy"}
var assistantEnvironmentProposalFields = []string{
	"name", "description", "imageArtifactRef", "publicValues", "publicValueUpdates", "publicValueRemovals",
	"secretBindings", "tools", "policy",
}

func (repository *Repository) readAssistantEnvironmentSnapshot(ctx context.Context, tx pgx.Tx, actorScope scope,
	projectRef, environmentRef string,
) (map[string]any, int64, error) {
	projection, err := repository.resolveAssistantContext(ctx, tx, actorScope,
		entity.AssistantContextDescriptor{EntityKind: "ENVIRONMENT", EntityRef: environmentRef}, projectRef)
	if err != nil {
		return nil, 0, err
	}
	if !contains(projection.AllowedOperations, "PREPARE_RUNTIME_ENVIRONMENT_REVISION") {
		return nil, 0, errs.ErrForbidden
	}
	environment, err := repository.getRuntimeEnvironmentTx(ctx, tx, actorScope, environmentRef)
	if err != nil {
		return nil, 0, err
	}
	if environment.ProjectRef != projectRef || environment.State == "DELETED" ||
		environment.CurrentVersion.Ref == "" || environment.CurrentVersion.Revision < 1 {
		return nil, 0, errs.ErrNotFound
	}
	bindings := make([]entity.RuntimeSecretBinding, 0, len(environment.CurrentVersion.SecretDescriptors))
	for _, descriptor := range environment.CurrentVersion.SecretDescriptors {
		bindings = append(bindings, entity.RuntimeSecretBinding{
			Name: descriptor.Name, SecretRef: descriptor.SecretRef, Revision: descriptor.Revision,
		})
	}
	specification := entity.RuntimeEnvironmentDraftSpecification{
		Name: environment.Name, Description: environment.Description,
		ImageArtifactRef: environment.CurrentVersion.Image.ArtifactRef,
		Values:           environment.CurrentVersion.Values, SecretBindings: bindings,
		Tools: environment.CurrentVersion.Tools, Policy: environment.CurrentVersion.Policy,
	}
	raw, err := json.Marshal(specification)
	if err != nil {
		return nil, 0, errs.ErrUnavailable
	}
	var safeSpecification map[string]any
	if json.Unmarshal(raw, &safeSpecification) != nil {
		return nil, 0, errs.ErrUnavailable
	}
	return map[string]any{
		"environmentRef": environment.Ref, "projectRef": environment.ProjectRef,
		"name": environment.Name, "description": environment.Description,
		"imageArtifactRef": environment.CurrentVersion.Image.ArtifactRef,
		"versionRef":       environment.CurrentVersion.Ref, "versionDigest": environment.CurrentVersion.Digest,
		"specification": safeSpecification,
		"policyInput":   assistantEnvironmentPolicyInput(environment.CurrentVersion.Policy),
	}, environment.Version, nil
}

func assistantEnvironmentPolicyInput(policy runtimecontract.RuntimeEnvironmentPolicy) map[string]any {
	resources := policy.Resources
	volumes := make([]map[string]any, 0, len(policy.Volumes))
	for _, volume := range policy.Volumes {
		volumes = append(volumes, map[string]any{"name": volume.Name, "kind": volume.Kind, "sizeMib": volume.SizeMiB})
	}
	destinations := []string{runtimecontract.RuntimeEgressDNS, runtimecontract.RuntimeEgressProviderProxy, runtimecontract.RuntimeEgressRuntimeCallback}
	if policy.KubernetesAccess.Kind == runtimecontract.RuntimeKubernetesAccessReadOwnExecution {
		destinations = append(destinations, runtimecontract.RuntimeEgressKubernetesAPI)
	}
	return map[string]any{
		"resources": map[string]any{
			"cpuRequestMilli": resources.CPURequestMilli, "cpuLimitMilli": resources.CPULimitMilli,
			"memoryRequestMib": resources.MemoryRequestMiB, "memoryLimitMib": resources.MemoryLimitMiB,
			"ephemeralStorageRequestMib": resources.EphemeralStorageRequestMiB,
			"ephemeralStorageLimitMib":   resources.EphemeralStorageLimitMiB,
		},
		"volumes": volumes, "networkDestinations": destinations,
		"kubernetesAccess": policy.KubernetesAccess.Kind,
	}
}

func (repository *Repository) hydrateAssistantEnvironmentOperation(ctx context.Context, tx pgx.Tx, actorScope scope,
	projectRef string, operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	if projectRef == "" || !onlyAssistantFields(operation.Parameters,
		append([]string{"environmentRef"}, assistantEnvironmentProposalFields...)...) {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	ref := assistantString(operation.Parameters, "environmentRef")
	if ref == "" {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	before, version, err := repository.readAssistantEnvironmentSnapshot(ctx, tx, actorScope, projectRef, ref)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	return hydrateAssistantEnvironmentFields(before, version, operation)
}

func hydrateAssistantEnvironmentFields(before map[string]any, version int64,
	operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	after := map[string]any{
		"environmentRef": before["environmentRef"], "projectRef": before["projectRef"],
		"name": before["name"], "description": before["description"],
		"imageArtifactRef": before["imageArtifactRef"],
	}
	changed := false
	for _, field := range assistantEnvironmentTextFields {
		value, supplied := operation.Parameters[field]
		if !supplied {
			continue
		}
		text, ok := value.(string)
		if !ok {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
		text = strings.TrimSpace(text)
		changed = changed || after[field] != text
		after[field] = text
	}
	_, valuesSupplied := operation.Parameters["publicValues"]
	_, updatesSupplied := operation.Parameters["publicValueUpdates"]
	_, removalsSupplied := operation.Parameters["publicValueRemovals"]
	if valuesSupplied && (updatesSupplied || removalsSupplied) {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	var effectiveValues []entity.RuntimeEnvironmentValue
	valuesChanged := valuesSupplied || updatesSupplied || removalsSupplied
	if valuesChanged {
		var valid bool
		if valuesSupplied {
			effectiveValues, valid = assistantEnvironmentPublicValues(operation.Parameters)
		} else {
			effectiveValues, valid = assistantEnvironmentPatchedPublicValues(before["specification"], operation.Parameters)
		}
		if !valid {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
		after["publicValues"] = assistantEnvironmentPublicValuesInput(effectiveValues)
		changed = changed || !assistantEnvironmentValuesMatch(before["specification"], effectiveValues)
	}
	if _, bindingsSupplied := operation.Parameters["secretBindings"]; bindingsSupplied {
		values := effectiveValues
		if !valuesChanged {
			var valid bool
			values, valid = assistantEnvironmentValuesForUpdate(before["specification"], operation.Parameters)
			if !valid {
				return entity.AssistantPlanOperation{}, errs.ErrInvalid
			}
		}
		bindings, valid := assistantEnvironmentSecretBindings(operation.Parameters, values)
		if !valid {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
		after["secretBindings"] = operation.Parameters["secretBindings"]
		changed = changed || !assistantEnvironmentBindingsMatch(before["specification"], bindings)
	}
	if _, toolsSupplied := operation.Parameters["tools"]; toolsSupplied {
		tools, valid := assistantEnvironmentTools(operation.Parameters)
		if !valid {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
		after["tools"] = operation.Parameters["tools"]
		changed = changed || !assistantEnvironmentToolsMatch(before["specification"], tools)
	}
	if _, policySupplied := operation.Parameters["policy"]; policySupplied {
		policy, valid := assistantEnvironmentPolicy(operation.Parameters)
		if !valid {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
		after["policy"] = operation.Parameters["policy"]
		changed = changed || !assistantEnvironmentPolicyMatch(before["specification"], policy)
	}
	if valuesChanged {
		if _, bindingsSupplied := operation.Parameters["secretBindings"]; !bindingsSupplied &&
			!assistantEnvironmentSnapshotBindingsCompatible(before["specification"], effectiveValues) {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
	}
	if !changed || assistantString(after, "name") == "" || len(assistantString(after, "name")) > 120 ||
		len(assistantString(after, "description")) > 1000 {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	imageRef := assistantString(after, "imageArtifactRef")
	if imageRef != "" && (!strings.HasPrefix(imageRef, "imgart_") || len(imageRef) > 96) {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	operation.Action = "UPDATE"
	operation.Target = entity.AssistantPlanTarget{Kind: "ENVIRONMENT", Ref: assistantString(before, "environmentRef"),
		Name: assistantString(before, "name"), Version: &version}
	operation.Before = cloneAssistantFields(before)
	operation.After = cloneAssistantFields(after)
	operation.Parameters = after
	operation.ExpectedVersion = &version
	operation.Selected = true
	operation.Input = nil
	return operation, nil
}

func rehydrateEditedAssistantEnvironment(original, edited entity.AssistantPlanOperation) (entity.AssistantPlanOperation, error) {
	if original.Type != "PREPARE_RUNTIME_ENVIRONMENT_REVISION" || original.Key != edited.Key ||
		original.Target.Kind != "ENVIRONMENT" || original.Target.Ref == "" ||
		original.ExpectedVersion == nil || *original.ExpectedVersion < 1 || edited.Parameters == nil ||
		!onlyAssistantFields(edited.Parameters, append([]string{"environmentRef", "projectRef"}, assistantEnvironmentEditableFields...)...) ||
		assistantString(edited.Parameters, "environmentRef") != original.Target.Ref ||
		assistantString(edited.Parameters, "projectRef") != assistantString(original.Before, "projectRef") {
		return entity.AssistantPlanOperation{}, errs.ErrForbidden
	}
	parameters := map[string]any{"environmentRef": original.Target.Ref}
	for _, field := range assistantEnvironmentEditableFields {
		if value, supplied := edited.Parameters[field]; supplied {
			parameters[field] = value
		}
	}
	edited.Parameters = parameters
	selected := edited.Selected
	hydrated, err := hydrateAssistantEnvironmentFields(original.Before, *original.ExpectedVersion, edited)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	if !reflect.DeepEqual(hydrated.Before, original.Before) {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	hydrated.Selected = selected
	return hydrated, nil
}

func (repository *Repository) assistantEnvironmentSnapshotMatches(ctx context.Context, tx pgx.Tx, actorScope scope,
	projectRef string, operation entity.AssistantPlanOperation,
) (bool, error) {
	before, version, err := repository.readAssistantEnvironmentSnapshot(ctx, tx, actorScope, projectRef, operation.Target.Ref)
	if err != nil {
		return false, err
	}
	return operation.ExpectedVersion != nil && *operation.ExpectedVersion == version &&
		operation.Target.Version != nil && *operation.Target.Version == version &&
		operation.Target.Name == assistantString(before, "name") &&
		assistantEnvironmentSnapshotIdentityMatches(operation.Before, before) &&
		reflect.DeepEqual(operation.Parameters, operation.After) &&
		assistantString(operation.Parameters, "environmentRef") == operation.Target.Ref &&
		assistantString(operation.Parameters, "projectRef") == projectRef, nil
}

func assistantEnvironmentSnapshotIdentityMatches(stored, current map[string]any) bool {
	if !onlyAssistantFields(stored, "environmentRef", "projectRef", "name", "description", "imageArtifactRef",
		"versionRef", "versionDigest", "specification", "policyInput") ||
		!onlyAssistantFields(current, "environmentRef", "projectRef", "name", "description", "imageArtifactRef",
			"versionRef", "versionDigest", "specification", "policyInput") {
		return false
	}
	for _, field := range []string{"environmentRef", "projectRef", "name", "description", "imageArtifactRef", "versionRef", "versionDigest"} {
		if assistantString(stored, field) != assistantString(current, field) {
			return false
		}
	}
	return assistantString(current, "versionRef") != "" && assistantString(current, "versionDigest") != ""
}

func assistantEnvironmentRevisionCommand(operation entity.AssistantPlanOperation) (command.Command, error) {
	input := operation.Input
	if !onlyAssistantFields(input, "environmentRef", "projectRef", "name", "description", "imageArtifactRef", "publicValues", "secretBindings", "tools", "policy", "expectedVersion") ||
		!hasAssistantFields(input, "environmentRef", "projectRef", "name", "description", "imageArtifactRef", "expectedVersion") ||
		assistantString(input, "environmentRef") != assistantString(operation.Before, "environmentRef") ||
		assistantString(input, "projectRef") != assistantString(operation.Before, "projectRef") {
		return command.Command{}, errs.ErrInvalid
	}
	version, ok := assistantInt64(input, "expectedVersion")
	if !ok || version < 1 || assistantString(input, "name") == "" ||
		len(assistantString(input, "name")) > 120 || len(assistantString(input, "description")) > 1000 {
		return command.Command{}, errs.ErrInvalid
	}
	imageRef := assistantString(input, "imageArtifactRef")
	if imageRef != "" && (!strings.HasPrefix(imageRef, "imgart_") || len(imageRef) > 96) {
		return command.Command{}, errs.ErrInvalid
	}
	raw, err := json.Marshal(operation.Before["specification"])
	if err != nil {
		return command.Command{}, errs.ErrInvalid
	}
	var specification entity.RuntimeEnvironmentDraftSpecification
	if json.Unmarshal(raw, &specification) != nil {
		return command.Command{}, errs.ErrInvalid
	}
	specification.Name = assistantString(input, "name")
	specification.Description = assistantString(input, "description")
	specification.ImageArtifactRef = imageRef
	if _, supplied := input["publicValues"]; supplied {
		values, valid := assistantEnvironmentPublicValues(input)
		if !valid {
			return command.Command{}, errs.ErrInvalid
		}
		specification.Values = values
	}
	if _, supplied := input["secretBindings"]; supplied {
		bindings, valid := assistantEnvironmentSecretBindings(input, specification.Values)
		if !valid {
			return command.Command{}, errs.ErrInvalid
		}
		specification.SecretBindings = bindings
	}
	if _, supplied := input["tools"]; supplied {
		tools, valid := assistantEnvironmentTools(input)
		if !valid {
			return command.Command{}, errs.ErrInvalid
		}
		specification.Tools = tools
	}
	if _, supplied := input["policy"]; supplied {
		policy, valid := assistantEnvironmentPolicy(input)
		if !valid {
			return command.Command{}, errs.ErrInvalid
		}
		specification.Policy = policy
	}
	if !assistantEnvironmentBindingsCompatible(specification.Values, specification.SecretBindings) {
		return command.Command{}, errs.ErrInvalid
	}
	return command.Command{Kind: command.CreateRuntimeEnvironmentDraft, Payload: command.RuntimeEnvironmentDraftInput{
		ProjectRef: assistantString(input, "projectRef"), EnvironmentRef: assistantString(input, "environmentRef"),
		ExpectedEnvironmentVersion: version, Specification: specification,
	}}, nil
}
