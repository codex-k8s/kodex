package integration

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/go-github/v74/github"
)

const githubPageFixtureCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func githubPageFixtureFile(content, path string) *github.RepositoryContent {
	// SHA-1 здесь проверяет Git object protocol, а не служит security digest.
	gitHash := sha1.Sum([]byte("blob " + strconv.Itoa(len(content)) + "\x00" + content))
	return &github.RepositoryContent{Path: github.Ptr(path), Type: github.Ptr("file"), SHA: github.Ptr(hex.EncodeToString(gitHash[:])),
		Size: github.Ptr(len(content)), Encoding: github.Ptr("base64"), Content: github.Ptr(base64.StdEncoding.EncodeToString([]byte(content)))}
}

func githubPageFixtureAdapter(t *testing.T, file *github.RepositoryContent) (*Adapter, *CredentialRevision, *int) {
	t.Helper()
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "test-token")
	count := 0
	adapter.githubHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		count++
		if r.Method != http.MethodGet || r.URL.Host != "api.github.com" || r.URL.Path != "/repos/acme/repo/contents/"+file.GetPath() ||
			r.URL.Query().Get("ref") != githubPageFixtureCommit || r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatal("exact repository, commit, method or credential boundary changed")
		}
		body, err := json.Marshal(file)
		if err != nil || len(body) > maximumGitHubProviderResponseBytes {
			t.Fatalf("fixture exceeds the unchanged HTTP bound: size=%d err=%v", len(body), err)
		}
		return &http.Response{Request: r, StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})}
	return adapter, credential, &count
}

func githubPageFixtureRead(t *testing.T, adapter *Adapter, credential *CredentialRevision, input map[string]any) githubContentPage {
	t.Helper()
	request := invocationRequest(t, adapter.definitions["github"], "github.repository.content.read", input, credential)
	result, err := adapter.Execute(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	var page githubContentPage
	if json.Unmarshal([]byte(result.Summary), &page) != nil || strings.Contains(result.Summary, "content_base64") {
		t.Fatal("native read returned an invalid page or a whole-file fallback")
	}
	responseHash := sha256.Sum256([]byte(result.Summary))
	if result.Receipt.ResponseDigest != hex.EncodeToString(responseHash[:]) || result.Receipt.InputDigest != request.InputDigest ||
		result.Receipt.EffectKey != request.EffectKey || result.Receipt.ProviderEffectRef != "github-content:"+page.SHA {
		t.Fatal("page receipt lost the existing invocation pins")
	}
	return page
}

func TestGitHubContentPagesReadFullPinnedSourceToEOF(t *testing.T) {
	for _, fixture := range []struct {
		name, content string
		maximum       int
	}{
		{"instructions_45730_bytes", strings.Repeat("x", 45730), 2048},
		{"source_over_projection_budget", strings.Repeat("x", 96<<10), 2048},
		{"unicode", strings.Repeat("Я🙂e\u0301\n", 300), 7},
		{"four_byte_runes", strings.Repeat("🙂", 9), 4},
		{"empty", "", 2048},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			file := githubPageFixtureFile(fixture.content, "AGENTS.md")
			adapter, credential, count := githubPageFixtureAdapter(t, file)
			sourceHash := sha256.Sum256([]byte(fixture.content))
			offset := int64(0)
			var reconstructed strings.Builder
			for iteration := 0; iteration < len(fixture.content)+1; iteration++ {
				input := map[string]any{"path": file.GetPath(), "ref": githubPageFixtureCommit}
				if fixture.maximum != 2048 {
					input["maximum_bytes"] = fixture.maximum
				}
				if offset > 0 {
					input["offset_bytes"], input["expected_sha"] = offset, file.GetSHA()
				}
				page := githubPageFixtureRead(t, adapter, credential, input)
				chunkHash := sha256.Sum256([]byte(page.Text))
				if page.Path != file.GetPath() || page.Type != "file" || page.SHA != file.GetSHA() || page.CommitSHA != githubPageFixtureCommit ||
					page.Size != len(fixture.content) || page.OffsetBytes != offset || page.NextOffsetBytes != offset+int64(len(page.Text)) ||
					!utf8.ValidString(page.Text) || len(page.Text) > fixture.maximum || page.Text == "" && !page.EOF ||
					page.SourceDigest != "sha256:"+hex.EncodeToString(sourceHash[:]) || page.ChunkDigest != "sha256:"+hex.EncodeToString(chunkHash[:]) ||
					page.EOF != (page.NextOffsetBytes == int64(len(fixture.content))) {
					t.Fatalf("invalid pinned page: %#v", page)
				}
				reconstructed.WriteString(page.Text)
				offset = page.NextOffsetBytes
				if page.EOF {
					break
				}
			}
			if reconstructed.String() != fixture.content || offset != int64(len(fixture.content)) || *count == 0 {
				t.Fatal("pages did not reconstruct the entire source to EOF")
			}
			// Повтор страницы EOF возвращает только пустой текст с теми же pins.
			page := githubPageFixtureRead(t, adapter, credential, map[string]any{"path": file.GetPath(), "ref": githubPageFixtureCommit,
				"offset_bytes": offset, "expected_sha": file.GetSHA()})
			if !page.EOF || page.Text != "" || page.NextOffsetBytes != offset {
				t.Fatal("EOF replay changed the source")
			}
		})
	}
}

func TestGitHubContentPagesRejectInvalidInput(t *testing.T) {
	for _, fixture := range []struct {
		name string
		edit func(*githubCatalogInput)
	}{
		{"mutable_ref", func(in *githubCatalogInput) { in.Ref = "main" }},
		{"missing_ref", func(in *githubCatalogInput) { in.Ref = "" }},
		{"uppercase_ref", func(in *githubCatalogInput) { in.Ref = strings.Repeat("A", 40) }},
		{"short_ref", func(in *githubCatalogInput) { in.Ref = "abc" }},
		{"empty_path", func(in *githubCatalogInput) { in.Path = "" }},
		{"path_escape", func(in *githubCatalogInput) { in.Path = "../AGENTS.md" }},
		{"long_path", func(in *githubCatalogInput) { in.Path = strings.Repeat("x", 1025) }},
		{"negative_offset", func(in *githubCatalogInput) { in.OffsetBytes = -1 }},
		{"oversized_offset", func(in *githubCatalogInput) { in.OffsetBytes = maximumGitHubContentSourceBytes + 1 }},
		{"continuation_without_sha", func(in *githubCatalogInput) { in.OffsetBytes = 1 }},
		{"invalid_expected_sha", func(in *githubCatalogInput) { in.ExpectedSHA = "abc" }},
		{"maximum_zero", func(in *githubCatalogInput) { in.MaximumBytes = github.Ptr(0) }},
		{"maximum_small", func(in *githubCatalogInput) { in.MaximumBytes = github.Ptr(3) }},
		{"maximum_large", func(in *githubCatalogInput) { in.MaximumBytes = github.Ptr(2049) }},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			in := githubCatalogInput{Path: "AGENTS.md", Ref: githubPageFixtureCommit}
			fixture.edit(&in)
			_, err := validateGitHubContentPageInput(in)
			assertGitHubPageError(t, err, "INTEGRATION_REQUEST_REJECTED")
		})
	}
	file := githubPageFixtureFile("Text", "AGENTS.md")
	adapter, credential, count := githubPageFixtureAdapter(t, file)
	// Schema validation сохраняет mutable ref для server-owned source work;
	// native execution закрыто отклоняет его до любого provider HTTP.
	request := invocationRequest(t, adapter.definitions["github"], "github.repository.content.read", map[string]any{"path": file.GetPath(), "ref": "main"}, credential)
	_, err := adapter.Execute(t.Context(), request)
	assertGitHubPageError(t, err, "INTEGRATION_REQUEST_REJECTED")
	if *count != 0 {
		t.Fatal("mutable ref reached the provider")
	}
}

func TestGitHubContentPagesLargeSourcePreservesPinsAndDeliveryBudget(t *testing.T) {
	for _, size := range []int{536156, maximumGitHubContentSourceBytes} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			content := strings.Repeat("x", size)
			file := githubPageFixtureFile(content, "docs/operations/large-source.md")
			adapter, credential, calls := githubPageFixtureAdapter(t, file)
			sourceHash := sha256.Sum256([]byte(content))
			// Проверяется весь executable adapter, включая generated package
			// input/output schema, а не только локальное разбиение строки.
			for _, offset := range []int64{0, (64 << 10) + 3, int64(size - 2048), int64(size)} {
				input := map[string]any{"path": file.GetPath(), "ref": githubPageFixtureCommit,
					"offset_bytes": offset, "maximum_bytes": 2048}
				if offset > 0 {
					input["expected_sha"] = file.GetSHA()
				}
				page := githubPageFixtureRead(t, adapter, credential, input)
				end := min(offset+2048, int64(size))
				chunkHash := sha256.Sum256([]byte(content[offset:end]))
				if page.Text != content[offset:end] || page.OffsetBytes != offset || page.NextOffsetBytes != end ||
					page.Size != size || page.SHA != file.GetSHA() || page.CommitSHA != githubPageFixtureCommit ||
					page.SourceDigest != "sha256:"+hex.EncodeToString(sourceHash[:]) ||
					page.ChunkDigest != "sha256:"+hex.EncodeToString(chunkHash[:]) || page.EOF != (end == int64(size)) ||
					!githubContentPageFitsEnvelope(page) {
					t.Fatal("large source lost immutable pins, exact offset, EOF or native delivery budget")
				}
			}
			if *calls != 4 {
				t.Fatal("large source added a provider fallback or repeated a successful read")
			}
		})
	}
}

func TestGitHubContentPagesLargeSourceDoesNotHideInvalidTail(t *testing.T) {
	for _, tail := range []string{"\xff", "\x00"} {
		file := githubPageFixtureFile(strings.Repeat("x", 96<<10)+tail, "AGENTS.md")
		adapter, credential, calls := githubPageFixtureAdapter(t, file)
		result, err := adapter.Execute(t.Context(), invocationRequest(t, adapter.definitions["github"],
			"github.repository.content.read", map[string]any{"path": file.GetPath(), "ref": githubPageFixtureCommit}, credential))
		assertGitHubPageError(t, err, "INTEGRATION_RESPONSE_INVALID")
		if result.Summary != "" || *calls != 1 {
			t.Fatal("invalid large source returned a partial page or retried a local validation refusal")
		}
	}
}

func TestGitHubContentPagesVerifyEntireSourceAndMetadata(t *testing.T) {
	for _, fixture := range []struct {
		name string
		edit func(*github.RepositoryContent)
	}{
		{"path_mismatch", func(file *github.RepositoryContent) { file.Path = github.Ptr("other.md") }},
		{"not_file", func(file *github.RepositoryContent) { file.Type = github.Ptr("symlink") }},
		{"encoding", func(file *github.RepositoryContent) { file.Encoding = github.Ptr("none") }},
		{"negative_size", func(file *github.RepositoryContent) { file.Size = github.Ptr(-1) }},
		{"wrong_size", func(file *github.RepositoryContent) { file.Size = github.Ptr(file.GetSize() + 1) }},
		{"oversized", func(file *github.RepositoryContent) { file.Size = github.Ptr(maximumGitHubContentSourceBytes + 1) }},
		{"bad_base64", func(file *github.RepositoryContent) { file.Content = github.Ptr("!") }},
		{"invalid_sha", func(file *github.RepositoryContent) { file.SHA = github.Ptr("abc") }},
		{"wrong_git_blob_sha", func(file *github.RepositoryContent) { file.SHA = github.Ptr(strings.Repeat("b", 40)) }},
		{"invalid_utf8_after_page", func(file *github.RepositoryContent) {
			*file = *githubPageFixtureFile(strings.Repeat("x", 2048)+"\xff", "AGENTS.md")
		}},
		{"nul_after_page", func(file *github.RepositoryContent) {
			*file = *githubPageFixtureFile(strings.Repeat("x", 2048)+"\x00", "AGENTS.md")
		}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			file := githubPageFixtureFile("Text", "AGENTS.md")
			fixture.edit(file)
			_, err := projectGitHubContentPage(file, githubCatalogInput{Path: "AGENTS.md", Ref: githubPageFixtureCommit}, 2048)
			assertGitHubPageError(t, err, "INTEGRATION_RESPONSE_INVALID")
		})
	}
	file := githubPageFixtureFile("Я🙂Text", "AGENTS.md")
	for _, in := range []githubCatalogInput{
		{Path: file.GetPath(), Ref: githubPageFixtureCommit, ExpectedSHA: strings.Repeat("b", 40)},
		{Path: file.GetPath(), Ref: githubPageFixtureCommit, OffsetBytes: int64(file.GetSize() + 1), ExpectedSHA: file.GetSHA()},
		{Path: file.GetPath(), Ref: githubPageFixtureCommit, OffsetBytes: 1, ExpectedSHA: file.GetSHA()},
	} {
		_, err := projectGitHubContentPage(file, in, 2048)
		assertGitHubPageError(t, err, "INTEGRATION_REQUEST_REJECTED")
	}
}

func TestGitHubContentPagesBoundActualSerializedNativeEnvelope(t *testing.T) {
	for _, text := range []string{strings.Repeat("\x01", 2048), strings.Repeat("\\\"<>&", 500), strings.Repeat("🙂", 512)} {
		path := strings.Repeat("p", 1024)
		file := githubPageFixtureFile(text, path)
		adapter, credential, _ := githubPageFixtureAdapter(t, file)
		page := githubPageFixtureRead(t, adapter, credential, map[string]any{"path": path, "ref": githubPageFixtureCommit})
		summary, _ := json.Marshal(page)
		// Независимо собираем реальный wire, включая два MCP представления и
		// JSON-RPC envelope; не используем проверяемый budget helper.
		native := map[string]any{"ok": true, "invocationRef": strings.Repeat("i", 128), "result": string(summary)}
		encoded, _ := json.Marshal(native)
		wire, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": strings.Repeat("i", 128), "result": map[string]any{
			"content": []map[string]string{{"type": "text", "text": string(encoded)}}, "structuredContent": native, "isError": false,
		}})
		if err != nil || len(wire) > 8192 || page.Text == "" || !utf8.ValidString(page.Text) {
			t.Fatalf("native envelope exceeds byte budget: %d, %v", len(wire), err)
		}
		if strings.HasPrefix(text, "\x01") && (len(page.Text) >= 2048 || page.EOF) {
			t.Fatal("escaping did not shorten the page or expose continuation")
		}
	}
	// Недопустимо возвращать пустую non-EOF страницу, если metadata уже
	// исчерпала бюджет доставки.
	path := strings.Repeat("\x01", 1024)
	file := githubPageFixtureFile("Text", path)
	_, err := projectGitHubContentPage(file, githubCatalogInput{Path: path, Ref: githubPageFixtureCommit}, 2048)
	assertGitHubPageError(t, err, "INTEGRATION_RESPONSE_INVALID")
}

func assertGitHubPageError(t *testing.T, err error, code string) {
	t.Helper()
	var safe *SafeError
	if !errors.As(err, &safe) || safe.Code != code {
		t.Fatalf("safe error = %v, want %s", err, code)
	}
}
