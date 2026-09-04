package integration

import (
	"net/http"
	"strings"
	"testing"
)

func jiraExtendedResponse(t *testing.T, operation string, r *http.Request) (string, bool) {
	t.Helper()
	if !strings.HasPrefix(operation, "jira.") {
		return "", false
	}
	check := func(method, path string) {
		if r.Method != method || r.URL.Path != path {
			t.Errorf("Jira route changed: %s %s", r.Method, r.URL.Path)
		}
	}
	comment := `{"id":"4","body":{"type":"doc","version":1,"content":[]}}`
	attachment := `{"id":"4","filename":"a.txt","size":4,"mimeType":"text/plain"}`
	switch operation {
	case "jira.project.user.search", "jira.project.user.read":
		check("GET", "/rest/api/3/user/assignable/search")
		if r.URL.Query().Get("project") != "OPS" {
			t.Error("users project lost")
		}
		if operation == "jira.project.user.read" && r.URL.Query().Get("accountId") != "account-4" {
			t.Error("user identity lost")
		}
		if operation == "jira.project.user.search" && (r.URL.Query().Get("startAt") != "1" || r.URL.Query().Get("maxResults") != "1") {
			t.Error("users pagination lost")
		}
		return `[{"accountId":"account-4","displayName":"User","active":true,"accountType":"atlassian"}]`, true
	case "jira.issue.transition.list":
		check("GET", "/rest/api/3/issue/OPS-3/transitions")
		return `{"transitions":[{"id":"4","name":"Close","to":{"id":"5","name":"Closed"}}]}`, true
	case "jira.issue.transition.apply":
		check("POST", "/rest/api/3/issue/OPS-3/transitions")
		return `{}`, true
	case "jira.issue.comment.list":
		check("GET", "/rest/api/3/issue/OPS-3/comment")
		if r.URL.Query().Get("startAt") != "1" || r.URL.Query().Get("maxResults") != "1" {
			t.Error("comments pagination lost")
		}
		return `{"comments":[` + comment + `],"startAt":1,"total":3}`, true
	case "jira.issue.comment.read":
		check("GET", "/rest/api/3/issue/OPS-3/comment/4")
		return comment, true
	case "jira.issue.comment.update":
		check("PUT", "/rest/api/3/issue/OPS-3/comment/4")
		return comment, true
	case "jira.issue.comment.delete":
		check("DELETE", "/rest/api/3/issue/OPS-3/comment/4")
		return `{}`, true
	case "jira.issue.link.list", "jira.issue.link.read", "jira.issue.link.delete", "jira.attachment.list", "jira.attachment.read", "jira.attachment.upload", "jira.attachment.delete":
		if r.URL.Path == "/rest/api/3/issue/OPS-3" && r.Method == "GET" {
			return `{"key":"OPS-3","fields":{"attachment":[` + attachment + `],"issuelinks":[{"id":"4","type":{"name":"Blocks","inward":"blocked by","outward":"blocks"},"outwardIssue":{"key":"OPS-4"}}]}}`, true
		}
		switch operation {
		case "jira.issue.link.delete":
			check("DELETE", "/rest/api/3/issueLink/4")
			return `{}`, true
		case "jira.attachment.delete":
			check("DELETE", "/rest/api/3/attachment/4")
			return `{}`, true
		case "jira.attachment.read":
			check("GET", "/rest/api/3/attachment/content/4")
			if r.URL.Query().Get("redirect") != "false" {
				t.Error("attachment redirect protection lost")
			}
			return "Text", true
		case "jira.attachment.upload":
			check("POST", "/rest/api/3/issue/OPS-3/attachments")
			if r.Header.Get("X-Atlassian-Token") != "nocheck" || r.ParseMultipartForm(64<<10) != nil {
				t.Error("multipart invalid")
			}
			return `[` + attachment + `]`, true
		default:
			t.Error("unexpected related resource call")
			return `{}`, true
		}
	}
	return "", false
}
