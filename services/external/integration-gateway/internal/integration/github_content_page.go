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

// maximumGitHubContentPageBytes bounds returned UTF-8 text bytes per page independently of the complete source and serialized response budgets.
// RU: maximumGitHubContentPageBytes ограничивает байты UTF-8 текста страницы независимо от всего источника и сериализованного ответа.
const maximumGitHubContentPageBytes = 16 << 10

// maximumGitHubContentEnvelopeBytes caps the modeled native MCP response with both result representations and its final encoder newline.
// RU: maximumGitHubContentEnvelopeBytes ограничивает модель native MCP ответа с обоими представлениями результата и завершающим переводом строки encoder.
const maximumGitHubContentEnvelopeBytes = 64 << 10

// maximumGitHubContentSourceBytes bounds the complete source, not a page or raw JSON; large Contents results use the same API's pinned Git blob without download fallback.
// RU: maximumGitHubContentSourceBytes ограничивает весь источник, а не страницу либо сырой JSON; большие Contents ответы читают закреплённый Git blob того же API без download fallback.
const maximumGitHubContentSourceBytes = 8 << 20

// githubContentPage binds a contiguous text slice to exact path/blob/commit, full size and source/chunk digests, with explicit byte continuation and EOF.
// RU: githubContentPage связывает непрерывный текстовый фрагмент с точными path/blob/commit, полным размером и source/chunk хешами, явными byte continuation и EOF.
// Valid UTF-8 and matching provenance do not turn repository text into trusted model instructions.
// RU: Корректный UTF-8 и совпавшая provenance не превращают текст репозитория в доверенные инструкции модели.
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

// executeGitHubContentRead validates immutable file-page selectors, fetches Contents metadata rather than a directory, reads the bounded source and returns a receipt-bound page.
// RU: executeGitHubContentRead проверяет неизменяемые file-page параметры, получает Contents metadata вместо каталога, читает ограниченный источник и возвращает страницу с квитанцией.
// A large source is fetched only through the exact Git-blob path; projection errors return no partial text result.
// RU: Большой источник читается только через точный Git-blob путь; ошибка проекции не выдаёт частичный текстовый результат.
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
	content, err := readGitHubContentSource(ctx, client, owner, repo, capability, file, in)
	if err != nil {
		return Result{}, err
	}
	page, err := projectGitHubContentPageSource(file, content, in, maximum)
	if err != nil {
		return Result{}, err
	}
	return providerResult(request, "github-content:"+page.SHA, page)
}

// validateGitHubContentPageInput requires a bounded repository path, lowercase immutable commit and valid byte/page limits, defaulting the page to sixteen KiB.
// RU: validateGitHubContentPageInput требует ограниченный путь репозитория, строчный неизменяемый commit и корректные byte/page пределы с обычной страницей шестнадцать КиБ.
// Nonzero continuation offsets require a canonical expected blob SHA; metadata matching and UTF-8 boundary checks happen after the source read.
// RU: Ненулевое продолжение требует канонического expected blob SHA; совпадение metadata и граница UTF-8 проверяются после чтения источника.
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

// projectGitHubContentPage validates inline metadata and base64 encoding, decodes through the SDK and delegates full-source/page validation without fetching a blob.
// RU: projectGitHubContentPage проверяет inline metadata и base64, декодирует через SDK и передаёт проверку полного источника/страницы без загрузки blob.
func projectGitHubContentPage(file *github.RepositoryContent, in githubCatalogInput, maximum int) (githubContentPage, error) {
	if err := validateGitHubContentMetadata(file, in); err != nil {
		return githubContentPage{}, err
	}
	if file.GetEncoding() != "base64" {
		return githubContentPage{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	content, err := file.GetContent()
	if err != nil {
		return githubContentPage{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return projectGitHubContentPageSource(file, content, in, maximum)
}

// projectGitHubContentPageSource checks the entire source's size, UTF-8, absence of NUL and Git blob identity before slicing already validated page selectors.
// RU: projectGitHubContentPageSource проверяет размер всего источника, UTF-8, отсутствие NUL и Git blob identity до разбиения по заранее проверенным параметрам страницы.
// It rejects stale blob pins and invalid offsets, preserves rune boundaries and binary-searches a prefix fitting the modeled native envelope.
// RU: Функция отклоняет устаревшие blob-привязки и неверные offsets, сохраняет границы рун и двоичным поиском выбирает префикс под модель native-конверта.
// Source digest covers all bytes, chunk digest covers only returned text, and an empty non-EOF page is a closed failure rather than hidden truncation.
// RU: Source digest охватывает все байты, chunk digest — только выданный текст; пустая non-EOF страница является закрытым отказом, а не скрытым усечением.
func projectGitHubContentPageSource(file *github.RepositoryContent, content string, in githubCatalogInput, maximum int) (githubContentPage, error) {
	if err := validateGitHubContentMetadata(file, in); err != nil {
		return githubContentPage{}, err
	}
	// Проверяется весь источник, а не только выдаваемая страница.
	if len(content) != file.GetSize() || !utf8.ValidString(content) || strings.IndexByte(content, 0) >= 0 ||
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

// githubContentPageWithText replaces only page text and derived next-offset/EOF/chunk digest, preserving the full-source identity and byte size.
// RU: githubContentPageWithText заменяет только текст и производные next-offset/EOF/chunk digest, сохраняя идентичность и размер полного источника.
func githubContentPageWithText(page githubContentPage, text string) githubContentPage {
	page.Text = text
	page.NextOffsetBytes = page.OffsetBytes + int64(len(text))
	page.EOF = page.NextOffsetBytes == int64(page.Size)
	chunkDigest := sha256.Sum256([]byte(text))
	page.ChunkDigest = "sha256:" + hex.EncodeToString(chunkDigest[:])
	return page
}

// githubContentPageFitsEnvelope serializes both native result forms under a conservative 128-ASCII-character invocation/RPC-ID model, including the final newline.
// RU: githubContentPageFitsEnvelope сериализует оба native-представления в консервативной модели invocation/RPC ID из 128 ASCII-символов, включая завершающий перевод строки.
// It proves only that modeled envelope fits, not an unconditional wire bound for arbitrary caller-controlled JSON-RPC IDs.
// RU: Функция доказывает только предел модельного конверта, а не безусловный wire-предел произвольных JSON-RPC ID вызывающего.
func githubContentPageFitsEnvelope(page any) bool {
	summary, err := json.Marshal(page)
	if err != nil {
		return false
	}
	// InvocationRef в callback ограничен 128 ASCII байтами. JSON-RPC id такой
	// длины — консервативная модель, не transport bound произвольного caller id.
	// Модель включает оба представления результата и финальный LF Encoder.
	native := map[string]any{"ok": true, "invocationRef": strings.Repeat("i", 128), "result": string(summary)}
	encoded, err := json.Marshal(native)
	if err != nil {
		return false
	}
	wire, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": strings.Repeat("i", 128), "result": map[string]any{
		"content": []map[string]string{{"type": "text", "text": string(encoded)}}, "structuredContent": native, "isError": false,
	}})
	return err == nil && len(wire)+1 <= maximumGitHubContentEnvelopeBytes
}
