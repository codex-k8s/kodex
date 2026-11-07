package integration

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// confluenceRecoveryInput pins original content/version selectors and optional preserved fields.
// The owner supplies canonical input; no server response may overwrite the immutable request.
// confluenceRecoveryInput закрепляет исходные content/version selectors и optional preserved fields.
// Owner передаёт canonical input; server response не переписывает immutable request.
type confluenceRecoveryInput struct {
	PageID          string `json:"page_id"`
	ParentID        string `json:"parent_id"`
	ExpectedVersion int64  `json:"expected_version"`
	Title, Body     string
}

// confluenceRecoveryParent compares the exact returned immediate ancestor without following links.
// confluenceRecoveryParent сравнивает exact returned immediate ancestor без перехода по ссылкам.
func confluenceRecoveryParent(page confluenceServerPage) string {
	if len(page.Ancestors) == 0 {
		return ""
	}
	return page.Ancestors[len(page.Ancestors)-1].ID
}

// readConfluenceRecoveryPage verifies addressed metadata before fetching storage body. Current
// and historical versions use explicit fixed selectors and must match in both observations.
// readConfluenceRecoveryPage проверяет addressed metadata до storage body. Current и historical
// versions используют explicit fixed selectors и совпадают в обоих observations.
func (adapter *Adapter) readConfluenceRecoveryPage(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, id string, version int64, historical bool) (confluenceServerPage, error) {
	if !decimalID(id) || version < 1 || version > 2147483647 {
		return confluenceServerPage{}, &UnknownOutcomeError{}
	}
	status := "current"
	if historical {
		status = "historical"
	}
	var page confluenceServerPage
	for _, bodyRequired := range []bool{false, true} {
		expand := "space,version,ancestors,history"
		if bodyRequired {
			expand += ",body.storage"
		}
		query := url.Values{"expand": {expand}, "status": {status}}
		if historical {
			query.Set("version", strconv.FormatInt(version, 10))
		}
		raw, err := adapter.confluenceServerJSON(ctx, request, capability, configuration, http.MethodGet, "/content/"+id, query, nil, "")
		if err != nil {
			return confluenceServerPage{}, err
		}
		var observed confluenceServerPage
		if decodeAtlassianRecoveryJSON(raw, &observed) != nil || observed.ID != id || observed.Type != "page" || observed.Status != status || observed.Space.Key != configuration["space_key"] || observed.Version.Number != version || observed.Title == "" || bodyRequired && observed.Body.Storage.Representation != "storage" {
			return confluenceServerPage{}, &UnknownOutcomeError{}
		}
		if bodyRequired && (page.Title != observed.Title || confluenceRecoveryParent(page) != confluenceRecoveryParent(observed) ||
			page.Version.By.UserKey != observed.Version.By.UserKey || page.Version.By.Type != observed.Version.By.Type ||
			page.History.CreatedBy.UserKey != observed.History.CreatedBy.UserKey || page.History.CreatedBy.Type != observed.History.CreatedBy.Type) {
			return confluenceServerPage{}, &UnknownOutcomeError{}
		}
		page = observed
	}
	return page, nil
}

// confluenceRecoveryAuthor requires the stable known-user key rather than username/display name.
// confluenceRecoveryAuthor требует stable known-user key, а не username/display name.
func confluenceRecoveryAuthor(user atlassianRecoveryUser, key string) bool {
	return user.Type == "known" && validAtlassianRecoveryKey(user.UserKey) && user.UserKey == key
}

// recoverConfluenceServerEffect checks a configured space and dispatches only marked page
// create/update recovery. A page existing in that space alone cannot prove either operation.
// recoverConfluenceServerEffect проверяет configured space и выбирает только marked page
// create/update recovery. Одно существование страницы в space не доказывает операцию.
func (adapter *Adapter) recoverConfluenceServerEffect(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, input []byte, userKey string) (Result, error) {
	raw, err := adapter.confluenceServerJSON(ctx, request, capability, configuration, http.MethodGet, "/space/"+url.PathEscape(configuration["space_key"]), nil, nil, "")
	if err != nil {
		return Result{}, err
	}
	var space struct {
		ID  int64
		Key string
	}
	if decodeAtlassianRecoveryJSON(raw, &space) != nil || space.ID < 1 || space.Key != configuration["space_key"] {
		return Result{}, &UnknownOutcomeError{}
	}
	var value confluenceRecoveryInput
	if decodeProviderJSON(input, &value) != nil {
		return Result{}, &UnknownOutcomeError{}
	}
	if request.Operation == "confluence.page.update" {
		return adapter.recoverConfluenceServerUpdate(ctx, request, capability, configuration, value, userKey)
	}
	return adapter.recoverConfluenceServerCreate(ctx, request, capability, configuration, value, userKey)
}

// confluenceRecoveryCreateMatches attributes the exact untouched marked page to the current key
// and canonical parent/body/title. Duplicate markers and later modified versions remain unknown.
// confluenceRecoveryCreateMatches связывает exact untouched marked page с current key и canonical
// parent/body/title. Duplicate markers и позднее изменённые versions остаются unknown.
func confluenceRecoveryCreateMatches(page confluenceServerPage, value confluenceRecoveryInput, configuration map[string]string, key, marker string) bool {
	return eligibleConfluenceServerPage(page, configuration) && page.Version.Number == 1 && page.Title == value.Title && page.Body.Storage.Representation == "storage" && page.Body.Storage.Value == value.Body+"\n"+marker && strings.Count(page.Body.Storage.Value, marker) == 1 && confluenceRecoveryParent(page) == value.ParentID && confluenceRecoveryAuthor(page.History.CreatedBy, key) && confluenceRecoveryAuthor(page.Version.By, key)
}

// recoverConfluenceServerCreate exhausts a bounded exact-title collection and re-reads the sole
// matching page. It never adopts a list snapshot without creator/version/body provenance.
// recoverConfluenceServerCreate исчерпывает bounded exact-title collection и перечитывает единственную
// matching page. List snapshot без creator/version/body provenance не принимается.
func (adapter *Adapter) recoverConfluenceServerCreate(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, value confluenceRecoveryInput, key string) (Result, error) {
	if value.ParentID != "" {
		if _, err := adapter.readConfluenceServerPage(ctx, request, capability, configuration, value.ParentID, false); err != nil {
			return Result{}, err
		}
	}
	marker := confluenceServerMarker(request)
	offset := 0
	var candidate *confluenceServerPage
	seen := map[string]bool{}
	exhausted := false
	for iteration := 0; iteration < 5; iteration++ {
		page, next, err := adapter.searchConfluenceServerPages(ctx, request, capability, configuration, value.Title, offset, 20, true, true)
		if err != nil {
			return Result{}, err
		}
		for _, item := range page.Results {
			if seen[item.ID] || candidate != nil || !confluenceRecoveryCreateMatches(item, value, configuration, key, marker) {
				return Result{}, &UnknownOutcomeError{}
			}
			seen[item.ID] = true
			copy := item
			candidate = &copy
		}
		if next == "" {
			exhausted = true
			break
		}
		offset, _ = strconv.Atoi(next)
	}
	if !exhausted || candidate == nil {
		return Result{}, &UnknownOutcomeError{}
	}
	page, err := adapter.readConfluenceRecoveryPage(ctx, request, capability, configuration, candidate.ID, 1, false)
	if err != nil {
		return Result{}, err
	}
	if !confluenceRecoveryCreateMatches(page, value, configuration, key, marker) {
		return Result{}, &UnknownOutcomeError{}
	}
	return confluenceServerPageResult(request, page, false)
}

// recoverConfluenceServerUpdate verifies the exact authored successor and historical predecessor.
// Omitted title/body are compared to that predecessor, not filled from the alleged updated page.
// No PUT is issued when evidence is missing, conflicted, moved or attributed to another key.
// recoverConfluenceServerUpdate сверяет exact authored successor и historical predecessor.
// Пропущенные title/body сравниваются с predecessor, а не берутся из alleged updated page.
// При missing/conflicted/moved evidence либо другом авторе PUT не выполняется.
func (adapter *Adapter) recoverConfluenceServerUpdate(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, value confluenceRecoveryInput, key string) (Result, error) {
	if value.ExpectedVersion < 1 || value.ExpectedVersion >= 2147483647 || value.Title == "" && value.Body == "" {
		return Result{}, &UnknownOutcomeError{}
	}
	page, err := adapter.readConfluenceRecoveryPage(ctx, request, capability, configuration, value.PageID, value.ExpectedVersion+1, false)
	if err != nil {
		return Result{}, err
	}
	marker := confluenceServerMarker(request)
	if !confluenceRecoveryAuthor(page.Version.By, key) || strings.Count(page.Body.Storage.Value, marker) != 1 {
		return Result{}, &UnknownOutcomeError{}
	}
	previous, err := adapter.readConfluenceRecoveryPage(ctx, request, capability, configuration, value.PageID, value.ExpectedVersion, true)
	if err != nil {
		return Result{}, err
	}
	title, body := value.Title, value.Body
	if title == "" {
		title = previous.Title
	}
	if body == "" {
		body = previous.Body.Storage.Value
	}
	if strings.Contains(previous.Body.Storage.Value, marker) || page.Title != title || page.Body.Storage.Value != body+"\n"+marker || confluenceRecoveryParent(page) != confluenceRecoveryParent(previous) {
		return Result{}, &UnknownOutcomeError{}
	}
	return confluenceServerPageResult(request, page, false)
}
