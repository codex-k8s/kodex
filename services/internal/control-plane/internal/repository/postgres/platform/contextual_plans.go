package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	if err := tx.QueryRow(ctx, queryConfigurationUpdateassistantplandraftSelectPlan, scope.organizationID, payload.PlanRef).Scan(
		&planID, &conversationRef, &state, &version, &revision, &projectRef,
	); err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	if version != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if state == "APPLIED" || state == "REJECTED" {
		return commandOutcome{}, errs.ErrAlreadyResolved
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
	case "CHANGE_CAPABILITY", "ARCHIVE_AGENT":
		kind, ref = "AGENT", assistantString(operation.Input, "agentRef")
		if operation.Type == "ARCHIVE_AGENT" {
			ref = operation.Target.Ref
		}
	case "ARCHIVE_WORKFLOW":
		kind, ref = "WORKFLOW", operation.Target.Ref
	case "CHANGE_INTEGRATION_GRANT", "TEST_INTEGRATION_CONNECTION":
		kind, ref = "INTEGRATION_CONNECTION", assistantString(operation.Input, "connectionRef")
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
