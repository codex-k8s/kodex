package platform

import (
	"context"
	"encoding/base64"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// TestWorkflowPDLCCompletionRPCBudgetComponent exercises the live completion shape:
// seventy settled operations, two captured files, a repository file and a comment.
// Each final commit and its exact replay must fit the unchanged five-second RPC
// budget; model success alone cannot dispatch the dependent employee.
// TestWorkflowPDLCCompletionRPCBudgetComponent проверяет форму живого completion:
// семьдесят завершённых операций, два captured файла, repository file и comment.
// Финальный commit и exact replay должны укладываться в прежние5s RPC; один
// успех модели не допускает зависимого сотрудника.
func TestWorkflowPDLCCompletionRPCBudgetComponent(t *testing.T) {
	for _, planner := range []string{"auto", "force_custom_plan", "force_generic_plan"} {
		t.Run(planner, func(t *testing.T) {
			fixture, lease := newWorkflowPDLCFileOwnerFixtureWithPlanner(t, planner,
				entity.WorkflowResultRequirement{Key: "response", Kind: "PR_COMMENT", Repository: "fixture/repository", TargetRef: "3"},
				entity.WorkflowResultRequirement{Key: "requirements", Kind: "ARTIFACT", FileName: "requirements.md", MediaType: "text/markdown"},
				entity.WorkflowResultRequirement{Key: "changes", Kind: "ARTIFACT", FileName: "requirements-changes.json", MediaType: "application/json"})
			f := fixture.f
			head, blob := strings.Repeat("a", 40), strings.Repeat("b", 40)
			content := []byte(strings.Repeat("Requirements for the issue journal.\n", 512))
			workflowPDLCSeedReadHistory(fixture, lease, head, blob, 66)
			write, err := fixture.resolve(lease, "budget-write", "github.repository.content.create", map[string]any{
				"path": "docs/requirements.md", "branch": "feature/journal", "message": "Сохранить требования", "content_base64": base64.StdEncoding.EncodeToString(content)})
			if err != nil {
				t.Fatal("admit bounded completion file write", completionOutcomeClass(err))
			}
			writeReceipt := fixture.finishEffect(write, "github-commit:"+head, string(asJSON(map[string]any{"sha": head, "path": "docs/requirements.md"})))
			pull, err := fixture.resolve(lease, "budget-head", "github.pull_request.read", map[string]any{"pull_request_number": int64(3)})
			if err != nil {
				t.Fatal("admit bounded completion head read", completionOutcomeClass(err))
			}
			headReceipt := fixture.finishEffect(pull, "github-pull:3", string(asJSON(map[string]any{"number": 3, "state": "open", "head": "feature/journal", "base": "main", "sha": head, "draft": true})))
			read, err := fixture.resolve(lease, "budget-content", "github.repository.content.read", map[string]any{"path": "docs/requirements.md", "ref": head, "maximum_bytes": int64(4096)})
			if err != nil {
				t.Fatal("admit bounded completion content read", completionOutcomeClass(err))
			}
			contentReceipt := fixture.finishEffect(read, "github-content:"+blob, string(asJSON(map[string]any{
				"path": "docs/requirements.md", "type": "file", "sha": blob, "size": len(content), "text": string(content[:4096]), "commit_sha": head,
				"offset_bytes": 0, "next_offset_bytes": 4096, "eof": false, "source_digest": "sha256:" + workflowResultSHA(content), "chunk_digest": "sha256:" + workflowResultSHA(content[:4096])})))
			comment, err := fixture.resolve(lease, "budget-comment", "github.pull_request.comment.create", map[string]any{"pull_request_number": int64(3), "body": "Требования и протокол изменений сохранены."})
			if err != nil {
				t.Fatal("admit bounded completion comment", completionOutcomeClass(err))
			}
			commentReceipt := fixture.finishEffect(comment, "github-comment:42", `{"pull_request_number":3,"comment_id":42}`)
			changes := workflowCaptureProgressArtifact("requirements-changes.json", []byte(`{"changes":[]}`))
			changes.MediaType = "application/json"
			captured := fixture.complete(lease, `{"schema":"kodex.workflow-capture-progress.v1","phase":"CAPTURE_PROGRESS","cause":"ARTIFACTS_FOR_CONTINUATION","artifactKeys":["requirements","changes"]}`,
				workflowCaptureProgressArtifact("requirements.md", content), changes)
			if len(fixture.history(lease)) != 0 || len(captured.CreatedRefs) != 2 {
				t.Fatal("physical capture substituted accepted business results")
			}
			declarations := []entity.WorkflowDeclaredResult{
				{Key: "document", ReceiptRef: writeReceipt, HeadReceiptRef: headReceipt, ContentReceiptRef: contentReceipt},
				{Key: "response", ReceiptRef: commentReceipt},
			}
			for _, ref := range captured.CreatedRefs {
				artifact, err := f.service.GetArtifact(f.ctx, f.owner, ref)
				if err != nil {
					t.Fatal("read exact captured completion file", completionOutcomeClass(err))
				}
				key := "requirements"
				if artifact.FileName == "requirements-changes.json" {
					key = "changes"
				}
				declarations = append(declarations, entity.WorkflowDeclaredResult{Key: key, Source: evidenceCapturedSource(artifact)})
			}
			previous := lease
			lease = fixture.claimOutcome(previous, f.agents["author"])
			assertClosed := workflowPDLCReadHistory(fixture, previous, lease, 70)
			payload := command.CompleteExecutionInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation"),
				Success: true, Usage: turnUsageFixture(), ResultSummary: evidenceDeclaration(declarations...)}
			key := fixture.key("bounded-complete")
			var first command.Result
			for replay := 0; replay < 2; replay++ {
				ctx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
				started := time.Now()
				result, err := f.service.Execute(ctx, command.Command{Kind: command.CompleteExecution, Principal: f.worker, Mutation: value.Mutation{IdempotencyKey: key}, Payload: payload})
				cancel()
				t.Logf("completion replay=%d elapsed=%s", replay, time.Since(started))
				if err != nil {
					t.Fatal("native completion exceeded unchanged RPC budget or failed", completionOutcomeClass(err))
				}
				if replay == 0 {
					first = result
				} else if !reflect.DeepEqual(first.CreatedRefs, result.CreatedRefs) {
					t.Fatal("exact bounded completion replay changed created resources")
				}
			}
			var restored string
			if err := f.pool.QueryRow(f.ctx, queryWorkflowEvidencePlanRead).Scan(&restored); err != nil || restored != planner {
				t.Fatal("bounded PDLC completion leaked its transaction planner")
			}
			assertClosed()
			history := fixture.history(lease)
			if len(history) != 1 || !history[0].Accepted || len(history[0].Results) != 4 {
				t.Fatal("bounded completion did not accept all four native predicates")
			}
			next := fixture.claim(f.agents["check"])
			fixture.complete(next, evidenceDeclaration(entity.WorkflowDeclaredResult{Key: "architecture", Source: &entity.WorkflowArtifactSource{Kind: "CURRENT_COMPLETION"}}),
				workflowCaptureProgressArtifact("architecture.md", []byte("Архитектура журнала обращений.\n")))
			root, err := f.service.GetRun(f.ctx, f.owner, f.runRef)
			if err != nil || root.State != "SUCCEEDED" {
				t.Fatal("bounded completion did not dispatch and finish the dependent graph")
			}
		})
	}
}
