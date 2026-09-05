package integration

import (
	"net/http"
	"strings"
	"testing"
)

func gitLabExtendedResponse(t *testing.T, operation string, r *http.Request) (string, bool) {
	t.Helper()
	if !strings.HasPrefix(operation, "gitlab.") {
		return "", false
	}
	if !strings.HasPrefix(r.URL.EscapedPath(), "/api/v4/projects/group%2Fproject/") {
		return "", false
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v4/projects/group/project")
	check := func(method, want string) {
		if r.Method != method || path != want {
			t.Errorf("provider route changed: %s %s", r.Method, path)
		}
	}
	list := func(want, item string) (string, bool) {
		check("GET", want)
		if r.URL.Query().Get("page") != "2" || r.URL.Query().Get("per_page") != "1" {
			t.Error("pagination changed")
		}
		return "[" + item + "]", true
	}
	commit := `{"id":"abc","short_id":"abc","title":"Title","message":"Text","web_url":"https://gitlab.example.test/commit"}`
	branch := `{"name":"main","protected":false,"default":true,"commit":` + commit + `}`
	note := `{"id":4,"body":"Text","system":false,"noteable_iid":3,"noteable_type":"Issue"}`
	mr := `{"iid":3,"title":"Title","state":"opened","description":"Text","source_branch":"feature","target_branch":"main","web_url":"https://gitlab.example.test/mr"}`
	pipeline := `{"id":3,"status":"canceled","ref":"main","sha":"abc","web_url":"https://gitlab.example.test/pipeline"}`
	job := `{"id":4,"name":"Build","stage":"test","status":"success","ref":"main","web_url":"https://gitlab.example.test/job","pipeline":` + pipeline + `}`
	switch operation {
	case "gitlab.merge_request.diff.list":
		return list("/merge_requests/3/diffs", `{"old_path":"a.txt","new_path":"a.txt","diff":"Text"}`)
	case "gitlab.branch.list":
		return list("/repository/branches", branch)
	case "gitlab.branch.read":
		check("GET", "/repository/branches/main")
		return branch, true
	case "gitlab.branch.delete":
		check("DELETE", "/repository/branches/feature")
		return `{}`, true
	case "gitlab.commit.list":
		return list("/repository/commits", commit)
	case "gitlab.commit.read":
		check("GET", "/repository/commits/abc")
		return commit, true
	case "gitlab.commit.diff":
		return list("/repository/commits/abc/diff", `{"old_path":"a.txt","new_path":"a.txt","diff":"Text"}`)
	case "gitlab.repository.tree.list":
		return list("/repository/tree", `{"id":"abc","name":"a.txt","path":"a.txt","type":"blob","mode":"100644"}`)
	case "gitlab.issue.note.list":
		return list("/issues/3/notes", note)
	case "gitlab.issue.note.read":
		check("GET", "/issues/3/notes/4")
		return note, true
	case "gitlab.issue.note.create":
		check("POST", "/issues/3/notes")
		return note, true
	case "gitlab.issue.note.update":
		check("PUT", "/issues/3/notes/4")
		return note, true
	case "gitlab.issue.note.delete":
		check("DELETE", "/issues/3/notes/4")
		return `{}`, true
	case "gitlab.merge_request.list":
		return list("/merge_requests", mr)
	case "gitlab.merge_request.update":
		check("PUT", "/merge_requests/3")
		return mr, true
	case "gitlab.merge_request.merge":
		check("PUT", "/merge_requests/3/merge")
		return mr, true
	case "gitlab.merge_request.discussion.list":
		return list("/merge_requests/3/discussions", `{"id":"discussion","individual_note":true,"notes":[{"id":4,"body":"Text","noteable_iid":3,"noteable_type":"MergeRequest"}]}`)
	case "gitlab.pipeline.list":
		return list("/pipelines", pipeline)
	case "gitlab.pipeline.cancel":
		check("POST", "/pipelines/3/cancel")
		return pipeline, true
	case "gitlab.job.list":
		return list("/pipelines/3/jobs", job)
	case "gitlab.job.read":
		check("GET", "/jobs/4")
		return job, true
	case "gitlab.job.retry":
		check("POST", "/jobs/4/retry")
		return job, true
	case "gitlab.job.cancel":
		check("POST", "/jobs/4/cancel")
		return job, true
	case "gitlab.job.trace.read":
		check("GET", "/jobs/4/trace")
		return "job output", true
	}
	return "", false
}
