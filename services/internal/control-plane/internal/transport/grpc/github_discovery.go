package grpc

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// castGitHubDiscovery projects only a validated safe page; persisted coordinates never become an execution grant.
// castGitHubDiscovery проецирует лишь проверенную безопасную страницу; хранимые координаты не становятся execution grant.
func castGitHubDiscovery(page *integrationpackage.GitHubDiscoveryPage) *cp.GitHubRepositoryDiscoveryPage {
	if page == nil || !page.Valid() {
		return nil
	}
	result := &cp.GitHubRepositoryDiscoveryPage{IdentityId: page.IdentityID, Login: page.Login, Page: int32(page.Page), NextPage: int32(page.NextPage), Items: []*cp.GitHubDiscoveredRepository{}}
	for _, repo := range page.Items {
		result.Items = append(result.Items, &cp.GitHubDiscoveredRepository{Id: repo.ID, FullName: repo.FullName, OwnerKind: repo.OwnerKind, Private: repo.Private, Archived: repo.Archived, CanRead: repo.CanRead, CanPush: repo.CanPush})
	}
	return result
}

// parseGitHubDiscovery rejects unknown wire fields or malformed safe projections before owner completion.
// parseGitHubDiscovery отклоняет неизвестные wire fields либо неверные безопасные проекции до owner completion.
func parseGitHubDiscovery(page *cp.GitHubRepositoryDiscoveryPage) (*integrationpackage.GitHubDiscoveryPage, bool) {
	if page == nil {
		return nil, true
	}
	if len(page.ProtoReflect().GetUnknown()) != 0 || len(page.Items) > 100 {
		return nil, false
	}
	result := &integrationpackage.GitHubDiscoveryPage{IdentityID: page.GetIdentityId(), Login: page.GetLogin(), Page: int(page.GetPage()), NextPage: int(page.GetNextPage()), Items: []integrationpackage.GitHubDiscoveredRepository{}}
	for _, repo := range page.Items {
		if repo == nil || len(repo.ProtoReflect().GetUnknown()) != 0 {
			return nil, false
		}
		result.Items = append(result.Items, integrationpackage.GitHubDiscoveredRepository{ID: repo.GetId(), FullName: repo.GetFullName(), OwnerKind: repo.GetOwnerKind(), Private: repo.GetPrivate(), Archived: repo.GetArchived(), CanRead: repo.GetCanRead(), CanPush: repo.GetCanPush()})
	}
	return result, result.Valid()
}
