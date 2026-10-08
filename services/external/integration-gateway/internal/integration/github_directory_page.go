package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/google/go-github/v74/github"
)

const maximumGitHubDirectoryEntries = 999
const maximumGitHubDirectoryPageSize = 50

type githubDirectoryPage struct {
	Items         string `json:"items"`
	Path          string `json:"path"`
	CommitSHA     string `json:"commit_sha"`
	CatalogDigest string `json:"catalog_digest"`
	Count         int    `json:"count"`
	TotalCount    int    `json:"total_count"`
	Cursor        int    `json:"cursor"`
	NextCursor    int    `json:"next_cursor,omitempty"`
	EOF           bool   `json:"eof"`
}

func executeGitHubDirectoryList(ctx context.Context, client *github.Client, owner, repo string, request Request, capability integrationpackage.Capability, in githubCatalogInput) (Result, error) {
	if in.Limit == 0 {
		in.Limit = 20
	}
	if !validGitHubIndexPath(in.Path, true) || !sourceCommitPattern.MatchString(in.Ref) ||
		in.Cursor < 0 || in.Cursor > maximumGitHubDirectoryEntries || in.Limit < 1 || in.Limit > maximumGitHubDirectoryPageSize ||
		in.ExpectedCatalogDigest != "" && !validGitHubDirectoryDigest(in.ExpectedCatalogDigest) || in.Cursor > 0 && in.ExpectedCatalogDigest == "" {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	var directory []*github.RepositoryContent
	file, err := githubRead(ctx, capability, func() (*github.RepositoryContent, *github.Response, error) {
		file, entries, response, err := client.Repositories.GetContents(ctx, owner, repo, in.Path, &github.RepositoryContentGetOptions{Ref: in.Ref})
		directory = entries
		return file, response, err
	})
	if err != nil {
		return Result{}, err
	}
	// При 1000 элементах Contents API не доказывает полноту. Отдельный Trees
	// путь не вводится скрыто; cap не превращается в успешный EOF.
	if file != nil || len(directory) > maximumGitHubDirectoryEntries {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	page, err := projectGitHubDirectoryPage(directory, owner, repo, in)
	if err != nil {
		return Result{}, err
	}
	return providerResult(request, "github-directory:"+page.CatalogDigest+":"+strconv.Itoa(page.Cursor), page)
}

func validGitHubDirectoryDigest(value string) bool {
	if len(value) != len("sha256:")+sha256.Size*2 || !strings.HasPrefix(value, "sha256:") || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}

func projectGitHubDirectoryPage(directory []*github.RepositoryContent, owner, repo string, in githubCatalogInput) (githubDirectoryPage, error) {
	items := make([]githubContentView, 0, len(directory))
	prefix := in.Path
	if prefix != "" {
		prefix += "/"
	}
	// Весь каталог проверяется до первой страницы, включая невыдаваемый хвост.
	for _, entry := range directory {
		if entry == nil || entry.Path == nil || entry.Type == nil || entry.SHA == nil || entry.Size == nil ||
			!validGitHubIndexPath(entry.GetPath(), false) || !strings.HasPrefix(entry.GetPath(), prefix) ||
			strings.TrimPrefix(entry.GetPath(), prefix) == "" || strings.Contains(strings.TrimPrefix(entry.GetPath(), prefix), "/") ||
			!sourceCommitPattern.MatchString(entry.GetSHA()) || entry.GetSize() < 0 || int64(entry.GetSize()) > maximumGitHubFileChangeCount {
			return githubDirectoryPage{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		switch entry.GetType() {
		case "file", "dir", "symlink", "submodule":
		default:
			return githubDirectoryPage{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		items = append(items, githubContentView{Path: entry.GetPath(), Type: entry.GetType(), SHA: entry.GetSHA(), Size: entry.GetSize()})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	for index := 1; index < len(items); index++ {
		if items[index-1].Path == items[index].Path {
			return githubDirectoryPage{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
	}
	canonical, err := json.Marshal(struct {
		Owner      string              `json:"owner"`
		Repository string              `json:"repository"`
		Path       string              `json:"path"`
		CommitSHA  string              `json:"commit_sha"`
		Items      []githubContentView `json:"items"`
	}{owner, repo, in.Path, in.Ref, items})
	if err != nil || len(canonical) > maximumGitHubProviderResponseBytes {
		return githubDirectoryPage{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	digest := sha256.Sum256(canonical)
	page := githubDirectoryPage{Path: in.Path, CommitSHA: in.Ref, CatalogDigest: "sha256:" + hex.EncodeToString(digest[:]), TotalCount: len(items), Cursor: in.Cursor}
	if in.Cursor > len(items) || in.ExpectedCatalogDigest != "" && in.ExpectedCatalogDigest != page.CatalogDigest {
		return githubDirectoryPage{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	// Уменьшается только число выдаваемых элементов. Offset непрерывен,
	// Оба native представления входят в прежний byte budget; receipt отдельно
	// хранится владельцем и не выдаётся внутри model result.
	for count := min(in.Limit, len(items)-in.Cursor); count >= 0; count-- {
		encoded, err := json.Marshal(items[in.Cursor : in.Cursor+count])
		if err != nil {
			return githubDirectoryPage{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		page.Items, page.Count = string(encoded), count
		page.EOF = in.Cursor+count == len(items)
		page.NextCursor = 0
		if !page.EOF {
			page.NextCursor = in.Cursor + count
		}
		if (count > 0 || page.EOF) && githubContentPageFitsEnvelope(page) {
			return page, nil
		}
	}
	return githubDirectoryPage{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
}
