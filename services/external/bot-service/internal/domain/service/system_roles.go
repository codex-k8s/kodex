package service

import (
	"context"
	"errors"
	"strconv"
	"strings"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	securityrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/security"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
)

const (
	systemImproverRoleName       = "improver"
	systemMatterCodexProjectSlug = "agents"
	systemMatterCodexRoleName    = "mattercodex-admin"
	systemMatterCodexChatSlug    = "agents-control"
)

func (svc *SlashCommandService) BootstrapSystemAgentRoles(ctx context.Context) error {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return nil
	}
	if _, _, err := svc.preflightMatterCodexAdminBinding(ctx, "system_role.bootstrap.preflight"); err != nil {
		return err
	}
	openAIAccountName, err := svc.preferredSystemOpenAIAccount(ctx)
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
		if err := svc.bootstrapImproverRole(ctx, project, gitHubAccounts, openAIAccountName); err != nil {
			return err
		}
	}
	matterCodexProject, err := svc.bootstrapMatterCodexAdmin(ctx, gitHubAccounts)
	if err != nil {
		return err
	}
	return svc.bootstrapImproverRole(ctx, matterCodexProject, gitHubAccounts, openAIAccountName)
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

func (svc *SlashCommandService) bootstrapMatterCodexAdmin(ctx context.Context, gitHubAccounts []entity.GitHubAccount) (entity.Project, error) {
	githubAccountName := preferredOwnerGitHubAccount(gitHubAccounts)
	project, role, err := svc.preflightMatterCodexAdminBinding(ctx, "system_role.bootstrap")
	if err != nil {
		return entity.Project{}, err
	}
	if svc.cfg.ChannelManager != nil {
		_, _, err := svc.cfg.ChannelManager.EnsureProjectTeam(ctx, systemMatterCodexProjectSlug, "MatterCodex", "")
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

func (svc *SlashCommandService) preflightMatterCodexAdminBinding(ctx context.Context, operation string) (entity.Project, entity.AgentRole, error) {
	project, err := svc.cfg.Store.GetProjectBySlug(ctx, systemMatterCodexProjectSlug)
	if err != nil {
		if errors.Is(err, adminrepo.ErrNotFound) {
			return entity.Project{}, entity.AgentRole{}, adminrepo.ErrClusterAdminAdmissionDenied
		}
		return entity.Project{}, entity.AgentRole{}, err
	}
	role, err := svc.systemRoleByName(ctx, project.ID, systemMatterCodexRoleName)
	if err != nil {
		return entity.Project{}, entity.AgentRole{}, err
	}
	if role.ID == 0 {
		return entity.Project{}, entity.AgentRole{}, adminrepo.ErrClusterAdminAdmissionDenied
	}
	if err := svc.admitClusterAdminRoleBinding(ctx, role, 0, systemMatterCodexChatSlug, "", "bootstrap", operation); err != nil {
		return entity.Project{}, entity.AgentRole{}, err
	}
	return project, role, nil
}

func (svc *SlashCommandService) admitClusterAdminRoleBinding(ctx context.Context, role entity.AgentRole, chatID int64, chatSlug string, actorUserID string, actorUser string, operation string) error {
	if !strings.EqualFold(strings.TrimSpace(role.KubernetesAccess), "cluster-admin") {
		return nil
	}
	repository, ok := svc.cfg.Store.(securityrepo.Repository)
	if !ok {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	allowed, err := repository.AdmitExistingClusterAdmin(ctx, securityrepo.ClusterAdminAdmissionInput{
		SubjectType: "agent_role", SubjectKey: strconv.FormatInt(role.ID, 10), ProjectID: role.ProjectID,
		ProfileName: role.Name, ActorUserID: actorUserID, ActorUser: actorUser, Operation: operation,
	})
	if err != nil {
		return err
	}
	if !allowed {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	bindings, ok := svc.cfg.Store.(securityrepo.ClusterAdminBindingRepository)
	if !ok {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	allowed, err = bindings.AdmitExistingClusterAdminBinding(ctx, securityrepo.ClusterAdminBindingInput{
		RoleID: role.ID, ProjectID: role.ProjectID, ChatID: chatID, ChatSlug: chatSlug,
		ActorUserID: actorUserID, ActorUser: actorUser, Operation: operation,
	})
	if err != nil {
		return err
	}
	if !allowed {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	return nil
}

func (svc *SlashCommandService) preferredSystemOpenAIAccount(ctx context.Context) (string, error) {
	accounts, err := svc.cfg.Store.ListOpenAIAccounts(ctx, 100)
	if err != nil {
		return "", err
	}
	preferredNames := []string{"openai-radar-delivery", "main", "primary", "openai-radar-ops-review"}
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
		if err := svc.admitClusterAdminRoleBinding(ctx, role, chat.ID, chat.Slug, "", "bootstrap", "system_role.bind_chat"); err != nil {
			return err
		}
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
