package integration

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/google/go-github/v74/github"
)

type githubDirectoryFixturePage struct {
	Items         string `json:"items"`
	Path          string `json:"path"`
	CommitSHA     string `json:"commit_sha"`
	CatalogDigest string `json:"catalog_digest"`
	Count         int    `json:"count"`
	TotalCount    int    `json:"total_count"`
	Cursor        int    `json:"cursor"`
	NextCursor    int    `json:"next_cursor"`
	EOF           bool   `json:"eof"`
}

func githubDirectoryFixtureEntries(count int, prefix string) []*github.RepositoryContent {
	entries := make([]*github.RepositoryContent, count)
	for index := range entries {
		entries[index] = &github.RepositoryContent{Path: github.Ptr(fmt.Sprintf("%s%04d.go", prefix, index)),
			Type: github.Ptr("file"), SHA: github.Ptr(strings.Repeat("b", 40)), Size: github.Ptr(index)}
	}
	return entries
}

func githubDirectoryFixtureAdapter(t *testing.T, entries []*github.RepositoryContent, path string) (*Adapter, *CredentialRevision, *int) {
	t.Helper()
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "test-token")
	calls := 0
	adapter.githubHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodGet || r.URL.Host != "api.github.com" || r.URL.Path != "/repos/acme/repo/contents/"+path ||
			r.URL.Query().Get("ref") != githubPageFixtureCommit || r.URL.Query().Get("page") != "" || r.URL.Query().Get("per_page") != "" ||
			r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatal("directory read changed exact repository/commit/path or credential boundary")
		}
		body, err := json.Marshal(entries)
		if err != nil || len(body) > maximumGitHubProviderResponseBytes {
			t.Fatal("invalid bounded directory fixture")
		}
		return &http.Response{Request: r, StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(body))}, nil
	})}
	return adapter, credential, &calls
}

func githubDirectoryFixtureWire(t *testing.T, summary string) []byte {
	t.Helper()
	// Та же публичная форма callback: receipt не входит в model result.
	native := map[string]any{"ok": true, "invocationRef": strings.Repeat("i", 128), "result": summary}
	encoded, err := json.Marshal(native)
	if err != nil {
		t.Fatal(err)
	}
	var wire bytes.Buffer
	if err := json.NewEncoder(&wire).Encode(map[string]any{"jsonrpc": "2.0", "id": strings.Repeat("i", 128), "result": map[string]any{
		"content": []map[string]string{{"type": "text", "text": string(encoded)}}, "structuredContent": native, "isError": false,
	}}); err != nil {
		t.Fatal(err)
	}
	return wire.Bytes()
}

func TestGitHubDirectoryPagesLargeCatalogComplete(t *testing.T) {
	entries := githubDirectoryFixtureEntries(507, "src/"+strings.Repeat("r", 95))
	entries[0].DownloadURL = github.Ptr("https://private-provider.example.test/content")
	entries[0].Content = github.Ptr("private-provider-body")
	adapter, credential, calls := githubDirectoryFixtureAdapter(t, entries, "src")
	input := map[string]any{"path": "src", "ref": githubPageFixtureCommit}
	var all []githubContentView
	digest := ""
	for iteration := 0; iteration < len(entries)+1; iteration++ {
		request := invocationRequest(t, adapter.definitions["github"], "github.repository.content.list", input, credential)
		result, err := adapter.Execute(t.Context(), request)
		if err != nil {
			t.Fatalf("bounded complete directory page failed: %v", err)
		}
		var page githubDirectoryFixturePage
		var rows []githubContentView
		if json.Unmarshal([]byte(result.Summary), &page) != nil || json.Unmarshal([]byte(page.Items), &rows) != nil ||
			len(result.Summary) > maximumResponseBytes || len(githubDirectoryFixtureWire(t, result.Summary)) > 64<<10 ||
			page.Path != "src" || page.CommitSHA != githubPageFixtureCommit || page.TotalCount != len(entries) ||
			page.Count != len(rows) || page.Count < 1 || page.Count > 20 || page.Cursor != len(all) {
			t.Fatal("directory page lost its bounded immutable index contract")
		}
		if strings.Contains(result.Summary, "private-provider") || strings.Contains(result.Summary, "download_url") || strings.Contains(result.Summary, "content_base64") {
			t.Fatal("directory projection leaked provider body or opaque URL")
		}
		if digest == "" {
			digest = page.CatalogDigest
		}
		if digest == "" || page.CatalogDigest != digest {
			t.Fatal("directory snapshot changed during continuation")
		}
		responseDigest := sha256.Sum256([]byte(result.Summary))
		if result.Receipt.ResponseDigest != hex.EncodeToString(responseDigest[:]) || result.Receipt.InputDigest != request.InputDigest ||
			result.Receipt.EffectKey != request.EffectKey || result.Receipt.ProviderEffectRef == "" {
			t.Fatal("page receipt lost exact invocation pins")
		}
		all = append(all, rows...)
		if page.EOF {
			if page.NextCursor != 0 || len(all) != len(entries) {
				t.Fatal("premature or ambiguous directory EOF")
			}
			break
		}
		if page.NextCursor != len(all) {
			t.Fatal("directory offset skipped or repeated entries")
		}
		input["cursor"], input["expected_catalog_digest"] = page.NextCursor, digest
	}
	if len(all) != len(entries) || *calls < 2 {
		t.Fatal("unpaged directory or incomplete index")
	}
	for index, row := range all {
		if row.Path != entries[index].GetPath() {
			t.Fatal("directory order or contents changed")
		}
	}
}

func TestGitHubDirectoryPagesRejectMalformedWholeIndex(t *testing.T) {
	for _, fixture := range []struct {
		name   string
		mutate func([]*github.RepositoryContent) []*github.RepositoryContent
	}{
		{"cap_1000", func(_ []*github.RepositoryContent) []*github.RepositoryContent {
			return githubDirectoryFixtureEntries(1000, "src/")
		}},
		{"cap_1001", func(_ []*github.RepositoryContent) []*github.RepositoryContent {
			return githubDirectoryFixtureEntries(1001, "src/")
		}},
		{"nil_tail", func(rows []*github.RepositoryContent) []*github.RepositoryContent { rows[30] = nil; return rows }},
		{"foreign_tail", func(rows []*github.RepositoryContent) []*github.RepositoryContent {
			rows[30].Path = github.Ptr("foreign/a.go")
			return rows
		}},
		{"nested_tail", func(rows []*github.RepositoryContent) []*github.RepositoryContent {
			rows[30].Path = github.Ptr("src/nested/a.go")
			return rows
		}},
		{"duplicate_tail", func(rows []*github.RepositoryContent) []*github.RepositoryContent {
			rows[30].Path = rows[0].Path
			return rows
		}},
		{"unknown_type", func(rows []*github.RepositoryContent) []*github.RepositoryContent {
			rows[30].Type = github.Ptr("unknown")
			return rows
		}},
		{"bad_sha", func(rows []*github.RepositoryContent) []*github.RepositoryContent {
			rows[30].SHA = github.Ptr("abc")
			return rows
		}},
		{"missing_size", func(rows []*github.RepositoryContent) []*github.RepositoryContent { rows[30].Size = nil; return rows }},
		{"negative_size", func(rows []*github.RepositoryContent) []*github.RepositoryContent {
			rows[30].Size = github.Ptr(-1)
			return rows
		}},
		{"control_path", func(rows []*github.RepositoryContent) []*github.RepositoryContent {
			rows[30].Path = github.Ptr("src/a\t.go")
			return rows
		}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			adapter, credential, calls := githubDirectoryFixtureAdapter(t, fixture.mutate(githubDirectoryFixtureEntries(31, "src/")), "src")
			result, err := adapter.Execute(t.Context(), invocationRequest(t, adapter.definitions["github"], "github.repository.content.list", map[string]any{"path": "src", "ref": githubPageFixtureCommit}, credential))
			var safe *SafeError
			if !errors.As(err, &safe) || safe.Code != "INTEGRATION_RESPONSE_INVALID" || result.Summary != "" || *calls != 1 {
				t.Fatal("invalid full index was partially returned, retried or leaked")
			}
		})
	}
}

func TestGitHubDirectoryPagesAdaptiveWireAndSortedSnapshot(t *testing.T) {
	entries := githubDirectoryFixtureEntries(80, "src/"+strings.Repeat("\"<&🙂", 100))
	adapter, credential, _ := githubDirectoryFixtureAdapter(t, entries, "src")
	input := map[string]any{"path": "src", "ref": githubPageFixtureCommit, "limit": 50}
	var all []githubContentView
	digest := ""
	for len(all) < len(entries) {
		result, err := adapter.Execute(t.Context(), invocationRequest(t, adapter.definitions["github"], "github.repository.content.list", input, credential))
		if err != nil {
			t.Fatal(err)
		}
		var page githubDirectoryFixturePage
		var rows []githubContentView
		if json.Unmarshal([]byte(result.Summary), &page) != nil || json.Unmarshal([]byte(page.Items), &rows) != nil || page.Count < 1 || page.Count >= 50 ||
			page.Cursor != len(all) || len(githubDirectoryFixtureWire(t, result.Summary)) > 64<<10 {
			t.Fatal("escaped adaptive page exceeded wire budget or lost continuity")
		}
		if digest == "" {
			digest = page.CatalogDigest
		}
		if digest != page.CatalogDigest {
			t.Fatal("sorted snapshot digest changed")
		}
		all = append(all, rows...)
		if page.EOF != (len(all) == len(entries)) {
			t.Fatal("adaptive count was mistaken for EOF")
		}
		if !page.EOF && page.NextCursor != len(all) {
			t.Fatal("adaptive next offset differs from consumed count")
		}
		input["cursor"], input["expected_catalog_digest"] = page.NextCursor, digest
		// Upstream order не является порядком канонической страницы.
		sort.Slice(entries, func(i, j int) bool { return entries[i].GetPath() > entries[j].GetPath() })
	}
	if !sort.SliceIsSorted(all, func(i, j int) bool { return all[i].Path < all[j].Path }) {
		t.Fatal("index is not deterministically ordered")
	}
}

func TestGitHubDirectoryPagesInputAndContinuationFailClosed(t *testing.T) {
	for _, fixture := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"missing_commit", func(in map[string]any) { delete(in, "ref") }},
		{"branch_ref", func(in map[string]any) { in["ref"] = "main" }},
		{"uppercase_commit", func(in map[string]any) { in["ref"] = strings.Repeat("A", 40) }},
		{"foreign_path", func(in map[string]any) { in["path"] = "../foreign" }},
		{"negative_offset", func(in map[string]any) { in["cursor"] = -1 }},
		{"oversized_offset", func(in map[string]any) { in["cursor"] = 1000 }},
		{"zero_limit", func(in map[string]any) { in["limit"] = 0 }},
		{"oversized_limit", func(in map[string]any) { in["limit"] = 51 }},
		{"missing_digest", func(in map[string]any) { in["cursor"] = 1 }},
		{"malformed_digest", func(in map[string]any) { in["expected_catalog_digest"] = "sha256:invalid" }},
		{"caller_repo_override", func(in map[string]any) { in["repository"] = "foreign" }},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			adapter, credential, calls := githubDirectoryFixtureAdapter(t, githubDirectoryFixtureEntries(2, "src/"), "src")
			input := map[string]any{"path": "src", "ref": githubPageFixtureCommit}
			fixture.mutate(input)
			request := invocationRequest(t, adapter.definitions["github"], "github.repository.content.list", map[string]any{"ref": githubPageFixtureCommit}, credential)
			request.Input = input
			encoded, _ := json.Marshal(input)
			digest := sha256.Sum256(encoded)
			request.InputDigest = hex.EncodeToString(digest[:])
			result, err := adapter.Execute(t.Context(), request)
			var safe *SafeError
			if !errors.As(err, &safe) || safe.Code != "INTEGRATION_REQUEST_REJECTED" || result.Summary != "" || *calls != 0 {
				t.Fatal("invalid selector reached provider or returned partial data")
			}
		})
	}
	for _, cursor := range []int{0, 1, 3} {
		adapter, credential, calls := githubDirectoryFixtureAdapter(t, githubDirectoryFixtureEntries(2, "src/"), "src")
		result, err := adapter.Execute(t.Context(), invocationRequest(t, adapter.definitions["github"], "github.repository.content.list", map[string]any{
			"path": "src", "ref": githubPageFixtureCommit, "cursor": cursor, "expected_catalog_digest": "sha256:" + strings.Repeat("c", 64),
		}, credential))
		var safe *SafeError
		if !errors.As(err, &safe) || safe.Code != "INTEGRATION_REQUEST_REJECTED" || result.Summary != "" || *calls != 1 {
			t.Fatal("mismatched pins were replayed or accepted")
		}
	}
}

func TestGitHubDirectoryPagesEmptyAndExactTerminalOffset(t *testing.T) {
	for _, count := range []int{0, 1, 999} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			adapter, credential, calls := githubDirectoryFixtureAdapter(t, githubDirectoryFixtureEntries(count, ""), "")
			first, err := adapter.Execute(t.Context(), invocationRequest(t, adapter.definitions["github"], "github.repository.content.list", map[string]any{"ref": githubPageFixtureCommit}, credential))
			if err != nil {
				t.Fatal(err)
			}
			var page githubDirectoryFixturePage
			if json.Unmarshal([]byte(first.Summary), &page) != nil || page.Path != "" || page.Cursor != 0 || page.Count != min(20, count) || page.EOF != (count <= 20) {
				t.Fatal("root/default page or empty catalog contract changed")
			}
			terminal, err := adapter.Execute(t.Context(), invocationRequest(t, adapter.definitions["github"], "github.repository.content.list", map[string]any{
				"ref": githubPageFixtureCommit, "cursor": count, "expected_catalog_digest": page.CatalogDigest,
			}, credential))
			if err != nil {
				t.Fatal(err)
			}
			if json.Unmarshal([]byte(terminal.Summary), &page) != nil || page.Items != "[]" || page.Count != 0 || !page.EOF || page.Cursor != count ||
				strings.Contains(terminal.Summary, "next_cursor") || *calls != 2 {
				t.Fatal("exact total offset must return unambiguous empty EOF")
			}
		})
	}
}

func TestGitHubDirectoryPagesDigestBindsRepositoryDirectoryCommitAndEntireIndex(t *testing.T) {
	in := githubCatalogInput{Path: "src", Ref: githubPageFixtureCommit, Limit: 20}
	entries := githubDirectoryFixtureEntries(31, "src/")
	first, err := projectGitHubDirectoryPage(entries, "acme", "repo", in)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct {
		owner, repo string
		input       githubCatalogInput
		rows        []*github.RepositoryContent
	}{
		{"foreign", "repo", in, entries}, {"acme", "foreign", in, entries},
		{"acme", "repo", githubCatalogInput{Path: "other", Ref: in.Ref, Limit: 20}, githubDirectoryFixtureEntries(31, "other/")},
		{"acme", "repo", githubCatalogInput{Path: in.Path, Ref: strings.Repeat("d", 40), Limit: 20}, entries},
		{"acme", "repo", in, githubDirectoryFixtureEntries(30, "src/")},
	} {
		page, err := projectGitHubDirectoryPage(change.rows, change.owner, change.repo, change.input)
		if err != nil || page.CatalogDigest == first.CatalogDigest {
			t.Fatal("catalog digest did not bind exact full scope")
		}
	}
	entries[30].SHA = github.Ptr(strings.Repeat("d", 40))
	page, err := projectGitHubDirectoryPage(entries, "acme", "repo", in)
	if err != nil || page.CatalogDigest == first.CatalogDigest {
		t.Fatal("hidden tail is not committed by catalog digest")
	}
	entries[30].Path = github.Ptr("src/\xff.go")
	if _, err := projectGitHubDirectoryPage(entries, "acme", "repo", in); err == nil {
		t.Fatal("invalid UTF-8 index path accepted")
	}
}

func TestGitHubDirectoryPagesPreserveCurrentPackageAndGrantPins(t *testing.T) {
	for _, fixture := range []struct {
		name, code string
		mutate     func(*Request)
	}{
		{"historical_version", "INTEGRATION_CONFIGURATION_INVALID", func(r *Request) { r.DefinitionVersion = "4.0.0" }},
		{"current_digest_mismatch", "INTEGRATION_CONFIGURATION_INVALID", func(r *Request) { r.DefinitionDigest = strings.Repeat("c", 64) }},
		{"revoked_grant", "INTEGRATION_GRANT_INVALID", func(r *Request) { r.GrantRef = "" }},
		{"stale_grant", "INTEGRATION_GRANT_INVALID", func(r *Request) { r.GrantVersion = 0 }},
		{"foreign_scope", "INTEGRATION_REQUEST_REJECTED", func(r *Request) { r.ResourceScope["repository"] = "foreign" }},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			adapter, credential, calls := githubDirectoryFixtureAdapter(t, githubDirectoryFixtureEntries(1, "src/"), "src")
			request := invocationRequest(t, adapter.definitions["github"], "github.repository.content.list", map[string]any{"path": "src", "ref": githubPageFixtureCommit}, credential)
			fixture.mutate(&request)
			result, err := adapter.Execute(t.Context(), request)
			var safe *SafeError
			if !errors.As(err, &safe) || safe.Code != fixture.code || result.Summary != "" || *calls != 0 {
				t.Fatal("invalid authority/pins reached provider or disclosed data")
			}
		})
	}
}
