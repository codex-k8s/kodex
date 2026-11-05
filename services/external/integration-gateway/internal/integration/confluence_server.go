package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// confluenceServerPage represents the Server v1 nested space/storage contract, independent of Cloud v2 spaceId.
// confluenceServerPage представляет вложенные space/storage Server v1 независимо от Cloud v2 spaceId.
type confluenceServerPage struct {
	ID, Type, Title, Status string
	Space                   struct{ Key string }
	Version                 struct{ Number int64 }
	Body                    struct {
		Storage struct {
			Value          string
			Representation string
		}
	}
	Ancestors []struct{ ID string }
}

// confluenceServerPages captures one bounded offset page; only a validated next offset can leave the adapter.
// confluenceServerPages хранит одну ограниченную страницу; наружу выходит только проверенное смещение продолжения.
type confluenceServerPages struct {
	Start, Limit, Size *int
	Results            []confluenceServerPage
	Links              struct{ Next string } `json:"_links"`
}

// confluenceServerSpaceKey rejects path/query delimiters and controls while permitting ordinary and personal space keys.
// confluenceServerSpaceKey отклоняет разделители пути/query и controls, допуская обычные и personal space keys.
func confluenceServerSpaceKey(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || strings.ContainsRune("._~-", character)) {
			return false
		}
	}
	return true
}

// confluenceServerJSON preserves fixed REST v1 routing beneath the explicit Connection context and exact space boundary.
// confluenceServerJSON сохраняет фиксированный REST v1 маршрут под явным context и точную границу пространства.
func (adapter *Adapter) confluenceServerJSON(ctx context.Context, request Request, capability integrationpackage.Capability,
	configuration map[string]string, method, path string, query url.Values, body any, effect string) ([]byte, error) {
	prefix, err := atlassianServerContext(configuration)
	if err != nil || !confluenceServerSpaceKey(configuration["space_key"]) {
		return nil, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	return adapter.configuredProviderJSON(ctx, request, capability, configuration, method, prefix+"/rest/api"+path, query, body, effect)
}

// confluenceServerSpace reads the configured key and verifies the Server numeric ID before producing metadata or health.
// confluenceServerSpace читает заданный ключ и проверяет Server numeric ID перед metadata либо health.
func (adapter *Adapter) confluenceServerSpace(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string) (map[string]any, error) {
	body, err := adapter.confluenceServerJSON(ctx, request, capability, configuration, http.MethodGet,
		"/space/"+url.PathEscape(configuration["space_key"]), nil, nil, "")
	if err != nil {
		return nil, err
	}
	var value struct {
		ID        json.Number
		Key, Name string
	}
	if decodeProviderJSON(body, &value) != nil {
		return nil, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	id, err := value.ID.Int64()
	if err != nil || id < 1 || value.Key != configuration["space_key"] || value.Name == "" {
		return nil, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return map[string]any{"id": strconv.FormatInt(id, 10), "key": value.Key, "name": value.Name}, nil
}

// eligibleConfluenceServerPage requires a real current page, exact space and bounded version before body or writes are exposed.
// eligibleConfluenceServerPage требует current page, точное пространство и ограниченную версию до выдачи тела либо записи.
func eligibleConfluenceServerPage(page confluenceServerPage, configuration map[string]string) bool {
	return decimalID(page.ID) && page.Type == "page" && page.Status == "current" && page.Title != "" &&
		page.Space.Key == configuration["space_key"] && page.Version.Number > 0 && page.Version.Number <= 2147483647
}

// readConfluenceServerPage fetches metadata first; storage content is requested only after the page passes space eligibility.
// Both observations are checked because a provider-side move can race the read; no snapshot or cross-service transaction is claimed.
// readConfluenceServerPage сначала получает metadata; storage читается лишь после проверки пространства.
// Обе выдачи проверяются из-за возможного concurrent move; snapshot и межсервисная транзакция не обещаются.
func (adapter *Adapter) readConfluenceServerPage(ctx context.Context, request Request, capability integrationpackage.Capability,
	configuration map[string]string, pageID string, includeBody bool) (confluenceServerPage, error) {
	if !decimalID(pageID) {
		return confluenceServerPage{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	var page confluenceServerPage
	expansions := []string{"space,version,ancestors"}
	if includeBody {
		expansions = append(expansions, "space,version,ancestors,body.storage")
	}
	for _, expand := range expansions {
		body, err := adapter.confluenceServerJSON(ctx, request, capability, configuration, http.MethodGet,
			"/content/"+url.PathEscape(pageID), url.Values{"expand": {expand}}, nil, "")
		if err != nil {
			return confluenceServerPage{}, err
		}
		var observed confluenceServerPage
		if decodeProviderJSON(body, &observed) != nil || observed.ID != pageID || !eligibleConfluenceServerPage(observed, configuration) ||
			expand == "space,version,ancestors,body.storage" && observed.Body.Storage.Representation != "storage" {
			return confluenceServerPage{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		page = observed
	}
	return page, nil
}

// confluenceServerNext extracts only the exact contiguous offset and never follows a provider-supplied destination.
// Both context-relative and origin-relative Server links are supported, but foreign hosts, paths and selectors are rejected.
// confluenceServerNext извлекает только непрерывное смещение и не следует provider destination.
// Допускаются context-relative и origin-relative Server links; чужие hosts, paths и selectors отклоняются.
func confluenceServerNext(configuration map[string]string, query url.Values, page confluenceServerPages) (string, error) {
	if page.Start == nil || page.Limit == nil {
		return "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	if page.Links.Next == "" {
		return "", nil
	}
	prefix, err := atlassianServerContext(configuration)
	if err != nil {
		return "", err
	}
	origin, originErr := url.Parse(configuration["base_url"])
	next, parseErr := url.Parse(page.Links.Next)
	if originErr != nil || parseErr != nil || next.User != nil || next.Fragment != "" ||
		(next.Scheme != "" || next.Host != "") && (next.Scheme != "https" || next.Host != origin.Host) ||
		(next.Path != "/rest/api/content" && next.Path != prefix+"/rest/api/content") || next.RawPath != "" {
		return "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	values, err := url.ParseQuery(next.RawQuery)
	if err != nil || len(values["start"]) != 1 {
		return "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	for key, value := range values {
		if len(value) != 1 {
			return "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		switch key {
		case "start":
		case "limit":
			if value[0] != strconv.Itoa(*page.Limit) {
				return "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
			}
		case "spaceKey", "title", "type", "status", "expand":
			if value[0] != query.Get(key) {
				return "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
			}
		default:
			return "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
	}
	offset, err := atlassianServerOffset(values.Get("start"))
	if err != nil || len(page.Results) == 0 || offset != *page.Start+len(page.Results) {
		return "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return strconv.Itoa(offset), nil
}

// searchConfluenceServerPages returns a scoped current-page list with verified offset metadata and next-link selectors.
// searchConfluenceServerPages возвращает current pages заданного пространства с проверкой offset metadata и selectors.
func (adapter *Adapter) searchConfluenceServerPages(ctx context.Context, request Request, capability integrationpackage.Capability,
	configuration map[string]string, title string, offset, limit int, includeBody bool) (confluenceServerPages, string, error) {
	expand := "space,version,ancestors"
	if includeBody {
		expand += ",body.storage"
	}
	query := url.Values{"spaceKey": {configuration["space_key"]}, "type": {"page"}, "status": {"current"},
		"title": {title}, "start": {strconv.Itoa(offset)}, "limit": {strconv.Itoa(limit)}, "expand": {expand}}
	body, err := adapter.confluenceServerJSON(ctx, request, capability, configuration, http.MethodGet, "/content", query, nil, "")
	if err != nil {
		return confluenceServerPages{}, "", err
	}
	var page confluenceServerPages
	if decodeProviderJSON(body, &page) != nil || page.Start == nil || *page.Start != offset || page.Limit == nil ||
		*page.Limit < 1 || *page.Limit > limit || page.Size == nil || *page.Size != len(page.Results) || len(page.Results) > *page.Limit {
		return page, "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	seen := make(map[string]bool, len(page.Results))
	for _, item := range page.Results {
		if !eligibleConfluenceServerPage(item, configuration) || seen[item.ID] || includeBody && item.Body.Storage.Representation != "storage" {
			return page, "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		seen[item.ID] = true
	}
	next, err := confluenceServerNext(configuration, query, page)
	return page, next, err
}

// confluenceServerPageResult exposes a bounded projection after the caller has verified space and page eligibility.
// confluenceServerPageResult выдаёт ограниченную проекцию после проверки пространства и страницы вызывающим.
func confluenceServerPageResult(request Request, page confluenceServerPage, includeBody bool) (Result, error) {
	output := map[string]any{"id": page.ID, "title": page.Title, "version": page.Version.Number, "status": page.Status}
	if includeBody {
		output["body"] = page.Body.Storage.Value
	}
	return providerResult(request, "confluence-page:"+page.ID, output)
}

// confluenceServerMarker binds reconciliation to the owner effect and immutable input digest without inserting raw IDs into HTML.
// confluenceServerMarker связывает сверку с owner effect и immutable input digest без вставки raw IDs в HTML.
func confluenceServerMarker(request Request) string {
	digest := sha256.Sum256([]byte(request.EffectKey + "\x00" + request.InputDigest))
	return "<!-- kodex-effect-sha256:" + hex.EncodeToString(digest[:]) + " -->"
}

// executeConfluenceServer uses only the separately versioned Server v1 surface with current-page and exact-space checks.
// executeConfluenceServer использует только отдельную Server v1 поверхность с current-page и exact-space проверками.
func (adapter *Adapter) executeConfluenceServer(ctx context.Context, request Request, capability integrationpackage.Capability,
	configuration map[string]string, input []byte) (Result, error) {
	if _, err := atlassianServerContext(configuration); err != nil || !confluenceServerSpaceKey(configuration["space_key"]) {
		return Result{}, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	switch request.Operation {
	case "confluence.identity.read":
		body, err := adapter.confluenceServerJSON(ctx, request, capability, configuration, http.MethodGet, "/user/current", nil, nil, "")
		if err != nil {
			return Result{}, err
		}
		var user struct{ Type, UserKey, Username, DisplayName string }
		if decodeProviderJSON(body, &user) != nil || user.Type != "known" || user.UserKey == "" || user.Username == "" || user.DisplayName == "" {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return providerResult(request, "confluence-identity:"+user.UserKey, map[string]any{"id": user.UserKey, "username": user.Username, "display_name": user.DisplayName})
	case "confluence.space.read":
		value, err := adapter.confluenceServerSpace(ctx, request, capability, configuration)
		if err != nil {
			return Result{}, err
		}
		return providerResult(request, "confluence-space:"+value["id"].(string), value)
	case "confluence.page.search":
		var value struct {
			Title, Cursor string
			Limit         int
		}
		if decodeProviderJSON(input, &value) != nil {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		if value.Limit == 0 {
			value.Limit = 20
		}
		offset, err := atlassianServerOffset(value.Cursor)
		if err != nil || value.Limit < 1 || value.Limit > 50 {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		page, next, err := adapter.searchConfluenceServerPages(ctx, request, capability, configuration, value.Title, offset, value.Limit, false)
		if err != nil {
			return Result{}, err
		}
		pages := make([]map[string]string, 0, len(page.Results))
		for _, item := range page.Results {
			pages = append(pages, map[string]string{"id": item.ID, "title": item.Title, "status": item.Status})
		}
		encoded, _ := json.Marshal(pages)
		output := map[string]any{"pages": string(encoded), "count": len(pages)}
		if next != "" {
			output["next_cursor"] = next
		}
		return providerResult(request, "confluence-pages:"+request.EffectKey, output)
	case "confluence.page.read":
		var value struct {
			PageID string `json:"page_id"`
		}
		if decodeProviderJSON(input, &value) != nil {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		page, err := adapter.readConfluenceServerPage(ctx, request, capability, configuration, value.PageID, true)
		if err != nil {
			return Result{}, err
		}
		return confluenceServerPageResult(request, page, true)
	case "confluence.page.create":
		return adapter.createConfluenceServerPage(ctx, request, capability, configuration, input)
	case "confluence.page.update":
		return adapter.updateConfluenceServerPage(ctx, request, capability, configuration, input)
	default:
		return Result{}, &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
}

// createConfluenceServerPage validates an optional parent and reconciles title/body/effect before sending one create.
// createConfluenceServerPage проверяет parent и сверяет title/body/effect до единственного create.
func (adapter *Adapter) createConfluenceServerPage(ctx context.Context, request Request, capability integrationpackage.Capability,
	configuration map[string]string, input []byte) (Result, error) {
	var value struct {
		Title, Body string
		ParentID    string `json:"parent_id"`
	}
	if decodeProviderJSON(input, &value) != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	if value.ParentID != "" {
		if _, err := adapter.readConfluenceServerPage(ctx, request, capability, configuration, value.ParentID, false); err != nil {
			return Result{}, err
		}
	}
	bodyValue := value.Body + "\n" + confluenceServerMarker(request)
	existing, next, err := adapter.searchConfluenceServerPages(ctx, request, capability, configuration, value.Title, 0, 2, true)
	if err != nil {
		return Result{}, err
	}
	if next != "" || len(existing.Results) > 1 {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	if len(existing.Results) == 1 {
		page := existing.Results[0]
		parent := ""
		if len(page.Ancestors) != 0 {
			parent = page.Ancestors[len(page.Ancestors)-1].ID
		}
		if page.Title != value.Title || page.Body.Storage.Value != bodyValue || parent != value.ParentID {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		return confluenceServerPageResult(request, page, false)
	}
	payload := map[string]any{"type": "page", "status": "current", "title": value.Title, "space": map[string]string{"key": configuration["space_key"]},
		"body": map[string]any{"storage": map[string]string{"representation": "storage", "value": bodyValue}}}
	if value.ParentID != "" {
		payload["ancestors"] = []map[string]string{{"id": value.ParentID}}
	}
	body, err := adapter.confluenceServerJSON(ctx, request, capability, configuration, http.MethodPost, "/content", url.Values{"expand": {"space,version,ancestors"}}, payload, request.EffectKey)
	if err != nil {
		return Result{}, err
	}
	var created confluenceServerPage
	if decodeProviderJSON(body, &created) != nil || !eligibleConfluenceServerPage(created, configuration) || created.Title != value.Title || created.Version.Number != 1 {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	parent := ""
	if len(created.Ancestors) != 0 {
		parent = created.Ancestors[len(created.Ancestors)-1].ID
	}
	if parent != value.ParentID {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return confluenceServerPageResult(request, created, false)
}

// updateConfluenceServerPage preserves unchanged fields, verifies expected version and sends only its next version.
// A matching prior effect can be read back without another PUT; conflicts and unknown outcomes never trigger a blind write.
// updateConfluenceServerPage сохраняет неизменяемые поля и отправляет только следующую ожидаемую версию.
// Совпавший прежний effect читается без повторного PUT; конфликт и unknown outcome не вызывают слепую запись.
func (adapter *Adapter) updateConfluenceServerPage(ctx context.Context, request Request, capability integrationpackage.Capability,
	configuration map[string]string, input []byte) (Result, error) {
	var value struct {
		PageID          string `json:"page_id"`
		ExpectedVersion int64  `json:"expected_version"`
		Title, Body     string
	}
	if decodeProviderJSON(input, &value) != nil || value.ExpectedVersion < 1 || value.ExpectedVersion >= 2147483647 || value.Title == "" && value.Body == "" {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	page, err := adapter.readConfluenceServerPage(ctx, request, capability, configuration, value.PageID, true)
	if err != nil {
		return Result{}, err
	}
	if value.Title == "" {
		value.Title = page.Title
	}
	marker := confluenceServerMarker(request)
	if value.Body == "" {
		value.Body = page.Body.Storage.Value
	}
	if page.Version.Number == value.ExpectedVersion+1 && page.Title == value.Title &&
		strings.HasSuffix(page.Body.Storage.Value, "\n"+marker) &&
		(value.Body == page.Body.Storage.Value || value.Body+"\n"+marker == page.Body.Storage.Value) {
		return confluenceServerPageResult(request, page, false)
	}
	if page.Version.Number != value.ExpectedVersion {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	payload := map[string]any{"id": value.PageID, "type": "page", "status": "current", "title": value.Title,
		"version": map[string]any{"number": value.ExpectedVersion + 1, "message": "Kodex managed effect"},
		"body":    map[string]any{"storage": map[string]string{"representation": "storage", "value": value.Body + "\n" + marker}}}
	body, err := adapter.confluenceServerJSON(ctx, request, capability, configuration, http.MethodPut, "/content/"+url.PathEscape(value.PageID),
		url.Values{"expand": {"space,version"}}, payload, request.EffectKey)
	if err != nil {
		return Result{}, err
	}
	var updated confluenceServerPage
	if decodeProviderJSON(body, &updated) != nil || !eligibleConfluenceServerPage(updated, configuration) || updated.ID != value.PageID ||
		updated.Version.Number != value.ExpectedVersion+1 || updated.Title != value.Title {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return confluenceServerPageResult(request, updated, false)
}
