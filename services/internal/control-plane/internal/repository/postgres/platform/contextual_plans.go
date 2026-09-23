package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

func (repository *Repository) updateAssistantConversationTitle(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AssistantConversationTitleInput)
	title := strings.TrimSpace(payload.Title)
	if !ok || payload.ConversationRef == "" || title == "" || len([]rune(title)) > 160 || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var conversationID, projectID, state string
	var version, titleRevision int64
	if err := tx.QueryRow(ctx, queryConfigurationUpdateassistantconversationtitleSelectConversation,
		scope.organizationID, payload.ConversationRef,
	).Scan(&conversationID, &projectID, &version, &titleRevision, &state); err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	if version != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if state != "ACTIVE" {
		return commandOutcome{}, errs.ErrConflict
	}
	item := entity.AssistantConversation{ProjectRef: projectRefByID(ctx, tx, projectID)}
	if err := tx.QueryRow(ctx, queryConfigurationUpdateassistantconversationtitleUpdateConversation,
		conversationID, title, "USER_EDITED",
	).Scan(&item.Ref, &item.Title, &item.TitleSource, &item.TitleRevision, &item.State, &item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	return commandOutcome{result: command.Result{Conversation: &item}, projectID: projectID,
		projectRef: item.ProjectRef, resourceKind: "ASSISTANT_CONVERSATION", resourceRef: item.Ref,
		summary: "i18n:ASSISTANT_CONVERSATION_TITLE_UPDATED", platformEvent: "SYSTEM_ASSISTANT_CHANGED"}, nil
}

func projectRefByID(ctx context.Context, tx pgx.Tx, projectID string) string {
	if projectID == "" {
		return ""
	}
	var ref string
	_ = tx.QueryRow(ctx, queryCommandsEmitruneventSelectProjectsId, projectID).Scan(&ref)
	return ref
}

func (repository *Repository) updateAssistantPlanDraft(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AssistantPlanDraftInput)
	if !ok || payload.PlanRef == "" || strings.TrimSpace(payload.Summary) == "" || len(payload.Summary) > 2000 ||
		input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var planID, conversationRef, state, projectRef string
	var version, revision int64
	var rawCurrent []byte
	if err := tx.QueryRow(ctx, queryConfigurationUpdateassistantplandraftSelectPlan, scope.organizationID, payload.PlanRef).Scan(
		&planID, &conversationRef, &state, &version, &revision, &projectRef, &rawCurrent,
	); err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	if version != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if state == "APPLIED" || state == "REJECTED" {
		return commandOutcome{}, errs.ErrAlreadyResolved
	}
	var current []entity.AssistantPlanOperation
	if json.Unmarshal(rawCurrent, &current) != nil {
		return commandOutcome{}, errs.ErrConflict
	}
	if len(payload.Operations) != len(current) {
		return commandOutcome{}, errs.ErrForbidden
	}
	currentByKey := make(map[string]entity.AssistantPlanOperation, len(current))
	for _, operation := range current {
		currentByKey[operation.Key] = operation
	}
	for index, operation := range payload.Operations {
		original, exists := currentByKey[operation.Key]
		if !exists || original.Type != operation.Type {
			return commandOutcome{}, errs.ErrForbidden
		}
		switch operation.Type {
		case "UPDATE_AGENT":
			updated, err := rehydrateEditedAssistantAgent(original, operation)
			if err != nil {
				return commandOutcome{}, err
			}
			payload.Operations[index] = updated
		case "UPDATE_INTEGRATION_CONNECTION":
			updated, err := rehydrateEditedAssistantConnection(original, operation)
			if err != nil {
				return commandOutcome{}, err
			}
			payload.Operations[index] = updated
		case "UPDATE_SCHEDULE":
			updated, err := rehydrateEditedAssistantSchedule(original, operation)
			if err != nil {
				return commandOutcome{}, err
			}
			payload.Operations[index] = updated
		case "CREATE_ROLE_IMAGE_RECIPE":
			updated, err := rehydrateEditedAssistantRoleImage(original, operation)
			if err != nil {
				return commandOutcome{}, err
			}
			payload.Operations[index] = updated
		}
	}
	operations, err := normalizeAssistantOperations(payload.Operations, projectRef)
	if err != nil {
		return commandOutcome{}, err
	}
	raw := asJSON(operations)
	digest := assistantPlanDigest(payload.Summary, raw)
	nextRevision := revision + 1
	revisionRef, err := newRef("prv")
	if err != nil {
		return commandOutcome{}, err
	}
	var createdAt time.Time
	if err := tx.QueryRow(ctx, queryConfigurationInsertAssistantPlanRevision, revisionRef, scope.organizationID,
		planID, nextRevision, strings.TrimSpace(payload.Summary), raw, digest, "USER", scope.actorRef,
	).Scan(&createdAt); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryConfigurationUpdateassistantplandraftUpdatePlan, planID,
		strings.TrimSpace(payload.Summary), raw, nextRevision, digest); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	plan := entity.AssistantPlan{Ref: payload.PlanRef, ConversationRef: conversationRef, ProjectRef: projectRef,
		Summary: strings.TrimSpace(payload.Summary), State: "DRAFT", Version: version + 1, Revision: nextRevision,
		ContentDigest: digest, Operations: operations, CreatedAt: createdAt}
	return commandOutcome{result: command.Result{Plan: &plan}, projectID: mustProjectID(ctx, tx, scope.organizationID, projectRef),
		projectRef: projectRef, resourceKind: "ASSISTANT_PLAN", resourceRef: payload.PlanRef,
		summary: "i18n:ASSISTANT_PLAN_DRAFT_UPDATED", platformEvent: "SYSTEM_ASSISTANT_CHANGED"}, nil
}

func normalizeAssistantOperations(input []entity.AssistantPlanOperation, projectRef string) ([]entity.AssistantPlanOperation, error) {
	if len(input) == 0 || len(input) > maximumAssistantPlanOperations {
		return nil, errs.ErrInvalid
	}
	seen := make(map[string]struct{}, len(input))
	selected := 0
	result := make([]entity.AssistantPlanOperation, 0, len(input))
	for _, operation := range input {
		if _, duplicate := seen[operation.Key]; duplicate {
			return nil, errs.ErrInvalid
		}
		seen[operation.Key] = struct{}{}
		var err error
		operation, err = normalizeAssistantOperation(operation)
		if err != nil {
			return nil, err
		}
		operation, err = bindAssistantOperationProject(operation, projectRef)
		if err != nil {
			return nil, err
		}
		if operation.Selected {
			selected++
		}
		result = append(result, operation)
	}
	if selected == 0 {
		return nil, errs.ErrInvalid
	}
	return result, nil
}

func (repository *Repository) validateAssistantPlan(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AssistantPlanInput)
	if !ok || payload.PlanRef == "" || payload.Revision < 1 || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var planID, conversationRef, summary, rawState, digest, projectRef string
	var raw []byte
	var version, revision int64
	if err := tx.QueryRow(ctx, queryConfigurationValidateassistantplanSelectPlan, scope.organizationID, payload.PlanRef).Scan(
		&planID, &conversationRef, &summary, &raw, &rawState, &version, &revision, &digest, &projectRef,
	); err != nil {
		return commandOutcome{}, errs.ErrConflict
	}
	if version != *input.Mutation.ExpectedVersion || revision != payload.Revision {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	var stored []entity.AssistantPlanOperation
	if json.Unmarshal(raw, &stored) != nil {
		return commandOutcome{}, errs.ErrConflict
	}
	operations, err := normalizeAssistantOperations(stored, projectRef)
	if err != nil {
		return commandOutcome{}, err
	}
	problems := make([]string, 0)
	for index, operation := range operations {
		if !operation.Selected {
			continue
		}
		planned, commandErr := assistantOperationCommand(operation)
		if commandErr != nil {
			problems = append(problems, fmt.Sprintf("operation-%d-invalid", index+1))
			continue
		}
		if commandErr = repository.authorizeCommand(ctx, tx, scope, planned); commandErr != nil {
			problems = append(problems, fmt.Sprintf("operation-%d-not-permitted", index+1))
			continue
		}
		if operation.Type == "UPDATE_AGENT" {
			matching, snapshotErr := repository.assistantAgentUpdateSnapshotMatches(ctx, tx, scope, projectRef, operation)
			if snapshotErr != nil || !matching {
				problems = append(problems, fmt.Sprintf("operation-%d-snapshot-conflict", index+1))
				continue
			}
		}
		if operation.Type == "UPDATE_INTEGRATION_CONNECTION" {
			matching, snapshotErr := repository.assistantConnectionUpdateSnapshotMatches(ctx, tx, scope, operation)
			if snapshotErr != nil || !matching {
				problems = append(problems, fmt.Sprintf("operation-%d-snapshot-conflict", index+1))
				continue
			}
		}
		if operation.Type == "UPDATE_SCHEDULE" {
			matching, snapshotErr := repository.assistantScheduleUpdateSnapshotMatches(ctx, tx, scope, projectRef, operation)
			if snapshotErr != nil || !matching {
				problems = append(problems, fmt.Sprintf("operation-%d-snapshot-conflict", index+1))
				continue
			}
		}
		current, checked, versionErr := repository.assistantTargetVersion(ctx, tx, scope, operation)
		if versionErr != nil {
			problems = append(problems, fmt.Sprintf("operation-%d-target-unavailable", index+1))
		} else if checked && current != *operation.ExpectedVersion {
			problems = append(problems, fmt.Sprintf("operation-%d-version-conflict", index+1))
		}
	}
	state := "VALID"
	if len(problems) > 0 {
		state = "INVALID"
	}
	if _, err := tx.Exec(ctx, queryConfigurationValidateassistantplanUpdatePlan, planID, state, revision, problems); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	now := time.Now().UTC()
	plan := entity.AssistantPlan{Ref: payload.PlanRef, ConversationRef: conversationRef, ProjectRef: projectRef,
		Summary: summary, State: state, Version: version + 1, Revision: revision, ValidatedRevision: &revision,
		ContentDigest: digest, ValidationProblems: problems, Operations: operations, ValidatedAt: &now}
	return commandOutcome{result: command.Result{Plan: &plan}, projectID: mustProjectID(ctx, tx, scope.organizationID, projectRef),
		projectRef: projectRef, resourceKind: "ASSISTANT_PLAN", resourceRef: payload.PlanRef,
		summary: "i18n:ASSISTANT_PLAN_VALIDATED", platformEvent: "SYSTEM_ASSISTANT_CHANGED"}, nil
}

func (repository *Repository) assistantTargetVersion(ctx context.Context, tx pgx.Tx, scope scope, operation entity.AssistantPlanOperation) (int64, bool, error) {
	if operation.ExpectedVersion == nil {
		return 0, false, nil
	}
	var kind, ref string
	switch operation.Type {
	case "UPDATE_PROJECT":
		kind, ref = "PROJECT", assistantString(operation.Input, "projectRef")
	case "UPDATE_AGENT":
		kind, ref = "AGENT", assistantString(operation.Input, "agentRef")
	case "CHANGE_CAPABILITY", "ARCHIVE_AGENT":
		kind, ref = "AGENT", assistantString(operation.Input, "agentRef")
		if operation.Type == "ARCHIVE_AGENT" {
			ref = operation.Target.Ref
		}
	case "ARCHIVE_WORKFLOW":
		kind, ref = "WORKFLOW", operation.Target.Ref
	case "CHANGE_INTEGRATION_GRANT", "UPDATE_INTEGRATION_CONNECTION", "TEST_INTEGRATION_CONNECTION":
		kind, ref = "INTEGRATION_CONNECTION", assistantString(operation.Input, "connectionRef")
	case "UPDATE_SCHEDULE":
		kind, ref = "SCHEDULE", assistantString(operation.Input, "scheduleRef")
	default:
		return 0, false, nil
	}
	var current int64
	if err := tx.QueryRow(ctx, queryConfigurationValidateassistantplanSelectTargetVersion,
		scope.organizationID, kind, ref,
	).Scan(&current); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, true, errs.ErrNotFound
		}
		return 0, true, errs.ErrUnavailable
	}
	return current, true, nil
}

func rehydrateEditedAssistantAgent(original, edited entity.AssistantPlanOperation) (entity.AssistantPlanOperation, error) {
	if original.Type != "UPDATE_AGENT" || original.Key != edited.Key || original.Target.Kind != "AGENT" ||
		original.Target.Ref == "" || original.ExpectedVersion == nil || *original.ExpectedVersion < 1 ||
		edited.Parameters == nil || !onlyAssistantFields(edited.Parameters, "agentRef", "name", "purpose", "roleDescription", "avatarUrl") ||
		assistantString(edited.Parameters, "agentRef") != original.Target.Ref {
		return entity.AssistantPlanOperation{}, errs.ErrForbidden
	}
	if value, supplied := edited.Parameters["avatarUrl"]; supplied && value != original.Before["avatarUrl"] {
		return entity.AssistantPlanOperation{}, errs.ErrForbidden
	}
	parameters := map[string]any{"agentRef": original.Target.Ref}
	for _, field := range []string{"name", "purpose", "roleDescription"} {
		value, supplied := edited.Parameters[field]
		if !supplied {
			continue
		}
		text, ok := value.(string)
		if !ok || strings.TrimSpace(text) == "" {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
		parameters[field] = text
	}
	edited.Parameters = parameters
	selected := edited.Selected
	hydrated, err := hydrateAssistantAgentFields(original.Target.Ref,
		assistantString(original.Before, "name"), assistantString(original.Before, "purpose"),
		assistantString(original.Before, "roleDescription"), assistantString(original.Before, "avatarUrl"),
		*original.ExpectedVersion, edited)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	if !reflect.DeepEqual(hydrated.Before, original.Before) {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	hydrated.Selected = selected
	return hydrated, nil
}

func rehydrateEditedAssistantConnection(original, edited entity.AssistantPlanOperation) (entity.AssistantPlanOperation, error) {
	if original.Type != "UPDATE_INTEGRATION_CONNECTION" || original.Key != edited.Key ||
		original.Target.Kind != "INTEGRATION_CONNECTION" || original.Target.Ref == "" ||
		original.ExpectedVersion == nil || *original.ExpectedVersion < 1 || edited.Parameters == nil ||
		!onlyAssistantFields(edited.Parameters, "connectionRef", "definitionKey", "name", "publicConfiguration") ||
		assistantString(edited.Parameters, "connectionRef") != original.Target.Ref ||
		assistantString(edited.Parameters, "definitionKey") != assistantString(original.Before, "definitionKey") {
		return entity.AssistantPlanOperation{}, errs.ErrForbidden
	}
	parameters := map[string]any{"connectionRef": original.Target.Ref}
	if name, supplied := edited.Parameters["name"]; supplied {
		parameters["name"] = name
	}
	if configuration, supplied := edited.Parameters["publicConfiguration"]; supplied {
		parameters["publicConfiguration"] = configuration
	}
	connection := entity.IntegrationConnection{Ref: original.Target.Ref,
		DefinitionKey: assistantString(original.Before, "definitionKey"),
		Name:          assistantString(original.Before, "name"), Version: *original.ExpectedVersion}
	configuration, ok := assistantObjectValue(original.Before, "publicConfiguration")
	if !ok {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	connection.PublicConfiguration = configuration
	edited.Parameters = parameters
	selected := edited.Selected
	hydrated, err := hydrateAssistantConnectionFields(connection, edited)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	if !reflect.DeepEqual(hydrated.Before, original.Before) {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	hydrated.Selected = selected
	return hydrated, nil
}

func (repository *Repository) assistantConnectionUpdateSnapshotMatches(ctx context.Context, tx pgx.Tx, actorScope scope,
	operation entity.AssistantPlanOperation,
) (bool, error) {
	connection, err := readConnection(ctx, tx, actorScope, operation.Target.Ref)
	if err != nil {
		return false, err
	}
	before := map[string]any{"connectionRef": connection.Ref, "definitionKey": connection.DefinitionKey,
		"name": connection.Name, "publicConfiguration": connection.PublicConfiguration}
	if err := repository.validateAssistantConnectionConfiguration(ctx, tx, actorScope, connection, operation.After); err != nil {
		return false, err
	}
	return operation.ExpectedVersion != nil && *operation.ExpectedVersion == connection.Version &&
		operation.Target.Version != nil && *operation.Target.Version == connection.Version &&
		operation.Target.Name == connection.Name && reflect.DeepEqual(operation.Before, before) &&
		reflect.DeepEqual(operation.Parameters, operation.After) &&
		assistantString(operation.Parameters, "definitionKey") == connection.DefinitionKey, nil
}

func rehydrateEditedAssistantRoleImage(original, edited entity.AssistantPlanOperation) (entity.AssistantPlanOperation, error) {
	if original.Type != "CREATE_ROLE_IMAGE_RECIPE" || original.Key != edited.Key ||
		original.Target.Kind != "ROLE_IMAGE_RECIPE" || edited.Parameters == nil ||
		!onlyAssistantFields(edited.Parameters, "projectRef", "agentRef", "agentVersion", "name", "environmentKey") ||
		assistantString(edited.Parameters, "projectRef") != assistantString(original.Parameters, "projectRef") ||
		assistantString(edited.Parameters, "agentRef") != assistantString(original.Parameters, "agentRef") {
		return entity.AssistantPlanOperation{}, errs.ErrForbidden
	}
	originalVersion, originalOK := assistantInt64(original.Parameters, "agentVersion")
	editedVersion, editedOK := assistantInt64(edited.Parameters, "agentVersion")
	if !originalOK || !editedOK || originalVersion != editedVersion {
		return entity.AssistantPlanOperation{}, errs.ErrForbidden
	}
	name := assistantString(edited.Parameters, "name")
	environmentKey := assistantString(edited.Parameters, "environmentKey")
	if name == "" || environmentKey == "" {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	parameters := cloneAssistantFields(original.Parameters)
	parameters["name"], parameters["environmentKey"] = name, environmentKey
	edited.Parameters = parameters
	edited.Action = "CREATE"
	edited.Target = entity.AssistantPlanTarget{Kind: "ROLE_IMAGE_RECIPE", Name: name}
	edited.Before = map[string]any{}
	edited.After = cloneAssistantFields(parameters)
	edited.ExpectedVersion = nil
	return edited, nil
}

func (repository *Repository) assistantAgentUpdateSnapshotMatches(ctx context.Context, tx pgx.Tx, scope scope, projectRef string,
	operation entity.AssistantPlanOperation,
) (bool, error) {
	var name, purpose, roleDescription, avatarURL string
	var version int64
	err := tx.QueryRow(ctx, queryConfigurationHydrateassistantoperationSelectAgent,
		scope.organizationID, projectRef, operation.Target.Ref,
	).Scan(&name, &purpose, &roleDescription, &avatarURL, &version)
	if err != nil {
		return false, err
	}
	before := map[string]any{"agentRef": operation.Target.Ref, "name": name, "purpose": purpose,
		"roleDescription": roleDescription, "avatarUrl": avatarURL}
	return operation.ExpectedVersion != nil && *operation.ExpectedVersion == version &&
		operation.Target.Version != nil && *operation.Target.Version == version &&
		operation.Target.Name == name && reflect.DeepEqual(operation.Before, before) &&
		reflect.DeepEqual(operation.Parameters, operation.After) &&
		assistantString(operation.Parameters, "avatarUrl") == avatarURL, nil
}

func (repository *Repository) rejectAssistantPlan(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AssistantPlanInput)
	if !ok || payload.PlanRef == "" || payload.Revision < 1 || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var planID, state string
	var version, revision int64
	if err := tx.QueryRow(ctx, queryConfigurationRejectassistantplanSelectPlan, scope.organizationID, payload.PlanRef).Scan(
		&planID, &state, &version, &revision,
	); err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	if version != *input.Mutation.ExpectedVersion || revision != payload.Revision {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if state == "APPLIED" || state == "REJECTED" {
		return commandOutcome{}, errs.ErrAlreadyResolved
	}
	if _, err := tx.Exec(ctx, queryConfigurationRejectassistantplanUpdatePlan, planID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	receipt, err := repository.insertAssistantPlanReceipt(ctx, tx, scope, planID, payload.PlanRef, revision, "REJECTED", nil, nil, nil)
	if err != nil {
		return commandOutcome{}, err
	}
	plan := entity.AssistantPlan{Ref: payload.PlanRef, State: "REJECTED", Version: version + 1, Revision: revision}
	return commandOutcome{result: command.Result{Plan: &plan, PlanReceipt: &receipt}, resourceKind: "ASSISTANT_PLAN",
		resourceRef: payload.PlanRef, summary: "i18n:ASSISTANT_PLAN_REJECTED", platformEvent: "SYSTEM_ASSISTANT_CHANGED"}, nil
}

func (repository *Repository) insertAssistantPlanReceipt(ctx context.Context, tx pgx.Tx, scope scope, planID, planRef string,
	revision int64, outcome string, operations []entity.AssistantPlanOperationReceipt, conflicts []entity.AssistantPlanConflict,
	created []string,
) (entity.AssistantPlanReceipt, error) {
	if operations == nil {
		operations = []entity.AssistantPlanOperationReceipt{}
	}
	if conflicts == nil {
		conflicts = []entity.AssistantPlanConflict{}
	}
	if created == nil {
		created = []string{}
	}
	ref, err := newRef("rct")
	if err != nil {
		return entity.AssistantPlanReceipt{}, err
	}
	auditRefs := make([]string, 0, len(operations))
	for _, operation := range operations {
		if operation.AuditRef != "" {
			auditRefs = append(auditRefs, operation.AuditRef)
		}
	}
	var createdAt time.Time
	if err := tx.QueryRow(ctx, queryConfigurationInsertAssistantPlanReceipt, ref, scope.organizationID, planID,
		revision, outcome, asJSON(operations), asJSON(conflicts), auditRefs, created, scope.actorID,
	).Scan(&createdAt); err != nil {
		return entity.AssistantPlanReceipt{}, errs.ErrUnavailable
	}
	return entity.AssistantPlanReceipt{Ref: ref, PlanRef: planRef, PlanRevision: revision, Outcome: outcome,
		Operations: operations, Conflicts: conflicts, AuditRefs: auditRefs, CreatedResourceRefs: created, CreatedAt: createdAt}, nil
}
