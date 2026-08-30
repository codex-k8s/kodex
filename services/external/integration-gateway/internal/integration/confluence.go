package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

type confluencePage struct {
	ID, Title, Status string
	Version           struct {
		Number int64 `json:"number"`
	} `json:"version"`
	Body struct {
		Storage struct {
			Value string `json:"value"`
		} `json:"storage"`
	} `json:"body"`
}

func (adapter *Adapter) testConfluence(ctx context.Context, request Request, configuration map[string]string) error {
	definition := adapter.definitions["confluence"]
	capability, _ := definition.Capability(definition.Spec.HealthCheck.Operation)
	_, err := adapter.confluenceJSON(ctx, request, capability, configuration, http.MethodGet,
		"/wiki/api/v2/spaces/"+url.PathEscape(configuration["space_id"]), nil, nil, "")
	return err
}

func (adapter *Adapter) executeConfluence(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, canonicalInput []byte) (Result, error) {
	switch request.Operation {
	case "confluence.space.read":
		body, err := adapter.confluenceJSON(ctx, request, capability, configuration, http.MethodGet,
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
			Title string `json:"title"`
			Limit int64  `json:"limit"`
		}
		if decodeProviderJSON(canonicalInput, &input) != nil {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		if input.Limit == 0 {
			input.Limit = 20
		}
		query := url.Values{"space-id": {configuration["space_id"]}, "title": {input.Title}, "limit": {strconv.FormatInt(input.Limit, 10)}}
		body, err := adapter.confluenceJSON(ctx, request, capability, configuration, http.MethodGet, "/wiki/api/v2/pages", query, nil, "")
		if err != nil {
			return Result{}, err
		}
		var provider struct {
			Results []confluencePage `json:"results"`
		}
		if decodeProviderJSON(body, &provider) != nil || int64(len(provider.Results)) > input.Limit {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		pages := make([]map[string]any, 0, len(provider.Results))
		for _, page := range provider.Results {
			if page.ID == "" {
				return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
			}
			pages = append(pages, map[string]any{"id": page.ID, "title": page.Title, "status": page.Status})
		}
		encoded, _ := json.Marshal(pages)
		return providerResult(request, "confluence-search:"+request.EffectKey, map[string]any{"count": len(pages), "pages": string(encoded)})
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
		return Result{}, &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
}

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
		payload["parentId"] = input.ParentID
	}
	body, err := adapter.confluenceJSON(ctx, request, capability, configuration, http.MethodPost, "/wiki/api/v2/pages", nil, payload, request.EffectKey)
	if err != nil {
		return Result{}, err
	}
	var provider confluencePage
	if decodeProviderJSON(body, &provider) != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return confluencePageResult(request, provider, false)
}

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
	body, err := adapter.confluenceJSON(ctx, request, capability, configuration, http.MethodPut,
		"/wiki/api/v2/pages/"+url.PathEscape(input.PageID), nil, payload, request.EffectKey)
	if err != nil {
		return Result{}, err
	}
	var provider confluencePage
	if decodeProviderJSON(body, &provider) != nil || provider.Version.Number != input.ExpectedVersion+1 {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return confluencePageResult(request, provider, false)
}

func (adapter *Adapter) readConfluencePage(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, pageID string) (confluencePage, error) {
	query := url.Values{"body-format": {"storage"}}
	body, err := adapter.confluenceJSON(ctx, request, capability, configuration, http.MethodGet,
		"/wiki/api/v2/pages/"+url.PathEscape(pageID), query, nil, "")
	if err != nil {
		return confluencePage{}, err
	}
	var provider confluencePage
	if decodeProviderJSON(body, &provider) != nil || provider.ID != pageID || provider.Version.Number < 1 {
		return confluencePage{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return provider, nil
}

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

func (adapter *Adapter) confluenceJSON(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, method, path string, query url.Values, body any, effectKey string) ([]byte, error) {
	return adapter.callProvider(ctx, providerCall{
		BaseURL: configuration["base_url"], Method: method, Path: path, Query: query, Body: body,
		AuthScheme: configuration["auth_scheme"], Username: configuration["username"], Credential: request.Credential,
		EffectKey: effectKey, Capability: capability,
	})
}

func confluenceBodyContainsEffect(page confluencePage, effectKey string) bool {
	return strings.Contains(page.Body.Storage.Value, "<!-- kodex-effect:"+effectKey+" -->")
}
