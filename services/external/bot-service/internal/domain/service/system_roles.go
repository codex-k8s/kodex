package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	securityrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/security"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
)

const (
	systemImproverRoleName        = "improver"
	systemDirectorRoleName        = "director"
	systemMatterCodexProjectSlug  = "agents"
	systemMatterCodexRoleName     = "mattercodex-admin"
	systemMatterCodexChatSlug     = "agents-control"
	systemProjectRunsChannelSlug  = "runs"
	systemCoordinationChannelSlug = "coordination"
	coordinationModeManagerOnly   = "manager-only"
)

func (svc *SlashCommandService) BootstrapSystemAgentRoles(ctx context.Context) error {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return nil
	}
	manageOpenAIAccountName, err := svc.preferredManageOpenAIAccount(ctx)
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
		if projectCoordinationMode(project) == coordinationModeManagerOnly {
			if err := svc.bootstrapManagerCoordinationRole(ctx, project); err != nil {
				return err
			}
		} else {
			if err := svc.bootstrapDirectorRole(ctx, project, gitHubAccounts, manageOpenAIAccountName); err != nil {
				return err
			}
		}
		if err := svc.reconcileProjectControlBotMemberships(ctx, project); err != nil {
			return err
		}
		if err := svc.reconcileProjectRoleBotIdentities(ctx, project); err != nil {
			return err
		}
	}
	matterCodexProject, err := svc.bootstrapMatterCodexAdmin(ctx, gitHubAccounts)
	if err != nil {
		if errors.Is(err, adminrepo.ErrClusterAdminAdmissionDenied) {
			return nil
		}
		return err
	}
	if _, err := svc.ensureProjectRunsChannel(ctx, matterCodexProject); err != nil {
		return err
	}
	if err := svc.bootstrapImproverRole(ctx, matterCodexProject, gitHubAccounts, manageOpenAIAccountName); err != nil {
		return err
	}
	if projectCoordinationMode(matterCodexProject) == coordinationModeManagerOnly {
		if err := svc.bootstrapManagerCoordinationRole(ctx, matterCodexProject); err != nil {
			return err
		}
	} else {
		if err := svc.bootstrapDirectorRole(ctx, matterCodexProject, gitHubAccounts, manageOpenAIAccountName); err != nil {
			return err
		}
	}
	if err := svc.reconcileProjectControlBotMemberships(ctx, matterCodexProject); err != nil {
		return err
	}
	return svc.reconcileProjectRoleBotIdentities(ctx, matterCodexProject)
}

func projectCoordinationMode(project entity.Project) string {
	var settings struct {
		CoordinationMode string `json:"coordination_mode"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(project.AdvancedSettings)), &settings); err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(settings.CoordinationMode))
}

func (svc *SlashCommandService) ensureProjectRunsChannel(ctx context.Context, project entity.Project) (entity.Project, error) {
	if svc.cfg.ChannelManager == nil || svc.cfg.Store == nil {
		return project, nil
	}
	channel, _, err := svc.cfg.ChannelManager.EnsureProjectChannel(ctx, project.Slug, systemProjectRunsChannelSlug, svc.t("system.channel.runs.name", nil), false, nil)
	if err != nil {
		return entity.Project{}, err
	}
	if strings.TrimSpace(channel.ID) == strings.TrimSpace(project.MattermostRunsChannelID) {
		return project, nil
	}
	return svc.cfg.Store.UpdateProjectRunsChannel(ctx, project.ID, channel.ID)
}

func (svc *SlashCommandService) bootstrapDirectorRole(ctx context.Context, project entity.Project, gitHubAccounts []entity.GitHubAccount, openAIAccountName string) error {
	role, err := svc.systemRoleByTypeOrName(ctx, project.ID, "director", systemDirectorRoleName)
	if err != nil {
		return err
	}
	if role.ID == 0 {
		promptTemplate, err := promptSeedMarkdownForProfileTemplate(systemDirectorRoleName, directorCoordinatePortfolioKey)
		if err != nil {
			return err
		}
		role, _, err = svc.cfg.Store.UpsertAgentRole(ctx, adminrepo.UpsertAgentRoleInput{
			ProjectID:         project.ID,
			Name:              systemDirectorRoleName,
			RoleType:          "director",
			Description:       svc.t("system.role.director.description", nil),
			PromptTemplate:    promptTemplate,
			PromptMode:        "template",
			GitHubAccountName: preferredProjectGitHubAccount(project, gitHubAccounts),
			OpenAIAccountName: openAIAccountName,
			KubernetesAccess:  "read-only",
			SandboxMode:       "danger-full-access",
			AdvancedSettings:  "{}",
			Enabled:           true,
			BotIdentity:       systemDirectorRoleName,
		})
		if err != nil {
			return err
		}
	}
	channelID := ""
	if svc.cfg.ChannelManager != nil {
		memberUserIDs := make([]string, 0, 1)
		if ownerUsername := strings.TrimSpace(svc.cfg.OwnerMattermostUsername); ownerUsername != "" {
			ownerUserID, err := svc.cfg.ChannelManager.ResolveMattermostUserID(ctx, ownerUsername)
			if err != nil {
				return err
			}
			memberUserIDs = append(memberUserIDs, ownerUserID)
		}
		channel, _, err := svc.cfg.ChannelManager.EnsureProjectChannel(ctx, project.Slug, systemCoordinationChannelSlug, svc.t("system.channel.coordination.name", nil), true, memberUserIDs)
		if err != nil {
			return err
		}
		channelID = channel.ID
	}
	if err := svc.ensureSystemRoleBotIdentity(ctx, project, role, channelID); err != nil {
		return err
	}
	_, _, err = svc.cfg.Store.CreateChat(ctx, adminrepo.CreateChatInput{
		ProjectID:           project.ID,
		MattermostChannelID: channelID,
		Name:                svc.t("system.channel.coordination.name", nil),
		Slug:                systemCoordinationChannelSlug,
		Description:         svc.t("system.channel.coordination.description", nil),
		ChatType:            "coordination",
		WorkPolicy:          "top_level_coordination",
		Settings:            "{}",
		SystemPurpose:       "coordination",
		RoleIDs:             []int64{role.ID},
	})
	if err != nil {
		return err
	}
	policyStore, ok := svc.cfg.Store.(adminrepo.CoordinationPolicyPresetRepository)
	if !ok {
		return nil
	}
	roles, err := svc.cfg.Store.ListAgentRoles(ctx, project.ID)
	if err != nil {
		return err
	}
	waveCoordinators := make([]int64, 0)
	for _, candidate := range roles {
		if candidate.Enabled && candidate.ID != role.ID && (strings.EqualFold(candidate.RoleType, "manager") || strings.EqualFold(candidate.RoleType, "coordinator") || strings.EqualFold(candidate.Name, "manager")) {
			waveCoordinators = append(waveCoordinators, candidate.ID)
		}
	}
	return policyStore.ApplyCoordinationPolicyPreset(ctx, project.ID, role.ID, waveCoordinators)
}

func (svc *SlashCommandService) bootstrapManagerCoordinationRole(ctx context.Context, project entity.Project) error {
	role, err := svc.systemRoleByTypeOrName(ctx, project.ID, "manager", "manager")
	if err != nil {
		return err
	}
	if role.ID == 0 || !role.Enabled {
		return nil
	}
	channelID := ""
	if svc.cfg.ChannelManager != nil {
		memberUserIDs := make([]string, 0, 1)
		if ownerUsername := strings.TrimSpace(svc.cfg.OwnerMattermostUsername); ownerUsername != "" {
			ownerUserID, err := svc.cfg.ChannelManager.ResolveMattermostUserID(ctx, ownerUsername)
			if err != nil {
				return err
			}
			memberUserIDs = append(memberUserIDs, ownerUserID)
		}
		channel, _, err := svc.cfg.ChannelManager.EnsureProjectChannel(
			ctx,
			project.Slug,
			systemCoordinationChannelSlug,
			svc.t("system.channel.coordination.name", nil),
			true,
			memberUserIDs,
		)
		if err != nil {
			return err
		}
		channelID = channel.ID
	}
	if err := svc.ensureSystemRoleBotIdentity(ctx, project, role, channelID); err != nil {
		return err
	}
	_, _, err = svc.cfg.Store.CreateChat(ctx, adminrepo.CreateChatInput{
		ProjectID:           project.ID,
		MattermostChannelID: channelID,
		Name:                svc.t("system.channel.coordination.name", nil),
		Slug:                systemCoordinationChannelSlug,
		Description:         svc.t("system.channel.coordination.description", nil),
		ChatType:            "coordination",
		WorkPolicy:          "manager_unit_coordination",
		Settings:            "{}",
		SystemPurpose:       "coordination",
		RoleIDs:             []int64{role.ID},
	})
	if err != nil {
		return err
	}
	policyStore, ok := svc.cfg.Store.(adminrepo.CoordinationPolicyPresetRepository)
	if !ok {
		return nil
	}
	return policyStore.ApplyManagerCoordinationPolicyPreset(ctx, project.ID, role.ID)
}

func (svc *SlashCommandService) systemRoleByTypeOrName(ctx context.Context, projectID int64, roleType string, roleName string) (entity.AgentRole, error) {
	roles, err := svc.cfg.Store.ListAgentRoles(ctx, projectID)
	if err != nil {
		return entity.AgentRole{}, err
	}
	for _, role := range roles {
		if strings.EqualFold(strings.TrimSpace(role.RoleType), strings.TrimSpace(roleType)) {
			return role, nil
		}
	}
	for _, role := range roles {
		if strings.EqualFold(strings.TrimSpace(role.Name), strings.TrimSpace(roleName)) {
			return role, nil
		}
	}
	return entity.AgentRole{}, nil
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

func (svc *SlashCommandService) bootstrapMatterCodexAdmin(ctx context.Context, _ []entity.GitHubAccount) (entity.Project, error) {
	project, role, chat, err := svc.preflightMatterCodexAdminBinding(ctx, "system_role.bootstrap")
	if err != nil {
		return entity.Project{}, err
	}
	repo, err := svc.cfg.Store.GetRepository(ctx, "github", "codex-k8s", "matter-codex")
	if err != nil {
		return entity.Project{}, adminrepo.ErrClusterAdminAdmissionDenied
	}
	projectRepositories, err := svc.cfg.Store.ListProjectRepositories(ctx, project.ID)
	if err != nil {
		return entity.Project{}, err
	}
	repositoryBound := false
	for _, binding := range projectRepositories {
		if binding.RepositoryID == repo.ID && binding.IsDefault {
			repositoryBound = true
			break
		}
	}
	if !repositoryBound {
		return entity.Project{}, adminrepo.ErrClusterAdminAdmissionDenied
	}
	if svc.cfg.ChannelManager != nil {
		err := svc.withClusterAdminRoleBindingGuard(ctx, role, chat.ID, chat.Slug, chat.MattermostChannelID, "", "", "bootstrap", "system_role.ensure_team.side_effect", func() error {
			_, _, ensureErr := svc.cfg.ChannelManager.EnsureProjectTeam(ctx, systemMatterCodexProjectSlug, "MatterCodex", "")
			return ensureErr
		})
		if err != nil {
			return entity.Project{}, err
		}
	}
	channelID := chat.MattermostChannelID
	if err := svc.withClusterAdminRoleBindingGuard(ctx, role, chat.ID, chat.Slug, channelID, "", "", "bootstrap", "system_role.bot_identity.side_effect", func() error {
		return svc.ensureSystemRoleBotIdentity(ctx, project, role, channelID)
	}); err != nil {
		return entity.Project{}, err
	}
	return project, nil
}

func (svc *SlashCommandService) preflightMatterCodexAdminBinding(ctx context.Context, operation string) (entity.Project, entity.AgentRole, entity.Chat, error) {
	project, err := svc.cfg.Store.GetProjectBySlug(ctx, systemMatterCodexProjectSlug)
	if err != nil {
		if errors.Is(err, adminrepo.ErrNotFound) {
			return entity.Project{}, entity.AgentRole{}, entity.Chat{}, adminrepo.ErrClusterAdminAdmissionDenied
		}
		return entity.Project{}, entity.AgentRole{}, entity.Chat{}, err
	}
	role, err := svc.systemRoleByName(ctx, project.ID, systemMatterCodexRoleName)
	if err != nil {
		return entity.Project{}, entity.AgentRole{}, entity.Chat{}, err
	}
	if role.ID == 0 {
		return entity.Project{}, entity.AgentRole{}, entity.Chat{}, adminrepo.ErrClusterAdminAdmissionDenied
	}
	chats, err := svc.cfg.Store.ListChats(ctx, project.ID)
	if err != nil {
		return entity.Project{}, entity.AgentRole{}, entity.Chat{}, err
	}
	for _, chat := range chats {
		if chat.Slug != systemMatterCodexChatSlug {
			continue
		}
		if err := svc.admitClusterAdminRoleBinding(ctx, role, chat.ID, chat.Slug, chat.MattermostChannelID, "", "", "bootstrap", operation); err != nil {
			return entity.Project{}, entity.AgentRole{}, entity.Chat{}, err
		}
		return project, role, chat, nil
	}
	return entity.Project{}, entity.AgentRole{}, entity.Chat{}, adminrepo.ErrClusterAdminAdmissionDenied
}

func (svc *SlashCommandService) admitClusterAdminRoleBinding(ctx context.Context, role entity.AgentRole, chatID int64, chatSlug string, channelID string, sessionKey string, actorUserID string, actorUser string, operation string) error {
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
		RoleID: role.ID, ProjectID: role.ProjectID, ChatID: chatID, ChatSlug: chatSlug, MattermostChannelID: channelID, SessionKey: sessionKey,
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

func (svc *SlashCommandService) withClusterAdminRoleBindingGuard(ctx context.Context, role entity.AgentRole, chatID int64, chatSlug string, channelID string, sessionKey string, actorUserID string, actorUser string, operation string, sideEffect func() error) error {
	if !strings.EqualFold(strings.TrimSpace(role.KubernetesAccess), "cluster-admin") {
		return sideEffect()
	}
	repository, ok := svc.cfg.Store.(securityrepo.ClusterAdminRuntimeGuardRepository)
	if !ok {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	return repository.WithExistingClusterAdminRuntimeGuard(ctx, securityrepo.ClusterAdminBindingInput{
		RoleID: role.ID, ProjectID: role.ProjectID, ChatID: chatID, ChatSlug: chatSlug,
		MattermostChannelID: channelID, SessionKey: sessionKey, ActorUserID: actorUserID, ActorUser: actorUser, Operation: operation,
	}, sideEffect)
}

func (svc *SlashCommandService) preferredManageOpenAIAccount(ctx context.Context) (string, error) {
	return svc.preferredOpenAIAccount(ctx, []string{"openai-codex-manage", "manage", "openai-codex-main", "main", "primary"})
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
		if strings.EqualFold(strings.TrimSpace(role.KubernetesAccess), "cluster-admin") {
			continue
		}
		if _, err := svc.ensureRoleBotIdentity(ctx, project, role, ""); err != nil {
			return err
		}
	}
	return nil
}

func (svc *SlashCommandService) reconcileProjectControlBotMemberships(ctx context.Context, project entity.Project) error {
	if svc.cfg.ChannelManager == nil || svc.cfg.RoleBotManager == nil {
		return nil
	}
	botUserID, err := svc.cfg.ChannelManager.BotUserID(ctx)
	if err != nil {
		return err
	}
	chats, err := svc.cfg.Store.ListChats(ctx, project.ID)
	if err != nil {
		return err
	}
	for _, chat := range chats {
		if strings.TrimSpace(chat.MattermostChannelID) == "" {
			continue
		}
		if err := svc.cfg.RoleBotManager.EnsureProjectChannelMember(ctx, project.Slug, chat.MattermostChannelID, botUserID); err != nil {
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
		if err := svc.admitClusterAdminRoleBinding(ctx, role, chat.ID, chat.Slug, chat.MattermostChannelID, "", "", "bootstrap", "system_role.bind_chat"); err != nil {
			return err
		}
		if err := svc.withClusterAdminRoleBindingGuard(ctx, role, chat.ID, chat.Slug, chat.MattermostChannelID, "", "", "bootstrap", "system_role.bind_chat.bot_identity.side_effect", func() error {
			return svc.ensureSystemRoleBotIdentity(ctx, project, role, chat.MattermostChannelID)
		}); err != nil {
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
			SystemPurpose:       chat.SystemPurpose,
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
