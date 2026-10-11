package integration

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/google/go-github/v74/github"
)

func githubBlobFixture(t *testing.T, content string) (*github.Client, *github.RepositoryContent, *github.Blob, *int) {
	t.Helper()
	file := githubPageFixtureFile(content, "libs/go/controlplaneapi/gen/controlplane/v1/control_plane.pb.go")
	file.Encoding, file.Content = github.Ptr("none"), github.Ptr("")
	blob := &github.Blob{SHA: file.SHA, Size: file.Size, Encoding: github.Ptr("base64"), Content: github.Ptr(base64.StdEncoding.EncodeToString([]byte(content)))}
	path, sha := file.GetPath(), file.GetSHA()
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.Context().Err() != nil {
			return nil, request.Context().Err()
		}
		if request.Method != http.MethodGet || request.URL.Host != "api.github.com" || request.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatal("repository read transport identity changed")
		}
		var value any
		switch request.URL.Path {
		case "/repos/acme/repo/contents/" + path:
			if request.URL.Query().Get("ref") != githubPageFixtureCommit {
				t.Fatal("contents commit changed")
			}
			value = file
		case "/repos/acme/repo/git/blobs/" + sha:
			if request.URL.RawQuery != "" {
				t.Fatal("blob query changed")
			}
			value = blob
		default:
			t.Fatal("unexpected provider endpoint")
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal("fixture encoding failed")
		}
		return &http.Response{Request: request, StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(encoded)))}, nil
	})
	client := github.NewClient(&http.Client{Transport: githubBoundedTransport{next: transport}, CheckRedirect: func(*http.Request, []*http.Request) error {
		return &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}}).WithAuthToken("test-token")
	return client, file, blob, &calls
}

func TestGitHubContentBlobRejectsUnboundMetadataBeforeBlobFetch(t *testing.T) {
	for _, scenario := range []string{"path", "ref", "expected-sha", "type", "encoding", "none-content", "oversize", "missing-size", "small-none"} {
		t.Run(scenario, func(t *testing.T) {
			client, file, _, calls := githubBlobFixture(t, strings.Repeat("x", (1<<20)+1))
			in := githubCatalogInput{Path: file.GetPath(), Ref: githubPageFixtureCommit, ExpectedSHA: file.GetSHA()}
			code := "INTEGRATION_RESPONSE_INVALID"
			wantCalls := 1
			switch scenario {
			case "path":
				file.Path = github.Ptr("other.go")
			case "ref":
				in.Ref, code, wantCalls = "main", "INTEGRATION_REQUEST_REJECTED", 0
			case "expected-sha":
				in.ExpectedSHA, code = strings.Repeat("b", 40), "INTEGRATION_REQUEST_REJECTED"
			case "type":
				file.Type = github.Ptr("symlink")
			case "encoding":
				file.Encoding = github.Ptr("unknown")
			case "none-content":
				file.Content = github.Ptr("PRIVATE_SENTINEL")
			case "oversize":
				file.Size = github.Ptr(maximumGitHubContentSourceBytes + 1)
			case "missing-size":
				file.Size = nil
			case "small-none":
				file.Size = github.Ptr(maximumGitHubInlineContentBytes)
			}
			result, err := executeGitHubContentRead(t.Context(), client, "acme", "repo", Request{}, integrationpackage.Capability{}, in)
			assertGitHubPageError(t, err, code)
			if *calls != wantCalls || result.Summary != "" {
				t.Fatal("unbound metadata fetched a blob or returned partial content")
			}
		})
	}
}

func TestGitHubContentBlobRejectsInvalidFullSource(t *testing.T) {
	for _, scenario := range []string{"sha", "size", "missing-size", "encoding", "missing-content", "base64", "nul", "utf8", "content-hash", "overflow"} {
		t.Run(scenario, func(t *testing.T) {
			content := strings.Repeat("x", (1<<20)+1)
			client, file, blob, calls := githubBlobFixture(t, content)
			in := githubCatalogInput{Path: file.GetPath(), Ref: githubPageFixtureCommit, ExpectedSHA: file.GetSHA()}
			switch scenario {
			case "sha":
				blob.SHA = github.Ptr(strings.Repeat("b", 40))
			case "size":
				blob.Size = github.Ptr(file.GetSize() + 1)
			case "missing-size":
				blob.Size = nil
			case "encoding":
				blob.Encoding = github.Ptr("none")
			case "missing-content":
				blob.Content = nil
			case "base64":
				blob.Content = github.Ptr("PRIVATE_SENTINEL!")
			case "nul", "utf8", "content-hash":
				prefix := map[string]string{"nul": "\x00", "utf8": "\xff", "content-hash": "y"}[scenario]
				blob.Content = github.Ptr(base64.StdEncoding.EncodeToString([]byte(prefix + content[1:])))
			case "overflow":
				blob.Content = github.Ptr(strings.Repeat("A", maximumGitHubBlobResponseBytes))
			}
			result, err := executeGitHubContentRead(t.Context(), client, "acme", "repo", Request{}, integrationpackage.Capability{}, in)
			assertGitHubPageError(t, err, "INTEGRATION_RESPONSE_INVALID")
			if *calls != 2 || result.Summary != "" {
				t.Fatal("invalid full blob source returned a partial page or repeated validation refusal")
			}
		})
	}
}

func TestGitHubContentBlobBoundaryPinsUTF8AndEOF(t *testing.T) {
	content := strings.Repeat("Я", maximumGitHubContentSourceBytes/2)
	client, file, _, calls := githubBlobFixture(t, content)
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "test-token")
	adapter.githubHTTPClient = client.Client()
	sourceDigest := sha256.Sum256([]byte(content))
	for _, offset := range []int64{0, int64(len(content) - (16 << 10)), int64(len(content))} {
		page := githubPageFixtureRead(t, adapter, credential, map[string]any{"path": file.GetPath(), "ref": githubPageFixtureCommit,
			"expected_sha": file.GetSHA(), "offset_bytes": offset})
		if page.Size != len(content) || page.OffsetBytes != offset ||
			page.NextOffsetBytes != min(offset+(16<<10), int64(len(content))) || page.EOF != (page.NextOffsetBytes == int64(len(content))) ||
			page.Text != content[offset:page.NextOffsetBytes] || !githubContentPageFitsEnvelope(page) || page.SHA != file.GetSHA() ||
			page.SourceDigest != "sha256:"+hex.EncodeToString(sourceDigest[:]) {
			t.Fatal("eight MiB source lost exact UTF8 pages or EOF")
		}
	}
	if *calls != 6 {
		t.Fatal("source read changed provider call cardinality")
	}
}

func TestGitHubContentBlobCancellationAndRedirectNeverReturnPage(t *testing.T) {
	for _, scenario := range []string{"cancel", "redirect"} {
		t.Run(scenario, func(t *testing.T) {
			client, file, _, calls := githubBlobFixture(t, strings.Repeat("x", (1<<20)+1))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			httpClient := client.Client()
			original := httpClient.Transport
			httpClient.Transport = githubBoundedTransport{next: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if strings.Contains(request.URL.Path, "/git/blobs/") {
					*calls++
					if scenario == "cancel" {
						cancel()
						return nil, ctx.Err()
					}
					return &http.Response{Request: request, StatusCode: http.StatusFound, Header: http.Header{"Location": {"https://foreign.invalid/PRIVATE_SENTINEL"}}, Body: io.NopCloser(strings.NewReader("PRIVATE_SENTINEL"))}, nil
				}
				return original.RoundTrip(request)
			})}
			client = github.NewClient(httpClient)
			in := githubCatalogInput{Path: file.GetPath(), Ref: githubPageFixtureCommit, ExpectedSHA: file.GetSHA()}
			result, err := executeGitHubContentRead(ctx, client, "acme", "repo", Request{}, integrationpackage.Capability{}, in)
			var safe *SafeError
			if !errors.As(err, &safe) || *calls != 2 || result.Summary != "" {
				t.Fatal("cancelled or redirected blob returned a page or followed another host")
			}
		})
	}
}

func TestGitHubContentBlobReadsActualGeneratedSourceWithinBoundedEnvelope(t *testing.T) {
	content := strings.Repeat("x", 3590649)
	client, file, _, calls := githubBlobFixture(t, content)
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "test-token")
	adapter.githubHTTPClient = client.Client()
	page := githubPageFixtureRead(t, adapter, credential, map[string]any{"path": file.GetPath(), "ref": githubPageFixtureCommit, "expected_sha": file.GetSHA()})
	if page.Size != len(content) || page.NextOffsetBytes != 16<<10 || page.EOF || *calls != 2 {
		t.Fatal("valid pinned generated source was not read through Contents and Git blob")
	}
	if !githubContentPageFitsEnvelope(page) || page.Text != content[:16<<10] || page.SHA != file.GetSHA() || page.CommitSHA != githubPageFixtureCommit {
		t.Fatal("generated source lost page integrity or envelope bound")
	}
}

func TestGitHubContentBlobBudgetIsExactPrivateAndClosesResponses(t *testing.T) {
	for _, scenario := range []string{"expanded", "ordinary", "error", "overflow", "foreign-repo", "foreign-sha", "method", "query", "host", "scheme", "unknown-intent", "oversize-intent", "small-intent"} {
		t.Run(scenario, func(t *testing.T) {
			sha := strings.Repeat("a", 40)
			endpoint := githubAPIBaseURL + "repos/acme/repo/git/blobs/" + sha
			intent := &githubContentBlobIntent{url: endpoint, owner: "acme", repo: "repo", sha: sha, sourceSize: maximumGitHubInlineContentBytes + 1}
			ctx := context.WithValue(t.Context(), githubContentBlobContextKey{}, intent)
			method, size, status, wantCalls := http.MethodGet, maximumGitHubProviderResponseBytes+1, http.StatusOK, 0
			switch scenario {
			case "expanded":
				wantCalls = 1
			case "ordinary":
				ctx, wantCalls = t.Context(), 1
			case "error":
				size, status, wantCalls = maximumResponseBytes+1, http.StatusServiceUnavailable, 1
			case "overflow":
				size, wantCalls = maximumGitHubBlobResponseBytes+1, 1
			case "foreign-repo":
				endpoint = strings.Replace(endpoint, "/repo/", "/other/", 1)
			case "foreign-sha":
				endpoint = strings.Replace(endpoint, sha, strings.Repeat("b", 40), 1)
			case "method":
				method = http.MethodPost
			case "query":
				endpoint += "?private=PRIVATE_SENTINEL"
			case "host":
				endpoint = strings.Replace(endpoint, "api.github.com", "foreign.invalid", 1)
			case "scheme":
				endpoint = strings.Replace(endpoint, "https:", "http:", 1)
			case "unknown-intent":
				ctx = context.WithValue(t.Context(), githubContentBlobContextKey{}, true)
			case "oversize-intent":
				intent.sourceSize = maximumGitHubContentSourceBytes + 1
			case "small-intent":
				intent.sourceSize = maximumGitHubInlineContentBytes
			}
			request, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
			if err != nil {
				t.Fatal("fixture request failed")
			}
			calls := 0
			var body *githubTrackedResponseBody
			transport := githubBoundedTransport{next: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				body = &githubTrackedResponseBody{Reader: strings.NewReader(strings.Repeat("x", size))}
				return &http.Response{Request: request, StatusCode: status, Header: http.Header{}, Body: body}, nil
			})}
			response, err := transport.RoundTrip(request)
			if scenario == "expanded" {
				if err != nil {
					t.Fatal("sealed blob response did not receive its bounded budget")
				}
				decoded, readErr := io.ReadAll(response.Body)
				_ = response.Body.Close()
				if readErr != nil || len(decoded) != size {
					t.Fatal("bounded response lost bytes")
				}
			} else {
				assertGitHubPageError(t, err, "INTEGRATION_RESPONSE_INVALID")
			}
			if calls != wantCalls || body != nil && !body.closed {
				t.Fatal("unbound request escaped transport or source body was not closed")
			}
		})
	}
}
