package service

import (
	"context"
	"errors"
	"strings"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
)

const (
	systemImproverRoleName       = "improver"
	systemMatterCodexProjectSlug = "agents"
	systemMatterCodexRoleName    = "mattercodex-admin"
	systemMatterCodexChatSlug    = "agents-control"
	systemProjectRunsChannelSlug = "runs"
)

func (svc *SlashCommandService) BootstrapSystemAgentRoles(ctx context.Context) error {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return nil
	}
	manageOpenAIAccountName, err := svc.preferredManageOpenAIAccount(ctx)
	if err != nil {
		return err
	}
	mainOpenAIAccountName, err := svc.preferredMainOpenAIAccount(ctx)
	if err != nil {
		return err
	}
	gitHubAccounts, err := svc.cfg.Store.ListGitHubAccounts(ctx, 100)
	if err != nil {
		return err
	}
	projects, err := svc.cfg.Store.ListProjects(ctx, 100)
	if err != nil {
		return err
	}
	for _, project := range projects {
		if _, err := svc.ensureProjectRunsChannel(ctx, project); err != nil {
			return err
		}
		if err := svc.bootstrapImproverRole(ctx, project, gitHubAccounts, manageOpenAIAccountName); err != nil {
			return err
		}
		if err := svc.reconcileProjectRoleBotIdentities(ctx, project); err != nil {
			return err
		}
	}
	matterCodexProject, err := svc.bootstrapMatterCodexAdmin(ctx, gitHubAccounts, mainOpenAIAccountName)
	if err != nil {
		return err
	}
	if _, err := svc.ensureProjectRunsChannel(ctx, matterCodexProject); err != nil {
		return err
	}
	if err := svc.bootstrapImproverRole(ctx, matterCodexProject, gitHubAccounts, manageOpenAIAccountName); err != nil {
		return err
	}
	return svc.reconcileProjectRoleBotIdentities(ctx, matterCodexProject)
}

func (svc *SlashCommandService) ensureProjectRunsChannel(ctx context.Context, project entity.Project) (entity.Project, error) {
	if svc.cfg.ChannelManager == nil || svc.cfg.Store == nil {
		return project, nil
	}
	channel, _, err := svc.cfg.ChannelManager.EnsureProjectChannel(ctx, project.Slug, systemProjectRunsChannelSlug, "Runs", false, nil)
	if err != nil {
		return entity.Project{}, err
	}
	if strings.TrimSpace(channel.ID) == strings.TrimSpace(project.MattermostRunsChannelID) {
		return project, nil
	}
	return svc.cfg.Store.UpdateProjectRunsChannel(ctx, project.ID, channel.ID)
}

func (svc *SlashCommandService) bootstrapImproverRole(ctx context.Context, project entity.Project, gitHubAccounts []entity.GitHubAccount, openAIAccountName string) error {
	githubAccountName := preferredProjectGitHubAccount(project, gitHubAccounts)
	role, err := svc.systemRoleByName(ctx, project.ID, systemImproverRoleName)
	if err != nil {
		return err
	}
	if role.ID == 0 {
		promptTemplate, err := promptSeedMarkdownForProfileTemplate(systemImproverRoleName, improverFeedbackTaskKey)
		if err != nil {
			return err
		}
		role, _, err = svc.cfg.Store.UpsertAgentRole(ctx, adminrepo.UpsertAgentRoleInput{
			ProjectID:         project.ID,
			Name:              systemImproverRoleName,
			RoleType:          "improver",
			Description:       "Collects repeated PR/review feedback and improves durable project instructions.",
			PromptTemplate:    promptTemplate,
			PromptMode:        "template",
			GitHubAccountName: githubAccountName,
			OpenAIAccountName: openAIAccountName,
			KubernetesAccess:  "read-only",
			SandboxMode:       "danger-full-access",
			AdvancedSettings:  "{}",
			Enabled:           true,
			BotIdentity:       systemImproverRoleName,
		})
		if err != nil {
			return err
		}
	}
	if err := svc.ensureSystemRoleBotIdentity(ctx, project, role, ""); err != nil {
		return err
	}
	return svc.ensureRoleInProjectChats(ctx, project, role)
}

func (svc *SlashCommandService) bootstrapMatterCodexAdmin(ctx context.Context, gitHubAccounts []entity.GitHubAccount, openAIAccountName string) (entity.Project, error) {
	githubAccountName := preferredOwnerGitHubAccount(gitHubAccounts)
	teamID := ""
	if svc.cfg.ChannelManager != nil {
		team, _, err := svc.cfg.ChannelManager.EnsureProjectTeam(ctx, systemMatterCodexProjectSlug, "MatterCodex", "")
		if err != nil {
			return entity.Project{}, err
		}
		teamID = team.ID
	}
	project, err := svc.cfg.Store.GetProjectBySlug(ctx, systemMatterCodexProjectSlug)
	if err != nil {
		if !errors.Is(err, adminrepo.ErrNotFound) {
			return entity.Project{}, err
		}
		project, _, err = svc.cfg.Store.UpsertProject(ctx, adminrepo.UpsertProjectInput{
			Name:              "MatterCodex",
			Slug:              systemMatterCodexProjectSlug,
			MattermostTeamID:  teamID,
			GitHubAccountName: githubAccountName,
			GitHubOwner:       "codex-k8s",
			GitHubOwnerType:   "organization",
			Description:       "MatterCodex control project bound to the agents Mattermost team.",
			AdvancedSettings:  "{}",
		})
		if err != nil {
			return entity.Project{}, err
		}
	}
	repo, _, err := svc.cfg.Store.UpsertRepository(ctx, adminrepo.UpsertRepositoryInput{
		Provider:          "github",
		Owner:             "codex-k8s",
		Name:              "matter-codex",
		DefaultBranch:     "main",
		GitHubAccountName: githubAccountName,
	})
	if err != nil {
		return entity.Project{}, err
	}
	projectRepo, _, err := svc.cfg.Store.UpsertProjectRepository(ctx, adminrepo.UpsertProjectRepositoryInput{
		ProjectID:    project.ID,
		RepositoryID: repo.ID,
		IsDefault:    true,
	})
	if err != nil {
		return entity.Project{}, err
	}
	role, err := svc.systemRoleByName(ctx, project.ID, systemMatterCodexRoleName)
	if err != nil {
		return entity.Project{}, err
	}
	if role.ID == 0 {
		promptTemplate, err := promptSeedMarkdownForProfileTemplate(systemMatterCodexRoleName, matterCodexAdminTaskTemplateKey)
		if err != nil {
			return entity.Project{}, err
		}
		role, _, err = svc.cfg.Store.UpsertAgentRole(ctx, adminrepo.UpsertAgentRoleInput{
			ProjectID:         project.ID,
			Name:              systemMatterCodexRoleName,
			RoleType:          "sre",
			Description:       "Owner-level MatterCodex administration agent with explicit cluster-admin access.",
			PromptTemplate:    promptTemplate,
			PromptMode:        "template",
			GitHubAccountName: githubAccountName,
			OpenAIAccountName: openAIAccountName,
			KubernetesAccess:  "cluster-admin",
			SandboxMode:       "danger-full-access",
			AdvancedSettings:  "{}",
			Enabled:           true,
			BotIdentity:       systemMatterCodexRoleName,
		})
		if err != nil {
			return entity.Project{}, err
		}
	}
	channelID := ""
	if svc.cfg.ChannelManager != nil {
		channel, _, err := svc.cfg.ChannelManager.EnsureProjectChannel(ctx, project.Slug, systemMatterCodexChatSlug, "Agents Control", true, nil)
		if err != nil {
			return entity.Project{}, err
		}
		channelID = channel.ID
	}
	if err := svc.ensureSystemRoleBotIdentity(ctx, project, role, channelID); err != nil {
		return entity.Project{}, err
	}
	_, _, err = svc.cfg.Store.CreateChat(ctx, adminrepo.CreateChatInput{
		ProjectID:           project.ID,
		MattermostChannelID: channelID,
		Name:                "Agents Control",
		Slug:                systemMatterCodexChatSlug,
		Description:         "MatterCodex owner control chat.",
		ChatType:            "single_custom",
		WorkPolicy:          "owner_control",
		Settings:            "{}",
		RoleIDs:             []int64{role.ID},
		RepositoryIDs:       []int64{projectRepo.RepositoryID},
	})
	return project, err
}

func (svc *SlashCommandService) preferredManageOpenAIAccount(ctx context.Context) (string, error) {
	return svc.preferredOpenAIAccount(ctx, []string{"openai-codex-manage", "manage", "openai-codex-main", "main", "primary"})
}

func (svc *SlashCommandService) preferredMainOpenAIAccount(ctx context.Context) (string, error) {
	return svc.preferredOpenAIAccount(ctx, []string{"openai-codex-main", "main", "primary", "openai-codex-manage", "manage"})
}

func (svc *SlashCommandService) preferredOpenAIAccount(ctx context.Context, preferredNames []string) (string, error) {
	accounts, err := svc.cfg.Store.ListOpenAIAccounts(ctx, 100)
	if err != nil {
		return "", err
	}
	for _, preferred := range preferredNames {
		for _, account := range accounts {
			if account.Name == preferred && account.Status == "authorized" {
				return account.Name, nil
			}
		}
	}
	for _, account := range accounts {
		if account.Status == "authorized" {
			return account.Name, nil
		}
	}
	for _, preferred := range preferredNames {
		for _, account := range accounts {
			if account.Name == preferred {
				return account.Name, nil
			}
		}
	}
	if len(accounts) > 0 {
		return accounts[0].Name, nil
	}
	return "", nil
}

func (svc *SlashCommandService) reconcileProjectRoleBotIdentities(ctx context.Context, project entity.Project) error {
	if svc.cfg.RoleBotManager == nil || svc.cfg.RuntimeRunner == nil {
		return nil
	}
	roles, err := svc.cfg.Store.ListAgentRoles(ctx, project.ID)
	if err != nil {
		return err
	}
	for _, role := range roles {
		if !role.Enabled {
			continue
		}
		if _, err := svc.ensureRoleBotIdentity(ctx, project, role, ""); err != nil {
			return err
		}
	}
	return nil
}

func preferredProjectGitHubAccount(project entity.Project, accounts []entity.GitHubAccount) string {
	if accountExists(accounts, project.GitHubAccountName) {
		return project.GitHubAccountName
	}
	return preferredOwnerGitHubAccount(accounts)
}

func preferredOwnerGitHubAccount(accounts []entity.GitHubAccount) string {
	preferredNames := []string{"github-myqrcontact-owner", "github-radar-owner-manager", "github-radar-owner-review"}
	for _, preferred := range preferredNames {
		if accountExists(accounts, preferred) {
			return preferred
		}
	}
	for _, account := range accounts {
		if account.Status == "configured" && strings.EqualFold(account.Username, "ai-da-stas") {
			return account.Name
		}
	}
	for _, account := range accounts {
		if account.Status == "configured" && strings.Contains(account.Name, "owner") {
			return account.Name
		}
	}
	for _, account := range accounts {
		if account.Status == "configured" {
			return account.Name
		}
	}
	if len(accounts) > 0 {
		return accounts[0].Name
	}
	return ""
}

func accountExists(accounts []entity.GitHubAccount, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, account := range accounts {
		if account.Name == name {
			return true
		}
	}
	return false
}

func (svc *SlashCommandService) systemRoleByName(ctx context.Context, projectID int64, roleName string) (entity.AgentRole, error) {
	roles, err := svc.cfg.Store.ListAgentRoles(ctx, projectID)
	if err != nil {
		return entity.AgentRole{}, err
	}
	for _, role := range roles {
		if role.Name == roleName {
			return role, nil
		}
	}
	return entity.AgentRole{}, nil
}

func (svc *SlashCommandService) ensureSystemRoleBotIdentity(ctx context.Context, project entity.Project, role entity.AgentRole, channelID string) error {
	if svc.cfg.RoleBotManager == nil || svc.cfg.RuntimeRunner == nil {
		return nil
	}
	_, err := svc.ensureRoleBotIdentity(ctx, project, role, channelID)
	return err
}

func (svc *SlashCommandService) ensureRoleInProjectChats(ctx context.Context, project entity.Project, role entity.AgentRole) error {
	chats, err := svc.cfg.Store.ListChats(ctx, project.ID)
	if err != nil {
		return err
	}
	for _, chat := range chats {
		if err := svc.ensureSystemRoleBotIdentity(ctx, project, role, chat.MattermostChannelID); err != nil {
			return err
		}
		participants, err := svc.cfg.Store.ListChatParticipants(ctx, chat.ID)
		if err != nil {
			return err
		}
		roleIDs := chatParticipantRoleIDs(participants)
		if containsInt64(roleIDs, role.ID) {
			continue
		}
		roleIDs = append(roleIDs, role.ID)
		repositories, err := svc.cfg.Store.ListChatRepositories(ctx, chat.ID)
		if err != nil {
			return err
		}
		_, _, err = svc.cfg.Store.CreateChat(ctx, adminrepo.CreateChatInput{
			ProjectID:           chat.ProjectID,
			MattermostChannelID: chat.MattermostChannelID,
			Name:                chat.Name,
			Slug:                chat.Slug,
			Description:         chat.Description,
			ChatType:            chat.ChatType,
			RootGitHubIssue:     chat.RootGitHubIssue,
			WorkPolicy:          chat.WorkPolicy,
			Settings:            chat.Settings,
			RoleIDs:             roleIDs,
			RepositoryIDs:       chatRepositoryIDs(repositories),
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func chatParticipantRoleIDs(participants []entity.ChatParticipant) []int64 {
	roleIDs := make([]int64, 0, len(participants))
	for _, participant := range participants {
		if participant.RoleID > 0 && participant.Enabled {
			roleIDs = append(roleIDs, participant.RoleID)
		}
	}
	return roleIDs
}

func chatRepositoryIDs(repositories []entity.ChatRepositoryBinding) []int64 {
	ids := make([]int64, 0, len(repositories))
	for _, repository := range repositories {
		if repository.RepositoryID > 0 {
			ids = append(ids, repository.RepositoryID)
		}
	}
	return ids
}

func containsInt64(values []int64, needle int64) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
