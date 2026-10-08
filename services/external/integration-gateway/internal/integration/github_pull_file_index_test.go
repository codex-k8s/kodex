package integration

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-github/v74/github"
)

const githubIndexHead = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const githubIndexBase = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func githubIndexPull(total int) *github.PullRequest {
	return &github.PullRequest{Number: github.Ptr(3), Title: github.Ptr("Title"), Body: github.Ptr("Полное описание"), State: github.Ptr("open"),
		Head: &github.PullRequestBranch{Ref: github.Ptr("feature"), SHA: github.Ptr(githubIndexHead)}, Base: &github.PullRequestBranch{Ref: github.Ptr("main"), SHA: github.Ptr(githubIndexBase)},
		ChangedFiles: github.Ptr(total), HTMLURL: github.Ptr("https://github.com/acme/repo/pull/3")}
}

func githubIndexFile(name string) *github.CommitFile {
	return &github.CommitFile{Filename: github.Ptr(name), SHA: github.Ptr(githubIndexHead), Status: github.Ptr("modified"), Additions: github.Ptr(1), Deletions: github.Ptr(2), Changes: github.Ptr(3)}
}

func githubIndexInput(total int) map[string]any {
	return map[string]any{"pull_request_number": 3, "expected_head_sha": githubIndexHead, "expected_base_sha": githubIndexBase, "expected_changed_files": total}
}

type githubIndexFixture struct {
	pull                 func(int) *github.PullRequest
	files                func(int, int) []*github.CommitFile
	next                 func(int, int) int
	pullCalls, fileCalls int
	maximumRaw           int
}

func githubIndexAdapter(t *testing.T, fixture *githubIndexFixture) (*Adapter, *CredentialRevision) {
	t.Helper()
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "test-token")
	adapter.githubHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || r.URL.Host != "api.github.com" || r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatal("source scope or credential changed")
		}
		var data any
		header := http.Header{}
		switch r.URL.Path {
		case "/repos/acme/repo/pulls/3":
			fixture.pullCalls++
			data = fixture.pull(fixture.pullCalls)
		case "/repos/acme/repo/pulls/3/files":
			fixture.fileCalls++
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			limit, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
			if page < 1 || limit < 1 || limit > maximumGitHubPullFilePageSize {
				t.Fatal("unbounded page requested")
			}
			data = fixture.files(page, limit)
			if fixture.next != nil {
				if next := fixture.next(page, limit); next != 0 {
					header.Set("Link", fmt.Sprintf(`<https://api.github.com/repos/acme/repo/pulls/3/files?per_page=%d&page=%d>; rel="next"`, limit, next))
				}
			}
		default:
			t.Fatal("repository route escaped")
		}
		body, err := json.Marshal(data)
		if err != nil {
			t.Fatal("invalid synthetic fixture")
		}
		fixture.maximumRaw = max(fixture.maximumRaw, len(body))
		return &http.Response{Request: r, StatusCode: 200, Header: header, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})}
	return adapter, credential
}

func githubIndexExecute(t *testing.T, adapter *Adapter, credential *CredentialRevision, input map[string]any) (githubPullFilePage, []map[string]any) {
	t.Helper()
	result, err := adapter.Execute(t.Context(), invocationRequest(t, adapter.definitions["github"], "github.pull_request.file.list", input, credential))
	var page githubPullFilePage
	var rows []map[string]any
	if err != nil || len(result.Summary) > maximumResponseBytes || json.Unmarshal([]byte(result.Summary), &page) != nil || json.Unmarshal([]byte(page.Items), &rows) != nil {
		t.Fatal("index failed or exceeded its envelope")
	}
	for _, row := range rows {
		if _, ok := row["patch"]; ok {
			t.Fatal("index leaked patch")
		}
	}
	return page, rows
}

func TestGitHubPullFileIndexPreservesPinnedPagesWithoutPatches(t *testing.T) {
	fixture := &githubIndexFixture{pull: func(int) *github.PullRequest { return githubIndexPull(5) }, files: func(page, limit int) []*github.CommitFile {
		result := []*github.CommitFile{}
		for i := (page - 1) * limit; i < min(page*limit, 5); i++ {
			file := githubIndexFile(fmt.Sprintf("src/Я🙂-%d.txt", i))
			file.Patch = github.Ptr(strings.Repeat("large patch\n", 12000))
			result = append(result, file)
		}
		return result
	}, next: func(page, limit int) int {
		if page*limit < 5 {
			return page + 1
		}
		return 0
	}}
	adapter, credential := githubIndexAdapter(t, fixture)
	seen := map[string]bool{}
	for cursor := 1; cursor <= 2; cursor++ {
		input := githubIndexInput(5)
		input["cursor"] = cursor
		page, rows := githubIndexExecute(t, adapter, credential, input)
		if page.HeadSHA != githubIndexHead || page.BaseSHA != githubIndexBase || page.Total != 5 || page.PageSize != 4 || page.Offset != (cursor-1)*4 || page.Count != min(4, 5-page.Offset) || page.EOF != (cursor == 2) || page.Next != map[int]int{1: 2, 2: 0}[cursor] {
			t.Fatal("pins, count or actual next cursor changed")
		}
		for _, row := range rows {
			name := row["filename"].(string)
			if seen[name] {
				t.Fatal("index duplicated a row")
			}
			seen[name] = true
		}
	}
	if len(seen) != 5 || fixture.pullCalls != 4 || fixture.fileCalls != 2 || fixture.maximumRaw <= maximumResponseBytes {
		t.Fatal("large provider source or page completeness not exercised")
	}
}

func TestGitHubPullFileIndexEmptyAndExactProviderCap(t *testing.T) {
	for _, total := range []int{0, 3000} {
		for _, cursor := range []int{1, max(1, total/4)} {
			fixture := &githubIndexFixture{pull: func(int) *github.PullRequest { return githubIndexPull(total) }, files: func(page, limit int) []*github.CommitFile {
				rows := []*github.CommitFile{}
				for i := (page - 1) * limit; i < min(page*limit, total); i++ {
					rows = append(rows, githubIndexFile(fmt.Sprintf("f%d", i)))
				}
				return rows
			}, next: func(page, limit int) int {
				if page*limit < total {
					return page + 1
				}
				return 0
			}}
			adapter, credential := githubIndexAdapter(t, fixture)
			input := githubIndexInput(total)
			input["cursor"] = cursor
			page, _ := githubIndexExecute(t, adapter, credential, input)
			if page.Count != min(4, total-page.Offset) || page.EOF != (page.Offset+page.Count == total) || fixture.pullCalls != 2 || fixture.fileCalls != 1 {
				t.Fatal("empty or 3000 boundary falsely completed")
			}
		}
	}
}

func TestGitHubPullFileIndexWorstCaseRenameEnvelope(t *testing.T) {
	files := []*github.CommitFile{}
	for i := 0; i < 4; i++ {
		file := githubIndexFile(strconv.Itoa(i) + strings.Repeat("<", 1023))
		file.PreviousFilename = github.Ptr(strings.Repeat("<", 1024))
		file.Status = github.Ptr("renamed")
		file.Additions = github.Ptr(int(maximumGitHubFileChangeCount))
		file.Deletions = github.Ptr(0)
		file.Changes = github.Ptr(int(maximumGitHubFileChangeCount))
		files = append(files, file)
	}
	fixture := &githubIndexFixture{pull: func(int) *github.PullRequest { return githubIndexPull(4) }, files: func(int, int) []*github.CommitFile { return files }}
	adapter, credential := githubIndexAdapter(t, fixture)
	page, rows := githubIndexExecute(t, adapter, credential, githubIndexInput(4))
	encoded, _ := json.Marshal(page)
	if len(rows) != 4 || len(encoded) > maximumResponseBytes || len(encoded) < 57000 || !page.EOF {
		t.Fatal("worst-case escaping fixture does not prove the bound")
	}
	t.Logf("maximum_rename_envelope_bytes=%d", len(encoded))
}

func TestGitHubPullFileIndexRenameDeletionAndNullableBlobSHA(t *testing.T) {
	rename := githubIndexFile("src/новое🙂.txt")
	rename.Status = github.Ptr("renamed")
	rename.PreviousFilename = github.Ptr("src/старое.txt")
	removed := githubIndexFile("src/deleted.bin")
	removed.Status = github.Ptr("removed")
	removed.SHA = nil
	fixture := &githubIndexFixture{pull: func(int) *github.PullRequest { return githubIndexPull(2) }, files: func(int, int) []*github.CommitFile { return []*github.CommitFile{rename, removed} }}
	adapter, credential := githubIndexAdapter(t, fixture)
	_, rows := githubIndexExecute(t, adapter, credential, githubIndexInput(2))
	if rows[0]["previous_filename"] != rename.GetPreviousFilename() || rows[1]["status"] != "removed" {
		t.Fatal("rename or deletion metadata lost")
	}
	if value, exists := rows[1]["sha"]; !exists || value != nil {
		t.Fatal("unavailable blob SHA was invented")
	}
}

func TestGitHubPullFileIndexRejectsDriftAndBrokenPagination(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		pull                 func(int) *github.PullRequest
		files                []*github.CommitFile
		next                 int
		code                 string
		pullCalls, fileCalls int
	}{
		{"head_before", func(int) *github.PullRequest {
			p := githubIndexPull(1)
			p.Head.SHA = github.Ptr(githubIndexBase)
			return p
		}, nil, 0, "INTEGRATION_REQUEST_REJECTED", 1, 0},
		{"base_after", func(call int) *github.PullRequest {
			p := githubIndexPull(1)
			if call == 2 {
				p.Base.SHA = github.Ptr(githubIndexHead)
			}
			return p
		}, []*github.CommitFile{githubIndexFile("a")}, 0, "INTEGRATION_REQUEST_REJECTED", 2, 1},
		{"count_after", func(call int) *github.PullRequest {
			p := githubIndexPull(1)
			if call == 2 {
				p.ChangedFiles = github.Ptr(2)
			}
			return p
		}, []*github.CommitFile{githubIndexFile("a")}, 0, "INTEGRATION_REQUEST_REJECTED", 2, 1},
		{"missing_count", func(int) *github.PullRequest { p := githubIndexPull(1); p.ChangedFiles = nil; return p }, nil, 0, "INTEGRATION_RESPONSE_INVALID", 1, 0},
		{"negative_count", func(int) *github.PullRequest { return githubIndexPull(-1) }, nil, 0, "INTEGRATION_RESPONSE_INVALID", 1, 0},
		{"wrong_number", func(int) *github.PullRequest { p := githubIndexPull(1); p.Number = github.Ptr(4); return p }, nil, 0, "INTEGRATION_RESPONSE_INVALID", 1, 0},
		{"missing_row", func(int) *github.PullRequest { return githubIndexPull(1) }, []*github.CommitFile{}, 0, "INTEGRATION_RESPONSE_INVALID", 1, 1},
		{"extra_row", func(int) *github.PullRequest { return githubIndexPull(1) }, []*github.CommitFile{githubIndexFile("a"), githubIndexFile("b")}, 0, "INTEGRATION_RESPONSE_INVALID", 1, 1},
		{"next_after_eof", func(int) *github.PullRequest { return githubIndexPull(1) }, []*github.CommitFile{githubIndexFile("a")}, 2, "INTEGRATION_RESPONSE_INVALID", 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := &githubIndexFixture{pull: tc.pull, files: func(int, int) []*github.CommitFile { return tc.files }, next: func(int, int) int { return tc.next }}
			adapter, credential := githubIndexAdapter(t, fixture)
			result, err := adapter.Execute(t.Context(), invocationRequest(t, adapter.definitions["github"], "github.pull_request.file.list", githubIndexInput(1), credential))
			var safe *SafeError
			if !errors.As(err, &safe) || safe.Code != tc.code || result.Summary != "" || fixture.pullCalls != tc.pullCalls || fixture.fileCalls != tc.fileCalls {
				t.Fatal("drift or invalid provider response accepted or retried")
			}
		})
	}
	for _, next := range []int{0, 3} {
		fixture := &githubIndexFixture{pull: func(int) *github.PullRequest { return githubIndexPull(5) }, files: func(int, int) []*github.CommitFile {
			return []*github.CommitFile{githubIndexFile("a"), githubIndexFile("b"), githubIndexFile("c"), githubIndexFile("d")}
		}, next: func(int, int) int { return next }}
		adapter, credential := githubIndexAdapter(t, fixture)
		_, err := adapter.Execute(t.Context(), invocationRequest(t, adapter.definitions["github"], "github.pull_request.file.list", githubIndexInput(5), credential))
		var safe *SafeError
		if !errors.As(err, &safe) || safe.Code != "INTEGRATION_RESPONSE_INVALID" || fixture.fileCalls != 1 {
			t.Fatal("missing or discontinuous next cursor accepted")
		}
	}
}

func TestGitHubPullFileIndexRejectsInvalidMetadata(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*github.CommitFile)
	}{
		{"oversized_path", func(f *github.CommitFile) { f.Filename = github.Ptr(strings.Repeat("x", 1025)) }},
		{"traversal", func(f *github.CommitFile) { f.Filename = github.Ptr("../a") }},
		{"control", func(f *github.CommitFile) { f.Filename = github.Ptr("a\tb") }},
		{"bad_sha", func(f *github.CommitFile) { f.SHA = github.Ptr("bad") }},
		{"unknown_status", func(f *github.CommitFile) { f.Status = github.Ptr("unknown") }},
		{"missing_rename_path", func(f *github.CommitFile) { f.Status = github.Ptr("renamed") }},
		{"negative", func(f *github.CommitFile) { f.Additions = github.Ptr(-1) }},
		{"missing", func(f *github.CommitFile) { f.Additions = nil }},
		{"arithmetic", func(f *github.CommitFile) { f.Changes = github.Ptr(4) }},
		{"overflow", func(f *github.CommitFile) { f.Additions = github.Ptr(int(maximumGitHubFileChangeCount + 1)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := githubIndexFile("a")
			tc.change(file)
			if _, err := projectGitHubPullFiles([]*github.CommitFile{file}); err == nil {
				t.Fatal("invalid metadata accepted")
			}
		})
	}
	for _, files := range [][]*github.CommitFile{{nil}, {githubIndexFile("a"), githubIndexFile("a")}} {
		if _, err := projectGitHubPullFiles(files); err == nil {
			t.Fatal("nil or duplicate row accepted")
		}
	}
}

func TestGitHubPullFileIndexRejectsInvalidPinsBeforeProvider(t *testing.T) {
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "test-token")
	adapter.githubHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("rejected input reached provider")
		return nil, nil
	})}
	for _, tc := range []struct {
		key   string
		value any
	}{{"expected_head_sha", strings.Repeat("z", 40)}, {"expected_base_sha", strings.Repeat("z", 40)}, {"cursor", 2}} {
		input := githubIndexInput(1)
		input[tc.key] = tc.value
		_, err := adapter.Execute(t.Context(), invocationRequest(t, adapter.definitions["github"], "github.pull_request.file.list", input, credential))
		var safe *SafeError
		if !errors.As(err, &safe) || safe.Code != "INTEGRATION_REQUEST_REJECTED" {
			t.Fatal("invalid pins or past-EOF page accepted")
		}
	}
	for _, tc := range []struct {
		key    string
		value  any
		remove bool
	}{
		{"limit", 5, false}, {"limit", 0, false}, {"expected_changed_files", 3001, false}, {"expected_changed_files", -1, false},
		{"expected_changed_files", nil, false}, {"expected_head_sha", nil, false}, {"expected_base_sha", true, false},
		{"expected_head_sha", nil, true}, {"expected_base_sha", nil, true}, {"expected_changed_files", nil, true},
	} {
		request := invocationRequest(t, adapter.definitions["github"], "github.pull_request.file.list", githubIndexInput(1), credential)
		if tc.remove {
			delete(request.Input, tc.key)
		} else {
			request.Input[tc.key] = tc.value
		}
		_, err := adapter.Execute(t.Context(), request)
		var safe *SafeError
		if !errors.As(err, &safe) || safe.Code != "INTEGRATION_REQUEST_REJECTED" {
			t.Fatal("invalid required field or page bound reached provider")
		}
	}
}

func TestGitHubPullIndexDoesNotChangeCommitPatchOrMutationReply(t *testing.T) {
	file := githubIndexFile("a")
	file.Patch = github.Ptr("whole commit patch")
	files, err := projectGitHubFiles([]*github.CommitFile{file})
	if err != nil || len(files) != 1 || files[0].Patch == nil || *files[0].Patch != *file.Patch {
		t.Fatal("PR-only index changed commit.read patch")
	}
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "test-token")
	for _, operation := range []string{"github.pull_request.create", "github.pull_request.update"} {
		adapter.githubHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			pull := githubIndexPull(0)
			pull.ChangedFiles = nil
			pull.Base.SHA = nil
			pull.Body = github.Ptr("Полный mutation body")
			raw, _ := json.Marshal(pull)
			return &http.Response{Request: r, StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
		})}
		input := map[string]any{"pull_request_number": 3, "body": "Полный mutation body"}
		if operation == "github.pull_request.create" {
			input = map[string]any{"title": "Title", "head": "feature", "base": "main", "body": "Полный mutation body"}
		}
		result, err := adapter.Execute(t.Context(), invocationRequest(t, adapter.definitions["github"], operation, input, credential))
		var view map[string]any
		if err != nil || json.Unmarshal([]byte(result.Summary), &view) != nil || view["body"] != "Полный mutation body" || view["state"] != "open" {
			t.Fatal("new read-only pins changed mutation outcome or dropped full body/state")
		}
		if _, exists := view["base_sha"]; exists {
			t.Fatal("mutation unexpectedly acquired source pin contract")
		}
	}
}

func TestGitHubPullReadPreservesBodyStateAndAddsExactSourcePins(t *testing.T) {
	fixture := &githubIndexFixture{pull: func(int) *github.PullRequest {
		p := githubIndexPull(3001)
		p.Body = github.Ptr(strings.Repeat("Полный текст🙂\n", 200))
		return p
	}}
	adapter, credential := githubIndexAdapter(t, fixture)
	result, err := adapter.Execute(t.Context(), invocationRequest(t, adapter.definitions["github"], "github.pull_request.read", map[string]any{"pull_request_number": 3}, credential))
	var view githubPullReadView
	if err != nil || json.Unmarshal([]byte(result.Summary), &view) != nil || view.Body != fixture.pull(1).GetBody() || view.State != "open" || view.SHA != githubIndexHead || view.BaseSHA != githubIndexBase || view.ChangedFiles != 3001 {
		t.Fatal("full read dropped body/state or source metadata")
	}
}

func TestGitHubPullFileIndexOversizedRawPatchDoesNotBecomeComplete(t *testing.T) {
	file := githubIndexFile("a")
	file.Patch = github.Ptr(strings.Repeat("x", maximumGitHubProviderResponseBytes))
	fixture := &githubIndexFixture{pull: func(int) *github.PullRequest { return githubIndexPull(1) }, files: func(int, int) []*github.CommitFile { return []*github.CommitFile{file} }}
	adapter, credential := githubIndexAdapter(t, fixture)
	result, err := adapter.Execute(t.Context(), invocationRequest(t, adapter.definitions["github"], "github.pull_request.file.list", githubIndexInput(1), credential))
	var safe *SafeError
	if !errors.As(err, &safe) || safe.Code != "INTEGRATION_RESPONSE_INVALID" || result.Summary != "" || fixture.pullCalls != 1 || fixture.fileCalls != 1 {
		t.Fatal("raw overflow was retried or falsely completed")
	}
}
