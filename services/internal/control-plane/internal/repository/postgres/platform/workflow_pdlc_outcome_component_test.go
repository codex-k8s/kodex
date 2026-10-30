package platform

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestWorkflowPDLCFileResultOwnerComponent proves native write/head/content receipt resolution
// and an automatic dependent claim using actual owner commands. Trusted fixture gateway outputs
// are not external GitHub delivery; the full SQL outcome fence is qualified separately.
// TestWorkflowPDLCFileResultOwnerComponent проверяет native write/head/content receipt resolution
// и автоматический dependent claim через owner commands. Trusted fixture gateway outputs не
// являются внешней GitHub delivery; полный SQL outcome fence проверяется отдельно.
func TestWorkflowPDLCFileResultOwnerComponent(t *testing.T) {
	for _, mode := range []string{"documentation-effect-first", "documentation-model-first", "developer-effect-first", "developer-model-first", "documentation-large-read-history-effect-first", "documentation-large-read-history-model-first"} {
		t.Run(mode, func(t *testing.T) {
			developer := strings.HasPrefix(mode, "developer-")
			modelFirst := strings.HasSuffix(mode, "model-first")
			var extras []entity.WorkflowResultRequirement
			if developer {
				extras = []entity.WorkflowResultRequirement{
					{Key: "response", Kind: "PR_COMMENT", Repository: "fixture/repository", TargetRef: "3"},
					{Key: "pr", Kind: "PULL_REQUEST", Repository: "fixture/repository", TargetRef: "3"},
				}
			}
			fixture, lease := newWorkflowPDLCFileOwnerFixture(t, extras...)
			f := fixture.f
			content := []byte("Требования к журналу обращений.\n")
			head := strings.Repeat("a", 40)
			blob := strings.Repeat("b", 40)
			if strings.Contains(mode, "large-read-history") {
				workflowPDLCSeedLargeReadHistory(fixture, lease, head, blob)
			}
			write, err := fixture.resolve(lease, "pdlc-result-write", "github.repository.content.create", map[string]any{"path": "docs/requirements.md", "branch": "feature/journal", "message": "Сохранить требования", "content_base64": base64.StdEncoding.EncodeToString(content)})
			if err != nil {
				t.Fatal("admit native file result WRITE", completionOutcomeClass(err))
			}
			writeReceipt := fixture.finishEffect(write, "github-commit:"+head, string(asJSON(map[string]any{"sha": head, "path": "docs/requirements.md"})))
			pull, err := fixture.resolve(lease, "pdlc-result-head", "github.pull_request.read", map[string]any{"pull_request_number": int64(3)})
			if err != nil {
				t.Fatal("admit native file result PR read", completionOutcomeClass(err))
			}
			headReceipt := fixture.finishEffect(pull, "github-pull:3", string(asJSON(map[string]any{"number": 3, "state": "open", "head": "feature/journal", "base": "main", "sha": head, "draft": true})))
			var declarations []entity.WorkflowDeclaredResult
			if developer {
				comment, err := fixture.resolve(lease, "pdlc-result-comment", "github.pull_request.comment.create", map[string]any{"pull_request_number": int64(3), "body": "Реализация и документация обновлены, замечания учтены."})
				if err != nil {
					t.Fatal("admit native PR comment", completionOutcomeClass(err))
				}
				commentReceipt := fixture.finishEffect(comment, "github-comment:42", `{"pull_request_number":3,"comment_id":42}`)
				ready, err := fixture.resolve(lease, "pdlc-result-ready", "github.pull_request.ready", map[string]any{"pull_request_number": int64(3), "expected_head_sha": head})
				if err != nil {
					t.Fatal("admit native Ready", completionOutcomeClass(err))
				}
				readyReceipt := fixture.finishEffect(ready, "github-pull:3", string(asJSON(map[string]any{"number": 3, "sha": head, "state": "open", "draft": false, "url": "https://github.com/fixture/repository/pull/3", "already_ready": false})))
				declarations = append(declarations, entity.WorkflowDeclaredResult{Key: "response", ReceiptRef: commentReceipt}, entity.WorkflowDeclaredResult{Key: "pr", ReceiptRef: readyReceipt})
			}
			read, err := fixture.resolve(lease, "pdlc-result-content", "github.repository.content.read", map[string]any{"path": "docs/requirements.md", "ref": head, "maximum_bytes": int64(4096)})
			if err != nil {
				t.Fatal("admit native immutable file read", completionOutcomeClass(err))
			}
			var readWork map[string]any
			if modelFirst {
				readWork = fixture.claimEffect(read)
				fixture.complete(lease, "Публикация завершена, ожидаю подтверждение чтения файла.")
				if len(fixture.history(lease)) != 0 {
					t.Fatal("pending content read produced business acceptance")
				}
			}
			contentSummary := string(asJSON(map[string]any{"path": "docs/requirements.md", "type": "file", "sha": blob, "size": len(content), "text": string(content), "commit_sha": head,
				"offset_bytes": 0, "next_offset_bytes": len(content), "eof": true, "source_digest": "sha256:" + workflowResultSHA(content), "chunk_digest": "sha256:" + workflowResultSHA(content)}))
			var contentReceipt string
			if modelFirst {
				contentReceipt = fixture.completeEffect(read, readWork, "github-content:"+blob, contentSummary)
			} else {
				contentReceipt = fixture.finishEffect(read, "github-content:"+blob, contentSummary)
			}
			declarations = append([]entity.WorkflowDeclaredResult{{Key: "document", ReceiptRef: writeReceipt, HeadReceiptRef: headReceipt, ContentReceiptRef: contentReceipt}}, declarations...)
			if !modelFirst {
				fixture.complete(lease, evidenceDeclaration(declarations...))
			}
			if early := fixture.history(lease); len(early) != 0 {
				t.Fatal("native physical run closed before owner outcome delivery", early[0].Accepted, early[0].Reason)
			}
			previous := lease
			lease = fixture.claimOutcome(lease, f.agents["author"])
			var assertClosed func()
			if strings.Contains(mode, "large-read-history") {
				assertClosed = workflowPDLCReadLargeHistory(fixture, previous, lease)
			}
			fixture.complete(lease, evidenceDeclaration(declarations...))
			if assertClosed != nil {
				assertClosed()
			}
			history := fixture.history(lease)
			if len(history) != 1 || !history[0].Accepted || len(history[0].Results) != 1+len(extras) {
				reason := "NO_HISTORY"
				if len(history) == 1 {
					reason = history[0].Reason
				}
				t.Fatal("actual native file proof did not become accepted evidence", reason)
			}
			accepted := history[0].Results[0]
			if accepted.Kind != "REPOSITORY_FILE" || accepted.Branch != "feature/journal" || accepted.FileName != "docs/requirements.md" || accepted.HeadSHA != head || accepted.ReceiptRef != writeReceipt || accepted.HeadReceiptRef != headReceipt || accepted.ContentReceiptRef != contentReceipt {
				t.Fatal("native accepted file lost immutable target or receipt pins")
			}
			var organization, anchor string
			var entry, requirements []byte
			if err := f.pool.QueryRow(f.ctx, `SELECT node.organization_id::text,node.id::text,node.workflow_attempts->0,step.value->'ResultRequirements'
FROM control_plane.runs root
JOIN control_plane.run_nodes node ON node.root_run_id=root.id AND node.organization_id=root.organization_id
JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
CROSS JOIN LATERAL jsonb_array_elements(version.spec->'Steps') step(value)
WHERE root.ref=$1 AND node.workflow_step_key='author' AND node.workflow_step_origin_node_id IS NULL AND step.value->>'Key'='author'`, f.runRef).Scan(&organization, &anchor, &entry, &requirements); err != nil {
				t.Fatal("read exact owner-created native outcome fixture")
			}
			for _, mode := range []string{"valid", "path", "branch", "repository", "digest", "head", "receipt", "duplicate-receipt", "unknown-field", "missing-content", "foreign-lease"} {
				t.Run("sql-"+mode, func(t *testing.T) {
					var changed map[string]any
					if json.Unmarshal(entry, &changed) != nil {
						t.Fatal("decode exact private outcome fixture")
					}
					fact := changed["results"].([]any)[0].(map[string]any)
					switch mode {
					case "path":
						fact["fileName"] = "docs/foreign.md"
					case "branch":
						fact["branch"] = "main"
					case "repository":
						fact["repository"] = "fixture/foreign"
					case "digest":
						fact["digest"] = strings.Repeat("f", 64)
					case "head":
						fact["headSha"] = strings.Repeat("c", 40)
					case "receipt":
						fact["receiptRef"] = "erc_foreign00"
					case "duplicate-receipt":
						fact["headReceiptRef"] = fact["receiptRef"]
					case "unknown-field":
						fact["approved"] = true
					case "missing-content":
						delete(fact, "contentReceiptRef")
					case "foreign-lease":
						changed["provenance"].(map[string]any)["leaseId"] = "00000000-0000-4000-8000-000000000001"
					}
					var valid bool
					if err := f.pool.QueryRow(f.ctx, `SELECT control_plane.workflow_pdlc_outcome_valid($1::uuid,$2::uuid,$3::jsonb,$4::jsonb)`, organization, anchor, asJSON(changed), requirements).Scan(&valid); err != nil || valid != (mode == "valid") {
						t.Fatal("native SQL outcome fence did not bind exact file evidence", mode, completionOutcomeClass(err))
					}
				})
			}
			next := fixture.claim(f.agents["check"])
			for _, pin := range []string{"feature/journal", "docs/requirements.md", writeReceipt, headReceipt, contentReceipt} {
				if !strings.Contains(stringMap(next, "task"), pin) {
					t.Fatal("automatically dispatched employee lost accepted native file evidence")
				}
			}
			fixture.complete(next, evidenceDeclaration(entity.WorkflowDeclaredResult{Key: "architecture", Source: &entity.WorkflowArtifactSource{Kind: "CURRENT_COMPLETION"}}), workflowCaptureProgressArtifact("architecture.md", []byte("Архитектура журнала обращений.\n")))
			root, err := f.service.GetRun(f.ctx, f.owner, f.runRef)
			if err != nil || root.State != "SUCCEEDED" {
				t.Fatal("native file-to-artifact graph did not finish automatically", completionOutcomeClass(err))
			}
		})
	}
}
