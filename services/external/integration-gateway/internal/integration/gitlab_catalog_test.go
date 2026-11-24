package integration

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestGitLabJobTracePlainText checks the entire scoped read and receipt path for ordinary terminal formatting and binary input.
// TestGitLabJobTracePlainText проверяет сквозное чтение и квитанцию в заданной области для форматированного журнала и двоичного входа.
func TestGitLabJobTracePlainText(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		wire  string
		trace string
	}{
		{name: "terminal formatting", wire: "section_start:1:build\r\x1b[0K\x1b[32mСборка\x1b[0m\r\n\tOK\rDone\n", trace: "section_start:1:build\nСборка\n\tOK\nDone\n"},
		{name: "invalid UTF-8", wire: "prefix\xff\n"},
		{name: "binary data", wire: "prefix\x00\n"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			adapter := testAdapter(t)
			request := scopedGitLabTestRequest(t, adapter, "gitlab.job.trace.read", map[string]string{
				"base_url": "https://git.example.invalid", "scope_kind": "GROUP", "group_path": "team",
			}, map[string]any{"project_id": 20, "project_path": "team/service", "job_id": 4})
			serve := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet {
					t.Fatal("trace lookup made a mutation")
				}
				var body string
				switch r.URL.Path {
				case "/api/v4/groups/team":
					body = `{"id":10,"full_path":"team"}`
				case "/api/v4/projects/team/service":
					body = `{"id":20,"path_with_namespace":"team/service","namespace":{"id":10,"full_path":"team","kind":"group"}}`
				case "/api/v4/projects/20/jobs/4/trace":
					body = scenario.wire
				default:
					t.Fatalf("unexpected provider route: %s", r.URL.Path)
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}
			adapter.nativeHTTPClient, adapter.providerHTTPClient = serve, serve
			result, err := adapter.Execute(t.Context(), request)
			if scenario.trace == "" {
				var safe *SafeError
				if !errors.As(err, &safe) || safe.Code != "INTEGRATION_RESPONSE_INVALID" || result.Receipt.EffectKey != "" {
					t.Fatal("invalid input yielded an attested trace", err)
				}
				return
			}
			var output struct {
				JobID int    `json:"job_id"`
				Trace string `json:"trace"`
			}
			if err != nil || json.Unmarshal([]byte(result.Summary), &output) != nil || output.JobID != 4 || output.Trace != scenario.trace || result.Receipt.EffectKey == "" {
				t.Fatal("trace did not return the expected plain text and receipt", err)
			}
		})
	}
}

// gitLabExtendedResponse supplies synthetic catalog responses only under the escaped configured project, checking operation-specific routes and pagination before returning branch, commit, note, merge-request or pipeline fixtures.
// gitLabExtendedResponse выдаёт синтетические ответы каталога только в экранированном заданном проекте, проверяя маршруты операций и страницы перед возвратом данных веток, коммитов, заметок, запросов слияния и конвейеров.
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
		// The nested provider fields can exceed 64 KiB even though the typed projection is small.
		// Вложенные поля провайдера могут превышать 64 КиБ, хотя типизированная проекция мала.
		return list("/merge_requests", strings.TrimSuffix(mr, "}")+`,"metadata":{"description":"`+strings.Repeat("x", 96<<10)+`"}}`)
	case "gitlab.merge_request.approval.read":
		check("GET", "/merge_requests/3/approvals")
		return `{"iid":3,"project_id":1,"approved":true,"approvals_required":0,"approvals_left":0,"approved_by":[]}`, true
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
