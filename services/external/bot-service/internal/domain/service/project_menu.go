package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
)

const (
	menuViewProjects = "projects"
	menuViewRoles    = "roles"
	menuViewChats    = "chats"
	menuViewAdvanced = "advanced"

	menuDialogProjectUpsert         = "project_upsert"
	menuDialogProjectRepositoryBind = "project_repository_bind"
	menuDialogProjectRuntimeVar     = "project_runtime_var"
	menuDialogRoleRuntimeVarAttach  = "role_runtime_var_attach"
	menuDialogRoleRuntimeVarDetach  = "role_runtime_var_detach"
	menuDialogAgentRoleUpsert       = "agent_role_upsert"
	menuDialogChatCreate            = "chat_create"

	menuActionProjectDashboard       = "project_dashboard"
	menuActionProjectBindRepo        = "project_bind_repo"
	menuActionThreadRepositorySelect = "thread_repository_select"

	menuResourceProject       = "project"
	menuResourceAgentRole     = "agent_role"
	menuResourceChat          = "chat"
	menuResourceThreadContext = "thread_context"
	menuResourceRuntimeVar    = "project_runtime_var"

	dialogCallbackProjectUpsert         = "agents_project_upsert"
	dialogCallbackProjectRepositoryBind = "agents_project_repository_bind"
	dialogCallbackProjectRuntimeVar     = "agents_project_runtime_var"
	dialogCallbackRoleRuntimeVarAttach  = "agents_role_runtime_var_attach"
	dialogCallbackRoleRuntimeVarDetach  = "agents_role_runtime_var_detach"
	dialogCallbackAgentRoleUpsert       = "agents_agent_role_upsert"
	dialogCallbackChatCreate            = "agents_chat_create"

	dialogOptionNone = "__none__"

	dialogFieldProjectID        = "project_id"
	dialogFieldProjectName      = "project_name"
	dialogFieldProjectSlug      = "project_slug"
	dialogFieldRoleType         = "role_type"
	dialogFieldPromptMode       = "prompt_mode"
	dialogFieldPromptTemplate   = "prompt_template"
	dialogFieldBotIdentity      = "bot_identity"
	dialogFieldAdvancedSettings = "advanced_settings"
	dialogFieldChatName         = "chat_name"
	dialogFieldChatType         = "chat_type"
	dialogFieldPrimaryRoleID    = "primary_role_id"
	dialogFieldSecondaryRoleID  = "secondary_role_id"
	dialogFieldRepositoryID     = "repository_id"
	dialogFieldRootIssue        = "root_issue"
	dialogFieldWorkPolicy       = "work_policy"
	dialogFieldGitHubOwner      = "github_owner"
	dialogFieldGitHubOwnerType  = "github_owner_type"
	dialogFieldRuntimeVarID     = "runtime_var_id"
	dialogFieldRuntimeVarName   = "runtime_var_name"
	dialogFieldRuntimeVarValue  = "runtime_var_value"
	dialogFieldSensitive        = "sensitive"
	dialogFieldEnabled          = "enabled"
)

func (svc *SlashCommandService) handleProject(ctx context.Context, args []string, command SlashCommand) string {
	if len(args) == 0 || args[0] != "list" {
		return svc.t("project.usage", nil)
	}
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return svc.t("project.storage_not_ready", nil)
	}
	projects, err := svc.cfg.Store.ListProjects(ctx, 100)
	if err != nil {
		return svc.t("project.list.failed", map[string]any{"Error": safeError(err)})
	}
	if len(projects) == 0 {
		return svc.t("project.list.empty", nil)
	}
	lines := []string{svc.t("project.list.header", nil)}
	for _, project := range projects {
		lines = append(lines, svc.t("project.list.item", map[string]any{
			"ID":   project.ID,
			"Name": project.Name,
			"Slug": project.Slug,
			"Team": emptyAsUnknown(project.MattermostTeamID),
		}))
	}
	_ = command
	return strings.Join(lines, "\n")
}

func (svc *SlashCommandService) handleAgentRole(ctx context.Context, args []string) string {
	if len(args) == 0 || args[0] != "list" {
		return svc.t("agent_role.usage", nil)
	}
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return svc.t("agent_role.storage_not_ready", nil)
	}
	roles, err := svc.cfg.Store.ListAgentRoles(ctx, 0)
	if err != nil {
		return svc.t("agent_role.list.failed", map[string]any{"Error": safeError(err)})
	}
	if len(roles) == 0 {
		return svc.t("agent_role.list.empty", nil)
	}
	lines := []string{svc.t("agent_role.list.header", nil)}
	for _, role := range roles {
		lines = append(lines, svc.t("agent_role.list.item", map[string]any{
			"ID":      role.ID,
			"Project": svc.roleProjectLabel(ctx, role.ProjectID),
			"Name":    role.Name,
			"Type":    role.RoleType,
			"Prompt":  rolePromptLabel(svc, role),
		}))
	}
	return strings.Join(lines, "\n")
}

func (svc *SlashCommandService) handleChat(ctx context.Context, args []string) string {
	if len(args) == 0 || args[0] != "list" {
		return svc.t("chat.usage", nil)
	}
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return svc.t("chat.storage_not_ready", nil)
	}
	chats, err := svc.cfg.Store.ListChats(ctx, 0)
	if err != nil {
		return svc.t("chat.list.failed", map[string]any{"Error": safeError(err)})
	}
	if len(chats) == 0 {
		return svc.t("chat.list.empty", nil)
	}
	lines := []string{svc.t("chat.list.header", nil)}
	for _, chat := range chats {
		lines = append(lines, svc.t("chat.list.item", map[string]any{
			"ID":      chat.ID,
			"Project": chat.ProjectID,
			"Name":    chat.Name,
			"Type":    chat.ChatType,
			"Channel": emptyAsUnknown(chat.MattermostChannelID),
		}))
	}
	return strings.Join(lines, "\n")
}

func (svc *SlashCommandService) projectListCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	card := svc.menuCard(ctx, menuViewProjects)
	card.Title = svc.t("menu.entity.projects.title", nil)
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		card.Text = svc.t("project.storage_not_ready", nil)
		card.Actions = svc.entityNavigationActions(menuViewProjects)
		return card
	}
	projects, err := svc.cfg.Store.ListProjects(ctx, 100)
	if err != nil {
		card.Text = svc.t("project.list.failed", map[string]any{"Error": safeError(err)})
		card.Actions = svc.entityNavigationActions(menuViewProjects)
		return card
	}
	card.Text = svc.entityListText(len(projects), command.Page)
	card.Fields = nil
	card.Actions = nil
	if len(projects) == 0 {
		card.Text = svc.t("project.list.empty", nil)
		card.Actions = append(card.Actions, svc.menuDialogAction(menuViewProjects, "dialogprojectadd", menuDialogProjectUpsert, "menu.action.project_add", "menu.action.project_add.tooltip", "primary"))
		card.Actions = append(card.Actions, svc.entityNavigationActions(menuViewProjects)...)
		return card
	}
	start, end, page := entityPageBounds(len(projects), command.Page)
	for idx, project := range projects[start:end] {
		number := start + idx + 1
		card.Fields = append(card.Fields, MattermostCardField{
			Title: svc.t("menu.entity.project.item_title", map[string]any{"Number": number, "Project": project.Name}),
			Value: svc.t("menu.entity.project.summary", map[string]any{
				"Slug":        project.Slug,
				"Team":        emptyAsUnknown(project.MattermostTeamID),
				"GitHub":      projectGitHubSummary(project),
				"Description": emptyAsUnknown(project.Description),
			}),
			Short: false,
		})
		card.Actions = append(card.Actions, svc.menuResourceAction(menuViewProjects, "openproject"+strconv.Itoa(number), menuActionShow, menuResourceProject, strconv.FormatInt(project.ID, 10), "menu.action.open_number", "menu.action.open_number.tooltip", "default", map[string]any{"Number": number}))
	}
	card.Actions = append(card.Actions, svc.pageActions(menuViewProjects, menuResourceProject, "", page, len(projects))...)
	card.Actions = append(card.Actions, svc.menuDialogAction(menuViewProjects, "dialogprojectadd", menuDialogProjectUpsert, "menu.action.project_add", "menu.action.project_add.tooltip", "primary"))
	card.Actions = append(card.Actions, svc.entityNavigationActions(menuViewProjects)...)
	return card
}

func (svc *SlashCommandService) projectsStatusText(ctx context.Context) string {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return readyLabel(svc.cfg.Localizer, false)
	}
	projects, err := svc.cfg.Store.ListProjects(ctx, 100)
	if err != nil {
		return readyLabel(svc.cfg.Localizer, false)
	}
	return fmt.Sprintf("`%d`", len(projects))
}

func (svc *SlashCommandService) projectEntityCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	card := svc.menuCard(ctx, menuViewProjects)
	projectID, ok := parseInt64ID(command.ID)
	if !ok || !svc.cfg.StorageReady || svc.cfg.Store == nil {
		card.Text = svc.t("menu.entity.invalid", nil)
		card.Actions = svc.entityNavigationActions(menuViewProjects)
		return card
	}
	project, err := svc.cfg.Store.GetProject(ctx, projectID)
	if err != nil {
		card.Text = svc.t("project.get.failed", map[string]any{"Error": safeError(err)})
		card.Actions = svc.entityNavigationActions(menuViewProjects)
		return card
	}
	repositories, _ := svc.cfg.Store.ListProjectRepositories(ctx, project.ID)
	roles, _ := svc.cfg.Store.ListAgentRoles(ctx, project.ID)
	chats, _ := svc.cfg.Store.ListChats(ctx, project.ID)
	runtimeVariables, _ := svc.cfg.Store.ListProjectRuntimeVariables(ctx, project.ID)
	card.Title = svc.t("menu.entity.project.card_title", map[string]any{"Project": project.Name})
	card.Text = svc.t("menu.entity.project.card_text", map[string]any{"Project": project.Name})
	card.Fields = []MattermostCardField{
		{Title: svc.t("menu.entity.field.project", nil), Value: "`" + project.Name + "`", Short: true},
		{Title: svc.t("menu.entity.field.slug", nil), Value: "`" + project.Slug + "`", Short: true},
		{Title: svc.t("menu.entity.field.github_owner", nil), Value: "`" + emptyAsUnknown(project.GitHubOwner) + "`", Short: true},
		{Title: svc.t("menu.entity.field.github", nil), Value: "`" + emptyAsUnknown(project.GitHubAccountName) + "`", Short: true},
		{Title: svc.t("menu.entity.field.team", nil), Value: "`" + emptyAsUnknown(project.MattermostTeamID) + "`", Short: false},
		{Title: svc.t("menu.entity.field.repositories", nil), Value: "`" + strconv.Itoa(len(repositories)) + "`", Short: true},
		{Title: svc.t("menu.entity.field.roles", nil), Value: "`" + strconv.Itoa(len(roles)) + "`", Short: true},
		{Title: svc.t("menu.entity.field.chats", nil), Value: "`" + strconv.Itoa(len(chats)) + "`", Short: true},
		{Title: svc.t("menu.entity.field.runtime_vars", nil), Value: "`" + strconv.Itoa(len(runtimeVariables)) + "`", Short: true},
		{Title: svc.t("menu.entity.field.description", nil), Value: emptyAsUnknown(project.Description), Short: false},
	}
	projectIDText := strconv.FormatInt(project.ID, 10)
	repoOnboardAction := svc.menuResourceAction(menuViewProjects, "projectrepoonboard", menuActionRepositoryOnboard, menuResourceProject, projectIDText, "menu.action.project_repo_onboard", "menu.action.project_repo_onboard.tooltip", "primary", nil)
	if strings.TrimSpace(project.GitHubAccountName) == "" {
		repoOnboardAction.Disabled = true
	}
	if strings.TrimSpace(project.GitHubOwner) == "" {
		repoOnboardAction.Disabled = true
	}
	card.Actions = []MattermostCardAction{
		svc.menuResourceDialogAction(menuViewProjects, "dialogprojectedit", menuDialogProjectUpsert, menuResourceProject, projectIDText, "menu.action.project_edit", "menu.action.project_edit.tooltip", "primary", nil),
		repoOnboardAction,
		svc.menuResourceDialogAction(menuViewProjects, "dialogchatcreate", menuDialogChatCreate, menuResourceProject, projectIDText, "menu.action.chat_add", "menu.action.chat_add.tooltip", "primary", nil),
		svc.menuResourceDialogAction(menuViewProjects, "dialogroleadd", menuDialogAgentRoleUpsert, menuResourceProject, projectIDText, "menu.action.role_add", "menu.action.role_add.tooltip", "primary", nil),
		svc.menuResourceDialogAction(menuViewProjects, "dialogprojectruntimevar", menuDialogProjectRuntimeVar, menuResourceProject, projectIDText, "menu.action.runtime_var_add", "menu.action.runtime_var_add.tooltip", "primary", nil),
		svc.menuResourceDialogAction(menuViewProjects, "dialogroleruntimevarattach", menuDialogRoleRuntimeVarAttach, menuResourceProject, projectIDText, "menu.action.runtime_var_attach", "menu.action.runtime_var_attach.tooltip", "default", nil),
		svc.menuResourceAction(menuViewProjects, "projectruntimevars", menuActionList, menuResourceRuntimeVar, projectIDText, "menu.action.runtime_var_list", "menu.action.runtime_var_list.tooltip", "default", nil),
		svc.menuResourceDialogAction(menuViewProjects, "dialogprojectrepo", menuDialogProjectRepositoryBind, menuResourceProject, projectIDText, "menu.action.project_repo_bind", "menu.action.project_repo_bind.tooltip", "default", nil),
		svc.menuResourceAction(menuViewRoles, "projectroles", menuActionList, menuResourceAgentRole, projectIDText, "menu.action.roles", "menu.action.roles.tooltip", "default", nil),
		svc.menuResourceAction(menuViewChats, "projectchats", menuActionList, menuResourceChat, projectIDText, "menu.action.chats", "menu.action.chats.tooltip", "default", nil),
		svc.menuDialogAction(menuViewProjects, "dialogprojectadd", menuDialogProjectUpsert, "menu.action.project_add", "menu.action.project_add.tooltip", "default"),
		svc.menuResourceAction(menuViewProjects, "projectlist", menuActionList, menuResourceProject, "", "menu.action.project_list", "menu.action.project_list.tooltip", "default", nil),
		svc.menuAction(menuViewMain, "menu.action.main", "menu.action.main.tooltip", "default"),
	}
	return card
}

func (svc *SlashCommandService) projectRuntimeVariableListCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	card := svc.menuCard(ctx, menuViewProjects)
	card.Title = svc.t("menu.entity.runtime_vars.title", nil)
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		card.Text = svc.t("project.storage_not_ready", nil)
		card.Actions = svc.entityNavigationActions(menuViewProjects)
		return card
	}
	projectID, ok := parseInt64ID(command.ID)
	if !ok {
		card.Text = svc.t("menu.entity.invalid", nil)
		card.Actions = svc.entityNavigationActions(menuViewProjects)
		return card
	}
	project, err := svc.cfg.Store.GetProject(ctx, projectID)
	if err != nil {
		card.Text = svc.t("project.get.failed", map[string]any{"Error": safeError(err)})
		card.Actions = svc.entityNavigationActions(menuViewProjects)
		return card
	}
	variables, err := svc.cfg.Store.ListProjectRuntimeVariables(ctx, project.ID)
	if err != nil {
		card.Text = svc.t("runtime_var.list.failed", map[string]any{"Error": safeError(err)})
		card.Actions = svc.entityNavigationActions(menuViewProjects)
		return card
	}
	card.Text = svc.t("menu.entity.runtime_vars.text", map[string]any{"Project": project.Name, "Count": len(variables)})
	card.Fields = nil
	card.Actions = nil
	if len(variables) == 0 {
		card.Text = svc.t("runtime_var.list.empty", map[string]any{"Project": project.Name})
		card.Actions = append(card.Actions, svc.menuResourceDialogAction(menuViewProjects, "dialogprojectruntimevar", menuDialogProjectRuntimeVar, menuResourceProject, strconv.FormatInt(project.ID, 10), "menu.action.runtime_var_add", "menu.action.runtime_var_add.tooltip", "primary", nil))
		card.Actions = append(card.Actions, svc.menuResourceAction(menuViewProjects, "openproject", menuActionShow, menuResourceProject, strconv.FormatInt(project.ID, 10), "menu.action.project_open", "menu.action.project_open.tooltip", "default", nil))
		card.Actions = append(card.Actions, svc.entityNavigationActions(menuViewProjects)...)
		return card
	}
	start, end, page := entityPageBounds(len(variables), command.Page)
	for idx, variable := range variables[start:end] {
		number := start + idx + 1
		card.Fields = append(card.Fields, MattermostCardField{
			Title: svc.t("menu.entity.runtime_var.item_title", map[string]any{"Number": number, "Name": variable.Name}),
			Value: svc.t("menu.entity.runtime_var.summary", map[string]any{
				"Description": emptyAsUnknown(variable.Description),
				"Secret":      maskedSecretRef(variable.SecretRef, variable.SecretKey),
				"Enabled":     variable.Enabled,
			}),
			Short: false,
		})
		card.Actions = append(card.Actions, svc.menuResourceAction(menuViewProjects, "openruntimevar"+strconv.Itoa(number), menuActionShow, menuResourceRuntimeVar, strconv.FormatInt(variable.ID, 10), "menu.action.open_number", "menu.action.open_number.tooltip", "default", map[string]any{"Number": number}))
	}
	projectIDText := strconv.FormatInt(project.ID, 10)
	card.Actions = append(card.Actions, svc.pageActions(menuViewProjects, menuResourceRuntimeVar, projectIDText, page, len(variables))...)
	card.Actions = append(card.Actions,
		svc.menuResourceDialogAction(menuViewProjects, "dialogprojectruntimevar", menuDialogProjectRuntimeVar, menuResourceProject, projectIDText, "menu.action.runtime_var_add", "menu.action.runtime_var_add.tooltip", "primary", nil),
		svc.menuResourceDialogAction(menuViewProjects, "dialogroleruntimevarattach", menuDialogRoleRuntimeVarAttach, menuResourceProject, projectIDText, "menu.action.runtime_var_attach", "menu.action.runtime_var_attach.tooltip", "default", nil),
		svc.menuResourceAction(menuViewProjects, "openproject", menuActionShow, menuResourceProject, projectIDText, "menu.action.project_open", "menu.action.project_open.tooltip", "default", nil),
	)
	return card
}

func (svc *SlashCommandService) projectRuntimeVariableEntityCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	card := svc.menuCard(ctx, menuViewProjects)
	card.Title = svc.t("menu.entity.runtime_var.card_title", nil)
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		card.Text = svc.t("project.storage_not_ready", nil)
		card.Actions = svc.entityNavigationActions(menuViewProjects)
		return card
	}
	variableID, ok := parseInt64ID(command.ID)
	if !ok {
		card.Text = svc.t("menu.entity.invalid", nil)
		card.Actions = svc.entityNavigationActions(menuViewProjects)
		return card
	}
	variable, err := svc.cfg.Store.GetProjectRuntimeVariable(ctx, variableID)
	if err != nil {
		card.Text = svc.t("runtime_var.get.failed", map[string]any{"Error": safeError(err)})
		card.Actions = svc.entityNavigationActions(menuViewProjects)
		return card
	}
	project, _ := svc.cfg.Store.GetProject(ctx, variable.ProjectID)
	card.Title = svc.t("menu.entity.runtime_var.card_title_name", map[string]any{"Name": variable.Name})
	card.Text = svc.t("menu.entity.runtime_var.card_text", map[string]any{"Name": variable.Name})
	card.Fields = []MattermostCardField{
		{Title: svc.t("menu.entity.field.project", nil), Value: "`" + emptyAsUnknown(project.Name) + "`", Short: true},
		{Title: svc.t("menu.entity.field.runtime_var", nil), Value: "`" + variable.Name + "`", Short: true},
		{Title: svc.t("menu.entity.field.slug", nil), Value: "`" + variable.Slug + "`", Short: true},
		{Title: svc.t("menu.entity.field.secret", nil), Value: "`" + maskedSecretRef(variable.SecretRef, variable.SecretKey) + "`", Short: true},
		{Title: svc.t("menu.entity.field.sensitive", nil), Value: "`" + strconv.FormatBool(variable.Sensitive) + "`", Short: true},
		{Title: svc.t("menu.entity.field.enabled", nil), Value: "`" + strconv.FormatBool(variable.Enabled) + "`", Short: true},
		{Title: svc.t("menu.entity.field.description", nil), Value: emptyAsUnknown(variable.Description), Short: false},
	}
	variableIDText := strconv.FormatInt(variable.ID, 10)
	projectIDText := strconv.FormatInt(variable.ProjectID, 10)
	card.Actions = []MattermostCardAction{
		svc.menuResourceDialogAction(menuViewProjects, "dialogruntimevaredit", menuDialogProjectRuntimeVar, menuResourceRuntimeVar, variableIDText, "menu.action.runtime_var_edit", "menu.action.runtime_var_edit.tooltip", "primary", nil),
		svc.menuResourceAction(menuViewProjects, "runtimevardeleteconfirm", menuActionConfirmDelete, menuResourceRuntimeVar, variableIDText, "menu.action.runtime_var_delete", "menu.action.runtime_var_delete.tooltip", "danger", nil),
		svc.menuResourceDialogAction(menuViewProjects, "dialogroleruntimevarattach", menuDialogRoleRuntimeVarAttach, menuResourceRuntimeVar, variableIDText, "menu.action.runtime_var_attach", "menu.action.runtime_var_attach.tooltip", "default", nil),
		svc.menuResourceAction(menuViewProjects, "projectruntimevars", menuActionList, menuResourceRuntimeVar, projectIDText, "menu.action.runtime_var_list", "menu.action.runtime_var_list.tooltip", "default", nil),
		svc.menuResourceAction(menuViewProjects, "openproject", menuActionShow, menuResourceProject, projectIDText, "menu.action.project_open", "menu.action.project_open.tooltip", "default", nil),
	}
	return card
}

func (svc *SlashCommandService) projectRuntimeVariableDeleteConfirmationCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	card := svc.menuCard(ctx, menuViewProjects)
	card.Title = svc.t("menu.confirm.runtime_var_delete.title", nil)
	card.Text = svc.t("menu.confirm.runtime_var_delete.text", map[string]any{"Variable": command.ID})
	card.Fields = []MattermostCardField{{Title: svc.t("menu.entity.field.runtime_var", nil), Value: "`" + command.ID + "`", Short: false}}
	card.Actions = []MattermostCardAction{
		svc.menuResourceAction(menuViewProjects, "runtimevardelete", menuActionDelete, menuResourceRuntimeVar, command.ID, "menu.action.confirm_delete", "menu.action.confirm_delete.tooltip", "danger", nil),
		svc.menuResourceAction(menuViewProjects, "runtimevarcancel", menuActionShow, menuResourceRuntimeVar, command.ID, "menu.action.cancel", "menu.action.cancel.tooltip", "default", nil),
	}
	return card
}

func (svc *SlashCommandService) deleteProjectRuntimeVariableFromMenu(ctx context.Context, command MenuActionCommand) string {
	variableID, ok := parseInt64ID(command.ID)
	if !ok {
		return svc.t("menu.entity.invalid", nil)
	}
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return svc.t("project.storage_not_ready", nil)
	}
	if svc.cfg.RuntimeRunner == nil {
		return svc.t("runtime.not_configured", nil)
	}
	variable, err := svc.cfg.Store.GetProjectRuntimeVariable(ctx, variableID)
	if err != nil {
		return svc.t("runtime_var.get.failed", map[string]any{"Error": safeError(err)})
	}
	roleIDs := svc.roleIDsUsingRuntimeVariable(ctx, variable.ProjectID, variable.ID)
	if strings.TrimSpace(variable.SecretRef) != "" {
		if _, err := svc.cfg.RuntimeRunner.DeleteProjectRuntimeVariableSecret(ctx, variable.SecretRef); err != nil {
			return svc.t("runtime_var.secret_delete.failed", map[string]any{"Error": safeError(err)})
		}
	}
	deleted, err := svc.cfg.Store.DeleteProjectRuntimeVariable(ctx, variableID)
	if err != nil {
		return svc.t("runtime_var.delete.failed", map[string]any{"Error": safeError(err)})
	}
	_ = svc.cfg.Store.RecordAuditEvent(ctx, adminrepo.AuditEventInput{
		EventType:    "project.runtime_variable.deleted",
		ActorUserID:  command.UserID,
		ActorUser:    command.UserName,
		ResourceType: "project_runtime_variable",
		ResourceName: deleted.Name,
		Summary:      "project runtime variable deleted from Mattermost entity card",
	})
	text := svc.t("runtime_var.delete.result", map[string]any{"Name": deleted.Name, "Project": deleted.ProjectID})
	text += svc.invalidateIdleAgentSessionsForRolesText(ctx, roleIDs)
	return text
}

func (svc *SlashCommandService) roleListCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	card := svc.menuCard(ctx, menuViewRoles)
	card.Title = svc.t("menu.entity.roles.title", nil)
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		card.Text = svc.t("agent_role.storage_not_ready", nil)
		card.Actions = svc.entityNavigationActions(menuViewRoles)
		return card
	}
	projectID, _ := parseInt64ID(command.ID)
	roles, err := svc.cfg.Store.ListAgentRoles(ctx, projectID)
	if err != nil {
		card.Text = svc.t("agent_role.list.failed", map[string]any{"Error": safeError(err)})
		card.Actions = svc.entityNavigationActions(menuViewRoles)
		return card
	}
	card.Text = svc.entityListText(len(roles), command.Page)
	card.Fields = nil
	card.Actions = nil
	if len(roles) == 0 {
		card.Text = svc.t("agent_role.list.empty", nil)
		if projectID > 0 {
			card.Actions = append(card.Actions, svc.menuResourceDialogAction(menuViewRoles, "dialogroleadd", menuDialogAgentRoleUpsert, menuResourceProject, strconv.FormatInt(projectID, 10), "menu.action.role_add", "menu.action.role_add.tooltip", "primary", nil))
		} else {
			card.Actions = append(card.Actions, svc.menuDialogAction(menuViewRoles, "dialogroleadd", menuDialogAgentRoleUpsert, "menu.action.role_add", "menu.action.role_add.tooltip", "primary"))
		}
		card.Actions = append(card.Actions, svc.entityNavigationActions(menuViewRoles)...)
		return card
	}
	start, end, page := entityPageBounds(len(roles), command.Page)
	for idx, role := range roles[start:end] {
		number := start + idx + 1
		card.Fields = append(card.Fields, MattermostCardField{
			Title: svc.t("menu.entity.role.item_title", map[string]any{"Number": number, "Role": role.Name}),
			Value: svc.t("menu.entity.role.summary", map[string]any{
				"Project": svc.roleProjectLabel(ctx, role.ProjectID),
				"Type":    role.RoleType,
				"OpenAI":  emptyAsUnknown(role.OpenAIAccountName),
				"GitHub":  emptyAsUnknown(role.GitHubAccountName),
				"Prompt":  rolePromptLabel(svc, role),
			}),
			Short: false,
		})
		card.Actions = append(card.Actions, svc.menuResourceAction(menuViewRoles, "openrole"+strconv.Itoa(number), menuActionShow, menuResourceAgentRole, strconv.FormatInt(role.ID, 10), "menu.action.open_number", "menu.action.open_number.tooltip", "default", map[string]any{"Number": number}))
	}
	card.Actions = append(card.Actions, svc.pageActions(menuViewRoles, menuResourceAgentRole, command.ID, page, len(roles))...)
	if projectID > 0 {
		card.Actions = append(card.Actions, svc.menuResourceDialogAction(menuViewRoles, "dialogroleadd", menuDialogAgentRoleUpsert, menuResourceProject, strconv.FormatInt(projectID, 10), "menu.action.role_add", "menu.action.role_add.tooltip", "primary", nil))
	} else {
		card.Actions = append(card.Actions, svc.menuDialogAction(menuViewRoles, "dialogroleadd", menuDialogAgentRoleUpsert, "menu.action.role_add", "menu.action.role_add.tooltip", "primary"))
	}
	card.Actions = append(card.Actions, svc.entityNavigationActions(menuViewRoles)...)
	return card
}

func (svc *SlashCommandService) roleEntityCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	card := svc.menuCard(ctx, menuViewRoles)
	roleID, ok := parseInt64ID(command.ID)
	if !ok || !svc.cfg.StorageReady || svc.cfg.Store == nil {
		card.Text = svc.t("menu.entity.invalid", nil)
		card.Actions = svc.entityNavigationActions(menuViewRoles)
		return card
	}
	role, err := svc.cfg.Store.GetAgentRole(ctx, roleID)
	if err != nil {
		card.Text = svc.t("agent_role.get.failed", map[string]any{"Error": safeError(err)})
		card.Actions = svc.entityNavigationActions(menuViewRoles)
		return card
	}
	runtimeVariables, _ := svc.cfg.Store.ListAgentRoleRuntimeVariables(ctx, role.ID)
	projectLabel := svc.roleProjectLabel(ctx, role.ProjectID)
	card.Title = svc.t("menu.entity.role.card_title", map[string]any{"Role": role.Name})
	card.Text = svc.t("menu.entity.role.card_text", map[string]any{"Role": role.Name})
	card.Fields = []MattermostCardField{
		{Title: svc.t("menu.entity.field.role", nil), Value: "`" + role.Name + "`", Short: true},
		{Title: svc.t("menu.entity.field.project", nil), Value: "`" + projectLabel + "`", Short: true},
		{Title: svc.t("menu.entity.field.type", nil), Value: "`" + role.RoleType + "`", Short: true},
		{Title: svc.t("menu.entity.field.bot_identity", nil), Value: "`" + emptyAsUnknown(role.BotIdentity) + "`", Short: true},
		{Title: svc.t("menu.entity.field.openai", nil), Value: "`" + emptyAsUnknown(role.OpenAIAccountName) + "`", Short: true},
		{Title: svc.t("menu.entity.field.github", nil), Value: "`" + emptyAsUnknown(role.GitHubAccountName) + "`", Short: true},
		{Title: svc.t("menu.entity.field.prompt_mode", nil), Value: "`" + defaultString(role.PromptMode, "raw") + "`", Short: true},
		{Title: svc.t("menu.entity.field.kubernetes_access", nil), Value: "`" + defaultString(role.KubernetesAccess, "read-only") + "`", Short: true},
		{Title: svc.t("menu.entity.field.sandbox", nil), Value: "`" + defaultString(role.SandboxMode, "danger-full-access") + "`", Short: true},
		{Title: svc.t("menu.entity.field.runtime_vars", nil), Value: "`" + roleRuntimeVariableNames(runtimeVariables) + "`", Short: false},
		{Title: svc.t("menu.entity.field.prompt", nil), Value: rolePromptLabel(svc, role), Short: true},
		{Title: svc.t("menu.entity.field.codex_config", nil), Value: svc.settingsSummary(role.ConfigOverlay), Short: true},
		{Title: svc.t("menu.entity.field.advanced_settings", nil), Value: svc.settingsSummary(role.AdvancedSettings), Short: true},
		{Title: svc.t("menu.entity.field.description", nil), Value: emptyAsUnknown(role.Description), Short: false},
	}
	card.Actions = []MattermostCardAction{
		svc.menuResourceDialogAction(menuViewRoles, "dialogroleedit", menuDialogAgentRoleUpsert, menuResourceAgentRole, strconv.FormatInt(role.ID, 10), "menu.action.role_edit", "menu.action.role_edit.tooltip", "primary", nil),
		svc.menuResourceDialogAction(menuViewRoles, "dialogroleruntimevarattach", menuDialogRoleRuntimeVarAttach, menuResourceAgentRole, strconv.FormatInt(role.ID, 10), "menu.action.runtime_var_attach", "menu.action.runtime_var_attach.tooltip", "primary", nil),
		svc.menuResourceDialogAction(menuViewRoles, "dialogroleruntimevardetach", menuDialogRoleRuntimeVarDetach, menuResourceAgentRole, strconv.FormatInt(role.ID, 10), "menu.action.runtime_var_detach", "menu.action.runtime_var_detach.tooltip", "default", nil),
		svc.menuResourceAction(menuViewProjects, "openproject", menuActionShow, menuResourceProject, strconv.FormatInt(role.ProjectID, 10), "menu.action.project_open", "menu.action.project_open.tooltip", "default", nil),
		svc.menuResourceAction(menuViewRoles, "rolelist", menuActionList, menuResourceAgentRole, strconv.FormatInt(role.ProjectID, 10), "menu.action.role_list", "menu.action.role_list.tooltip", "default", nil),
		svc.menuAction(menuViewMain, "menu.action.main", "menu.action.main.tooltip", "default"),
	}
	return card
}

func (svc *SlashCommandService) chatListCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	card := svc.menuCard(ctx, menuViewChats)
	card.Title = svc.t("menu.entity.chats.title", nil)
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		card.Text = svc.t("chat.storage_not_ready", nil)
		card.Actions = svc.entityNavigationActions(menuViewChats)
		return card
	}
	projectID, _ := parseInt64ID(command.ID)
	chats, err := svc.cfg.Store.ListChats(ctx, projectID)
	if err != nil {
		card.Text = svc.t("chat.list.failed", map[string]any{"Error": safeError(err)})
		card.Actions = svc.entityNavigationActions(menuViewChats)
		return card
	}
	card.Text = svc.entityListText(len(chats), command.Page)
	card.Fields = nil
	card.Actions = nil
	if len(chats) == 0 {
		card.Text = svc.t("chat.list.empty", nil)
		if projectID > 0 {
			card.Actions = append(card.Actions, svc.menuResourceDialogAction(menuViewChats, "dialogchatcreate", menuDialogChatCreate, menuResourceProject, strconv.FormatInt(projectID, 10), "menu.action.chat_add", "menu.action.chat_add.tooltip", "primary", nil))
		} else {
			card.Actions = append(card.Actions, svc.menuDialogAction(menuViewChats, "dialogchatcreate", menuDialogChatCreate, "menu.action.chat_add", "menu.action.chat_add.tooltip", "primary"))
		}
		card.Actions = append(card.Actions, svc.entityNavigationActions(menuViewChats)...)
		return card
	}
	start, end, page := entityPageBounds(len(chats), command.Page)
	for idx, chat := range chats[start:end] {
		number := start + idx + 1
		card.Fields = append(card.Fields, MattermostCardField{
			Title: svc.t("menu.entity.chat.item_title", map[string]any{"Number": number, "Chat": chat.Name}),
			Value: svc.t("menu.entity.chat.summary", map[string]any{
				"Project": chat.ProjectID,
				"Type":    chat.ChatType,
				"Channel": emptyAsUnknown(chat.MattermostChannelID),
				"Issue":   emptyAsUnknown(chat.RootGitHubIssue),
			}),
			Short: false,
		})
		card.Actions = append(card.Actions, svc.menuResourceAction(menuViewChats, "openchat"+strconv.Itoa(number), menuActionShow, menuResourceChat, strconv.FormatInt(chat.ID, 10), "menu.action.open_number", "menu.action.open_number.tooltip", "default", map[string]any{"Number": number}))
	}
	card.Actions = append(card.Actions, svc.pageActions(menuViewChats, menuResourceChat, command.ID, page, len(chats))...)
	if projectID > 0 {
		card.Actions = append(card.Actions, svc.menuResourceDialogAction(menuViewChats, "dialogchatcreate", menuDialogChatCreate, menuResourceProject, strconv.FormatInt(projectID, 10), "menu.action.chat_add", "menu.action.chat_add.tooltip", "primary", nil))
	} else {
		card.Actions = append(card.Actions, svc.menuDialogAction(menuViewChats, "dialogchatcreate", menuDialogChatCreate, "menu.action.chat_add", "menu.action.chat_add.tooltip", "primary"))
	}
	card.Actions = append(card.Actions, svc.entityNavigationActions(menuViewChats)...)
	return card
}

func (svc *SlashCommandService) chatEntityCard(ctx context.Context, command MenuActionCommand) *MattermostCard {
	card := svc.menuCard(ctx, menuViewChats)
	chatID, ok := parseInt64ID(command.ID)
	if !ok || !svc.cfg.StorageReady || svc.cfg.Store == nil {
		card.Text = svc.t("menu.entity.invalid", nil)
		card.Actions = svc.entityNavigationActions(menuViewChats)
		return card
	}
	chat, err := svc.cfg.Store.GetChat(ctx, chatID)
	if err != nil {
		card.Text = svc.t("chat.get.failed", map[string]any{"Error": safeError(err)})
		card.Actions = svc.entityNavigationActions(menuViewChats)
		return card
	}
	participants, _ := svc.cfg.Store.ListChatParticipants(ctx, chat.ID)
	repositories, _ := svc.cfg.Store.ListChatRepositories(ctx, chat.ID)
	card.Title = svc.t("menu.entity.chat.card_title", map[string]any{"Chat": chat.Name})
	card.Text = svc.t("menu.entity.chat.card_text", map[string]any{"Chat": chat.Name})
	card.Fields = []MattermostCardField{
		{Title: svc.t("menu.entity.field.chat", nil), Value: "`" + chat.Name + "`", Short: true},
		{Title: svc.t("menu.entity.field.project", nil), Value: "`" + strconv.FormatInt(chat.ProjectID, 10) + "`", Short: true},
		{Title: svc.t("menu.entity.field.type", nil), Value: "`" + chat.ChatType + "`", Short: true},
		{Title: svc.t("menu.entity.field.channel", nil), Value: "`" + emptyAsUnknown(chat.MattermostChannelID) + "`", Short: false},
		{Title: svc.t("menu.entity.field.roles", nil), Value: "`" + chatParticipantNames(participants) + "`", Short: false},
		{Title: svc.t("menu.entity.field.repositories", nil), Value: "`" + chatRepositoryNames(repositories) + "`", Short: false},
		{Title: svc.t("menu.entity.field.issue", nil), Value: emptyAsUnknown(chat.RootGitHubIssue), Short: false},
	}
	card.Actions = []MattermostCardAction{
		svc.menuResourceAction(menuViewProjects, "openproject", menuActionShow, menuResourceProject, strconv.FormatInt(chat.ProjectID, 10), "menu.action.project_open", "menu.action.project_open.tooltip", "default", nil),
		svc.menuResourceAction(menuViewChats, "chatlist", menuActionList, menuResourceChat, strconv.FormatInt(chat.ProjectID, 10), "menu.action.chat_list", "menu.action.chat_list.tooltip", "default", nil),
		svc.menuAction(menuViewMain, "menu.action.main", "menu.action.main.tooltip", "default"),
	}
	return card
}

func (svc *SlashCommandService) projectDialog(ctx context.Context, command MenuActionCommand) (*MattermostDialog, string) {
	project := entity.Project{}
	titleID := "dialog.project.add.title"
	introID := "dialog.project.add.intro"
	submitID := "dialog.project.add.submit"
	editMode := command.Resource == menuResourceProject && strings.TrimSpace(command.ID) != ""
	if editMode {
		projectID, ok := parseInt64ID(command.ID)
		if !ok || !svc.cfg.StorageReady || svc.cfg.Store == nil {
			return nil, svc.t("menu.entity.invalid", nil)
		}
		current, err := svc.cfg.Store.GetProject(ctx, projectID)
		if err != nil {
			return nil, svc.t("project.get.failed", map[string]any{"Error": safeError(err)})
		}
		project = current
		titleID = "dialog.project.edit.title"
		introID = "dialog.project.edit.intro"
		submitID = "dialog.project.edit.submit"
	}
	elements := []MattermostDialogElement{
		{
			DisplayName: svc.t("dialog.project.field.name", nil),
			Name:        dialogFieldProjectName,
			Type:        "text",
			Default:     project.Name,
			Placeholder: "Matter Codex",
			HelpText:    svc.t("dialog.project.field.name.help", nil),
			MinLength:   2,
			MaxLength:   64,
		},
		{
			DisplayName: svc.t("dialog.project.field.slug", nil),
			Name:        dialogFieldProjectSlug,
			Type:        "text",
			Default:     project.Slug,
			Placeholder: "matter-codex",
			HelpText:    svc.t("dialog.project.field.slug.help", nil),
			Optional:    true,
			MaxLength:   48,
		},
	}
	if svc.cfg.StorageReady && svc.cfg.Store != nil {
		if githubOptions, errText := svc.optionalGitHubAccountOptions(ctx, project.GitHubAccountName); errText == "" {
			elements = append(elements, MattermostDialogElement{
				DisplayName: svc.t("dialog.project.field.github", nil),
				Name:        dialogFieldGitHubAccount,
				Type:        "select",
				Default:     optionalSelectDefault(project.GitHubAccountName),
				HelpText:    svc.t("dialog.project.field.github.help", nil),
				Optional:    true,
				Options:     githubOptions,
			})
		}
	}
	elements = append(elements,
		MattermostDialogElement{
			DisplayName: svc.t("dialog.project.field.github_owner", nil),
			Name:        dialogFieldGitHubOwner,
			Type:        "text",
			Default:     project.GitHubOwner,
			Placeholder: "radar-auto",
			HelpText:    svc.t("dialog.project.field.github_owner.help", nil),
			Optional:    true,
			MaxLength:   100,
		},
		MattermostDialogElement{
			DisplayName: svc.t("dialog.project.field.github_owner_type", nil),
			Name:        dialogFieldGitHubOwnerType,
			Type:        "select",
			Default:     defaultString(project.GitHubOwnerType, "org"),
			HelpText:    svc.t("dialog.project.field.github_owner_type.help", nil),
			Optional:    true,
			Options: []MattermostDialogOption{
				{Text: svc.t("dialog.project.github_owner_type.org", nil), Value: "org"},
				{Text: svc.t("dialog.project.github_owner_type.user", nil), Value: "user"},
			},
		},
	)
	elements = append(elements,
		MattermostDialogElement{
			DisplayName: svc.t("dialog.project.field.description", nil),
			Name:        dialogFieldDescription,
			Type:        "textarea",
			Default:     project.Description,
			HelpText:    svc.t("dialog.project.field.description.help", nil),
			Optional:    true,
			MaxLength:   1000,
		},
		MattermostDialogElement{
			DisplayName: svc.t("dialog.project.field.advanced", nil),
			Name:        dialogFieldAdvancedSettings,
			Type:        "textarea",
			Default:     project.AdvancedSettings,
			Placeholder: "{}",
			HelpText:    svc.t("dialog.project.field.advanced.help", nil),
			Optional:    true,
			MaxLength:   4000,
		},
	)
	return &MattermostDialog{
		SubmitURL:        svc.cfg.DialogSubmitURL,
		CallbackID:       dialogCallbackProjectUpsert,
		Title:            svc.t(titleID, nil),
		IntroductionText: svc.t(introID, nil),
		Elements:         elements,
		SubmitLabel:      svc.t(submitID, nil),
		State:            encodeDialogState(command),
	}, ""
}

func (svc *SlashCommandService) projectRepositoryBindDialog(ctx context.Context, command MenuActionCommand) (*MattermostDialog, string) {
	projectID, _ := parseInt64ID(command.ID)
	projectOptions, errText := svc.projectOptions(ctx, projectID)
	if errText != "" {
		return nil, errText
	}
	repositoryOptions, errText := svc.repositoryEntityOptions(ctx, 0)
	if errText != "" {
		return nil, errText
	}
	return &MattermostDialog{
		SubmitURL:        svc.cfg.DialogSubmitURL,
		CallbackID:       dialogCallbackProjectRepositoryBind,
		Title:            svc.t("dialog.project_repo.title", nil),
		IntroductionText: svc.t("dialog.project_repo.intro", nil),
		Elements: []MattermostDialogElement{
			{
				DisplayName: svc.t("dialog.project.field.project", nil),
				Name:        dialogFieldProjectID,
				Type:        "select",
				Default:     selectedIDString(projectID),
				Options:     projectOptions,
			},
			{
				DisplayName: svc.t("dialog.project_repo.field.repository", nil),
				Name:        dialogFieldRepositoryID,
				Type:        "select",
				Options:     repositoryOptions,
			},
			{
				DisplayName: svc.t("dialog.project_repo.field.default", nil),
				Name:        dialogFieldStatus,
				Type:        "select",
				Default:     "true",
				Options: []MattermostDialogOption{
					{Text: svc.t("label.yes", nil), Value: "true"},
					{Text: svc.t("label.no", nil), Value: "false"},
				},
			},
		},
		SubmitLabel: svc.t("dialog.project_repo.submit", nil),
		State:       encodeDialogState(command),
	}, ""
}

func (svc *SlashCommandService) projectRuntimeVariableDialog(ctx context.Context, command MenuActionCommand) (*MattermostDialog, string) {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return nil, svc.t("project.storage_not_ready", nil)
	}
	projectID, errText := svc.projectRuntimeVariableDialogProjectID(ctx, command)
	if errText != "" {
		return nil, errText
	}
	variable := entity.ProjectRuntimeVariable{
		ProjectID: projectID,
		SecretKey: "value",
		Sensitive: true,
		Enabled:   true,
	}
	titleID := "dialog.runtime_var.add.title"
	introID := "dialog.runtime_var.add.intro"
	submitID := "dialog.runtime_var.add.submit"
	if command.Resource == menuResourceAgentRole {
		introID = "dialog.runtime_var.add_and_attach.intro"
		submitID = "dialog.runtime_var.add_and_attach.submit"
	}
	editMode := command.Resource == menuResourceRuntimeVar && strings.TrimSpace(command.ID) != ""
	if editMode {
		variableID, ok := parseInt64ID(command.ID)
		if !ok {
			return nil, svc.t("menu.entity.invalid", nil)
		}
		current, err := svc.cfg.Store.GetProjectRuntimeVariable(ctx, variableID)
		if err != nil {
			return nil, svc.t("runtime_var.get.failed", map[string]any{"Error": safeError(err)})
		}
		variable = current
		titleID = "dialog.runtime_var.edit.title"
		introID = "dialog.runtime_var.edit.intro"
		submitID = "dialog.runtime_var.edit.submit"
	}
	projectOptions, errText := svc.projectOptions(ctx, variable.ProjectID)
	if errText != "" {
		return nil, errText
	}
	return &MattermostDialog{
		SubmitURL:        svc.cfg.DialogSubmitURL,
		CallbackID:       dialogCallbackProjectRuntimeVar,
		Title:            svc.t(titleID, nil),
		IntroductionText: svc.t(introID, nil),
		Elements: []MattermostDialogElement{
			{
				DisplayName: svc.t("dialog.project.field.project", nil),
				Name:        dialogFieldProjectID,
				Type:        "select",
				Default:     selectedIDString(variable.ProjectID),
				Options:     projectOptions,
			},
			{
				DisplayName: svc.t("dialog.runtime_var.field.name", nil),
				Name:        dialogFieldRuntimeVarName,
				Type:        "text",
				Default:     variable.Name,
				Placeholder: "RADAR_AUTO_KUBECONFIG",
				HelpText:    svc.t("dialog.runtime_var.field.name.help", nil),
				MinLength:   2,
				MaxLength:   128,
			},
			{
				DisplayName: svc.t("dialog.runtime_var.field.value", nil),
				Name:        dialogFieldRuntimeVarValue,
				Type:        "textarea",
				Placeholder: svc.t("dialog.runtime_var.field.value.placeholder", nil),
				HelpText:    svc.t("dialog.runtime_var.field.value.help", nil),
				Optional:    editMode,
				MaxLength:   60000,
			},
			{
				DisplayName: svc.t("dialog.runtime_var.field.sensitive", nil),
				Name:        dialogFieldSensitive,
				Type:        "select",
				Default:     strconv.FormatBool(variable.Sensitive),
				Options: []MattermostDialogOption{
					{Text: svc.t("label.yes", nil), Value: "true"},
					{Text: svc.t("label.no", nil), Value: "false"},
				},
			},
			{
				DisplayName: svc.t("dialog.runtime_var.field.enabled", nil),
				Name:        dialogFieldEnabled,
				Type:        "select",
				Default:     strconv.FormatBool(variable.Enabled),
				Options: []MattermostDialogOption{
					{Text: svc.t("label.yes", nil), Value: "true"},
					{Text: svc.t("label.no", nil), Value: "false"},
				},
			},
			{
				DisplayName: svc.t("dialog.runtime_var.field.description", nil),
				Name:        dialogFieldDescription,
				Type:        "textarea",
				Default:     variable.Description,
				HelpText:    svc.t("dialog.runtime_var.field.description.help", nil),
				Optional:    true,
				MaxLength:   1000,
			},
		},
		SubmitLabel: svc.t(submitID, nil),
		State:       encodeDialogState(command),
	}, ""
}

func (svc *SlashCommandService) roleRuntimeVariableAttachDialog(ctx context.Context, command MenuActionCommand) (*MattermostDialog, string) {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return nil, svc.t("project.storage_not_ready", nil)
	}
	projectID, selectedRoleID, selectedVariableID, errText := svc.runtimeVariableSelectionDefaults(ctx, command)
	if errText != "" {
		return nil, errText
	}
	roleOptions, errText := svc.agentRoleOptions(ctx, projectID, true)
	if errText != "" {
		return nil, errText
	}
	variables, err := svc.cfg.Store.ListProjectRuntimeVariables(ctx, projectID)
	if err != nil {
		return nil, svc.t("runtime_var.list.failed", map[string]any{"Error": safeError(err)})
	}
	if len(variables) == 0 {
		return svc.projectRuntimeVariableDialog(ctx, command)
	}
	variableOptions := svc.projectRuntimeVariableOptionsFromList(variables, selectedVariableID)
	return &MattermostDialog{
		SubmitURL:        svc.cfg.DialogSubmitURL,
		CallbackID:       dialogCallbackRoleRuntimeVarAttach,
		Title:            svc.t("dialog.role_runtime_var.attach.title", nil),
		IntroductionText: svc.t("dialog.role_runtime_var.attach.intro", nil),
		Elements: []MattermostDialogElement{
			{
				DisplayName: svc.t("dialog.role_runtime_var.field.role", nil),
				Name:        dialogFieldRole,
				Type:        "select",
				Default:     selectedIDString(selectedRoleID),
				Options:     roleOptions,
			},
			{
				DisplayName: svc.t("dialog.role_runtime_var.field.variable", nil),
				Name:        dialogFieldRuntimeVarID,
				Type:        "select",
				Default:     selectedIDString(selectedVariableID),
				Options:     variableOptions,
			},
		},
		SubmitLabel: svc.t("dialog.role_runtime_var.attach.submit", nil),
		State:       encodeDialogState(command),
	}, ""
}

func (svc *SlashCommandService) roleRuntimeVariableDetachDialog(ctx context.Context, command MenuActionCommand) (*MattermostDialog, string) {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return nil, svc.t("project.storage_not_ready", nil)
	}
	roleID, ok := parseInt64ID(command.ID)
	if command.Resource != menuResourceAgentRole || !ok {
		return nil, svc.t("dialog.role_runtime_var.detach_from_role_required", nil)
	}
	role, err := svc.cfg.Store.GetAgentRole(ctx, roleID)
	if err != nil {
		return nil, svc.t("agent_role.get.failed", map[string]any{"Error": safeError(err)})
	}
	roleOptions := []MattermostDialogOption{{
		Text:  svc.t("dialog.role.option", map[string]any{"Role": role.Name, "Type": role.RoleType, "Project": role.ProjectID}),
		Value: strconv.FormatInt(role.ID, 10),
	}}
	variableOptions, errText := svc.agentRoleRuntimeVariableOptions(ctx, role.ID)
	if errText != "" {
		return nil, errText
	}
	return &MattermostDialog{
		SubmitURL:        svc.cfg.DialogSubmitURL,
		CallbackID:       dialogCallbackRoleRuntimeVarDetach,
		Title:            svc.t("dialog.role_runtime_var.detach.title", nil),
		IntroductionText: svc.t("dialog.role_runtime_var.detach.intro", nil),
		Elements: []MattermostDialogElement{
			{
				DisplayName: svc.t("dialog.role_runtime_var.field.role", nil),
				Name:        dialogFieldRole,
				Type:        "select",
				Default:     strconv.FormatInt(role.ID, 10),
				Options:     roleOptions,
			},
			{
				DisplayName: svc.t("dialog.role_runtime_var.field.variable", nil),
				Name:        dialogFieldRuntimeVarID,
				Type:        "select",
				Options:     variableOptions,
			},
		},
		SubmitLabel: svc.t("dialog.role_runtime_var.detach.submit", nil),
		State:       encodeDialogState(command),
	}, ""
}

func (svc *SlashCommandService) agentRoleDialog(ctx context.Context, command MenuActionCommand) (*MattermostDialog, string) {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return nil, svc.t("agent_role.storage_not_ready", nil)
	}
	role := entity.AgentRole{
		ProjectID:         selectedProjectID(command),
		RoleType:          "worker",
		PromptMode:        "template",
		OpenAIAccountName: "",
		GitHubAccountName: "",
		KubernetesAccess:  "read-only",
		SandboxMode:       "danger-full-access",
		Enabled:           true,
	}
	editMode := command.Resource == menuResourceAgentRole && strings.TrimSpace(command.ID) != ""
	if editMode {
		roleID, ok := parseInt64ID(command.ID)
		if !ok {
			return nil, svc.t("menu.entity.invalid", nil)
		}
		current, err := svc.cfg.Store.GetAgentRole(ctx, roleID)
		if err != nil {
			return nil, svc.t("agent_role.get.failed", map[string]any{"Error": safeError(err)})
		}
		role = current
	}
	if !editMode && role.ProjectID > 0 {
		if project, err := svc.cfg.Store.GetProject(ctx, role.ProjectID); err == nil {
			role.GitHubAccountName = strings.TrimSpace(project.GitHubAccountName)
		}
	}
	projectOptions, errText := svc.projectOptions(ctx, role.ProjectID)
	if errText != "" {
		return nil, errText
	}
	openAIOptions, errText := svc.optionalOpenAIAccountOptions(ctx, role.OpenAIAccountName)
	if errText != "" {
		return nil, errText
	}
	githubOptions, errText := svc.optionalGitHubAccountOptions(ctx, role.GitHubAccountName)
	if errText != "" {
		return nil, errText
	}
	titleID := "dialog.role.add.title"
	introID := "dialog.role.add.intro"
	submitID := "dialog.role.add.submit"
	if editMode {
		titleID = "dialog.role.edit.title"
		introID = "dialog.role.edit.intro"
		submitID = "dialog.role.edit.submit"
	}
	return &MattermostDialog{
		SubmitURL:        svc.cfg.DialogSubmitURL,
		CallbackID:       dialogCallbackAgentRoleUpsert,
		Title:            svc.t(titleID, nil),
		IntroductionText: svc.t(introID, nil),
		Elements: []MattermostDialogElement{
			{
				DisplayName: svc.t("dialog.project.field.project", nil),
				Name:        dialogFieldProjectID,
				Type:        "select",
				Default:     selectedIDString(role.ProjectID),
				Options:     projectOptions,
			},
			{
				DisplayName: svc.t("dialog.role.field.name", nil),
				Name:        dialogFieldRole,
				Type:        "text",
				Default:     role.Name,
				Placeholder: "backend-developer",
				HelpText:    svc.t("dialog.role.field.name.help", nil),
				MinLength:   2,
				MaxLength:   48,
			},
			{
				DisplayName: svc.t("dialog.role.field.bot_identity", nil),
				Name:        dialogFieldBotIdentity,
				Type:        "text",
				Default:     role.BotIdentity,
				Placeholder: "backend-dev-bot",
				HelpText:    svc.t("dialog.role.field.bot_identity.help", nil),
				Optional:    true,
				MinLength:   3,
				MaxLength:   48,
			},
			{
				DisplayName: svc.t("dialog.role.field.type", nil),
				Name:        dialogFieldRoleType,
				Type:        "select",
				Default:     defaultString(role.RoleType, "worker"),
				Options:     agentRoleTypeOptions(),
			},
			{
				DisplayName: svc.t("dialog.profile.field.openai", nil),
				Name:        dialogFieldOpenAIAccount,
				Type:        "select",
				Default:     optionalSelectDefault(role.OpenAIAccountName),
				Options:     openAIOptions,
			},
			{
				DisplayName: svc.t("dialog.profile.field.github", nil),
				Name:        dialogFieldGitHubAccount,
				Type:        "select",
				Default:     optionalSelectDefault(role.GitHubAccountName),
				Options:     githubOptions,
			},
			{
				DisplayName: svc.t("dialog.role.field.prompt_mode", nil),
				Name:        dialogFieldPromptMode,
				Type:        "select",
				Default:     defaultString(role.PromptMode, "raw"),
				Options: []MattermostDialogOption{
					{Text: svc.t("dialog.role.prompt.raw", nil), Value: "raw"},
					{Text: svc.t("dialog.role.prompt.template", nil), Value: "template"},
				},
			},
			{
				DisplayName: svc.t("dialog.role.field.prompt", nil),
				Name:        dialogFieldPromptTemplate,
				Type:        "textarea",
				Default:     role.PromptTemplate,
				HelpText:    svc.t("dialog.role.field.prompt.help", nil),
				Optional:    true,
				MaxLength:   12000,
			},
			{
				DisplayName: svc.t("dialog.profile.field.kubernetes", nil),
				Name:        dialogFieldKubernetesAccess,
				Type:        "select",
				Default:     defaultString(role.KubernetesAccess, "read-only"),
				Options: []MattermostDialogOption{
					{Text: svc.t("dialog.profile.kubernetes.read_only", nil), Value: "read-only"},
					{Text: svc.t("dialog.profile.kubernetes.cluster_admin", nil), Value: "cluster-admin"},
				},
			},
			{
				DisplayName: svc.t("dialog.profile.field.sandbox", nil),
				Name:        dialogFieldSandboxMode,
				Type:        "select",
				Default:     defaultString(role.SandboxMode, "danger-full-access"),
				Options: []MattermostDialogOption{
					{Text: "danger-full-access", Value: "danger-full-access"},
					{Text: "workspace-write", Value: "workspace-write"},
					{Text: "read-only", Value: "read-only"},
				},
			},
			{
				DisplayName: svc.t("dialog.profile.field.description", nil),
				Name:        dialogFieldDescription,
				Type:        "textarea",
				Default:     role.Description,
				Optional:    true,
				MaxLength:   1000,
			},
			{
				DisplayName: svc.t("dialog.profile.field.config", nil),
				Name:        dialogFieldConfigOverlay,
				Type:        "textarea",
				Default:     role.ConfigOverlay,
				Placeholder: "sandbox_mode = \"danger-full-access\"",
				HelpText:    svc.t("dialog.profile.field.config.help", nil),
				Optional:    true,
				MaxLength:   4000,
			},
			{
				DisplayName: svc.t("dialog.role.field.advanced", nil),
				Name:        dialogFieldAdvancedSettings,
				Type:        "textarea",
				Default:     role.AdvancedSettings,
				Placeholder: "{}",
				HelpText:    svc.t("dialog.role.field.advanced.help", nil),
				Optional:    true,
				MaxLength:   4000,
			},
		},
		SubmitLabel: svc.t(submitID, nil),
		State:       encodeDialogState(command),
	}, ""
}

func (svc *SlashCommandService) chatDialog(ctx context.Context, command MenuActionCommand) (*MattermostDialog, string) {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return nil, svc.t("chat.storage_not_ready", nil)
	}
	projectID := selectedProjectID(command)
	projectOptions, errText := svc.projectOptions(ctx, projectID)
	if errText != "" {
		return nil, errText
	}
	roleOptions, errText := svc.agentRoleOptions(ctx, projectID, true)
	if errText != "" {
		return nil, errText
	}
	repoOptions, errText := svc.projectRepositoryOptions(ctx, projectID, true)
	if errText != "" {
		return nil, errText
	}
	return &MattermostDialog{
		SubmitURL:        svc.cfg.DialogSubmitURL,
		CallbackID:       dialogCallbackChatCreate,
		Title:            svc.t("dialog.chat.create.title", nil),
		IntroductionText: svc.t("dialog.chat.create.intro", nil),
		Elements: []MattermostDialogElement{
			{
				DisplayName: svc.t("dialog.project.field.project", nil),
				Name:        dialogFieldProjectID,
				Type:        "select",
				Default:     selectedIDString(projectID),
				Options:     projectOptions,
			},
			{
				DisplayName: svc.t("dialog.chat.field.name", nil),
				Name:        dialogFieldChatName,
				Type:        "text",
				Placeholder: "Backend developer review",
				HelpText:    svc.t("dialog.chat.field.name.help", nil),
				MinLength:   2,
				MaxLength:   64,
			},
			{
				DisplayName: svc.t("dialog.chat.field.type", nil),
				Name:        dialogFieldChatType,
				Type:        "select",
				Default:     "worker_reviewer",
				Options:     chatTypeOptions(),
			},
			{
				DisplayName: svc.t("dialog.chat.field.primary_role", nil),
				Name:        dialogFieldPrimaryRoleID,
				Type:        "select",
				Options:     roleOptions,
			},
			{
				DisplayName: svc.t("dialog.chat.field.secondary_role", nil),
				Name:        dialogFieldSecondaryRoleID,
				Type:        "select",
				Optional:    true,
				Options:     svc.optionalDialogOptions(roleOptions),
			},
			{
				DisplayName: svc.t("dialog.chat.field.repository", nil),
				Name:        dialogFieldRepositoryID,
				Type:        "select",
				Optional:    true,
				Options:     repoOptions,
			},
			{
				DisplayName: svc.t("dialog.chat.field.issue", nil),
				Name:        dialogFieldRootIssue,
				Type:        "text",
				Placeholder: "https://github.com/org/repo/issues/123",
				HelpText:    svc.t("dialog.chat.field.issue.help", nil),
				Optional:    true,
				MaxLength:   240,
			},
			{
				DisplayName: svc.t("dialog.chat.field.policy", nil),
				Name:        dialogFieldWorkPolicy,
				Type:        "textarea",
				HelpText:    svc.t("dialog.chat.field.policy.help", nil),
				Optional:    true,
				MaxLength:   2000,
			},
		},
		SubmitLabel: svc.t("dialog.chat.create.submit", nil),
		State:       encodeDialogState(command),
	}, ""
}

func (svc *SlashCommandService) handleProjectDialogUpsert(ctx context.Context, command DialogSubmissionCommand, state mattermostDialogState) DialogSubmissionResult {
	input, fieldErrors := svc.projectDialogInput(command.Submission)
	if len(fieldErrors) > 0 {
		return DialogSubmissionResult{StatusCode: 200, Errors: fieldErrors}
	}
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("project.storage_not_ready", nil)}
	}
	editMode := state.ResourceType == menuResourceProject && strings.TrimSpace(state.ResourceID) != ""
	var current entity.Project
	if editMode {
		projectID, ok := parseInt64ID(state.ResourceID)
		if !ok {
			return DialogSubmissionResult{StatusCode: 200, Error: svc.t("menu.entity.invalid", nil)}
		}
		var err error
		current, err = svc.cfg.Store.GetProject(ctx, projectID)
		if err != nil {
			return DialogSubmissionResult{StatusCode: 200, Error: svc.t("project.get.failed", map[string]any{"Error": safeError(err)})}
		}
		if input.Slug != current.Slug {
			return DialogSubmissionResult{StatusCode: 200, Errors: map[string]string{dialogFieldProjectSlug: svc.t("dialog.project.slug_edit_not_supported", nil)}}
		}
		input.MattermostTeamID = current.MattermostTeamID
	}
	if strings.TrimSpace(input.GitHubAccountName) != "" {
		account, err := svc.cfg.Store.GetGitHubAccount(ctx, input.GitHubAccountName)
		if err != nil {
			return DialogSubmissionResult{StatusCode: 200, Errors: map[string]string{dialogFieldGitHubAccount: svc.t("dialog.github.not_found", map[string]any{"Account": input.GitHubAccountName})}}
		}
		if account.Status != "configured" || strings.TrimSpace(account.SecretRef) == "" {
			return DialogSubmissionResult{StatusCode: 200, Errors: map[string]string{dialogFieldGitHubAccount: svc.t("repo.onboard.account_not_configured", map[string]any{"Account": account.Name})}}
		}
	}
	if strings.TrimSpace(input.GitHubOwner) != "" && strings.TrimSpace(input.GitHubAccountName) == "" {
		return DialogSubmissionResult{StatusCode: 200, Errors: map[string]string{dialogFieldGitHubAccount: svc.t("project.github_account.required", map[string]any{"Project": input.Name})}}
	}
	if svc.cfg.ChannelManager != nil {
		team, _, err := svc.cfg.ChannelManager.EnsureProjectTeam(ctx, input.Slug, input.Name, command.UserID)
		if err != nil {
			return DialogSubmissionResult{StatusCode: 200, Error: svc.t("project.team.failed", map[string]any{"Error": safeError(err)})}
		}
		input.MattermostTeamID = team.ID
	}
	project, created, err := svc.cfg.Store.UpsertProject(ctx, input)
	if err != nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("project.save.failed", map[string]any{"Error": safeError(err)})}
	}
	project, err = svc.ensureProjectRunsChannel(ctx, project)
	if err != nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("project.runs_channel.failed", map[string]any{"Error": safeError(err)})}
	}
	svc.recordProjectAudit(ctx, mattermostDialogSlash(state, command), "project.upserted", project.Slug, "project upserted from Mattermost dialog")
	stateID := "label.updated"
	if created {
		stateID = "label.created"
	}
	text := svc.t("project.save.result", map[string]any{
		"State":  svc.t(stateID, nil),
		"ID":     project.ID,
		"Name":   project.Name,
		"Slug":   project.Slug,
		"Team":   emptyAsUnknown(project.MattermostTeamID),
		"GitHub": projectGitHubSummary(project),
	})
	if editMode && strings.TrimSpace(current.GitHubAccountName) != strings.TrimSpace(project.GitHubAccountName) {
		text += svc.invalidateIdleAgentSessionsForRolesText(ctx, svc.projectDefaultGitHubRoleIDs(ctx, project.ID))
	}
	card := svc.projectEntityCard(ctx, MenuActionCommand{View: menuViewProjects, ID: strconv.FormatInt(project.ID, 10), ChannelID: state.ChannelID, PostID: state.PostID})
	card.Text = text + "\n\n" + card.Text
	return DialogSubmissionResult{StatusCode: 200, Card: card}
}

func (svc *SlashCommandService) handleProjectRepositoryBindDialog(ctx context.Context, command DialogSubmissionCommand, state mattermostDialogState) DialogSubmissionResult {
	projectID, ok := parseInt64ID(submissionString(command.Submission, dialogFieldProjectID))
	if !ok {
		return DialogSubmissionResult{StatusCode: 200, Errors: map[string]string{dialogFieldProjectID: svc.t("dialog.project.project_invalid", nil)}}
	}
	repositoryID, ok := parseInt64ID(submissionString(command.Submission, dialogFieldRepositoryID))
	if !ok {
		return DialogSubmissionResult{StatusCode: 200, Errors: map[string]string{dialogFieldRepositoryID: svc.t("dialog.project_repo.repository_invalid", nil)}}
	}
	isDefault := submissionString(command.Submission, dialogFieldStatus) != "false"
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("project.storage_not_ready", nil)}
	}
	binding, created, err := svc.cfg.Store.UpsertProjectRepository(ctx, adminrepo.UpsertProjectRepositoryInput{
		ProjectID:    projectID,
		RepositoryID: repositoryID,
		IsDefault:    isDefault,
	})
	if err != nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("project_repo.save.failed", map[string]any{"Error": safeError(err)})}
	}
	svc.recordProjectAudit(ctx, mattermostDialogSlash(state, command), "project.repository_bound", strconv.FormatInt(projectID, 10)+":"+binding.FullName(), "repository bound to project from Mattermost dialog")
	stateID := "label.updated"
	if created {
		stateID = "label.created"
	}
	text := svc.t("project_repo.save.result", map[string]any{
		"State":      svc.t(stateID, nil),
		"Repository": binding.Provider + ":" + binding.FullName(),
		"Project":    projectID,
		"Default":    binding.IsDefault,
	})
	return DialogSubmissionResult{StatusCode: 200, Card: svc.dialogResultCard(ctx, state, command, text)}
}

func (svc *SlashCommandService) handleProjectRuntimeVariableDialog(ctx context.Context, command DialogSubmissionCommand, state mattermostDialogState) DialogSubmissionResult {
	input, value, fieldErrors := svc.projectRuntimeVariableDialogInput(command.Submission)
	editMode := state.ResourceType == menuResourceRuntimeVar && strings.TrimSpace(state.ResourceID) != ""
	if len(fieldErrors) > 0 {
		return DialogSubmissionResult{StatusCode: 200, Errors: fieldErrors}
	}
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("project.storage_not_ready", nil)}
	}
	if svc.cfg.RuntimeRunner == nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("runtime.not_configured", nil)}
	}
	project, err := svc.cfg.Store.GetProject(ctx, input.ProjectID)
	if err != nil {
		return DialogSubmissionResult{StatusCode: 200, Errors: map[string]string{dialogFieldProjectID: svc.t("dialog.project.project_invalid", nil)}}
	}
	var current entity.ProjectRuntimeVariable
	if editMode {
		variableID, ok := parseInt64ID(state.ResourceID)
		if !ok {
			return DialogSubmissionResult{StatusCode: 200, Error: svc.t("menu.entity.invalid", nil)}
		}
		current, err = svc.cfg.Store.GetProjectRuntimeVariable(ctx, variableID)
		if err != nil {
			return DialogSubmissionResult{StatusCode: 200, Error: svc.t("runtime_var.get.failed", map[string]any{"Error": safeError(err)})}
		}
		if input.ProjectID != current.ProjectID {
			return DialogSubmissionResult{StatusCode: 200, Errors: map[string]string{dialogFieldProjectID: svc.t("dialog.runtime_var.project_edit_not_supported", nil)}}
		}
		if input.Name != current.Name {
			return DialogSubmissionResult{StatusCode: 200, Errors: map[string]string{dialogFieldRuntimeVarName: svc.t("dialog.runtime_var.name_edit_not_supported", nil)}}
		}
		input.Slug = current.Slug
		input.SecretRef = current.SecretRef
		input.SecretKey = current.SecretKey
	}
	if strings.TrimSpace(input.Slug) == "" {
		input.Slug = runtimeVariableSlug(input.Name)
	}
	if strings.TrimSpace(input.SecretKey) == "" {
		input.SecretKey = "value"
	}
	if strings.TrimSpace(input.SecretRef) == "" {
		input.SecretRef = projectRuntimeVariableSecretName(project, input.Name)
	}
	if strings.TrimSpace(value) == "" && !editMode {
		return DialogSubmissionResult{StatusCode: 200, Errors: map[string]string{dialogFieldRuntimeVarValue: svc.t("dialog.runtime_var.value_required", nil)}}
	}
	secretCreated := false
	if strings.TrimSpace(value) != "" {
		secret, err := svc.cfg.RuntimeRunner.UpsertProjectRuntimeVariableSecret(ctx, runtimerepo.ProjectRuntimeVariableSecretInput{
			ProjectSlug: project.Slug,
			Variable: runtimerepo.RuntimeEnvVar{
				Name:       input.Name,
				SecretName: input.SecretRef,
				SecretKey:  input.SecretKey,
				Sensitive:  input.Sensitive,
			},
			Value: value,
		})
		if err != nil {
			return DialogSubmissionResult{StatusCode: 200, Error: svc.t("runtime_var.secret_save.failed", map[string]any{"Error": safeError(err)})}
		}
		input.SecretRef = secret.SecretName
		secretCreated = secret.Created
	}
	variable, created, err := svc.cfg.Store.UpsertProjectRuntimeVariable(ctx, input)
	if err != nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("runtime_var.save.failed", map[string]any{"Error": safeError(err)})}
	}
	svc.recordProjectAudit(ctx, mattermostDialogSlash(state, command), "project.runtime_variable.upserted", variable.Name, "project runtime variable upserted from Mattermost dialog")
	stateID := "label.updated"
	if created {
		stateID = "label.created"
	}
	text := svc.t("runtime_var.save.result", map[string]any{
		"State":         svc.t(stateID, nil),
		"Name":          variable.Name,
		"Project":       project.Name,
		"Secret":        maskedSecretRef(variable.SecretRef, variable.SecretKey),
		"Enabled":       variable.Enabled,
		"SecretCreated": secretCreated,
	})
	if state.ResourceType == menuResourceAgentRole && strings.TrimSpace(state.ResourceID) != "" {
		roleID, ok := parseInt64ID(state.ResourceID)
		if !ok {
			return DialogSubmissionResult{StatusCode: 200, Error: svc.t("dialog.role_runtime_var.role_invalid", nil)}
		}
		role, err := svc.cfg.Store.GetAgentRole(ctx, roleID)
		if err != nil {
			return DialogSubmissionResult{StatusCode: 200, Error: svc.t("dialog.role_runtime_var.role_invalid", nil)}
		}
		if role.ProjectID != variable.ProjectID {
			return DialogSubmissionResult{StatusCode: 200, Errors: map[string]string{dialogFieldProjectID: svc.t("dialog.role_runtime_var.project_mismatch", nil)}}
		}
		binding, bindingCreated, err := svc.cfg.Store.UpsertAgentRoleRuntimeVariable(ctx, adminrepo.UpsertAgentRoleRuntimeVariableInput{
			RoleID:     role.ID,
			VariableID: variable.ID,
		})
		if err != nil {
			return DialogSubmissionResult{StatusCode: 200, Error: svc.t("runtime_var.save.attach_failed", map[string]any{"Error": safeError(err)})}
		}
		svc.recordProjectAudit(ctx, mattermostDialogSlash(state, command), "agent_role.runtime_variable.attached", role.Name+":"+variable.Name, "runtime variable attached to agent role after creation from Mattermost dialog")
		bindingStateID := "label.updated"
		if bindingCreated {
			bindingStateID = "label.created"
		}
		text += "\n\n" + svc.t("role_runtime_var.attach.result", map[string]any{
			"State":    svc.t(bindingStateID, nil),
			"Role":     binding.RoleName,
			"Variable": binding.Name,
		})
		text += svc.invalidateIdleAgentSessionsForRolesText(ctx, []int64{role.ID})
		card := svc.roleEntityCard(ctx, MenuActionCommand{View: menuViewRoles, ID: strconv.FormatInt(role.ID, 10), ChannelID: state.ChannelID, PostID: state.PostID})
		card.Text = text + "\n\n" + card.Text
		return DialogSubmissionResult{StatusCode: 200, Card: card}
	}
	if editMode {
		text += svc.invalidateIdleAgentSessionsForRolesText(ctx, svc.roleIDsUsingRuntimeVariable(ctx, variable.ProjectID, variable.ID))
	}
	card := svc.projectRuntimeVariableEntityCard(ctx, MenuActionCommand{View: menuViewProjects, ID: strconv.FormatInt(variable.ID, 10), ChannelID: state.ChannelID, PostID: state.PostID})
	card.Text = text + "\n\n" + card.Text
	return DialogSubmissionResult{StatusCode: 200, Card: card}
}

func (svc *SlashCommandService) handleRoleRuntimeVariableAttachDialog(ctx context.Context, command DialogSubmissionCommand, state mattermostDialogState) DialogSubmissionResult {
	roleID, variableID, fieldErrors := svc.roleRuntimeVariableDialogInput(command.Submission)
	if len(fieldErrors) > 0 {
		return DialogSubmissionResult{StatusCode: 200, Errors: fieldErrors}
	}
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("project.storage_not_ready", nil)}
	}
	role, err := svc.cfg.Store.GetAgentRole(ctx, roleID)
	if err != nil {
		return DialogSubmissionResult{StatusCode: 200, Errors: map[string]string{dialogFieldRole: svc.t("dialog.role_runtime_var.role_invalid", nil)}}
	}
	variable, err := svc.cfg.Store.GetProjectRuntimeVariable(ctx, variableID)
	if err != nil {
		return DialogSubmissionResult{StatusCode: 200, Errors: map[string]string{dialogFieldRuntimeVarID: svc.t("dialog.role_runtime_var.variable_invalid", nil)}}
	}
	if role.ProjectID != variable.ProjectID {
		return DialogSubmissionResult{StatusCode: 200, Errors: map[string]string{dialogFieldRuntimeVarID: svc.t("dialog.role_runtime_var.project_mismatch", nil)}}
	}
	binding, created, err := svc.cfg.Store.UpsertAgentRoleRuntimeVariable(ctx, adminrepo.UpsertAgentRoleRuntimeVariableInput{
		RoleID:     role.ID,
		VariableID: variable.ID,
	})
	if err != nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("role_runtime_var.attach.failed", map[string]any{"Error": safeError(err)})}
	}
	svc.recordProjectAudit(ctx, mattermostDialogSlash(state, command), "agent_role.runtime_variable.attached", role.Name+":"+variable.Name, "runtime variable attached to agent role from Mattermost dialog")
	stateID := "label.updated"
	if created {
		stateID = "label.created"
	}
	text := svc.t("role_runtime_var.attach.result", map[string]any{
		"State":    svc.t(stateID, nil),
		"Role":     binding.RoleName,
		"Variable": binding.Name,
	})
	text += svc.invalidateIdleAgentSessionsForRolesText(ctx, []int64{role.ID})
	card := svc.roleEntityCard(ctx, MenuActionCommand{View: menuViewRoles, ID: strconv.FormatInt(role.ID, 10), ChannelID: state.ChannelID, PostID: state.PostID})
	card.Text = text + "\n\n" + card.Text
	return DialogSubmissionResult{StatusCode: 200, Card: card}
}

func (svc *SlashCommandService) handleRoleRuntimeVariableDetachDialog(ctx context.Context, command DialogSubmissionCommand, state mattermostDialogState) DialogSubmissionResult {
	roleID, variableID, fieldErrors := svc.roleRuntimeVariableDialogInput(command.Submission)
	if len(fieldErrors) > 0 {
		return DialogSubmissionResult{StatusCode: 200, Errors: fieldErrors}
	}
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("project.storage_not_ready", nil)}
	}
	binding, err := svc.cfg.Store.DeleteAgentRoleRuntimeVariable(ctx, roleID, variableID)
	if err != nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("role_runtime_var.detach.failed", map[string]any{"Error": safeError(err)})}
	}
	svc.recordProjectAudit(ctx, mattermostDialogSlash(state, command), "agent_role.runtime_variable.detached", binding.RoleName+":"+binding.Name, "runtime variable detached from agent role from Mattermost dialog")
	text := svc.t("role_runtime_var.detach.result", map[string]any{
		"Role":     binding.RoleName,
		"Variable": binding.Name,
	})
	text += svc.invalidateIdleAgentSessionsForRolesText(ctx, []int64{roleID})
	card := svc.roleEntityCard(ctx, MenuActionCommand{View: menuViewRoles, ID: strconv.FormatInt(roleID, 10), ChannelID: state.ChannelID, PostID: state.PostID})
	card.Text = text + "\n\n" + card.Text
	return DialogSubmissionResult{StatusCode: 200, Card: card}
}

func (svc *SlashCommandService) handleAgentRoleDialogUpsert(ctx context.Context, command DialogSubmissionCommand, state mattermostDialogState) DialogSubmissionResult {
	input, fieldErrors := svc.agentRoleDialogInput(command.Submission)
	editMode := state.ResourceType == menuResourceAgentRole && strings.TrimSpace(state.ResourceID) != ""
	if editMode {
		roleID, ok := parseInt64ID(state.ResourceID)
		if !ok {
			return DialogSubmissionResult{StatusCode: 200, Error: svc.t("menu.entity.invalid", nil)}
		}
		current, err := svc.cfg.Store.GetAgentRole(ctx, roleID)
		if err != nil {
			return DialogSubmissionResult{StatusCode: 200, Error: svc.t("agent_role.get.failed", map[string]any{"Error": safeError(err)})}
		}
		if input.ProjectID != current.ProjectID || input.Name != current.Name {
			fieldErrors[dialogFieldRole] = svc.t("dialog.role.rename_not_supported", nil)
		}
	}
	if len(fieldErrors) > 0 {
		return DialogSubmissionResult{StatusCode: 200, Errors: fieldErrors}
	}
	if !editMode && strings.TrimSpace(input.PromptTemplate) == "" && input.PromptMode == "template" {
		if seed, ok := promptSeedForAgentRole(input.Name, input.RoleType); ok {
			body, err := promptSeedMarkdown(seed)
			if err != nil {
				return DialogSubmissionResult{StatusCode: 200, Errors: map[string]string{dialogFieldPromptTemplate: svc.t("prompt.set.render_failed", map[string]any{"Error": safeError(err)})}}
			}
			input.PromptTemplate = body
		}
	}
	if strings.TrimSpace(input.PromptTemplate) != "" {
		if _, err := RenderRolePromptTemplate(input.PromptTemplate, SampleRolePromptData(input.Name, input.RoleType, svc.promptTemplateLocaleData())); err != nil {
			return DialogSubmissionResult{StatusCode: 200, Errors: map[string]string{dialogFieldPromptTemplate: svc.t("prompt.set.render_failed", map[string]any{"Error": safeError(err)})}}
		}
	} else {
		input.PromptMode = "raw"
	}
	role, created, err := svc.cfg.Store.UpsertAgentRole(ctx, input)
	if err != nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("agent_role.save.failed", map[string]any{"Error": safeError(err)})}
	}
	project, err := svc.cfg.Store.GetProject(ctx, role.ProjectID)
	if err != nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("project.get.failed", map[string]any{"Error": safeError(err)})}
	}
	if _, err := svc.ensureRoleBotIdentity(ctx, project, role, ""); err != nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("agent_role.bot_identity.failed", map[string]any{"Error": safeError(err)})}
	}
	svc.recordProjectAudit(ctx, mattermostDialogSlash(state, command), "agent_role.upserted", role.Name, "agent role upserted from Mattermost dialog")
	stateID := "label.updated"
	if created {
		stateID = "label.created"
	}
	text := svc.t("agent_role.save.result", map[string]any{
		"State":   svc.t(stateID, nil),
		"ID":      role.ID,
		"Project": svc.roleProjectLabel(ctx, role.ProjectID),
		"Role":    role.Name,
		"Type":    role.RoleType,
		"Prompt":  rolePromptLabel(svc, role),
	})
	if !created {
		text += svc.invalidateIdleAgentSessionsForRolesText(ctx, []int64{role.ID})
	}
	return DialogSubmissionResult{StatusCode: 200, Card: svc.dialogResultCard(ctx, state, command, text)}
}

func (svc *SlashCommandService) handleChatCreateDialog(ctx context.Context, command DialogSubmissionCommand, state mattermostDialogState) DialogSubmissionResult {
	input, fieldErrors := svc.chatDialogInput(command.Submission)
	if len(fieldErrors) > 0 {
		return DialogSubmissionResult{StatusCode: 200, Errors: fieldErrors}
	}
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("chat.storage_not_ready", nil)}
	}
	project, err := svc.cfg.Store.GetProject(ctx, input.ProjectID)
	if err != nil {
		return DialogSubmissionResult{StatusCode: 200, Errors: map[string]string{dialogFieldProjectID: svc.t("dialog.project.project_invalid", nil)}}
	}
	roles := make([]entity.AgentRole, 0, len(input.RoleIDs))
	for _, roleID := range input.RoleIDs {
		role, err := svc.cfg.Store.GetAgentRole(ctx, roleID)
		if err != nil || role.ProjectID != project.ID || !role.Enabled {
			return DialogSubmissionResult{StatusCode: 200, Errors: map[string]string{dialogFieldPrimaryRoleID: svc.t("dialog.chat.role_invalid", nil)}}
		}
		roles = append(roles, role)
	}
	for _, repositoryID := range input.RepositoryIDs {
		if _, _, err := svc.cfg.Store.UpsertProjectRepository(ctx, adminrepo.UpsertProjectRepositoryInput{
			ProjectID:    project.ID,
			RepositoryID: repositoryID,
			IsDefault:    false,
		}); err != nil {
			return DialogSubmissionResult{StatusCode: 200, Errors: map[string]string{dialogFieldRepositoryID: svc.t("dialog.project_repo.repository_invalid", nil)}}
		}
	}
	if svc.cfg.ChannelManager != nil {
		if _, _, err := svc.cfg.ChannelManager.EnsureProjectTeam(ctx, project.Slug, project.Name, command.UserID); err != nil {
			return DialogSubmissionResult{StatusCode: 200, Error: svc.t("project.team.failed", map[string]any{"Error": safeError(err)})}
		}
		channel, _, err := svc.cfg.ChannelManager.EnsureProjectChannel(ctx, project.Slug, input.Slug, input.Name, true, []string{command.UserID})
		if err != nil {
			return DialogSubmissionResult{StatusCode: 200, Error: svc.t("chat.channel.failed", map[string]any{"Error": safeError(err)})}
		}
		input.MattermostChannelID = channel.ID
		for _, role := range roles {
			if _, err := svc.ensureRoleBotIdentity(ctx, project, role, channel.ID); err != nil {
				return DialogSubmissionResult{StatusCode: 200, Error: svc.t("agent_role.bot_identity.failed", map[string]any{"Error": safeError(err)})}
			}
		}
	}
	chat, created, err := svc.cfg.Store.CreateChat(ctx, input)
	if err != nil {
		return DialogSubmissionResult{StatusCode: 200, Error: svc.t("chat.save.failed", map[string]any{"Error": safeError(err)})}
	}
	svc.recordProjectAudit(ctx, mattermostDialogSlash(state, command), "chat.upserted", chat.Slug, "chat upserted from Mattermost dialog")
	stateID := "label.updated"
	if created {
		stateID = "label.created"
	}
	text := svc.t("chat.save.result", map[string]any{
		"State":   svc.t(stateID, nil),
		"ID":      chat.ID,
		"Project": project.Name,
		"Chat":    chat.Name,
		"Channel": emptyAsUnknown(chat.MattermostChannelID),
	})
	card := svc.chatEntityCard(ctx, MenuActionCommand{View: menuViewChats, ID: strconv.FormatInt(chat.ID, 10), ChannelID: state.ChannelID, PostID: state.PostID})
	card.Text = text + "\n\n" + card.Text
	return DialogSubmissionResult{StatusCode: 200, Card: card}
}

func (svc *SlashCommandService) projectDialogInput(submission map[string]any) (adminrepo.UpsertProjectInput, map[string]string) {
	fieldErrors := map[string]string{}
	name := strings.TrimSpace(submissionString(submission, dialogFieldProjectName))
	if len(name) < 2 {
		fieldErrors[dialogFieldProjectName] = svc.t("dialog.project.name_invalid", nil)
	}
	slug := strings.TrimSpace(submissionString(submission, dialogFieldProjectSlug))
	if slug == "" {
		slug = slugifyName(name, "project")
	}
	if !validMattermostName(slug) {
		fieldErrors[dialogFieldProjectSlug] = svc.t("dialog.project.slug_invalid", nil)
	}
	advanced := strings.TrimSpace(submissionString(submission, dialogFieldAdvancedSettings))
	if advanced != "" && !looksLikeJSONObject(advanced) {
		fieldErrors[dialogFieldAdvancedSettings] = svc.t("dialog.advanced.json_invalid", nil)
	}
	githubAccount := optionalSubmissionString(submission, dialogFieldGitHubAccount)
	if githubAccount != "" {
		if _, err := parseOpenAIAccountName(githubAccount); err != nil {
			fieldErrors[dialogFieldGitHubAccount] = svc.t("parse.github_account.invalid", nil)
		}
	}
	githubOwner := strings.ToLower(strings.TrimSpace(submissionString(submission, dialogFieldGitHubOwner)))
	if githubOwner != "" && !validGitHubUsername(githubOwner) {
		fieldErrors[dialogFieldGitHubOwner] = svc.t("dialog.project.github_owner_invalid", nil)
	}
	githubOwnerType := strings.ToLower(defaultString(submissionString(submission, dialogFieldGitHubOwnerType), "org"))
	if githubOwner == "" {
		githubOwnerType = ""
	} else if githubOwnerType != "org" && githubOwnerType != "user" {
		fieldErrors[dialogFieldGitHubOwnerType] = svc.t("dialog.project.github_owner_type_invalid", nil)
	}
	if len(fieldErrors) > 0 {
		return adminrepo.UpsertProjectInput{}, fieldErrors
	}
	return adminrepo.UpsertProjectInput{
		Name:              name,
		Slug:              slug,
		GitHubAccountName: githubAccount,
		GitHubOwner:       githubOwner,
		GitHubOwnerType:   githubOwnerType,
		Description:       strings.TrimSpace(submissionString(submission, dialogFieldDescription)),
		AdvancedSettings:  advanced,
	}, nil
}

func (svc *SlashCommandService) projectRuntimeVariableDialogInput(submission map[string]any) (adminrepo.UpsertProjectRuntimeVariableInput, string, map[string]string) {
	fieldErrors := map[string]string{}
	projectID, ok := parseInt64ID(submissionString(submission, dialogFieldProjectID))
	if !ok {
		fieldErrors[dialogFieldProjectID] = svc.t("dialog.project.project_invalid", nil)
	}
	name := strings.ToUpper(strings.TrimSpace(submissionString(submission, dialogFieldRuntimeVarName)))
	if !validRuntimeVariableName(name) {
		fieldErrors[dialogFieldRuntimeVarName] = svc.t("dialog.runtime_var.name_invalid", nil)
	}
	enabled, ok := parseSubmittedBool(submissionString(submission, dialogFieldEnabled), true)
	if !ok {
		fieldErrors[dialogFieldEnabled] = svc.t("dialog.runtime_var.enabled_invalid", nil)
	}
	sensitive, ok := parseSubmittedBool(submissionString(submission, dialogFieldSensitive), true)
	if !ok {
		fieldErrors[dialogFieldSensitive] = svc.t("dialog.runtime_var.sensitive_invalid", nil)
	}
	if len(fieldErrors) > 0 {
		return adminrepo.UpsertProjectRuntimeVariableInput{}, "", fieldErrors
	}
	return adminrepo.UpsertProjectRuntimeVariableInput{
		ProjectID:   projectID,
		Name:        name,
		Slug:        runtimeVariableSlug(name),
		Description: strings.TrimSpace(submissionString(submission, dialogFieldDescription)),
		SecretKey:   "value",
		Sensitive:   sensitive,
		Enabled:     enabled,
	}, submissionString(submission, dialogFieldRuntimeVarValue), nil
}

func (svc *SlashCommandService) roleRuntimeVariableDialogInput(submission map[string]any) (int64, int64, map[string]string) {
	fieldErrors := map[string]string{}
	roleID, ok := parseInt64ID(submissionString(submission, dialogFieldRole))
	if !ok {
		fieldErrors[dialogFieldRole] = svc.t("dialog.role_runtime_var.role_invalid", nil)
	}
	variableID, ok := parseInt64ID(submissionString(submission, dialogFieldRuntimeVarID))
	if !ok {
		fieldErrors[dialogFieldRuntimeVarID] = svc.t("dialog.role_runtime_var.variable_invalid", nil)
	}
	return roleID, variableID, fieldErrors
}

func (svc *SlashCommandService) agentRoleDialogInput(submission map[string]any) (adminrepo.UpsertAgentRoleInput, map[string]string) {
	fieldErrors := map[string]string{}
	projectID, ok := parseInt64ID(submissionString(submission, dialogFieldProjectID))
	if !ok {
		fieldErrors[dialogFieldProjectID] = svc.t("dialog.project.project_invalid", nil)
	}
	name := slugifyName(submissionString(submission, dialogFieldRole), "role")
	if !validRuntimeRunID(name) {
		fieldErrors[dialogFieldRole] = svc.t("parse.profile.invalid", nil)
	}
	roleType := strings.ToLower(strings.TrimSpace(submissionString(submission, dialogFieldRoleType)))
	if !validAgentRoleType(roleType) {
		fieldErrors[dialogFieldRoleType] = svc.t("dialog.role.type_invalid", nil)
	}
	botIdentity := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(submissionString(submission, dialogFieldBotIdentity)), "@"))
	if botIdentity != "" && !validMattermostBotUsername(botIdentity) {
		fieldErrors[dialogFieldBotIdentity] = svc.t("dialog.role.bot_identity_invalid", nil)
	}
	openAIAccount := optionalSubmissionString(submission, dialogFieldOpenAIAccount)
	if openAIAccount != "" {
		if _, err := parseOpenAIAccountName(openAIAccount); err != nil {
			fieldErrors[dialogFieldOpenAIAccount] = svc.t("parse.openai_account.invalid", nil)
		}
	}
	githubAccount := optionalSubmissionString(submission, dialogFieldGitHubAccount)
	if githubAccount != "" {
		if _, err := parseOpenAIAccountName(githubAccount); err != nil {
			fieldErrors[dialogFieldGitHubAccount] = svc.t("parse.github_account.invalid", nil)
		}
	}
	promptMode := strings.ToLower(defaultString(submissionString(submission, dialogFieldPromptMode), "raw"))
	if promptMode != "raw" && promptMode != "template" {
		fieldErrors[dialogFieldPromptMode] = svc.t("dialog.role.prompt_mode_invalid", nil)
	}
	kubernetesAccess := strings.ToLower(defaultString(submissionString(submission, dialogFieldKubernetesAccess), "read-only"))
	if !validKubernetesAccess(kubernetesAccess) {
		fieldErrors[dialogFieldKubernetesAccess] = svc.t("dialog.profile.kubernetes_invalid", nil)
	}
	sandboxMode := strings.ToLower(defaultString(submissionString(submission, dialogFieldSandboxMode), "danger-full-access"))
	if !validSandboxMode(sandboxMode) {
		fieldErrors[dialogFieldSandboxMode] = svc.t("dialog.profile.sandbox_invalid", nil)
	}
	advanced := strings.TrimSpace(submissionString(submission, dialogFieldAdvancedSettings))
	if advanced != "" && !looksLikeJSONObject(advanced) {
		fieldErrors[dialogFieldAdvancedSettings] = svc.t("dialog.advanced.json_invalid", nil)
	}
	if len(fieldErrors) > 0 {
		return adminrepo.UpsertAgentRoleInput{}, fieldErrors
	}
	return adminrepo.UpsertAgentRoleInput{
		ProjectID:         projectID,
		Name:              name,
		RoleType:          roleType,
		Description:       strings.TrimSpace(submissionString(submission, dialogFieldDescription)),
		PromptTemplate:    strings.TrimSpace(submissionString(submission, dialogFieldPromptTemplate)),
		PromptMode:        promptMode,
		GitHubAccountName: githubAccount,
		OpenAIAccountName: openAIAccount,
		KubernetesAccess:  kubernetesAccess,
		SandboxMode:       sandboxMode,
		ConfigOverlay:     strings.TrimSpace(submissionString(submission, dialogFieldConfigOverlay)),
		AdvancedSettings:  advanced,
		Enabled:           true,
		BotIdentity:       botIdentity,
	}, nil
}

func (svc *SlashCommandService) chatDialogInput(submission map[string]any) (adminrepo.CreateChatInput, map[string]string) {
	fieldErrors := map[string]string{}
	projectID, ok := parseInt64ID(submissionString(submission, dialogFieldProjectID))
	if !ok {
		fieldErrors[dialogFieldProjectID] = svc.t("dialog.project.project_invalid", nil)
	}
	name := strings.TrimSpace(submissionString(submission, dialogFieldChatName))
	if len(name) < 2 {
		fieldErrors[dialogFieldChatName] = svc.t("dialog.chat.name_invalid", nil)
	}
	chatType := strings.ToLower(defaultString(submissionString(submission, dialogFieldChatType), "custom"))
	if !validChatType(chatType) {
		fieldErrors[dialogFieldChatType] = svc.t("dialog.chat.type_invalid", nil)
	}
	primaryRoleID, ok := parseInt64ID(submissionString(submission, dialogFieldPrimaryRoleID))
	if !ok {
		fieldErrors[dialogFieldPrimaryRoleID] = svc.t("dialog.chat.role_invalid", nil)
	}
	roleIDs := []int64{primaryRoleID}
	if secondaryRoleID, ok := parseInt64ID(optionalSubmissionString(submission, dialogFieldSecondaryRoleID)); ok && secondaryRoleID != primaryRoleID {
		roleIDs = append(roleIDs, secondaryRoleID)
	}
	var repositoryIDs []int64
	if repositoryID, ok := parseInt64ID(optionalSubmissionString(submission, dialogFieldRepositoryID)); ok {
		repositoryIDs = append(repositoryIDs, repositoryID)
	}
	if len(fieldErrors) > 0 {
		return adminrepo.CreateChatInput{}, fieldErrors
	}
	return adminrepo.CreateChatInput{
		ProjectID:       projectID,
		Name:            name,
		Slug:            slugifyName(name, "chat"),
		Description:     strings.TrimSpace(submissionString(submission, dialogFieldDescription)),
		ChatType:        chatType,
		RootGitHubIssue: strings.TrimSpace(submissionString(submission, dialogFieldRootIssue)),
		WorkPolicy:      strings.TrimSpace(submissionString(submission, dialogFieldWorkPolicy)),
		RoleIDs:         roleIDs,
		RepositoryIDs:   repositoryIDs,
	}, nil
}

func (svc *SlashCommandService) projectOptions(ctx context.Context, selected int64) ([]MattermostDialogOption, string) {
	projects, err := svc.cfg.Store.ListProjects(ctx, 100)
	if err != nil {
		return nil, svc.t("project.list.failed", map[string]any{"Error": safeError(err)})
	}
	if len(projects) == 0 {
		return nil, svc.t("project.list.empty", nil)
	}
	options := make([]MattermostDialogOption, 0, len(projects))
	for _, project := range projects {
		options = append(options, MattermostDialogOption{
			Text:  svc.t("dialog.project.option", map[string]any{"Project": project.Name, "Slug": project.Slug}),
			Value: strconv.FormatInt(project.ID, 10),
		})
	}
	return ensureDialogOption(options, selectedIDString(selected)), ""
}

func (svc *SlashCommandService) repositoryEntityOptions(ctx context.Context, selected int64) ([]MattermostDialogOption, string) {
	repositories, err := svc.cfg.Store.ListRepositories(ctx, 100)
	if err != nil {
		return nil, svc.t("repo.list.read_failed", map[string]any{"Error": safeError(err)})
	}
	if len(repositories) == 0 {
		return nil, svc.t("repo.list.empty", nil)
	}
	sort.Slice(repositories, func(i int, j int) bool { return repositories[i].FullName() < repositories[j].FullName() })
	options := make([]MattermostDialogOption, 0, len(repositories))
	for _, repo := range repositories {
		options = append(options, MattermostDialogOption{
			Text:  svc.t("dialog.repo.option", map[string]any{"Repository": repo.FullName(), "Branch": repo.DefaultBranch}),
			Value: strconv.FormatInt(repo.ID, 10),
		})
	}
	return ensureDialogOption(options, selectedIDString(selected)), ""
}

func (svc *SlashCommandService) projectRepositoryOptions(ctx context.Context, projectID int64, allowEmpty bool) ([]MattermostDialogOption, string) {
	var options []MattermostDialogOption
	if allowEmpty {
		options = append(options, MattermostDialogOption{Text: svc.t("dialog.option.none", nil), Value: dialogOptionNone})
	}
	if projectID > 0 {
		repositories, err := svc.cfg.Store.ListProjectRepositories(ctx, projectID)
		if err != nil {
			return nil, svc.t("project_repo.list.failed", map[string]any{"Error": safeError(err)})
		}
		for _, repo := range repositories {
			options = append(options, MattermostDialogOption{
				Text:  svc.t("dialog.repo.option", map[string]any{"Repository": repo.FullName(), "Branch": repo.DefaultBranch}),
				Value: strconv.FormatInt(repo.RepositoryID, 10),
			})
		}
	}
	if len(options) == 0 || (allowEmpty && len(options) == 1) {
		allRepos, errText := svc.repositoryEntityOptions(ctx, 0)
		if errText != "" && !allowEmpty {
			return nil, errText
		}
		options = append(options, allRepos...)
	}
	return options, ""
}

func (svc *SlashCommandService) projectRuntimeVariableOptions(ctx context.Context, projectID int64, selected int64) ([]MattermostDialogOption, string) {
	if projectID <= 0 {
		return nil, svc.t("dialog.project.project_invalid", nil)
	}
	variables, err := svc.cfg.Store.ListProjectRuntimeVariables(ctx, projectID)
	if err != nil {
		return nil, svc.t("runtime_var.list.failed", map[string]any{"Error": safeError(err)})
	}
	if len(variables) == 0 {
		return nil, svc.t("runtime_var.list.empty", map[string]any{"Project": projectID})
	}
	return svc.projectRuntimeVariableOptionsFromList(variables, selected), ""
}

func (svc *SlashCommandService) projectRuntimeVariableOptionsFromList(variables []entity.ProjectRuntimeVariable, selected int64) []MattermostDialogOption {
	options := make([]MattermostDialogOption, 0, len(variables))
	for _, variable := range variables {
		options = append(options, MattermostDialogOption{
			Text:  svc.t("dialog.runtime_var.option", map[string]any{"Name": variable.Name, "Enabled": variable.Enabled}),
			Value: strconv.FormatInt(variable.ID, 10),
		})
	}
	return ensureDialogOption(options, selectedIDString(selected))
}

func (svc *SlashCommandService) agentRoleRuntimeVariableOptions(ctx context.Context, roleID int64) ([]MattermostDialogOption, string) {
	if roleID <= 0 {
		return nil, svc.t("dialog.role_runtime_var.role_invalid", nil)
	}
	variables, err := svc.cfg.Store.ListAgentRoleRuntimeVariables(ctx, roleID)
	if err != nil {
		return nil, svc.t("role_runtime_var.list.failed", map[string]any{"Error": safeError(err)})
	}
	options := make([]MattermostDialogOption, 0, len(variables))
	for _, variable := range variables {
		options = append(options, MattermostDialogOption{
			Text:  svc.t("dialog.runtime_var.option", map[string]any{"Name": variable.Name, "Enabled": variable.Enabled}),
			Value: strconv.FormatInt(variable.VariableID, 10),
		})
	}
	if len(options) == 0 {
		return nil, svc.t("role_runtime_var.list.empty", nil)
	}
	return options, ""
}

func (svc *SlashCommandService) runtimeVariableSelectionDefaults(ctx context.Context, command MenuActionCommand) (int64, int64, int64, string) {
	switch command.Resource {
	case menuResourceProject:
		projectID, ok := parseInt64ID(command.ID)
		if !ok {
			return 0, 0, 0, svc.t("dialog.project.project_invalid", nil)
		}
		return projectID, 0, 0, ""
	case menuResourceAgentRole:
		roleID, ok := parseInt64ID(command.ID)
		if !ok {
			return 0, 0, 0, svc.t("dialog.role_runtime_var.role_invalid", nil)
		}
		role, err := svc.cfg.Store.GetAgentRole(ctx, roleID)
		if err != nil {
			return 0, 0, 0, svc.t("agent_role.get.failed", map[string]any{"Error": safeError(err)})
		}
		return role.ProjectID, role.ID, 0, ""
	case menuResourceRuntimeVar:
		variableID, ok := parseInt64ID(command.ID)
		if !ok {
			return 0, 0, 0, svc.t("dialog.role_runtime_var.variable_invalid", nil)
		}
		variable, err := svc.cfg.Store.GetProjectRuntimeVariable(ctx, variableID)
		if err != nil {
			return 0, 0, 0, svc.t("runtime_var.get.failed", map[string]any{"Error": safeError(err)})
		}
		return variable.ProjectID, 0, variable.ID, ""
	default:
		return 0, 0, 0, svc.t("dialog.project.project_invalid", nil)
	}
}

func (svc *SlashCommandService) projectRuntimeVariableDialogProjectID(ctx context.Context, command MenuActionCommand) (int64, string) {
	switch command.Resource {
	case menuResourceProject:
		projectID, ok := parseInt64ID(command.ID)
		if !ok {
			return 0, svc.t("dialog.project.project_invalid", nil)
		}
		return projectID, ""
	case menuResourceAgentRole:
		roleID, ok := parseInt64ID(command.ID)
		if !ok {
			return 0, svc.t("dialog.role_runtime_var.role_invalid", nil)
		}
		role, err := svc.cfg.Store.GetAgentRole(ctx, roleID)
		if err != nil {
			return 0, svc.t("agent_role.get.failed", map[string]any{"Error": safeError(err)})
		}
		return role.ProjectID, ""
	case menuResourceRuntimeVar:
		return 0, ""
	default:
		return selectedProjectID(command), ""
	}
}

func projectGitHubSummary(project entity.Project) string {
	owner := strings.TrimSpace(project.GitHubOwner)
	account := strings.TrimSpace(project.GitHubAccountName)
	if owner == "" && account == "" {
		return "-"
	}
	if owner == "" {
		return "account " + account
	}
	if account == "" {
		return owner
	}
	return owner + " via " + account
}

func roleRuntimeVariableNames(variables []entity.AgentRoleRuntimeVariableBinding) string {
	if len(variables) == 0 {
		return "-"
	}
	names := make([]string, 0, len(variables))
	for _, variable := range variables {
		if !variable.Enabled {
			names = append(names, variable.Name+" (disabled)")
			continue
		}
		names = append(names, variable.Name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func runtimeVariableSlug(name string) string {
	return slugifyName(strings.ReplaceAll(name, "_", "-"), "runtime-var")
}

func projectRuntimeVariableSecretName(project entity.Project, name string) string {
	base := "mc-var-" + slugifyName(project.Slug, "project") + "-" + runtimeVariableSlug(name)
	if len(base) <= 63 {
		return base
	}
	sum := sha1.Sum([]byte(project.Slug + ":" + name))
	suffix := hex.EncodeToString(sum[:4])
	maxPrefix := 63 - len(suffix) - 1
	prefix := strings.TrimRight(base[:maxPrefix], "-")
	if prefix == "" {
		prefix = "mc-var"
	}
	return prefix + "-" + suffix
}

func maskedSecretRef(secretRef string, secretKey string) string {
	secretRef = strings.TrimSpace(secretRef)
	secretKey = defaultString(secretKey, "value")
	if secretRef == "" {
		return "-"
	}
	return secretRef + ":" + secretKey
}

func validRuntimeVariableName(value string) bool {
	if len(value) < 2 || len(value) > 128 || value[0] < 'A' || value[0] > 'Z' {
		return false
	}
	for _, char := range value[1:] {
		if char >= 'A' && char <= 'Z' {
			continue
		}
		if char >= '0' && char <= '9' {
			continue
		}
		if char == '_' {
			continue
		}
		return false
	}
	return true
}

func parseSubmittedBool(value string, fallback bool) (bool, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return fallback, true
	}
	switch value {
	case "true", "yes", "1":
		return true, true
	case "false", "no", "0":
		return false, true
	default:
		return false, false
	}
}

func (svc *SlashCommandService) agentRoleOptions(ctx context.Context, projectID int64, requireOne bool) ([]MattermostDialogOption, string) {
	roles, err := svc.cfg.Store.ListAgentRoles(ctx, projectID)
	if err != nil {
		return nil, svc.t("agent_role.list.failed", map[string]any{"Error": safeError(err)})
	}
	options := make([]MattermostDialogOption, 0, len(roles))
	for _, role := range roles {
		if !role.Enabled {
			continue
		}
		options = append(options, MattermostDialogOption{
			Text:  svc.t("dialog.role.option", map[string]any{"Role": role.Name, "Type": role.RoleType, "Project": role.ProjectID}),
			Value: strconv.FormatInt(role.ID, 10),
		})
	}
	if len(options) == 0 && requireOne {
		return nil, svc.t("agent_role.list.empty", nil)
	}
	return options, ""
}

func (svc *SlashCommandService) optionalOpenAIAccountOptions(ctx context.Context, selected string) ([]MattermostDialogOption, string) {
	options := []MattermostDialogOption{{Text: svc.t("dialog.option.none", nil), Value: dialogOptionNone}}
	accounts, err := svc.cfg.Store.ListOpenAIAccounts(ctx, 100)
	if err != nil {
		return nil, svc.t("openai.list.failed", map[string]any{"Error": safeError(err)})
	}
	sort.Slice(accounts, func(i int, j int) bool { return accounts[i].Name < accounts[j].Name })
	for _, account := range accounts {
		options = append(options, MattermostDialogOption{Text: svc.t("dialog.account.option", map[string]any{"Account": account.Name, "Status": account.Status}), Value: account.Name})
	}
	return ensureDialogOption(options, optionalSelectDefault(selected)), ""
}

func (svc *SlashCommandService) optionalGitHubAccountOptions(ctx context.Context, selected string) ([]MattermostDialogOption, string) {
	options := []MattermostDialogOption{{Text: svc.t("dialog.option.none", nil), Value: dialogOptionNone}}
	accounts, err := svc.cfg.Store.ListGitHubAccounts(ctx, 100)
	if err != nil {
		return nil, svc.t("github.account.list.failed", map[string]any{"Error": safeError(err)})
	}
	sort.Slice(accounts, func(i int, j int) bool { return accounts[i].Name < accounts[j].Name })
	for _, account := range accounts {
		options = append(options, MattermostDialogOption{Text: svc.t("dialog.account.option", map[string]any{"Account": account.Name, "Status": account.Status}), Value: account.Name})
	}
	return ensureDialogOption(options, optionalSelectDefault(selected)), ""
}

func agentRoleTypeOptions() []MattermostDialogOption {
	return []MattermostDialogOption{
		{Text: "director", Value: "director"},
		{Text: "coordinator", Value: "coordinator"},
		{Text: "manager", Value: "manager"},
		{Text: "pm_delivery", Value: "pm_delivery"},
		{Text: "worker", Value: "worker"},
		{Text: "reviewer", Value: "reviewer"},
		{Text: "analyst", Value: "analyst"},
		{Text: "architect", Value: "architect"},
		{Text: "writer", Value: "writer"},
		{Text: "sre", Value: "sre"},
		{Text: "improver", Value: "improver"},
		{Text: "lexical_guard", Value: "lexical_guard"},
		{Text: "custom", Value: "custom"},
	}
}

func chatTypeOptions() []MattermostDialogOption {
	return []MattermostDialogOption{
		{Text: "coordination", Value: "coordination"},
		{Text: "manager", Value: "manager"},
		{Text: "pm_delivery", Value: "pm_delivery"},
		{Text: "worker_reviewer", Value: "worker_reviewer"},
		{Text: "single_custom", Value: "single_custom"},
		{Text: "multi_role_custom", Value: "multi_role_custom"},
	}
}

func validAgentRoleType(value string) bool {
	switch value {
	case "director", "coordinator", "manager", "pm_delivery", "worker", "reviewer", "analyst", "architect", "writer", "sre", "improver", "lexical_guard", "custom":
		return true
	default:
		return false
	}
}

func validChatType(value string) bool {
	switch value {
	case "coordination", "manager", "pm_delivery", "worker_reviewer", "single_custom", "multi_role_custom", "custom":
		return true
	default:
		return false
	}
}

func selectedProjectID(command MenuActionCommand) int64 {
	if command.Resource == menuResourceProject {
		if projectID, ok := parseInt64ID(command.ID); ok {
			return projectID
		}
	}
	return 0
}

func selectedIDString(id int64) string {
	if id <= 0 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}

func (svc *SlashCommandService) optionalDialogOptions(options []MattermostDialogOption) []MattermostDialogOption {
	return append([]MattermostDialogOption{{Text: svc.t("dialog.option.none", nil), Value: dialogOptionNone}}, options...)
}

func optionalSelectDefault(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return dialogOptionNone
	}
	return value
}

func optionalSubmissionString(submission map[string]any, field string) string {
	value := strings.TrimSpace(submissionString(submission, field))
	if value == dialogOptionNone {
		return ""
	}
	return value
}

func rolePromptLabel(svc *SlashCommandService, role entity.AgentRole) string {
	if strings.TrimSpace(role.PromptTemplate) == "" {
		return svc.t("label.prompt.raw", nil)
	}
	return svc.t("label.prompt.template", nil)
}

func (svc *SlashCommandService) roleProjectLabel(ctx context.Context, projectID int64) string {
	if projectID <= 0 || svc.cfg.Store == nil {
		return strconv.FormatInt(projectID, 10)
	}
	project, err := svc.cfg.Store.GetProject(ctx, projectID)
	if err != nil {
		return strconv.FormatInt(projectID, 10)
	}
	if strings.TrimSpace(project.Slug) == "" {
		return fmt.Sprintf("%s #%d", project.Name, project.ID)
	}
	return fmt.Sprintf("%s #%d", project.Slug, project.ID)
}

func chatParticipantNames(participants []entity.ChatParticipant) string {
	if len(participants) == 0 {
		return "-"
	}
	names := make([]string, 0, len(participants))
	for _, participant := range participants {
		names = append(names, participant.RoleName)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func chatRepositoryNames(repositories []entity.ChatRepositoryBinding) string {
	if len(repositories) == 0 {
		return "-"
	}
	names := make([]string, 0, len(repositories))
	for _, repository := range repositories {
		names = append(names, repository.FullName())
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func mattermostDialogSlash(state mattermostDialogState, command DialogSubmissionCommand) SlashCommand {
	return SlashCommand{
		UserID:    command.UserID,
		UserName:  defaultString(command.UserName, state.UserName),
		ChannelID: defaultString(state.ChannelID, command.ChannelID),
	}
}

func parseInt64ID(value string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return id, err == nil && id > 0
}

func slugifyName(value string, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if valid {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(builder.String(), "-")
	if slug == "" {
		slug = fallback
	}
	if len(slug) > 48 {
		slug = strings.TrimRight(slug[:48], "-")
	}
	if slug == "" {
		return fallback
	}
	return slug
}

func validMattermostName(value string) bool {
	return validRuntimeRunID(value) && len(value) >= 2 && len(value) <= 48
}

func validMattermostBotUsername(value string) bool {
	return validRuntimeRunID(value) && len(value) >= 3 && len(value) <= 48
}

func looksLikeJSONObject(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	var decoded map[string]any
	return json.Unmarshal([]byte(value), &decoded) == nil
}

func (svc *SlashCommandService) settingsSummary(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "{}" {
		return "`" + svc.t("label.not_set", nil) + "`"
	}
	return "`" + svc.t("label.set_bytes", map[string]any{"Bytes": len(value)}) + "`"
}

func (svc *SlashCommandService) recordProjectAudit(ctx context.Context, command SlashCommand, eventType string, resourceName string, summary string) {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return
	}
	_ = svc.cfg.Store.RecordAuditEvent(ctx, adminrepo.AuditEventInput{
		EventType:    eventType,
		ActorUserID:  command.UserID,
		ActorUser:    command.UserName,
		ResourceType: "project",
		ResourceName: resourceName,
		Summary:      summary,
	})
}

func (svc *SlashCommandService) ensureRoleBotIdentity(ctx context.Context, project entity.Project, role entity.AgentRole, channelID string) (entity.MattermostBotIdentity, error) {
	if svc.cfg.Store == nil || svc.cfg.RoleBotManager == nil || svc.cfg.RuntimeRunner == nil {
		return entity.MattermostBotIdentity{}, fmt.Errorf("role bot identity runtime is not configured")
	}
	username := roleBotUsername(project, role)
	existing, err := svc.cfg.Store.GetMattermostBotIdentityByRoleID(ctx, role.ID)
	if err == nil && existing.MattermostUserID != "" && existing.TokenSecretRef != "" && existing.Username == username {
		botErr := svc.cfg.RoleBotManager.EnsureExistingRoleBot(ctx, existing.MattermostUserID)
		if _, secretErr := svc.cfg.RuntimeRunner.GetMattermostBotTokenSecret(ctx, existing.TokenSecretRef); botErr == nil && secretErr == nil {
			if channelID != "" {
				_ = svc.cfg.RoleBotManager.EnsureProjectChannelMember(ctx, project.Slug, channelID, existing.MattermostUserID)
			}
			return existing, nil
		}
	}
	displayName := roleBotDisplayName(project, role)
	binding, err := svc.cfg.RoleBotManager.EnsureRoleBot(ctx, MattermostRoleBotInput{
		Username:    username,
		DisplayName: displayName,
		Description: "matter-codex role bot for " + project.Name + " / " + role.Name,
	})
	if err != nil {
		_, _, _ = svc.cfg.Store.UpsertMattermostBotIdentity(ctx, adminrepo.UpsertMattermostBotIdentityInput{
			ProjectID:        project.ID,
			RoleID:           role.ID,
			Username:         username,
			DisplayName:      displayName,
			MattermostUserID: "",
			TokenSecretRef:   "",
			Status:           "error",
			LastError:        safeError(err),
		})
		return entity.MattermostBotIdentity{}, err
	}
	secretName := roleBotTokenSecretName(role.ID)
	secret, err := svc.cfg.RuntimeRunner.UpsertMattermostBotTokenSecret(ctx, adminrepoMattermostBotSecretInput(secretName, binding.Token))
	if err != nil {
		return entity.MattermostBotIdentity{}, err
	}
	identity, _, err := svc.cfg.Store.UpsertMattermostBotIdentity(ctx, adminrepo.UpsertMattermostBotIdentityInput{
		ProjectID:        project.ID,
		RoleID:           role.ID,
		Username:         binding.Username,
		DisplayName:      defaultString(binding.DisplayName, displayName),
		MattermostUserID: binding.UserID,
		TokenSecretRef:   secret.SecretName,
		Status:           "configured",
		LastError:        "",
	})
	if err != nil {
		return entity.MattermostBotIdentity{}, err
	}
	if channelID != "" {
		_ = svc.cfg.RoleBotManager.EnsureProjectChannelMember(ctx, project.Slug, channelID, binding.UserID)
	}
	return identity, nil
}

func adminrepoMattermostBotSecretInput(secretName string, token string) runtimerepo.MattermostBotTokenSecretInput {
	return runtimerepo.MattermostBotTokenSecretInput{SecretName: secretName, Token: token}
}

func roleBotUsername(project entity.Project, role entity.AgentRole) string {
	if username := strings.TrimSpace(role.BotIdentity); username != "" {
		return username
	}
	base := slugifyName(project.Slug+"-"+role.Name, "agent")
	base = strings.TrimPrefix(base, "mc-")
	name := "mc-" + base
	if len(name) > 60 {
		name = strings.TrimRight(name[:60], "-")
	}
	return name
}

func roleBotDisplayName(project entity.Project, role entity.AgentRole) string {
	if strings.TrimSpace(role.BotIdentity) != "" {
		return strings.TrimSpace(role.BotIdentity)
	}
	return project.Name + " / " + role.Name
}

func roleBotTokenSecretName(roleID int64) string {
	return "matter-codex-mm-bot-" + strconv.FormatInt(roleID, 10)
}
