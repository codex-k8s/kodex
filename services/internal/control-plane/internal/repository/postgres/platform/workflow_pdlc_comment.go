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
	digest := workflowPDLCResultDigest("kodex.workflow-pr-comment.v1", requirement.Key, requirement.Repository, requirement.TargetRef, declaration.ReceiptRef, proof.Digest, workflowResultSHA(proof.Input))
	return entity.WorkflowAcceptedResult{Key: requirement.Key, Kind: requirement.Kind, Repository: requirement.Repository, Number: number, ReceiptRef: declaration.ReceiptRef, Digest: digest}, ""
}

// acceptWorkflowPDLCReadyResult accepts only the native Ready capability's exact-head
// readback, including an already-ready PR. A generic PR read does not attest the author slot;
// the caller separately proves lineage and matches this head with the accepted file.
// acceptWorkflowPDLCReadyResult принимает только exact-head readback штатной Ready capability,
// включая уже готовый PR. Обычный PR read не подтверждает author slot; вызывающий отдельно
// доказывает lineage и сопоставляет head с принятым файлом.
func acceptWorkflowPDLCReadyResult(requirement entity.WorkflowResultRequirement, declaration entity.WorkflowDeclaredResult, proof workflowIntegrationResult) (entity.WorkflowAcceptedResult, string) {
	number, validNumber := workflowservice.RepositoryPullNumber(requirement.TargetRef)
	if requirement.Kind != "PULL_REQUEST" || !validNumber || !workflowPDLCProofScope(proof, requirement.Repository) || proof.Operation != "github.pull_request.ready" {
		return entity.WorkflowAcceptedResult{}, workflowPullRequestRequired
	}
	_, validInput := workflowResultObject(proof.Input, []string{"pull_request_number", "expected_head_sha"}, []string{"pull_request_number", "expected_head_sha"})
	fields := []string{"number", "sha", "state", "draft", "url", "already_ready"}
	_, validOutput := workflowResultObject([]byte(proof.Summary), fields, fields)
	var input struct {
		Number int64  `json:"pull_request_number"`
		Head   string `json:"expected_head_sha"`
	}
	var output struct {
		Number       int64  `json:"number"`
		SHA          string `json:"sha"`
		State        string `json:"state"`
		Draft        bool   `json:"draft"`
		URL          string `json:"url"`
		AlreadyReady bool   `json:"already_ready"`
	}
	if !validInput || !validOutput || json.Unmarshal(proof.Input, &input) != nil || json.Unmarshal([]byte(proof.Summary), &output) != nil ||
		input.Number != number || output.Number != number || !workflowservice.CommitPattern.MatchString(input.Head) || output.SHA != input.Head || output.State != "open" || output.Draft ||
		output.URL != "https://github.com/"+requirement.Repository+"/pull/"+requirement.TargetRef || proof.ProviderRef != "github-pull:"+requirement.TargetRef {
		return entity.WorkflowAcceptedResult{}, workflowPullRequestRequired
	}
	return entity.WorkflowAcceptedResult{Key: requirement.Key, Kind: requirement.Kind, Repository: requirement.Repository, Number: number, HeadSHA: output.SHA, ReceiptRef: declaration.ReceiptRef, Digest: proof.Digest}, ""
}
