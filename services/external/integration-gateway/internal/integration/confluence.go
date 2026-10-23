package integration

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// confluencePage decodes the page identity, space, version and storage body needed for scoped reads and versioned updates.
// Callers must verify the returned space/page identity before exposing body data or issuing follow-up writes.
// confluencePage декодирует идентичность страницы, пространство, версию и storage body для ограниченного чтения и версионированного обновления.
// Вызывающий обязан проверить возвращённые пространство и страницу до выдачи тела или последующих записей.
type confluencePage struct {
	ID, Title, Status string
	SpaceID           string `json:"spaceId"`
	Version           struct {
		Number int64 `json:"number"`
	} `json:"version"`
	Body struct {
		Storage struct {
			Value string `json:"value"`
		} `json:"storage"`
	} `json:"body"`
}

// testConfluence validates the pinned definition and probes the configured space with its registered health operation.
// It returns the provider failure unchanged and creates no page, attachment or authorization grant.
// testConfluence проверяет закреплённое определение и обращается к настроенному пространству зарегистрированной health-операцией.
// Ошибка провайдера возвращается без изменения; страницы, вложения и grants полномочий не создаются.
func (adapter *Adapter) testConfluence(ctx context.Context, request Request, configuration map[string]string) error {
	definition, err := adapter.validateDefinition(request)
	if err != nil {
		return err
	}
	capability, _ := definition.Capability(definition.Spec.HealthCheck.Operation)
	_, err = adapter.configuredProviderJSON(ctx, request, capability, configuration, http.MethodGet,
		"/wiki/api/v2/spaces/"+url.PathEscape(configuration["space_id"]), nil, nil, "")
	return err
}

// executeConfluence dispatches typed space/page operations and attachment upload, delegating the remaining closed catalog separately.
// Search validates returned space IDs and extracts only the next cursor, never following a provider-supplied next URL as a network target.
// executeConfluence направляет типизированные операции пространства и страниц и загрузку вложений, отдельно передавая остальной закрытый каталог.
// Поиск проверяет возвращённые space IDs и извлекает только курсор, не используя next URL провайдера как сетевое назначение.
func (adapter *Adapter) executeConfluence(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, canonicalInput []byte) (Result, error) {
	switch request.Operation {
	case "confluence.attachment.upload":
		return adapter.uploadConfluenceAttachment(ctx, request, capability, configuration, canonicalInput)
	case "confluence.space.read":
		body, err := adapter.configuredProviderJSON(ctx, request, capability, configuration, http.MethodGet,
			"/wiki/api/v2/spaces/"+url.PathEscape(configuration["space_id"]), nil, nil, "")
		if err != nil {
			return Result{}, err
		}
		var provider struct{ ID, Key, Name string }
		if decodeProviderJSON(body, &provider) != nil || provider.ID != configuration["space_id"] {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return providerResult(request, "confluence-space:"+provider.ID, map[string]any{"id": provider.ID, "key": provider.Key, "name": provider.Name})
	case "confluence.page.search":
		var input struct {
			Title  string `json:"title"`
			Cursor string `json:"cursor"`
			Limit  int64  `json:"limit"`
		}
		if decodeProviderJSON(canonicalInput, &input) != nil {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		if input.Limit == 0 {
			input.Limit = 20
		}
		query := url.Values{"space-id": {configuration["space_id"]}, "title": {input.Title}, "limit": {strconv.FormatInt(input.Limit, 10)}}
		if input.Cursor != "" {
			query.Set("cursor", input.Cursor)
		}
		body, err := adapter.configuredProviderJSON(ctx, request, capability, configuration, http.MethodGet, "/wiki/api/v2/pages", query, nil, "")
		if err != nil {
			return Result{}, err
		}
		var provider struct {
			Results []confluencePage `json:"results"`
			Links   struct {
				Next string `json:"next"`
			} `json:"_links"`
		}
		if decodeProviderJSON(body, &provider) != nil || int64(len(provider.Results)) > input.Limit {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		pages := make([]map[string]any, 0, len(provider.Results))
		for _, page := range provider.Results {
			if page.ID == "" || page.SpaceID != configuration["space_id"] {
				return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
			}
			pages = append(pages, map[string]any{"id": page.ID, "title": page.Title, "status": page.Status})
		}
		encoded, _ := json.Marshal(pages)
		output := map[string]any{"count": len(pages), "pages": string(encoded)}
		if provider.Links.Next != "" {
			next, err := url.Parse(provider.Links.Next)
			if err != nil || next.Query().Get("cursor") == "" {
				return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
			}
			output["next_cursor"] = next.Query().Get("cursor")
		}
		return providerResult(request, "confluence-search:"+request.EffectKey, output)
	case "confluence.page.read":
		var input struct {
			PageID string `json:"page_id"`
		}
		if decodeProviderJSON(canonicalInput, &input) != nil {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		provider, err := adapter.readConfluencePage(ctx, request, capability, configuration, input.PageID)
		if err != nil {
			return Result{}, err
		}
		return confluencePageResult(request, provider, true)
	case "confluence.page.create":
		return adapter.createConfluencePage(ctx, request, capability, configuration, canonicalInput)
	case "confluence.page.update":
		return adapter.updateConfluencePage(ctx, request, capability, configuration, canonicalInput)
	default:
		return adapter.executeConfluenceCatalog(ctx, request, capability, configuration, canonicalInput)
	}
}

// createConfluencePage posts a draft with an effect marker in the configured space, first checking any requested parent through scoped read.
// It validates the returned space and page projection without treating the marker as native idempotency or owner approval.
// createConfluencePage создаёт черновик с меткой эффекта в настроенном пространстве, предварительно проверяя заданного родителя ограниченным чтением.
// Проверяются пространство ответа и проекция страницы; метка не считается нативной идемпотентностью или одобрением владельца.
func (adapter *Adapter) createConfluencePage(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, canonicalInput []byte) (Result, error) {
	var input struct {
		Title    string `json:"title"`
		Body     string `json:"body"`
		ParentID string `json:"parent_id"`
	}
	if decodeProviderJSON(canonicalInput, &input) != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	marker := "<!-- kodex-effect:" + request.EffectKey + " -->"
	payload := map[string]any{
		"spaceId": configuration["space_id"], "status": "draft", "title": input.Title,
		"body": map[string]any{"representation": "storage", "value": input.Body + "\n" + marker},
	}
	if input.ParentID != "" {
		if _, err := adapter.readConfluencePage(ctx, request, capability, configuration, input.ParentID); err != nil {
			return Result{}, err
		}
		payload["parentId"] = input.ParentID
	}
	body, err := adapter.configuredProviderJSON(ctx, request, capability, configuration, http.MethodPost, "/wiki/api/v2/pages", nil, payload, request.EffectKey)
	if err != nil {
		return Result{}, err
	}
	var provider confluencePage
	if decodeProviderJSON(body, &provider) != nil || provider.SpaceID != configuration["space_id"] {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return confluencePageResult(request, provider, false)
}

// updateConfluencePage reads the exact scoped page, checks expected version and preserves omitted title/body before sending version+1.
// The returned page, space and version must match; authorization and mutation-outcome handling remain outside this helper.
// updateConfluencePage читает точную страницу пространства, проверяет ожидаемую версию и сохраняет пропущенные title и body перед отправкой версии +1.
// Возвращённые страница, пространство и версия обязаны совпасть; полномочия и обработка исхода изменения остаются вне помощника.
func (adapter *Adapter) updateConfluencePage(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, canonicalInput []byte) (Result, error) {
	var input struct {
		PageID          string `json:"page_id"`
		ExpectedVersion int64  `json:"expected_version"`
		Title, Body     string
	}
	if decodeProviderJSON(canonicalInput, &input) != nil || (input.Title == "" && input.Body == "") {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	current, err := adapter.readConfluencePage(ctx, request, capability, configuration, input.PageID)
	if err != nil {
		return Result{}, err
	}
	if current.Version.Number != input.ExpectedVersion {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	if input.Title == "" {
		input.Title = current.Title
	}
	if input.Body == "" {
		input.Body = current.Body.Storage.Value
	}
	payload := map[string]any{
		"id": input.PageID, "status": current.Status, "title": input.Title,
		"body":    map[string]any{"representation": "storage", "value": input.Body},
		"version": map[string]any{"number": input.ExpectedVersion + 1, "message": "Kodex effect " + request.EffectKey},
	}
	body, err := adapter.configuredProviderJSON(ctx, request, capability, configuration, http.MethodPut,
		"/wiki/api/v2/pages/"+url.PathEscape(input.PageID), nil, payload, request.EffectKey)
	if err != nil {
		return Result{}, err
	}
	var provider confluencePage
	if decodeProviderJSON(body, &provider) != nil || provider.ID != input.PageID || provider.SpaceID != configuration["space_id"] || provider.Version.Number != input.ExpectedVersion+1 {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return confluencePageResult(request, provider, false)
}

// readConfluencePage accepts a decimal page ID, requests storage body and checks exact page/space plus a positive returned version.
// It does not use an arbitrary download URL or broaden the configured space when resolving the page.
// readConfluencePage принимает десятичный page ID, запрашивает storage body и проверяет точные страницу и пространство и положительную версию ответа.
// Произвольный download URL не используется, а настроенная область пространства при поиске страницы не расширяется.
func (adapter *Adapter) readConfluencePage(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, pageID string) (confluencePage, error) {
	if pageID == "" || strings.Trim(pageID, "0123456789") != "" {
		return confluencePage{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	query := url.Values{"body-format": {"storage"}}
	body, err := adapter.configuredProviderJSON(ctx, request, capability, configuration, http.MethodGet,
		"/wiki/api/v2/pages/"+url.PathEscape(pageID), query, nil, "")
	if err != nil {
		return confluencePage{}, err
	}
	var provider confluencePage
	if decodeProviderJSON(body, &provider) != nil || provider.ID != pageID || provider.SpaceID != configuration["space_id"] || provider.Version.Number < 1 {
		return confluencePage{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return provider, nil
}

// confluencePageResult requires nonempty page identity/title/status and positive version, optionally including storage body in the safe projection.
// It assumes the caller already verified the space; receipt binding is delegated to providerResult.
// confluencePageResult требует непустые идентичность, title и status страницы и положительную версию, необязательно добавляя storage body в безопасную проекцию.
// Предполагается предварительная проверка пространства вызывающим; привязка квитанции передаётся providerResult.
func confluencePageResult(request Request, provider confluencePage, includeBody bool) (Result, error) {
	if provider.ID == "" || provider.Title == "" || provider.Version.Number < 1 || provider.Status == "" {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	projection := map[string]any{
		"id": provider.ID, "title": provider.Title, "version": provider.Version.Number, "status": provider.Status,
	}
	if includeBody {
		projection["body"] = provider.Body.Storage.Value
	}
	return providerResult(request, "confluence-page:"+provider.ID, projection)
}

// confluenceBodyContainsEffect performs a literal storage-body marker lookup for reconciliation, not signature or provenance verification.
// confluenceBodyContainsEffect буквально ищет метку в storage body для сверки, не проверяя подпись или происхождение.
func confluenceBodyContainsEffect(page confluencePage, effectKey string) bool {
	return strings.Contains(page.Body.Storage.Value, "<!-- kodex-effect:"+effectKey+" -->")
}

// uploadConfluenceAttachment validates selected filename/media/base64 fields, resolves the page in the configured space and posts one multipart file.
// It requires a single returned attachment with matching filename; transport payload bounds and mutation retry policy remain in callProvider.
// uploadConfluenceAttachment проверяет выбранные поля имени, media и base64, разрешает страницу в настроенном пространстве и отправляет один multipart-файл.
// Требуется одно возвращённое вложение с совпадающим именем; пределы payload и политика повторов изменения остаются в callProvider.
func (adapter *Adapter) uploadConfluenceAttachment(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, raw []byte) (Result, error) {
	var input struct {
		PageID    string `json:"page_id"`
		FileName  string `json:"file_name"`
		MediaType string `json:"media_type"`
		Content   string `json:"content_base64"`
	}
	if decodeProviderJSON(raw, &input) != nil || strings.ContainsAny(input.FileName, "\r\n/\\") {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	mediaType, _, err := mime.ParseMediaType(input.MediaType)
	if err != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	content, err := base64.StdEncoding.Strict().DecodeString(input.Content)
	if err != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	if _, err := adapter.readConfluencePage(ctx, request, capability, configuration, input.PageID); err != nil {
		return Result{}, err
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": "file", "filename": input.FileName}))
	header.Set("Content-Type", mediaType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	if _, err = part.Write(content); err != nil {
		return Result{}, err
	}
	if err = writer.WriteField("minorEdit", "true"); err != nil {
		return Result{}, err
	}
	if err = writer.Close(); err != nil {
		return Result{}, err
	}
	response, err := adapter.callProvider(ctx, providerCall{BaseURL: configuration["base_url"], Method: http.MethodPost,
		Path: "/wiki/rest/api/content/" + url.PathEscape(input.PageID) + "/child/attachment", AuthScheme: configuration["auth_scheme"], Username: configuration["username"],
		Credential: request.Credential, Capability: capability, EffectKey: request.EffectKey, MultipartBody: body.Bytes(), MultipartType: writer.FormDataContentType()})
	if err != nil {
		return Result{}, err
	}
	var result struct {
		Results []struct {
			ID, Title string
			Metadata  struct {
				MediaType string `json:"mediaType"`
			} `json:"metadata"`
		} `json:"results"`
	}
	if decodeProviderJSON(response, &result) != nil || len(result.Results) != 1 || result.Results[0].ID == "" || result.Results[0].Title != input.FileName {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	item := result.Results[0]
	return providerResult(request, "confluence-attachment:"+item.ID, map[string]any{"attachment_id": item.ID, "title": item.Title, "media_type": item.Metadata.MediaType})
}
