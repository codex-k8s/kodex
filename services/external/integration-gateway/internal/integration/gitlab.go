package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

type gitLabProject struct {
	ID                int64  `json:"id"`
	PathWithNamespace string `json:"path_with_namespace"`
	DefaultBranch     string `json:"default_branch"`
	Visibility        string `json:"visibility"`
	Archived          bool   `json:"archived"`
}

type gitLabIssue struct {
	IID         int64  `json:"iid"`
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	State       string `json:"state"`
	Description string `json:"description"`
	WebURL      string `json:"web_url"`
}

type gitLabMergeRequest struct {
	IID          int64  `json:"iid"`
	Title        string `json:"title"`
	State        string `json:"state"`
	Description  string `json:"description"`
	SourceBranch string `json:"source_branch"`
	TargetBranch string `json:"target_branch"`
	WebURL       string `json:"web_url"`
}

type gitLabPipeline struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
	Ref    string `json:"ref"`
	SHA    string `json:"sha"`
	WebURL string `json:"web_url"`
}

func (adapter *Adapter) testGitLab(ctx context.Context, request Request, configuration map[string]string) error {
	definition := adapter.definitions["gitlab"]
	capability, _ := definition.Capability(definition.Spec.HealthCheck.Operation)
	_, err := adapter.gitLabJSON(ctx, request, capability, configuration, http.MethodGet, adapter.gitLabProjectPath(configuration), nil, nil, "")
	return err
}

func (adapter *Adapter) executeGitLab(
	ctx context.Context,
	request Request,
	capability integrationpackage.Capability,
	configuration map[string]string,
	canonicalInput []byte,
) (Result, error) {
	projectPath := adapter.gitLabProjectPath(configuration)
	switch request.Operation {
	case "gitlab.issue.list":
		var input struct {
			State         string
			Limit, Cursor int
		}
		if decodeProviderJSON(canonicalInput, &input) != nil {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		if input.Limit == 0 {
			input.Limit = 20
		}
		if input.Cursor == 0 {
			input.Cursor = 1
		}
		if input.State == "" {
			input.State = "all"
		}
		query := url.Values{"state": {input.State}, "page": {strconv.Itoa(input.Cursor)}, "per_page": {strconv.Itoa(input.Limit)}}
		body, err := adapter.gitLabJSON(ctx, request, capability, configuration, http.MethodGet, projectPath+"/issues", query, nil, "")
		if err != nil {
			return Result{}, err
		}
		var issues []gitLabIssue
		if decodeProviderJSON(body, &issues) != nil || len(issues) > input.Limit {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		items := make([]map[string]any, 0, len(issues))
		for _, issue := range issues {
			if issue.IID < 1 {
				return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
			}
			items = append(items, map[string]any{"iid": issue.IID, "title": issue.Title, "state": issue.State, "web_url": issue.WebURL})
		}
		encoded, _ := json.Marshal(items)
		output := map[string]any{"count": len(items), "issues": string(encoded)}
		if len(items) == input.Limit {
			output["next_cursor"] = input.Cursor + 1
		}
		return providerResult(request, "gitlab-issues:"+strconv.Itoa(input.Cursor), output)
	case "gitlab.project.metadata.read":
		body, err := adapter.gitLabJSON(ctx, request, capability, configuration, http.MethodGet, projectPath, nil, nil, "")
		if err != nil {
			return Result{}, err
		}
		var provider gitLabProject
		if decodeProviderJSON(body, &provider) != nil || provider.ID < 1 || provider.PathWithNamespace != configuration["project_path"] {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return providerResult(request, "gitlab-project:"+strconv.FormatInt(provider.ID, 10), map[string]any{
			"id": provider.ID, "path_with_namespace": provider.PathWithNamespace, "default_branch": provider.DefaultBranch,
			"visibility": provider.Visibility, "archived": provider.Archived,
		})
	case "gitlab.repository.file.read":
		var input struct {
			FilePath string `json:"file_path"`
			Ref      string `json:"ref"`
		}
		if decodeProviderJSON(canonicalInput, &input) != nil {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		query := url.Values{"ref": {input.Ref}}
		body, err := adapter.gitLabJSON(ctx, request, capability, configuration, http.MethodGet,
			projectPath+"/repository/files/"+url.PathEscape(input.FilePath), query, nil, "")
		if err != nil {
			return Result{}, err
		}
		var provider struct {
			FilePath string `json:"file_path"`
			Ref      string `json:"ref"`
			BlobID   string `json:"blob_id"`
			Encoding string `json:"encoding"`
			Content  string `json:"content"`
		}
		if decodeProviderJSON(body, &provider) != nil || provider.FilePath != input.FilePath || provider.Encoding != "base64" {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return providerResult(request, "gitlab-blob:"+provider.BlobID, map[string]any{
			"file_path": provider.FilePath, "ref": input.Ref, "blob_id": provider.BlobID,
			"encoding": provider.Encoding, "content": provider.Content,
		})
	case "gitlab.issue.read", "gitlab.issue.update":
		return adapter.gitLabIssueOperation(ctx, request, capability, configuration, canonicalInput, projectPath)
	case "gitlab.issue.create":
		return adapter.createGitLabIssue(ctx, request, capability, configuration, canonicalInput, projectPath)
	case "gitlab.merge_request.read", "gitlab.merge_request.create":
		return adapter.gitLabMergeRequestOperation(ctx, request, capability, configuration, canonicalInput, projectPath)
	case "gitlab.merge_request.discussion.create":
		return adapter.createGitLabDiscussion(ctx, request, capability, configuration, canonicalInput, projectPath)
	case "gitlab.branch.create":
		var input struct {
			Branch string `json:"branch"`
			Ref    string `json:"ref"`
		}
		if decodeProviderJSON(canonicalInput, &input) != nil {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		body, err := adapter.gitLabJSON(ctx, request, capability, configuration, http.MethodPost, projectPath+"/repository/branches", nil,
			map[string]any{"branch": input.Branch, "ref": input.Ref}, request.EffectKey)
		if err != nil {
			return Result{}, err
		}
		var provider struct {
			Name   string `json:"name"`
			Commit struct {
				ID string `json:"id"`
			} `json:"commit"`
		}
		if decodeProviderJSON(body, &provider) != nil || provider.Name != input.Branch || provider.Commit.ID == "" {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return providerResult(request, "gitlab-branch:"+provider.Name, map[string]any{"name": provider.Name, "commit_id": provider.Commit.ID})
	case "gitlab.commit.create":
		var input struct {
			Branch        string `json:"branch"`
			Action        string `json:"action"`
			FilePath      string `json:"file_path"`
			Content       string `json:"content"`
			CommitMessage string `json:"commit_message"`
		}
		if decodeProviderJSON(canonicalInput, &input) != nil {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		message := strings.TrimSpace(input.CommitMessage) + "\n\n[kodex-effect:" + request.EffectKey + "]"
		body, err := adapter.gitLabJSON(ctx, request, capability, configuration, http.MethodPost, projectPath+"/repository/commits", nil,
			map[string]any{"branch": input.Branch, "commit_message": message, "actions": []map[string]string{{
				"action": input.Action, "file_path": input.FilePath, "content": input.Content,
			}}}, request.EffectKey)
		if err != nil {
			return Result{}, err
		}
		var provider struct {
			ID      string `json:"id"`
			ShortID string `json:"short_id"`
			WebURL  string `json:"web_url"`
		}
		if decodeProviderJSON(body, &provider) != nil || provider.ID == "" {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return providerResult(request, "gitlab-commit:"+provider.ID, map[string]any{
			"id": provider.ID, "short_id": provider.ShortID, "web_url": provider.WebURL,
		})
	case "gitlab.pipeline.read", "gitlab.pipeline.retry":
		var input struct {
			PipelineID int64 `json:"pipeline_id"`
		}
		if decodeProviderJSON(canonicalInput, &input) != nil {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		method, suffix, effectKey := http.MethodGet, "", ""
		if request.Operation == "gitlab.pipeline.retry" {
			method, suffix, effectKey = http.MethodPost, "/retry", request.EffectKey
		}
		body, err := adapter.gitLabJSON(ctx, request, capability, configuration, method,
			projectPath+"/pipelines/"+strconv.FormatInt(input.PipelineID, 10)+suffix, nil, nil, effectKey)
		if err != nil {
			return Result{}, err
		}
		var provider gitLabPipeline
		if decodeProviderJSON(body, &provider) != nil || provider.ID != input.PipelineID {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		projection := map[string]any{"id": provider.ID, "status": provider.Status, "web_url": provider.WebURL}
		if request.Operation == "gitlab.pipeline.read" {
			projection["ref"], projection["sha"] = provider.Ref, provider.SHA
		}
		return providerResult(request, "gitlab-pipeline:"+strconv.FormatInt(provider.ID, 10), projection)
	default:
		return Result{}, &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
}

func (adapter *Adapter) gitLabIssueOperation(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, canonicalInput []byte, projectPath string) (Result, error) {
	var input struct {
		IssueIID    int64  `json:"issue_iid"`
		Title       string `json:"title"`
		Description string `json:"description"`
		StateEvent  string `json:"state_event"`
	}
	if decodeProviderJSON(canonicalInput, &input) != nil || input.IssueIID < 1 {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	method, effectKey, payload := http.MethodGet, "", any(nil)
	if request.Operation == "gitlab.issue.update" {
		if input.Title == "" && input.Description == "" && input.StateEvent == "" {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		method, effectKey = http.MethodPut, request.EffectKey
		fields := map[string]any{}
		if input.Title != "" {
			fields["title"] = input.Title
		}
		if input.Description != "" {
			fields["description"] = input.Description
		}
		if input.StateEvent != "" {
			fields["state_event"] = input.StateEvent
		}
		payload = fields
	}
	body, err := adapter.gitLabJSON(ctx, request, capability, configuration, method,
		projectPath+"/issues/"+strconv.FormatInt(input.IssueIID, 10), nil, payload, effectKey)
	if err != nil {
		return Result{}, err
	}
	var provider gitLabIssue
	if decodeProviderJSON(body, &provider) != nil || provider.IID != input.IssueIID {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	projection := map[string]any{"iid": provider.IID, "title": provider.Title, "state": provider.State, "web_url": provider.WebURL}
	if request.Operation == "gitlab.issue.read" && provider.Description != "" {
		projection["description"] = provider.Description
	}
	return providerResult(request, "gitlab-issue:"+strconv.FormatInt(provider.IID, 10), projection)
}

func (adapter *Adapter) createGitLabIssue(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, canonicalInput []byte, projectPath string) (Result, error) {
	var input struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if decodeProviderJSON(canonicalInput, &input) != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	marker := "[kodex-effect:" + request.EffectKey + "]"
	query := url.Values{"scope": {"all"}, "state": {"all"}, "search": {marker}, "in": {"description"}, "per_page": {"100"}}
	body, err := adapter.gitLabJSON(ctx, request, capability, configuration, http.MethodGet, projectPath+"/issues", query, nil, "")
	if err != nil {
		return Result{}, err
	}
	var existing []gitLabIssue
	if decodeProviderJSON(body, &existing) != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	for _, issue := range existing {
		if strings.Contains(issue.Description, marker) {
			return gitLabIssueResult(request, issue)
		}
	}
	description := strings.TrimSpace(input.Description)
	if description != "" {
		description += "\n\n"
	}
	description += marker
	body, err = adapter.gitLabJSON(ctx, request, capability, configuration, http.MethodPost, projectPath+"/issues", nil,
		map[string]any{"title": input.Title, "description": description}, request.EffectKey)
	if err != nil {
		return Result{}, err
	}
	var created gitLabIssue
	if decodeProviderJSON(body, &created) != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return gitLabIssueResult(request, created)
}

func gitLabIssueResult(request Request, issue gitLabIssue) (Result, error) {
	if issue.IID < 1 || issue.Title == "" || issue.WebURL == "" {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return providerResult(request, "gitlab-issue:"+strconv.FormatInt(issue.IID, 10), map[string]any{
		"iid": issue.IID, "title": issue.Title, "state": issue.State, "web_url": issue.WebURL,
	})
}

func (adapter *Adapter) gitLabMergeRequestOperation(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, canonicalInput []byte, projectPath string) (Result, error) {
	if request.Operation == "gitlab.merge_request.read" {
		var input struct {
			MergeRequestIID int64 `json:"merge_request_iid"`
		}
		if decodeProviderJSON(canonicalInput, &input) != nil || input.MergeRequestIID < 1 {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		body, err := adapter.gitLabJSON(ctx, request, capability, configuration, http.MethodGet,
			projectPath+"/merge_requests/"+strconv.FormatInt(input.MergeRequestIID, 10), nil, nil, "")
		if err != nil {
			return Result{}, err
		}
		var provider gitLabMergeRequest
		if decodeProviderJSON(body, &provider) != nil || provider.IID != input.MergeRequestIID {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return gitLabMergeRequestResult(request, provider)
	}
	var input struct {
		SourceBranch string `json:"source_branch"`
		TargetBranch string `json:"target_branch"`
		Title        string `json:"title"`
		Description  string `json:"description"`
	}
	if decodeProviderJSON(canonicalInput, &input) != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	marker := "[kodex-effect:" + request.EffectKey + "]"
	description := strings.TrimSpace(input.Description)
	if description != "" {
		description += "\n\n"
	}
	description += marker
	body, err := adapter.gitLabJSON(ctx, request, capability, configuration, http.MethodPost, projectPath+"/merge_requests", nil,
		map[string]any{"source_branch": input.SourceBranch, "target_branch": input.TargetBranch, "title": input.Title, "description": description}, request.EffectKey)
	if err != nil {
		return Result{}, err
	}
	var provider gitLabMergeRequest
	if decodeProviderJSON(body, &provider) != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return gitLabMergeRequestResult(request, provider)
}

func gitLabMergeRequestResult(request Request, provider gitLabMergeRequest) (Result, error) {
	if provider.IID < 1 || provider.WebURL == "" {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	output := map[string]any{"iid": provider.IID, "title": provider.Title, "state": provider.State, "web_url": provider.WebURL}
	if request.Operation == "gitlab.merge_request.read" {
		output["source_branch"], output["target_branch"] = provider.SourceBranch, provider.TargetBranch
	}
	return providerResult(request, "gitlab-merge-request:"+strconv.FormatInt(provider.IID, 10), output)
}

func (adapter *Adapter) createGitLabDiscussion(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, canonicalInput []byte, projectPath string) (Result, error) {
	var input struct {
		MergeRequestIID int64  `json:"merge_request_iid"`
		Body            string `json:"body"`
	}
	if decodeProviderJSON(canonicalInput, &input) != nil || input.MergeRequestIID < 1 {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	marker := "[kodex-effect:" + request.EffectKey + "]"
	body, err := adapter.gitLabJSON(ctx, request, capability, configuration, http.MethodPost,
		projectPath+"/merge_requests/"+strconv.FormatInt(input.MergeRequestIID, 10)+"/notes", nil,
		map[string]any{"body": input.Body + "\n\n" + marker}, request.EffectKey)
	if err != nil {
		return Result{}, err
	}
	var provider struct {
		ID int64 `json:"id"`
	}
	if decodeProviderJSON(body, &provider) != nil || provider.ID < 1 {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return providerResult(request, "gitlab-note:"+strconv.FormatInt(provider.ID, 10), map[string]any{
		"note_id": provider.ID, "merge_request_iid": input.MergeRequestIID,
	})
}

func (adapter *Adapter) gitLabJSON(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, method, path string, query url.Values, body any, effectKey string) ([]byte, error) {
	return adapter.callProvider(ctx, providerCall{
		BaseURL: configuration["base_url"], Method: method, Path: path, Query: query, Body: body,
		AuthScheme: "BEARER", Credential: request.Credential, EffectKey: effectKey, Capability: capability,
	})
}

func (adapter *Adapter) gitLabProjectPath(configuration map[string]string) string {
	return "/api/v4/projects/" + url.PathEscape(configuration["project_path"])
}
