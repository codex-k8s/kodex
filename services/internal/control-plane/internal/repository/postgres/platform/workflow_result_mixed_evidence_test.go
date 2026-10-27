package platform

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	workflowservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/workflow"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// mixedWorkflowEvidenceRows preserves the real artifact resolver's boolean projection.
// mixedWorkflowEvidenceRows сохраняет boolean projection настоящего artifact resolver.
type mixedWorkflowEvidenceRows struct {
	workflowEvaluatorRows
	exact bool
}

// Scan exposes only an owner match; it cannot turn a changed locator into matching evidence.
// Scan возвращает только owner match, не превращая изменённый locator в evidence.
func (rows *mixedWorkflowEvidenceRows) Scan(dest ...any) error {
	*dest[0].(*bool) = rows.exact
	return nil
}

// mixedWorkflowEvidenceTx routes read projections for the composed production evaluator.
// mixedWorkflowEvidenceTx маршрутизирует read projections составленного production evaluator.
type mixedWorkflowEvidenceTx struct {
	workflowEvaluatorTx
	missingReceipt bool
	expectedDigest string
	proof          workflowIntegrationResult
}

// Query checks exact numeric revision and digest before supplying the artifact owner match;
// receipt absence remains absence through both canonical receipt readers.
// Query сверяет numeric revision и digest до owner match; отсутствующий receipt остаётся
// отсутствующим в обоих canonical receipt readers.
func (tx *mixedWorkflowEvidenceTx) Query(_ context.Context, query string, args ...any) (pgx.Rows, error) {
	tx.queries = append(tx.queries, query)
	if query == queryWorkflowArtifactReview {
		pins := args[0].(pgx.StrictNamedArgs)
		if pins["artifact_revision"] != int64(1) || pins["artifact_digest"] != "sha256:"+tx.expectedDigest {
			return &workflowEvaluatorRows{}, nil
		}
		return &mixedWorkflowEvidenceRows{workflowEvaluatorRows: workflowEvaluatorRows{rows: [][]any{{true}}}, exact: true}, nil
	}
	if query == queryWorkflowEffectResult || query == queryWorkflowResultIntegration {
		if tx.missingReceipt {
			return &workflowEvaluatorRows{}, nil
		}
		p := tx.proof
		return &workflowEvaluatorRows{rows: [][]any{{p.Operation, p.Input, p.Scope, p.Summary, p.Digest, p.ProviderRef, ""}}}, nil
	}
	panic("unexpected mixed evidence query")
}

// TestWorkflowMixedArtifactReviewDoesNotWaiveNativeComment evaluates both mandatory predicates.
// It preserves exact negative facts and rework routing only after every owner lookup succeeds.
// TestWorkflowMixedArtifactReviewDoesNotWaiveNativeComment проверяет оба обязательных predicate.
// Exact negative facts и rework routing сохраняются лишь после успешных owner lookup.
func TestWorkflowMixedArtifactReviewDoesNotWaiveNativeComment(t *testing.T) {
	for _, mode := range []string{"valid", "missing-comment", "changed-revision", "changed-digest", "legacy"} {
		t.Run(mode, func(t *testing.T) {
			head, digest := strings.Repeat("a", 40), strings.Repeat("b", 64)
			step := entity.WorkflowStep{Key: "reviewer", DependsOn: []string{"author"}, ResultRequirements: []entity.WorkflowResultRequirement{
				{Key: "design", Kind: "ARTIFACT_REVIEW", SubjectStepKey: "author", SubjectResultKey: "spec"},
				{Key: "comment", Kind: "REVIEW", SubjectStepKey: "author"},
			}}
			inputs := workflowEvidenceSubjectFixture()
			inputs[0].Receipt.Results = append(inputs[0].Receipt.Results, entity.WorkflowAcceptedResult{Key: "spec", Kind: "ARTIFACT", ArtifactRef: "art_12345678", ArtifactRevision: "arev_12345678", Digest: digest})
			rawStep, _ := json.Marshal(step)
			rawInputs, _ := json.Marshal(inputs)
			proof := workflowIntegrationResult{Operation: "github.pull_request.review.create", Input: []byte(`{"pull_request_number":42,"sha":"` + head + `","event":"COMMENT","body":"Проверено"}`), Scope: []byte(`{"owner":"owner","repository":"repo"}`), Summary: `{"id":123,"body":"Проверено","state":"COMMENTED","commit_id":"` + head + `"}`, ProviderRef: "github-review:123"}
			proof.Digest = workflowResultSHA([]byte(proof.Summary))
			tx := &mixedWorkflowEvidenceTx{workflowEvaluatorTx: workflowEvaluatorTx{context: workflowEvaluatorRow{values: []any{rawStep, rawInputs, "project-owner", "SUCCEEDED"}}}, expectedDigest: digest, proof: proof, missingReceipt: mode == "missing-comment"}
			declaration := entity.WorkflowResultDeclaration{Schema: "kodex.workflow-result.v1", Results: []entity.WorkflowDeclaredResult{
				{Key: "design", Verdict: "CHANGES_REQUIRED", Source: &entity.WorkflowArtifactSource{Kind: "CAPTURED_ARTIFACT", ArtifactRef: "art_12345678", Revision: 1, Digest: "sha256:" + digest}},
				{Key: "comment", Verdict: "PASS", HeadSHA: head, ReceiptRef: "erc_12345678"},
			}}
			if mode == "changed-revision" {
				declaration.Results[0].Source.Revision = 2
			}
			if mode == "changed-digest" {
				declaration.Results[0].Source.Digest = "sha256:" + strings.Repeat("c", 64)
			}
			profile := workflowservice.GitHubReviewQAV1
			if mode == "legacy" {
				profile = ""
				declaration.Results[1].ReceiptRef = ""
			}
			summary, _ := json.Marshal(declaration)
			result, err := (&Repository{}).evaluateWorkflowStepResultDetailedForProfileTx(t.Context(), tx, scope{organizationID: "owner"}, map[string]any{}, command.CompleteExecutionInput{Success: true, ResultSummary: string(summary)}, true, profile)
			if err != nil {
				t.Fatal("mixed evaluator failed", err)
			}
			switch mode {
			case "valid":
				if result.Reason != workflowChangesRequired || len(result.Results) != 0 || len(result.ReviewEvidence) != 2 || len(result.Rework) != 1 || result.Rework[0].SubjectResultKey != "spec" || result.ReviewEvidence[1].ReceiptRef != "erc_12345678" {
					t.Fatal("mixed facts or owner rework lost")
				}
			case "legacy":
				if result.Reason != workflowChangesRequired || len(result.ReviewEvidence) != 0 || len(result.Rework) != 1 {
					t.Fatal("legacy output changed")
				}
			default:
				if result.Reason == workflowChangesRequired || len(result.Results) != 0 || len(result.ReviewEvidence) != 0 || len(result.Rework) != 0 {
					t.Fatal("invalid mandatory evidence produced accepted facts or rework")
				}
			}
		})
	}
}
