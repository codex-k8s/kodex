package platform

import (
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/artifactpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// workflowEvidenceQARequirements publishes the canonical MIME of the actual text log, while the
// report remains a separate JSON artifact. These fixture selectors grant no runtime authority.
// workflowEvidenceQARequirements публикует канонический MIME actual text log; report остаётся
// отдельным JSON artifact. Селекторы оснастки не предоставляют runtime authority.
func workflowEvidenceQARequirements(retention bool) []entity.WorkflowResultRequirement {
	items := []entity.WorkflowResultRequirement{
		{Key: "log", Kind: "ARTIFACT", FileName: "test-output.txt", MediaType: "text/plain"},
		{Key: "report", Kind: "ARTIFACT", FileName: "qa-report.json", MediaType: "application/json"},
		{Key: "qa", Kind: "QA", SubjectStepKey: "author", EvidenceKey: "report"},
	}
	if retention {
		items = append(items, entity.WorkflowResultRequirement{Key: "unrelated", Kind: "ARTIFACT", FileName: "unrelated-output.txt", MediaType: "text/plain"})
	}
	return items
}

// workflowEvidenceQALogFiles prepares actual UTF-8 text, never a fabricated QA execution receipt.
// Both the declared and policy-derived MIME must match the published exact artifact requirement.
// workflowEvidenceQALogFiles готовит actual UTF-8 text, не выдуманный receipt выполнения QA.
// Declared и policy-derived MIME обязаны совпадать с published exact artifact requirement.
func workflowEvidenceQALogFiles(retention bool) []command.CompletedArtifact {
	items := []command.CompletedArtifact{workflowCaptureProgressArtifact("test-output.txt", []byte("Fixture-reported command output; no real business tests are claimed.\n"))}
	if retention {
		items = append(items, workflowCaptureProgressArtifact("unrelated-output.txt", []byte("Captured by the same employee, absent from the QA report log selectors.")))
	}
	for index := range items {
		items[index].MediaType = "text/plain"
	}
	return items
}

// history reads the authoritative owner graph and selects the exact physical business run from
// the actual lease. It does not reuse the parallel fixture's fixed a/b step-key projection.
// history читает authoritative owner graph и выбирает exact physical business run из actual lease.
// Фиксированная проекция step keys a/b другой параллельной оснастки не используется.
func (fixture *workflowEvidenceNativeFixture) history(lease map[string]any) []entity.WorkflowAttemptResult {
	fixture.f.t.Helper()
	_, graph, err := fixture.f.service.GetRunGraph(fixture.f.ctx, fixture.f.owner, fixture.f.runRef)
	if err != nil {
		fixture.f.t.Fatal("read exact evidence business history", completionOutcomeClass(err))
	}
	items, err := workflowEvidenceHistoryForRun(graph, stringMap(lease, "runRef"))
	if err != nil {
		fixture.f.t.Fatal("ambiguous evidence business history")
	}
	return items
}

// workflowEvidenceHistoryForRun selects by an owner-issued run reference rather than an assumed
// step name or display name. Duplicate receipts for that business run fail the fixture closed.
// workflowEvidenceHistoryForRun выбирает по owner-issued run reference вместо предполагаемого
// имени шага либо display name. Duplicate receipts этого business run закрыто отклоняются.
func workflowEvidenceHistoryForRun(graph entity.RunGraph, runRef string) ([]entity.WorkflowAttemptResult, error) {
	if !workflowLaunchRefPattern.MatchString(runRef) {
		return nil, errs.ErrConflict
	}
	var result []entity.WorkflowAttemptResult
	for _, node := range graph.Nodes {
		for _, attempt := range node.WorkflowResults {
			if attempt.RunRef == runRef {
				result = append(result, attempt)
			}
		}
	}
	if len(result) > 1 {
		return nil, errs.ErrConflict
	}
	return result, nil
}

// TestWorkflowEvidenceFixtureQALogMIME compares the actual published selectors and completion
// bytes through the production artifact policy; a .txt/text-markdown mismatch cannot pass.
// TestWorkflowEvidenceFixtureQALogMIME сравнивает actual published selectors и completion bytes
// через production artifact policy; несовпадение .txt/text-markdown не может пройти.
func TestWorkflowEvidenceFixtureQALogMIME(t *testing.T) {
	for _, retention := range []bool{false, true} {
		for _, file := range workflowEvidenceQALogFiles(retention) {
			verdict := artifactpolicy.Inspect(file.FileName, file.MediaType, file.Content)
			matches := 0
			for _, requirement := range workflowEvidenceQARequirements(retention) {
				if requirement.Kind == "ARTIFACT" && requirement.FileName == file.FileName {
					matches++
					if verdict.ScanState != artifactpolicy.ScanClean || verdict.MediaType != requirement.MediaType || file.MediaType != requirement.MediaType {
						t.Fatal("QA log completion disagrees with published canonical MIME")
					}
				}
			}
			if matches != 1 {
				t.Fatal("QA log has no unique published artifact requirement")
			}
		}
	}
	if artifactpolicy.Inspect("test-output.txt", "text/markdown", []byte("old mismatched fixture")).MediaType != "text/plain" {
		t.Fatal("canonical text MIME policy changed")
	}
}

// TestWorkflowEvidenceHistoryUsesExactRun proves that a dynamically named check step cannot be
// mistaken for an empty a/b history and that unrelated, absent or duplicate receipts stay distinct.
// TestWorkflowEvidenceHistoryUsesExactRun доказывает, что dynamically named check step не выглядит
// пустой a/b history; unrelated, absent и duplicate receipts остаются различимыми.
func TestWorkflowEvidenceHistoryUsesExactRun(t *testing.T) {
	const selected = "run_dynamic_check"
	graph := entity.RunGraph{Nodes: []entity.RunNode{
		{DisplayName: "author", WorkflowResults: []entity.WorkflowAttemptResult{{RunRef: "run_other_author", Accepted: true}}},
		{DisplayName: "dynamic-check-not-a-or-b", WorkflowResults: []entity.WorkflowAttemptResult{{RunRef: selected, Reason: "RESULT_INVALID"}}},
	}}
	items, err := workflowEvidenceHistoryForRun(graph, selected)
	if err != nil || len(items) != 1 || items[0].Accepted || items[0].Reason != "RESULT_INVALID" {
		t.Fatal("exact dynamic business run history was silently lost")
	}
	if items, err := workflowEvidenceHistoryForRun(graph, "run_still_pending"); err != nil || len(items) != 0 {
		t.Fatal("unrelated receipt widened pending run history")
	}
	graph.Nodes = append(graph.Nodes, graph.Nodes[1])
	if _, err := workflowEvidenceHistoryForRun(graph, selected); err == nil {
		t.Fatal("duplicate business history was silently selected")
	}
}
