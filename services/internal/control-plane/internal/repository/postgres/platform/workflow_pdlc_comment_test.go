package platform

import (
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestWorkflowPDLCCommentEvidence distinguishes a native PR comment from issue comments, reads,
// unrelated PR numbers and corrupt receipt envelopes even when the response digest is recomputed.
// TestWorkflowPDLCCommentEvidence отличает native PR comment от issue comments, reads, чужих
// номеров PR и повреждённых receipt envelopes даже при пересчитанном response digest.
func TestWorkflowPDLCCommentEvidence(t *testing.T) {
	for _, mode := range []string{"valid", "issue-operation", "read-operation", "wrong-repository", "wrong-input-pr", "wrong-output-pr", "empty-body", "null-body", "duplicate-body", "case-key", "unknown-input", "unknown-output", "zero-id", "unsafe-id", "provider", "digest", "lineage"} {
		t.Run(mode, func(t *testing.T) {
			r := entity.WorkflowResultRequirement{Key: "response", Kind: "PR_COMMENT", Repository: "acme/repo", TargetRef: "3"}
			d := entity.WorkflowDeclaredResult{Key: r.Key, ReceiptRef: "erc_comment00"}
			p := workflowIntegrationResult{Operation: "github.pull_request.comment.create", Scope: []byte(`{"owner":"acme","repository":"repo"}`), Input: []byte(`{"pull_request_number":3,"body":"Требования обновлены, замечания учтены"}`), Summary: `{"pull_request_number":3,"comment_id":42}`, ProviderRef: "github-comment:42"}
			switch mode {
			case "issue-operation":
				p.Operation = "github.issue.comment.create"
			case "read-operation":
				p.Operation = "github.pull_request.comment.read"
			case "wrong-repository":
				r.Repository = "acme/foreign"
			case "wrong-input-pr":
				p.Input = []byte(`{"pull_request_number":4,"body":"Проверено"}`)
			case "wrong-output-pr":
				p.Summary = `{"pull_request_number":4,"comment_id":42}`
			case "empty-body":
				p.Input = []byte(`{"pull_request_number":3,"body":" \n "}`)
			case "null-body":
				p.Input = []byte(`{"pull_request_number":3,"body":null}`)
			case "duplicate-body":
				p.Input = []byte(`{"pull_request_number":3,"body":"","body":"Проверено"}`)
			case "case-key":
				p.Input = []byte(`{"Pull_Request_Number":3,"body":"Проверено"}`)
			case "unknown-input":
				p.Input = []byte(`{"pull_request_number":3,"body":"Проверено","actor":"owner"}`)
			case "unknown-output":
				p.Summary = `{"pull_request_number":3,"comment_id":42,"approved":true}`
			case "zero-id":
				p.Summary = `{"pull_request_number":3,"comment_id":0}`
			case "unsafe-id":
				p.Summary = `{"pull_request_number":3,"comment_id":9007199254740992}`
			case "provider":
				p.ProviderRef = "github-comment:43"
			case "lineage":
				p.LineageFailure = workflowReceiptLineageInvalid
			}
			p.Digest = workflowResultSHA([]byte(p.Summary))
			if mode == "digest" {
				p.Digest = strings.Repeat("f", 64)
			}
			result, reason := acceptWorkflowPRCommentResult(r, d, p)
			if mode == "valid" {
				if reason != "" || result.Number != 3 || result.ReceiptRef != d.ReceiptRef || result.Digest == "" || result.Digest == p.Digest {
					t.Fatal("native PR comment lost immutable evidence pins")
				}
			} else if reason != workflowPRCommentRequired || result.ReceiptRef != "" {
				t.Fatal("unverified PR comment accepted")
			}
		})
	}
}

// TestWorkflowPDLCReadyEvidence closes the native Ready contract against generic reads,
// wrong-head observations, malformed fields and unrelated provider identities.
// TestWorkflowPDLCReadyEvidence закрывает native Ready contract от обычных reads,
// наблюдений другого head, неверных полей и чужих provider identities.
func TestWorkflowPDLCReadyEvidence(t *testing.T) {
	for _, mode := range []string{"transition", "already-ready", "read", "input-head", "output-head", "draft", "closed", "number", "url", "provider", "unknown", "null-already", "digest", "lineage"} {
		t.Run(mode, func(t *testing.T) {
			r := entity.WorkflowResultRequirement{Key: "pr", Kind: "PULL_REQUEST", Repository: "acme/repo", TargetRef: "3"}
			d := entity.WorkflowDeclaredResult{Key: "pr", ReceiptRef: "erc_ready0000"}
			input := map[string]any{"pull_request_number": 3, "expected_head_sha": strings.Repeat("a", 40)}
			output := map[string]any{"number": 3, "sha": strings.Repeat("a", 40), "state": "open", "draft": false, "url": "https://github.com/acme/repo/pull/3", "already_ready": false}
			p := workflowIntegrationResult{Operation: "github.pull_request.ready", Scope: []byte(`{"owner":"acme","repository":"repo"}`), ProviderRef: "github-pull:3"}
			switch mode {
			case "already-ready":
				output["already_ready"] = true
			case "read":
				p.Operation = "github.pull_request.read"
			case "input-head":
				input["expected_head_sha"] = "feature/journal"
			case "output-head":
				output["sha"] = strings.Repeat("b", 40)
			case "draft":
				output["draft"] = true
			case "closed":
				output["state"] = "closed"
			case "number":
				output["number"] = 4
			case "url":
				output["url"] = "https://github.com/acme/foreign/pull/3"
			case "provider":
				p.ProviderRef = "github-pull:4"
			case "unknown":
				output["approved"] = true
			case "null-already":
				output["already_ready"] = nil
			case "lineage":
				p.LineageFailure = workflowReceiptLineageInvalid
			}
			p.Input, p.Summary = asJSON(input), string(asJSON(output))
			p.Digest = workflowResultSHA([]byte(p.Summary))
			if mode == "digest" {
				p.Digest = strings.Repeat("f", 64)
			}
			result, reason := acceptWorkflowPDLCReadyResult(r, d, p)
			valid := mode == "transition" || mode == "already-ready"
			if (reason == "") != valid || valid && (result.HeadSHA != strings.Repeat("a", 40) || result.Digest != p.Digest || result.ReceiptRef != d.ReceiptRef) {
				t.Fatal("native Ready evidence did not preserve its exact-head contract", mode)
			}
		})
	}
}
