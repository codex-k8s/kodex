package integration

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// confluenceServerFooterComment carries the comment identity and its page container, version and storage text.
// confluenceServerFooterComment хранит идентификацию комментария, страницу, версию и содержимое storage.
type confluenceServerFooterComment struct {
	ID, Type, Status string
	Container        struct{ ID, Type string }
	Extensions       struct{ Location string }
	Version          struct{ Number int64 }
	Body             struct {
		Storage struct{ Value, Representation string }
	}
}

// validForPage accepts a current footer comment with the exact verified page as its container.
// validForPage принимает актуальный комментарий под страницей с точно сверенным контейнером.
func (comment confluenceServerFooterComment) validForPage(pageID string) bool {
	return decimalID(comment.ID) && comment.ID != pageID && comment.Type == "comment" && comment.Status == "current" &&
		comment.Container.ID == pageID && comment.Container.Type == "page" &&
		(comment.Extensions.Location == "" || comment.Extensions.Location == "footer") &&
		comment.Version.Number > 0 && comment.Version.Number <= 2147483647 && comment.Body.Storage.Representation == "storage"
}

// executeConfluenceServerComment resolves the current parent page in the configured space before listing or adding footer comments.
// executeConfluenceServerComment разрешает текущую страницу в заданном пространстве перед чтением либо добавлением комментариев под ней.
func (adapter *Adapter) executeConfluenceServerComment(ctx context.Context, request Request, capability integrationpackage.Capability,
	configuration map[string]string, input []byte) (Result, error) {
	var value struct {
		PageID       string `json:"page_id"`
		Body, Cursor string
		Limit        int
	}
	if decodeProviderJSON(input, &value) != nil || !decimalID(value.PageID) {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	if _, err := adapter.readConfluenceServerPage(ctx, request, capability, configuration, value.PageID, false); err != nil {
		return Result{}, err
	}
	if request.Operation == "confluence.page.comment.create" {
		return adapter.createConfluenceServerComment(ctx, request, capability, configuration, value.PageID, value.Body)
	}
	limit := value.Limit
	if limit == 0 {
		limit = 10
	}
	offset, err := atlassianServerOffset(value.Cursor)
	if err != nil || limit < 1 || limit > 20 {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	path := "/content/" + value.PageID + "/child/comment"
	query := url.Values{"expand": {"container,version,body.storage"}, "location": {"footer"},
		"start": {strconv.Itoa(offset)}, "limit": {strconv.Itoa(limit)}}
	response, err := adapter.confluenceServerJSON(ctx, request, capability, configuration, http.MethodGet, path, query, nil, "")
	if err != nil {
		return Result{}, err
	}
	var page struct {
		Start, Limit, Size *int
		Results            []confluenceServerFooterComment
		Links              struct{ Next string } `json:"_links"`
	}
	if decodeProviderJSON(response, &page) != nil || page.Start == nil || page.Limit == nil || page.Size == nil ||
		*page.Start != offset || *page.Limit < 1 || *page.Limit > limit || *page.Size != len(page.Results) || len(page.Results) > *page.Limit {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	comments := make([]map[string]any, 0, len(page.Results))
	seen := make(map[string]bool, len(page.Results))
	for _, item := range page.Results {
		if !item.validForPage(value.PageID) || seen[item.ID] {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		seen[item.ID] = true
		comments = append(comments, map[string]any{"id": item.ID, "version": item.Version.Number, "body": item.Body.Storage.Value})
	}
	next, err := confluenceServerNextOffset(configuration, path, query, offset, *page.Limit, len(comments), page.Links.Next)
	if err != nil {
		return Result{}, err
	}
	encoded, err := json.Marshal(comments)
	if err != nil || len(encoded) > 65536 {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	output := map[string]any{"count": len(comments), "comments": string(encoded)}
	if next != "" {
		output["next_cursor"] = next
	}
	if _, err := adapter.readConfluenceServerPage(ctx, request, capability, configuration, value.PageID, false); err != nil {
		return Result{}, err
	}
	return providerResult(request, "confluence-comments:"+value.PageID, output)
}

// createConfluenceServerComment submits one escaped plain-text footer comment. The provider enforces the caller's write permission.
// createConfluenceServerComment отправляет один экранированный текстовый комментарий. Право вызывающего на запись проверяет Confluence.
func (adapter *Adapter) createConfluenceServerComment(ctx context.Context, request Request, capability integrationpackage.Capability,
	configuration map[string]string, pageID, body string) (Result, error) {
	if strings.TrimSpace(body) == "" || len(body) > 8192 {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	text := strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\r", "\n")
	expected := "<p>" + strings.ReplaceAll(html.EscapeString(text), "\n", "<br />") + "</p>"
	marker := confluenceServerMarker(request)
	storage := expected + "\n" + marker
	payload := map[string]any{"type": "comment", "container": map[string]string{"id": pageID, "type": "page"},
		"body": map[string]any{"storage": map[string]string{"representation": "storage", "value": storage}}}
	response, err := adapter.confluenceServerJSON(ctx, request, capability, configuration, http.MethodPost, "/content",
		url.Values{"expand": {"container,version,body.storage"}}, payload, request.EffectKey)
	if err != nil {
		return Result{}, err
	}
	var comment confluenceServerFooterComment
	if decodeProviderJSON(response, &comment) != nil || !decimalID(comment.ID) || comment.ID == pageID || comment.Type != "comment" {
		return Result{}, &UnknownOutcomeError{}
	}
	if !comment.matchesCreate(pageID, expected, marker) {
		id := comment.ID
		// Server/DC may omit expansions in the acknowledgment. Verify its exact object once, without replaying the write.
		// Server/DC может опустить expanded поля в ответе. Сверяем точный объект отдельным чтением без повтора записи.
		observed, err := adapter.confluenceServerJSON(ctx, request, capability, configuration, http.MethodGet, "/content/"+id,
			url.Values{"expand": {"container,version,body.storage"}}, nil, "")
		comment = confluenceServerFooterComment{}
		if err != nil || decodeProviderJSON(observed, &comment) != nil || comment.ID != id || !comment.matchesCreate(pageID, expected, marker) {
			return Result{}, &UnknownOutcomeError{}
		}
	}
	return providerResult(request, "confluence-comment:"+comment.ID, map[string]any{"id": comment.ID, "page_id": pageID, "version": comment.Version.Number})
}

// matchesCreate verifies the acknowledged comment's parent, first version and exact body. Confluence may strip the HTML marker.
// matchesCreate сверяет страницу, первую версию и точный текст комментария из ответа Confluence. Он может удалить HTML-маркер.
func (comment confluenceServerFooterComment) matchesCreate(pageID, expected, marker string) bool {
	storage := strings.TrimSpace(comment.Body.Storage.Value)
	body := strings.TrimSpace(strings.TrimSuffix(storage, marker))
	return comment.validForPage(pageID) && comment.Version.Number == 1 && body == expected
}
