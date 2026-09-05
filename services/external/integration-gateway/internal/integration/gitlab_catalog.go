package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
)

type gitLabCatalogInput struct {
	Branch          string `json:"branch"`
	Ref             string `json:"ref"`
	Path            string `json:"path"`
	IssueIID        int64  `json:"issue_iid"`
	NoteID          int64  `json:"note_id"`
	MergeRequestIID int64  `json:"merge_request_iid"`
	PipelineID      int64  `json:"pipeline_id"`
	JobID           int64  `json:"job_id"`
	Body            string `json:"body"`
	Title           string `json:"title"`
	Description     string `json:"description"`
	State           string `json:"state"`
	TargetBranch    string `json:"target_branch"`
	SHA             string `json:"sha"`
	Squash          bool   `json:"squash"`
	Limit           int    `json:"limit"`
	Cursor          int    `json:"cursor"`
}

type gitLabCommitView struct {
	ID      string `json:"id"`
	ShortID string `json:"short_id"`
	Title   string `json:"title"`
	Message string `json:"message"`
	WebURL  string `json:"web_url"`
}

type gitLabBranchView struct {
	Name      string           `json:"name"`
	Protected bool             `json:"protected"`
	Default   bool             `json:"default"`
	Commit    gitLabCommitView `json:"commit"`
}

type gitLabTreeView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
	Mode string `json:"mode"`
}

type gitLabDiffView struct {
	OldPath     string `json:"old_path"`
	NewPath     string `json:"new_path"`
	Diff        string `json:"diff"`
	NewFile     bool   `json:"new_file"`
	DeletedFile bool   `json:"deleted_file"`
	RenamedFile bool   `json:"renamed_file"`
	Collapsed   bool   `json:"collapsed"`
	TooLarge    bool   `json:"too_large"`
}

type gitLabNoteView struct {
	ID           int64  `json:"id"`
	Body         string `json:"body"`
	System       bool   `json:"system"`
	NoteableIID  int64  `json:"noteable_iid"`
	NoteableType string `json:"noteable_type"`
}

type gitLabDiscussionView struct {
	ID             string           `json:"id"`
	IndividualNote bool             `json:"individual_note"`
	Notes          []gitLabNoteView `json:"notes"`
}

type gitLabJobView struct {
	ID       int64          `json:"id"`
	Name     string         `json:"name"`
	Stage    string         `json:"stage"`
	Status   string         `json:"status"`
	Ref      string         `json:"ref"`
	WebURL   string         `json:"web_url"`
	Pipeline gitLabPipeline `json:"pipeline"`
}

func providerTypedObject[T any](request Request, body []byte, reference string, valid func(T) bool) (Result, error) {
	var value T
	if decodeProviderJSON(body, &value) != nil || !valid(value) {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return providerResult(request, reference, value)
}

func providerPageResult[T any](request Request, response *providerResponse, limit, page int, valid func(T) bool) (Result, error) {
	var values []T
	if decodeProviderJSON(response.Body, &values) != nil || len(values) > limit {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	for _, value := range values {
		if !valid(value) {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
	}
	if values == nil {
		values = []T{}
	}
	next := 0
	if header, exists := response.Header[http.CanonicalHeaderKey("X-Next-Page")]; exists {
		if len(header) != 1 {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		if header[0] != "" {
			value, err := strconv.Atoi(header[0])
			if err != nil || value != page+1 || value > 10000 {
				return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
			}
			next = value
		}
	} else if len(values) == limit && page < 10000 {
		next = page + 1
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return providerResult(request, request.Operation+":"+strconv.Itoa(page), struct {
		Items string `json:"items"`
		Count int    `json:"count"`
		Next  int    `json:"next_cursor,omitempty"`
	}{string(encoded), len(values), next})
}

func (adapter *Adapter) executeGitLabCatalog(ctx context.Context, request Request, call providerCall, raw []byte) (Result, error) {
	var in gitLabCatalogInput
	if json.Unmarshal(raw, &in) != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	if in.Limit == 0 {
		in.Limit = 20
	}
	if in.Cursor == 0 {
		in.Cursor = 1
	}
	project := call.Path
	call.Method = http.MethodGet
	call.Query = url.Values{}
	page := func() {
		call.Query.Set("page", strconv.Itoa(in.Cursor))
		call.Query.Set("per_page", strconv.Itoa(in.Limit))
	}
	issuePath := project + "/issues/" + strconv.FormatInt(in.IssueIID, 10)
	mrPath := project + "/merge_requests/" + strconv.FormatInt(in.MergeRequestIID, 10)
	switch request.Operation {
	case "gitlab.branch.list":
		call.Path += "/repository/branches"
		page()
	case "gitlab.branch.read":
		call.Path += "/repository/branches/" + url.PathEscape(in.Branch)
	case "gitlab.branch.delete":
		call.Path += "/repository/branches/" + url.PathEscape(in.Branch)
		call.Method = http.MethodDelete
	case "gitlab.commit.list":
		call.Path += "/repository/commits"
		page()
		if in.Ref != "" {
			call.Query.Set("ref_name", in.Ref)
		}
		if in.Path != "" {
			call.Query.Set("path", in.Path)
		}
	case "gitlab.commit.read":
		call.Path += "/repository/commits/" + url.PathEscape(in.Ref)
	case "gitlab.commit.diff":
		call.Path += "/repository/commits/" + url.PathEscape(in.Ref) + "/diff"
		page()
	case "gitlab.repository.tree.list":
		call.Path += "/repository/tree"
		page()
		if in.Ref != "" {
			call.Query.Set("ref", in.Ref)
		}
		if in.Path != "" {
			call.Query.Set("path", in.Path)
		}
	case "gitlab.issue.note.list":
		call.Path = issuePath + "/notes"
		page()
	case "gitlab.issue.note.read":
		call.Path = issuePath + "/notes/" + strconv.FormatInt(in.NoteID, 10)
	case "gitlab.issue.note.create":
		call.Path = issuePath + "/notes"
		call.Method = http.MethodPost
		call.Body = struct {
			Body string `json:"body"`
		}{in.Body}
	case "gitlab.issue.note.update":
		call.Path = issuePath + "/notes/" + strconv.FormatInt(in.NoteID, 10)
		call.Method = http.MethodPut
		call.Body = struct {
			Body string `json:"body"`
		}{in.Body}
	case "gitlab.issue.note.delete":
		call.Path = issuePath + "/notes/" + strconv.FormatInt(in.NoteID, 10)
		call.Method = http.MethodDelete
	case "gitlab.merge_request.list":
		call.Path += "/merge_requests"
		page()
		if in.State != "" {
			call.Query.Set("state", in.State)
		}
	case "gitlab.merge_request.update":
		call.Path = mrPath
		call.Method = http.MethodPut
		body := struct {
			Title       *string `json:"title,omitempty"`
			Description *string `json:"description,omitempty"`
			State       string  `json:"state_event,omitempty"`
			Target      string  `json:"target_branch,omitempty"`
		}{State: in.State, Target: in.TargetBranch}
		if _, ok := request.Input["title"]; ok {
			body.Title = &in.Title
		}
		if _, ok := request.Input["description"]; ok {
			body.Description = &in.Description
		}
		call.Body = body
	case "gitlab.merge_request.merge":
		call.Path = mrPath + "/merge"
		call.Method = http.MethodPut
		call.Body = struct {
			SHA    string `json:"sha"`
			Squash bool   `json:"squash"`
		}{in.SHA, in.Squash}
	case "gitlab.merge_request.discussion.list":
		call.Path = mrPath + "/discussions"
		page()
	case "gitlab.merge_request.diff.list":
		call.Path = mrPath + "/diffs"
		page()
	case "gitlab.pipeline.list":
		call.Path += "/pipelines"
		page()
		if in.Ref != "" {
			call.Query.Set("ref", in.Ref)
		}
		if in.State != "" {
			call.Query.Set("status", in.State)
		}
	case "gitlab.pipeline.cancel":
		call.Path += "/pipelines/" + strconv.FormatInt(in.PipelineID, 10) + "/cancel"
		call.Method = http.MethodPost
	case "gitlab.job.list":
		if in.PipelineID > 0 {
			call.Path += "/pipelines/" + strconv.FormatInt(in.PipelineID, 10) + "/jobs"
		} else {
			call.Path += "/jobs"
		}
		page()
	case "gitlab.job.read":
		call.Path += "/jobs/" + strconv.FormatInt(in.JobID, 10)
	case "gitlab.job.retry":
		call.Path += "/jobs/" + strconv.FormatInt(in.JobID, 10) + "/retry"
		call.Method = http.MethodPost
	case "gitlab.job.cancel":
		call.Path += "/jobs/" + strconv.FormatInt(in.JobID, 10) + "/cancel"
		call.Method = http.MethodPost
	case "gitlab.job.trace.read":
		call.Path += "/jobs/" + strconv.FormatInt(in.JobID, 10) + "/trace"
	default:
		return Result{}, &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
	if call.Method != http.MethodGet {
		call.EffectKey = request.EffectKey
	}
	response, err := adapter.callProviderResponse(ctx, call)
	if err != nil {
		return Result{}, err
	}
	reference := request.Operation + ":" + request.EffectKey
	switch request.Operation {
	case "gitlab.branch.list":
		return providerPageResult(request, response, in.Limit, in.Cursor, func(v gitLabBranchView) bool { return v.Name != "" && v.Commit.ID != "" })
	case "gitlab.branch.read":
		var branch gitLabBranchView
		if decodeProviderJSON(response.Body, &branch) != nil || branch.Name != in.Branch || branch.Commit.ID == "" {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return providerResult(request, reference, struct {
			Name      string `json:"name"`
			Protected bool   `json:"protected"`
			Default   bool   `json:"default"`
			CommitID  string `json:"commit_id"`
		}{branch.Name, branch.Protected, branch.Default, branch.Commit.ID})
	case "gitlab.branch.delete", "gitlab.issue.note.delete":
		return providerResult(request, reference, struct {
			Accepted bool `json:"accepted"`
		}{true})
	case "gitlab.commit.list":
		return providerPageResult(request, response, in.Limit, in.Cursor, func(v gitLabCommitView) bool { return v.ID != "" })
	case "gitlab.commit.read":
		return providerTypedObject(request, response.Body, reference, func(v gitLabCommitView) bool { return v.ID != "" })
	case "gitlab.commit.diff", "gitlab.merge_request.diff.list":
		return providerPageResult(request, response, in.Limit, in.Cursor, func(v gitLabDiffView) bool {
			return validRepositoryPath(v.OldPath, false) && validRepositoryPath(v.NewPath, false)
		})
	case "gitlab.repository.tree.list":
		return providerPageResult(request, response, in.Limit, in.Cursor, func(v gitLabTreeView) bool { return v.ID != "" && validRepositoryPath(v.Path, false) })
	case "gitlab.issue.note.list":
		return providerPageResult(request, response, in.Limit, in.Cursor, func(v gitLabNoteView) bool {
			return v.ID > 0 && v.NoteableIID == in.IssueIID && v.NoteableType == "Issue"
		})
	case "gitlab.issue.note.read", "gitlab.issue.note.create", "gitlab.issue.note.update":
		return providerTypedObject(request, response.Body, reference, func(v gitLabNoteView) bool {
			return v.ID > 0 && (in.NoteID == 0 || v.ID == in.NoteID) && v.NoteableIID == in.IssueIID && v.NoteableType == "Issue"
		})
	case "gitlab.merge_request.list":
		return providerPageResult(request, response, in.Limit, in.Cursor, func(v gitLabMergeRequest) bool { return v.IID > 0 })
	case "gitlab.merge_request.update", "gitlab.merge_request.merge":
		return providerTypedObject(request, response.Body, reference, func(v gitLabMergeRequest) bool { return v.IID == in.MergeRequestIID })
	case "gitlab.merge_request.discussion.list":
		return providerPageResult(request, response, in.Limit, in.Cursor, func(v gitLabDiscussionView) bool {
			if v.ID == "" || len(v.Notes) > 100 {
				return false
			}
			for _, note := range v.Notes {
				if note.ID < 1 || note.NoteableIID != in.MergeRequestIID || note.NoteableType != "MergeRequest" {
					return false
				}
			}
			return true
		})
	case "gitlab.pipeline.list":
		return providerPageResult(request, response, in.Limit, in.Cursor, func(v gitLabPipeline) bool { return v.ID > 0 })
	case "gitlab.pipeline.cancel":
		return providerTypedObject(request, response.Body, reference, func(v gitLabPipeline) bool { return v.ID == in.PipelineID })
	case "gitlab.job.list":
		return providerPageResult(request, response, in.Limit, in.Cursor, func(v gitLabJobView) bool { return v.ID > 0 && (in.PipelineID == 0 || v.Pipeline.ID == in.PipelineID) })
	case "gitlab.job.read", "gitlab.job.retry", "gitlab.job.cancel":
		var job gitLabJobView
		if decodeProviderJSON(response.Body, &job) != nil || job.ID < 1 || job.Pipeline.ID < 1 || request.Operation != "gitlab.job.retry" && job.ID != in.JobID {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return providerResult(request, reference, struct {
			ID         int64  `json:"id"`
			Name       string `json:"name"`
			Stage      string `json:"stage"`
			Status     string `json:"status"`
			Ref        string `json:"ref"`
			WebURL     string `json:"web_url"`
			PipelineID int64  `json:"pipeline_id"`
		}{job.ID, job.Name, job.Stage, job.Status, job.Ref, job.WebURL, job.Pipeline.ID})
	case "gitlab.job.trace.read":
		return providerResult(request, reference, struct {
			JobID int64  `json:"job_id"`
			Trace string `json:"trace"`
		}{in.JobID, string(response.Body)})
	}
	return Result{}, &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
}
