package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/prompt_schedule_preview_saved.sql
var queryPromptSchedulePreviewSaved string

func (r *Repository) GetSchedulePromptPreviewSnapshot(ctx context.Context, principal value.Principal, input command.ScheduleInput, ref string, expected int64, scheduledFor time.Time, mode string) (entity.PromptMaterializationSnapshot, entity.SchedulePromptPreviewPin, []entity.TemplateVariable, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var snapshot entity.PromptMaterializationSnapshot
	if mode == "" {
		mode = "DRAFT"
	}
	pin := entity.SchedulePromptPreviewPin{ScheduledFor: scheduledFor, Timezone: input.Timezone, Mode: mode}
	if mode != "DRAFT" && mode != "CURRENT_REVISION" || mode == "CURRENT_REVISION" && ref == "" {
		return snapshot, pin, nil, errs.ErrInvalid
	}
	if strings.TrimSpace(input.AutomationText) == "" || expected < 0 || expected > 9007199254740991 || (ref == "") != (expected == 0) || scheduledFor.IsZero() {
		return snapshot, pin, nil, errs.ErrInvalid
	}
	normalized, err := normalizeScheduleInput(input, scheduledFor.Add(-time.Second))
	if err != nil {
		return snapshot, pin, nil, err
	}
	input.Name = strings.TrimSpace(input.Name)
	applyNormalizedSchedulePolicies(&input, normalized)
	s, err := r.resolveScope(ctx, principal)
	if err != nil {
		return snapshot, pin, nil, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return snapshot, pin, nil, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	input.Ref = ref
	kindCommand := command.CreateSchedule
	if ref != "" {
		kindCommand = command.UpdateSchedule
	}
	permission, project, err := r.commandAccessTarget(ctx, tx, s, command.Command{Kind: kindCommand, Payload: input})
	if err != nil || r.requireAccess(ctx, tx, s, permission, project) != nil || s.authorityProjectID != "" && s.authorityProjectID != project.projectID {
		return snapshot, pin, nil, errs.ErrNotFound
	}
	var saved entity.Schedule
	execution := s
	var authorID, authorRef, authorName string
	if ref != "" {
		// Точная command authority уже проверена; legacy membership не заменяет
		// право schedule.manage на этот ресурс и не требует права на соседний.
		err = tx.QueryRow(ctx, queryPromptSchedulePreviewSaved, pgx.StrictNamedArgs{"organization_id": s.organizationID, "schedule_ref": ref, "project_id": project.projectID}).Scan(&saved.ProjectRef, &saved.Version, &saved.CurrentRevision.Ref, &saved.CurrentRevision.Digest, &saved.Target.Type, &saved.Target.Ref, &saved.ContinueSessionRef, &authorID, &authorRef, &authorName)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return snapshot, pin, nil, errs.ErrNotFound
			}
			return snapshot, pin, nil, errs.ErrUnavailable
		}
		if saved.ProjectRef != input.ProjectRef {
			return snapshot, pin, nil, errs.ErrNotFound
		}
		if saved.Version != expected {
			return snapshot, pin, nil, errs.ErrVersionMismatch
		}
		pin.ScheduleRef, pin.ScheduleVersion = ref, saved.Version
		pin.BaseRevisionRef, pin.BaseRevisionDigest = saved.CurrentRevision.Ref, saved.CurrentRevision.Digest
		if mode == "CURRENT_REVISION" {
			execution.actorID, execution.actorRef, execution.actorName = authorID, authorRef, authorName
		}
	}
	expectedTargetVersion := input.TargetVersion
	input.TargetVersion, input.TargetDigest, err = r.validateScheduleTarget(ctx, tx, s.organizationID, project.projectID, input.Target)
	if err != nil {
		return snapshot, pin, nil, err
	}
	if expectedTargetVersion != 0 && expectedTargetVersion != input.TargetVersion {
		return snapshot, pin, nil, errs.ErrVersionMismatch
	}
	if mode == "CURRENT_REVISION" {
		digest, digestErr := scheduleRevisionDigest(input)
		if digestErr != nil {
			return snapshot, pin, nil, digestErr
		}
		if digest != saved.CurrentRevision.Digest {
			return snapshot, pin, nil, errs.ErrVersionMismatch
		}
		pin.RevisionAvailable = true
		pin.RevisionRef, pin.RevisionDigest = saved.CurrentRevision.Ref, digest
	}
	pin.ExecutionActorRef = execution.actorRef
	selection := query.PromptPreviewContext{Task: input.AutomationText, Input: input.Input}
	kind := promptservice.TargetAgent
	if input.Target.Type == "WORKFLOW" {
		workflow, readErr := scanWorkflow(tx.QueryRow(ctx, queryQueriesGetworkflowSelectWorkflowsOrganizationIdRefProjectId, s.organizationID, input.Target.Ref, s.role, s.actorID), true)
		if readErr != nil {
			return snapshot, pin, nil, readErr
		}
		if workflow.Published == nil {
			return snapshot, pin, nil, errs.ErrConflict
		}
		selection.WorkflowRevisionRef = workflow.Published.Ref
		selection.WorkflowStageKey = "workflow.coordinator.initial"
		kind = promptservice.TargetWorkflowStage
	}
	if input.SessionPolicy == "CONTINUE_ONE" && saved.ContinueSessionRef != "" && saved.Target.Type == input.Target.Type && saved.Target.Ref == input.Target.Ref {
		pin.SessionRef = saved.ContinueSessionRef
		pin.Continuation = true
		snapshot, err = r.promptContinuationPreviewForActorTx(ctx, tx, s, execution, pin.SessionRef, selection)
	} else {
		snapshot, err = r.promptPreviewContextForActorTx(ctx, tx, s, execution, kind, input.Target.Ref, selection)
	}
	if err != nil {
		return snapshot, pin, nil, err
	}
	if snapshot.ProjectRef != input.ProjectRef {
		return snapshot, pin, nil, errs.ErrNotFound
	}
	if mode == "CURRENT_REVISION" {
		launch := command.Command{Kind: command.LaunchRun, Payload: command.LaunchRunInput{ProjectRef: input.ProjectRef, Title: input.Name, Task: input.AutomationText, Source: "SCHEDULE", SessionRef: pin.SessionRef, Target: input.Target, Input: input.Input}}
		if err := r.authorizeCommand(ctx, tx, execution, launch); err != nil {
			return snapshot, pin, nil, errs.ErrNotFound
		}
	}
	revision := ""
	if pin.RevisionAvailable {
		revision = pin.RevisionRef
	}
	applyAutomationPromptVariables(&snapshot, ref, input.Name, input.AutomationText, scheduledFor.Format(time.RFC3339Nano), input.Timezone, revision)
	if ref != "" {
		raw, captureErr := r.captureSchedulePromptTx(ctx, tx, s, ref, asJSON(input.PromptInputs))
		if captureErr != nil {
			return snapshot, pin, nil, captureErr
		}
		capture, captureErr := decodeSchedulePromptCapture(1, raw)
		if captureErr != nil {
			return snapshot, pin, nil, captureErr
		}
		if capture.Template != nil {
			probe := promptservice.FromSnapshot(snapshot)
			probe.TargetKind = promptservice.TargetAutomation
			probe.ExtraTemplates = nil
			probe.StagePurposeTemplate = ""
			probe.StageExpectedResultTemplate = ""
			preview, previewErr := promptservice.Materialize(capture.Template.Content, probe)
			if previewErr != nil {
				return snapshot, pin, nil, errs.ErrInvalid
			}
			if preview.Complete {
				if _, err = renderSchedulePromptTask(capture.Template, &snapshot); err != nil {
					return snapshot, pin, nil, err
				}
			} else {
				snapshot.ExtraTemplates = append(snapshot.ExtraTemplates, entity.PromptUserTemplate{Kind: "AUTOMATION_TASK", Ref: capture.Template.Ref, Digest: capture.Template.Digest, Content: capture.Template.Content})
			}
		}
	}
	if err := prepareAutomationCoordinatorPurpose(&snapshot); err != nil {
		return snapshot, pin, nil, err
	}
	raw, err := json.Marshal(struct {
		Actor    string
		Input    command.ScheduleInput
		Pin      entity.SchedulePromptPreviewPin
		Snapshot entity.PromptMaterializationSnapshot
	}{s.actorRef, input, pin, snapshot})
	if err != nil {
		return snapshot, pin, nil, errs.ErrUnavailable
	}
	digest := sha256.Sum256(raw)
	snapshot.ContextPin.Digest = hex.EncodeToString(digest[:])
	variables := []entity.TemplateVariable{}
	for _, item := range templateVariableCatalog() {
		if strings.HasPrefix(item.Name, "automation.") {
			item.Available = snapshot.Variables[item.Name] != ""
			item.Reason = "AVAILABLE"
			if !item.Available {
				item.Reason = snapshot.UnavailableVariables[item.Name]
			}
			variables = append(variables, item)
		}
	}
	if tx.Commit(ctx) != nil {
		return snapshot, pin, nil, errs.ErrUnavailable
	}
	return snapshot, pin, variables, nil
}

func applyAutomationPromptVariables(snapshot *entity.PromptMaterializationSnapshot, ref, name, task, scheduledAt, timezone, revision string) {
	if parsed, err := time.Parse(time.RFC3339Nano, scheduledAt); err == nil {
		scheduledAt = parsed.UTC().Format(time.RFC3339Nano)
	}
	if snapshot.Variables == nil {
		snapshot.Variables = map[string]string{}
	}
	if snapshot.UnavailableVariables == nil {
		snapshot.UnavailableVariables = map[string]string{}
	}
	for key, value := range map[string]string{"ref": ref, "name": name, "task": task, "scheduled_at": scheduledAt, "timezone": timezone, "revision": revision} {
		name := "automation." + key
		snapshot.Variables[name] = value
		if value == "" {
			snapshot.UnavailableVariables[name] = "RUNTIME_CONTEXT_REQUIRED"
			if key == "revision" {
				snapshot.UnavailableVariables[name] = "REVISION_NOT_SAVED"
			}
		} else {
			delete(snapshot.UnavailableVariables, name)
		}
	}
	snapshot.Automation = task
	snapshot.Variables["task"] = task
}

func prepareAutomationCoordinatorPurpose(snapshot *entity.PromptMaterializationSnapshot) error {
	if snapshot.TargetKind != promptservice.TargetWorkflowStage || snapshot.ContextPin.WorkflowStageKey != "workflow.coordinator.initial" {
		return nil
	}
	purpose, err := promptservice.AutomationTaskPurpose(snapshot.StagePurposeTemplate, promptservice.FromSnapshot(*snapshot))
	if err != nil {
		return errs.ErrInvalid
	}
	snapshot.StagePurposeTemplate = purpose
	return nil
}
