package integration

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// gitLabScopePageInput contains only bounded discovery/search parameters; callers cannot select a URL, authorization header or provider API scope.
// gitLabScopePageInput содержит только bounded discovery/search параметры; caller не выбирает URL, authorization header либо provider API scope.
type gitLabScopePageInput struct {
	Search string `json:"search"`
	Ref    string `json:"ref"`
	Limit  int    `json:"limit"`
	Cursor int    `json:"cursor"`
}

// gitLabCodeMatch projects one basic-search snippet from the already verified project and requested ref; it carries no download URL or credential.
// gitLabCodeMatch проецирует один basic-search snippet проверенного проекта и requested ref; download URL либо credential отсутствуют.
type gitLabCodeMatch struct {
	ProjectID int64  `json:"project_id"`
	Path      string `json:"path"`
	Ref       string `json:"ref"`
	Data      string `json:"data"`
	StartLine int64  `json:"startline"`
}

// gitLabScopePage validates bounded values even for internal callers and supplies the same finite offset pagination defaults as existing typed catalog tools.
// gitLabScopePage проверяет bounded значения даже для внутренних callers и задаёт те же finite offset pagination defaults, что прежние typed catalog tools.
func gitLabScopePage(raw []byte) (gitLabScopePageInput, error) {
	var input gitLabScopePageInput
	if decodeProviderJSON(raw, &input) != nil || input.Limit < 0 || input.Limit > 100 || input.Cursor < 0 || input.Cursor > 10000 {
		return input, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	if input.Limit == 0 {
		input.Limit = 20
	}
	if input.Cursor == 0 {
		input.Cursor = 1
	}
	return input, nil
}

// validGitLabScopedProject requires canonical namespace data and, for direct group projects, the fresh configured group ID; shared foreign projects fail closed.
// validGitLabScopedProject требует canonical namespace data и fresh configured group ID для direct group projects; shared foreign projects закрыто отклоняются.
func validGitLabScopedProject(scope integrationpackage.GitLabResourceScope, group gitLabScopedGroupMetadata, project gitLabScopedProjectMetadata) bool {
	return project.ID > 0 && project.ID <= maxGitLabScopedProjectID && project.Namespace.ID > 0 && project.Namespace.ID <= maxGitLabScopedProjectID && (project.Namespace.Kind == "group" || project.Namespace.Kind == "user") &&
		scope.AllowsProviderProject(project.PathWithNamespace, project.Namespace.FullPath) &&
		(scope.Kind != "GROUP" || project.Namespace.Kind == "group" && (project.Namespace.FullPath != group.FullPath || project.Namespace.ID == group.ID))
}

// executeGitLabScopeCatalog checks only current identity/root metadata or one bounded project page; discovery never proves write permission and never follows provider URLs.
// executeGitLabScopeCatalog проверяет только current identity/root metadata либо одну bounded project page; discovery не доказывает write permission и не следует provider URLs.
func (adapter *Adapter) executeGitLabScopeCatalog(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, raw []byte) (Result, error) {
	scope, err := integrationpackage.ParseGitLabResourceScope(configuration)
	if err != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	var group gitLabScopedGroupMetadata
	if scope.Kind == "GROUP" {
		group, err = adapter.readGitLabScopedGroup(ctx, request, capability, configuration, scope)
		if err != nil {
			return Result{}, err
		}
	}
	var project gitLabScopedProjectMetadata
	if scope.Kind == "PROJECT" {
		body, err := adapter.gitLabJSON(ctx, request, capability, configuration, http.MethodGet, "/api/v4/projects/"+url.PathEscape(scope.ProjectPath), nil, nil, "")
		if err != nil {
			return Result{}, err
		}
		if decodeProviderJSON(body, &project) != nil || !validGitLabScopedProject(scope, group, project) {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
	}
	if request.Operation == "gitlab.scope.health.read" {
		body, err := adapter.gitLabJSON(ctx, request, capability, configuration, http.MethodGet, "/api/v4/user", nil, nil, "")
		if err != nil {
			return Result{}, err
		}
		var identity struct {
			ID int64 `json:"id"`
		}
		if decodeProviderJSON(body, &identity) != nil || identity.ID < 1 {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return providerResult(request, "gitlab-scope:"+scope.Kind, map[string]any{"scope_kind": scope.Kind, "identity_id": identity.ID})
	}
	if request.Operation != "gitlab.project.list" {
		return Result{}, &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
	input, err := gitLabScopePage(raw)
	if err != nil {
		return Result{}, err
	}
	if scope.Kind == "PROJECT" {
		items := []gitLabScopedProjectMetadata{}
		if input.Cursor == 1 && (input.Search == "" || strings.Contains(strings.ToLower(project.PathWithNamespace), strings.ToLower(input.Search))) {
			items = append(items, project)
		}
		return catalogPageProjection(request, input.Cursor, items, 0)
	}
	query := url.Values{"per_page": {strconv.Itoa(input.Limit)}, "page": {strconv.Itoa(input.Cursor)}, "order_by": {"id"}, "sort": {"asc"}}
	if input.Search != "" {
		query.Set("search", input.Search)
	}
	path := "/api/v4/projects"
	if scope.Kind == "GROUP" {
		path = "/api/v4/groups/" + strconv.FormatInt(group.ID, 10) + "/projects"
		query.Set("with_shared", "false")
		query.Set("include_subgroups", strconv.FormatBool(scope.IncludeSubgroups))
	}
	response, err := adapter.callProviderResponse(ctx, providerCall{BaseURL: configuration["base_url"], Method: http.MethodGet, Path: path, Query: query,
		AuthScheme: "BEARER", Credential: request.Credential, Capability: capability, Client: adapter.nativeHTTPClient})
	if err != nil {
		return Result{}, err
	}
	seen := map[int64]bool{}
	return providerPageResult(request, response, input.Limit, input.Cursor, func(project gitLabScopedProjectMetadata) bool {
		if seen[project.ID] || !validGitLabScopedProject(scope, group, project) {
			return false
		}
		seen[project.ID] = true
		return true
	})
}

// searchGitLabScopedRepository searches only the already verified numeric project identity/ref with explicit basic search; unsupported licensing or provider refusals never trigger a broader fallback.
// searchGitLabScopedRepository ищет только в проверенной numeric project identity/ref с explicit basic search; unsupported licensing либо provider refusal не запускают расширяющий fallback.
func (adapter *Adapter) searchGitLabScopedRepository(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, raw []byte) (Result, error) {
	input, err := gitLabScopePage(raw)
	projectID, idErr := strconv.ParseInt(configuration[gitLabResolvedProjectKey], 10, 64)
	if err != nil || idErr != nil || projectID < 1 || input.Search == "" || input.Ref == "" {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	query := url.Values{"scope": {"blobs"}, "search_type": {"basic"}, "search": {input.Search}, "ref": {input.Ref},
		"per_page": {strconv.Itoa(input.Limit)}, "page": {strconv.Itoa(input.Cursor)}}
	response, err := adapter.callProviderResponse(ctx, providerCall{BaseURL: configuration["base_url"], Method: http.MethodGet, Path: adapter.gitLabProjectPath(configuration) + "/search", Query: query,
		AuthScheme: "BEARER", Credential: request.Credential, Capability: capability, Client: adapter.nativeHTTPClient})
	if err != nil {
		return Result{}, err
	}
	return providerPageResult(request, response, input.Limit, input.Cursor, func(match gitLabCodeMatch) bool {
		if match.ProjectID != projectID || match.Ref != input.Ref || match.Path == "" || len(match.Path) > 1024 || strings.HasPrefix(match.Path, "/") ||
			len(match.Data) > 32768 || match.StartLine < 1 {
			return false
		}
		for _, part := range strings.Split(match.Path, "/") {
			if part == "" || part == "." || part == ".." || strings.ContainsAny(part, "\\\x00\r\n") {
				return false
			}
		}
		return utf8.ValidString(match.Path) && utf8.ValidString(match.Data)
	})
}
