package platform

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestWorkflowPDLCFileBusinessRetryComponent verifies native file and comment receipt rejoin
// across a rejected business attempt and an isolated owner-approved retry. Fresh immutable
// reads permit the dependent employee to start without issuing another external WRITE.
// TestWorkflowPDLCFileBusinessRetryComponent проверяет rejoin native file/comment receipts
// после отклонения бизнес-попытки и согласованного тестовым владельцем retry. Свежие immutable
// чтения позволяют запустить зависимого сотрудника без второй внешней записи.
func TestWorkflowPDLCFileBusinessRetryComponent(t *testing.T) {
	fixture, original := newWorkflowPDLCFileOwnerFixture(t, entity.WorkflowResultRequirement{Key: "response", Kind: "PR_COMMENT", Repository: "fixture/repository", TargetRef: "3"})
	f := fixture.f
	content := []byte("Требования к журналу обращений.\n")
	head, blob := strings.Repeat("a", 40), strings.Repeat("b", 40)
	writeInput := map[string]any{"path": "docs/requirements.md", "branch": "feature/journal", "message": "Сохранить требования", "content_base64": base64.StdEncoding.EncodeToString(content)}
	commentInput := map[string]any{"pull_request_number": int64(3), "body": "Требования обновлены в ветке PR."}
	write, err := fixture.resolve(original, "pdlc-retry-write", "github.repository.content.create", writeInput)
	if err != nil {
		t.Fatal("admit original business file WRITE", completionOutcomeClass(err))
	}
	writeReceipt := fixture.finishEffect(write, "github-commit:"+head, string(asJSON(map[string]any{"sha": head, "path": "docs/requirements.md"})))
	comment, err := fixture.resolve(original, "pdlc-retry-comment", "github.pull_request.comment.create", commentInput)
	if err != nil {
		t.Fatal("admit original business comment WRITE", completionOutcomeClass(err))
	}
	commentReceipt := fixture.finishEffect(comment, "github-comment:42", `{"pull_request_number":3,"comment_id":42}`)
	fixture.complete(original, "Материалы опубликованы, но обязательное подтверждение файла отсутствует.")
	if len(fixture.history(original)) == 0 {
		outcome := fixture.claimOutcome(original, f.agents["author"])
		fixture.complete(outcome, evidenceDeclaration(entity.WorkflowDeclaredResult{Key: "response", ReceiptRef: commentReceipt}))
	}
	history := fixture.history(original)
	if len(history) != 1 || history[0].Accepted {
		t.Fatal("missing file proof did not reject the original business attempt")
	}
	manager := fixture.claim(f.managerRef)
	fixture.complete(manager, "Нужна новая попытка с подтверждением уже опубликованных материалов.")
	gate := f.recoveryGate(f.read())
	f.execute(command.ResolveOwnerGate, "pdlc-business-retry-approve", &gate.Version, command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE", Comment: "Повторить проверку результата без повторной внешней записи."})
	retry := fixture.claim(f.agents["author"])
	if stringMap(retry, "runRef") == stringMap(original, "runRef") || stringMap(retry, "runtimeRevisionRef") == stringMap(original, "runtimeRevisionRef") {
		t.Fatal("business retry did not allocate a fresh physical execution")
	}
	for _, effect := range []struct {
		operation string
		input     map[string]any
		previous  map[string]any
		receipt   string
	}{
		{"github.repository.content.create", writeInput, write, writeReceipt},
		{"github.pull_request.comment.create", commentInput, comment, commentReceipt},
	} {
		rejoined, err := fixture.resolve(retry, fixture.key("business-rejoin"), effect.operation, effect.input)
		if err != nil || stringMap(rejoined, "invocationRef") != stringMap(effect.previous, "invocationRef") || stringMap(rejoined, "receiptRef") != effect.receipt {
			t.Fatal("business retry failed to rejoin exact successful native WRITE", effect.operation, completionOutcomeClass(err))
		}
	}
	if work, err := f.service.ClaimIntegrationInvocations(f.ctx, fixture.gateway, "pdlc-business-no-repeat", 2); err != nil || len(work) != 0 {
		t.Fatal("business retry scheduled another external WRITE")
	}
	pull, err := fixture.resolve(retry, "pdlc-retry-head", "github.pull_request.read", map[string]any{"pull_request_number": int64(3)})
	if err != nil {
		t.Fatal("admit fresh retry head read", completionOutcomeClass(err))
	}
	headReceipt := fixture.finishEffect(pull, "github-pull:3", string(asJSON(map[string]any{"number": 3, "state": "open", "head": "feature/journal", "base": "main", "sha": head, "draft": true})))
	read, err := fixture.resolve(retry, "pdlc-retry-content", "github.repository.content.read", map[string]any{"path": "docs/requirements.md", "ref": head, "maximum_bytes": int64(4096)})
	if err != nil {
		t.Fatal("admit fresh retry content read", completionOutcomeClass(err))
	}
	contentReceipt := fixture.finishEffect(read, "github-content:"+blob, string(asJSON(map[string]any{"path": "docs/requirements.md", "type": "file", "sha": blob, "size": len(content), "text": string(content), "commit_sha": head, "offset_bytes": 0, "next_offset_bytes": len(content), "eof": true, "source_digest": "sha256:" + workflowResultSHA(content), "chunk_digest": "sha256:" + workflowResultSHA(content)})))
	declaration := evidenceDeclaration(entity.WorkflowDeclaredResult{Key: "document", ReceiptRef: writeReceipt, HeadReceiptRef: headReceipt, ContentReceiptRef: contentReceipt}, entity.WorkflowDeclaredResult{Key: "response", ReceiptRef: commentReceipt})
	fixture.complete(retry, declaration)
	if len(fixture.history(retry)) == 0 {
		outcome := fixture.claimOutcome(retry, f.agents["author"])
		fixture.complete(outcome, declaration)
	}
	freshHistory := fixture.history(retry)
	oldHistory := fixture.history(original)
	if len(freshHistory) != 1 || !freshHistory[0].Accepted || len(freshHistory[0].Results) != 2 || len(oldHistory) != 1 || oldHistory[0].Accepted {
		reason := "NO_HISTORY"
		if len(freshHistory) == 1 {
			reason = freshHistory[0].Reason
		}
		t.Fatal("business retry lost old rejection or failed to accept rejoined evidence", reason)
	}
	next := fixture.claim(f.agents["check"])
	fixture.complete(next, evidenceDeclaration(entity.WorkflowDeclaredResult{Key: "architecture", Source: &entity.WorkflowArtifactSource{Kind: "CURRENT_COMPLETION"}}), workflowCaptureProgressArtifact("architecture.md", []byte("Архитектура по принятой версии требований.\n")))
	root, err := f.service.GetRun(f.ctx, f.owner, f.runRef)
	if err != nil || root.State != "SUCCEEDED" {
		t.Fatal("business retry did not automatically finish the dependent graph", completionOutcomeClass(err))
	}
}
