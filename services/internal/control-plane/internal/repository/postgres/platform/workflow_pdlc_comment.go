package platform

import (
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"

	workflowservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/workflow"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// acceptWorkflowPRCommentResult requires the exact native pull-request comment creation,
// repository and published PR number. Ordinary issue comments and narrative URLs are insufficient.
// The caller must first resolve the proof through the canonical owner lineage reader.
// acceptWorkflowPRCommentResult требует точное native создание комментария PR, repository и
// опубликованный номер PR. Issue comments и narrative URL недостаточны. Вызывающий предварительно
// разрешает proof через канонический owner lineage reader.
func acceptWorkflowPRCommentResult(requirement entity.WorkflowResultRequirement, declaration entity.WorkflowDeclaredResult, proof workflowIntegrationResult) (entity.WorkflowAcceptedResult, string) {
	number, validNumber := workflowservice.RepositoryPullNumber(requirement.TargetRef)
	if requirement.Kind != "PR_COMMENT" || !validNumber || !workflowPDLCProofScope(proof, requirement.Repository) || proof.Operation != "github.pull_request.comment.create" {
		return entity.WorkflowAcceptedResult{}, workflowPRCommentRequired
	}
	_, validInput := workflowResultObject(proof.Input, []string{"pull_request_number", "body"}, []string{"pull_request_number", "body"})
	_, validOutput := workflowResultObject([]byte(proof.Summary), []string{"pull_request_number", "comment_id"}, []string{"pull_request_number", "comment_id"})
	var input struct {
		Number int64  `json:"pull_request_number"`
		Body   string `json:"body"`
	}
	var output struct {
		Number int64 `json:"pull_request_number"`
		ID     int64 `json:"comment_id"`
	}
	if !validInput || !validOutput || json.Unmarshal(proof.Input, &input) != nil || json.Unmarshal([]byte(proof.Summary), &output) != nil || input.Number != number || output.Number != number ||
		strings.TrimSpace(input.Body) == "" || !utf8.ValidString(input.Body) || output.ID < 1 || output.ID > 9007199254740991 || proof.ProviderRef != "github-comment:"+strconv.FormatInt(output.ID, 10) {
		return entity.WorkflowAcceptedResult{}, workflowPRCommentRequired
	}
	digest := workflowResultSHA(asJSON([]string{"kodex.workflow-pr-comment.v1", requirement.Key, requirement.Repository, requirement.TargetRef, declaration.ReceiptRef, proof.Digest, workflowResultSHA(proof.Input)}))
	return entity.WorkflowAcceptedResult{Key: requirement.Key, Kind: requirement.Kind, Repository: requirement.Repository, Number: number, ReceiptRef: declaration.ReceiptRef, Digest: digest}, ""
}
