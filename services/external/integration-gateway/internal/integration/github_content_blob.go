package integration

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/google/go-github/v74/github"
)

const (
	maximumGitHubInlineContentBytes = 1 << 20
	maximumGitHubBlobResponseBytes  = 12 << 20
)

// Этот private intent создаётся только после проверенного Contents read.
// Он выбирает локальный byte budget, не заменяя claim/grant/repository authority.
type githubContentBlobIntent struct {
	url        string
	owner      string
	repo       string
	sourceSize int
	sha        string
}

type githubContentBlobContextKey struct{}

func validateGitHubContentMetadata(file *github.RepositoryContent, in githubCatalogInput) error {
	if file == nil || file.GetPath() != in.Path || file.GetType() != "file" || file.Size == nil ||
		file.GetSize() < 0 || file.GetSize() > maximumGitHubContentSourceBytes || !sourceCommitPattern.MatchString(file.GetSHA()) {
		return &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	switch file.GetEncoding() {
	case "base64":
		if file.Content == nil {
			return &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
	case "none":
		if file.GetSize() <= maximumGitHubInlineContentBytes || file.Content == nil || *file.Content != "" {
			return &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
	default:
		return &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	if in.ExpectedSHA != "" && in.ExpectedSHA != file.GetSHA() {
		return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	return nil
}

func readGitHubContentSource(ctx context.Context, client *github.Client, owner, repo string, capability integrationpackage.Capability, file *github.RepositoryContent, in githubCatalogInput) (string, error) {
	if err := validateGitHubContentMetadata(file, in); err != nil {
		return "", err
	}
	if file.GetEncoding() == "base64" {
		content, err := file.GetContent()
		if err != nil {
			return "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return content, nil
	}
	if client == nil || client.BaseURL == nil || client.BaseURL.String() != githubAPIBaseURL {
		return "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	endpoint := client.BaseURL.ResolveReference(&url.URL{Path: "repos/" + owner + "/" + repo + "/git/blobs/" + file.GetSHA()})
	intent := &githubContentBlobIntent{url: endpoint.String(), owner: owner, repo: repo, sourceSize: file.GetSize(), sha: file.GetSHA()}
	ctx = context.WithValue(ctx, githubContentBlobContextKey{}, intent)
	blob, err := githubRead(ctx, capability, func() (*github.Blob, *github.Response, error) {
		return client.Git.GetBlob(ctx, owner, repo, file.GetSHA())
	})
	if err != nil {
		return "", err
	}
	if blob == nil || blob.Content == nil || blob.Size == nil || blob.GetEncoding() != "base64" ||
		blob.GetSHA() != file.GetSHA() || blob.GetSize() != file.GetSize() {
		return "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	decoded, err := io.ReadAll(io.LimitReader(base64.NewDecoder(base64.StdEncoding.Strict(), strings.NewReader(blob.GetContent())), int64(maximumGitHubContentSourceBytes)+1))
	if err != nil || len(decoded) != file.GetSize() {
		return "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return string(decoded), nil
}

func githubProviderResponseLimit(request *http.Request) (int, bool) {
	value := request.Context().Value(githubContentBlobContextKey{})
	if value == nil {
		return maximumGitHubProviderResponseBytes, true
	}
	intent, ok := value.(*githubContentBlobIntent)
	if !ok || intent == nil || intent.sourceSize <= maximumGitHubInlineContentBytes || intent.sourceSize > maximumGitHubContentSourceBytes ||
		!sourceCommitPattern.MatchString(intent.sha) || request.Method != http.MethodGet || request.URL.String() != intent.url ||
		intent.owner == "" || intent.repo == "" || intent.url != githubAPIBaseURL+"repos/"+url.PathEscape(intent.owner)+"/"+url.PathEscape(intent.repo)+"/git/blobs/"+intent.sha ||
		request.URL.Scheme != "https" || request.URL.Host != "api.github.com" || request.URL.User != nil ||
		request.URL.RawQuery != "" || request.URL.Fragment != "" {
		return 0, false
	}
	return maximumGitHubBlobResponseBytes, true
}
