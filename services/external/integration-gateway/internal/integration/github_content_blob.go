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
	// maximumGitHubInlineContentBytes selects the inline Contents range; larger encoding-none files require separately pinned blob retrieval.
	// RU: maximumGitHubInlineContentBytes задаёт inline Contents диапазон; большие encoding-none файлы требуют отдельно закреплённого чтения blob.
	maximumGitHubInlineContentBytes = 1 << 20
	// maximumGitHubBlobResponseBytes bounds the raw encoded blob response, not decoded source bytes or the returned model page.
	// RU: maximumGitHubBlobResponseBytes ограничивает сырой кодированный blob-ответ, а не декодированные байты источника и не страницу модели.
	maximumGitHubBlobResponseBytes = 12 << 20
)

// githubContentBlobIntent is created after validated Contents metadata to select an exact local raw-response budget, not to replace owner claim/grant/repository authority.
// RU: githubContentBlobIntent создаётся после проверки Contents metadata для выбора точного локального raw-response бюджета, не заменяя owner claim/grant/repository authority.
type githubContentBlobIntent struct {
	url        string
	owner      string
	repo       string
	sourceSize int
	sha        string
}

// githubContentBlobContextKey keeps the private budget intent separate from caller input and ordinary HTTP metadata.
// RU: githubContentBlobContextKey отделяет закрытый budget intent от входа вызывающего и обычных HTTP-метаданных.
type githubContentBlobContextKey struct{}

// validateGitHubContentMetadata requires the exact file path, regular file type, explicit bounded size, canonical blob SHA and supported encoding representation.
// RU: validateGitHubContentMetadata требует точный file path, обычный file type, явный ограниченный размер, канонический blob SHA и допустимое encoding-представление.
// Encoding none is permitted only above the inline threshold with explicitly empty content; a supplied expected SHA must match before blob retrieval.
// RU: Encoding none допускается только выше inline-порога с явно пустым content; заданный expected SHA обязан совпасть до загрузки blob.
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

// readGitHubContentSource decodes valid inline Contents or retrieves the exact same-origin Git blob with a private method/repository/SHA/size budget intent.
// RU: readGitHubContentSource декодирует корректный inline Contents либо читает точный same-origin Git blob с закрытым method/repository/SHA/size budget intent.
// Large blobs require matching metadata and strict bounded base64 decoding; UTF-8, NUL and full Git-hash checks remain in the page projection.
// RU: Большой blob требует совпавших метаданных и строгого ограниченного base64-разбора; UTF-8, NUL и полный Git hash проверяет проекция страницы.
// It never follows a download URL or substitutes another host when the selected source cannot be verified.
// RU: Функция не следует download URL и не подставляет другой хост, когда выбранный источник не удаётся проверить.
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

// githubProviderResponseLimit uses the ordinary SDK cap without an intent and grants the larger blob cap only to its exact validated HTTPS GET without query or fragment.
// RU: githubProviderResponseLimit использует обычный SDK cap без intent и допускает больший blob cap только для точного проверенного HTTPS GET без query и fragment.
// Invalid intent type, source size or request binding fails before transport rather than silently falling back to a wider response budget.
// RU: Неверные тип intent, размер источника либо привязка запроса отклоняются до transport вместо скрытого перехода к большему response-бюджету.
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
