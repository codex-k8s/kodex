package admin

import (
	"context"
	"fmt"
	"strings"
)

func (repo *Repository) IsFrozenClusterAdminOpenAIAccount(ctx context.Context, accountName string) (bool, error) {
	return repo.isFrozenClusterAdminAccount(ctx, "openai", accountName)
}

func (repo *Repository) IsFrozenClusterAdminGitHubAccount(ctx context.Context, accountName string) (bool, error) {
	return repo.isFrozenClusterAdminAccount(ctx, "github", accountName)
}

func (repo *Repository) isFrozenClusterAdminAccount(ctx context.Context, provider string, accountName string) (bool, error) {
	accountName = strings.TrimSpace(accountName)
	if accountName == "" {
		return false, nil
	}
	var frozen bool
	err := repo.db.QueryRow(ctx, `
select exists (
	select 1
	from matter_codex_cluster_admin_subjects subject
	where case $1
		when 'openai' then subject.privilege_state ->> 'openai_account_name' = $2
		when 'github' then (
			subject.privilege_state ->> 'github_account_name' = $2
			or subject.privilege_state ->> 'project_github_account_name' = $2
		)
		else false
	end
)
`, provider, accountName).Scan(&frozen)
	if err != nil {
		return false, fmt.Errorf("check frozen cluster-admin account dependency: %w", err)
	}
	return frozen, nil
}
