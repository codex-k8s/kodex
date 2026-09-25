package platform

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

var assistantEnvironmentTextFields = []string{"name", "description", "imageArtifactRef"}
var assistantEnvironmentEditableFields = []string{"name", "description", "imageArtifactRef", "publicValues", "secretBindings"}

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
	}, environment.Version, nil
}

func (repository *Repository) hydrateAssistantEnvironmentOperation(ctx context.Context, tx pgx.Tx, actorScope scope,
	projectRef string, operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	if projectRef == "" || !onlyAssistantFields(operation.Parameters,
		append([]string{"environmentRef"}, assistantEnvironmentEditableFields...)...) {
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
	if _, valuesSupplied := operation.Parameters["publicValues"]; valuesSupplied {
		values, valid := assistantEnvironmentPublicValues(operation.Parameters)
		if !valid {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
		after["publicValues"] = operation.Parameters["publicValues"]
		changed = changed || !assistantEnvironmentValuesMatch(before["specification"], values)
	}
	if _, bindingsSupplied := operation.Parameters["secretBindings"]; bindingsSupplied {
		values, valid := assistantEnvironmentValuesForUpdate(before["specification"], operation.Parameters)
		if !valid {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
		bindings, valid := assistantEnvironmentSecretBindings(operation.Parameters, values)
		if !valid {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
		after["secretBindings"] = operation.Parameters["secretBindings"]
		changed = changed || !assistantEnvironmentBindingsMatch(before["specification"], bindings)
	}
	if _, valuesSupplied := operation.Parameters["publicValues"]; valuesSupplied {
		if _, bindingsSupplied := operation.Parameters["secretBindings"]; !bindingsSupplied &&
			!assistantEnvironmentExistingBindingsCompatible(before["specification"], operation.Parameters) {
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
		reflect.DeepEqual(operation.Before, before) &&
		reflect.DeepEqual(operation.Parameters, operation.After) &&
		assistantString(operation.Parameters, "environmentRef") == operation.Target.Ref &&
		assistantString(operation.Parameters, "projectRef") == projectRef, nil
}

func assistantEnvironmentRevisionCommand(operation entity.AssistantPlanOperation) (command.Command, error) {
	input := operation.Input
	if !onlyAssistantFields(input, "environmentRef", "projectRef", "name", "description", "imageArtifactRef", "publicValues", "secretBindings", "expectedVersion") ||
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
	if !assistantEnvironmentBindingsCompatible(specification.Values, specification.SecretBindings) {
		return command.Command{}, errs.ErrInvalid
	}
	return command.Command{Kind: command.CreateRuntimeEnvironmentDraft, Payload: command.RuntimeEnvironmentDraftInput{
		ProjectRef: assistantString(input, "projectRef"), EnvironmentRef: assistantString(input, "environmentRef"),
		ExpectedEnvironmentVersion: version, Specification: specification,
	}}, nil
}
