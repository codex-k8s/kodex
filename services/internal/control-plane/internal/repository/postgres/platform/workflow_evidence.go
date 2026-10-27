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
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	workflowservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/workflow"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryWorkflowEvidenceArtifact resolves only closed own ancestor capture in the current exact
// frozen catalog, retaining the mutable HEAD lock before eligibility and object metadata reads.
// queryWorkflowEvidenceArtifact разрешает только closed own ancestor capture в current exact
// frozen catalog, удерживая mutable HEAD lock до eligibility и object metadata reads.
//
//go:embed sql/workflow_evidence__artifact.sql
var queryWorkflowEvidenceArtifact string

// queryWorkflowEvidenceContent reads an already owner-resolved immutable result pin; it is not
// an eligibility query and must never be called with a model-selected unverified result.
// queryWorkflowEvidenceContent читает уже разрешённый owner immutable result pin; это не
// eligibility query, её нельзя вызывать с непроверенным результатом модели.
//
//go:embed sql/workflow_evidence__content.sql
var queryWorkflowEvidenceContent string

// workflowEvidenceFile keeps storage custody private while exposing only exact public report pins.
// workflowEvidenceFile сохраняет storage custody приватной, выдавая только exact public report pins.
type workflowEvidenceFile struct {
	Ref, RevisionRef, Digest, Name, MediaType string
	Revision, Size                            int64
	ObjectKey, ObjectVersion, ObjectETag      string
}

// workflowQAReportPin is appended only by the owner and binds issue publication to one immutable
// captured report. It contains no object locator, credential or model-supplied authority tuple.
// workflowQAReportPin добавляется только owner и связывает Issue с одним immutable captured
// report. Здесь нет object locator, credential либо authority tuple из модели.
type workflowQAReportPin struct {
	Ref         string `json:"ref"`
	RevisionRef string `json:"revisionRef"`
	Revision    int64  `json:"revision"`
	Digest      string `json:"digest"`
}

// workflowEvidenceDestinations fixes the exact metadata row shared by the two closed readers.
// workflowEvidenceDestinations фиксирует exact metadata row двух закрытых readers.
func workflowEvidenceDestinations(file *workflowEvidenceFile) []any {
	return []any{&file.Ref, &file.RevisionRef, &file.Revision, &file.Digest, &file.Name, &file.MediaType, &file.Size, &file.ObjectKey, &file.ObjectVersion, &file.ObjectETag}
}

// readWorkflowCapturedEvidenceTx resolves one early report or exact log through native capture,
// frozen RR and fresh ACL. A duplicate filename is rejected before eligibility filtering.
// readWorkflowCapturedEvidenceTx разрешает early report либо exact log через native capture,
// frozen RR и fresh ACL. Duplicate filename отвергается до eligibility filtering.
func readWorkflowCapturedEvidenceTx(ctx context.Context, tx pgx.Tx, current scope, lease map[string]any, name string, selector *entity.WorkflowArtifactSource) (workflowEvidenceFile, error) {
	pins := workflowResultPins(current, lease)
	pins["file_name"], pins["artifact_ref"], pins["artifact_revision"], pins["artifact_digest"] = name, "", int64(0), ""
	if selector != nil {
		pins["artifact_ref"], pins["artifact_revision"], pins["artifact_digest"] = selector.ArtifactRef, selector.Revision, selector.Digest
	}
	rows, err := tx.Query(ctx, queryWorkflowEvidenceArtifact, pins)
	if err != nil {
		return workflowEvidenceFile{}, errs.ErrUnavailable
	}
	defer rows.Close()
	var file workflowEvidenceFile
	count := 0
	for rows.Next() {
		if rows.Scan(workflowEvidenceDestinations(&file)...) != nil {
			return workflowEvidenceFile{}, errs.ErrUnavailable
		}
		count++
	}
	if rows.Err() != nil {
		return workflowEvidenceFile{}, errs.ErrUnavailable
	}
	if count != 1 {
		return workflowEvidenceFile{}, errs.ErrNotFound
	}
	return file, nil
}

// readWorkflowQAReport reads exact immutable object bytes under owner-proved capture/HEAD pins.
// Size, digest, storage receipt and UTF-8/schema checks all precede interpretation of finding data.
// readWorkflowQAReport читает exact immutable object bytes по owner-proved capture/HEAD pins.
// Size, digest, storage receipt и UTF-8/schema проверяются до интерпретации findings.
func (repository *Repository) readWorkflowQAReport(ctx context.Context, file workflowEvidenceFile) (workflowservice.QAReport, error) {
	if file.MediaType != "application/json" || file.Size < 1 || file.Size > workflowservice.MaximumQAReportBytes || file.ObjectKey == "" {
		return workflowservice.QAReport{}, errs.ErrConflict
	}
	object, err := repository.objects.Get(ctx, file.ObjectKey, file.ObjectVersion)
	if err != nil {
		return workflowservice.QAReport{}, mapObjectStorageError(err)
	}
	if object.Digest != file.Digest || object.SizeBytes != file.Size || file.ObjectVersion != "" && object.VersionID != file.ObjectVersion || file.ObjectETag != "" && object.ETag != file.ObjectETag {
		_ = object.Body.Close()
		return workflowservice.QAReport{}, errs.ErrConflict
	}
	raw, readErr := io.ReadAll(io.LimitReader(object.Body, workflowservice.MaximumQAReportBytes+1))
	closeErr := object.Body.Close()
	if readErr != nil || closeErr != nil {
		return workflowservice.QAReport{}, errs.ErrUnavailable
	}
	digest := sha256.Sum256(raw)
	if int64(len(raw)) != file.Size || "sha256:"+hex.EncodeToString(digest[:]) != file.Digest {
		return workflowservice.QAReport{}, errs.ErrConflict
	}
	report, err := workflowservice.DecodeQAReport(raw)
	if err != nil {
		return workflowservice.QAReport{}, errs.ErrConflict
	}
	return report, nil
}

// readWorkflowAcceptedEvidenceTx only reads content after the ordinary artifact predicate proved
// exact capture, revision, CLEAN and ACL; a missing immutable row remains a closed storage failure.
// readWorkflowAcceptedEvidenceTx читает content только после обычного artifact predicate,
// доказавшего exact capture/revision/CLEAN/ACL; отсутствие immutable row закрыто отклоняется.
func readWorkflowAcceptedEvidenceTx(ctx context.Context, tx pgx.Tx, current scope, projectID string, result entity.WorkflowAcceptedResult) (workflowEvidenceFile, error) {
	var file workflowEvidenceFile
	err := tx.QueryRow(ctx, queryWorkflowEvidenceContent, pgx.StrictNamedArgs{"organization_id": current.organizationID, "project_id": projectID, "artifact_ref": result.ArtifactRef, "revision_ref": result.ArtifactRevision, "artifact_digest": "sha256:" + result.Digest}).Scan(workflowEvidenceDestinations(&file)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return file, errs.ErrConflict
	}
	if err != nil {
		return file, errs.ErrUnavailable
	}
	return file, nil
}

// validateWorkflowQALogsTx resolves every declared output selector using the same own captured
// lineage and current frozen catalog; repeated references are allowed only for identical pins.
// validateWorkflowQALogsTx разрешает каждый output selector через ту же own captured lineage
// и current frozen catalog; повторные ссылки допустимы только с одинаковыми pins.
func validateWorkflowQALogsTx(ctx context.Context, tx pgx.Tx, current scope, lease map[string]any, report workflowservice.QAReport) error {
	pins := map[string]*entity.WorkflowArtifactSource{}
	for _, test := range report.Tests {
		if previous, exists := pins[test.Log.ArtifactRef]; exists && !bytes.Equal(asJSON(previous), asJSON(test.Log)) {
			return errs.ErrConflict
		}
		if _, err := readWorkflowCapturedEvidenceTx(ctx, tx, current, lease, "", test.Log); err != nil {
			return err
		}
		pins[test.Log.ArtifactRef] = test.Log
	}
	return nil
}

// workflowEvidenceOperation identifies only the two per-business evidence effects of the new
// profile; it never changes legacy logical-slot identity or admits another write operation.
// workflowEvidenceOperation выделяет только два per-business evidence effects нового профиля;
// она не меняет identity прежних slots и не допускает другие WRITE.
func workflowEvidenceOperation(profile, operation string) bool {
	return profile == workflowservice.GitHubReviewQAV1 && (operation == "github.pull_request.review.create" || operation == "github.issue.create")
}

// workflowIssueEvidenceTx requires a previously captured finding report in the current frozen RR
// before allocating any Issue. The published QA predicate supplies the report key and subject;
// model-selected title/body must equal the one aggregate issue stored in that exact report.
// workflowIssueEvidenceTx требует captured finding report в current frozen RR до создания Issue.
// Published QA predicate задаёт report key и subject; title/body модели должны совпасть с одной
// агрегированной Issue, сохранённой в точном report.
func (repository *Repository) workflowIssueEvidenceTx(ctx context.Context, tx pgx.Tx, current scope, admission *workflowEffectAdmission, intent workflowEffectIntent, step entity.WorkflowStep, inputs []workflowResultDependency, input []byte) (*workflowQAReportPin, error) {
	var requirement, artifact entity.WorkflowResultRequirement
	for _, candidate := range step.ResultRequirements {
		if candidate.Key == intent.ResultKey {
			requirement = candidate
		}
	}
	for _, candidate := range step.ResultRequirements {
		if candidate.Key == requirement.EvidenceKey {
			artifact = candidate
		}
	}
	if requirement.Kind != "QA" || artifact.Kind != "ARTIFACT" || artifact.MediaType != "application/json" {
		return nil, errs.ErrForbidden
	}
	lease := map[string]any{"rootRunID": admission.RootID, "nodeID": admission.NodeID, "runtimeRevisionID": intent.OriginRevisionID, "leaseID": intent.OriginLeaseID}
	file, err := readWorkflowCapturedEvidenceTx(ctx, tx, current, lease, artifact.FileName, nil)
	if err != nil {
		return nil, err
	}
	report, err := repository.readWorkflowQAReport(ctx, file)
	if err != nil {
		return nil, err
	}
	subject, found := workflowResultSubject(inputs, requirement.SubjectStepKey)
	if !found || report.Repository != subject.Repository || report.Number != subject.Number || report.HeadSHA != subject.HeadSHA || report.Verdict != "CHANGES_REQUIRED" || report.Issue == nil || len(report.Findings) == 0 {
		return nil, errs.ErrForbidden
	}
	if err := validateWorkflowQALogsTx(ctx, tx, current, lease, report); err != nil {
		return nil, err
	}
	_, valid := workflowResultObject(input, []string{"title", "body"}, []string{"title", "body"})
	var requested workflowservice.QAIssue
	if !valid || json.Unmarshal(input, &requested) != nil || requested != *report.Issue {
		return nil, errs.ErrForbidden
	}
	return &workflowQAReportPin{Ref: file.Ref, RevisionRef: file.RevisionRef, Revision: file.Revision, Digest: file.Digest}, nil
}

// acceptWorkflowCommentResult verifies the native COMMENT receipt against the exact accepted
// subject and completed producing lineage. A model verdict never becomes GitHub APPROVED.
// acceptWorkflowCommentResult проверяет native COMMENT receipt по exact accepted subject и
// completed producing lineage. Verdict модели никогда не становится GitHub APPROVED.
func acceptWorkflowCommentResult(requirement entity.WorkflowResultRequirement, declaration entity.WorkflowDeclaredResult, proof workflowIntegrationResult, inputs []workflowResultDependency) (entity.WorkflowAcceptedResult, string) {
	subject, found := workflowResultSubject(inputs, requirement.SubjectStepKey)
	var scope map[string]string
	_, validInput := workflowResultObject(proof.Input, []string{"pull_request_number", "sha", "event", "body"}, []string{"pull_request_number", "sha", "event", "body"})
	_, validOutput := workflowResultObject([]byte(proof.Summary), []string{"id", "body", "state", "commit_id"}, []string{"id", "body", "state", "commit_id"})
	var input struct {
		Number           int64 `json:"pull_request_number"`
		SHA, Event, Body string
	}
	var output struct {
		ID          int64 `json:"id"`
		Body, State string
		CommitID    string `json:"commit_id"`
	}
	if !found || declaration.HeadSHA != subject.HeadSHA || proof.Operation != "github.pull_request.review.create" ||
		json.Unmarshal(proof.Scope, &scope) != nil || scope["owner"]+"/"+scope["repository"] != subject.Repository ||
		!validInput || !validOutput || json.Unmarshal(proof.Input, &input) != nil || json.Unmarshal([]byte(proof.Summary), &output) != nil ||
		input.Number != subject.Number || input.SHA != subject.HeadSHA || input.Event != "COMMENT" || strings.TrimSpace(input.Body) == "" ||
		output.Body != input.Body || output.State != "COMMENTED" || output.CommitID != subject.HeadSHA || output.ID < 1 || output.ID > 9007199254740991 ||
		proof.ProviderRef != "github-review:"+strconv.FormatInt(output.ID, 10) || proof.Digest != workflowResultSHA([]byte(proof.Summary)) {
		return entity.WorkflowAcceptedResult{}, workflowReceiptInvalid
	}
	return entity.WorkflowAcceptedResult{Key: requirement.Key, Kind: "REVIEW", Repository: subject.Repository, Number: subject.Number, HeadSHA: subject.HeadSHA, Verdict: declaration.Verdict, ReceiptRef: declaration.ReceiptRef, Digest: proof.Digest}, ""
}

// evaluateWorkflowQAResultTx resolves the required report and every log through native producing
// capture, then binds optional issue receipt and exact subject. Actual test execution remains a
// separately reviewed employee action; this boundary attests only owner-resolved bytes and facts.
// evaluateWorkflowQAResultTx разрешает report и каждый log через native producing capture,
// связывая optional Issue receipt и exact subject. Выполнение теста отдельно проверяется по работе
// сотрудника; эта граница доказывает только owner-resolved bytes и facts.
func (repository *Repository) evaluateWorkflowQAResultTx(ctx context.Context, tx pgx.Tx, current scope, lease map[string]any, projectID string, step entity.WorkflowStep, requirement entity.WorkflowResultRequirement, declaration entity.WorkflowDeclaredResult, declared map[string]entity.WorkflowDeclaredResult, inputs []workflowResultDependency) (entity.WorkflowAcceptedResult, string, error) {
	var artifact entity.WorkflowResultRequirement
	for _, candidate := range step.ResultRequirements {
		if candidate.Key == requirement.EvidenceKey {
			artifact = candidate
		}
	}
	captured, found, err := resolveWorkflowArtifactTx(ctx, tx, current, lease, artifact, declared[requirement.EvidenceKey])
	if err != nil {
		return entity.WorkflowAcceptedResult{}, "", err
	}
	if !found {
		return entity.WorkflowAcceptedResult{}, workflowArtifactRequired, nil
	}
	file, err := readWorkflowAcceptedEvidenceTx(ctx, tx, current, projectID, captured)
	if err != nil {
		return entity.WorkflowAcceptedResult{}, "", err
	}
	report, err := repository.readWorkflowQAReport(ctx, file)
	if err != nil {
		if errors.Is(err, errs.ErrConflict) {
			return entity.WorkflowAcceptedResult{}, workflowReceiptInvalid, nil
		}
		return entity.WorkflowAcceptedResult{}, "", err
	}
	subject, valid := workflowResultSubject(inputs, requirement.SubjectStepKey)
	if !valid || declaration.HeadSHA != subject.HeadSHA || report.HeadSHA != subject.HeadSHA || report.Repository != subject.Repository || report.Number != subject.Number || report.Verdict != declaration.Verdict {
		return entity.WorkflowAcceptedResult{}, workflowReviewSubjectMismatch, nil
	}
	if err := validateWorkflowQALogsTx(ctx, tx, current, lease, report); err != nil {
		if errors.Is(err, errs.ErrNotFound) || errors.Is(err, errs.ErrConflict) {
			return entity.WorkflowAcceptedResult{}, workflowArtifactRequired, nil
		}
		return entity.WorkflowAcceptedResult{}, "", err
	}
	if (report.Issue != nil) != (declaration.ReceiptRef != "") {
		return entity.WorkflowAcceptedResult{}, workflowReceiptRequired, nil
	}
	if declaration.ReceiptRef != "" {
		proof, reason, err := readWorkflowIntegrationResultTx(ctx, tx, current, lease, declaration.ReceiptRef)
		if err != nil || reason != "" {
			return entity.WorkflowAcceptedResult{}, reason, err
		}
		if !workflowQAIssueReceipt(proof, report) {
			return entity.WorkflowAcceptedResult{}, workflowReceiptInvalid, nil
		}
		pins := workflowResultPins(current, lease)
		pins["receipt_ref"], pins["result_key"], pins["qa_report"] = declaration.ReceiptRef, requirement.Key, workflowQAReportPin{Ref: file.Ref, RevisionRef: file.RevisionRef, Revision: file.Revision, Digest: file.Digest}
		var bound bool
		if err := tx.QueryRow(ctx, queryWorkflowEvidenceIssuePin, pins).Scan(&bound); err != nil {
			return entity.WorkflowAcceptedResult{}, "", errs.ErrUnavailable
		}
		if !bound {
			return entity.WorkflowAcceptedResult{}, workflowReceiptInvalid, nil
		}

	}
	return entity.WorkflowAcceptedResult{Key: requirement.Key, Kind: "QA", Repository: subject.Repository, Number: subject.Number, HeadSHA: subject.HeadSHA, Verdict: declaration.Verdict, ArtifactRef: file.Ref, ArtifactRevision: file.RevisionRef, Digest: strings.TrimPrefix(file.Digest, "sha256:"), ReceiptRef: declaration.ReceiptRef}, "", nil
}

// workflowQAIssueReceipt requires one real issue creation with the exact stored report request;
// an issue URL, unrelated read, PR receipt or invented positive number cannot satisfy it.
// workflowQAIssueReceipt требует реальное создание Issue с exact stored report request;
// URL, чужой READ, PR receipt либо выдуманный номер условие не выполняют.
func workflowQAIssueReceipt(proof workflowIntegrationResult, report workflowservice.QAReport) bool {
	var scope map[string]string
	var input workflowservice.QAIssue
	var output struct {
		Number       int64 `json:"number"`
		Title, State string
	}
	_, validInput := workflowResultObject(proof.Input, []string{"title", "body"}, []string{"title", "body"})
	_, validOutput := workflowResultObject([]byte(proof.Summary), []string{"number", "title", "state"}, []string{"number", "title", "state"})
	return report.Verdict == "CHANGES_REQUIRED" && report.Issue != nil && len(report.Findings) > 0 &&
		proof.Operation == "github.issue.create" && json.Unmarshal(proof.Scope, &scope) == nil && scope["owner"]+"/"+scope["repository"] == report.Repository &&
		validInput && json.Unmarshal(proof.Input, &input) == nil && input == *report.Issue && validOutput && json.Unmarshal([]byte(proof.Summary), &output) == nil &&
		output.Number > 0 && output.Number <= 9007199254740991 && output.Title == input.Title && output.State == "open" &&
		proof.ProviderRef == "github-issue:"+strconv.FormatInt(output.Number, 10) && proof.Digest == workflowResultSHA([]byte(proof.Summary))
}

// queryWorkflowEvidenceIssuePin binds the declared report bytes to the immutable native admission
// record already proved by receipt lineage; matching Issue text alone cannot substitute another report.
// queryWorkflowEvidenceIssuePin связывает report bytes с immutable native admission record,
// уже проверенным receipt lineage; одинаковый Issue text не подменяет другой report.
//
//go:embed sql/workflow_evidence__issue_pin.sql
var queryWorkflowEvidenceIssuePin string
