package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	workflowservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/workflow"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// workflowPDLCOrderRow represents the independent chronology query without fabricating receipt authority.
// workflowPDLCOrderRow представляет отдельный запрос хронологии без имитации полномочий receipt.
type workflowPDLCOrderRow struct {
	ordered bool
	err     error
}

// Scan retains database failure separately from a proved negative ordering result.
// Scan сохраняет отказ БД отдельно от доказанного отрицательного результата хронологии.
func (row workflowPDLCOrderRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(dest) != 1 {
		return errors.New("unexpected chronology projection")
	}
	*dest[0].(*bool) = row.ordered
	return nil
}

// workflowPDLCEvaluatorTx substitutes only owner reads for the composed production evaluator.
// All write methods remain unavailable; these tests do not attest PostgreSQL lineage semantics.
// workflowPDLCEvaluatorTx подменяет только owner reads составленного production evaluator.
// Запись недоступна; эти тесты не подтверждают PostgreSQL semantics проверки lineage.
type workflowPDLCEvaluatorTx struct {
	workflowEvaluatorTx
	proofs      map[string]workflowIntegrationResult
	order       workflowPDLCOrderRow
	orderChecks int
}

// Query supplies a result only for the exact locator passed to the canonical receipt readers.
// Query возвращает результат только для точного locator канонического receipt reader.
func (tx *workflowPDLCEvaluatorTx) Query(_ context.Context, query string, args ...any) (pgx.Rows, error) {
	if query != queryWorkflowEffectResult && query != queryWorkflowResultIntegration {
		return nil, errors.New("unexpected PDLC receipt query")
	}
	pins := args[0].(pgx.StrictNamedArgs)
	proof, found := tx.proofs[pins["receipt_ref"].(string)]
	if !found {
		return &workflowEvaluatorRows{}, nil
	}
	return &workflowEvaluatorRows{rows: [][]any{{proof.Operation, proof.Input, proof.Scope, proof.Summary, proof.Digest, proof.ProviderRef, proof.LineageFailure}}}, nil
}

// QueryRow separates the immutable step context from subsequent receipt chronology checks.
// QueryRow отделяет immutable step context от последующей проверки хронологии receipts.
func (tx *workflowPDLCEvaluatorTx) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	if query == queryWorkflowResultLineage {
		return tx.context
	}
	if !strings.HasPrefix(query, "-- name: workflow_pdlc__read_order :one") {
		return workflowPDLCOrderRow{err: errors.New("unexpected PDLC owner query")}
	}
	pins := args[0].(pgx.StrictNamedArgs)
	if pins["organization_id"] != "owner" || pins["write_receipt_ref"] != "erc_write0000" || pins["head_receipt_ref"] != "erc_head00000" || pins["content_receipt_ref"] != "erc_content00" {
		return workflowPDLCOrderRow{err: errors.New("chronology lost owner receipt pins")}
	}
	tx.orderChecks++
	return tx.order
}

// TestWorkflowPDLCComposedFileEvidence requires all three owner-resolved receipts and a separate
// ordered-read proof; a completed model turn or one successful external write cannot advance DAG.
// TestWorkflowPDLCComposedFileEvidence требует три owner-resolved receipts и отдельный ordered-read
// proof; завершение model turn либо одна успешная внешняя запись не продвигают DAG.
func TestWorkflowPDLCComposedFileEvidence(t *testing.T) {
	for _, mode := range []string{"valid", "write-missing", "head-missing", "content-missing", "foreign-lineage", "before-write", "order-outage"} {
		t.Run(mode, func(t *testing.T) {
			r, d, proofs := workflowPDLCFileFixture([]byte("Требования к журналу обращений\n"))
			step := entity.WorkflowStep{Key: "analysis", ResultRequirements: []entity.WorkflowResultRequirement{r}}
			tx := &workflowPDLCEvaluatorTx{
				workflowEvaluatorTx: workflowEvaluatorTx{context: workflowEvaluatorRow{values: []any{asJSON(step), []byte(`[]`), "project-owner", "SUCCEEDED"}}},
				proofs:              map[string]workflowIntegrationResult{d.ReceiptRef: proofs[0], d.HeadReceiptRef: proofs[1], d.ContentReceiptRef: proofs[2]},
				order:               workflowPDLCOrderRow{ordered: true},
			}
			switch mode {
			case "write-missing":
				delete(tx.proofs, d.ReceiptRef)
			case "head-missing":
				delete(tx.proofs, d.HeadReceiptRef)
			case "content-missing":
				delete(tx.proofs, d.ContentReceiptRef)
			case "foreign-lineage":
				proofs[2].LineageFailure = workflowReceiptLineageInvalid
				tx.proofs[d.ContentReceiptRef] = proofs[2]
			case "before-write":
				tx.order.ordered = false
			case "order-outage":
				tx.order.err = errors.New("SQL_UNAVAILABLE")
			}
			declaration := entity.WorkflowResultDeclaration{Schema: "kodex.workflow-result.v1", Results: []entity.WorkflowDeclaredResult{d}}
			actual, err := (&Repository{}).evaluateWorkflowStepResultDetailedForProfileTx(t.Context(), tx, scope{organizationID: "owner"}, map[string]any{}, command.CompleteExecutionInput{Success: true, ResultSummary: string(asJSON(declaration))}, false, workflowservice.GitHubPDLCV1)
			if mode == "order-outage" {
				if !errors.Is(err, errs.ErrUnavailable) || len(actual.Results) != 0 {
					t.Fatal("chronology outage became absence or accepted evidence")
				}
				return
			}
			if err != nil {
				t.Fatal("PDLC owner evaluation failed", err)
			}
			if mode == "valid" {
				if actual.Reason != "" || len(actual.Results) != 1 || actual.Results[0].ContentReceiptRef != d.ContentReceiptRef || tx.orderChecks != 1 {
					t.Fatalf("published native file was not accepted with ordered reads: reason=%s checks=%d", actual.Reason, tx.orderChecks)
				}
			} else if actual.Reason == "" || len(actual.Results) != 0 || len(actual.Rework) != 0 || len(actual.ReviewEvidence) != 0 {
				t.Fatal("unproved file evidence advanced the workflow")
			}
		})
	}
}

// TestWorkflowPDLCDeveloperHeadMatchesPublishedFile prevents an earlier Ready PR receipt from
// selecting an obsolete head while the same step proves newly published file bytes at another head.
// TestWorkflowPDLCDeveloperHeadMatchesPublishedFile запрещает прежнему Ready PR receipt выбирать
// устаревший head, когда этот же шаг подтверждает новые bytes файла на другом head.
func TestWorkflowPDLCDeveloperHeadMatchesPublishedFile(t *testing.T) {
	for _, mode := range []string{"same-head", "old-head", "old-head-reversed"} {
		t.Run(mode, func(t *testing.T) {
			r, d, proofs := workflowPDLCFileFixture([]byte("Реализация сервиса\n"))
			ready := proofs[1]
			ready.Summary = strings.Replace(ready.Summary, `"draft":true`, `"draft":false`, 1)
			if mode != "same-head" {
				ready.Summary = strings.Replace(ready.Summary, strings.Repeat("b", 40), strings.Repeat("a", 40), 1)
			}
			ready.Digest = workflowResultSHA([]byte(ready.Summary))
			pr := entity.WorkflowResultRequirement{Key: "pr", Kind: "PULL_REQUEST", Repository: r.Repository, TargetRef: r.TargetRef}
			requirements := []entity.WorkflowResultRequirement{r, pr}
			declarations := []entity.WorkflowDeclaredResult{d, {Key: "pr", ReceiptRef: "erc_readyread"}}
			if mode == "old-head-reversed" {
				requirements[0], requirements[1] = requirements[1], requirements[0]
				declarations[0], declarations[1] = declarations[1], declarations[0]
			}
			step := entity.WorkflowStep{Key: "implementation", ResultRequirements: requirements}
			tx := &workflowPDLCEvaluatorTx{
				workflowEvaluatorTx: workflowEvaluatorTx{context: workflowEvaluatorRow{values: []any{asJSON(step), []byte(`[]`), "project-owner", "SUCCEEDED"}}},
				proofs:              map[string]workflowIntegrationResult{d.ReceiptRef: proofs[0], d.HeadReceiptRef: proofs[1], d.ContentReceiptRef: proofs[2], "erc_readyread": ready},
				order:               workflowPDLCOrderRow{ordered: true},
			}
			declaration := entity.WorkflowResultDeclaration{Schema: "kodex.workflow-result.v1", Results: declarations}
			actual, err := (&Repository{}).evaluateWorkflowStepResultDetailedForProfileTx(t.Context(), tx, scope{organizationID: "owner"}, map[string]any{}, command.CompleteExecutionInput{Success: true, ResultSummary: string(asJSON(declaration))}, false, workflowservice.GitHubPDLCV1)
			if err != nil {
				t.Fatal("developer result evaluation unavailable", err)
			}
			if mode == "same-head" {
				if actual.Reason != "" || len(actual.Results) != 2 {
					t.Fatal("matching native file and Ready head rejected")
				}
			} else if actual.Reason == "" || len(actual.Results) != 0 {
				t.Fatal("obsolete Ready head accepted beside current file evidence")
			}
		})
	}
}

// TestWorkflowPDLCReviewRequiresNativeReceipt prevents the new profile from falling back to
// historical model-only verdict/head acceptance when no native review receipt exists.
// TestWorkflowPDLCReviewRequiresNativeReceipt запрещает новому профилю возвращаться к прежней
// приёмке model-only verdict/head при отсутствии native review receipt.
func TestWorkflowPDLCReviewRequiresNativeReceipt(t *testing.T) {
	tx := workflowEvaluatorFixture("REVIEW")
	declaration := entity.WorkflowResultDeclaration{Schema: "kodex.workflow-result.v1", Results: []entity.WorkflowDeclaredResult{{Key: "result", HeadSHA: strings.Repeat("a", 40), Verdict: "PASS", ReceiptRef: "erc_missing00"}}}
	actual, err := (&Repository{}).evaluateWorkflowStepResultDetailedForProfileTx(t.Context(), tx, scope{}, map[string]any{}, command.CompleteExecutionInput{Success: true, ResultSummary: string(asJSON(declaration))}, false, workflowservice.GitHubPDLCV1)
	if err != nil || actual.Reason != workflowReceiptRequired || len(actual.Results) != 0 {
		t.Fatal("model-only verdict bypassed native PDLC review evidence", err)
	}
}
