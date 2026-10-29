package integration

import (
	"context"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/google/go-github/v74/github"
)

// requireGitHubAutonomousContentBranch checks fresh repository and branch metadata before an autonomous file write; unknown, default, protected, or ambiguous refs cannot bypass the separate merge approval.
// requireGitHubAutonomousContentBranch проверяет свежие metadata репозитория и ветки до автономной записи файла; неизвестные, основные, защищённые и неоднозначные refs не обходят отдельное согласование слияния.
func requireGitHubAutonomousContentBranch(ctx context.Context, client *github.Client, owner, repo, branch string, capability integrationpackage.Capability) error {
	if branch == "" || strings.TrimSpace(branch) != branch || strings.HasPrefix(branch, "refs/") {
		return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	repository, err := githubRead(ctx, capability, func() (*github.Repository, *github.Response, error) {
		return client.Repositories.Get(ctx, owner, repo)
	})
	if err != nil {
		return err
	}
	if repository == nil || repository.GetDefaultBranch() == "" || repository.GetDefaultBranch() == branch {
		return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	target, err := githubRead(ctx, capability, func() (*github.Branch, *github.Response, error) {
		return client.Repositories.GetBranch(ctx, owner, repo, branch, 0)
	})
	if err != nil {
		return err
	}
	if target == nil || target.GetName() != branch || target.Protected == nil || target.GetProtected() {
		return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	return nil
}
