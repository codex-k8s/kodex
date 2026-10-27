package platform

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	workflowservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/workflow"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// workflowEvidenceSubjectFixture supplies a pinned accepted input for pure boundary tests;
// it does not replace the native fixture proving how that owner input is created.
// workflowEvidenceSubjectFixture задаёт pinned accepted input для pure boundary tests;
// она не заменяет native fixture создания этого owner input.
func workflowEvidenceSubjectFixture() []workflowResultDependency {
	return []workflowResultDependency{{StepKey: "author", Receipt: entity.WorkflowAttemptResult{Accepted: true, Results: []entity.WorkflowAcceptedResult{{Key: "pr", Kind: "PULL_REQUEST", Repository: "owner/repo", Number: 42, HeadSHA: strings.Repeat("a", 40)}}}}}
}

// TestWorkflowEvidenceSlots rejects global writes, self-approval and subject drift before
// native allocation while preserving the older profile's denial of these new operations.
// TestWorkflowEvidenceSlots отвергает global WRITE, self-approval и subject drift до native
// allocation, сохраняя отказ прежнего профиля для новых operations.
func TestWorkflowEvidenceSlots(t *testing.T) {
	inputs := workflowEvidenceSubjectFixture()
	step := entity.WorkflowStep{DependsOn: []string{"author"}, ResultRequirements: []entity.WorkflowResultRequirement{{Key: "review", Kind: "REVIEW", SubjectStepKey: "author"}}}
	scope := []byte(`{"owner":"owner","repository":"repo"}`)
	input := `{"pull_request_number":42,"sha":"` + strings.Repeat("a", 40) + `","event":"COMMENT","body":"Проверено"}`
	if key, err := workflowEffectSlotForProfile(workflowservice.GitHubReviewQAV1, step, inputs, "github.pull_request.review.create", scope, []byte(input)); err != nil || key != "review" {
		t.Fatal("exact review slot unavailable", err)
	}
	if _, err := workflowEffectSlot(step, inputs, "github.pull_request.review.create", scope, []byte(input)); err == nil {
		t.Fatal("legacy policy widened")
	}
	for _, bad := range []string{strings.Replace(input, "COMMENT", "APPROVE", 1), strings.Replace(input, "COMMENT", "REQUEST_CHANGES", 1), strings.Replace(input, strings.Repeat("a", 40), strings.Repeat("b", 40), 1), strings.Replace(input, ":42", ":43", 1), strings.Replace(input, "Проверено", " ", 1)} {
		if _, err := workflowEffectSlotForProfile(workflowservice.GitHubReviewQAV1, step, inputs, "github.pull_request.review.create", scope, []byte(bad)); err == nil {
			t.Fatal("wrong review subject/event admitted")
		}
	}
	for _, operation := range []string{"github.issue.create", "github.pull_request.merge", "github.issue.comment.create"} {
		if _, err := workflowEffectSlotForProfile(workflowservice.GitHubReviewQAV1, step, inputs, operation, scope, []byte(input)); err == nil {
			t.Fatal("unbound write admitted")
		}
	}
	if _, err := workflowEffectSlotForProfile(workflowservice.GitHubReviewQAV1, step, inputs, "github.pull_request.review.create", []byte(`{"owner":"foreign","repository":"repo"}`), []byte(input)); err == nil {
		t.Fatal("foreign repository admitted")
	}
}

// TestWorkflowNativeCommentEvidence verifies actual provider projection bytes and operation
// identity rather than accepting an arbitrary URL, READ receipt or model PASS as evidence.
// TestWorkflowNativeCommentEvidence проверяет provider projection bytes и operation identity,
// не принимая URL, READ receipt либо model PASS за evidence.
func TestWorkflowNativeCommentEvidence(t *testing.T) {
	sha := strings.Repeat("a", 40)
	proof := workflowIntegrationResult{Operation: "github.pull_request.review.create", Input: []byte(`{"pull_request_number":42,"sha":"` + sha + `","event":"COMMENT","body":"Проверено"}`), Scope: []byte(`{"owner":"owner","repository":"repo"}`), Summary: `{"id":123,"body":"Проверено","state":"COMMENTED","commit_id":"` + sha + `"}`, ProviderRef: "github-review:123"}
	proof.Digest = workflowResultSHA([]byte(proof.Summary))
	req := entity.WorkflowResultRequirement{Key: "review", Kind: "REVIEW", SubjectStepKey: "author"}
	declaration := entity.WorkflowDeclaredResult{Key: "review", ReceiptRef: "erc_12345678", HeadSHA: sha, Verdict: "CHANGES_REQUIRED"}
	result, reason := acceptWorkflowCommentResult(req, declaration, proof, workflowEvidenceSubjectFixture())
	if reason != "" || result.Verdict != "CHANGES_REQUIRED" || result.Digest != proof.Digest || result.ReceiptRef != declaration.ReceiptRef {
		t.Fatal("negative native facts lost", reason)
	}
	for _, mutate := range []func(*workflowIntegrationResult){
		func(v *workflowIntegrationResult) { v.Operation = "github.pull_request.review.read" },
		func(v *workflowIntegrationResult) { v.ProviderRef = "github-review:124" },
		func(v *workflowIntegrationResult) { v.Digest = strings.Repeat("0", 64) },
		func(v *workflowIntegrationResult) {
			v.Summary = strings.Replace(v.Summary, "COMMENTED", "APPROVED", 1)
			v.Digest = workflowResultSHA([]byte(v.Summary))
		},
		func(v *workflowIntegrationResult) {
			v.Summary = strings.Replace(v.Summary, "Проверено", "Другой текст", 1)
			v.Digest = workflowResultSHA([]byte(v.Summary))
		},
		func(v *workflowIntegrationResult) {
			v.Input = []byte(strings.Replace(string(v.Input), sha, strings.Repeat("b", 40), 1))
		},
	} {
		changed := proof
		mutate(&changed)
		if _, why := acceptWorkflowCommentResult(req, declaration, changed, workflowEvidenceSubjectFixture()); why == "" {
			t.Fatal("unrelated or changed native proof accepted")
		}
	}
}

// TestWorkflowQAIssueEvidence distinguishes real defect publication from a PASS report and
// requires the exact captured request; the database reader separately binds the report pin.
// TestWorkflowQAIssueEvidence отличает реальную публикацию дефекта от PASS report и требует
// exact captured request; database reader отдельно связывает report pin.
func TestWorkflowQAIssueEvidence(t *testing.T) {
	report := workflowservice.QAReport{Repository: "owner/repo", Verdict: "CHANGES_REQUIRED", Findings: []workflowservice.QAFinding{{Key: "bug", Summary: "Ошибка"}}, Issue: &workflowservice.QAIssue{Title: "Дефект", Body: "Шаги"}}
	proof := workflowIntegrationResult{Operation: "github.issue.create", Scope: []byte(`{"owner":"owner","repository":"repo"}`), Input: asJSON(report.Issue), Summary: `{"number":7,"title":"Дефект","state":"open"}`, ProviderRef: "github-issue:7"}
	proof.Digest = workflowResultSHA([]byte(proof.Summary))
	if !workflowQAIssueReceipt(proof, report) {
		t.Fatal("native issue unavailable")
	}
	changed := report
	changed.Verdict = "PASS"
	if workflowQAIssueReceipt(proof, changed) {
		t.Fatal("PASS required a fabricated bug")
	}
	changed = report
	changed.Issue = &workflowservice.QAIssue{Title: "Дефект", Body: "Другие шаги"}
	if workflowQAIssueReceipt(proof, changed) {
		t.Fatal("unrelated report adopted issue")
	}
	before, _ := workflowEffectSemanticDigest("qa", "github.issue.create", "scope", asJSON([]any{"kodex.workflow-qa-issue.v1", json.RawMessage(proof.Input), workflowQAReportPin{Ref: "art_12345678", RevisionRef: "arv_12345678", Revision: 1, Digest: "sha256:" + strings.Repeat("a", 64)}}))
	after, _ := workflowEffectSemanticDigest("qa", "github.issue.create", "scope", asJSON([]any{"kodex.workflow-qa-issue.v1", json.RawMessage(proof.Input), workflowQAReportPin{Ref: "art_12345678", RevisionRef: "arv_12345678", Revision: 2, Digest: "sha256:" + strings.Repeat("b", 64)}}))
	if before == after {
		t.Fatal("new report lost semantic identity")
	}
}

// TestWorkflowEvidenceContinuationEnvelope retains every failure/receipt and the original task;
// overflow is a typed durable delivery outcome rather than truncated evidence or another effect.
// TestWorkflowEvidenceContinuationEnvelope сохраняет каждый failure/receipt и исходный task;
// overflow — typed durable delivery outcome, не усечение evidence либо повторный effect.
func TestWorkflowEvidenceContinuationEnvelope(t *testing.T) {
	snapshot := asJSON(map[string]any{"schema": "kodex.workflow-evidence-outcomes.v1", "task": "Original published stage and final declaration.", "nativeOutcomes": []map[string]any{{"invocationRef": "inv_12345678", "state": "FAILED", "safeErrorCode": "INTEGRATION_REQUEST_REJECTED"}, {"invocationRef": "inv_23456789", "state": "SUCCEEDED", "receiptRef": "erc_12345678", "resultSummary": "Exact result"}}})
	task, err := workflowEvidenceContinuationTask(snapshot)
	if err != nil || !strings.Contains(task, "Original published stage") || !strings.Contains(task, "FAILED") || !strings.Contains(task, "erc_12345678") {
		t.Fatal("continuation omitted owner fact", err)
	}
	oversized := asJSON(map[string]any{"schema": "kodex.workflow-evidence-outcomes.v1", "task": strings.Repeat("x", 32768), "nativeOutcomes": []map[string]any{{"state": "SUCCEEDED"}}})
	if _, err := workflowEvidenceContinuationTask(oversized); !errors.Is(err, errWorkflowEvidenceDeliveryLimit) {
		t.Fatal("oversized evidence silently truncated")
	}
}
