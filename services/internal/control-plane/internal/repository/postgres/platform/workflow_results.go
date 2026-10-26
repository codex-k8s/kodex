package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strconv"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	workflowservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/workflow"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// workflowExecutionFailed denotes a failed physical execution, not accepted business output.
// workflowExecutionFailed обозначает отказ physical execution, а не принятый business output.
const workflowExecutionFailed = "EXECUTION_FAILED"

// workflowDeclarationInvalid denotes a malformed closed result declaration.
// workflowDeclarationInvalid обозначает неверное закрытое объявление результата.
const workflowDeclarationInvalid = "RESULT_INVALID"

// workflowArtifactRequired denotes missing or ineligible exact captured evidence.
// workflowArtifactRequired обозначает отсутствие допустимого exact captured evidence.
const workflowArtifactRequired = "ARTIFACT_REQUIRED"

// workflowReceiptRequired denotes a missing owner receipt without exposing foreign records.
// workflowReceiptRequired обозначает отсутствие owner receipt без раскрытия чужих записей.
const workflowReceiptRequired = "RECEIPT_REQUIRED"

// workflowReceiptLineageInvalid denotes an unproven or broken physical ancestry.
// workflowReceiptLineageInvalid обозначает недоказанную либо нарушенную physical ancestry.
const workflowReceiptLineageInvalid = "RECEIPT_LINEAGE_INVALID"

// workflowReceiptLineageAmbiguous denotes ambiguous receipt or originating revision identity.
// workflowReceiptLineageAmbiguous обозначает неоднозначный receipt либо originating revision.
const workflowReceiptLineageAmbiguous = "RECEIPT_LINEAGE_AMBIGUOUS"

// workflowReceiptScopeMismatch denotes a receipt outside the required repository or pinned grant.
// workflowReceiptScopeMismatch обозначает receipt вне требуемого repository либо pinned grant.
const workflowReceiptScopeMismatch = "RECEIPT_SCOPE_MISMATCH"

// workflowReceiptInvalid denotes corrupt receipt bytes or a mismatched immutable digest.
// workflowReceiptInvalid обозначает испорченные receipt bytes либо несовпадающий digest.
const workflowReceiptInvalid = "RECEIPT_INVALID"

// workflowPullRequestRequired denotes the absence of a verified open non-draft pull request.
// workflowPullRequestRequired обозначает отсутствие проверенного открытого недрафтового PR.
const workflowPullRequestRequired = "PULL_REQUEST_REQUIRED"

// workflowReviewSubjectMismatch denotes review of a different accepted dependency commit.
// workflowReviewSubjectMismatch обозначает review другого accepted dependency commit.
const workflowReviewSubjectMismatch = "REVIEW_SUBJECT_MISMATCH"

// workflowChangesRequired preserves a negative reviewer verdict as a business rejection.
// workflowChangesRequired сохраняет отрицательный reviewer verdict как business rejection.
const workflowChangesRequired = "CHANGES_REQUIRED"

// workflowMergeRequired denotes a merge that does not match the accepted PR/head pair.
// workflowMergeRequired обозначает merge вне accepted PR/head pair.
const workflowMergeRequired = "RELEASE_REQUIRED"

// workflowDeploymentUnsupported closes the absent admitted SSH compose consumer explicitly.
// workflowDeploymentUnsupported явно закрывает отсутствие admitted SSH compose consumer.
const workflowDeploymentUnsupported = "DEPLOYMENT_UNSUPPORTED"

// queryWorkflowResultLineage derives the published predicate and frozen accepted inputs from
// exact completed physical owner rows; caller/model selectors do not choose a step or dependency.
// queryWorkflowResultLineage выводит published predicate и frozen accepted inputs из exact
// completed owner rows; caller/model selectors не выбирают этап или предшественника.
//
//go:embed sql/workflow_result__lineage.sql
var queryWorkflowResultLineage string

// queryWorkflowResultIntegration reads the sole existing immutable effect registry through
// closed capture ancestry and exact invocation/effect/grant pins, never by a URL or latest node.
// queryWorkflowResultIntegration читает existing immutable effect registry через closed capture
// ancestry и exact invocation/effect/grant pins, а не через URL или latest node.
//
//go:embed sql/workflow_result__integration.sql
var queryWorkflowResultIntegration string

// queryWorkflowEffectResult joins an earlier immutable native receipt through the current attempt's owner-proven read-only rejoin.
// queryWorkflowEffectResult читает прежний native receipt через owner-proven read-only rejoin текущей attempt.
//
//go:embed sql/workflow_effect__result.sql
var queryWorkflowEffectResult string

// workflowResultDependency mirrors the existing immutable input receipt, not a new evidence registry.
// workflowResultDependency отражает existing immutable input receipt, не создавая evidence registry.
type workflowResultDependency struct {
	StepKey      string                       `json:"stepKey"`
	SourceNodeID string                       `json:"sourceNodeId"`
	Receipt      entity.WorkflowAttemptResult `json:"receipt"`
}

// workflowIntegrationResult contains only immutable owner receipt bytes and a closed lineage failure;
// no provider endpoint, credential or mutable model claim can authorize a result.
// workflowIntegrationResult содержит immutable owner receipt bytes и closed lineage failure;
// provider endpoint, credential либо model claim не разрешают результат.
type workflowIntegrationResult struct {
	Operation                                    string
	Input, Scope                                 []byte
	Summary, Digest, ProviderRef, LineageFailure string
}

// evaluateWorkflowStepResultTx evaluates business predicates without changing state. The caller
// owns root locks, pending-work barriers, capture, journal append and dispatch in the same Tx.
// A reason rejects the entire result; SQL failure returns an error, never partial acceptance.
// evaluateWorkflowStepResultTx проверяет business predicates без изменения состояния. Caller
// владеет root locks, pending-work barriers, capture, journal append и dispatch в той же Tx.
// Reason отклоняет весь результат; SQL failure возвращает ошибку без partial acceptance.
func (repository *Repository) evaluateWorkflowStepResultTx(ctx context.Context, tx pgx.Tx, current scope, lease map[string]any, payload command.CompleteExecutionInput) ([]entity.WorkflowAcceptedResult, string, error) {
	var step entity.WorkflowStep
	var inputs []workflowResultDependency
	var rawStep, rawInputs []byte
	var projectID, state string
	err := tx.QueryRow(ctx, queryWorkflowResultLineage, workflowResultPins(current, lease)).Scan(&rawStep, &rawInputs, &projectID, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", errs.ErrConflict
	}
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	if decodeStrict(rawStep, &step) != nil || decodeStrict(rawInputs, &inputs) != nil || len(inputs) > 8 || !workflowservice.ValidRequirements(step.ResultRequirements, step.DependsOn) {
		return nil, "", errs.ErrConflict
	}
	if payload.Success != (state == "SUCCEEDED") {
		return nil, "", errs.ErrConflict
	}
	if !payload.Success {
		return nil, workflowExecutionFailed, nil
	}
	declared, err := workflowservice.DecodeDeclaration(payload.ResultSummary, step.ResultRequirements)
	if err != nil {
		return nil, workflowDeclarationInvalid, nil
	}
	results := make([]entity.WorkflowAcceptedResult, 0, len(step.ResultRequirements))
	for _, requirement := range step.ResultRequirements {
		declaration := declared[requirement.Key]
		var result entity.WorkflowAcceptedResult
		switch requirement.Kind {
		case "ARTIFACT":
			var found bool
			result, found, err = resolveWorkflowArtifactTx(ctx, tx, current, lease, requirement, declaration)
			if err != nil {
				return nil, "", err
			}
			if !found {
				return nil, workflowArtifactRequired, nil
			}
		case "PULL_REQUEST", "INTEGRATION_RECEIPT":
			proof, reason, lookupErr := readWorkflowIntegrationResultTx(ctx, tx, current, lease, declaration.ReceiptRef)
			if lookupErr != nil {
				return nil, "", lookupErr
			}
			if reason != "" {
				return nil, reason, nil
			}
			result, reason = acceptWorkflowIntegrationResult(requirement, declaration, proof, inputs)
			if reason != "" {
				return nil, reason, nil
			}
		case "REVIEW", "QA":
			subject, found := workflowResultSubject(inputs, requirement.SubjectStepKey)
			if !found || declaration.HeadSHA != subject.HeadSHA {
				return nil, workflowReviewSubjectMismatch, nil
			}
			if declaration.Verdict != "PASS" {
				return nil, workflowChangesRequired, nil
			}
			result = entity.WorkflowAcceptedResult{Key: requirement.Key, Kind: requirement.Kind, Repository: subject.Repository, Number: subject.Number, HeadSHA: subject.HeadSHA, Verdict: "PASS", Digest: workflowResultSHA(asJSON(declaration))}
		case "DEPLOYMENT":
			// The existing profile has no admitted SSH compose receipt path; a schema enum is not proof.
			// В existing profile нет admitted SSH compose receipt path; schema enum не является proof.
			return nil, workflowDeploymentUnsupported, nil
		default:
			return nil, workflowDeclarationInvalid, nil
		}
		results = append(results, result)
	}
	return results, "", nil
}

// workflowResultPins copies only server-owned coordinates needed by the completed-owner queries.
// workflowResultPins копирует лишь server-owned coordinates для completed-owner queries.
func workflowResultPins(current scope, lease map[string]any) pgx.StrictNamedArgs {
	return pgx.StrictNamedArgs{"organization_id": current.organizationID, "node_id": stringMap(lease, "nodeID"), "root_run_id": stringMap(lease, "rootRunID"), "runtime_revision_id": stringMap(lease, "runtimeRevisionID"), "lease_id": stringMap(lease, "leaseID")}
}

// readWorkflowIntegrationResultTx distinguishes missing or ambiguous evidence from SQL failure;
// existing receipt data is never inserted, repaired or canceled by this reader.
// readWorkflowIntegrationResultTx отличает missing/ambiguous evidence от SQL failure;
// reader не вставляет, не исправляет и не отменяет existing receipt data.
func readWorkflowIntegrationResultTx(ctx context.Context, tx pgx.Tx, current scope, lease map[string]any, ref string) (workflowIntegrationResult, string, error) {
	pins := workflowResultPins(current, lease)
	pins["receipt_ref"] = ref
	rows, err := tx.Query(ctx, queryWorkflowResultIntegration, pins)
	if err != nil {
		return workflowIntegrationResult{}, "", errs.ErrUnavailable
	}
	defer rows.Close()
	var result workflowIntegrationResult
	count := 0
	for rows.Next() {
		if rows.Scan(&result.Operation, &result.Input, &result.Scope, &result.Summary, &result.Digest, &result.ProviderRef, &result.LineageFailure) != nil {
			return workflowIntegrationResult{}, "", errs.ErrUnavailable
		}
		count++
	}
	if rows.Err() != nil {
		return workflowIntegrationResult{}, "", errs.ErrUnavailable
	}
	if count == 0 {
		rows.Close()
		rows, err = tx.Query(ctx, queryWorkflowEffectResult, pins)
		if err != nil {
			return workflowIntegrationResult{}, "", errs.ErrUnavailable
		}
		defer rows.Close()
		for rows.Next() {
			if rows.Scan(&result.Operation, &result.Input, &result.Scope, &result.Summary, &result.Digest, &result.ProviderRef, &result.LineageFailure) != nil {
				return workflowIntegrationResult{}, "", errs.ErrUnavailable
			}
			count++
		}
		if rows.Err() != nil {
			return workflowIntegrationResult{}, "", errs.ErrUnavailable
		}
		if count == 0 {
			return workflowIntegrationResult{}, workflowReceiptRequired, nil
		}
	}
	if count != 1 {
		return workflowIntegrationResult{}, workflowReceiptLineageAmbiguous, nil
	}
	switch result.LineageFailure {
	case "":
		return result, "", nil
	case workflowReceiptLineageInvalid, workflowReceiptLineageAmbiguous, workflowReceiptScopeMismatch:
		return workflowIntegrationResult{}, result.LineageFailure, nil
	default:
		return workflowIntegrationResult{}, "", errs.ErrUnavailable
	}
}

// workflowResultSHA hashes exact immutable receipt bytes, without digestBytes' framing NUL.
// workflowResultSHA хеширует exact immutable receipt bytes без framing NUL из digestBytes.
func workflowResultSHA(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// workflowResultObject accepts one flat known-key object and rejects duplicate/case-folded keys,
// trailing values and null required values before any business projection is interpreted.
// workflowResultObject принимает один flat known-key object, отвергая duplicate/case-folded keys,
// trailing values и null required values до разбора business projection.
func workflowResultObject(raw []byte, required []string, allowed []string) (map[string]json.RawMessage, bool) {
	if len(raw) == 0 || len(raw) > 64<<10 {
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, false
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		key, err := decoder.Token()
		name, ok := key.(string)
		if err != nil || !ok || !contains(allowed, name) {
			return nil, false
		}
		if _, exists := fields[name]; exists {
			return nil, false
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return nil, false
		}
		fields[name] = value
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, false
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, false
	}
	for _, name := range required {
		value, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, false
		}
	}
	return fields, true
}

// workflowResultSubject resolves exactly one immutable accepted dependency PR identity; repeated
// agreeing reviewer evidence is permitted, but conflicting or malformed identities fail closed.
// workflowResultSubject разрешает одну immutable accepted dependency PR identity; совпадающие
// reviewer evidence допустимы, conflicting/malformed identities закрыто отклоняются.
func workflowResultSubject(inputs []workflowResultDependency, key string) (entity.WorkflowAcceptedResult, bool) {
	var subject entity.WorkflowAcceptedResult
	found := false
	matches := 0
	for _, input := range inputs {
		if input.StepKey != key {
			continue
		}
		matches++
		if input.SourceNodeID == "" || !input.Receipt.Accepted || input.Receipt.Attempt < 1 || input.Receipt.Attempt > 4 || len(input.Receipt.Results) > 4 {
			return subject, false
		}
		for _, result := range input.Receipt.Results {
			if result.HeadSHA == "" {
				continue
			}
			if !contains([]string{"PULL_REQUEST", "REVIEW", "QA", "INTEGRATION_RECEIPT"}, result.Kind) || !workflowservice.RepositoryPattern.MatchString(result.Repository) || result.Number < 1 || result.Number > 9007199254740991 || !workflowservice.CommitPattern.MatchString(result.HeadSHA) {
				return subject, false
			}
			if (result.Kind == "REVIEW" || result.Kind == "QA") && result.Verdict != "PASS" {
				return subject, false
			}
			if found && (subject.Repository != result.Repository || subject.Number != result.Number || subject.HeadSHA != result.HeadSHA) {
				return subject, false
			}
			subject = result
			found = true
		}
	}
	return subject, found && matches == 1
}

// acceptWorkflowIntegrationResult applies operation-specific predicates to owner-read receipt
// bytes; provider URL or narrative success is never accepted as a PR or merge fact.
// acceptWorkflowIntegrationResult проверяет operation-specific predicates по owner-read receipt
// bytes; provider URL или narrative success не принимаются как PR/merge fact.
func acceptWorkflowIntegrationResult(requirement entity.WorkflowResultRequirement, declaration entity.WorkflowDeclaredResult, proof workflowIntegrationResult, inputs []workflowResultDependency) (entity.WorkflowAcceptedResult, string) {
	var resource map[string]string
	if json.Unmarshal(proof.Scope, &resource) != nil || resource["owner"]+"/"+resource["repository"] != requirement.Repository {
		return entity.WorkflowAcceptedResult{}, workflowReceiptScopeMismatch
	}
	if workflowResultSHA([]byte(proof.Summary)) != proof.Digest {
		return entity.WorkflowAcceptedResult{}, workflowReceiptInvalid
	}
	result := entity.WorkflowAcceptedResult{Key: requirement.Key, Kind: requirement.Kind, Repository: requirement.Repository, ReceiptRef: declaration.ReceiptRef, Digest: proof.Digest}
	if requirement.Kind == "PULL_REQUEST" {
		allowed := []string{"number", "title", "body", "state", "head", "base", "sha", "draft", "url", "base_sha", "changed_files"}
		fields, valid := workflowResultObject([]byte(proof.Summary), []string{"number", "state", "sha", "draft"}, allowed)
		var pull struct {
			Number *int64  `json:"number"`
			State  *string `json:"state"`
			SHA    *string `json:"sha"`
			Draft  *bool   `json:"draft"`
		}
		if !valid || json.Unmarshal([]byte(proof.Summary), &pull) != nil || pull.Number == nil || *pull.Number < 1 || *pull.Number > 9007199254740991 || pull.State == nil || *pull.State != "open" || pull.SHA == nil || !workflowservice.CommitPattern.MatchString(*pull.SHA) || pull.Draft == nil || *pull.Draft || !contains([]string{"github.pull_request.create", "github.pull_request.read", "github.pull_request.update"}, proof.Operation) {
			return entity.WorkflowAcceptedResult{}, workflowPullRequestRequired
		}
		for name, value := range fields {
			if contains([]string{"title", "body", "head", "base", "url", "base_sha"}, name) {
				var text string
				if json.Unmarshal(value, &text) != nil {
					return entity.WorkflowAcceptedResult{}, workflowPullRequestRequired
				}
			}
		}
		result.Number, result.HeadSHA = *pull.Number, *pull.SHA
		if proof.ProviderRef != "github-pull:"+strconv.FormatInt(result.Number, 10) {
			return entity.WorkflowAcceptedResult{}, workflowPullRequestRequired
		}
		if proof.Operation != "github.pull_request.create" {
			allowedInput := []string{"pull_request_number"}
			if proof.Operation == "github.pull_request.update" {
				allowedInput = append(allowedInput, "title", "body", "state", "base")
			}
			_, validInput := workflowResultObject(proof.Input, []string{"pull_request_number"}, allowedInput)
			var input struct {
				Number int64 `json:"pull_request_number"`
			}
			if !validInput || json.Unmarshal(proof.Input, &input) != nil || input.Number != result.Number {
				return entity.WorkflowAcceptedResult{}, workflowPullRequestRequired
			}
		}
		return result, ""
	}
	subject, found := workflowResultSubject(inputs, requirement.SubjectStepKey)
	_, validInput := workflowResultObject(proof.Input, []string{"pull_request_number", "sha"}, []string{"pull_request_number", "sha", "message", "merge_method"})
	_, validOutput := workflowResultObject([]byte(proof.Summary), []string{"merged", "sha"}, []string{"merged", "sha"})
	var input struct {
		Number int64  `json:"pull_request_number"`
		SHA    string `json:"sha"`
	}
	var merge struct {
		Merged *bool  `json:"merged"`
		SHA    string `json:"sha"`
	}
	if requirement.Kind != "INTEGRATION_RECEIPT" || !found || subject.Repository != requirement.Repository || proof.Operation != requirement.Operation || proof.Operation != "github.pull_request.merge" || !validInput || !validOutput || json.Unmarshal(proof.Input, &input) != nil || input.Number != subject.Number || input.SHA != subject.HeadSHA || json.Unmarshal([]byte(proof.Summary), &merge) != nil || merge.Merged == nil || !*merge.Merged || !workflowservice.CommitPattern.MatchString(merge.SHA) || proof.ProviderRef != "github-pull:"+strconv.FormatInt(subject.Number, 10) {
		return entity.WorkflowAcceptedResult{}, workflowMergeRequired
	}
	result.Number, result.HeadSHA = subject.Number, merge.SHA
	return result, ""
}
