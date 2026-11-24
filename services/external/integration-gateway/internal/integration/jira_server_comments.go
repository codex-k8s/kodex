package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// jiraServerComment keeps the exact ID and explicitly present plaintext body for ordinary reads.
// jiraServerComment хранит точный ID и явно присутствующий текст комментария для обычного чтения.
type jiraServerComment struct {
	ID   string
	Body *string
}

// readJiraServerComments checks the addressed issue's configured project, then exposes bounded comments visible to the current account.
// readJiraServerComments сверяет проект задачи, затем возвращает ограниченную выдачу комментариев, видимых текущему аккаунту.
func (adapter *Adapter) readJiraServerComments(ctx context.Context, request Request, capability integrationpackage.Capability,
	configuration map[string]string, input []byte) (Result, error) {
	var value struct {
		IssueKey  string `json:"issue_key"`
		CommentID string `json:"comment_id"`
		Cursor    string
		Limit     int
	}
	if decodeProviderJSON(input, &value) != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	issue, err := adapter.readJiraServerIssue(ctx, request, capability, configuration, value.IssueKey)
	if err != nil {
		return Result{}, err
	}
	checkIssue := func() error {
		current, err := adapter.readJiraServerIssue(ctx, request, capability, configuration, value.IssueKey)
		if err != nil {
			return err
		}
		if current.ID != issue.ID {
			return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		return nil
	}
	path := "/issue/" + url.PathEscape(value.IssueKey) + "/comment"
	if request.Operation == "jira.issue.comment.read" {
		if !decimalID(value.CommentID) {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		body, err := adapter.jiraServerJSON(ctx, request, capability, configuration, http.MethodGet, path+"/"+value.CommentID, nil, nil, "")
		if err != nil {
			return Result{}, err
		}
		var comment jiraServerComment
		if decodeProviderJSON(body, &comment) != nil || comment.ID != value.CommentID || comment.Body == nil || len(*comment.Body) > 65536 {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		if err := checkIssue(); err != nil {
			return Result{}, err
		}
		return providerResult(request, "jira-comment:"+comment.ID, map[string]any{"issue_key": value.IssueKey, "comment_id": comment.ID, "body": *comment.Body})
	}
	if value.Limit == 0 {
		value.Limit = 10
	}
	offset, err := atlassianServerOffset(value.Cursor)
	if err != nil || value.Limit < 1 || value.Limit > 20 {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	query := url.Values{"startAt": {strconv.Itoa(offset)}, "maxResults": {strconv.Itoa(value.Limit)}, "orderBy": {"created"}}
	body, err := adapter.jiraServerJSON(ctx, request, capability, configuration, http.MethodGet, path, query, nil, "")
	if err != nil {
		return Result{}, err
	}
	var page struct {
		StartAt, MaxResults, Total *int
		Comments                   []jiraServerComment
	}
	if decodeProviderJSON(body, &page) != nil || page.StartAt == nil || *page.StartAt != offset || page.MaxResults == nil ||
		*page.MaxResults < 1 || *page.MaxResults > value.Limit || page.Total == nil || page.Comments == nil {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	next, err := atlassianServerNext(offset, len(page.Comments), *page.MaxResults, *page.Total)
	if err != nil {
		return Result{}, err
	}
	comments := make([]map[string]string, 0, len(page.Comments))
	seen := make(map[string]bool, len(page.Comments))
	for _, comment := range page.Comments {
		if !decimalID(comment.ID) || seen[comment.ID] || comment.Body == nil {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		seen[comment.ID] = true
		comments = append(comments, map[string]string{"id": comment.ID, "body": *comment.Body})
	}
	encoded, err := json.Marshal(comments)
	if err != nil || len(encoded) > 65536 {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	output := map[string]any{"count": len(comments), "comments": string(encoded)}
	if next != "" {
		output["next_cursor"] = next
	}
	if err := checkIssue(); err != nil {
		return Result{}, err
	}
	return providerResult(request, "jira-comments:"+value.IssueKey, output)
}
