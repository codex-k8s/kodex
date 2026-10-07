package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/google/go-github/v74/github"
)

const maximumGitHubContentPageBytes = 2048

const maximumGitHubContentEnvelopeBytes = 8192

// Полный UTF-8 источник имеет отдельный бюджет: страница и provider JSON
// не определяют его размер. Contents API используется без download fallback.
const maximumGitHubContentSourceBytes = 1 << 20

type githubContentPage struct {
	Path            string `json:"path"`
	Type            string `json:"type"`
	SHA             string `json:"sha"`
	Size            int    `json:"size"`
	Text            string `json:"text"`
	CommitSHA       string `json:"commit_sha"`
	OffsetBytes     int64  `json:"offset_bytes"`
	NextOffsetBytes int64  `json:"next_offset_bytes"`
	EOF             bool   `json:"eof"`
	SourceDigest    string `json:"source_digest"`
	ChunkDigest     string `json:"chunk_digest"`
}

func executeGitHubContentRead(ctx context.Context, client *github.Client, owner, repo string, request Request, capability integrationpackage.Capability, in githubCatalogInput) (Result, error) {
	maximum, err := validateGitHubContentPageInput(in)
	if err != nil {
		return Result{}, err
	}
	file, err := githubRead(ctx, capability, func() (*github.RepositoryContent, *github.Response, error) {
		file, directory, response, err := client.Repositories.GetContents(ctx, owner, repo, in.Path, &github.RepositoryContentGetOptions{Ref: in.Ref})
		if err == nil && len(directory) != 0 {
			return nil, response, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return file, response, err
	})
	if err != nil {
		return Result{}, err
	}
	page, err := projectGitHubContentPage(file, in, maximum)
	if err != nil {
		return Result{}, err
	}
	return providerResult(request, "github-content:"+page.SHA, page)
}

func validateGitHubContentPageInput(in githubCatalogInput) (int, error) {
	maximum := maximumGitHubContentPageBytes
	if in.MaximumBytes != nil {
		maximum = *in.MaximumBytes
	}
	if len(in.Path) > 1024 || !validRepositoryPath(in.Path, false) || !sourceCommitPattern.MatchString(in.Ref) ||
		in.OffsetBytes < 0 || in.OffsetBytes > maximumGitHubContentSourceBytes || maximum < utf8.UTFMax || maximum > maximumGitHubContentPageBytes ||
		in.ExpectedSHA != "" && !sourceCommitPattern.MatchString(in.ExpectedSHA) || in.OffsetBytes > 0 && in.ExpectedSHA == "" {
		return 0, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	return maximum, nil
}

func projectGitHubContentPage(file *github.RepositoryContent, in githubCatalogInput, maximum int) (githubContentPage, error) {
	if file == nil || file.GetPath() != in.Path || file.GetType() != "file" || file.GetEncoding() != "base64" ||
		file.GetSize() < 0 || file.GetSize() > maximumGitHubContentSourceBytes || !sourceCommitPattern.MatchString(file.GetSHA()) {
		return githubContentPage{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	content, err := file.GetContent()
	// Проверяется весь источник, а не только выдаваемая страница.
	if err != nil || len(content) != file.GetSize() || !utf8.ValidString(content) || strings.IndexByte(content, 0) >= 0 ||
		!matchesGitBlobSHA([]byte(content), file.GetSHA()) {
		return githubContentPage{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	if in.ExpectedSHA != "" && in.ExpectedSHA != file.GetSHA() || in.OffsetBytes > int64(len(content)) ||
		in.OffsetBytes < int64(len(content)) && !utf8.RuneStart(content[in.OffsetBytes]) {
		return githubContentPage{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	end := min(in.OffsetBytes+int64(maximum), int64(len(content)))
	for end < int64(len(content)) && !utf8.RuneStart(content[end]) {
		end--
	}
	text := content[in.OffsetBytes:end]
	sourceDigest := sha256.Sum256([]byte(content))
	page := githubContentPage{
		Path: file.GetPath(), Type: file.GetType(), SHA: file.GetSHA(), Size: len(content), Text: text,
		CommitSHA: in.Ref, OffsetBytes: in.OffsetBytes, NextOffsetBytes: end, EOF: end == int64(len(content)),
		SourceDigest: "sha256:" + hex.EncodeToString(sourceDigest[:]),
	}
	// Лимит включает вложенный JSON и оба native MCP представления. Сокращение
	// по рунам сохраняет непрерывность offset и никогда не скрывает EOF.
	runes := []rune(text)
	lower, upper := 0, len(runes)
	for lower < upper {
		middle := (lower + upper + 1) / 2
		candidate := githubContentPageWithText(page, string(runes[:middle]))
		if githubContentPageFitsEnvelope(candidate) {
			lower = middle
		} else {
			upper = middle - 1
		}
	}
	page = githubContentPageWithText(page, string(runes[:lower]))
	if !githubContentPageFitsEnvelope(page) || page.Text == "" && !page.EOF {
		return githubContentPage{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return page, nil
}

func githubContentPageWithText(page githubContentPage, text string) githubContentPage {
	page.Text = text
	page.NextOffsetBytes = page.OffsetBytes + int64(len(text))
	page.EOF = page.NextOffsetBytes == int64(page.Size)
	chunkDigest := sha256.Sum256([]byte(text))
	page.ChunkDigest = "sha256:" + hex.EncodeToString(chunkDigest[:])
	return page
}

func githubContentPageFitsEnvelope(page githubContentPage) bool {
	summary, err := json.Marshal(page)
	if err != nil {
		return false
	}
	// InvocationRef в callback ограничен 128 байтами. Дополнительный JSON-RPC
	// envelope с id той же длины оставляет запас поверх фактического tool result.
	native := map[string]any{"ok": true, "invocationRef": strings.Repeat("i", 128), "result": string(summary)}
	encoded, err := json.Marshal(native)
	if err != nil {
		return false
	}
	wire, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": strings.Repeat("i", 128), "result": map[string]any{
		"content": []map[string]string{{"type": "text", "text": string(encoded)}}, "structuredContent": native, "isError": false,
	}})
	return err == nil && len(wire) <= maximumGitHubContentEnvelopeBytes
}
