package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// workflowResultFixtureProof builds exact provider bytes rather than using the framed digest helper.
// workflowResultFixtureProof создаёт exact provider bytes, не используя framed digest helper.
func workflowResultFixtureProof() workflowIntegrationResult {
	summary := `{"number":7,"state":"open","sha":"` + strings.Repeat("a", 40) + `","draft":false,"title":"PR","body":"Source","head":"feature","base":"main","url":"https://github.com/acme/repo/pull/7"}`
	return workflowIntegrationResult{Operation: "github.pull_request.create", Input: []byte(`{"head":"feature"}`), Scope: []byte(`{"owner":"acme","repository":"repo"}`), Summary: summary, Digest: workflowResultSHA([]byte(summary)), ProviderRef: "github-pull:7"}
}

// workflowResultFixtureInputs uses the existing frozen dependency receipt shape with one PR identity.
// workflowResultFixtureInputs использует existing frozen dependency receipt shape с одной PR identity.
func workflowResultFixtureInputs() []workflowResultDependency {
	return []workflowResultDependency{{StepKey: "author", SourceNodeID: "owner-source", Receipt: entity.WorkflowAttemptResult{Attempt: 1, Accepted: true, Results: []entity.WorkflowAcceptedResult{{Kind: "PULL_REQUEST", Repository: "acme/repo", Number: 7, HeadSHA: strings.Repeat("a", 40)}}}}}
}

// TestWorkflowIntegrationResultPredicates accepts only native provider proof with exact bytes,
// repository, response identity and operation semantics; URLs alone never satisfy the predicate.
// TestWorkflowIntegrationResultPredicates принимает native provider proof лишь с exact bytes,
// repository/response identity/operation semantics; одного URL недостаточно.
func TestWorkflowIntegrationResultPredicates(t *testing.T) {
	requirement := entity.WorkflowResultRequirement{Key: "pr", Kind: "PULL_REQUEST", Repository: "acme/repo"}
	declaration := entity.WorkflowDeclaredResult{Key: "pr", ReceiptRef: "erc_fixture123"}
	for _, fault := range []string{"valid", "framed-digest", "wrong-repository", "url-only", "missing-draft", "null-draft", "draft", "closed", "bad-sha", "wrong-provider", "wrong-read-number", "duplicate-input", "case-input", "valid-read", "valid-update", "duplicate-key", "case-key", "trailing", "unsafe-number", "wrong-operation"} {
		t.Run(fault, func(t *testing.T) {
			proof := workflowResultFixtureProof()
			switch fault {
			case "framed-digest":
				proof.Digest = digestBytes([]byte(proof.Summary))
			case "wrong-repository":
				proof.Scope = []byte(`{"owner":"foreign","repository":"repo"}`)
			case "url-only":
				proof.Summary = `{"url":"https://github.com/acme/repo/pull/7"}`
			case "missing-draft":
				proof.Summary = strings.Replace(proof.Summary, `,"draft":false`, "", 1)
			case "null-draft":
				proof.Summary = strings.Replace(proof.Summary, `"draft":false`, `"draft":null`, 1)
			case "draft":
				proof.Summary = strings.Replace(proof.Summary, `"draft":false`, `"draft":true`, 1)
			case "closed":
				proof.Summary = strings.Replace(proof.Summary, `"state":"open"`, `"state":"closed"`, 1)
			case "bad-sha":
				proof.Summary = strings.Replace(proof.Summary, strings.Repeat("a", 40), "main", 1)
			case "wrong-provider":
				proof.ProviderRef = "github-pull:8"
			case "wrong-read-number":
				proof.Operation = "github.pull_request.read"
				proof.Input = []byte(`{"pull_request_number":8}`)
			case "duplicate-input":
				proof.Operation = "github.pull_request.read"
				proof.Input = []byte(`{"pull_request_number":8,"pull_request_number":7}`)
			case "case-input":
				proof.Operation = "github.pull_request.read"
				proof.Input = []byte(`{"Pull_Request_Number":7}`)
			case "valid-read":
				proof.Operation = "github.pull_request.read"
				proof.Input = []byte(`{"pull_request_number":7}`)
			case "valid-update":
				proof.Operation = "github.pull_request.update"
				proof.Input = []byte(`{"pull_request_number":7,"title":"Updated"}`)
			case "duplicate-key":
				proof.Summary = strings.Replace(proof.Summary, `"draft":false`, `"draft":true,"draft":false`, 1)
			case "case-key":
				proof.Summary = strings.Replace(proof.Summary, `"draft":false`, `"Draft":false`, 1)
			case "trailing":
				proof.Summary += " {}"
			case "unsafe-number":
				proof.Summary = strings.Replace(proof.Summary, `"number":7`, `"number":9007199254740992`, 1)
			case "wrong-operation":
				proof.Operation = "github.repository.bundle.publish"
			}
			if fault != "framed-digest" {
				proof.Digest = workflowResultSHA([]byte(proof.Summary))
			}
			result, reason := acceptWorkflowIntegrationResult(requirement, declaration, proof, nil)
			if fault == "valid" || fault == "valid-read" || fault == "valid-update" {
				if reason != "" || result.Number != 7 || result.HeadSHA != strings.Repeat("a", 40) || result.ReceiptRef != declaration.ReceiptRef || result.Digest != proof.Digest {
					t.Fatal("verified PR lost immutable pins")
				}
			} else if reason == "" || result.ReceiptRef != "" {
				t.Fatal("unverified PR accepted")
			}
		})
	}
}

// TestWorkflowMergeRequiresExactAcceptedSubject binds a successful merge receipt to the accepted
// dependency PR/head pair, rejecting provider success for another request or unapproved verdict.
// TestWorkflowMergeRequiresExactAcceptedSubject связывает merge receipt с accepted dependency
// PR/head pair, отвергая provider success другого запроса или неподтверждённый verdict.
func TestWorkflowMergeRequiresExactAcceptedSubject(t *testing.T) {
	requirement := entity.WorkflowResultRequirement{Key: "merge", Kind: "INTEGRATION_RECEIPT", Repository: "acme/repo", SubjectStepKey: "author", Operation: "github.pull_request.merge"}
	for _, fault := range []string{"valid", "wrong-head", "wrong-number", "not-merged", "missing-merged", "foreign-subject", "rejected-subject", "duplicate-input", "ambiguous-subject", "unproved-subject-kind"} {
		t.Run(fault, func(t *testing.T) {
			inputs := workflowResultFixtureInputs()
			proof := workflowResultFixtureProof()
			proof.Operation = requirement.Operation
			proof.Input = []byte(`{"pull_request_number":7,"sha":"` + strings.Repeat("a", 40) + `"}`)
			proof.Summary = `{"merged":true,"sha":"` + strings.Repeat("b", 40) + `"}`
			switch fault {
			case "wrong-head":
				proof.Input = []byte(`{"pull_request_number":7,"sha":"` + strings.Repeat("c", 40) + `"}`)
			case "wrong-number":
				proof.Input = []byte(`{"pull_request_number":8,"sha":"` + strings.Repeat("a", 40) + `"}`)
			case "not-merged":
				proof.Summary = strings.Replace(proof.Summary, "true", "false", 1)
			case "missing-merged":
				proof.Summary = `{"sha":"` + strings.Repeat("b", 40) + `"}`
			case "foreign-subject":
				inputs[0].StepKey = "foreign"
			case "rejected-subject":
				inputs[0].Receipt.Accepted = false
			case "duplicate-input":
				inputs = append(inputs, inputs[0])
			case "ambiguous-subject":
				inputs[0].Receipt.Results = append(inputs[0].Receipt.Results, entity.WorkflowAcceptedResult{Kind: "PULL_REQUEST", Repository: "acme/repo", Number: 8, HeadSHA: strings.Repeat("a", 40)})
			case "unproved-subject-kind":
				inputs[0].Receipt.Results[0].Kind = "ARTIFACT"
			}
			proof.Digest = workflowResultSHA([]byte(proof.Summary))
			result, reason := acceptWorkflowIntegrationResult(requirement, entity.WorkflowDeclaredResult{Key: "merge", ReceiptRef: "erc_merge123"}, proof, inputs)
			if fault == "valid" {
				if reason != "" || result.Number != 7 || result.HeadSHA != strings.Repeat("b", 40) {
					t.Fatal("merge receipt lost subject pins")
				}
			} else if reason == "" {
				t.Fatal("merge accepted without the exact prerequisite")
			}
		})
	}
}

// workflowEvaluatorRow exposes only the native query projection or a controlled SQL error.
// workflowEvaluatorRow возвращает native query projection либо управляемую SQL error.
type workflowEvaluatorRow struct {
	values []any
	err    error
}

// Scan assigns the finite projection without granting authority from test payload fields.
// Scan назначает конечную projection, не выводя authority из test payload fields.
func (row workflowEvaluatorRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(dest) != len(row.values) {
		return errors.New("fixture column mismatch")
	}
	for i, value := range row.values {
		switch target := dest[i].(type) {
		case *[]byte:
			*target = value.([]byte)
		case *string:
			*target = value.(string)
		case *int64:
			*target = value.(int64)
		default:
			return errors.New("fixture column type mismatch")
		}
	}
	return nil
}

// workflowEvaluatorRows feeds real resolver iteration, including unknown lineage states and errors.
// workflowEvaluatorRows обслуживает настоящий resolver iterator, включая unknown lineage и errors.
type workflowEvaluatorRows struct {
	pgx.Rows
	rows  [][]any
	index int
	err   error
}

// Next preserves a bounded owner result set; it does not manufacture missing matches.
// Next сохраняет bounded owner result set, не создавая отсутствующие matches.
func (rows *workflowEvaluatorRows) Next() bool { rows.index++; return rows.index <= len(rows.rows) }

// Scan delegates the real resolver's finite column list to the controlled owner row.
// Scan передаёт конечные колонки настоящего resolver управляемой owner row.
func (rows *workflowEvaluatorRows) Scan(dest ...any) error {
	return (workflowEvaluatorRow{values: rows.rows[rows.index-1]}).Scan(dest...)
}

// Err returns a selected iteration failure instead of treating outage as absence.
// Err возвращает выбранный iteration failure, не считая outage отсутствием.
func (rows *workflowEvaluatorRows) Err() error { return rows.err }

// Close releases the local fixture without changing transaction or owner state.
// Close освобождает локальную fixture без изменения transaction или owner state.
func (rows *workflowEvaluatorRows) Close() {}

// workflowEvaluatorTx stubs only read boundaries; embedded write methods intentionally cannot run.
// workflowEvaluatorTx подменяет лишь read boundaries; embedded write methods не могут исполняться.
type workflowEvaluatorTx struct {
	pgx.Tx
	context    workflowEvaluatorRow
	rows       *workflowEvaluatorRows
	queryError error
	queries    []string
}

// QueryRow verifies that the public evaluator obtains owner context before interpreting output.
// QueryRow проверяет чтение owner context до интерпретации model output.
func (tx *workflowEvaluatorTx) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	tx.queries = append(tx.queries, query)
	return tx.context
}

// Query records artifact delegation or native receipt lookup and preserves explicit SQL failure.
// Query отмечает artifact delegation либо native receipt lookup и сохраняет SQL failure.
func (tx *workflowEvaluatorTx) Query(_ context.Context, query string, _ ...any) (pgx.Rows, error) {
	tx.queries = append(tx.queries, query)
	return tx.rows, tx.queryError
}

// workflowEvaluatorFixture builds an owner-context projection with the published predicate and inputs.
// workflowEvaluatorFixture создаёт owner-context projection с published predicate и inputs.
func workflowEvaluatorFixture(kind string) *workflowEvaluatorTx {
	requirement := entity.WorkflowResultRequirement{Key: "result", Kind: kind, FileName: "report.md", MediaType: "text/markdown"}
	switch kind {
	case "PULL_REQUEST":
		requirement = entity.WorkflowResultRequirement{Key: "result", Kind: kind, Repository: "acme/repo"}
	case "REVIEW", "QA":
		requirement = entity.WorkflowResultRequirement{Key: "result", Kind: kind, SubjectStepKey: "author"}
	case "DEPLOYMENT":
		requirement = entity.WorkflowResultRequirement{Key: "result", Kind: kind, SubjectStepKey: "author", Repository: "acme/repo", Operation: "ssh.compose.deploy", TargetRef: "ssh_fixture"}
	}
	step := entity.WorkflowStep{Key: "step", DependsOn: []string{"author"}, ResultRequirements: []entity.WorkflowResultRequirement{requirement}}
	raw, _ := json.Marshal(step)
	inputs, _ := json.Marshal(workflowResultFixtureInputs())
	return &workflowEvaluatorTx{context: workflowEvaluatorRow{values: []any{raw, inputs, "project-owner", "SUCCEEDED"}}, rows: &workflowEvaluatorRows{}}
}

// TestWorkflowEvaluatorOwnsContextAndRejectsUnprovenEvidence covers the actual public evaluator,
// including artifact resolver reuse, review verdicts and the missing native deployment evidence.
// TestWorkflowEvaluatorOwnsContextAndRejectsUnprovenEvidence проверяет настоящий evaluator,
// включая переиспользование artifact resolver, review verdicts и отсутствующее native deployment evidence.
func TestWorkflowEvaluatorOwnsContextAndRejectsUnprovenEvidence(t *testing.T) {
	r := &Repository{}
	for _, mode := range []string{"artifact", "artifact-missing", "review", "qa-changes", "review-mismatch", "invalid-declaration", "execution-failed", "owner-mismatch", "sql-error", "artifact-outage", "deployment-missing"} {
		t.Run(mode, func(t *testing.T) {
			kind := "ARTIFACT"
			if strings.HasPrefix(mode, "review") {
				kind = "REVIEW"
			}
			if mode == "qa-changes" {
				kind = "QA"
			}
			if mode == "deployment-missing" {
				kind = "DEPLOYMENT"
			}
			tx := workflowEvaluatorFixture(kind)
			summary := `{"schema":"kodex.workflow-result.v1","results":[{"key":"result","source":{"kind":"CURRENT_COMPLETION"}}]}`
			reason := ""
			success := true
			switch mode {
			case "artifact":
				tx.rows.rows = [][]any{{"art_fixture123", "arev_fixture123", strings.Repeat("a", 64), int64(1)}}
			case "artifact-missing":
				reason = workflowArtifactRequired
			case "review":
				summary = `{"schema":"kodex.workflow-result.v1","results":[{"key":"result","headSha":"` + strings.Repeat("a", 40) + `","verdict":"PASS"}]}`
			case "qa-changes":
				summary = `{"schema":"kodex.workflow-result.v1","results":[{"key":"result","headSha":"` + strings.Repeat("a", 40) + `","verdict":"CHANGES_REQUIRED"}]}`
				reason = workflowChangesRequired
			case "review-mismatch":
				summary = `{"schema":"kodex.workflow-result.v1","results":[{"key":"result","headSha":"` + strings.Repeat("b", 40) + `","verdict":"PASS"}]}`
				reason = workflowReviewSubjectMismatch
			case "invalid-declaration":
				summary = "Published successfully https://example.invalid/pull/7"
				reason = workflowDeclarationInvalid
			case "execution-failed":
				success = false
				tx.context.values[3] = "FAILED"
				reason = workflowExecutionFailed
			case "owner-mismatch":
				tx.context.err = pgx.ErrNoRows
			case "sql-error":
				tx.context.err = errors.New("SQL_UNAVAILABLE")
			case "artifact-outage":
				tx.queryError = errors.New("SQL_UNAVAILABLE")
			case "deployment-missing":
				summary = `{"schema":"kodex.workflow-result.v1","results":[{"key":"result","receiptRef":"erc_fixture123"}]}`
				reason = workflowReceiptRequired
			}
			results, actual, err := r.evaluateWorkflowStepResultTx(t.Context(), tx, scope{organizationID: "tenant-owner"}, map[string]any{}, command.CompleteExecutionInput{Success: success, ResultSummary: summary})
			if mode == "owner-mismatch" {
				if !errors.Is(err, errs.ErrConflict) {
					t.Fatal("missing owner context became a model result")
				}
				return
			}
			if mode == "sql-error" || mode == "artifact-outage" {
				if !errors.Is(err, errs.ErrUnavailable) || len(results) != 0 {
					t.Fatal("SQL outage became business absence/success")
				}
				return
			}
			if err != nil || actual != reason || reason != "" && len(results) != 0 || reason == "" && len(results) != 1 {
				t.Fatal("business evaluator outcome changed")
			}
			if mode == "artifact" && (len(tx.queries) != 2 || tx.queries[1] != queryWorkflowResultArtifact) {
				t.Fatal("current artifact bypassed existing owner resolver")
			}
		})
	}
}

// TestWorkflowReceiptReaderClosesAmbiguity checks the real read/mapping path for closed SQL reasons,
// duplicate candidates and unknown states instead of silently accepting an ambiguous revision.
// TestWorkflowReceiptReaderClosesAmbiguity проверяет настоящий read/mapping для closed SQL reasons,
// duplicate candidates и unknown states без молчаливого принятия ambiguous revision.
func TestWorkflowReceiptReaderClosesAmbiguity(t *testing.T) {
	for _, mode := range []string{"valid", "missing", "ambiguous", "invalid-lineage", "unknown-state", "duplicate", "iteration-error"} {
		t.Run(mode, func(t *testing.T) {
			proof := workflowResultFixtureProof()
			failure := ""
			if mode == "ambiguous" {
				failure = workflowReceiptLineageAmbiguous
			}
			if mode == "invalid-lineage" {
				failure = workflowReceiptLineageInvalid
			}
			if mode == "unknown-state" {
				failure = "UNREGISTERED_REASON"
			}
			row := []any{proof.Operation, proof.Input, proof.Scope, proof.Summary, proof.Digest, proof.ProviderRef, failure}
			tx := workflowEvaluatorFixture("PULL_REQUEST")
			tx.rows.rows = [][]any{row}
			if mode == "missing" {
				tx.rows.rows = nil
			}
			if mode == "duplicate" {
				tx.rows.rows = append(tx.rows.rows, row)
			}
			if mode == "iteration-error" {
				tx.rows.err = errors.New("SQL_UNAVAILABLE")
			}
			_, reason, err := readWorkflowIntegrationResultTx(t.Context(), tx, scope{}, map[string]any{}, "erc_fixture123")
			switch mode {
			case "valid":
				if err != nil || reason != "" {
					t.Fatal("verified receipt rejected")
				}
			case "unknown-state", "iteration-error":
				if !errors.Is(err, errs.ErrUnavailable) {
					t.Fatal("unknown owner state/outage downgraded to absence")
				}
			default:
				if err != nil || reason == "" {
					t.Fatal("unproven receipt accepted")
				}
			}
		})
	}
}
