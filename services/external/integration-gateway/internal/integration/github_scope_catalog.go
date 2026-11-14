package integration

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/google/go-github/v74/github"
)

// executeGitHubScopeCatalog returns authenticated identity or one bounded discovery page under the configured boundary; neither result grants execution authority.
// executeGitHubScopeCatalog возвращает аутентифицированную identity либо одну bounded discovery page настроенной области; результат не выдаёт полномочий исполнения.
func executeGitHubScopeCatalog(ctx context.Context, client *github.Client, request Request, capability integrationpackage.Capability, configuration map[string]string, raw []byte) (Result, error) {
	identity, err := readGitHubScopedIdentity(ctx, client, capability)
	if err != nil {
		return Result{}, err
	}
	if !request.PersonalCredentialAdmission && configuration["service_identity"] != "" && configuration["service_identity"] != identity.Login {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	if request.Operation == "github.identity.read" {
		return providerResult(request, "github-user:"+strconv.FormatInt(identity.ID, 10), identity)
	}
	if request.Operation != "github.repository.list" {
		return Result{}, &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
	scope, err := integrationpackage.ParseGitHubResourceScope(configuration)
	if err != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	var input struct {
		Limit  int `json:"limit"`
		Cursor int `json:"cursor"`
	}
	if json.Unmarshal(raw, &input) != nil || input.Limit < 0 || input.Limit > 100 || input.Cursor < 0 || input.Cursor > 10000 {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	if input.Limit == 0 {
		input.Limit = 20
	}
	if input.Cursor == 0 {
		input.Cursor = 1
	}
	repos, next, err := listGitHubAuthenticatedRepositories(ctx, client, capability, input.Cursor, input.Limit)
	if err != nil {
		return Result{}, err
	}
	items := make([]githubScopedRepository, 0, len(repos))
	seen := map[int64]bool{}
	for _, repo := range repos {
		view, valid := projectGitHubScopedRepository(repo)
		if !valid || seen[view.ID] {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		seen[view.ID] = true
		if scope.AllowsRepository(view.ID, view.FullName) {
			items = append(items, view)
		}
	}
	return catalogPageProjection(request, input.Cursor, items, next)
}

// discoverPersonalGitHubRepositories runs the same fixed authenticated discovery through an owner-pinned verification job, never a browser-supplied token or endpoint.
// discoverPersonalGitHubRepositories выполняет тот же фиксированный authenticated discovery через owner-pinned verification job без browser token либо endpoint.
func (adapter *Adapter) discoverPersonalGitHubRepositories(ctx context.Context, request Request, definition integrationpackage.Package, configuration map[string]string) (*integrationpackage.GitHubDiscoveryPage, error) {
	capability, exists := definition.Capability("github.repository.list")
	if !exists {
		return nil, &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
	client, cleanup, err := adapter.githubClient(ctx, request.Credential)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	identity, err := readGitHubScopedIdentity(ctx, client, capability)
	if err != nil {
		return nil, err
	}
	_, err = integrationpackage.ParseGitHubResourceScope(configuration)
	if err != nil {
		return nil, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	repos, next, err := listGitHubAuthenticatedRepositories(ctx, client, capability, request.GitHubDiscoveryPage, 100)
	if err != nil {
		return nil, err
	}
	page := &integrationpackage.GitHubDiscoveryPage{IdentityID: identity.ID, Login: identity.Login, Page: request.GitHubDiscoveryPage, NextPage: next, Items: []integrationpackage.GitHubDiscoveredRepository{}}
	for _, repo := range repos {
		view, valid := projectGitHubScopedRepository(repo)
		if !valid {
			return nil, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		page.Items = append(page.Items, view)
	}
	if !page.Valid() {
		return nil, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return page, nil
}
