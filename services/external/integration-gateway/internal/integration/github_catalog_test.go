package integration

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func githubExtendedResponse(t *testing.T, operation string, r *http.Request) (string, bool) {
	t.Helper()
	if !strings.HasPrefix(operation, "github.") {
		return "", false
	}
	path := strings.TrimPrefix(r.URL.Path, "/repos/acme/repo")
	if path == r.URL.Path {
		t.Fatal("repository boundary escaped")
	}
	list := func(item string) (string, bool) {
		if r.Method != "GET" || r.URL.Query().Get("page") != "2" || r.URL.Query().Get("per_page") != "1" {
			t.Error("list method or pagination changed")
		}
		return "[" + item + "]", true
	}
	pull := `{"number":3,"title":"Title","body":"Text","state":"open","head":{"ref":"feature","sha":"abc"},"base":{"ref":"main"},"html_url":"https://github.com/acme/repo/pull/3"}`
	review := `{"id":4,"body":"Text","state":"COMMENTED","commit_id":"abc"}`
	branch := `{"name":"main","commit":{"sha":"abc"},"protected":false}`
	commit := `{"sha":"abc","commit":{"message":"Text"},"html_url":"https://github.com/acme/repo/commit/abc"}`
	check := `{"id":4,"name":"check","status":"completed","conclusion":"success","head_sha":"abc","output":{"title":"Title","summary":"Text"}}`
	workflow := `{"id":4,"name":"Build","path":".github/workflows/build.yaml","state":"active"}`
	run := `{"id":4,"workflow_id":4,"name":"Build","status":"completed","conclusion":"success","head_sha":"abc","head_branch":"main","html_url":"https://github.com/acme/repo/actions/runs/4"}`
	job := `{"id":5,"run_id":4,"name":"Build","status":"completed","conclusion":"success","head_sha":"abc"}`
	scheme, host := r.URL.Scheme, r.URL.Host
	if scheme == "" {
		scheme = "http"
	}
	if host == "" {
		host = r.Host
	}
	comment := fmt.Sprintf(`{"id":4,"body":"Text","issue_url":"%s://%s/repos/acme/repo/issues/3"}`, scheme, host)
	switch operation {
	case "github.pull_request.file.list":
		if path != "/pulls/3/files" {
			t.Error("pull files route changed")
		}
		return list(`{"sha":"abc","filename":"a.txt","status":"modified","additions":1,"deletions":0,"patch":"@@ -0,0 +1 @@\n+Text"}`)
	case "github.repository.content.list":
		if path != "/contents/src" || r.Method != "GET" {
			t.Error("content list route changed")
		}
		return `[{"path":"src/a.txt","type":"file","sha":"abc","size":4}]`, true
	case "github.repository.content.read":
		if path != "/contents/src/a.txt" || r.Method != "GET" {
			t.Error("content read route changed")
		}
		return `{"path":"src/a.txt","type":"file","sha":"abc","size":4,"encoding":"base64","content":"VGV4dA=="}`, true
	case "github.repository.content.create", "github.repository.content.update", "github.repository.content.delete":
		method := "PUT"
		if strings.HasSuffix(operation, "delete") {
			method = "DELETE"
		}
		if path != "/contents/src/a.txt" || r.Method != method {
			t.Error("content mutation route changed")
		}
		return `{"commit":{"sha":"abc"},"content":{"path":"src/a.txt","sha":"abc"}}`, true
	case "github.branch.list":
		if path != "/branches" {
			t.Error("branch route changed")
		}
		return list(branch)
	case "github.branch.read":
		if path != "/branches/main" {
			t.Error("branch route changed")
		}
		return branch, true
	case "github.branch.create":
		if path != "/git/refs" || r.Method != "POST" {
			t.Error("branch creation changed")
		}
		return `{"ref":"refs/heads/feature","object":{"sha":"abc"}}`, true
	case "github.branch.delete":
		if path != "/git/refs/heads/feature" || r.Method != "DELETE" {
			t.Error("branch deletion changed")
		}
		return `{}`, true
	case "github.commit.list":
		if path != "/commits" {
			t.Error("commit route changed")
		}
		return list(commit)
	case "github.commit.read":
		if path != "/commits/abc" {
			t.Error("commit route changed")
		}
		return commit, true
	case "github.pull_request.list":
		if path != "/pulls" {
			t.Error("pull route changed")
		}
		return list(pull)
	case "github.pull_request.read":
		if path != "/pulls/3" || r.Method != "GET" {
			t.Error("pull read changed")
		}
		return pull, true
	case "github.pull_request.create":
		if path != "/pulls" || r.Method != "POST" {
			t.Error("pull create changed")
		}
		return pull, true
	case "github.pull_request.update":
		if path != "/pulls/3" || r.Method != "PATCH" {
			t.Error("pull update changed")
		}
		return pull, true
	case "github.pull_request.merge":
		if path != "/pulls/3/merge" || r.Method != "PUT" {
			t.Error("pull merge changed")
		}
		return `{"merged":true,"sha":"abc"}`, true
	case "github.pull_request.review.list":
		if path != "/pulls/3/reviews" {
			t.Error("review route changed")
		}
		return list(review)
	case "github.pull_request.review.read":
		if path != "/pulls/3/reviews/4" || r.Method != "GET" {
			t.Error("review read changed")
		}
		return review, true
	case "github.pull_request.review.create":
		if path != "/pulls/3/reviews" || r.Method != "POST" {
			t.Error("review create changed")
		}
		return review, true
	case "github.issue.comment.list", "github.issue.comment.read", "github.issue.comment.update", "github.issue.comment.delete":
		if path == "/issues/3" && r.Method == "GET" {
			return `{"number":3,"title":"Title"}`, true
		}
		if strings.HasSuffix(operation, "list") {
			if path != "/issues/3/comments" {
				t.Error("comment list changed")
			}
			return list(comment)
		}
		if path != "/issues/comments/4" {
			t.Error("comment route changed")
		}
		return comment, true
	case "github.check_run.list":
		if path != "/commits/abc/check-runs" {
			t.Error("checks route changed")
		}
		items, _ := list(check)
		return `{"total_count":1,"check_runs":` + items + `}`, true
	case "github.check_run.read":
		if path != "/check-runs/4" {
			t.Error("check route changed")
		}
		return check, true
	case "github.actions.workflow.list":
		if path != "/actions/workflows" {
			t.Error("workflows route changed")
		}
		items, _ := list(workflow)
		return `{"total_count":1,"workflows":` + items + `}`, true
	case "github.actions.workflow.read":
		if path != "/actions/workflows/4" {
			t.Error("workflow route changed")
		}
		return workflow, true
	case "github.actions.workflow.dispatch":
		if path != "/actions/workflows/4/dispatches" || r.Method != "POST" {
			t.Error("dispatch route changed")
		}
		return `{}`, true
	case "github.actions.run.list":
		if path != "/actions/runs" {
			t.Error("runs route changed")
		}
		items, _ := list(run)
		return `{"total_count":1,"workflow_runs":` + items + `}`, true
	case "github.actions.run.read":
		if path != "/actions/runs/4" {
			t.Error("run route changed")
		}
		return run, true
	case "github.actions.run.rerun":
		if path != "/actions/runs/4/rerun" || r.Method != "POST" {
			t.Error("rerun route changed")
		}
		return `{}`, true
	case "github.actions.run.cancel":
		if path != "/actions/runs/4/cancel" || r.Method != "POST" {
			t.Error("cancel route changed")
		}
		return `{}`, true
	case "github.actions.job.list":
		if path != "/actions/runs/4/jobs" {
			t.Error("jobs route changed")
		}
		items, _ := list(job)
		return `{"total_count":1,"jobs":` + items + `}`, true
	case "github.actions.job.read":
		if path != "/actions/jobs/5" {
			t.Error("job route changed")
		}
		return job, true
	}
	return "", false
}
