package platform

import (
	"context"
	"reflect"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	revisionservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/revision"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// План содержит только идентификаторы и дайджест. Содержимое OpenAPI остается
// в авторитетной ревизии и никогда не попадает в assistant runtime.
func (repository *Repository) hydrateAssistantIntegrationDefinitionPublication(
	ctx context.Context, tx pgx.Tx, current scope, projectRef string, operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	if projectRef != "" || !onlyAssistantFields(operation.Parameters, "configurationRef", "revisionRef") ||
		!hasAssistantFields(operation.Parameters, "configurationRef", "revisionRef") {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	configurationRef := assistantString(operation.Parameters, "configurationRef")
	revisionRef := assistantString(operation.Parameters, "revisionRef")
	if !strings.HasPrefix(configurationRef, "mcfg_") || len(configurationRef) > 96 ||
		!strings.HasPrefix(revisionRef, "mrev_") || len(revisionRef) > 96 {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	set, err := repository.resolveManagedSet(ctx, tx, current,
		command.ManagedConfigurationInput{ConfigurationRef: configurationRef}, revisionservice.KindIntegrationDefinition, false)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	if set.ProjectRef != "" || set.ManagedBy != "UI" || set.Archived {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	revision, err := repository.lockManagedRevision(ctx, tx, current, set, revisionRef)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	if revision.State != "VALID" || !validAssistantDefinitionDigest(revision.Digest) {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	currentRevisionRef := ""
	if set.CurrentRevision != nil {
		currentRevisionRef = set.CurrentRevision.Ref
	}
	version := set.Version
	operation.Action = "UPDATE"
	operation.Target = entity.AssistantPlanTarget{Kind: "INTEGRATION_DEFINITION", Ref: set.Ref, Name: set.Name, Version: &version}
	operation.Parameters = map[string]any{
		"configurationRef": set.Ref, "revisionRef": revision.Ref, "revisionDigest": revision.Digest,
	}
	operation.Before = map[string]any{
		"currentRevisionRef": currentRevisionRef, "revisionState": "VALID", "revisionDigest": revision.Digest,
	}
	operation.After = map[string]any{
		"currentRevisionRef": revision.Ref, "revisionState": "PUBLISHED", "revisionDigest": revision.Digest,
	}
	operation.ExpectedVersion = &version
	operation.Selected = true
	return operation, nil
}

func validAssistantDefinitionDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				return false
			}
		}
	}
	return true
}

func assistantIntegrationDefinitionPublicationCommand(operation entity.AssistantPlanOperation) (command.Command, error) {
	if !onlyAssistantFields(operation.Input, "configurationRef", "revisionRef", "revisionDigest", "expectedVersion") ||
		!hasAssistantFields(operation.Input, "configurationRef", "revisionRef", "revisionDigest", "expectedVersion") {
		return command.Command{}, errs.ErrInvalid
	}
	version, valid := assistantInt64(operation.Input, "expectedVersion")
	configurationRef := assistantString(operation.Input, "configurationRef")
	revisionRef := assistantString(operation.Input, "revisionRef")
	if !valid || version < 1 || operation.ExpectedVersion == nil || *operation.ExpectedVersion != version ||
		!strings.HasPrefix(configurationRef, "mcfg_") || len(configurationRef) > 96 ||
		!strings.HasPrefix(revisionRef, "mrev_") || len(revisionRef) > 96 ||
		!validAssistantDefinitionDigest(assistantString(operation.Input, "revisionDigest")) ||
		operation.Target.Kind != "INTEGRATION_DEFINITION" || operation.Target.Ref != configurationRef {
		return command.Command{}, errs.ErrInvalid
	}
	return command.Command{
		Kind: command.PublishIntegrationDefinition,
		Payload: command.ManagedConfigurationInput{
			ConfigurationRef: configurationRef, RevisionRef: revisionRef, Kind: revisionservice.KindIntegrationDefinition,
		},
		Mutation: value.Mutation{ExpectedVersion: &version},
	}, nil
}

func rehydrateEditedAssistantIntegrationDefinitionPublication(original, edited entity.AssistantPlanOperation) (entity.AssistantPlanOperation, error) {
	if original.Type != "PUBLISH_INTEGRATION_DEFINITION" || original.Key != edited.Key ||
		original.Target.Kind != "INTEGRATION_DEFINITION" || original.ExpectedVersion == nil ||
		!reflect.DeepEqual(edited.Parameters, original.Parameters) {
		return entity.AssistantPlanOperation{}, errs.ErrForbidden
	}
	original.Selected = edited.Selected
	original.Title = edited.Title
	original.Summary = edited.Summary
	return original, nil
}

func (repository *Repository) assistantIntegrationDefinitionPublicationSnapshotMatches(
	ctx context.Context, tx pgx.Tx, current scope, operation entity.AssistantPlanOperation,
) (bool, error) {
	parameters := map[string]any{
		"configurationRef": assistantString(operation.Parameters, "configurationRef"),
		"revisionRef":      assistantString(operation.Parameters, "revisionRef"),
	}
	fresh, err := repository.hydrateAssistantIntegrationDefinitionPublication(ctx, tx, current, "",
		entity.AssistantPlanOperation{Type: operation.Type, Parameters: parameters})
	if err != nil {
		return false, err
	}
	return reflect.DeepEqual(fresh.Target, operation.Target) &&
		reflect.DeepEqual(fresh.Parameters, operation.Parameters) &&
		reflect.DeepEqual(fresh.Before, operation.Before) &&
		reflect.DeepEqual(fresh.After, operation.After) &&
		reflect.DeepEqual(fresh.ExpectedVersion, operation.ExpectedVersion), nil
}
