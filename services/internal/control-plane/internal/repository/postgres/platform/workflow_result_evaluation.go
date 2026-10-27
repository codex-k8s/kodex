package platform

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	workflowservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/workflow"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// workflowReviewRejection selects only published subjects already pinned in the failed review input.
// It carries no caller-chosen run/node/agent and cannot authorize a new execution by itself.
// workflowReviewRejection выбирает только published subjects из pinned input отказавшего review.
// Caller run/node/agent отсутствуют; значение само не разрешает новое исполнение.
type workflowReviewRejection = entity.WorkflowReviewSubject

// workflowStepEvaluation separates all-or-nothing evidence acceptance from typed owner rework causes.
// workflowStepEvaluation отделяет полную приёмку evidence от typed owner rework causes.
type workflowStepEvaluation struct {
	Results []entity.WorkflowAcceptedResult
	Reason  string
	Rework  []workflowReviewRejection
}

// rejectedWorkflowEvaluation emits no accepted evidence when any mandatory predicate fails.
// rejectedWorkflowEvaluation не возвращает accepted evidence при отказе обязательного predicate.
func rejectedWorkflowEvaluation(reason string) workflowStepEvaluation {
	return workflowStepEvaluation{Reason: reason}
}

// evaluateWorkflowStepResultDetailedTx derives evidence and rework subjects from the exact owner rows.
// Opt-in rework validates every predicate before routing a reviewer rejection; legacy keeps its
// existing first-failure behavior. This function changes no state, grants, history or external effect.
// evaluateWorkflowStepResultDetailedTx выводит evidence и rework subjects из exact owner rows.
// Opt-in rework проверяет все predicates до routing; legacy сохраняет прежний первый отказ.
// Функция не меняет state, grants, history или внешнее действие.
func (repository *Repository) evaluateWorkflowStepResultDetailedTx(ctx context.Context, tx pgx.Tx, current scope, lease map[string]any, payload command.CompleteExecutionInput, upstream bool) (workflowStepEvaluation, error) {
	var step entity.WorkflowStep
	var inputs []workflowResultDependency
	var rawStep, rawInputs []byte
	var projectID, state string
	err := tx.QueryRow(ctx, queryWorkflowResultLineage, workflowResultPins(current, lease)).Scan(&rawStep, &rawInputs, &projectID, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return workflowStepEvaluation{}, errs.ErrConflict
	}
	if err != nil {
		return workflowStepEvaluation{}, errs.ErrUnavailable
	}
	if decodeStrict(rawStep, &step) != nil || decodeStrict(rawInputs, &inputs) != nil || len(inputs) > 8 || !workflowservice.ValidRequirements(step.ResultRequirements, step.DependsOn) || payload.Success != (state == "SUCCEEDED") {
		return workflowStepEvaluation{}, errs.ErrConflict
	}
	if !payload.Success {
		return rejectedWorkflowEvaluation(workflowExecutionFailed), nil
	}
	declared, err := workflowservice.DecodeDeclaration(payload.ResultSummary, step.ResultRequirements)
	if err != nil {
		return rejectedWorkflowEvaluation(workflowDeclarationInvalid), nil
	}
	results := make([]entity.WorkflowAcceptedResult, 0, len(step.ResultRequirements))
	var changes []workflowReviewRejection
	for _, requirement := range step.ResultRequirements {
		declaration := declared[requirement.Key]
		var result entity.WorkflowAcceptedResult
		switch requirement.Kind {
		case "ARTIFACT":
			var found bool
			result, found, err = resolveWorkflowArtifactTx(ctx, tx, current, lease, requirement, declaration)
			if err != nil {
				return workflowStepEvaluation{}, err
			}
			if !found {
				return rejectedWorkflowEvaluation(workflowArtifactRequired), nil
			}
		case "PULL_REQUEST", "INTEGRATION_RECEIPT":
			proof, reason, lookupErr := readWorkflowIntegrationResultTx(ctx, tx, current, lease, declaration.ReceiptRef)
			if lookupErr != nil {
				return workflowStepEvaluation{}, lookupErr
			}
			if reason != "" {
				return rejectedWorkflowEvaluation(reason), nil
			}
			result, reason = acceptWorkflowIntegrationResult(requirement, declaration, proof, inputs)
			if reason != "" {
				return rejectedWorkflowEvaluation(reason), nil
			}
		case "ARTIFACT_REVIEW":
			var found bool
			result, found, err = resolveWorkflowArtifactReviewTx(ctx, tx, current, lease, inputs, requirement, declaration)
			if err != nil {
				return workflowStepEvaluation{}, err
			}
			if !found {
				return rejectedWorkflowEvaluation(workflowReviewSubjectMismatch), nil
			}
			result.Key, result.Kind, result.Verdict = requirement.Key, requirement.Kind, declaration.Verdict
			if declaration.Verdict != "PASS" {
				if !upstream {
					return rejectedWorkflowEvaluation(workflowChangesRequired), nil
				}
				changes = append(changes, workflowReviewRejection{requirement.Key, requirement.SubjectStepKey, requirement.SubjectResultKey})
				continue
			}
		case "REVIEW", "QA":
			subject, found := workflowResultSubject(inputs, requirement.SubjectStepKey)
			if !found || declaration.HeadSHA != subject.HeadSHA {
				return rejectedWorkflowEvaluation(workflowReviewSubjectMismatch), nil
			}
			if declaration.Verdict != "PASS" {
				if !upstream {
					return rejectedWorkflowEvaluation(workflowChangesRequired), nil
				}
				changes = append(changes, workflowReviewRejection{requirement.Key, requirement.SubjectStepKey, ""})
				continue
			}
			result = entity.WorkflowAcceptedResult{Key: requirement.Key, Kind: requirement.Kind, Repository: subject.Repository, Number: subject.Number, HeadSHA: subject.HeadSHA, Verdict: "PASS", Digest: workflowResultSHA(asJSON(declaration))}
		case "DEPLOYMENT":
			return rejectedWorkflowEvaluation(workflowDeploymentUnsupported), nil
		default:
			return rejectedWorkflowEvaluation(workflowDeclarationInvalid), nil
		}
		results = append(results, result)
	}
	if len(changes) != 0 {
		return workflowStepEvaluation{Reason: workflowChangesRequired, Rework: changes}, nil
	}
	return workflowStepEvaluation{Results: results}, nil
}

// workflowArtifactReviewSubject selects one exact accepted ARTIFACT result, never an arbitrary file.
// Ambiguous inputs/results fail closed; freshness and file admission remain mandatory owner checks.
// workflowArtifactReviewSubject выбирает exact accepted ARTIFACT result, не произвольный файл.
// Неоднозначность закрыта; freshness и file admission остаются обязательными owner checks.
func workflowArtifactReviewSubject(inputs []workflowResultDependency, requirement entity.WorkflowResultRequirement) (entity.WorkflowAcceptedResult, bool) {
	var subject entity.WorkflowAcceptedResult
	inputCount, resultCount := 0, 0
	for _, input := range inputs {
		if input.StepKey != requirement.SubjectStepKey {
			continue
		}
		inputCount++
		if input.SourceNodeID == "" || !input.Receipt.Accepted || input.Receipt.Attempt < 1 || input.Receipt.Attempt > 4 || len(input.Receipt.Results) > 4 {
			return subject, false
		}
		for _, result := range input.Receipt.Results {
			if result.Key != requirement.SubjectResultKey {
				continue
			}
			resultCount++
			digest, err := hex.DecodeString(result.Digest)
			if result.Kind != "ARTIFACT" || !strings.HasPrefix(result.ArtifactRef, "art_") || !workflowLaunchRefPattern.MatchString(result.ArtifactRef) || !workflowLaunchRefPattern.MatchString(result.ArtifactRevision) || err != nil || len(digest) != 32 || strings.ToLower(result.Digest) != result.Digest {
				return subject, false
			}
			subject = result
		}
	}
	return subject, inputCount == 1 && resultCount == 1
}
