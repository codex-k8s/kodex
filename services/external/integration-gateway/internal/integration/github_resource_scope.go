package integration

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/google/go-github/v74/github"
)

// githubScopedIdentity is a safe authenticated identity projection without profile PII or token metadata.
// githubScopedIdentity — безопасная проекция аутентифицированной identity без персональных данных профиля и metadata ключа.
type githubScopedIdentity struct {
	ID    int64  `json:"identity_id"`
	Login string `json:"login"`
}

// readGitHubScopedIdentity obtains identity from the authenticated endpoint; a repository owner or payload user is never treated as the initiator.
// readGitHubScopedIdentity получает identity из аутентифицированного endpoint; владелец репозитория либо пользователь payload не становится инициатором.
func readGitHubScopedIdentity(ctx context.Context, client *github.Client, capability integrationpackage.Capability) (githubScopedIdentity, error) {
	user, err := githubRead(ctx, capability, func() (*github.User, *github.Response, error) { return client.Users.Get(ctx, "") })
	if err != nil {
		return githubScopedIdentity{}, err
	}
	identity := githubScopedIdentity{ID: user.GetID(), Login: user.GetLogin()}
	if identity.ID < 1 || identity.ID > 9007199254740991 || !integrationpackage.ValidGitHubRepositoryName(identity.Login+"/identity") {
		return githubScopedIdentity{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return identity, nil
}

// githubScopedRepository projects only stable repository coordinates and observed repository access; none of these observations are reusable authorization.
// githubScopedRepository проецирует лишь устойчивые координаты репозитория и наблюдаемый доступ; эти наблюдения не являются многоразовыми полномочиями.
type githubScopedRepository = integrationpackage.GitHubDiscoveredRepository

// projectGitHubScopedRepository requires canonical coordinates, an exact owner relationship and explicit read permission from authenticated provider metadata.
// projectGitHubScopedRepository требует канонических координат, точного соответствия владельца и явного права чтения аутентифицированных metadata провайдера.
func projectGitHubScopedRepository(repo *github.Repository) (githubScopedRepository, bool) {
	if repo == nil || repo.GetID() < 1 || repo.GetID() > 9007199254740991 || !integrationpackage.ValidGitHubRepositoryName(repo.GetFullName()) || repo.GetOwner() == nil || repo.GetOwner().GetID() < 1 || repo.GetOwner().GetLogin()+"/"+repo.GetName() != repo.GetFullName() || repo.GetPermissions() == nil || !repo.GetPermissions()["pull"] {
		return githubScopedRepository{}, false
	}
	kind := repo.GetOwner().GetType()
	if kind != "User" && kind != "Organization" {
		return githubScopedRepository{}, false
	}
	return githubScopedRepository{ID: repo.GetID(), FullName: repo.GetFullName(), OwnerKind: kind, Private: repo.GetPrivate(), Archived: repo.GetArchived(), CanRead: true, CanPush: repo.GetPermissions()["push"]}, true
}

// listGitHubAuthenticatedRepositories uses only the fixed authenticated listing and numeric pagination; provider Link URLs are never followed.
// listGitHubAuthenticatedRepositories использует лишь фиксированный аутентифицированный список и числовые страницы; URL provider Link не открываются.
func listGitHubAuthenticatedRepositories(ctx context.Context, client *github.Client, capability integrationpackage.Capability, page, limit int) ([]*github.Repository, int, error) {
	var response *github.Response
	repos, err := githubRead(ctx, capability, func() ([]*github.Repository, *github.Response, error) {
		items, result, callErr := client.Repositories.ListByAuthenticatedUser(ctx, &github.RepositoryListByAuthenticatedUserOptions{Affiliation: "owner,collaborator,organization_member", Visibility: "all", Sort: "full_name", Direction: "asc", ListOptions: github.ListOptions{Page: page, PerPage: limit}})
		response = result
		return items, result, callErr
	})
	if err != nil {
		return nil, 0, err
	}
	if len(repos) > limit || response == nil || response.NextPage != 0 && (response.NextPage != page+1 || response.NextPage > 10000) {
		return nil, 0, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return repos, response.NextPage, nil
}

// requireGitHubAuthenticatedMembership freshly finds the exact repository in explicit user access, excluding arbitrary public repositories outside /user/repos.
// requireGitHubAuthenticatedMembership заново находит точный репозиторий среди явного доступа пользователя и исключает произвольные публичные репозитории вне /user/repos.
// The finite scan closes on overflow or transport failure; discovery cache and caller IDs are never used as permission evidence.
// Конечный обход закрывается при переполнении либо ошибке transport; discovery cache и ID caller не используются как доказательство прав.
func requireGitHubAuthenticatedMembership(ctx context.Context, client *github.Client, capability integrationpackage.Capability, id int64, name string) error {
	for page := 1; page <= 100; page++ {
		repos, next, err := listGitHubAuthenticatedRepositories(ctx, client, capability, page, 100)
		if err != nil {
			return err
		}
		for _, repo := range repos {
			view, valid := projectGitHubScopedRepository(repo)
			if !valid {
				return &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
			}
			if view.ID == id && view.FullName == name {
				return nil
			}
		}
		if next == 0 {
			break
		}
	}
	return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
}

// resolveGitHubScopedRepository binds typed selectors to fresh authenticated identity, current explicit membership and canonical repository metadata before dispatch.
// resolveGitHubScopedRepository связывает typed selectors с текущими аутентифицированной identity, явным доступом и каноническими metadata репозитория до исполнения.
// Selectors identify the intended resource but cannot establish authority; existing typed handlers receive private derived copies and unchanged receipt pins.
// Селекторы обозначают целевой ресурс, но не доказывают полномочий; прежние typed handlers получают отдельные производные копии с неизменными receipt pins.
func resolveGitHubScopedRepository(ctx context.Context, client *github.Client, request Request, capability integrationpackage.Capability, configuration map[string]string, raw []byte) (map[string]string, []byte, error) {
	scope, err := integrationpackage.ParseGitHubResourceScope(configuration)
	if err != nil {
		return nil, nil, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	var input map[string]json.RawMessage
	var name string
	var id int64
	if json.Unmarshal(raw, &input) != nil || json.Unmarshal(input["repository_full_name"], &name) != nil || json.Unmarshal(input["repository_id"], &id) != nil || !scope.AllowsRepository(id, name) {
		return nil, nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	identity, err := readGitHubScopedIdentity(ctx, client, capability)
	if err != nil {
		return nil, nil, err
	}
	if !request.PersonalCredentialAdmission && configuration["service_identity"] != "" && configuration["service_identity"] != identity.Login {
		return nil, nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}

	if err := requireGitHubAuthenticatedMembership(ctx, client, capability, id, name); err != nil {
		return nil, nil, err
	}
	parts := strings.Split(name, "/")
	repo, err := githubRead(ctx, capability, func() (*github.Repository, *github.Response, error) {
		return client.Repositories.Get(ctx, parts[0], parts[1])
	})
	if err != nil {
		return nil, nil, err
	}
	observed, valid := projectGitHubScopedRepository(repo)
	if !valid || observed.ID != id || observed.FullName != name {
		return nil, nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	if !gitHubScopedOperationAccess(capability, observed) {
		return nil, nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	if request.PersonalCredentialAdmission && request.ResourcePermissionProfile != integrationpackage.CredentialResourcePermissionGitHubScopedV1 {
		return nil, nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	selected := make(map[string]string, len(configuration)+2)
	for key, value := range configuration {
		selected[key] = value
	}
	selected["owner"] = parts[0]
	selected["repository"] = parts[1]
	delete(input, "repository_id")
	delete(input, "repository_full_name")
	providerInput, err := json.Marshal(input)
	if err != nil {
		return nil, nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	return selected, providerInput, nil
}

// gitHubScopedOperationAccess separates collaboration endpoints, whose author/triage permissions are enforced by their exact GitHub API, from code/branch/merge/actions endpoints requiring repository push. Unknown operations or mismatched risks never inherit a write profile.
// gitHubScopedOperationAccess отделяет collaboration endpoints с author/triage проверкой точным GitHub API от code/branch/merge/actions, требующих repository push. Неизвестные операции либо неверные risk не наследуют write profile.
func gitHubScopedOperationAccess(capability integrationpackage.Capability, repository githubScopedRepository) bool {
	contract, known := integrationpackage.CredentialResourcePermissionContractForProfile("github", capability.Operation, integrationpackage.CredentialResourcePermissionGitHubScopedV1)
	if !known || contract.Risk != capability.Risk || !repository.CanRead {
		return false
	}
	if capability.Risk == "READ" {
		return true
	}
	if repository.Archived {
		return false
	}
	switch capability.Operation {
	case "github.issue.create", "github.issue.update", "github.issue.comment.create", "github.issue.comment.update", "github.issue.comment.delete", "github.pull_request.create", "github.pull_request.update", "github.pull_request.review.create", "github.pull_request.comment.create", "github.pull_request.comment.update", "github.pull_request.ready":
		return true
	case "github.repository.bundle.publish", "github.repository.content.create", "github.repository.content.update", "github.repository.content.delete", "github.branch.create", "github.branch.delete", "github.pull_request.merge", "github.actions.workflow.dispatch", "github.actions.run.rerun", "github.actions.run.cancel":
		return repository.CanPush
	default:
		return false
	}
}
