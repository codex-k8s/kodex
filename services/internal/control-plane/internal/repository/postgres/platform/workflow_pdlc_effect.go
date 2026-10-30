package platform

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	workflowservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/workflow"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// parseWorkflowPDLCFileWrite shares the closed native file-write shape between slot selection,
// semantic normalization and result evidence. It validates data, never execution authority.
// parseWorkflowPDLCFileWrite объединяет закрытую форму native file-write для selector,
// semantic normalization и result evidence. Проверяет данные, не полномочия исполнения.
func parseWorkflowPDLCFileWrite(operation string, raw []byte) (workflowPDLCFileWrite, []byte, error) {
	required := []string{"path", "branch", "message", "content_base64"}
	if operation == "github.repository.content.update" {
		required = append(required, "sha")
	} else if operation != "github.repository.content.create" {
		return workflowPDLCFileWrite{}, nil, errs.ErrConflict
	}
	_, valid := workflowResultObjectWithLimit(raw, required, required, 384<<10)
	var input workflowPDLCFileWrite
	if !valid || json.Unmarshal(raw, &input) != nil || !workflowservice.ValidRepositoryFilePath(input.Path) || !workflowservice.ValidRepositoryBranch(input.Branch) ||
		strings.TrimSpace(input.Message) == "" || operation == "github.repository.content.update" && !workflowservice.CommitPattern.MatchString(input.SHA) {
		return workflowPDLCFileWrite{}, nil, errs.ErrConflict
	}
	data, err := base64.StdEncoding.Strict().DecodeString(input.Content)
	if err != nil || len(data) > 256<<10 || !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return workflowPDLCFileWrite{}, nil, errs.ErrInvalid
	}
	return input, data, nil
}

// workflowPDLCFileOperation identifies the native text-file write family without adding a
// callable capability; publication, current grants and the existing adapter remain mandatory.
// workflowPDLCFileOperation определяет семейство native text-file writes без новой вызываемой
// capability; publication, current grants и существующий adapter остаются обязательными.
func workflowPDLCFileOperation(operation string) bool {
	return operation == "github.repository.content.create" || operation == "github.repository.content.update"
}

// workflowEffectSemanticDigestForProfile binds native file intent to exact decoded bytes,
// path and branch while excluding the commit message and mutable blob precondition. Other
// profiles and operations retain their historical complete-input digest unchanged.
// workflowEffectSemanticDigestForProfile связывает native file intent с точными decoded bytes,
// path и branch, исключая сообщение commit и изменяемый blob precondition. Остальные профили
// и операции сохраняют прежний digest полного input без изменений.
func workflowEffectSemanticDigestForProfile(profile, key, operation, scopeDigest string, raw []byte) (string, error) {
	if profile != workflowservice.GitHubPDLCV1 || !workflowPDLCFileOperation(operation) {
		return workflowEffectSemanticDigest(key, operation, scopeDigest, raw)
	}
	input, data, err := parseWorkflowPDLCFileWrite(operation, raw)
	if err != nil {
		return "", err
	}
	return workflowResultSHA([]byte(strings.Join([]string{"kodex.workflow-file-intent.v1", key, scopeDigest, input.Path, input.Branch, workflowResultSHA(data)}, "\x00"))), nil
}

// workflowPDLCAuthorEffectSlot handles only the three new author operation families. File keys
// are deterministic per published result and exact path; comments and Ready bind an existing PR.
// Other operations return handled=false and retain the canonical merge/review/deploy selector.
// workflowPDLCAuthorEffectSlot обрабатывает только три новых семейства author operations. File keys
// выводятся из published result и exact path; comment и Ready закрепляют существующий PR.
// Остальные операции возвращают handled=false и сохраняют canonical selector merge/review/deploy.
func workflowPDLCAuthorEffectSlot(step entity.WorkflowStep, operation string, rawScope, rawInput []byte) (string, bool, error) {
	if !contains([]string{"github.repository.content.create", "github.repository.content.update", "github.pull_request.comment.create", "github.pull_request.ready"}, operation) {
		return "", false, nil
	}
	_, validScope := workflowResultObject(rawScope, []string{"owner", "repository"}, []string{"owner", "repository"})
	var resource map[string]string
	if !validScope || json.Unmarshal(rawScope, &resource) != nil {
		return "", true, errs.ErrConflict
	}
	repository := resource["owner"] + "/" + resource["repository"]
	key := ""
	for _, requirement := range step.ResultRequirements {
		if requirement.Repository != repository {
			continue
		}
		candidate := ""
		if requirement.Kind == "REPOSITORY_FILE" && contains([]string{"github.repository.content.create", "github.repository.content.update"}, operation) {
			input, _, err := parseWorkflowPDLCFileWrite(operation, rawInput)
			if err != nil {
				return "", true, err
			}
			if input.Branch == requirement.Branch {
				candidate = "file_" + workflowResultSHA([]byte(requirement.Key+"\x00"+input.Path))
			}
		}
		if requirement.Kind == "PR_COMMENT" && operation == "github.pull_request.comment.create" || requirement.Kind == "PULL_REQUEST" && operation == "github.pull_request.ready" {
			fields := []string{"pull_request_number", "body"}
			if operation == "github.pull_request.ready" {
				fields = []string{"pull_request_number", "expected_head_sha"}
			}
			_, valid := workflowResultObject(rawInput, fields, fields)
			var input struct {
				Number int64  `json:"pull_request_number"`
				Body   string `json:"body"`
				Head   string `json:"expected_head_sha"`
			}
			number, validNumber := workflowservice.RepositoryPullNumber(requirement.TargetRef)
			if !valid || !validNumber || json.Unmarshal(rawInput, &input) != nil || input.Number != number {
				return "", true, errs.ErrConflict
			}
			if operation == "github.pull_request.comment.create" && strings.TrimSpace(input.Body) != "" {
				candidate = requirement.Key
			} else if operation == "github.pull_request.ready" && workflowservice.CommitPattern.MatchString(input.Head) {
				candidate = "ready_" + workflowResultSHA([]byte(requirement.Key))
			}
		}
		if candidate != "" {
			if key != "" {
				return "", true, errs.ErrConflict
			}
			key = candidate
		}
	}
	if key == "" {
		return "", true, errs.ErrForbidden
	}
	return key, true, nil
}
