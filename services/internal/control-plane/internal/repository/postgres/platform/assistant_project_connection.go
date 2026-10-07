package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

const prepareProjectAssistantConnection = "PREPARE_PROJECT_ASSISTANT_INTEGRATION_CONNECTION"

func (repository *Repository) authorizeAssistantPreparedOperation(ctx context.Context, tx pgx.Tx, current scope, operation entity.AssistantPlanOperation, input command.Command) error {
	if operation.Type != prepareProjectAssistantConnection {
		return repository.authorizeCommand(ctx, tx, current, input)
	}
	payload, valid := input.Payload.(command.ProjectAssistantConnectionInput)
	if !valid {
		return errs.ErrInvalid
	}
	return repository.authorizeProjectAssistantConnection(ctx, tx, current, payload, false)
}

//go:embed sql/project_assistant_connection__owner.sql
var queryProjectAssistantConnectionOwner string

//go:embed sql/project_assistant_connection__insert.sql
var queryProjectAssistantConnectionInsert string

//go:embed sql/project_assistant_connection__operations.sql
var queryProjectAssistantConnectionOperations string

var projectConnectionEditable = []string{"projectAssistantRef", "definitionKey", "name", "publicConfiguration"}
var projectConnectionPins = []string{"assistantScope", "scopeKind", "organizationRef", "projectRef", "assistantProfileRef", "agentVersion", "profileVersion", "definitionVersion", "definitionDigest"}

func (repository *Repository) projectAssistantConnectionOwner(ctx context.Context, tx pgx.Tx, current scope, assistantRef string) (map[string]any, error) {
	var projectRef, profileRef, name string
	var profileVersion, agentVersion int64
	err := tx.QueryRow(ctx, queryProjectAssistantConnectionOwner, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID,
		"assistant_ref": assistantRef, "authority_project": current.authorityProjectID,
	}).Scan(&projectRef, &profileRef, &profileVersion, &agentVersion, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errs.ErrNotFound
	}
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	if err := repository.requireAccess(ctx, tx, current, "organization.manage", organizationTarget(current.organizationRef)); err != nil {
		return nil, errs.ErrNotFound
	}
	return map[string]any{"projectAssistantRef": assistantRef, "assistantScope": "PROJECT", "scopeKind": "ORGANIZATION", "organizationRef": current.organizationRef,
		"projectRef": projectRef, "assistantProfileRef": profileRef, "agentVersion": agentVersion, "profileVersion": profileVersion, "assistantName": name}, nil
}

func (repository *Repository) hydrateProjectAssistantConnection(ctx context.Context, tx pgx.Tx, current scope, operation entity.AssistantPlanOperation) (entity.AssistantPlanOperation, error) {
	if !onlyAssistantFields(operation.Parameters, projectConnectionEditable...) || !hasAssistantFields(operation.Parameters, projectConnectionEditable...) {
		return operation, errs.ErrInvalid
	}
	owner, err := repository.projectAssistantConnectionOwner(ctx, tx, current, assistantString(operation.Parameters, "projectAssistantRef"))
	if err != nil {
		return operation, err
	}
	definitionKey := assistantString(operation.Parameters, "definitionKey")
	definition, exists := repository.integrationDefinitions[definitionKey]
	configuration, valid := assistantObjectValue(operation.Parameters, "publicConfiguration")
	values, validValues := integrationStringConfiguration(configuration)
	name := strings.TrimSpace(assistantString(operation.Parameters, "name"))
	if !exists || !valid || !validValues || definition.ValidateConnectionBootstrapConfiguration(values) != nil || name == "" || len(name) > 160 {
		return operation, errs.ErrInvalid
	}
	var version, digest string
	if err := tx.QueryRow(ctx, queryConfigurationChangeconnectionSelectIntegrationDefinitionsStableKeyEnabled, definitionKey).Scan(&version, &digest); errors.Is(err, pgx.ErrNoRows) {
		return operation, errs.ErrNotFound
	} else if err != nil {
		return operation, errs.ErrUnavailable
	}
	if version != definition.Metadata.Version || digest != definition.Digest {
		return operation, errs.ErrConflict
	}
	params := cloneAssistantFields(owner)
	delete(params, "assistantName")
	params["definitionKey"], params["name"], params["publicConfiguration"] = definitionKey, name, configuration
	params["definitionVersion"], params["definitionDigest"] = version, digest
	operation.Parameters, operation.Before, operation.After = params, cloneAssistantFields(owner), cloneAssistantFields(params)
	operation.Before["definitionVersion"], operation.Before["definitionDigest"] = version, digest
	agentVersion, _ := assistantInt64(owner, "agentVersion")
	operation.ExpectedVersion = &agentVersion
	operation.Target = entity.AssistantPlanTarget{Kind: "PROJECT_ASSISTANT", Ref: assistantString(params, "projectAssistantRef"), Name: assistantString(owner, "assistantName"), Version: &agentVersion}
	operation.Action, operation.Selected = "CREATE", true
	return operation, nil
}

func projectAssistantConnectionCommand(operation entity.AssistantPlanOperation) (command.Command, error) {
	allowed := append(append([]string{}, projectConnectionEditable...), projectConnectionPins...)
	allowed = append(allowed, "expectedVersion")
	if !onlyAssistantFields(operation.Input, allowed...) || !hasAssistantFields(operation.Input, allowed...) ||
		assistantString(operation.Input, "scopeKind") != "ORGANIZATION" || assistantString(operation.Input, "assistantScope") != "PROJECT" {
		return command.Command{}, errs.ErrInvalid
	}
	version, validVersion := assistantInt64(operation.Input, "expectedVersion")
	agentVersion, validAgentVersion := assistantInt64(operation.Input, "agentVersion")
	profileVersion, validProfileVersion := assistantInt64(operation.Input, "profileVersion")
	configuration, validConfig := assistantObjectValue(operation.Input, "publicConfiguration")
	_, validConfiguration := integrationStringConfiguration(configuration)
	if !validVersion || version < 1 || !validAgentVersion || agentVersion != version || !validProfileVersion || profileVersion < 1 || !validConfig || !validConfiguration ||
		!validCapabilityKey(assistantString(operation.Input, "definitionKey")) || assistantString(operation.Input, "name") == "" || len(assistantString(operation.Input, "name")) > 160 {
		return command.Command{}, errs.ErrInvalid
	}
	for _, field := range []string{"projectAssistantRef", "organizationRef", "projectRef", "assistantProfileRef", "definitionVersion", "definitionDigest"} {
		if assistantString(operation.Input, field) == "" {
			return command.Command{}, errs.ErrInvalid
		}
	}
	return command.Command{Kind: command.CreateProjectAssistantIntegrationConnection, Payload: command.ProjectAssistantConnectionInput{
		AssistantRef: assistantString(operation.Input, "projectAssistantRef"), OrganizationRef: assistantString(operation.Input, "organizationRef"),
		ProjectRef: assistantString(operation.Input, "projectRef"), ProfileRef: assistantString(operation.Input, "assistantProfileRef"),
		AgentVersion: version, ProfileVersion: profileVersion,
		DefinitionVersion: assistantString(operation.Input, "definitionVersion"), DefinitionDigest: assistantString(operation.Input, "definitionDigest"),
		Connection: command.ConnectionInput{DefinitionKey: assistantString(operation.Input, "definitionKey"), Name: assistantString(operation.Input, "name"), PublicConfiguration: configuration},
	}}, nil
}

func mustAssistantInt64(fields map[string]any, field string) int64 {
	value, _ := assistantInt64(fields, field)
	return value
}

func (repository *Repository) projectAssistantConnectionSnapshotMatches(ctx context.Context, tx pgx.Tx, current scope, original entity.AssistantPlanOperation) (bool, error) {
	request := original
	request.Parameters = map[string]any{}
	for _, field := range projectConnectionEditable {
		request.Parameters[field] = original.Parameters[field]
	}
	fresh, err := repository.hydrateProjectAssistantConnection(ctx, tx, current, request)
	if err != nil {
		return false, err
	}
	return assistantJSONEqual(fresh.Before, original.Before) && assistantJSONEqual(fresh.After, original.After) && assistantJSONEqual(fresh.Target, original.Target), nil
}

func (repository *Repository) rehydrateEditedProjectAssistantConnection(ctx context.Context, tx pgx.Tx, current scope, original, edited entity.AssistantPlanOperation, refreshStale bool) (entity.AssistantPlanOperation, error) {
	allowed := append(append([]string{}, projectConnectionEditable...), projectConnectionPins...)
	if !onlyAssistantFields(edited.Parameters, allowed...) || !hasAssistantFields(edited.Parameters, allowed...) {
		return edited, errs.ErrInvalid
	}
	for _, field := range append(append([]string{}, projectConnectionPins...), "projectAssistantRef") {
		if !assistantJSONEqual(edited.Parameters[field], original.Parameters[field]) {
			return edited, errs.ErrForbidden
		}
	}
	if !refreshStale {
		matching, err := repository.projectAssistantConnectionSnapshotMatches(ctx, tx, current, original)
		if err != nil || !matching {
			return edited, errs.ErrConflict
		}
	}
	request := edited
	request.Parameters = map[string]any{}
	for _, field := range projectConnectionEditable {
		request.Parameters[field] = edited.Parameters[field]
	}
	fresh, err := repository.hydrateProjectAssistantConnection(ctx, tx, current, request)
	if err != nil {
		return edited, err
	}
	for _, field := range []string{"projectAssistantRef", "assistantScope", "scopeKind", "organizationRef", "projectRef", "assistantProfileRef"} {
		if !assistantJSONEqual(fresh.Parameters[field], original.Parameters[field]) {
			return edited, errs.ErrForbidden
		}
	}
	fresh.Selected = edited.Selected
	return fresh, nil
}

func (repository *Repository) authorizeProjectAssistantConnection(ctx context.Context, tx pgx.Tx, current scope, payload command.ProjectAssistantConnectionInput, effect bool) error {
	if effect && current.authorityProjectID != "" {
		return errs.ErrNotFound
	}
	owner, err := repository.projectAssistantConnectionOwner(ctx, tx, current, payload.AssistantRef)
	if err != nil {
		return err
	}
	if payload.OrganizationRef != current.organizationRef || payload.ProjectRef != assistantString(owner, "projectRef") || payload.ProfileRef != assistantString(owner, "assistantProfileRef") {
		return errs.ErrNotFound
	}
	return nil
}

func (repository *Repository) createProjectAssistantConnection(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, valid := input.Payload.(command.ProjectAssistantConnectionInput)
	if !valid {
		return commandOutcome{}, errs.ErrInvalid
	}
	if err := repository.authorizeProjectAssistantConnection(ctx, tx, current, payload, true); err != nil {
		return commandOutcome{}, err
	}
	owner, err := repository.projectAssistantConnectionOwner(ctx, tx, current, payload.AssistantRef)
	if err != nil {
		return commandOutcome{}, err
	}
	if payload.AgentVersion != mustAssistantInt64(owner, "agentVersion") || payload.ProfileVersion != mustAssistantInt64(owner, "profileVersion") {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	definition, exists := repository.integrationDefinitions[payload.Connection.DefinitionKey]
	if !exists || payload.DefinitionVersion != definition.Metadata.Version || payload.DefinitionDigest != definition.Digest {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	canonical := input
	canonical.Kind, canonical.Payload = command.CreateConnection, payload.Connection
	result, err := repository.changeConnection(ctx, tx, current, canonical)
	if err != nil {
		return result, err
	}
	if result.result.Connection == nil {
		return result, errs.ErrUnavailable
	}
	inserted, err := tx.Exec(ctx, queryProjectAssistantConnectionInsert, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID, "connection_ref": result.resourceRef,
		"project_ref": payload.ProjectRef, "profile_ref": payload.ProfileRef, "assistant_ref": payload.AssistantRef,
		"profile_version": payload.ProfileVersion, "agent_version": payload.AgentVersion,
	})
	if err != nil || inserted.RowsAffected() != 1 {
		return commandOutcome{}, errs.ErrUnavailable
	}
	return result, nil
}
