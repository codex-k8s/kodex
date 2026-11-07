package integration

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// gitLabRecoveryInput contains only the already canonical owner-pinned create payload.
// It never selects an endpoint, project, external identity or authorization header.
// gitLabRecoveryInput содержит только canonical owner-pinned create payload.
// Endpoint, project, external identity либо authorization header не выбираются.
type gitLabRecoveryInput struct {
	Title        string `json:"title"`
	Description  string `json:"description"`
	SourceBranch string `json:"source_branch"`
	TargetBranch string `json:"target_branch"`
}

// recoverGitLabCreate rejoins only issue/MR creation through fixed GETs and exact current
// authenticated author/project, unique marker and canonical payload. Missing, changed, duplicate,
// truncated or unsupported evidence stays UNKNOWN; this method never calls Execute or POST.
// recoverGitLabCreate согласует только issue/MR create через fixed GET и exact current
// authenticated author/project, unique marker и canonical payload. Отсутствующие, изменённые,
// duplicate, truncated либо unsupported evidence сохраняют UNKNOWN; Execute/POST не вызываются.
func (adapter *Adapter) recoverGitLabCreate(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, canonicalInput []byte) (Result, error) {
	contract, exists := integrationpackage.CredentialResourcePermissionContractFor("gitlab", request.CapabilityKey)
	if !exists || (request.Operation != "gitlab.issue.create" && request.Operation != "gitlab.merge_request.create") ||
		request.PersonalCredentialAdmission && request.ResourcePermissionProfile != contract.Profile {
		return Result{}, &UnknownOutcomeError{}
	}
	var input gitLabRecoveryInput
	if decodeProviderJSON(canonicalInput, &input) != nil || input.Title == "" {
		return Result{}, &UnknownOutcomeError{}
	}
	metadata, err := adapter.readGitLabWriteMetadata(ctx, request, capability, configuration)
	if err != nil {
		return Result{}, err
	}
	marker := "[kodex-effect:" + request.EffectKey + "]"
	description := strings.TrimSpace(input.Description)
	if description != "" {
		description += "\n\n"
	}
	description += marker
	path := adapter.gitLabProjectPath(configuration) + "/issues"
	if request.Operation == "gitlab.merge_request.create" {
		path = adapter.gitLabProjectPath(configuration) + "/merge_requests"
	}
	var candidate map[string]json.RawMessage
	seen := map[int64]bool{}
	exhausted := false
	for page := 1; page <= 5; page++ {
		query := url.Values{"scope": {"all"}, "state": {"all"}, "search": {marker}, "in": {"description"}, "per_page": {"100"}, "page": {strconv.Itoa(page)}, "order_by": {"created_at"}, "sort": {"asc"}}
		body, err := adapter.gitLabPermissionGET(ctx, request, capability, configuration, path, query)
		if err != nil {
			return Result{}, err
		}
		var rows []map[string]json.RawMessage
		if integrationpackage.ValidateCredentialPermissionJSON(body) != nil || json.Unmarshal(body, &rows) != nil || rows == nil || len(rows) > 100 {
			return Result{}, &UnknownOutcomeError{}
		}
		for _, row := range rows {
			id, valid := gitLabPermissionID(row, "id")
			if !valid || seen[id] {
				return Result{}, &UnknownOutcomeError{}
			}
			seen[id] = true
			text, valid := gitLabRecoveryDescription(row)
			if !valid {
				return Result{}, &UnknownOutcomeError{}
			}
			if !strings.Contains(text, marker) {
				continue
			}
			if candidate != nil || !gitLabRecoveryMatches(row, metadata, input, description, marker, request.Operation) {
				return Result{}, &UnknownOutcomeError{}
			}
			candidate = row
		}
		if len(rows) < 100 {
			exhausted = true
			break
		}
	}
	if !exhausted || candidate == nil {
		return Result{}, &UnknownOutcomeError{}
	}
	iid, _ := gitLabPermissionID(candidate, "iid")
	body, err := adapter.gitLabPermissionGET(ctx, request, capability, configuration, path+"/"+strconv.FormatInt(iid, 10), nil)
	if err != nil {
		return Result{}, err
	}
	detail, err := gitLabPermissionObject(body)
	if err != nil {
		return Result{}, err
	}
	originalID, _ := gitLabPermissionID(candidate, "id")
	detailID, _ := gitLabPermissionID(detail, "id")
	detailIID, _ := gitLabPermissionID(detail, "iid")
	if originalID != detailID || iid != detailIID || !gitLabRecoveryMatches(detail, metadata, input, description, marker, request.Operation) {
		return Result{}, &UnknownOutcomeError{}
	}
	var result Result
	if request.Operation == "gitlab.issue.create" {
		var issue gitLabIssue
		if decodeProviderJSON(body, &issue) != nil {
			return Result{}, &UnknownOutcomeError{}
		}
		result, err = gitLabIssueResult(request, issue)
	} else {
		var mr gitLabMergeRequest
		if decodeProviderJSON(body, &mr) != nil {
			return Result{}, &UnknownOutcomeError{}
		}
		result, err = gitLabMergeRequestResult(request, mr)
	}
	return result, err
}

// gitLabRecoveryDescription distinguishes an explicitly empty description from null/missing
// provider projection; unobservable marker content never proves absence or permits replay.
// gitLabRecoveryDescription отличает явно пустое описание от null/missing provider projection;
// ненаблюдаемый marker content не доказывает отсутствие и не разрешает replay.
func gitLabRecoveryDescription(row map[string]json.RawMessage) (string, bool) {
	var value *string
	if json.Unmarshal(row["description"], &value) != nil || value == nil {
		return "", false
	}
	return *value, true
}

// gitLabRecoveryMatches verifies complete creation provenance without treating a title, branch
// name or arbitrary marker substring as proof. Foreign project/author and cross-project MR close.
// gitLabRecoveryMatches проверяет полное creation provenance, не считая title, имя ветки либо
// произвольный marker substring доказательством. Foreign project/author и cross-project MR закрыты.
func gitLabRecoveryMatches(row map[string]json.RawMessage, metadata gitLabWriteMetadata, input gitLabRecoveryInput, description, marker, operation string) bool {
	iid, valid := gitLabPermissionID(row, "iid")
	if !valid || iid < 1 {
		return false
	}
	title, valid := gitLabPermissionString(row, "title")
	if !valid || title != input.Title {
		return false
	}
	text, valid := gitLabRecoveryDescription(row)
	if !valid || text != description || strings.Count(text, marker) != 1 {
		return false
	}
	var author map[string]json.RawMessage
	if json.Unmarshal(row["author"], &author) != nil {
		return false
	}
	authorID, valid := gitLabPermissionID(author, "id")
	if !valid || authorID != metadata.userID {
		return false
	}
	if operation == "gitlab.issue.create" {
		projectID, valid := gitLabPermissionID(row, "project_id")
		return valid && projectID == metadata.projectID
	}
	source, sourceOK := gitLabPermissionID(row, "source_project_id")
	target, targetOK := gitLabPermissionID(row, "target_project_id")
	sourceBranch, sourceBranchOK := gitLabPermissionString(row, "source_branch")
	targetBranch, targetBranchOK := gitLabPermissionString(row, "target_branch")
	return sourceOK && targetOK && source == metadata.projectID && target == metadata.projectID && sourceBranchOK && targetBranchOK && sourceBranch == input.SourceBranch && targetBranch == input.TargetBranch
}
