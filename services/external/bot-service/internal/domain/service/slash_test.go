package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	providerrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/provider"
	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	texti18n "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/i18n"
	mattermostmodel "github.com/mattermost/mattermost/server/public/model"
)

func TestSlashTokenCheck(t *testing.T) {
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:             localizer,
		StatusService:         testStatusService(localizer),
		BotTokenConfigured:    true,
		SlashTokenConfigured:  true,
		GitHubTokenConfigured: true,
		DatabaseConfigured:    true,
		StorageReady:          true,
		ChannelManagerEnabled: true,
	})
	text := svc.Handle(context.Background(), SlashCommand{Text: "token check"})
	for _, want := range []string{
		"mattermost bot token: configured",
		"mattermost slash token: configured",
		"github token: configured",
		"github webhook secret: missing",
		"database dsn: configured",
		"storage: ready",
		"kubernetes runtime: missing",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("Handle(token check) missing %q in %q", want, text)
		}
	}
}

func TestSlashEmptyTextReturnsMenuCard(t *testing.T) {
	store := &fakeAdminStore{
		openAIAccounts: map[string]entity.OpenAIAccount{
			"primary": {Name: "primary", Status: "authorized"},
		},
		githubAccounts: map[string]entity.GitHubAccount{
			"agent":   {Name: "agent", Status: "configured"},
			"primary": {Name: "primary", Status: "unknown"},
		},
	}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:             localizer,
		StatusService:         testStatusService(localizer),
		Store:                 store,
		MenuActionURL:         "http://bot-service/mattermost/actions/agents",
		GitHubTokenConfigured: true,
		StorageReady:          true,
		RuntimeConfigured:     true,
	})

	result := svc.HandleResponse(context.Background(), SlashCommand{})

	if !strings.Contains(result.Text, "control menu") {
		t.Fatalf("HandleResponse(empty) text = %q", result.Text)
	}
	if result.Card == nil {
		t.Fatal("HandleResponse(empty) card is nil")
	}
	if !result.ChannelVisible {
		t.Fatal("HandleResponse(empty) should be channel-visible")
	}
	if result.Card.Title != "Main menu" {
		t.Fatalf("card title = %q", result.Card.Title)
	}
	if result.Card.ActionURL != "http://bot-service/mattermost/actions/agents" {
		t.Fatalf("card action URL = %q", result.Card.ActionURL)
	}
	if len(result.Card.Actions) == 0 {
		t.Fatal("card actions are empty")
	}
	if got := cardFieldValue(result.Card.Fields, "GitHub"); got != "`1/2`" {
		t.Fatalf("GitHub field = %q", got)
	}
	if got := cardFieldValue(result.Card.Fields, "OpenAI"); got != "`1/1`" {
		t.Fatalf("OpenAI field = %q", got)
	}
	if result.Card.Actions[0].ID != "menuprojects" {
		t.Fatalf("first action id = %q", result.Card.Actions[0].ID)
	}
	if result.Card.Actions[0].Context["view"] != menuViewProjects {
		t.Fatalf("first action context = %#v", result.Card.Actions[0].Context)
	}
}

func cardFieldValue(fields []MattermostCardField, title string) string {
	for _, field := range fields {
		if field.Title == title {
			return field.Value
		}
	}
	return ""
}

func dialogElementByName(elements []MattermostDialogElement, name string) (MattermostDialogElement, bool) {
	for _, element := range elements {
		if element.Name == name {
			return element, true
		}
	}
	return MattermostDialogElement{}, false
}

func TestMenuActionReturnsSubmenuCard(t *testing.T) {
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:     localizer,
		StatusService: testStatusService(localizer),
		MenuActionURL: "http://bot-service/mattermost/actions/agents",
	})

	result := svc.HandleMenuAction(context.Background(), MenuActionCommand{
		View:      "runtime",
		ChannelID: "channel-1",
		PostID:    "post-1",
	})

	if result.StatusCode != 200 {
		t.Fatalf("status = %d", result.StatusCode)
	}
	if result.Card == nil {
		t.Fatal("card is nil")
	}
	if result.Card.ChannelID != "channel-1" || result.Card.PostID != "post-1" {
		t.Fatalf("card identity = %#v", result.Card)
	}
	if result.Card.Title != "Runtime" || !strings.Contains(result.Card.Text, "Kubernetes Job/PVC") {
		t.Fatalf("card content = %#v", result.Card)
	}
	action, ok := mattermostActionByID(result.Card.Actions, "runtimesmoke")
	if !ok || action.Context["action"] != menuActionRuntimeSmoke {
		t.Fatalf("runtime smoke action = %#v", result.Card.Actions)
	}
	if !strings.Contains(result.EphemeralText, "opened") {
		t.Fatalf("ephemeral text = %q", result.EphemeralText)
	}
}

func TestMenuCardsDoNotExposeTypedCommandBlocks(t *testing.T) {
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:     localizer,
		StatusService: testStatusService(localizer),
		MenuActionURL: "http://bot-service/mattermost/actions/agents",
	})
	views := []string{
		menuViewMain,
		menuViewProjects,
		menuViewRepositories,
		menuViewAccounts,
		menuViewOpenAI,
		menuViewGitHub,
		menuViewRoles,
		menuViewChats,
		menuViewProfiles,
		menuViewPrompts,
		menuViewRuntime,
		menuViewSystem,
		menuViewAdvanced,
		menuViewHelp,
	}

	for _, view := range views {
		result := svc.HandleMenuAction(context.Background(), MenuActionCommand{View: view})
		if result.Card == nil {
			t.Fatalf("%s card is nil", view)
		}
		if strings.Contains(result.Card.Text, "/agents ") {
			t.Fatalf("%s card still exposes typed command text: %q", view, result.Card.Text)
		}
	}
}

func TestSlashLegacyDevAndFlowCommandsAreDisabled(t *testing.T) {
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:     localizer,
		StatusService: testStatusService(localizer),
	})

	for _, text := range []string{
		"dev smoke codex-k8s/matter-codex",
		"flow start codex-k8s/matter-codex flow1 Update docs",
	} {
		got := svc.Handle(context.Background(), SlashCommand{Text: text, UserName: "owner"})
		if !strings.Contains(got, "unknown command") {
			t.Fatalf("Handle(%q) = %q", text, got)
		}
	}
}

func TestMenuSectionsUseTypedActionButtons(t *testing.T) {
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:     localizer,
		StatusService: testStatusService(localizer),
		MenuActionURL: "http://bot-service/mattermost/actions/agents",
	})
	cases := []struct {
		view     string
		actionID string
		action   string
		resource string
	}{
		{view: menuViewProjects, actionID: "projectlist", action: menuActionList, resource: menuResourceProject},
		{view: menuViewRepositories, actionID: "repolist", action: menuActionList, resource: menuResourceRepository},
		{view: menuViewRoles, actionID: "rolelist", action: menuActionList, resource: menuResourceAgentRole},
		{view: menuViewChats, actionID: "chatlist", action: menuActionList, resource: menuResourceChat},
		{view: menuViewProfiles, actionID: "profilelist", action: menuActionList, resource: menuResourceProfile},
		{view: menuViewPrompts, actionID: "promptlist", action: menuActionList, resource: menuResourcePromptTemplate},
		{view: menuViewRuntime, actionID: "runtimesmoke", action: menuActionRuntimeSmoke, resource: menuResourceRuntime},
		{view: menuViewRuntime, actionID: "runtimeprunedryrun", action: menuActionRuntimePruneDry, resource: menuResourceRuntime},
		{view: menuViewSystem, actionID: "systemstatus", action: menuActionSystemStatus, resource: menuResourceSystem},
		{view: menuViewSystem, actionID: "tokencheck", action: menuActionTokenCheck, resource: menuResourceSystem},
		{view: menuViewSystem, actionID: "localeget", action: menuActionLocaleGet, resource: menuResourceSystem},
		{view: menuViewSystem, actionID: "localesetru", action: menuActionLocaleSetRU, resource: menuResourceSystem},
		{view: menuViewSystem, actionID: "localeseten", action: menuActionLocaleSetEN, resource: menuResourceSystem},
	}

	for _, tc := range cases {
		result := svc.HandleMenuAction(context.Background(), MenuActionCommand{View: tc.view})
		if result.Card == nil {
			t.Fatalf("%s card is nil", tc.view)
		}
		action, ok := mattermostActionByID(result.Card.Actions, tc.actionID)
		if !ok {
			t.Fatalf("%s action %s is missing: %#v", tc.view, tc.actionID, result.Card.Actions)
		}
		if action.Context["action"] != tc.action || action.Context["resource_type"] != tc.resource {
			t.Fatalf("%s action context = %#v", tc.actionID, action.Context)
		}
		assertCardDoesNotExposeSlashCommand(t, result.Card)
	}
}

func TestRepositoriesMenuUsesOnboardingAndEntityList(t *testing.T) {
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:       localizer,
		StatusService:   testStatusService(localizer),
		MenuActionURL:   "http://bot-service/mattermost/actions/agents",
		DialogSubmitURL: "http://bot-service/mattermost/dialogs/agents",
	})

	result := svc.HandleMenuAction(context.Background(), MenuActionCommand{View: menuViewRepositories})

	if result.Card == nil {
		t.Fatal("card is nil")
	}
	addAction, ok := mattermostActionByID(result.Card.Actions, "repoonboard")
	if !ok || addAction.Context["action"] != menuActionRepositoryOnboard || addAction.Context["resource_type"] != menuResourceRepository {
		t.Fatalf("onboarding action = %#v", result.Card.Actions)
	}
	listAction, ok := mattermostActionByID(result.Card.Actions, "repolist")
	if !ok || listAction.Context["action"] != menuActionList || listAction.Context["resource_type"] != menuResourceRepository {
		t.Fatalf("list action = %#v", result.Card.Actions)
	}
	if _, ok := mattermostActionByID(result.Card.Actions, "dialogrepodelete"); ok {
		t.Fatalf("repository menu should not expose delete dialog: %#v", result.Card.Actions)
	}
}

func TestAccountMenusUseCreateDialogAndEntityLists(t *testing.T) {
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:       localizer,
		StatusService:   testStatusService(localizer),
		MenuActionURL:   "http://bot-service/mattermost/actions/agents",
		DialogSubmitURL: "http://bot-service/mattermost/dialogs/agents",
	})
	cases := []struct {
		view       string
		listAction string
		resource   string
		addAction  string
		addDialog  string
	}{
		{view: menuViewOpenAI, listAction: "openailist", resource: menuResourceOpenAIAccount, addAction: "dialogopenaiauth", addDialog: menuDialogOpenAIAuth},
		{view: menuViewGitHub, listAction: "githublist", resource: menuResourceGitHubAccount, addAction: "dialoggithubadd", addDialog: menuDialogGitHubAccountAdd},
	}

	for _, tc := range cases {
		result := svc.HandleMenuAction(context.Background(), MenuActionCommand{View: tc.view})
		if result.Card == nil {
			t.Fatalf("%s card is nil", tc.view)
		}
		listAction, ok := mattermostActionByID(result.Card.Actions, tc.listAction)
		if !ok || listAction.Context["action"] != menuActionList || listAction.Context["resource_type"] != tc.resource {
			t.Fatalf("%s list action = %#v", tc.view, result.Card.Actions)
		}
		addAction, ok := mattermostActionByID(result.Card.Actions, tc.addAction)
		if !ok || addAction.Context["dialog"] != tc.addDialog {
			t.Fatalf("%s add action = %#v", tc.view, result.Card.Actions)
		}
		assertCardDoesNotExposeSlashCommand(t, result.Card)
	}
}

func TestMenuActionReturnsRepositoryDialog(t *testing.T) {
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:       localizer,
		StatusService:   testStatusService(localizer),
		MenuActionURL:   "http://bot-service/mattermost/actions/agents",
		DialogSubmitURL: "http://bot-service/mattermost/dialogs/agents",
	})

	result := svc.HandleMenuAction(context.Background(), MenuActionCommand{
		View:      menuViewRepositories,
		Dialog:    menuDialogRepositoryAdd,
		UserName:  "owner",
		ChannelID: "channel-1",
		PostID:    "post-1",
	})

	if result.StatusCode != 200 {
		t.Fatalf("status = %d", result.StatusCode)
	}
	if result.Dialog == nil {
		t.Fatal("dialog is nil")
	}
	if result.Dialog.SubmitURL != "http://bot-service/mattermost/dialogs/agents" || result.Dialog.CallbackID != dialogCallbackRepositoryAdd {
		t.Fatalf("dialog = %#v", result.Dialog)
	}
	if result.Dialog.Title != "Add repo" || len(result.Dialog.Elements) != 3 {
		t.Fatalf("dialog content = %#v", result.Dialog)
	}
	state, err := decodeDialogState(result.Dialog.State)
	if err != nil {
		t.Fatalf("decodeDialogState() error = %v", err)
	}
	if state.ChannelID != "channel-1" || state.PostID != "post-1" || state.UserName != "owner" {
		t.Fatalf("state = %#v", state)
	}
}

func TestMattermostDialogsFitMattermostLimits(t *testing.T) {
	for _, locale := range []string{texti18n.DefaultLocale, "ru"} {
		localizer := testLocalizer(t, locale)
		svc := NewSlashCommandService(SlashCommandServiceConfig{
			Localizer:       localizer,
			StatusService:   testStatusService(localizer),
			DialogSubmitURL: "http://bot-service/mattermost/dialogs/agents",
		})
		for _, dialog := range []string{
			menuDialogRepositoryAdd,
			menuDialogRepositoryEdit,
			menuDialogRepositoryDelete,
			menuDialogRepositorySearch,
			menuDialogOpenAIAuth,
			menuDialogOpenAIStatus,
			menuDialogOpenAICleanup,
			menuDialogOpenAIDelete,
			menuDialogGitHubAccountAdd,
			menuDialogGitHubAccountEdit,
			menuDialogGitHubAccountDelete,
		} {
			result := svc.HandleMenuAction(context.Background(), MenuActionCommand{
				View:   dialogView(dialog),
				Dialog: dialog,
			})
			if result.Dialog == nil {
				t.Fatalf("dialog %q for locale %q is nil", dialog, locale)
			}
			openedDialog := result.Dialog
			if len(openedDialog.Title) > mattermostmodel.DialogTitleMaxLength {
				t.Fatalf("dialog %q title %q for locale %q length = %d, want <= %d", dialog, openedDialog.Title, locale, len(openedDialog.Title), mattermostmodel.DialogTitleMaxLength)
			}
			for _, element := range openedDialog.Elements {
				if len(element.DisplayName) > mattermostmodel.DialogElementDisplayNameMaxLength {
					t.Fatalf("dialog %q field %q display name length = %d, want <= %d", dialog, element.Name, len(element.DisplayName), mattermostmodel.DialogElementDisplayNameMaxLength)
				}
				if len(element.HelpText) > mattermostmodel.DialogElementHelpTextMaxLength {
					t.Fatalf("dialog %q field %q help text length = %d, want <= %d", dialog, element.Name, len(element.HelpText), mattermostmodel.DialogElementHelpTextMaxLength)
				}
				if element.Type == "text" && len(element.Placeholder) > mattermostmodel.DialogElementTextMaxLength {
					t.Fatalf("dialog %q field %q placeholder length = %d, want <= %d", dialog, element.Name, len(element.Placeholder), mattermostmodel.DialogElementTextMaxLength)
				}
			}
		}
	}
}

func dialogView(dialog string) string {
	switch dialog {
	case menuDialogOpenAIAuth, menuDialogOpenAIStatus, menuDialogOpenAICleanup, menuDialogOpenAIDelete:
		return menuViewOpenAI
	case menuDialogGitHubAccountAdd, menuDialogGitHubAccountEdit, menuDialogGitHubAccountDelete:
		return menuViewGitHub
	default:
		return menuViewRepositories
	}
}

func TestRepositoryDialogSubmissionAddsRepository(t *testing.T) {
	store := &fakeAdminStore{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:             localizer,
		StatusService:         testStatusService(localizer),
		Store:                 store,
		DefaultTeamName:       "agents",
		MenuActionURL:         "http://bot-service/mattermost/actions/agents",
		DialogSubmitURL:       "http://bot-service/mattermost/dialogs/agents",
		StorageReady:          true,
		ChannelManagerEnabled: true,
	})
	state := encodeDialogState(MenuActionCommand{View: menuViewRepositories, ChannelID: "channel-1", PostID: "post-1", UserName: "owner"})

	result := svc.HandleDialogSubmission(context.Background(), DialogSubmissionCommand{
		CallbackID: dialogCallbackRepositoryAdd,
		State:      state,
		UserID:     "owner-id",
		Submission: map[string]any{
			dialogFieldProvider:      "github",
			dialogFieldRepository:    "codex-k8s/matter-codex",
			dialogFieldDefaultBranch: "main",
		},
	})

	if result.Error != "" || len(result.Errors) > 0 {
		t.Fatalf("dialog errors = %q %#v", result.Error, result.Errors)
	}
	if store.upsert.Provider != "github" || store.upsert.Owner != "codex-k8s" || store.upsert.Name != "matter-codex" {
		t.Fatalf("upsert = %#v", store.upsert)
	}
	if !store.auditRecorded {
		t.Fatal("audit event was not recorded")
	}
	if result.Card == nil || result.Card.ChannelID != "channel-1" || result.Card.PostID != "post-1" {
		t.Fatalf("card = %#v", result.Card)
	}
	if !strings.Contains(result.Card.Text, "github:codex-k8s/matter-codex") {
		t.Fatalf("card text = %q", result.Card.Text)
	}
}

func TestProjectDialogSubmissionCreatesMattermostTeam(t *testing.T) {
	store := &fakeAdminStore{
		githubAccounts: map[string]entity.GitHubAccount{
			"agent": {Name: "agent", SecretRef: "matter-codex-github-agent", Status: "configured"},
		},
	}
	channels := &fakeChannelManager{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:       localizer,
		StatusService:   testStatusService(localizer),
		Store:           store,
		ChannelManager:  channels,
		DialogSubmitURL: "http://bot-service/mattermost/dialogs/agents",
		StorageReady:    true,
	})
	state := encodeDialogState(MenuActionCommand{View: menuViewProjects, ChannelID: "channel-1", PostID: "post-1", UserName: "owner"})

	result := svc.HandleDialogSubmission(context.Background(), DialogSubmissionCommand{
		CallbackID: dialogCallbackProjectUpsert,
		State:      state,
		UserID:     "owner-id",
		Submission: map[string]any{
			dialogFieldProjectName:      "Demo Project",
			dialogFieldProjectSlug:      "demo-project",
			dialogFieldGitHubAccount:    "agent",
			dialogFieldDescription:      "Project context",
			dialogFieldAdvancedSettings: `{"model":"gpt-5"}`,
		},
	})

	if result.Error != "" || len(result.Errors) > 0 {
		t.Fatalf("dialog errors = %q %#v", result.Error, result.Errors)
	}
	project, err := store.GetProject(context.Background(), 1)
	if err != nil {
		t.Fatalf("project not stored: %v", err)
	}
	if project.Slug != "demo-project" || project.MattermostTeamID != "team-demo-project" || project.GitHubAccountName != "agent" {
		t.Fatalf("project = %#v", project)
	}
	if channels.projectTeamName != "demo-project" {
		t.Fatalf("team name = %q", channels.projectTeamName)
	}
	if result.Card == nil || result.Card.Title != "Project `Demo Project`" {
		t.Fatalf("card = %#v", result.Card)
	}
}

func TestProjectDialogReadOnlyPrevalidationRejectsStoredSlugChange(t *testing.T) {
	store := &fakeAdminStore{
		projects: map[int64]entity.Project{
			1: {ID: 1, Name: "Demo Project", Slug: "demo-project"},
		},
	}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:     localizer,
		StatusService: testStatusService(localizer),
		Store:         store,
		StorageReady:  true,
	})

	result := svc.PrevalidateDialogSubmissionReadOnly(context.Background(), DialogSubmissionCommand{
		CallbackID: dialogCallbackProjectUpsert,
		State:      encodeDialogState(MenuActionCommand{Resource: menuResourceProject, ID: "1"}),
		Submission: map[string]any{
			dialogFieldProjectName: "Demo Project",
			dialogFieldProjectSlug: "changed-slug",
		},
	})

	if result.Errors[dialogFieldProjectSlug] == "" {
		t.Fatalf("read-only prevalidation result = %#v", result)
	}
	project, err := store.GetProject(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetProject() error = %v", err)
	}
	if project.Slug != "demo-project" {
		t.Fatalf("project changed during read-only prevalidation: %#v", project)
	}
}

func TestAgentRoleDialogDefaultsGitHubAccountFromProject(t *testing.T) {
	store := &fakeAdminStore{
		projects: map[int64]entity.Project{
			1: {ID: 1, Name: "Demo Project", Slug: "demo-project", GitHubAccountName: "project-gh", GitHubOwner: "codex-k8s", GitHubOwnerType: "org"},
		},
		githubAccounts: map[string]entity.GitHubAccount{
			"project-gh": {Name: "project-gh", SecretRef: "matter-codex-github-project", Status: "configured"},
		},
	}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:       localizer,
		StatusService:   testStatusService(localizer),
		Store:           store,
		DialogSubmitURL: "http://bot-service/mattermost/dialogs/agents",
		StorageReady:    true,
	})

	result := svc.HandleMenuAction(context.Background(), MenuActionCommand{
		View:     menuViewProjects,
		Dialog:   menuDialogAgentRoleUpsert,
		Resource: menuResourceProject,
		ID:       "1",
	})

	if result.Dialog == nil {
		t.Fatalf("dialog is nil: %#v", result)
	}
	githubField, ok := dialogElementByName(result.Dialog.Elements, dialogFieldGitHubAccount)
	if !ok {
		t.Fatalf("github field is missing: %#v", result.Dialog.Elements)
	}
	if githubField.Default != "project-gh" {
		t.Fatalf("github field default = %q", githubField.Default)
	}
}

func TestAgentRoleDialogSeedsKnownRolePromptTemplate(t *testing.T) {
	store := &fakeAdminStore{
		projects: map[int64]entity.Project{
			1: {ID: 1, Name: "Demo Project", Slug: "demo-project"},
		},
		openAIAccounts: map[string]entity.OpenAIAccount{
			"primary": {Name: "primary", Status: "authorized"},
		},
		githubAccounts: map[string]entity.GitHubAccount{
			"agent": {Name: "agent", Status: "configured"},
		},
	}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:       localizer,
		StatusService:   testStatusService(localizer),
		Store:           store,
		RoleBotManager:  &fakeRoleBotManager{},
		RuntimeRunner:   &fakeRuntimeRunner{},
		DialogSubmitURL: "http://bot-service/mattermost/dialogs/agents",
		StorageReady:    true,
	})
	state := encodeDialogState(MenuActionCommand{View: menuViewRoles, Resource: menuResourceProject, ID: "1", ChannelID: "channel-1", PostID: "post-1"})

	result := svc.HandleDialogSubmission(context.Background(), DialogSubmissionCommand{
		CallbackID: dialogCallbackAgentRoleUpsert,
		State:      state,
		UserID:     "owner-id",
		Submission: map[string]any{
			dialogFieldProjectID:        "1",
			dialogFieldRole:             "backend-developer",
			dialogFieldBotIdentity:      "@backend-dev-bot",
			dialogFieldRoleType:         "worker",
			dialogFieldOpenAIAccount:    "primary",
			dialogFieldGitHubAccount:    "agent",
			dialogFieldPromptMode:       "template",
			dialogFieldPromptTemplate:   "",
			dialogFieldKubernetesAccess: "read-only",
			dialogFieldSandboxMode:      "danger-full-access",
			dialogFieldDescription:      "Developer role",
			dialogFieldConfigOverlay:    "sandbox_mode = \"danger-full-access\"",
			dialogFieldAdvancedSettings: "{}",
		},
	})

	if result.Error != "" || len(result.Errors) > 0 {
		t.Fatalf("dialog errors = %q %#v", result.Error, result.Errors)
	}
	role, err := store.GetAgentRole(context.Background(), 1)
	if err != nil {
		t.Fatalf("role not stored: %v", err)
	}
	if role.PromptMode != "template" || !strings.Contains(role.PromptTemplate, "Ты агент developer проекта") || !strings.Contains(role.PromptTemplate, "MatterCodex MCP") {
		t.Fatalf("prompt mode/template = %q/%q", role.PromptMode, role.PromptTemplate)
	}
	if role.OpenAIAccountName != "primary" || role.GitHubAccountName != "agent" {
		t.Fatalf("accounts = %#v", role)
	}
	if role.BotIdentity != "backend-dev-bot" {
		t.Fatalf("bot identity = %q", role.BotIdentity)
	}
	identity, err := store.GetMattermostBotIdentityByRoleID(context.Background(), role.ID)
	if err != nil {
		t.Fatalf("bot identity not stored: %v", err)
	}
	if identity.Username != "backend-dev-bot" || identity.MattermostUserID != "bot-user-backend-dev-bot" || identity.TokenSecretRef == "" {
		t.Fatalf("bot identity = %#v", identity)
	}
	if !strings.Contains(result.Card.Text, "prompt: `template`") {
		t.Fatalf("card text = %q", result.Card.Text)
	}
}

func TestBootstrapSystemAgentRolesCreatesImproverButDoesNotCreateClusterAdmin(t *testing.T) {
	store := &fakeAdminStore{
		projects: map[int64]entity.Project{
			1: {ID: 1, Name: "Radar", Slug: "radar-auto", GitHubAccountName: "github-radar-owner-manager"},
			2: {ID: 2, Name: "My QR Contact", Slug: "myqrcontact", GitHubAccountName: "github-myqrcontact-owner"},
		},
		openAIAccounts: map[string]entity.OpenAIAccount{
			"openai-codex-main":   {Name: "openai-codex-main", Status: "authorized"},
			"openai-codex-manage": {Name: "openai-codex-manage", Status: "authorized"},
		},
		githubAccounts: map[string]entity.GitHubAccount{
			"github-myqrcontact-owner":      {Name: "github-myqrcontact-owner", Username: "ai-da-stas", Status: "configured"},
			"github-radar-owner-manager":    {Name: "github-radar-owner-manager", Username: "ai-da-stas", Status: "configured"},
			"github-myqrcontact-agent":      {Name: "github-myqrcontact-agent", Username: "kodex-agent", Status: "configured"},
			"matter-codex-github-sre-agent": {Name: "matter-codex-github-sre-agent", Username: "kodex-agent", Status: "configured"},
		},
		repositories: map[string]entity.Repository{
			repositoryStoreKey("github", "radar-auto", "marketplace"): {
				ID:            1,
				Provider:      "github",
				Owner:         "radar-auto",
				Name:          "marketplace",
				DefaultBranch: "main",
			},
		},
		agentRoles: map[int64]entity.AgentRole{
			1:  {ID: 1, ProjectID: 1, Name: "manager", RoleType: "manager", Enabled: true},
			10: {ID: 10, ProjectID: 1, Name: "improver", RoleType: "improver", PromptTemplate: "custom prompt", PromptMode: "template", Enabled: true, BotIdentity: "improver"},
			20: {ID: 20, ProjectID: 2, Name: "manager", RoleType: "manager", Enabled: true},
		},
		chats: map[int64]entity.Chat{
			1: {ID: 1, ProjectID: 1, MattermostChannelID: "channel-radar", Name: "Radar Dev", Slug: "radar-dev", ChatType: "worker_reviewer", Settings: "{}"},
		},
		chatParticipants: map[int64][]entity.ChatParticipant{
			1: {{ID: 1, ChatID: 1, RoleID: 1, RoleName: "manager", Enabled: true}},
		},
		chatRepositories: map[int64][]entity.ChatRepositoryBinding{
			1: {{ID: 1, ChatID: 1, RepositoryID: 1, Provider: "github", Owner: "radar-auto", Name: "marketplace"}},
		},
	}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:      localizer,
		StatusService:  testStatusService(localizer),
		Store:          store,
		ChannelManager: &fakeChannelManager{},
		RoleBotManager: &fakeRoleBotManager{},
		RuntimeRunner:  &fakeRuntimeRunner{},
		StorageReady:   true,
	})

	if err := svc.BootstrapSystemAgentRoles(context.Background()); err != nil {
		t.Fatalf("BootstrapSystemAgentRoles() error = %v", err)
	}

	radarImprover := testRoleByName(t, store, 1, "improver")
	if radarImprover.PromptTemplate != "custom prompt" {
		t.Fatalf("existing improver prompt was overwritten: %q", radarImprover.PromptTemplate)
	}
	myQRImprover := testRoleByName(t, store, 2, "improver")
	if myQRImprover.RoleType != "improver" || myQRImprover.PromptMode != "template" || !strings.Contains(myQRImprover.PromptTemplate, "повторяющиеся замечания") {
		t.Fatalf("myqr improver = %#v", myQRImprover)
	}
	if myQRImprover.GitHubAccountName != "github-myqrcontact-owner" || myQRImprover.OpenAIAccountName != "openai-codex-manage" {
		t.Fatalf("myqr improver accounts = %#v", myQRImprover)
	}
	participants, _ := store.ListChatParticipants(context.Background(), 1)
	if !testParticipantsContainRole(participants, radarImprover.ID) {
		t.Fatalf("bootstrap did not bind safe improver before admin admission: %#v", participants)
	}
	if _, err := store.GetMattermostBotIdentityByRoleID(context.Background(), radarImprover.ID); err != nil {
		t.Fatalf("bootstrap did not create safe improver bot identity: %v", err)
	}
	if _, err := store.GetMattermostBotIdentityByRoleID(context.Background(), myQRImprover.ID); err != nil {
		t.Fatalf("bootstrap did not create second safe improver bot identity: %v", err)
	}

	if _, err := store.GetProjectBySlug(context.Background(), "agents"); !errors.Is(err, adminrepo.ErrNotFound) {
		t.Fatalf("bootstrap created matter-codex project before cluster-admin admission: %v", err)
	}
	for _, projectID := range []int64{1, 2} {
		project, err := store.GetProject(context.Background(), projectID)
		if err != nil || project.MattermostRunsChannelID != "channel-runs" {
			t.Fatalf("project %d runs channel = %#v error=%v", projectID, project, err)
		}
	}
	director := testRoleByName(t, store, 1, "director")
	if !strings.Contains(director.PromptTemplate, "явное подтверждение") {
		t.Fatalf("director prompt does not require owner approval: %q", director.PromptTemplate)
	}
	director.Name = "руководитель"
	store.agentRoles[director.ID] = director
	roleCount := len(store.agentRoles)
	if err := svc.BootstrapSystemAgentRoles(context.Background()); err != nil {
		t.Fatalf("BootstrapSystemAgentRoles() after director rename error = %v", err)
	}
	if len(store.agentRoles) != roleCount {
		t.Fatalf("director rename created a duplicate role: before=%d after=%d", roleCount, len(store.agentRoles))
	}
}

func TestBootstrapMatterCodexAdminGuardsEverySideEffectAdapter(t *testing.T) {
	tests := []struct {
		name           string
		denyAt         int
		wantTeam       bool
		wantOperations []string
	}{
		{name: "Mattermost team", denyAt: 1, wantOperations: []string{"system_role.ensure_team.side_effect"}},
		{name: "bot identity and token", denyAt: 2, wantTeam: true, wantOperations: []string{"system_role.ensure_team.side_effect", "system_role.bot_identity.side_effect"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			baseStore := &fakeAdminStore{
				repositories: map[string]entity.Repository{
					repositoryStoreKey("github", "codex-k8s", "matter-codex"): {
						ID: 1, Provider: "github", Owner: "codex-k8s", Name: "matter-codex", DefaultBranch: "main",
					},
				},
				projects: map[int64]entity.Project{
					1: {ID: 1, Name: "MatterCodex", Slug: systemMatterCodexProjectSlug},
				},
				agentRoles: map[int64]entity.AgentRole{
					1: {
						ID: 1, ProjectID: 1, Name: systemMatterCodexRoleName, RoleType: "admin",
						KubernetesAccess: "cluster-admin", Enabled: true, BotIdentity: systemMatterCodexRoleName,
					},
				},
				chats: map[int64]entity.Chat{
					1: {ID: 1, ProjectID: 1, MattermostChannelID: "channel-control", Name: "Agents Control", Slug: systemMatterCodexChatSlug},
				},
				botIdentities: map[int64]entity.MattermostBotIdentity{
					1: {
						ID: 1, ProjectID: 1, RoleID: 1, Username: systemMatterCodexRoleName,
						MattermostUserID: "synthetic-admin-user", TokenSecretRef: "synthetic-admin-token-ref", Status: "configured",
					},
				},
				projectRepositories: map[string]entity.ProjectRepository{
					"1:1": {ID: 1, ProjectID: 1, RepositoryID: 1, Provider: "github", Owner: "codex-k8s", Name: "matter-codex", DefaultBranch: "main", IsDefault: true},
				},
			}
			store := &admittedAdminStore{fakeAdminStore: baseStore, allowed: true, denyGuardAt: test.denyAt}
			channelManager := &fakeChannelManager{}
			roleBotManager := &fakeRoleBotManager{}
			runner := &fakeRuntimeRunner{botTokenSecrets: map[string]string{"synthetic-admin-token-ref": "synthetic-token"}}
			localizer := testLocalizer(t, texti18n.RussianLocale)
			svc := NewSlashCommandService(SlashCommandServiceConfig{
				Localizer: localizer, StatusService: testStatusService(localizer), Store: store,
				ChannelManager: channelManager, RoleBotManager: roleBotManager, RuntimeRunner: runner,
				StorageReady: true,
			})

			_, err := svc.bootstrapMatterCodexAdmin(context.Background(), nil)
			if !errors.Is(err, adminrepo.ErrClusterAdminAdmissionDenied) {
				t.Fatalf("bootstrapMatterCodexAdmin() error = %v", err)
			}
			if (channelManager.projectTeamName != "") != test.wantTeam {
				t.Fatalf("team side effect=%q, want=%t", channelManager.projectTeamName, test.wantTeam)
			}
			if len(baseStore.repositories) != 1 || len(baseStore.projectRepositories) != 1 {
				t.Fatalf("bootstrap изменил frozen repository state: repositories=%d project_repositories=%d", len(baseStore.repositories), len(baseStore.projectRepositories))
			}
			if roleBotManager.channelMemberUserID != "" {
				t.Fatalf("bot identity guard допустил membership: %q", roleBotManager.channelMemberUserID)
			}
			if len(store.guardInputs) != len(test.wantOperations) {
				t.Fatalf("guard inputs=%#v", store.guardInputs)
			}
			for index, operation := range test.wantOperations {
				if store.guardInputs[index].Operation != operation {
					t.Fatalf("guard %d=%#v", index, store.guardInputs[index])
				}
			}
		})
	}
}

func TestReconcileProjectRoleBotIdentitiesExcludesClusterAdmin(t *testing.T) {
	store := &fakeAdminStore{
		projects: map[int64]entity.Project{
			1: {ID: 1, Name: "MatterCodex", Slug: systemMatterCodexProjectSlug},
		},
		agentRoles: map[int64]entity.AgentRole{
			1: {ID: 1, ProjectID: 1, Name: systemMatterCodexRoleName, RoleType: "admin", KubernetesAccess: "cluster-admin", Enabled: true},
			2: {ID: 2, ProjectID: 1, Name: "improver", RoleType: "improver", KubernetesAccess: "read-only", Enabled: true},
		},
	}
	localizer := testLocalizer(t, texti18n.RussianLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer: localizer, StatusService: testStatusService(localizer), Store: store,
		RoleBotManager: &fakeRoleBotManager{}, RuntimeRunner: &fakeRuntimeRunner{}, StorageReady: true,
	})
	if err := svc.reconcileProjectRoleBotIdentities(context.Background(), store.projects[1]); err != nil {
		t.Fatalf("reconcileProjectRoleBotIdentities() error = %v", err)
	}
	if _, err := store.GetMattermostBotIdentityByRoleID(context.Background(), 1); !errors.Is(err, adminrepo.ErrNotFound) {
		t.Fatalf("generic reconcile создал cluster-admin identity: %v", err)
	}
	if _, err := store.GetMattermostBotIdentityByRoleID(context.Background(), 2); err != nil {
		t.Fatalf("generic reconcile не обработал safe role: %v", err)
	}
}

func TestChatDialogCreatesPrivateProjectChannelWithRolesAndRepository(t *testing.T) {
	store := &fakeAdminStore{
		projects: map[int64]entity.Project{
			1: {ID: 1, Name: "Demo Project", Slug: "demo-project"},
		},
		repositories: map[string]entity.Repository{
			repositoryStoreKey("github", "codex-k8s", "matter-codex"): {
				ID:            1,
				Provider:      "github",
				Owner:         "codex-k8s",
				Name:          "matter-codex",
				DefaultBranch: "main",
			},
		},
		agentRoles: map[int64]entity.AgentRole{
			1: {ID: 1, ProjectID: 1, Name: "worker", RoleType: "worker", Enabled: true},
			2: {ID: 2, ProjectID: 1, Name: "reviewer", RoleType: "reviewer", Enabled: true},
		},
	}
	channels := &fakeChannelManager{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:       localizer,
		StatusService:   testStatusService(localizer),
		Store:           store,
		ChannelManager:  channels,
		RoleBotManager:  &fakeRoleBotManager{},
		RuntimeRunner:   &fakeRuntimeRunner{},
		DialogSubmitURL: "http://bot-service/mattermost/dialogs/agents",
		StorageReady:    true,
	})
	state := encodeDialogState(MenuActionCommand{View: menuViewChats, Resource: menuResourceProject, ID: "1", ChannelID: "channel-1", PostID: "post-1"})

	result := svc.HandleDialogSubmission(context.Background(), DialogSubmissionCommand{
		CallbackID: dialogCallbackChatCreate,
		State:      state,
		UserID:     "owner-id",
		Submission: map[string]any{
			dialogFieldProjectID:       "1",
			dialogFieldChatName:        "Backend Review",
			dialogFieldChatType:        "worker_reviewer",
			dialogFieldPrimaryRoleID:   "1",
			dialogFieldSecondaryRoleID: "2",
			dialogFieldRepositoryID:    "1",
			dialogFieldRootIssue:       "https://github.com/codex-k8s/matter-codex/issues/1",
			dialogFieldWorkPolicy:      "Work through GitHub issues and PRs.",
		},
	})

	if result.Error != "" || len(result.Errors) > 0 {
		t.Fatalf("dialog errors = %q %#v", result.Error, result.Errors)
	}
	chat, err := store.GetChat(context.Background(), 1)
	if err != nil {
		t.Fatalf("chat not stored: %v", err)
	}
	if chat.MattermostChannelID != "channel-backend-review" || channels.projectChannelType != "P" {
		t.Fatalf("chat/channel = %#v type %q", chat, channels.projectChannelType)
	}
	participants, _ := store.ListChatParticipants(context.Background(), chat.ID)
	if len(participants) != 2 || participants[0].RoleName != "worker" || participants[1].RoleName != "reviewer" {
		t.Fatalf("participants = %#v", participants)
	}
	repositories, _ := store.ListChatRepositories(context.Background(), chat.ID)
	if len(repositories) != 1 || repositories[0].FullName() != "codex-k8s/matter-codex" {
		t.Fatalf("repositories = %#v", repositories)
	}
}

func TestChatDialogRejectsNewClusterAdminBindingBeforeSideEffects(t *testing.T) {
	baseStore := &fakeAdminStore{
		projects: map[int64]entity.Project{1: {ID: 1, Name: "Admin", Slug: "admin"}},
		agentRoles: map[int64]entity.AgentRole{
			1: {ID: 1, ProjectID: 1, Name: "admin", RoleType: "admin", KubernetesAccess: "cluster-admin", Enabled: true},
		},
	}
	store := &admittedAdminStore{fakeAdminStore: baseStore, allowed: true, denyBinding: true}
	channels := &fakeChannelManager{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer: localizer, StatusService: testStatusService(localizer), Store: store,
		ChannelManager: channels, RoleBotManager: &fakeRoleBotManager{}, RuntimeRunner: &fakeRuntimeRunner{},
		DialogSubmitURL: "http://bot-service/mattermost/dialogs/agents", StorageReady: true,
	})
	result := svc.HandleDialogSubmission(context.Background(), DialogSubmissionCommand{
		CallbackID: dialogCallbackChatCreate,
		State:      encodeDialogState(MenuActionCommand{View: menuViewChats, Resource: menuResourceProject, ID: "1", ChannelID: "channel-1", PostID: "post-1"}),
		UserID:     "owner-id", UserName: "owner",
		Submission: map[string]any{
			dialogFieldProjectID: "1", dialogFieldChatName: "New Admin", dialogFieldChatType: "single_custom",
			dialogFieldPrimaryRoleID: "1", dialogFieldWorkPolicy: "Owner control.",
		},
	})
	if result.StatusCode != 403 || result.Error == "" {
		t.Fatalf("dialog result = %#v", result)
	}
	if store.calls != 1 || store.bindingCalls != 1 {
		t.Fatalf("admission calls: subject=%d binding=%d", store.calls, store.bindingCalls)
	}
	if channels.projectTeamName != "" || channels.projectChannelName != "" || len(baseStore.chats) != 0 || len(baseStore.chatParticipants) != 0 {
		t.Fatalf("denied binding caused side effects: channels=%#v chats=%#v participants=%#v", channels, baseStore.chats, baseStore.chatParticipants)
	}
}

func TestChatDialogRechecksClusterAdminBeforeBotIdentitySideEffects(t *testing.T) {
	baseStore := &fakeAdminStore{
		projects: map[int64]entity.Project{1: {ID: 1, Name: "Admin", Slug: "admin"}},
		agentRoles: map[int64]entity.AgentRole{
			1: {ID: 1, ProjectID: 1, Name: "admin", RoleType: "admin", KubernetesAccess: "cluster-admin", Enabled: true},
		},
		chats: map[int64]entity.Chat{
			1: {ID: 1, ProjectID: 1, MattermostChannelID: "channel-existing", Name: "Existing Admin", Slug: "existing-admin"},
		},
	}
	store := &admittedAdminStore{fakeAdminStore: baseStore, allowed: true, denyGuard: true}
	channels := &fakeChannelManager{}
	roleBots := &fakeRoleBotManager{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer: localizer, StatusService: testStatusService(localizer), Store: store,
		ChannelManager: channels, RoleBotManager: roleBots, RuntimeRunner: &fakeRuntimeRunner{},
		DialogSubmitURL: "http://bot-service/mattermost/dialogs/agents", StorageReady: true,
	})
	result := svc.HandleDialogSubmission(context.Background(), DialogSubmissionCommand{
		CallbackID: dialogCallbackChatCreate,
		State:      encodeDialogState(MenuActionCommand{View: menuViewChats, Resource: menuResourceProject, ID: "1", ChannelID: "channel-1", PostID: "post-1"}),
		UserID:     "owner-id", UserName: "owner",
		Submission: map[string]any{
			dialogFieldProjectID: "1", dialogFieldChatName: "Existing Admin", dialogFieldChatType: "single_custom",
			dialogFieldPrimaryRoleID: "1", dialogFieldWorkPolicy: "Owner control.",
		},
	})
	if result.Error == "" {
		t.Fatalf("dialog result = %#v", result)
	}
	if store.calls != 1 || store.bindingCalls != 1 || store.guardCalls != 1 {
		t.Fatalf("admission calls: subject=%d binding=%d guard=%d", store.calls, store.bindingCalls, store.guardCalls)
	}
	if roleBots.channelMemberChannelID != "" || channels.projectTeamName != "" || channels.projectChannelName != "" {
		t.Fatalf("runtime guard denial caused Mattermost side effects: role_bot=%#v channels=%#v", roleBots, channels)
	}
	if chat := baseStore.chats[1]; chat.MattermostChannelID != "channel-existing" || chat.Name != "Existing Admin" {
		t.Fatalf("runtime guard denial changed chat: %#v", chat)
	}
}

func TestBuildRolePromptUsesRawMessageWithoutTemplate(t *testing.T) {
	prompt, err := BuildRolePrompt(RolePromptInput{
		Project:      entity.Project{Name: "Platform", Slug: "platform"},
		Role:         entity.AgentRole{Name: "adhoc", RoleType: "custom"},
		Chat:         entity.Chat{Name: "Manager", ChatType: "manager"},
		Repositories: []entity.ProjectRepository{{Provider: "github", Owner: "codex-k8s", Name: "matter-codex", DefaultBranch: "main"}},
		GitHubAccount: entity.GitHubAccount{
			Name:     "github-platform-owner",
			Username: "ai-da-stas",
		},
		RuntimeVariables: []entity.ProjectRuntimeVariable{{
			Name:        "RADAR_AUTO_KUBECONFIG",
			Description: "kubeconfig for the external radar-auto cluster",
			Enabled:     true,
		}},
		UserMessage: "Inspect the current issue and propose next steps.",
		Locale:      promptTemplateLocaleData{Language: "English"},
	})
	if err != nil {
		t.Fatalf("BuildRolePrompt() error = %v", err)
	}
	if !strings.Contains(prompt, "# Инструкция пользователя") || !strings.Contains(prompt, "Inspect the current issue and propose next steps.") {
		t.Fatalf("prompt = %q", prompt)
	}
	if !strings.Contains(prompt, "Проект: Platform") || !strings.Contains(prompt, "github:codex-k8s/matter-codex") {
		t.Fatalf("prompt = %q", prompt)
	}
	if !strings.Contains(prompt, "`gh` 2.95.0") || !strings.Contains(prompt, "`go` 1.26") || !strings.Contains(prompt, "`kubectl` 1.36.2") {
		t.Fatalf("prompt runtime tools = %q", prompt)
	}
	for _, expected := range []string{"`node` 24.17.x", "`vite` 8.0.16", "`asyncapi` 6.0.2", "`wscat` 6.1.0", "`playwright` 1.61.1", "`chromium` distro package"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("prompt missing runtime tool %q: %q", expected, prompt)
		}
	}
	for _, expected := range []string{"Доступные привязки учетных данных", "GH_TOKEN", "KUBERNETES_SERVICE_HOST", "/var/run/secrets/kubernetes.io/serviceaccount/token"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("prompt missing credential binding %q: %q", expected, prompt)
		}
	}
	for _, expected := range []string{
		"mattermost_update_turn_status",
		"--body-file",
		"Не встраивай Markdown",
		"mattermost_list_chats(target_agent=",
		"mattermost_get_chat(chat=",
		"mattermost_start_agent_thread(target_chat=",
		"mattermost_return_to_requester(message=",
		"конфигурация проекта в MatterCodex является источником истины",
		"не зашивай имя чата",
		"MatterCodex alias привязки `github-platform-owner`",
		"ожидаемый аутентифицированный GitHub login `ai-da-stas`",
		"Alias привязки не обязан совпадать с login",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("prompt missing runtime contract %q: %q", expected, prompt)
		}
	}
	for _, expected := range []string{"заголовки и описания пул-реквестов", "строчные замечания ревью", "пиши на English"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("prompt missing language contract %q: %q", expected, prompt)
		}
	}
	for _, expected := range []string{"RADAR_AUTO_KUBECONFIG", "kubeconfig for the external radar-auto cluster"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("prompt missing runtime variable %q: %q", expected, prompt)
		}
	}
}

func testRoleByName(t *testing.T, store *fakeAdminStore, projectID int64, name string) entity.AgentRole {
	t.Helper()
	roles, err := store.ListAgentRoles(context.Background(), projectID)
	if err != nil {
		t.Fatalf("list roles: %v", err)
	}
	for _, role := range roles {
		if role.Name == name {
			return role
		}
	}
	t.Fatalf("role %q not found in project %d: %#v", name, projectID, roles)
	return entity.AgentRole{}
}

func testParticipantsContainRole(participants []entity.ChatParticipant, roleID int64) bool {
	for _, participant := range participants {
		if participant.RoleID == roleID && participant.Enabled {
			return true
		}
	}
	return false
}

func TestBuildRolePromptExposesRuntimeToolsAndSecretsToTemplate(t *testing.T) {
	prompt, err := BuildRolePrompt(RolePromptInput{
		Project: entity.Project{Name: "Platform", Slug: "platform"},
		Role: entity.AgentRole{
			Name:           "developer",
			RoleType:       "worker",
			PromptTemplate: "{{range .Tools}}{{.Command}}={{.Version}}\n{{end}}{{range .Secrets}}{{.Name}}={{.Env}}|{{.File}}\n{{end}}",
		},
		Chat: entity.Chat{Name: "Backend", ChatType: "worker_reviewer"},
		RuntimeVariables: []entity.ProjectRuntimeVariable{{
			Name:        "STAGING_DB_URL",
			Description: "staging database DSN",
			Enabled:     true,
		}},
		UserMessage: "Run codegen.",
	})
	if err != nil {
		t.Fatalf("BuildRolePrompt() error = %v", err)
	}
	for _, expected := range []string{
		"codex=0.144.1",
		"gh=2.95.0",
		"go=1.26",
		"goose=3.27.1",
		"oapi-codegen=2.7.1",
		"openapi-ts=0.98.2",
		"asyncapi=6.0.2",
		"modelina=5.10.1",
		"playwright=1.61.1",
		"playwright-mcp=0.0.77",
		"wait-on=9.0.10",
		"GitHub-аккаунт=GH_TOKEN",
		"Kubernetes service account=KUBERNETES_SERVICE_HOST",
		"Проектная переменная STAGING_DB_URL=STAGING_DB_URL",
		"mattermost_update_turn_status",
		"--body-file",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("prompt missing %q: %q", expected, prompt)
		}
	}
}

func TestRepositoryOnboardingListsGitHubAccounts(t *testing.T) {
	store := &fakeAdminStore{
		githubAccounts: map[string]entity.GitHubAccount{
			"agent":    {Name: "agent", SecretRef: "matter-codex-github-agent", Status: "configured", Username: "agent-bot", Email: "agent@example.invalid"},
			"disabled": {Name: "disabled", SecretRef: "matter-codex-github-disabled", Status: "disabled"},
		},
	}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:     localizer,
		StatusService: testStatusService(localizer),
		Store:         store,
		MenuActionURL: "http://bot-service/mattermost/actions/agents",
		StorageReady:  true,
	})

	result := svc.HandleMenuAction(context.Background(), MenuActionCommand{
		View:     menuViewRepositories,
		Action:   menuActionRepositoryOnboard,
		Resource: menuResourceRepository,
	})

	if result.Card == nil || result.Card.Title != "Choose GitHub account" {
		t.Fatalf("card = %#v", result.Card)
	}
	action, ok := mattermostActionByID(result.Card.Actions, "repoaccount1")
	if !ok || action.Context["action"] != menuActionRepositoryRepos || action.Context["resource_id"] != "agent" {
		t.Fatalf("account action = %#v", result.Card.Actions)
	}
	disabled, ok := mattermostActionByID(result.Card.Actions, "repoaccount2")
	if !ok || !disabled.Disabled {
		t.Fatalf("disabled account action = %#v", result.Card.Actions)
	}
	assertCardDoesNotExposeSlashCommand(t, result.Card)
}

func TestRepositoryOnboardingConnectsRepositoryWithAccount(t *testing.T) {
	store := &fakeAdminStore{
		githubAccounts: map[string]entity.GitHubAccount{
			"agent": {Name: "agent", SecretRef: "matter-codex-github-agent", Status: "configured"},
		},
	}
	provider := &fakeGitHubRepositoryProvider{
		candidates: []providerrepo.RepositoryCandidate{{
			Provider:      "github",
			Owner:         "codex-k8s",
			Name:          "matter-codex",
			FullName:      "codex-k8s/matter-codex",
			DefaultBranch: "main",
		}},
		branches: []providerrepo.BranchCandidate{{Name: "main"}},
	}
	channels := &fakeChannelManager{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:                localizer,
		StatusService:            testStatusService(localizer),
		Store:                    store,
		ChannelManager:           channels,
		GitHubRepositoryProvider: provider,
		DefaultTeamName:          "agents",
		MenuActionURL:            "http://bot-service/mattermost/actions/agents",
		GitHubWebhookConfigured:  true,
		StorageReady:             true,
	})

	repositories := svc.HandleMenuAction(context.Background(), MenuActionCommand{
		View:     menuViewRepositories,
		Action:   menuActionRepositoryRepos,
		Resource: menuResourceGitHubAccount,
		ID:       "agent",
	})
	if repositories.Card == nil {
		t.Fatal("repositories card is nil")
	}
	if provider.listAccount.Name != "agent" || provider.listOwner != "" || provider.listOwnerType != "" {
		t.Fatalf("provider list = account %#v owner %q type %q", provider.listAccount, provider.listOwner, provider.listOwnerType)
	}
	candidateAction, ok := mattermostActionByID(repositories.Card.Actions, "repocandidate1")
	if !ok {
		t.Fatalf("candidate action missing: %#v", repositories.Card.Actions)
	}
	branches := svc.HandleMenuAction(context.Background(), MenuActionCommand{
		View:     menuViewRepositories,
		Action:   menuActionRepositoryBranches,
		Resource: menuResourceRepository,
		ID:       fmt.Sprint(candidateAction.Context["resource_id"]),
	})
	if branches.Card == nil {
		t.Fatal("branches card is nil")
	}
	branchAction, ok := mattermostActionByID(branches.Card.Actions, "repobranch1")
	if !ok {
		t.Fatalf("branch action missing: %#v", branches.Card.Actions)
	}
	connected := svc.HandleMenuAction(context.Background(), MenuActionCommand{
		View:     menuViewRepositories,
		Action:   menuActionRepositoryConnect,
		Resource: menuResourceRepository,
		ID:       fmt.Sprint(branchAction.Context["resource_id"]),
		UserName: "owner",
	})

	if connected.Card == nil || !strings.Contains(connected.Card.Text, "github account: `agent`") {
		t.Fatalf("connected card = %#v", connected.Card)
	}
	if store.upsert.GitHubAccountName != "agent" || store.upsert.Owner != "codex-k8s" || store.upsert.Name != "matter-codex" || store.upsert.DefaultBranch != "main" {
		t.Fatalf("upsert = %#v", store.upsert)
	}
	if provider.listAccount.Name != "agent" || provider.branchesAccount.Name != "agent" || provider.webhookAccount.Name != "agent" {
		t.Fatalf("provider accounts = list %#v branches %#v webhook %#v", provider.listAccount, provider.branchesAccount, provider.webhookAccount)
	}
	if channels.channelName != "repo-codex-k8s-matter-codex" {
		t.Fatalf("channelName = %q", channels.channelName)
	}
	openAction, ok := mattermostActionByID(connected.Card.Actions, "openrepo")
	if !ok || openAction.Context["resource_id"] != "github:codex-k8s/matter-codex" {
		t.Fatalf("open repo action = %#v", connected.Card.Actions)
	}
}

func TestProjectRepositoryOnboardingUsesProjectGitHubAccount(t *testing.T) {
	store := &fakeAdminStore{
		projects: map[int64]entity.Project{
			1: {ID: 1, Name: "Demo Project", Slug: "demo-project", GitHubAccountName: "project-gh", GitHubOwner: "codex-k8s", GitHubOwnerType: "org"},
		},
		githubAccounts: map[string]entity.GitHubAccount{
			"project-gh": {Name: "project-gh", SecretRef: "matter-codex-github-project", Status: "configured"},
		},
	}
	provider := &fakeGitHubRepositoryProvider{
		candidates: []providerrepo.RepositoryCandidate{{
			Provider:      "github",
			Owner:         "codex-k8s",
			Name:          "kodex",
			FullName:      "codex-k8s/kodex",
			DefaultBranch: "main",
		}},
		branches: []providerrepo.BranchCandidate{{Name: "main"}},
	}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:                localizer,
		StatusService:            testStatusService(localizer),
		Store:                    store,
		ChannelManager:           &fakeChannelManager{},
		GitHubRepositoryProvider: provider,
		DefaultTeamName:          "agents",
		MenuActionURL:            "http://bot-service/mattermost/actions/agents",
		StorageReady:             true,
		GitHubWebhookConfigured:  true,
	})

	repositories := svc.HandleMenuAction(context.Background(), MenuActionCommand{
		View:     menuViewProjects,
		Action:   menuActionRepositoryOnboard,
		Resource: menuResourceProject,
		ID:       "1",
	})
	if repositories.Card == nil {
		t.Fatal("repositories card is nil")
	}
	if provider.listAccount.Name != "project-gh" || provider.listOwner != "codex-k8s" || provider.listOwnerType != "org" {
		t.Fatalf("provider list = account %#v owner %q type %q", provider.listAccount, provider.listOwner, provider.listOwnerType)
	}
	candidateAction, ok := mattermostActionByID(repositories.Card.Actions, "repocandidate1")
	if !ok {
		t.Fatalf("candidate action missing: %#v", repositories.Card.Actions)
	}
	branches := svc.HandleMenuAction(context.Background(), MenuActionCommand{
		View:     menuViewProjects,
		Action:   menuActionRepositoryBranches,
		Resource: menuResourceRepository,
		ID:       fmt.Sprint(candidateAction.Context["resource_id"]),
	})
	if branches.Card == nil {
		t.Fatal("branches card is nil")
	}
	branchAction, ok := mattermostActionByID(branches.Card.Actions, "repobranch1")
	if !ok {
		t.Fatalf("branch action missing: %#v", branches.Card.Actions)
	}
	connected := svc.HandleMenuAction(context.Background(), MenuActionCommand{
		View:     menuViewProjects,
		Action:   menuActionRepositoryConnect,
		Resource: menuResourceRepository,
		ID:       fmt.Sprint(branchAction.Context["resource_id"]),
		UserName: "owner",
	})

	if connected.Card == nil || !strings.Contains(connected.Card.Text, "github account: `project-gh`") {
		t.Fatalf("connected card = %#v", connected.Card)
	}
	if store.upsert.GitHubAccountName != "project-gh" || store.upsert.Owner != "codex-k8s" || store.upsert.Name != "kodex" {
		t.Fatalf("upsert = %#v", store.upsert)
	}
	if provider.listAccount.Name != "project-gh" || provider.branchesAccount.Name != "project-gh" || provider.webhookAccount.Name != "project-gh" {
		t.Fatalf("provider accounts = list %#v branches %#v webhook %#v", provider.listAccount, provider.branchesAccount, provider.webhookAccount)
	}
	bindings, err := store.ListProjectRepositories(context.Background(), 1)
	if err != nil || len(bindings) != 1 {
		t.Fatalf("bindings = %#v err=%v", bindings, err)
	}
	if bindings[0].FullName() != "codex-k8s/kodex" || !bindings[0].IsDefault {
		t.Fatalf("binding = %#v", bindings[0])
	}
}

func TestRepositorySearchDialogUsesSelectForms(t *testing.T) {
	store := &fakeAdminStore{
		githubAccounts: map[string]entity.GitHubAccount{
			"agent": {Name: "agent", SecretRef: "matter-codex-github-agent", Status: "configured"},
		},
	}
	provider := &fakeGitHubRepositoryProvider{
		candidates: []providerrepo.RepositoryCandidate{{
			Provider:      "github",
			Owner:         "codex-k8s",
			Name:          "matter-codex",
			FullName:      "codex-k8s/matter-codex",
			DefaultBranch: "main",
		}},
		branches: []providerrepo.BranchCandidate{{Name: "main"}, {Name: "feature"}},
	}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:                localizer,
		StatusService:            testStatusService(localizer),
		Store:                    store,
		GitHubRepositoryProvider: provider,
		MenuActionURL:            "http://bot-service/mattermost/actions/agents",
		ChannelManager:           &fakeChannelManager{},
		DefaultTeamName:          "agents",
		StorageReady:             true,
	})

	search := svc.HandleDialogSubmission(context.Background(), DialogSubmissionCommand{
		CallbackID: dialogCallbackRepositorySearch,
		State: encodeDialogState(MenuActionCommand{
			View:      menuViewRepositories,
			ID:        "agent",
			ChannelID: "channel-1",
			PostID:    "post-1",
		}),
		UserID:    "owner-id",
		ChannelID: "channel-1",
		Submission: map[string]any{
			dialogFieldSearch: "matter-codex",
		},
	})

	if search.Error != "" || len(search.Errors) > 0 {
		t.Fatalf("search dialog errors = %q %#v", search.Error, search.Errors)
	}
	if search.Dialog == nil || search.Dialog.CallbackID != dialogCallbackRepositorySearchPick {
		t.Fatalf("search dialog = %#v", search.Dialog)
	}
	if len(search.Dialog.Elements) != 1 || search.Dialog.Elements[0].Name != dialogFieldRepositoryChoice || len(search.Dialog.Elements[0].Options) != 1 {
		t.Fatalf("search dialog elements = %#v", search.Dialog.Elements)
	}
	if provider.searchQuery != "matter-codex" || provider.searchAccount.Name != "agent" {
		t.Fatalf("search = query %q account %#v", provider.searchQuery, provider.searchAccount)
	}
	repositoryChoice := search.Dialog.Elements[0].Options[0].Value
	branches := svc.HandleDialogSubmission(context.Background(), DialogSubmissionCommand{
		CallbackID: dialogCallbackRepositorySearchPick,
		State:      search.Dialog.State,
		UserID:     "owner-id",
		ChannelID:  "channel-1",
		Submission: map[string]any{
			dialogFieldRepositoryChoice: repositoryChoice,
		},
	})
	if branches.Error != "" || len(branches.Errors) > 0 {
		t.Fatalf("branch dialog errors = %q %#v", branches.Error, branches.Errors)
	}
	if branches.Dialog == nil || branches.Dialog.CallbackID != dialogCallbackRepositorySearchBranch {
		t.Fatalf("branch dialog = %#v", branches.Dialog)
	}
	if len(branches.Dialog.Elements) != 1 || branches.Dialog.Elements[0].Name != dialogFieldBranchChoice || len(branches.Dialog.Elements[0].Options) != 2 {
		t.Fatalf("branch dialog elements = %#v", branches.Dialog.Elements)
	}
	branchChoice := branches.Dialog.Elements[0].Options[0].Value
	connected := svc.HandleDialogSubmission(context.Background(), DialogSubmissionCommand{
		CallbackID: dialogCallbackRepositorySearchBranch,
		State:      branches.Dialog.State,
		UserID:     "owner-id",
		UserName:   "owner",
		ChannelID:  "channel-1",
		Submission: map[string]any{
			dialogFieldBranchChoice: branchChoice,
		},
	})
	if connected.Error != "" || len(connected.Errors) > 0 {
		t.Fatalf("connect errors = %q %#v", connected.Error, connected.Errors)
	}
	if connected.Card == nil || !strings.Contains(connected.Card.Text, "github account: `agent`") {
		t.Fatalf("connected card = %#v", connected.Card)
	}
	if store.upsert.GitHubAccountName != "agent" || store.upsert.Owner != "codex-k8s" || store.upsert.Name != "matter-codex" || store.upsert.DefaultBranch != "main" {
		t.Fatalf("upsert = %#v", store.upsert)
	}
}

func TestOpenAIAuthDialogSubmissionStartsCustomAccount(t *testing.T) {
	store := &fakeAdminStore{}
	runner := &fakeRuntimeRunner{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:           localizer,
		StatusService:       testStatusService(localizer),
		Store:               store,
		RuntimeRunner:       runner,
		DialogSubmitURL:     "http://bot-service/mattermost/dialogs/agents",
		CodexAuthSecretName: "matter-codex-codex-auth",
		StorageReady:        true,
	})

	result := svc.HandleDialogSubmission(context.Background(), DialogSubmissionCommand{
		CallbackID: dialogCallbackOpenAIAuth,
		State:      encodeDialogState(MenuActionCommand{View: menuViewOpenAI, ChannelID: "channel-1", PostID: "post-1"}),
		UserID:     "owner-id",
		UserName:   "owner",
		Submission: map[string]any{
			dialogFieldAccount: "reviewer-plus",
		},
	})

	if result.Error != "" || len(result.Errors) > 0 {
		t.Fatalf("dialog errors = %q %#v", result.Error, result.Errors)
	}
	if runner.authAccount != "reviewer-plus" || runner.authSecret != "matter-codex-codex-auth-reviewer-plus" {
		t.Fatalf("runner auth = account %q secret %q", runner.authAccount, runner.authSecret)
	}
	account := store.openAIAccounts["reviewer-plus"]
	if account.Status != "auth_pending" || account.SecretRef != "matter-codex-codex-auth-reviewer-plus" {
		t.Fatalf("openAI account = %#v", account)
	}
	if result.Card == nil || !strings.Contains(result.Card.Text, "reviewer-plus") {
		t.Fatalf("card = %#v", result.Card)
	}
}

func TestOpenAIAccountDialogSubmissionDeletesAccount(t *testing.T) {
	store := &fakeAdminStore{
		openAIAccounts: map[string]entity.OpenAIAccount{
			"reviewer-test": {Name: "reviewer-test", SecretRef: "matter-codex-codex-auth-reviewer-test", Status: "authorized"},
		},
	}
	runner := &fakeRuntimeRunner{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:       localizer,
		StatusService:   testStatusService(localizer),
		Store:           store,
		RuntimeRunner:   runner,
		DialogSubmitURL: "http://bot-service/mattermost/dialogs/agents",
		StorageReady:    true,
	})

	result := svc.HandleDialogSubmission(context.Background(), DialogSubmissionCommand{
		CallbackID: dialogCallbackOpenAIDelete,
		State:      encodeDialogState(MenuActionCommand{View: menuViewOpenAI, ChannelID: "channel-1", PostID: "post-1"}),
		UserID:     "owner-id",
		UserName:   "owner",
		Submission: map[string]any{
			dialogFieldAccount: "reviewer-test",
			dialogFieldConfirm: "delete",
		},
	})

	if result.Error != "" || len(result.Errors) > 0 {
		t.Fatalf("dialog errors = %q %#v", result.Error, result.Errors)
	}
	if runner.deletedAuthAccount != "reviewer-test" || runner.deletedAuthSecret != "matter-codex-codex-auth-reviewer-test" {
		t.Fatalf("deleted runtime auth = account %q secret %q", runner.deletedAuthAccount, runner.deletedAuthSecret)
	}
	if _, ok := store.openAIAccounts["reviewer-test"]; ok {
		t.Fatalf("openAI account was not deleted: %#v", store.openAIAccounts["reviewer-test"])
	}
	if store.deletedOpenAIAccount.Name != "reviewer-test" {
		t.Fatalf("deleted openAI account = %#v", store.deletedOpenAIAccount)
	}
	if result.Card == nil || !strings.Contains(result.Card.Text, "auth secret deleted") {
		t.Fatalf("card = %#v", result.Card)
	}
}

func TestOpenAIAccountDialogSubmissionBlocksProfileAccountDeletion(t *testing.T) {
	store := &fakeAdminStore{
		profiles:       []entity.AgentProfile{{Name: "reviewer", OpenAIAccountName: "primary"}},
		openAIAccounts: map[string]entity.OpenAIAccount{"primary": {Name: "primary", SecretRef: "matter-codex-codex-auth-primary", Status: "authorized"}},
	}
	runner := &fakeRuntimeRunner{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:       localizer,
		StatusService:   testStatusService(localizer),
		Store:           store,
		RuntimeRunner:   runner,
		DialogSubmitURL: "http://bot-service/mattermost/dialogs/agents",
		StorageReady:    true,
	})

	result := svc.HandleDialogSubmission(context.Background(), DialogSubmissionCommand{
		CallbackID: dialogCallbackOpenAIDelete,
		State:      encodeDialogState(MenuActionCommand{View: menuViewOpenAI, ChannelID: "channel-1", PostID: "post-1"}),
		UserID:     "owner-id",
		UserName:   "owner",
		Submission: map[string]any{
			dialogFieldAccount: "primary",
			dialogFieldConfirm: "delete",
		},
	})

	if result.Error != "" || len(result.Errors) > 0 {
		t.Fatalf("dialog errors = %q %#v", result.Error, result.Errors)
	}
	if result.Card == nil || !strings.Contains(result.Card.Text, "reviewer") {
		t.Fatalf("card = %#v", result.Card)
	}
	if runner.deletedAuthAccount != "" {
		t.Fatalf("runtime auth should not be deleted, got %q", runner.deletedAuthAccount)
	}
	if _, ok := store.openAIAccounts["primary"]; !ok {
		t.Fatal("profile-bound OpenAI account should not be deleted")
	}
}

func TestGitHubAccountDialogSubmissionAddsAccount(t *testing.T) {
	store := &fakeAdminStore{githubAccounts: map[string]entity.GitHubAccount{}}
	runner := &fakeRuntimeRunner{}
	inspector := &fakeGitHubAccountInspector{
		inspection: providerrepo.GitHubTokenInspection{
			Username: "reviewer-user",
			Email:    "reviewer@example.com",
			Scopes:   []string{"repo", "workflow"},
		},
	}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:              localizer,
		StatusService:          testStatusService(localizer),
		Store:                  store,
		GitHubAccountInspector: inspector,
		RuntimeRunner:          runner,
		GitHubSecretName:       "matter-codex-github",
		DialogSubmitURL:        "http://bot-service/mattermost/dialogs/agents",
		StorageReady:           true,
	})

	result := svc.HandleDialogSubmission(context.Background(), DialogSubmissionCommand{
		CallbackID: dialogCallbackGitHubAccountAdd,
		State:      encodeDialogState(MenuActionCommand{View: menuViewGitHub, ChannelID: "channel-1", PostID: "post-1"}),
		UserID:     "owner-id",
		UserName:   "owner",
		Submission: map[string]any{
			dialogFieldAccount: "reviewer",
			dialogFieldToken:   "test-token-dialog",
		},
	})

	if result.Error != "" || len(result.Errors) > 0 {
		t.Fatalf("dialog errors = %q %#v", result.Error, result.Errors)
	}
	if inspector.token != "test-token-dialog" {
		t.Fatalf("inspector token = %q", inspector.token)
	}
	if runner.githubSecretInput.SecretName != "matter-codex-github-reviewer" || runner.githubSecretInput.Token != "test-token-dialog" || runner.githubSecretInput.Username != "reviewer-user" {
		t.Fatalf("github secret input = %#v", runner.githubSecretInput)
	}
	account := store.githubAccounts["reviewer"]
	if account.SecretRef != "matter-codex-github-reviewer" || account.Username != "reviewer-user" || account.Email != "reviewer@example.com" || account.Scopes != "repo, workflow" || account.Status != "configured" {
		t.Fatalf("github account = %#v", account)
	}
	if store.githubUpsert.CredentialName != "github:reviewer" {
		t.Fatalf("github upsert = %#v", store.githubUpsert)
	}
	if !store.auditRecorded {
		t.Fatal("audit event was not recorded")
	}
	if result.Card == nil || !strings.Contains(result.Card.Text, "reviewer") {
		t.Fatalf("card = %#v", result.Card)
	}
}

func TestGitHubAccountDialogSubmissionDeletesAccount(t *testing.T) {
	store := &fakeAdminStore{githubAccounts: map[string]entity.GitHubAccount{
		"reviewer": {Name: "reviewer", SecretRef: "matter-codex-github-reviewer", Status: "configured"},
	}}
	runner := &fakeRuntimeRunner{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:        localizer,
		StatusService:    testStatusService(localizer),
		Store:            store,
		RuntimeRunner:    runner,
		GitHubSecretName: "matter-codex-github",
		DialogSubmitURL:  "http://bot-service/mattermost/dialogs/agents",
		StorageReady:     true,
	})

	result := svc.HandleDialogSubmission(context.Background(), DialogSubmissionCommand{
		CallbackID: dialogCallbackGitHubAccountDelete,
		State:      encodeDialogState(MenuActionCommand{View: menuViewGitHub}),
		Submission: map[string]any{
			dialogFieldAccount: "reviewer",
			dialogFieldConfirm: "delete",
		},
	})

	if result.Error != "" || len(result.Errors) > 0 {
		t.Fatalf("dialog errors = %q %#v", result.Error, result.Errors)
	}
	if _, ok := store.githubAccounts["reviewer"]; ok {
		t.Fatalf("github account was not deleted: %#v", store.githubAccounts)
	}
	if store.deletedGitHubAccount.Name != "reviewer" {
		t.Fatalf("deletedGitHubAccount = %#v", store.deletedGitHubAccount)
	}
	if runner.deletedGitHubSecretAccount != "reviewer" || runner.deletedGitHubSecretName != "matter-codex-github-reviewer" {
		t.Fatalf("deleted github secret = account %q secret %q", runner.deletedGitHubSecretAccount, runner.deletedGitHubSecretName)
	}
	if result.Card == nil || !strings.Contains(result.Card.Text, "managed secret deleted") {
		t.Fatalf("card = %#v", result.Card)
	}
}

func TestProfileDialogSubmissionRejectsNewClusterAdmin(t *testing.T) {
	store := &fakeAdminStore{
		profiles:       []entity.AgentProfile{{Name: "developer", Role: "developer", Enabled: true, OpenAIAccountName: "primary", GitHubAccountName: "agent"}},
		openAIAccounts: map[string]entity.OpenAIAccount{"primary": {Name: "primary", SecretRef: "matter-codex-codex-auth-primary", Status: "authorized"}},
		githubAccounts: map[string]entity.GitHubAccount{"agent": {Name: "agent", SecretRef: "matter-codex-github-agent", Status: "configured"}},
	}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:       localizer,
		StatusService:   testStatusService(localizer),
		Store:           store,
		DialogSubmitURL: "http://bot-service/mattermost/dialogs/agents",
		StorageReady:    true,
	})

	result := svc.HandleDialogSubmission(context.Background(), DialogSubmissionCommand{
		CallbackID: dialogCallbackProfileUpsert,
		State:      encodeDialogState(MenuActionCommand{View: menuViewProfiles, ChannelID: "channel-1", PostID: "post-1"}),
		UserID:     "owner-id",
		UserName:   "owner",
		Submission: map[string]any{
			dialogFieldProfile:          "deployer",
			dialogFieldRole:             "deployer",
			dialogFieldOpenAIAccount:    "primary",
			dialogFieldGitHubAccount:    "agent",
			dialogFieldKubernetesAccess: "cluster-admin",
			dialogFieldSandboxMode:      "workspace-write",
			dialogFieldDescription:      "Deployment profile",
			dialogFieldConfigOverlay:    "sandbox_mode = \"workspace-write\"",
		},
	})

	if result.Error == "" {
		t.Fatalf("cluster-admin submission unexpectedly succeeded: %#v", result)
	}
	if _, err := store.GetAgentProfile(context.Background(), "deployer"); !errors.Is(err, adminrepo.ErrNotFound) {
		t.Fatalf("forbidden profile lookup error = %v", err)
	}
	if _, ok := store.promptTemplates[promptTemplateMapKey("deployer", developerImplementTaskKey)]; ok {
		t.Fatalf("forbidden profile seeded prompt: %#v", store.promptTemplates)
	}
}

func TestProfileDialogSubmissionCreatesArchitectPromptSeed(t *testing.T) {
	store := &fakeAdminStore{
		openAIAccounts: map[string]entity.OpenAIAccount{"primary": {Name: "primary", SecretRef: "matter-codex-codex-auth-primary", Status: "authorized"}},
		githubAccounts: map[string]entity.GitHubAccount{"agent": {Name: "agent", SecretRef: "matter-codex-github-agent", Status: "configured"}},
	}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:       localizer,
		StatusService:   testStatusService(localizer),
		Store:           store,
		DialogSubmitURL: "http://bot-service/mattermost/dialogs/agents",
		StorageReady:    true,
	})

	result := svc.HandleDialogSubmission(context.Background(), DialogSubmissionCommand{
		CallbackID: dialogCallbackProfileUpsert,
		State:      encodeDialogState(MenuActionCommand{View: menuViewProfiles, ChannelID: "channel-1", PostID: "post-1"}),
		UserID:     "owner-id",
		UserName:   "owner",
		Submission: map[string]any{
			dialogFieldProfile:          "architect",
			dialogFieldRole:             "architect",
			dialogFieldOpenAIAccount:    "primary",
			dialogFieldGitHubAccount:    "agent",
			dialogFieldKubernetesAccess: "read-only",
			dialogFieldSandboxMode:      "workspace-write",
			dialogFieldDescription:      "Architecture profile",
		},
	})

	if result.Error != "" || len(result.Errors) > 0 {
		t.Fatalf("dialog errors = %q %#v", result.Error, result.Errors)
	}
	item, err := store.GetAgentPromptTemplate(context.Background(), "architect", architectDocsTaskKey)
	if err != nil {
		t.Fatalf("architect prompt seed was not saved: %v", err)
	}
	if !strings.Contains(item.Body, "- Проект: {{.Project.Name}}") ||
		!strings.Contains(item.Body, "Язык владельца: {{.Locale.Language}}") ||
		!strings.Contains(item.Body, "MatterCodex") ||
		!strings.Contains(item.Body, "MCP") {
		t.Fatalf("architect seed body missing generic locale/MCP policy:\n%s", item.Body)
	}
}

func TestPromptEditDialogRendersBeforeSave(t *testing.T) {
	store := &fakeAdminStore{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:       localizer,
		StatusService:   testStatusService(localizer),
		Store:           store,
		DialogSubmitURL: "http://bot-service/mattermost/dialogs/agents",
		StorageReady:    true,
	})
	body := "Review {{.Repository.FullName}} in {{.Locale.Language}}."

	result := svc.HandleDialogSubmission(context.Background(), DialogSubmissionCommand{
		CallbackID: dialogCallbackPromptEdit,
		State:      encodeDialogState(MenuActionCommand{View: menuViewPrompts, ID: promptTemplateResourceID("reviewer", reviewPRTemplateKey), ChannelID: "channel-1", PostID: "post-1"}),
		UserID:     "owner-id",
		UserName:   "owner",
		Submission: map[string]any{
			dialogFieldTemplateBody: body,
		},
	})

	if result.Error != "" || len(result.Errors) > 0 {
		t.Fatalf("dialog errors = %q %#v", result.Error, result.Errors)
	}
	item, err := store.GetAgentPromptTemplate(context.Background(), "reviewer", reviewPRTemplateKey)
	if err != nil || item.Body != body {
		t.Fatalf("prompt template = %#v err=%v", item, err)
	}
	if result.Card == nil || !strings.Contains(result.Card.Text, "Review codex-k8s/matter-codex in English") {
		t.Fatalf("card = %#v", result.Card)
	}
}

func TestGitHubAccountDialogSubmissionBlocksProfileAccountDeletion(t *testing.T) {
	store := &fakeAdminStore{
		profiles: []entity.AgentProfile{{Name: "developer", GitHubAccountName: "reviewer"}},
		githubAccounts: map[string]entity.GitHubAccount{
			"reviewer": {Name: "reviewer", SecretRef: "matter-codex-github-reviewer", Status: "configured"},
		},
	}
	runner := &fakeRuntimeRunner{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:        localizer,
		StatusService:    testStatusService(localizer),
		Store:            store,
		RuntimeRunner:    runner,
		GitHubSecretName: "matter-codex-github",
		DialogSubmitURL:  "http://bot-service/mattermost/dialogs/agents",
		StorageReady:     true,
	})

	result := svc.HandleDialogSubmission(context.Background(), DialogSubmissionCommand{
		CallbackID: dialogCallbackGitHubAccountDelete,
		State:      encodeDialogState(MenuActionCommand{View: menuViewGitHub}),
		Submission: map[string]any{
			dialogFieldAccount: "reviewer",
			dialogFieldConfirm: "delete",
		},
	})

	if result.Error != "" || len(result.Errors) > 0 {
		t.Fatalf("dialog errors = %q %#v", result.Error, result.Errors)
	}
	if result.Card == nil || !strings.Contains(result.Card.Text, "developer") {
		t.Fatalf("card = %#v", result.Card)
	}
	if _, ok := store.githubAccounts["reviewer"]; !ok {
		t.Fatal("profile-bound GitHub account should not be deleted")
	}
	if runner.deletedGitHubSecretAccount != "" {
		t.Fatalf("github secret should not be deleted, got %q", runner.deletedGitHubSecretAccount)
	}
}

func TestGitHubAccountDialogSubmissionBlocksRepositoryAccountDeletion(t *testing.T) {
	store := &fakeAdminStore{
		githubAccounts: map[string]entity.GitHubAccount{
			"reviewer": {Name: "reviewer", SecretRef: "matter-codex-github-reviewer", Status: "configured"},
		},
		repositories: map[string]entity.Repository{
			repositoryStoreKey("github", "codex-k8s", "matter-codex"): {
				Provider:          "github",
				Owner:             "codex-k8s",
				Name:              "matter-codex",
				DefaultBranch:     "main",
				GitHubAccountName: "reviewer",
				Status:            "active",
				MattermostChannel: "repo-codex-k8s-matter-codex",
			},
		},
	}
	runner := &fakeRuntimeRunner{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:        localizer,
		StatusService:    testStatusService(localizer),
		Store:            store,
		RuntimeRunner:    runner,
		GitHubSecretName: "matter-codex-github",
		DialogSubmitURL:  "http://bot-service/mattermost/dialogs/agents",
		StorageReady:     true,
	})

	result := svc.HandleDialogSubmission(context.Background(), DialogSubmissionCommand{
		CallbackID: dialogCallbackGitHubAccountDelete,
		State:      encodeDialogState(MenuActionCommand{View: menuViewGitHub}),
		Submission: map[string]any{
			dialogFieldAccount: "reviewer",
			dialogFieldConfirm: "delete",
		},
	})

	if result.Error != "" || len(result.Errors) > 0 {
		t.Fatalf("dialog errors = %q %#v", result.Error, result.Errors)
	}
	if result.Card == nil || !strings.Contains(result.Card.Text, "codex-k8s/matter-codex") {
		t.Fatalf("card = %#v", result.Card)
	}
	if _, ok := store.githubAccounts["reviewer"]; !ok {
		t.Fatal("repository-bound GitHub account should not be deleted")
	}
	if runner.deletedGitHubSecretAccount != "" {
		t.Fatalf("github secret should not be deleted, got %q", runner.deletedGitHubSecretAccount)
	}
}

func TestGitHubAccountDialogSubmissionBlocksProjectAccountDeletion(t *testing.T) {
	store := &fakeAdminStore{
		githubAccounts: map[string]entity.GitHubAccount{
			"project-gh": {Name: "project-gh", SecretRef: "matter-codex-github-project", Status: "configured"},
		},
		projects: map[int64]entity.Project{
			1: {ID: 1, Name: "Demo Project", Slug: "demo-project", GitHubAccountName: "project-gh"},
		},
	}
	runner := &fakeRuntimeRunner{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:        localizer,
		StatusService:    testStatusService(localizer),
		Store:            store,
		RuntimeRunner:    runner,
		GitHubSecretName: "matter-codex-github",
		DialogSubmitURL:  "http://bot-service/mattermost/dialogs/agents",
		StorageReady:     true,
	})

	result := svc.HandleDialogSubmission(context.Background(), DialogSubmissionCommand{
		CallbackID: dialogCallbackGitHubAccountDelete,
		State:      encodeDialogState(MenuActionCommand{View: menuViewGitHub}),
		Submission: map[string]any{
			dialogFieldAccount: "project-gh",
			dialogFieldConfirm: "delete",
		},
	})

	if result.Error != "" || len(result.Errors) > 0 {
		t.Fatalf("dialog errors = %q %#v", result.Error, result.Errors)
	}
	if result.Card == nil || !strings.Contains(result.Card.Text, "Demo Project") {
		t.Fatalf("card = %#v", result.Card)
	}
	if _, ok := store.githubAccounts["project-gh"]; !ok {
		t.Fatal("project-bound GitHub account should not be deleted")
	}
	if runner.deletedGitHubSecretAccount != "" {
		t.Fatalf("github secret should not be deleted, got %q", runner.deletedGitHubSecretAccount)
	}
}

func TestRepositoryDialogSubmissionDeletesRepository(t *testing.T) {
	store := &fakeAdminStore{
		repositories: map[string]entity.Repository{
			repositoryStoreKey("github", "codex-k8s", "matter-codex"): {
				Provider:          "github",
				Owner:             "codex-k8s",
				Name:              "matter-codex",
				DefaultBranch:     "main",
				Status:            "active",
				MattermostChannel: "repo-codex-k8s-matter-codex",
			},
		},
	}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:       localizer,
		StatusService:   testStatusService(localizer),
		Store:           store,
		MenuActionURL:   "http://bot-service/mattermost/actions/agents",
		DialogSubmitURL: "http://bot-service/mattermost/dialogs/agents",
		StorageReady:    true,
	})

	result := svc.HandleDialogSubmission(context.Background(), DialogSubmissionCommand{
		CallbackID: dialogCallbackRepositoryDelete,
		State:      encodeDialogState(MenuActionCommand{View: menuViewRepositories}),
		Submission: map[string]any{
			dialogFieldProvider:   "github",
			dialogFieldRepository: "codex-k8s/matter-codex",
			dialogFieldConfirm:    "delete",
		},
	})

	if result.Error != "" || len(result.Errors) > 0 {
		t.Fatalf("dialog errors = %q %#v", result.Error, result.Errors)
	}
	if store.deletedRepo.FullName() != "codex-k8s/matter-codex" {
		t.Fatalf("deletedRepo = %#v", store.deletedRepo)
	}
	if !store.auditRecorded {
		t.Fatal("audit event was not recorded")
	}
	if result.Card == nil || !strings.Contains(result.Card.Text, "metadata deleted") {
		t.Fatalf("card = %#v", result.Card)
	}
}

func TestRepositoryDialogDeleteRequiresConfirmation(t *testing.T) {
	store := &fakeAdminStore{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:     localizer,
		StatusService: testStatusService(localizer),
		Store:         store,
		StorageReady:  true,
	})

	result := svc.HandleDialogSubmission(context.Background(), DialogSubmissionCommand{
		CallbackID: dialogCallbackRepositoryDelete,
		State:      encodeDialogState(MenuActionCommand{View: menuViewRepositories}),
		Submission: map[string]any{
			dialogFieldProvider:   "github",
			dialogFieldRepository: "codex-k8s/matter-codex",
			dialogFieldConfirm:    "DELETE",
		},
	})

	if result.Errors[dialogFieldConfirm] == "" {
		t.Fatalf("confirm error is missing: %#v", result.Errors)
	}
}

func TestHelpMenuDoesNotExposeTypedHelpButton(t *testing.T) {
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:     localizer,
		StatusService: testStatusService(localizer),
		MenuActionURL: "http://bot-service/mattermost/actions/agents",
	})

	result := svc.HandleMenuAction(context.Background(), MenuActionCommand{View: menuViewHelp})

	if result.Card == nil {
		t.Fatal("card is nil")
	}
	if _, ok := mattermostActionByID(result.Card.Actions, "cmdhelp"); ok {
		t.Fatalf("help menu still exposes typed help action: %#v", result.Card.Actions)
	}
	for _, action := range result.Card.Actions {
		if _, ok := action.Context["command"]; ok {
			t.Fatalf("help menu action exposes command context: %#v", action)
		}
	}
	if _, ok := mattermostActionByID(result.Card.Actions, "menuaccounts"); !ok {
		t.Fatalf("help menu should link to accounts: %#v", result.Card.Actions)
	}
}

func TestMenuOpenAISectionUsesEntityListAction(t *testing.T) {
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:     localizer,
		StatusService: testStatusService(localizer),
		MenuActionURL: "http://bot-service/mattermost/actions/agents",
	})

	result := svc.HandleMenuAction(context.Background(), MenuActionCommand{View: menuViewOpenAI})

	if result.Card == nil {
		t.Fatal("card is nil")
	}
	if result.Card.Title != "OpenAI accounts" {
		t.Fatalf("card title = %q", result.Card.Title)
	}
	action, ok := mattermostActionByID(result.Card.Actions, "openailist")
	if !ok {
		t.Fatalf("openailist action is missing: %#v", result.Card.Actions)
	}
	if action.Context["action"] != menuActionList || action.Context["resource_type"] != menuResourceOpenAIAccount {
		t.Fatalf("action context = %#v", action.Context)
	}
	if strings.Contains(result.Card.Text, "/agents openai") {
		t.Fatalf("OpenAI card still leads with typed commands: %q", result.Card.Text)
	}
	assertCardDoesNotExposeSlashCommand(t, result.Card)
}

func TestMenuEntityListActionShowsOpenAIAccountCardButtons(t *testing.T) {
	store := &fakeAdminStore{
		openAIAccounts: map[string]entity.OpenAIAccount{
			"primary": {Name: "primary", Status: "authorized", SecretRef: "matter-codex-codex-auth-primary"},
		},
	}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:     localizer,
		StatusService: testStatusService(localizer),
		Store:         store,
		MenuActionURL: "http://bot-service/mattermost/actions/agents",
		StorageReady:  true,
	})

	result := svc.HandleMenuAction(context.Background(), MenuActionCommand{
		View:     menuViewOpenAI,
		Action:   menuActionList,
		Resource: menuResourceOpenAIAccount,
	})

	if result.Card == nil || result.Card.Title != "OpenAI accounts" {
		t.Fatalf("card = %#v", result.Card)
	}
	action, ok := mattermostActionByID(result.Card.Actions, "openopenai1")
	if !ok {
		t.Fatalf("open account action is missing: %#v", result.Card.Actions)
	}
	if action.Context["action"] != menuActionShow || action.Context["resource_type"] != menuResourceOpenAIAccount || action.Context["resource_id"] != "primary" {
		t.Fatalf("open account context = %#v", action.Context)
	}
	assertCardDoesNotExposeSlashCommand(t, result.Card)
}

func TestMenuOpenAIStatusActionShowsDeviceCodeInCard(t *testing.T) {
	store := &fakeAdminStore{
		openAIAccounts: map[string]entity.OpenAIAccount{
			"primary": {Name: "primary", Status: "awaiting_user", SecretRef: "matter-codex-codex-auth-primary"},
		},
	}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:         localizer,
		StatusService:     testStatusService(localizer),
		Store:             store,
		RuntimeRunner:     &fakeRuntimeRunner{},
		MenuActionURL:     "http://bot-service/mattermost/actions/agents",
		StorageReady:      true,
		RuntimeConfigured: true,
	})

	result := svc.HandleMenuAction(context.Background(), MenuActionCommand{
		View:     menuViewOpenAI,
		Action:   menuActionOpenAIStatus,
		Resource: menuResourceOpenAIAccount,
		ID:       "primary",
	})

	if result.EphemeralText != "" {
		t.Fatalf("ephemeral text = %q", result.EphemeralText)
	}
	if result.Card == nil || result.Card.Title != "Result - OpenAI accounts" {
		t.Fatalf("card = %#v", result.Card)
	}
	if !strings.Contains(result.Card.Text, "ABCD-12345") || !strings.Contains(result.Card.Text, "https://auth.openai.com") {
		t.Fatalf("card does not show device-code result: %q", result.Card.Text)
	}
	if _, ok := mattermostActionByID(result.Card.Actions, "openaiauthrestart"); !ok {
		t.Fatalf("auth restart action is missing: %#v", result.Card.Actions)
	}
	assertCardDoesNotExposeSlashCommand(t, result.Card)
}

func TestMenuOpenAIAuthActionRestartsExistingAccount(t *testing.T) {
	store := &fakeAdminStore{
		openAIAccounts: map[string]entity.OpenAIAccount{
			"primary": {Name: "primary", Status: "auth_failed", SecretRef: "matter-codex-codex-auth-primary"},
		},
	}
	runner := &fakeRuntimeRunner{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:           localizer,
		StatusService:       testStatusService(localizer),
		Store:               store,
		RuntimeRunner:       runner,
		MenuActionURL:       "http://bot-service/mattermost/actions/agents",
		CodexAuthSecretName: "matter-codex-codex-auth",
		StorageReady:        true,
		RuntimeConfigured:   true,
	})

	result := svc.HandleMenuAction(context.Background(), MenuActionCommand{
		View:     menuViewOpenAI,
		Action:   menuActionOpenAIAuth,
		Resource: menuResourceOpenAIAccount,
		ID:       "primary",
	})

	if result.EphemeralText != "" {
		t.Fatalf("ephemeral text = %q", result.EphemeralText)
	}
	if runner.authAccount != "primary" || runner.authSecret != "matter-codex-codex-auth-primary" {
		t.Fatalf("auth session = account %q secret %q", runner.authAccount, runner.authSecret)
	}
	account := store.openAIAccounts["primary"]
	if account.Status != "auth_pending" {
		t.Fatalf("account = %#v", account)
	}
	if result.Card == nil || !strings.Contains(result.Card.Text, "OpenAI account updated") {
		t.Fatalf("card = %#v", result.Card)
	}
	if _, ok := mattermostActionByID(result.Card.Actions, "openaistatus"); !ok {
		t.Fatalf("status action is missing: %#v", result.Card.Actions)
	}
	assertCardDoesNotExposeSlashCommand(t, result.Card)
}

func TestMenuGitHubAccountCardOffersEditDialog(t *testing.T) {
	store := &fakeAdminStore{
		githubAccounts: map[string]entity.GitHubAccount{
			"reviewer": {Name: "reviewer", SecretRef: "matter-codex-github-reviewer", Status: "configured", Username: "reviewer-user", Email: "reviewer@example.com", Scopes: "repo"},
		},
	}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:       localizer,
		StatusService:   testStatusService(localizer),
		Store:           store,
		MenuActionURL:   "http://bot-service/mattermost/actions/agents",
		DialogSubmitURL: "http://bot-service/mattermost/dialogs/agents",
		StorageReady:    true,
	})

	result := svc.HandleMenuAction(context.Background(), MenuActionCommand{
		View:     menuViewGitHub,
		Action:   menuActionShow,
		Resource: menuResourceGitHubAccount,
		ID:       "reviewer",
	})

	if result.Card == nil || result.Card.Title != "GitHub account `reviewer`" {
		t.Fatalf("card = %#v", result.Card)
	}
	action, ok := mattermostActionByID(result.Card.Actions, "githubedit")
	if !ok {
		t.Fatalf("edit action is missing: %#v", result.Card.Actions)
	}
	if action.Context["dialog"] != menuDialogGitHubAccountEdit || action.Context["resource_type"] != menuResourceGitHubAccount || action.Context["resource_id"] != "reviewer" {
		t.Fatalf("edit action context = %#v", action.Context)
	}
	assertCardDoesNotExposeSlashCommand(t, result.Card)
}

func TestMenuRepositoryDeleteUsesConfirmationCard(t *testing.T) {
	store := &fakeAdminStore{
		repositories: map[string]entity.Repository{
			repositoryStoreKey("github", "codex-k8s", "matter-codex"): {
				Provider:          "github",
				Owner:             "codex-k8s",
				Name:              "matter-codex",
				DefaultBranch:     "main",
				Status:            "active",
				MattermostChannel: "repo-codex-k8s-matter-codex",
			},
		},
	}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:     localizer,
		StatusService: testStatusService(localizer),
		Store:         store,
		MenuActionURL: "http://bot-service/mattermost/actions/agents",
		StorageReady:  true,
	})

	result := svc.HandleMenuAction(context.Background(), MenuActionCommand{
		View:     menuViewRepositories,
		Action:   menuActionConfirmDelete,
		Resource: menuResourceRepository,
		ID:       "github:codex-k8s/matter-codex",
	})

	if result.Card == nil || result.Card.Title != "Delete repository metadata?" {
		t.Fatalf("confirm card = %#v", result.Card)
	}
	confirm, ok := mattermostActionByID(result.Card.Actions, "repodelete")
	if !ok {
		t.Fatalf("confirm action is missing: %#v", result.Card.Actions)
	}
	if confirm.Context["action"] != menuActionDelete || confirm.Context["resource_id"] != "github:codex-k8s/matter-codex" {
		t.Fatalf("confirm context = %#v", confirm.Context)
	}

	deleted := svc.HandleMenuAction(context.Background(), MenuActionCommand{
		View:     menuViewRepositories,
		Action:   menuActionDelete,
		Resource: menuResourceRepository,
		ID:       "github:codex-k8s/matter-codex",
	})

	if deleted.Card == nil || !strings.Contains(deleted.Card.Text, "metadata deleted") {
		t.Fatalf("delete result = %#v", deleted.Card)
	}
	if store.deletedRepo.FullName() != "codex-k8s/matter-codex" {
		t.Fatalf("deleted repo = %#v", store.deletedRepo)
	}
	assertCardDoesNotExposeSlashCommand(t, deleted.Card)
}

func TestMenuCommandActionExecutesOpenAIList(t *testing.T) {
	store := &fakeAdminStore{
		openAIAccounts: map[string]entity.OpenAIAccount{
			"primary": {Name: "primary", Status: "authorized", SecretRef: "matter-codex-codex-auth-primary"},
		},
	}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:     localizer,
		StatusService: testStatusService(localizer),
		Store:         store,
		MenuActionURL: "http://bot-service/mattermost/actions/agents",
		StorageReady:  true,
	})

	result := svc.HandleMenuAction(context.Background(), MenuActionCommand{
		View:      menuViewOpenAI,
		Command:   "openai list",
		ChannelID: "channel-1",
		PostID:    "post-1",
	})

	if result.StatusCode != 200 {
		t.Fatalf("status = %d", result.StatusCode)
	}
	if !strings.Contains(result.EphemeralText, "`primary` status `authorized`") {
		t.Fatalf("ephemeral text = %q", result.EphemeralText)
	}
	if result.Card == nil || result.Card.Title != "Result - OpenAI accounts" {
		t.Fatalf("card = %#v", result.Card)
	}
	if !strings.Contains(result.Card.Text, "`primary` status `authorized`") {
		t.Fatalf("card text = %q", result.Card.Text)
	}
	if result.Card.ChannelID != "channel-1" || result.Card.PostID != "post-1" {
		t.Fatalf("card identity = %#v", result.Card)
	}
	assertCardDoesNotExposeSlashCommand(t, result.Card)
}

func TestMenuCommandActionKeepsOpenAIDeviceCodePrivate(t *testing.T) {
	store := &fakeAdminStore{
		openAIAccounts: map[string]entity.OpenAIAccount{
			"primary": {Name: "primary", Status: "awaiting_user", SecretRef: "matter-codex-codex-auth-primary"},
		},
	}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:         localizer,
		StatusService:     testStatusService(localizer),
		Store:             store,
		RuntimeRunner:     &fakeRuntimeRunner{},
		MenuActionURL:     "http://bot-service/mattermost/actions/agents",
		StorageReady:      true,
		RuntimeConfigured: true,
	})

	result := svc.HandleMenuAction(context.Background(), MenuActionCommand{
		View:    menuViewOpenAI,
		Command: "openai status primary",
	})

	if !strings.Contains(result.EphemeralText, "ABCD-12345") {
		t.Fatalf("ephemeral text = %q", result.EphemeralText)
	}
	if result.Card == nil || result.Card.Title != "Result - OpenAI accounts" {
		t.Fatalf("card = %#v", result.Card)
	}
	if strings.Contains(result.Card.Text, "ABCD-12345") || strings.Contains(result.Card.Text, "https://auth.openai.com") {
		t.Fatalf("card exposes device-code result: %q", result.Card.Text)
	}
	if !strings.Contains(result.Card.Text, "private Mattermost response") {
		t.Fatalf("card text = %q", result.Card.Text)
	}
	assertCardDoesNotExposeSlashCommand(t, result.Card)
}

func mattermostActionByID(actions []MattermostCardAction, id string) (MattermostCardAction, bool) {
	for _, action := range actions {
		if action.ID == id {
			return action, true
		}
	}
	return MattermostCardAction{}, false
}

func assertCardDoesNotExposeSlashCommand(t *testing.T, card *MattermostCard) {
	t.Helper()
	if card == nil {
		t.Fatal("card is nil")
	}
	if strings.Contains(card.Title, "/agents ") || strings.Contains(card.Text, "/agents ") {
		t.Fatalf("card exposes slash command: %#v", card)
	}
	for _, field := range card.Fields {
		if strings.Contains(field.Title, "/agents ") || strings.Contains(field.Value, "/agents ") {
			t.Fatalf("card field exposes slash command: %#v", field)
		}
	}
	for _, action := range card.Actions {
		if _, ok := action.Context["command"]; ok {
			t.Fatalf("card action exposes command context: %#v", action)
		}
	}
}

func TestRuntimeVariableDialogCreatesSecretAndMetadata(t *testing.T) {
	store := &fakeAdminStore{
		projects: map[int64]entity.Project{
			1: {ID: 1, Name: "Platform", Slug: "platform"},
		},
	}
	runner := &fakeRuntimeRunner{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:       localizer,
		StatusService:   testStatusService(localizer),
		Store:           store,
		RuntimeRunner:   runner,
		DialogSubmitURL: "http://bot-service/mattermost/dialogs/agents",
		StorageReady:    true,
	})

	result := svc.HandleDialogSubmission(context.Background(), DialogSubmissionCommand{
		CallbackID: dialogCallbackProjectRuntimeVar,
		State: encodeDialogState(MenuActionCommand{
			Resource: menuResourceProject,
			ID:       "1",
		}),
		Submission: map[string]any{
			dialogFieldProjectID:       "1",
			dialogFieldRuntimeVarName:  "radar_auto_kubeconfig",
			dialogFieldRuntimeVarValue: "secret-value",
			dialogFieldSensitive:       "true",
			dialogFieldEnabled:         "true",
			dialogFieldDescription:     "kubeconfig for radar-auto external cluster",
		},
	})

	if result.Error != "" || len(result.Errors) > 0 {
		t.Fatalf("dialog result error = %q errors = %#v", result.Error, result.Errors)
	}
	if runner.runtimeVariableSecretInput.ProjectSlug != "platform" {
		t.Fatalf("secret input = %#v", runner.runtimeVariableSecretInput)
	}
	if runner.runtimeVariableSecretInput.Variable.Name != "RADAR_AUTO_KUBECONFIG" {
		t.Fatalf("secret variable = %#v", runner.runtimeVariableSecretInput.Variable)
	}
	if runner.runtimeVariableSecretInput.Value != "secret-value" {
		t.Fatalf("secret value was not passed to runner")
	}
	variables, _ := store.ListProjectRuntimeVariables(context.Background(), 1)
	if len(variables) != 1 || variables[0].Name != "RADAR_AUTO_KUBECONFIG" || variables[0].SecretRef == "" {
		t.Fatalf("variables = %#v", variables)
	}
	if result.Card == nil || strings.Contains(result.Card.Text, "secret-value") {
		t.Fatalf("card exposes secret value or is nil: %#v", result.Card)
	}
}

func TestFrozenClusterAdminRuntimeVariableValueEditStopsBeforeSecretMutation(t *testing.T) {
	const secretName = "mc-var-platform-frozen-key"
	store := &fakeAdminStore{
		projects: map[int64]entity.Project{
			1: {ID: 1, Name: "Platform", Slug: "platform"},
		},
		agentRoles: map[int64]entity.AgentRole{
			1: {ID: 1, ProjectID: 1, Name: "configured-admin", RoleType: "admin", KubernetesAccess: "cluster-admin", Enabled: true},
		},
		runtimeVariables: map[int64]entity.ProjectRuntimeVariable{
			1: {ID: 1, ProjectID: 1, Name: "FROZEN_KEY", Slug: "frozen-key", SecretRef: secretName, SecretKey: "value", Sensitive: true, Enabled: true},
		},
		roleRuntimeVariables: map[string]entity.AgentRoleRuntimeVariableBinding{
			"1:1": {ID: 1, RoleID: 1, RoleName: "configured-admin", VariableID: 1, ProjectID: 1, Name: "FROZEN_KEY", Slug: "frozen-key", SecretRef: secretName, SecretKey: "value", Sensitive: true, Enabled: true},
		},
	}
	runner := &fakeRuntimeRunner{runtimeVariableSecrets: map[string]string{secretName: "synthetic-original-value"}}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer: localizer, StatusService: testStatusService(localizer), Store: store,
		RuntimeRunner: runner, StorageReady: true,
	})
	result := svc.HandleDialogSubmission(context.Background(), DialogSubmissionCommand{
		CallbackID: dialogCallbackProjectRuntimeVar,
		State:      encodeDialogState(MenuActionCommand{Resource: menuResourceRuntimeVar, ID: "1"}),
		Submission: map[string]any{
			dialogFieldProjectID:       "1",
			dialogFieldRuntimeVarName:  "FROZEN_KEY",
			dialogFieldRuntimeVarValue: "synthetic-replacement-value",
			dialogFieldSensitive:       "true",
			dialogFieldEnabled:         "true",
		},
	})
	if result.Errors[dialogFieldRuntimeVarValue] == "" {
		t.Fatalf("frozen runtime variable edit result = %#v", result)
	}
	if runner.runtimeVariableSecretInput.Variable.SecretName != "" || runner.runtimeVariableSecrets[secretName] != "synthetic-original-value" {
		t.Fatalf("frozen runtime variable changed Secret: input=%#v secrets=%#v", runner.runtimeVariableSecretInput, runner.runtimeVariableSecrets)
	}
}

func TestRoleRuntimeVariableAttachDialogFallsBackToCreateWhenProjectHasNoEnv(t *testing.T) {
	store := &fakeAdminStore{
		projects: map[int64]entity.Project{
			1: {ID: 1, Name: "Platform", Slug: "platform"},
		},
		agentRoles: map[int64]entity.AgentRole{
			1: {ID: 1, ProjectID: 1, Name: "sre", RoleType: "worker", Enabled: true},
		},
	}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:       localizer,
		StatusService:   testStatusService(localizer),
		Store:           store,
		DialogSubmitURL: "http://bot-service/mattermost/dialogs/agents",
		StorageReady:    true,
	})

	result := svc.HandleMenuAction(context.Background(), MenuActionCommand{
		View:     menuViewRoles,
		Dialog:   menuDialogRoleRuntimeVarAttach,
		Resource: menuResourceAgentRole,
		ID:       "1",
	})

	if result.Dialog == nil {
		t.Fatalf("dialog is nil, ephemeral = %q", result.EphemeralText)
	}
	if result.Dialog.CallbackID != dialogCallbackProjectRuntimeVar {
		t.Fatalf("callback id = %q", result.Dialog.CallbackID)
	}
	projectElement, ok := dialogElementByName(result.Dialog.Elements, dialogFieldProjectID)
	if !ok || projectElement.Default != "1" {
		t.Fatalf("project element = %#v", projectElement)
	}
	state, err := decodeDialogState(result.Dialog.State)
	if err != nil {
		t.Fatalf("decode dialog state: %v", err)
	}
	if state.ResourceType != menuResourceAgentRole || state.ResourceID != "1" {
		t.Fatalf("state = %#v", state)
	}
}

func TestRuntimeVariableDialogCreatesSecretAndAttachesToRoleFromRoleContext(t *testing.T) {
	store := &fakeAdminStore{
		projects: map[int64]entity.Project{
			1: {ID: 1, Name: "Platform", Slug: "platform"},
		},
		agentRoles: map[int64]entity.AgentRole{
			1: {ID: 1, ProjectID: 1, Name: "sre", RoleType: "worker", Enabled: true},
		},
	}
	runner := &fakeRuntimeRunner{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:       localizer,
		StatusService:   testStatusService(localizer),
		Store:           store,
		RuntimeRunner:   runner,
		DialogSubmitURL: "http://bot-service/mattermost/dialogs/agents",
		StorageReady:    true,
	})

	result := svc.HandleDialogSubmission(context.Background(), DialogSubmissionCommand{
		CallbackID: dialogCallbackProjectRuntimeVar,
		State: encodeDialogState(MenuActionCommand{
			Resource: menuResourceAgentRole,
			ID:       "1",
		}),
		Submission: map[string]any{
			dialogFieldProjectID:       "1",
			dialogFieldRuntimeVarName:  "RADAR_AUTO_KUBECONFIG",
			dialogFieldRuntimeVarValue: "secret-value",
			dialogFieldSensitive:       "true",
			dialogFieldEnabled:         "true",
			dialogFieldDescription:     "kubeconfig for radar-auto external cluster",
		},
	})

	if result.Error != "" || len(result.Errors) > 0 {
		t.Fatalf("dialog result error = %q errors = %#v", result.Error, result.Errors)
	}
	bindings, _ := store.ListAgentRoleRuntimeVariables(context.Background(), 1)
	if len(bindings) != 1 || bindings[0].Name != "RADAR_AUTO_KUBECONFIG" {
		t.Fatalf("bindings = %#v", bindings)
	}
	if result.Card == nil || !strings.Contains(result.Card.Text, "sre") || strings.Contains(result.Card.Text, "secret-value") {
		t.Fatalf("card is invalid or exposes secret: %#v", result.Card)
	}
}

func TestRoleRuntimeVariableAttachDialogCreatesBinding(t *testing.T) {
	store := &fakeAdminStore{
		projects: map[int64]entity.Project{
			1: {ID: 1, Name: "Platform", Slug: "platform"},
		},
		agentRoles: map[int64]entity.AgentRole{
			1: {ID: 1, ProjectID: 1, Name: "sre", RoleType: "worker", Enabled: true},
		},
		runtimeVariables: map[int64]entity.ProjectRuntimeVariable{
			1: {ID: 1, ProjectID: 1, Name: "RADAR_AUTO_KUBECONFIG", Slug: "radar-auto-kubeconfig", SecretRef: "mc-var-platform-radar-auto-kubeconfig", SecretKey: "value", Sensitive: true, Enabled: true},
		},
	}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:       localizer,
		StatusService:   testStatusService(localizer),
		Store:           store,
		DialogSubmitURL: "http://bot-service/mattermost/dialogs/agents",
		StorageReady:    true,
	})

	result := svc.HandleDialogSubmission(context.Background(), DialogSubmissionCommand{
		CallbackID: dialogCallbackRoleRuntimeVarAttach,
		State:      encodeDialogState(MenuActionCommand{Resource: menuResourceAgentRole, ID: "1"}),
		Submission: map[string]any{
			dialogFieldRole:         "1",
			dialogFieldRuntimeVarID: "1",
		},
	})

	if result.Error != "" || len(result.Errors) > 0 {
		t.Fatalf("dialog result error = %q errors = %#v", result.Error, result.Errors)
	}
	bindings, _ := store.ListAgentRoleRuntimeVariables(context.Background(), 1)
	if len(bindings) != 1 || bindings[0].Name != "RADAR_AUTO_KUBECONFIG" {
		t.Fatalf("bindings = %#v", bindings)
	}
}

func TestEnqueueAgentTurnPassesRoleRuntimeVariablesToSession(t *testing.T) {
	store := &fakeAdminStore{
		projects: map[int64]entity.Project{
			1: {ID: 1, Name: "Radar Auto", Slug: "radar-auto"},
		},
		agentRoles: map[int64]entity.AgentRole{
			1: {ID: 1, ProjectID: 1, Name: "sre", RoleType: "worker", OpenAIAccountName: "main", SandboxMode: "danger-full-access", Enabled: true},
		},
		chats: map[int64]entity.Chat{
			1: {ID: 1, ProjectID: 1, Name: "Deploy", ChatType: "single_custom", MattermostChannelID: "channel-1"},
		},
		openAIAccounts: map[string]entity.OpenAIAccount{
			"main": {Name: "main", SecretRef: "matter-codex-codex-auth-main", Status: "authorized"},
		},
		runtimeVariables: map[int64]entity.ProjectRuntimeVariable{
			1: {ID: 1, ProjectID: 1, Name: "RADAR_AUTO_KUBECONFIG", Slug: "radar-auto-kubeconfig", Description: "kubeconfig for radar-auto external cluster", SecretRef: "mc-var-radar-auto-kubeconfig", SecretKey: "value", Sensitive: true, Enabled: true},
		},
		roleRuntimeVariables: map[string]entity.AgentRoleRuntimeVariableBinding{
			"1:1": {ID: 1, RoleID: 1, RoleName: "sre", VariableID: 1, ProjectID: 1, Name: "RADAR_AUTO_KUBECONFIG", Slug: "radar-auto-kubeconfig", Description: "kubeconfig for radar-auto external cluster", SecretRef: "mc-var-radar-auto-kubeconfig", SecretKey: "value", Sensitive: true, Enabled: true},
		},
	}
	runner := &fakeRuntimeRunner{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewChatRunService(ChatRunServiceConfig{
		Localizer:     localizer,
		Store:         store,
		RuntimeRunner: runner,
		BotServiceURL: "http://bot-service",
		StorageReady:  true,
		RuntimeReady:  true,
	})

	queued, err := svc.EnqueueAgentTurn(context.Background(), AgentTurnRequest{
		Project:      store.projects[1],
		Chat:         store.chats[1],
		Role:         store.agentRoles[1],
		UserMessage:  "Check staging deployment access.",
		SourcePostID: "post-1",
		ReplyRootID:  "post-1",
		UserName:     "owner",
	})
	if err != nil {
		t.Fatalf("EnqueueAgentTurn() error = %v", err)
	}
	if queued.SessionKey == "" || len(runner.sessionRuns) != 1 {
		t.Fatalf("queued = %#v sessionRuns = %#v", queued, runner.sessionRuns)
	}
	env := runner.sessionRuns[0].RuntimeEnv
	if len(env) != 1 || env[0].Name != "RADAR_AUTO_KUBECONFIG" || env[0].SecretName != "mc-var-radar-auto-kubeconfig" {
		t.Fatalf("runtime env = %#v", env)
	}
	if len(store.sessionTurns) != 1 || !strings.Contains(store.sessionTurns[0].Message, "RADAR_AUTO_KUBECONFIG") {
		t.Fatalf("session turns = %#v", store.sessionTurns)
	}
	session := store.agentSessions[queued.SessionKey]
	if !strings.Contains(session.Capabilities, "RADAR_AUTO_KUBECONFIG") {
		t.Fatalf("capabilities = %q", session.Capabilities)
	}
}

func TestInvalidateIdleAgentSessionsForRolesDeletesOnlyIdlePods(t *testing.T) {
	now := time.Now().UTC()
	store := &fakeAdminStore{
		agentSessions: map[string]entity.AgentSession{
			"idle": {
				ID:                  1,
				SessionKey:          "idle",
				RoleID:              1,
				Status:              agentSessionStatusIdle,
				KubernetesNamespace: "mattermost",
				PodName:             "mc-session-idle",
				PVCName:             "mc-session-ws-idle",
				TokenSecretRef:      "matter-codex-session-idle",
				ExpiresAt:           now.Add(time.Hour),
			},
			"running": {
				ID:                  2,
				SessionKey:          "running",
				RoleID:              1,
				Status:              agentSessionStatusRunning,
				ActiveTurnID:        2,
				KubernetesNamespace: "mattermost",
				PodName:             "mc-session-running",
				PVCName:             "mc-session-ws-running",
				TokenSecretRef:      "matter-codex-session-running",
				ExpiresAt:           now.Add(time.Hour),
			},
			"queued": {
				ID:                  3,
				SessionKey:          "queued",
				RoleID:              1,
				Status:              agentSessionStatusIdle,
				KubernetesNamespace: "mattermost",
				PodName:             "mc-session-queued",
				PVCName:             "mc-session-ws-queued",
				TokenSecretRef:      "matter-codex-session-queued",
				ExpiresAt:           now.Add(time.Hour),
			},
		},
		sessionTurns: []entity.AgentSessionTurn{
			{ID: 3, SessionID: 3, RunID: "run-queued", Status: agentSessionTurnQueued},
		},
	}
	runner := &fakeRuntimeRunner{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:     localizer,
		StatusService: testStatusService(localizer),
		Store:         store,
		RuntimeRunner: runner,
		StorageReady:  true,
	})

	text := svc.invalidateIdleAgentSessionsForRolesText(context.Background(), []int64{1})

	if !strings.Contains(text, "invalidated idle pods `1`") || !strings.Contains(text, "skipped active/queued `2`") {
		t.Fatalf("summary = %q", text)
	}
	if len(runner.cleanedSessionKeys) != 1 || runner.cleanedSessionKeys[0] != "idle" {
		t.Fatalf("cleaned session keys = %#v", runner.cleanedSessionKeys)
	}
	if session := store.agentSessions["idle"]; session.PodName != "" || session.TokenSecretRef != "" || session.Status != agentSessionStatusIdle {
		t.Fatalf("idle session = %#v", session)
	}
	if store.agentSessions["running"].PodName == "" || store.agentSessions["queued"].PodName == "" {
		t.Fatalf("active sessions were invalidated: %#v", store.agentSessions)
	}
}

func TestSlashRepoAddCreatesChannelAndStoresRepository(t *testing.T) {
	store := &fakeAdminStore{}
	channels := &fakeChannelManager{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:             localizer,
		StatusService:         testStatusService(localizer),
		Store:                 store,
		ChannelManager:        channels,
		DefaultTeamName:       "agents",
		BotTokenConfigured:    true,
		SlashTokenConfigured:  true,
		DatabaseConfigured:    true,
		StorageReady:          true,
		ChannelManagerEnabled: true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "repo add github codex-k8s/matter-codex main", UserName: "owner"})

	if !strings.Contains(text, "github:codex-k8s/matter-codex") {
		t.Fatalf("Handle(repo add) text = %q", text)
	}
	if store.upsert.Provider != "github" || store.upsert.Owner != "codex-k8s" || store.upsert.Name != "matter-codex" {
		t.Fatalf("unexpected upsert input: %#v", store.upsert)
	}
	if channels.channelName != "repo-codex-k8s-matter-codex" {
		t.Fatalf("channelName = %q", channels.channelName)
	}
	if !store.auditRecorded {
		t.Fatal("audit event was not recorded")
	}
}

func TestSlashRepoAddEnsuresGitHubWebhook(t *testing.T) {
	store := &fakeAdminStore{}
	provider := &fakeRepositoryProvider{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:               localizer,
		StatusService:           testStatusService(localizer),
		Store:                   store,
		RepositoryProvider:      provider,
		GitHubTokenConfigured:   true,
		GitHubWebhookConfigured: true,
		StorageReady:            true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "repo add github codex-k8s/matter-codex main", UserName: "owner"})

	if !strings.Contains(text, "webhook: `created` id `99` active `true`") {
		t.Fatalf("Handle(repo add) text = %q", text)
	}
	if provider.webhookOwner != "codex-k8s" || provider.webhookName != "matter-codex" {
		t.Fatalf("webhook target = %s/%s", provider.webhookOwner, provider.webhookName)
	}
}

func TestSlashGitHubCheckUsesProvider(t *testing.T) {
	store := &fakeAdminStore{}
	provider := &fakeRepositoryProvider{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:             localizer,
		StatusService:         testStatusService(localizer),
		Store:                 store,
		RepositoryProvider:    provider,
		GitHubTokenConfigured: true,
		StorageReady:          true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "github check codex-k8s/matter-codex", UserName: "owner"})

	if !strings.Contains(text, "matter-codex GitHub repo access") || !strings.Contains(text, "github:codex-k8s/matter-codex") {
		t.Fatalf("Handle(github check) text = %q", text)
	}
	if provider.checkedOwner != "codex-k8s" || provider.checkedName != "matter-codex" {
		t.Fatalf("unexpected provider call: %#v", provider)
	}
	if !store.auditRecorded {
		t.Fatal("audit event was not recorded")
	}
}

func TestSlashGitHubBranchDryRun(t *testing.T) {
	provider := &fakeRepositoryProvider{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:             localizer,
		StatusService:         testStatusService(localizer),
		RepositoryProvider:    provider,
		GitHubTokenConfigured: true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "github branch dry-run codex-k8s/matter-codex smoke-branch main"})

	if !strings.Contains(text, "GitHub branch dry-run") || !strings.Contains(text, "changes: none") {
		t.Fatalf("Handle(github branch dry-run) text = %q", text)
	}
	if provider.resolvedBranch != "main" {
		t.Fatalf("resolvedBranch = %q", provider.resolvedBranch)
	}
}

func TestSlashGitHubPRStatus(t *testing.T) {
	provider := &fakeRepositoryProvider{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:             localizer,
		StatusService:         testStatusService(localizer),
		RepositoryProvider:    provider,
		GitHubTokenConfigured: true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "github pr status codex-k8s/matter-codex 4"})

	if !strings.Contains(text, "GitHub PR status") || !strings.Contains(text, "`#4`") || !strings.Contains(text, "review: `APPROVED`") {
		t.Fatalf("Handle(github pr status) text = %q", text)
	}
	if provider.prNumber != 4 {
		t.Fatalf("prNumber = %d", provider.prNumber)
	}
}

func TestSlashGitHubWebhookEnsure(t *testing.T) {
	provider := &fakeRepositoryProvider{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:               localizer,
		StatusService:           testStatusService(localizer),
		RepositoryProvider:      provider,
		GitHubTokenConfigured:   true,
		GitHubWebhookConfigured: true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "github webhook ensure codex-k8s/matter-codex"})

	if !strings.Contains(text, "matter-codex GitHub webhook") || !strings.Contains(text, "webhook: `created`") {
		t.Fatalf("Handle(github webhook ensure) text = %q", text)
	}
	if provider.webhookOwner != "codex-k8s" || provider.webhookName != "matter-codex" {
		t.Fatalf("webhook target = %s/%s", provider.webhookOwner, provider.webhookName)
	}
}

func TestSlashGitHubAccountListUsesStorage(t *testing.T) {
	store := &fakeAdminStore{
		githubAccounts: map[string]entity.GitHubAccount{
			"agent":   {Name: "agent", SecretRef: "matter-codex-github-agent", Status: "configured", Username: "agent-bot", Email: "agent@example.invalid"},
			"primary": {Name: "primary", SecretRef: "matter-codex-github", Status: "configured", Username: "owner", Email: "owner@example.invalid"},
		},
	}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:     localizer,
		StatusService: testStatusService(localizer),
		Store:         store,
		StorageReady:  true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "github account list"})

	if !strings.Contains(text, "matter-codex GitHub accounts:") || !strings.Contains(text, "`agent` status `configured`") || !strings.Contains(text, "`primary` status `configured`") {
		t.Fatalf("Handle(github account list) text = %q", text)
	}
}

func TestSlashProfileList(t *testing.T) {
	store := &fakeAdminStore{
		profiles: []entity.AgentProfile{{Name: "developer", Role: "developer", Description: "dev", Enabled: true, OpenAIAccountName: "primary", GitHubAccountName: "agent"}},
	}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:     localizer,
		StatusService: testStatusService(localizer),
		Store:         store,
		StorageReady:  true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "profile list"})

	if !strings.Contains(text, "`developer` role `developer` openai `primary` github `agent` enabled") {
		t.Fatalf("Handle(profile list) text = %q", text)
	}
}

func TestSlashOpenAIAuthStatusAndList(t *testing.T) {
	store := &fakeAdminStore{}
	runner := &fakeRuntimeRunner{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:           localizer,
		StatusService:       testStatusService(localizer),
		Store:               store,
		RuntimeRunner:       runner,
		CodexAuthSecretName: "matter-codex-codex-auth",
		StorageReady:        true,
		RuntimeConfigured:   true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "openai auth primary", UserName: "owner"})
	if !strings.Contains(text, "OpenAI account created") || !strings.Contains(text, "`primary`") {
		t.Fatalf("Handle(openai auth) text = %q", text)
	}
	if runner.authAccount != "primary" || runner.authSecret != "matter-codex-codex-auth-primary" {
		t.Fatalf("auth session = %q %q", runner.authAccount, runner.authSecret)
	}

	text = svc.Handle(context.Background(), SlashCommand{Text: "openai status primary", UserName: "owner"})
	if !strings.Contains(text, "https://auth.openai.com/codex/device") || !strings.Contains(text, "`ABCD-12345`") {
		t.Fatalf("Handle(openai status) text = %q", text)
	}

	runner.authReady = true
	text = svc.Handle(context.Background(), SlashCommand{Text: "openai status primary", UserName: "owner"})
	if !strings.Contains(text, "OpenAI account authorized") || !strings.Contains(text, "matter-codex-codex-auth-primary") {
		t.Fatalf("Handle(openai status ready) text = %q", text)
	}
	account, ok := store.openAIAccounts["primary"]
	if !ok || account.Status != "authorized" {
		t.Fatalf("account = %#v ok=%v", account, ok)
	}

	text = svc.Handle(context.Background(), SlashCommand{Text: "openai list"})
	if !strings.Contains(text, "`primary` status `authorized`") {
		t.Fatalf("Handle(openai list) text = %q", text)
	}
}

func TestFrozenClusterAdminAccountsDenyProviderAndSecretSideEffects(t *testing.T) {
	store := &fakeAdminStore{
		frozenOpenAIAccount: "primary",
		openAIAccounts: map[string]entity.OpenAIAccount{
			"primary": {Name: "primary", SecretRef: "matter-codex-codex-auth-primary", Status: "authorized"},
		},
		frozenGitHubAccount: "agent",
		githubAccounts: map[string]entity.GitHubAccount{
			"agent": {Name: "agent", SecretRef: "matter-codex-github-agent", Status: "configured"},
		},
	}
	runner := &fakeRuntimeRunner{}
	inspector := &fakeGitHubAccountInspector{inspection: providerrepo.GitHubTokenInspection{Username: "mutated"}}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer: localizer, StatusService: testStatusService(localizer), Store: store,
		RuntimeRunner: runner, GitHubAccountInspector: inspector, StorageReady: true, RuntimeConfigured: true,
		CodexAuthSecretName: "matter-codex-codex-auth", GitHubSecretName: "matter-codex-github",
	})
	for _, command := range []string{
		"openai auth primary", "openai status primary", "openai cleanup primary", "openai delete primary",
	} {
		text := svc.Handle(context.Background(), SlashCommand{Text: command, UserName: "owner"})
		if !strings.Contains(text, "cluster-admin") {
			t.Fatalf("%s result = %q", command, text)
		}
	}
	if runner.authAccount != "" || runner.authStatusChecks != 0 || runner.authCompleteCalls != 0 || runner.authCleanupCalls != 0 || runner.deletedAuthAccount != "" {
		t.Fatalf("frozen OpenAI side effects: %#v", runner)
	}
	result := svc.HandleDialogSubmission(context.Background(), DialogSubmissionCommand{
		CallbackID: dialogCallbackGitHubAccountEdit,
		State:      encodeDialogState(MenuActionCommand{View: menuViewGitHub, Resource: menuResourceGitHubAccount, ID: "agent"}),
		UserID:     "owner-id", UserName: "owner",
		Submission: map[string]any{dialogFieldAccount: "agent", dialogFieldToken: "synthetic-mutated-token"},
	})
	if result.Error == "" || inspector.token != "" || runner.githubSecretInput.SecretName != "" {
		t.Fatalf("frozen GitHub result=%#v inspector=%q secret=%#v", result, inspector.token, runner.githubSecretInput)
	}
}

func TestSlashPromptSetRenderShowAndList(t *testing.T) {
	store := &fakeAdminStore{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:     localizer,
		StatusService: testStatusService(localizer),
		Store:         store,
		StorageReady:  true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "prompt help reviewer review_pr"})
	if !strings.Contains(text, "{{.Repository.FullName}}") || !strings.Contains(text, "{{now}}") {
		t.Fatalf("Handle(prompt help) text = %q", text)
	}

	templateBody := "DECISION: comment\nRepo: {{.Repository.FullName}}\nRun: {{.Run.ID}}"
	text = svc.Handle(context.Background(), SlashCommand{Text: "prompt set reviewer review_pr " + templateBody, UserName: "owner"})
	if !strings.Contains(text, "prompt template updated") || !strings.Contains(text, "codex-k8s/matter-codex") {
		t.Fatalf("Handle(prompt set) text = %q", text)
	}

	text = svc.Handle(context.Background(), SlashCommand{Text: "prompt show reviewer review_pr"})
	if !strings.Contains(text, templateBody) {
		t.Fatalf("Handle(prompt show) text = %q", text)
	}

	text = svc.Handle(context.Background(), SlashCommand{Text: "prompt render reviewer review_pr"})
	if !strings.Contains(text, "prompt template render OK") || !strings.Contains(text, "Repo: codex-k8s/matter-codex") {
		t.Fatalf("Handle(prompt render) text = %q", text)
	}

	text = svc.Handle(context.Background(), SlashCommand{Text: "prompt list reviewer"})
	if !strings.Contains(text, "`reviewer/review_pr`") {
		t.Fatalf("Handle(prompt list) text = %q", text)
	}
}

func TestSlashPromptHelpUsesLocale(t *testing.T) {
	localizer := testLocalizer(t, texti18n.RussianLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:     localizer,
		StatusService: testStatusService(localizer),
		Store:         &fakeAdminStore{},
		StorageReady:  true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "prompt help reviewer review_pr"})
	if !strings.Contains(text, "Доступные placeholders") || !strings.Contains(text, "{{.Repository.FullName}}") || !strings.Contains(text, "{{now}}") {
		t.Fatalf("Handle(prompt help) text = %q", text)
	}
}

func TestSlashPromptRenderUsesSelectedLocale(t *testing.T) {
	store := &fakeAdminStore{
		promptTemplates: map[string]entity.AgentPromptTemplate{
			promptTemplateMapKey("reviewer", reviewPRTemplateKey): {
				ID:          1,
				ProfileName: "reviewer",
				TemplateKey: reviewPRTemplateKey,
				Body:        "Language: {{.Locale.Language}}\nLocale: {{.Locale.Code}}",
			},
		},
	}
	localizer := testLocalizer(t, texti18n.RussianLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:     localizer,
		StatusService: testStatusService(localizer),
		Store:         store,
		StorageReady:  true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "prompt render reviewer review_pr"})
	if !strings.Contains(text, "Language: Russian") || !strings.Contains(text, "Locale: ru") {
		t.Fatalf("Handle(prompt render ru) text = %q", text)
	}

	if _, err := localizer.SetLocale(texti18n.DefaultLocale); err != nil {
		t.Fatalf("SetLocale(en) error = %v", err)
	}
	text = svc.Handle(context.Background(), SlashCommand{Text: "prompt render reviewer review_pr"})
	if !strings.Contains(text, "Language: English") || !strings.Contains(text, "Locale: en") {
		t.Fatalf("Handle(prompt render en) text = %q", text)
	}
}

func TestSlashLocaleSetChangesResponses(t *testing.T) {
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:             localizer,
		StatusService:         testStatusService(localizer),
		BotTokenConfigured:    true,
		SlashTokenConfigured:  true,
		DatabaseConfigured:    true,
		StorageReady:          true,
		ChannelManagerEnabled: true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "locale set ru"})
	if !strings.Contains(text, "локаль изменена") {
		t.Fatalf("Handle(locale set ru) text = %q", text)
	}
	text = svc.Handle(context.Background(), SlashCommand{Text: "token check"})
	if !strings.Contains(text, "mattermost bot token: настроен") || !strings.Contains(text, "storage: готов") {
		t.Fatalf("Handle(token check) text = %q", text)
	}
	text = svc.Handle(context.Background(), SlashCommand{Text: "locale set en"})
	if !strings.Contains(text, "locale changed to `en`") {
		t.Fatalf("Handle(locale set en) text = %q", text)
	}
}

func TestSlashRuntimeSmokeStatusAndCleanup(t *testing.T) {
	runner := &fakeRuntimeRunner{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:         localizer,
		StatusService:     testStatusService(localizer),
		RuntimeRunner:     runner,
		RuntimeConfigured: true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "runtime smoke smoke-test"})
	if !strings.Contains(text, "Kubernetes smoke runner started") || !strings.Contains(text, "`smoke-test`") {
		t.Fatalf("Handle(runtime smoke) text = %q", text)
	}
	if runner.startedRunID != "smoke-test" {
		t.Fatalf("startedRunID = %q", runner.startedRunID)
	}

	text = svc.Handle(context.Background(), SlashCommand{Text: "runtime status smoke-test"})
	if !strings.Contains(text, "Kubernetes run status") || !strings.Contains(text, "phase `Succeeded`") || !strings.Contains(text, "smoke-ok") {
		t.Fatalf("Handle(runtime status) text = %q", text)
	}

	text = svc.Handle(context.Background(), SlashCommand{Text: "runtime cleanup smoke-test"})
	if !strings.Contains(text, "Kubernetes run cleanup") || !strings.Contains(text, "job deleted: `true`") {
		t.Fatalf("Handle(runtime cleanup) text = %q", text)
	}
	if runner.cleanedRunID != "smoke-test" {
		t.Fatalf("cleanedRunID = %q", runner.cleanedRunID)
	}
}

func TestSlashRuntimeRejectsInvalidRunID(t *testing.T) {
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:         localizer,
		StatusService:     testStatusService(localizer),
		RuntimeRunner:     &fakeRuntimeRunner{},
		RuntimeConfigured: true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "runtime smoke Bad_ID"})
	if !strings.Contains(text, "run id must be lowercase") {
		t.Fatalf("Handle(runtime smoke Bad_ID) text = %q", text)
	}
}

func TestSlashRuntimePruneDefaultsToDryRun(t *testing.T) {
	runner := &fakeRuntimeRunner{
		retentionResult: runtimerepo.RetentionCleanupResult{
			Namespace:         "mattermost",
			DryRun:            true,
			OlderThan:         2 * time.Hour,
			RunsMatched:       1,
			JobsMatched:       1,
			PVCsMatched:       1,
			ConfigMapsMatched: 1,
			MatchedRunIDs:     []string{"old-run"},
		},
	}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:         localizer,
		StatusService:     testStatusService(localizer),
		Store:             &fakeAdminStore{},
		RuntimeRunner:     runner,
		StorageReady:      true,
		RuntimeConfigured: true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "runtime prune 2h", UserName: "owner"})

	if !strings.Contains(text, "runtime retention cleanup") || !strings.Contains(text, "mode: `dry-run`") || !strings.Contains(text, "old-run") {
		t.Fatalf("Handle(runtime prune) text = %q", text)
	}
	if runner.retentionInput.OlderThan != 2*time.Hour || !runner.retentionInput.DryRun {
		t.Fatalf("retentionInput = %#v", runner.retentionInput)
	}
}

func TestSlashRuntimePruneApply(t *testing.T) {
	runner := &fakeRuntimeRunner{
		retentionResult: runtimerepo.RetentionCleanupResult{
			Namespace:         "mattermost",
			DryRun:            false,
			OlderThan:         time.Hour,
			RunsMatched:       1,
			JobsMatched:       1,
			JobsDeleted:       1,
			PVCsMatched:       1,
			PVCsDeleted:       1,
			ConfigMapsMatched: 1,
			ConfigMapsDeleted: 1,
			MatchedRunIDs:     []string{"old-run"},
		},
	}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:         localizer,
		StatusService:     testStatusService(localizer),
		Store:             &fakeAdminStore{},
		RuntimeRunner:     runner,
		StorageReady:      true,
		RuntimeConfigured: true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "runtime prune 1h --apply", UserName: "owner"})

	if !strings.Contains(text, "mode: `apply`") || !strings.Contains(text, "jobs matched/deleted: `1`/`1`") {
		t.Fatalf("Handle(runtime prune apply) text = %q", text)
	}
	if runner.retentionInput.OlderThan != time.Hour || runner.retentionInput.DryRun {
		t.Fatalf("retentionInput = %#v", runner.retentionInput)
	}
}

func TestSlashReviewPRStatusAndCleanup(t *testing.T) {
	store := &fakeAdminStore{
		profiles: []entity.AgentProfile{{Name: "reviewer", Role: "reviewer", Enabled: true, OpenAIAccountName: "primary", GitHubAccountName: "primary"}},
		openAIAccounts: map[string]entity.OpenAIAccount{
			"primary": {Name: "primary", Status: "authorized", SecretRef: "matter-codex-codex-auth-primary"},
		},
	}
	runner := &fakeRuntimeRunner{}
	localizer := testLocalizer(t, texti18n.DefaultLocale)
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:             localizer,
		StatusService:         testStatusService(localizer),
		Store:                 store,
		RuntimeRunner:         runner,
		GitHubTokenConfigured: true,
		StorageReady:          true,
		RuntimeConfigured:     true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "review pr codex-k8s/matter-codex 12 review-test", UserName: "owner"})
	if !strings.Contains(text, "reviewer run started") || !strings.Contains(text, "`review-test`") || !strings.Contains(text, "`#12`") {
		t.Fatalf("Handle(review pr) text = %q", text)
	}
	if runner.startedReviewRunID != "review-test" || runner.reviewPRNumber != 12 {
		t.Fatalf("runner = %#v", runner)
	}
	if runner.reviewCodexSecret != "matter-codex-codex-auth-primary" {
		t.Fatalf("reviewCodexSecret = %q", runner.reviewCodexSecret)
	}
	if runner.reviewGitHubSecret != "matter-codex-github" {
		t.Fatalf("reviewGitHubSecret = %q", runner.reviewGitHubSecret)
	}
	if store.agentRun.RunID != "review-test" || store.agentRun.ProfileName != "reviewer" || store.agentRun.HeadBranch != "pr-12" {
		t.Fatalf("agentRun = %#v", store.agentRun)
	}

	text = svc.Handle(context.Background(), SlashCommand{Text: "review status review-test"})
	if !strings.Contains(text, "reviewer run status") || !strings.Contains(text, "review-decision") || !strings.Contains(text, "comment") {
		t.Fatalf("Handle(review status) text = %q", text)
	}
	if store.updatedRunStatus != "review_comment" || store.updatedPRURL != "https://github.example/pr/12" {
		t.Fatalf("updated run = %q %q", store.updatedRunStatus, store.updatedPRURL)
	}

	text = svc.Handle(context.Background(), SlashCommand{Text: "review cleanup review-test", UserName: "owner"})
	if !strings.Contains(text, "reviewer run cleanup") || !strings.Contains(text, "pvc deleted: `true`") {
		t.Fatalf("Handle(review cleanup) text = %q", text)
	}
}

func fakeSucceededStatus(runID string, artifacts map[string]string) runtimerepo.RunStatus {
	return runtimerepo.RunStatus{
		RunID:        runID,
		Namespace:    "mattermost",
		JobName:      "mc-run-" + runID,
		PVCName:      "mc-ws-" + runID,
		PodName:      "mc-run-" + runID + "-pod",
		Exists:       true,
		JobSucceeded: 1,
		PodPhase:     "Succeeded",
		Artifacts:    artifacts,
	}
}

func testLocalizer(t *testing.T, locale string) *texti18n.Localizer {
	t.Helper()
	localizer, err := texti18n.New(locale)
	if err != nil {
		t.Fatalf("New localizer error = %v", err)
	}
	return localizer
}

func testStatusService(localizer *texti18n.Localizer) *StatusService {
	return NewStatusService(Config{
		Localizer:            localizer,
		ServiceName:          "matter-codex-bot-service",
		ServiceVersion:       "0.1.0",
		MattermostConfigured: true,
		BotTokenConfigured:   true,
		SlashTokenConfigured: true,
		DatabaseConfigured:   true,
		StorageReady:         true,
		RuntimeConfigured:    true,
		DefaultTeamName:      "agents",
		DefaultChannels:      []string{"agents-control"},
	})
}

type fakeGitHubAccountInspector struct {
	token      string
	inspection providerrepo.GitHubTokenInspection
	err        error
}

func (inspector *fakeGitHubAccountInspector) InspectToken(_ context.Context, token string) (providerrepo.GitHubTokenInspection, error) {
	inspector.token = token
	if inspector.err != nil {
		return providerrepo.GitHubTokenInspection{}, inspector.err
	}
	return inspector.inspection, nil
}

type fakeRuntimeRunner struct {
	startedRunID                string
	startedDeveloperRunID       string
	developerRuns               []runtimerepo.DeveloperRunInput
	developerBaseBranch         string
	developerHeadBranch         string
	developerCodexSecret        string
	developerGitHubSecret       string
	startedReviewRunID          string
	reviewRuns                  []runtimerepo.ReviewRunInput
	reviewPRNumber              int
	reviewCodexSecret           string
	reviewGitHubSecret          string
	startedChatRunID            string
	chatRuns                    []runtimerepo.ChatRunInput
	chatCodexSecret             string
	chatGitHubSecret            string
	startedSessionKey           string
	sessionRuns                 []runtimerepo.AgentSessionPodInput
	sessionCodexSecret          string
	sessionGitHubSecret         string
	cleanedSessionKey           string
	cleanedSessionKeys          []string
	sessionRuntimeHealth        runtimerepo.AgentSessionRuntimeHealth
	sessionRuntimeHealthCalls   int
	sessionStartErrors          []error
	sessionCleanupErrors        []error
	botTokenSecrets             map[string]string
	botTokenSecretReads         int
	cleanedRunID                string
	cleanedRunIDs               []string
	retentionInput              runtimerepo.RetentionCleanupInput
	retentionResult             runtimerepo.RetentionCleanupResult
	authAccount                 string
	authSecret                  string
	authReady                   bool
	authSecretNotReady          bool
	authStatusWithoutDeviceCode bool
	authSecretChecks            int
	authStatusChecks            int
	authCompleteCalls           int
	authCleanupCalls            int
	authSecretCheckErr          error
	deletedAuthAccount          string
	deletedAuthSecret           string
	githubSecretInput           runtimerepo.GitHubTokenSecretInput
	deletedGitHubSecretAccount  string
	deletedGitHubSecretName     string
	runtimeVariableSecrets      map[string]string
	runtimeVariableSecretInput  runtimerepo.ProjectRuntimeVariableSecretInput
	deletedRuntimeVariable      string
	secretIntegrity             map[string]runtimerepo.SecretIntegrity
	secretIntegrityErr          error
	secretIntegrityReads        int
	runStatuses                 map[string]runtimerepo.RunStatus
}

func (runner *fakeRuntimeRunner) InspectSecretIntegrity(_ context.Context, input runtimerepo.SecretIntegrityInput) (runtimerepo.SecretIntegrity, error) {
	runner.secretIntegrityReads++
	if runner.secretIntegrityErr != nil {
		return runtimerepo.SecretIntegrity{}, runner.secretIntegrityErr
	}
	if integrity, ok := runner.secretIntegrity[input.SecretName+"/"+input.SecretKey]; ok {
		return integrity, nil
	}
	return runtimerepo.SecretIntegrity{
		SecretName: input.SecretName, SecretKey: input.SecretKey,
		ContentSHA256: "synthetic-sha256", UID: "synthetic-uid", ResourceVersion: "1",
	}, nil
}

func (runner *fakeRuntimeRunner) StartSmokeRun(_ context.Context, input runtimerepo.SmokeRunInput) (runtimerepo.StartedRun, error) {
	runner.startedRunID = input.RunID
	return runtimerepo.StartedRun{
		RunID:     input.RunID,
		Namespace: "mattermost",
		JobName:   "mc-run-" + input.RunID,
		PVCName:   "mc-ws-" + input.RunID,
		Created:   true,
	}, nil
}

func (runner *fakeRuntimeRunner) StartCodexAuthSession(_ context.Context, input runtimerepo.CodexAuthSessionInput) (runtimerepo.CodexAuthSession, error) {
	runner.authAccount = input.AccountName
	runner.authSecret = input.SecretName
	return runtimerepo.CodexAuthSession{
		AccountName: input.AccountName,
		SecretName:  input.SecretName,
		Namespace:   "mattermost",
		JobName:     "mc-codex-auth-" + input.AccountName,
		Created:     true,
	}, nil
}

func (runner *fakeRuntimeRunner) GetCodexAuthStatus(_ context.Context, accountName string, secretName string) (runtimerepo.CodexAuthStatus, error) {
	runner.authStatusChecks++
	status := runtimerepo.CodexAuthStatus{
		AccountName: accountName,
		SecretName:  secretName,
		Namespace:   "mattermost",
		JobName:     "mc-codex-auth-" + accountName,
		PodName:     "mc-codex-auth-" + accountName + "-pod",
		Exists:      true,
		JobActive:   1,
		PodPhase:    "Running",
		DeviceURL:   "https://auth.openai.com/codex/device",
		DeviceCode:  "ABCD-12345",
		AuthReady:   runner.authReady,
	}
	if runner.authStatusWithoutDeviceCode {
		status.DeviceURL = ""
		status.DeviceCode = ""
	}
	return status, nil
}

func (runner *fakeRuntimeRunner) CheckCodexAuthSecret(_ context.Context, input runtimerepo.CodexAuthSecretCheckInput) (runtimerepo.CodexAuthSecretCheckResult, error) {
	runner.authSecretChecks++
	if runner.authSecretCheckErr != nil {
		return runtimerepo.CodexAuthSecretCheckResult{}, runner.authSecretCheckErr
	}
	return runtimerepo.CodexAuthSecretCheckResult{
		AccountName: input.AccountName,
		SecretName:  input.SecretName,
		Namespace:   "mattermost",
		JobName:     "mc-codex-auth-check-" + input.AccountName,
		PodName:     "mc-codex-auth-check-" + input.AccountName + "-pod",
		Ready:       !runner.authSecretNotReady,
	}, nil
}

func (runner *fakeRuntimeRunner) CompleteCodexAuthSession(_ context.Context, input runtimerepo.CodexAuthCompleteInput) (runtimerepo.CodexAuthCompleteResult, error) {
	runner.authCompleteCalls++
	return runtimerepo.CodexAuthCompleteResult{
		AccountName: input.AccountName,
		SecretName:  input.SecretName,
		Namespace:   "mattermost",
		Saved:       true,
	}, nil
}

func (runner *fakeRuntimeRunner) CleanupCodexAuthSession(_ context.Context, accountName string) (runtimerepo.CodexAuthCleanupResult, error) {
	runner.authCleanupCalls++
	return runtimerepo.CodexAuthCleanupResult{
		AccountName: accountName,
		Namespace:   "mattermost",
		JobDeleted:  true,
	}, nil
}

func (runner *fakeRuntimeRunner) DeleteCodexAuthAccount(_ context.Context, accountName string, secretName string) (runtimerepo.CodexAuthAccountDeleteResult, error) {
	runner.deletedAuthAccount = accountName
	runner.deletedAuthSecret = secretName
	return runtimerepo.CodexAuthAccountDeleteResult{
		AccountName:   accountName,
		SecretName:    secretName,
		Namespace:     "mattermost",
		JobDeleted:    true,
		SecretDeleted: true,
	}, nil
}

func (runner *fakeRuntimeRunner) UpsertGitHubTokenSecret(_ context.Context, input runtimerepo.GitHubTokenSecretInput) (runtimerepo.GitHubTokenSecret, error) {
	runner.githubSecretInput = input
	return runtimerepo.GitHubTokenSecret{
		AccountName: input.AccountName,
		SecretName:  input.SecretName,
		Namespace:   "mattermost",
		Created:     true,
	}, nil
}

func (runner *fakeRuntimeRunner) DeleteGitHubTokenSecret(_ context.Context, accountName string, secretName string) (runtimerepo.GitHubTokenSecretDeleteResult, error) {
	runner.deletedGitHubSecretAccount = accountName
	runner.deletedGitHubSecretName = secretName
	return runtimerepo.GitHubTokenSecretDeleteResult{
		AccountName:   accountName,
		SecretName:    secretName,
		Namespace:     "mattermost",
		SecretDeleted: true,
	}, nil
}

func (runner *fakeRuntimeRunner) UpsertProjectRuntimeVariableSecret(_ context.Context, input runtimerepo.ProjectRuntimeVariableSecretInput) (runtimerepo.ProjectRuntimeVariableSecret, error) {
	runner.runtimeVariableSecretInput = input
	if runner.runtimeVariableSecrets == nil {
		runner.runtimeVariableSecrets = map[string]string{}
	}
	runner.runtimeVariableSecrets[input.Variable.SecretName] = input.Value
	return runtimerepo.ProjectRuntimeVariableSecret{
		SecretName: input.Variable.SecretName,
		Namespace:  "mattermost",
		Created:    true,
	}, nil
}

func (runner *fakeRuntimeRunner) DeleteProjectRuntimeVariableSecret(_ context.Context, secretName string) (runtimerepo.ProjectRuntimeVariableSecret, error) {
	runner.deletedRuntimeVariable = secretName
	if runner.runtimeVariableSecrets != nil {
		delete(runner.runtimeVariableSecrets, secretName)
	}
	return runtimerepo.ProjectRuntimeVariableSecret{
		SecretName: secretName,
		Namespace:  "mattermost",
		Created:    true,
	}, nil
}

func (runner *fakeRuntimeRunner) StartDeveloperRun(_ context.Context, input runtimerepo.DeveloperRunInput) (runtimerepo.StartedRun, error) {
	runner.startedDeveloperRunID = input.RunID
	runner.developerRuns = append(runner.developerRuns, input)
	runner.developerBaseBranch = input.BaseBranch
	runner.developerHeadBranch = input.HeadBranch
	runner.developerCodexSecret = input.CodexAuthSecretName
	runner.developerGitHubSecret = input.GitHubSecretName
	if strings.TrimSpace(input.Prompt) == "" {
		return runtimerepo.StartedRun{}, fmt.Errorf("prompt is required")
	}
	return runtimerepo.StartedRun{
		RunID:     input.RunID,
		Namespace: "mattermost",
		JobName:   "mc-run-" + input.RunID,
		PVCName:   "mc-ws-" + input.RunID,
		Created:   true,
	}, nil
}

func (runner *fakeRuntimeRunner) StartReviewRun(_ context.Context, input runtimerepo.ReviewRunInput) (runtimerepo.StartedRun, error) {
	runner.startedReviewRunID = input.RunID
	runner.reviewRuns = append(runner.reviewRuns, input)
	runner.reviewPRNumber = input.PRNumber
	runner.reviewCodexSecret = input.CodexAuthSecretName
	runner.reviewGitHubSecret = input.GitHubSecretName
	if strings.TrimSpace(input.Prompt) == "" {
		return runtimerepo.StartedRun{}, fmt.Errorf("prompt is required")
	}
	return runtimerepo.StartedRun{
		RunID:     input.RunID,
		Namespace: "mattermost",
		JobName:   "mc-run-" + input.RunID,
		PVCName:   "mc-ws-" + input.RunID,
		Created:   true,
	}, nil
}

func (runner *fakeRuntimeRunner) StartChatRun(_ context.Context, input runtimerepo.ChatRunInput) (runtimerepo.StartedRun, error) {
	runner.startedChatRunID = input.RunID
	runner.chatRuns = append(runner.chatRuns, input)
	runner.chatCodexSecret = input.CodexAuthSecretName
	runner.chatGitHubSecret = input.GitHubSecretName
	if strings.TrimSpace(input.Prompt) == "" {
		return runtimerepo.StartedRun{}, fmt.Errorf("prompt is required")
	}
	return runtimerepo.StartedRun{
		RunID:     input.RunID,
		Namespace: "mattermost",
		JobName:   "mc-run-" + input.RunID,
		PVCName:   "mc-ws-" + input.RunID,
		Created:   true,
	}, nil
}

func (runner *fakeRuntimeRunner) StartAgentSession(_ context.Context, input runtimerepo.AgentSessionPodInput) (runtimerepo.StartedAgentSession, error) {
	runner.startedSessionKey = input.SessionKey
	runner.sessionRuns = append(runner.sessionRuns, input)
	runner.sessionCodexSecret = input.CodexAuthSecretName
	runner.sessionGitHubSecret = input.GitHubSecretName
	if strings.TrimSpace(input.InternalToken) == "" {
		return runtimerepo.StartedAgentSession{}, fmt.Errorf("internal token is required")
	}
	if len(runner.sessionStartErrors) > 0 {
		err := runner.sessionStartErrors[0]
		runner.sessionStartErrors = runner.sessionStartErrors[1:]
		if err != nil {
			return runtimerepo.StartedAgentSession{}, err
		}
	}
	secretName := "matter-codex-session-" + input.SessionKey
	if runner.botTokenSecrets == nil {
		runner.botTokenSecrets = map[string]string{}
	}
	runner.botTokenSecrets[secretName] = input.InternalToken
	return runtimerepo.StartedAgentSession{
		SessionKey: input.SessionKey,
		Namespace:  "mattermost",
		PodName:    "mc-session-" + input.SessionKey,
		PVCName:    "mc-session-ws-" + input.SessionKey,
		SecretName: secretName,
		Created:    true,
	}, nil
}

func (runner *fakeRuntimeRunner) CleanupAgentSession(_ context.Context, sessionKey string) (runtimerepo.AgentSessionCleanupResult, error) {
	runner.cleanedSessionKey = sessionKey
	runner.cleanedSessionKeys = append(runner.cleanedSessionKeys, sessionKey)
	if len(runner.sessionCleanupErrors) > 0 {
		err := runner.sessionCleanupErrors[0]
		runner.sessionCleanupErrors = runner.sessionCleanupErrors[1:]
		if err != nil {
			return runtimerepo.AgentSessionCleanupResult{}, err
		}
	}
	return runtimerepo.AgentSessionCleanupResult{
		SessionKey: sessionKey,
		Namespace:  "mattermost",
		PodName:    "mc-session-" + sessionKey,
		PodDeleted: true,
	}, nil
}

func (runner *fakeRuntimeRunner) GetAgentSessionRuntimeHealth(_ context.Context, sessionKey string) (runtimerepo.AgentSessionRuntimeHealth, error) {
	runner.sessionRuntimeHealthCalls++
	if strings.TrimSpace(runner.sessionRuntimeHealth.SessionKey) == "" {
		return runtimerepo.AgentSessionRuntimeHealth{
			SessionKey: sessionKey,
			Namespace:  "mattermost",
			PodName:    "mc-session-" + sessionKey,
			Exists:     true,
			Phase:      "Running",
			Terminal:   false,
			Reason:     "Running",
		}, nil
	}
	health := runner.sessionRuntimeHealth
	health.SessionKey = sessionKey
	if strings.TrimSpace(health.PodName) == "" {
		health.PodName = "mc-session-" + sessionKey
	}
	if strings.TrimSpace(health.Namespace) == "" {
		health.Namespace = "mattermost"
	}
	return health, nil
}

func (runner *fakeRuntimeRunner) UpsertMattermostBotTokenSecret(_ context.Context, input runtimerepo.MattermostBotTokenSecretInput) (runtimerepo.MattermostBotTokenSecret, error) {
	if runner.botTokenSecrets == nil {
		runner.botTokenSecrets = map[string]string{}
	}
	runner.botTokenSecrets[input.SecretName] = input.Token
	return runtimerepo.MattermostBotTokenSecret{
		SecretName: input.SecretName,
		Namespace:  "mattermost",
		Created:    true,
		Token:      input.Token,
	}, nil
}

func (runner *fakeRuntimeRunner) GetMattermostBotTokenSecret(_ context.Context, secretName string) (runtimerepo.MattermostBotTokenSecret, error) {
	runner.botTokenSecretReads++
	if token := runner.botTokenSecrets[secretName]; token != "" {
		if runner.secretIntegrityErr != nil {
			return runtimerepo.MattermostBotTokenSecret{}, runner.secretIntegrityErr
		}
		integrity, ok := runner.secretIntegrity[secretName+"/token"]
		if !ok {
			integrity = runtimerepo.SecretIntegrity{
				SecretName: secretName, SecretKey: "token",
				ContentSHA256: "synthetic-sha256", UID: "synthetic-uid", ResourceVersion: "1",
			}
		}
		return runtimerepo.MattermostBotTokenSecret{SecretName: secretName, Namespace: "mattermost", Token: token, Integrity: integrity}, nil
	}
	return runtimerepo.MattermostBotTokenSecret{}, fmt.Errorf("token secret not found")
}

func (runner *fakeRuntimeRunner) GetRunStatus(_ context.Context, runID string) (runtimerepo.RunStatus, error) {
	if status, ok := runner.runStatuses[runID]; ok {
		return status, nil
	}
	artifacts := map[string]string{"pr-url": "https://github.example/pr/10"}
	logTail := "matter-codex smoke done\nmatter-codex artifact pr-url: https://github.example/pr/10\nsmoke-ok"
	if strings.HasPrefix(runID, "review-") {
		artifacts = map[string]string{
			"pr-url":           "https://github.example/pr/12",
			"review-decision":  "comment",
			"review-submitted": "true",
		}
		logTail = "matter-codex reviewer run done\nmatter-codex artifact pr-url: https://github.example/pr/12\nmatter-codex artifact review-decision: comment\nmatter-codex artifact review-submitted: true"
	}
	return runtimerepo.RunStatus{
		RunID:        runID,
		Namespace:    "mattermost",
		JobName:      "mc-run-" + runID,
		PVCName:      "mc-ws-" + runID,
		PodName:      "mc-run-" + runID + "-pod",
		Exists:       true,
		JobSucceeded: 1,
		PodPhase:     "Succeeded",
		LogTail:      logTail,
		Artifacts:    artifacts,
	}, nil
}

func (runner *fakeRuntimeRunner) CleanupRun(_ context.Context, runID string) (runtimerepo.CleanupResult, error) {
	runner.cleanedRunID = runID
	runner.cleanedRunIDs = append(runner.cleanedRunIDs, runID)
	return runtimerepo.CleanupResult{
		RunID:      runID,
		Namespace:  "mattermost",
		JobDeleted: true,
		PVCDeleted: true,
	}, nil
}

func (runner *fakeRuntimeRunner) CleanupExpiredRuns(_ context.Context, input runtimerepo.RetentionCleanupInput) (runtimerepo.RetentionCleanupResult, error) {
	runner.retentionInput = input
	result := runner.retentionResult
	if result.Namespace == "" {
		result.Namespace = "mattermost"
	}
	if result.OlderThan == 0 {
		result.OlderThan = input.OlderThan
	}
	result.DryRun = input.DryRun
	return result, nil
}

type fakeAdminStore struct {
	capacityMu           sync.Mutex
	deliveryMu           sync.Mutex
	upsert               adminrepo.UpsertRepositoryInput
	auditRecorded        bool
	repositories         map[string]entity.Repository
	deletedRepo          entity.Repository
	profiles             []entity.AgentProfile
	openAIAccounts       map[string]entity.OpenAIAccount
	deletedOpenAIAccount entity.OpenAIAccount
	githubAccounts       map[string]entity.GitHubAccount
	githubUpsert         adminrepo.UpsertGitHubAccountInput
	deletedGitHubAccount entity.GitHubAccount
	profileUpsert        adminrepo.UpsertAgentProfileInput
	promptTemplates      map[string]entity.AgentPromptTemplate
	agentRun             entity.AgentRun
	agentRuns            map[string]entity.AgentRun
	agentFlows           map[string]entity.AgentFlow
	updatedRunStatus     string
	updatedPRURL         string
	projects             map[int64]entity.Project
	projectRepositories  map[string]entity.ProjectRepository
	runtimeVariables     map[int64]entity.ProjectRuntimeVariable
	roleRuntimeVariables map[string]entity.AgentRoleRuntimeVariableBinding
	agentRoles           map[int64]entity.AgentRole
	chats                map[int64]entity.Chat
	chatParticipants     map[int64][]entity.ChatParticipant
	chatRepositories     map[int64][]entity.ChatRepositoryBinding
	threadContexts       map[int64]entity.ThreadContext
	botIdentities        map[int64]entity.MattermostBotIdentity
	agentSessions        map[string]entity.AgentSession
	sessionTurns         []entity.AgentSessionTurn
	agentDelegations     map[int64]entity.AgentDelegation
	callbackDeliveries   map[int64]entity.AgentDelegationCallbackDelivery
	callbackManifests    map[string]adminrepo.CreateAgentDelegationCallbackDeliveryManifestInput
	postMessageMaxRunes  int
	frozenOpenAIAccount  string
	frozenGitHubAccount  string
	clearIdleCalls       int
	resetSessionCalls    int
	completeTurnCalls    int
	auditCalls           int
}

func (store *fakeAdminStore) RequiresClusterAdminSessionGuard(_ context.Context, roleID int64, _ string) (bool, error) {
	role := store.agentRoles[roleID]
	return strings.EqualFold(strings.TrimSpace(role.KubernetesAccess), "cluster-admin"), nil
}

func (store *fakeAdminStore) IsFrozenClusterAdminOpenAIAccount(_ context.Context, accountName string) (bool, error) {
	return accountName != "" && accountName == store.frozenOpenAIAccount, nil
}

func (store *fakeAdminStore) IsFrozenClusterAdminGitHubAccount(_ context.Context, accountName string) (bool, error) {
	return accountName != "" && accountName == store.frozenGitHubAccount, nil
}

func (store *fakeAdminStore) MattermostPostMessageMaxRunes(context.Context) (int, error) {
	return store.postMessageMaxRunes, nil
}

func (store *fakeAdminStore) UpsertRepository(_ context.Context, input adminrepo.UpsertRepositoryInput) (entity.Repository, bool, error) {
	store.upsert = input
	store.ensureRepositories()
	key := repositoryStoreKey(input.Provider, input.Owner, input.Name)
	existing, exists := store.repositories[key]
	id := existing.ID
	if id == 0 {
		id = int64(len(store.repositories) + 1)
	}
	item := entity.Repository{
		ID:                id,
		Provider:          input.Provider,
		Owner:             input.Owner,
		Name:              input.Name,
		DefaultBranch:     input.DefaultBranch,
		GitHubAccountName: input.GitHubAccountName,
		Status:            "active",
		MattermostChannel: input.MattermostChannel,
	}
	store.repositories[key] = item
	return item, !exists, nil
}

func (store *fakeAdminStore) GetRepository(_ context.Context, provider string, owner string, name string) (entity.Repository, error) {
	store.ensureRepositories()
	item, ok := store.repositories[repositoryStoreKey(provider, owner, name)]
	if !ok {
		return entity.Repository{}, adminrepo.ErrNotFound
	}
	return item, nil
}

func (store *fakeAdminStore) ListRepositories(context.Context, int) ([]entity.Repository, error) {
	store.ensureRepositories()
	items := make([]entity.Repository, 0, len(store.repositories))
	for _, item := range store.repositories {
		items = append(items, item)
	}
	return items, nil
}

func (store *fakeAdminStore) DeleteRepository(_ context.Context, provider string, owner string, name string) (entity.Repository, error) {
	store.ensureRepositories()
	key := repositoryStoreKey(provider, owner, name)
	item, ok := store.repositories[key]
	if !ok {
		return entity.Repository{}, adminrepo.ErrNotFound
	}
	delete(store.repositories, key)
	store.deletedRepo = item
	return item, nil
}

func (store *fakeAdminStore) ensureRepositories() {
	if store.repositories == nil {
		store.repositories = map[string]entity.Repository{}
	}
}

func (store *fakeAdminStore) UpsertProject(_ context.Context, input adminrepo.UpsertProjectInput) (entity.Project, bool, error) {
	store.ensureProjects()
	for id, project := range store.projects {
		if project.Slug == input.Slug {
			project.Name = input.Name
			project.MattermostTeamID = input.MattermostTeamID
			project.GitHubAccountName = input.GitHubAccountName
			project.GitHubOwner = input.GitHubOwner
			project.GitHubOwnerType = input.GitHubOwnerType
			project.Description = input.Description
			project.AdvancedSettings = input.AdvancedSettings
			store.projects[id] = project
			return project, false, nil
		}
	}
	project := entity.Project{
		ID:                int64(len(store.projects) + 1),
		Name:              input.Name,
		Slug:              input.Slug,
		MattermostTeamID:  input.MattermostTeamID,
		GitHubAccountName: input.GitHubAccountName,
		GitHubOwner:       input.GitHubOwner,
		GitHubOwnerType:   input.GitHubOwnerType,
		Description:       input.Description,
		AdvancedSettings:  input.AdvancedSettings,
	}
	store.projects[project.ID] = project
	return project, true, nil
}

func (store *fakeAdminStore) UpdateProjectRunsChannel(_ context.Context, projectID int64, channelID string) (entity.Project, error) {
	store.ensureProjects()
	project, ok := store.projects[projectID]
	if !ok {
		return entity.Project{}, adminrepo.ErrNotFound
	}
	project.MattermostRunsChannelID = channelID
	store.projects[projectID] = project
	return project, nil
}

func (store *fakeAdminStore) GetProject(_ context.Context, id int64) (entity.Project, error) {
	store.ensureProjects()
	project, ok := store.projects[id]
	if !ok {
		return entity.Project{}, adminrepo.ErrNotFound
	}
	return project, nil
}

func (store *fakeAdminStore) GetProjectBySlug(_ context.Context, slug string) (entity.Project, error) {
	store.ensureProjects()
	for _, project := range store.projects {
		if project.Slug == slug {
			return project, nil
		}
	}
	return entity.Project{}, adminrepo.ErrNotFound
}

func (store *fakeAdminStore) ListProjects(_ context.Context, limit int) ([]entity.Project, error) {
	store.ensureProjects()
	projects := make([]entity.Project, 0, len(store.projects))
	for _, project := range store.projects {
		projects = append(projects, project)
	}
	sort.Slice(projects, func(i, j int) bool {
		return projects[i].ID < projects[j].ID
	})
	if limit > 0 && len(projects) > limit {
		projects = projects[:limit]
	}
	return projects, nil
}

func (store *fakeAdminStore) UpsertProjectRepository(_ context.Context, input adminrepo.UpsertProjectRepositoryInput) (entity.ProjectRepository, bool, error) {
	store.ensureProjectRepositories()
	repo, ok := store.findRepositoryByID(input.RepositoryID)
	if !ok {
		return entity.ProjectRepository{}, false, adminrepo.ErrNotFound
	}
	key := fmt.Sprintf("%d:%d", input.ProjectID, input.RepositoryID)
	existing, exists := store.projectRepositories[key]
	id := existing.ID
	if id == 0 {
		id = int64(len(store.projectRepositories) + 1)
	}
	binding := entity.ProjectRepository{
		ID:            id,
		ProjectID:     input.ProjectID,
		RepositoryID:  input.RepositoryID,
		Provider:      repo.Provider,
		Owner:         repo.Owner,
		Name:          repo.Name,
		DefaultBranch: repo.DefaultBranch,
		IsDefault:     input.IsDefault,
		Metadata:      input.Metadata,
	}
	store.projectRepositories[key] = binding
	return binding, !exists, nil
}

func (store *fakeAdminStore) ListProjectRepositories(_ context.Context, projectID int64) ([]entity.ProjectRepository, error) {
	store.ensureProjectRepositories()
	items := make([]entity.ProjectRepository, 0, len(store.projectRepositories))
	for _, item := range store.projectRepositories {
		if item.ProjectID == projectID {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].FullName() < items[j].FullName()
	})
	return items, nil
}

func (store *fakeAdminStore) UpsertProjectRuntimeVariable(_ context.Context, input adminrepo.UpsertProjectRuntimeVariableInput) (entity.ProjectRuntimeVariable, bool, error) {
	store.ensureRuntimeVariables()
	for id, item := range store.runtimeVariables {
		if item.ProjectID == input.ProjectID && item.Name == input.Name {
			item.Slug = input.Slug
			item.Description = input.Description
			item.SecretRef = input.SecretRef
			item.SecretKey = input.SecretKey
			item.Sensitive = input.Sensitive
			item.Enabled = input.Enabled
			store.runtimeVariables[id] = item
			return item, false, nil
		}
	}
	item := entity.ProjectRuntimeVariable{
		ID:          int64(len(store.runtimeVariables) + 1),
		ProjectID:   input.ProjectID,
		Name:        input.Name,
		Slug:        input.Slug,
		Description: input.Description,
		SecretRef:   input.SecretRef,
		SecretKey:   input.SecretKey,
		Sensitive:   input.Sensitive,
		Enabled:     input.Enabled,
	}
	store.runtimeVariables[item.ID] = item
	return item, true, nil
}

func (store *fakeAdminStore) GetProjectRuntimeVariable(_ context.Context, id int64) (entity.ProjectRuntimeVariable, error) {
	store.ensureRuntimeVariables()
	item, ok := store.runtimeVariables[id]
	if !ok {
		return entity.ProjectRuntimeVariable{}, adminrepo.ErrNotFound
	}
	return item, nil
}

func (store *fakeAdminStore) ListProjectRuntimeVariables(_ context.Context, projectID int64) ([]entity.ProjectRuntimeVariable, error) {
	store.ensureRuntimeVariables()
	items := make([]entity.ProjectRuntimeVariable, 0, len(store.runtimeVariables))
	for _, item := range store.runtimeVariables {
		if item.ProjectID == projectID {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	return items, nil
}

func (store *fakeAdminStore) DeleteProjectRuntimeVariable(_ context.Context, id int64) (entity.ProjectRuntimeVariable, error) {
	store.ensureRuntimeVariables()
	item, ok := store.runtimeVariables[id]
	if !ok {
		return entity.ProjectRuntimeVariable{}, adminrepo.ErrNotFound
	}
	delete(store.runtimeVariables, id)
	for key, binding := range store.roleRuntimeVariables {
		if binding.VariableID == id {
			delete(store.roleRuntimeVariables, key)
		}
	}
	return item, nil
}

func (store *fakeAdminStore) UpsertAgentRoleRuntimeVariable(_ context.Context, input adminrepo.UpsertAgentRoleRuntimeVariableInput) (entity.AgentRoleRuntimeVariableBinding, bool, error) {
	store.ensureRoleRuntimeVariables()
	role, ok := store.agentRoles[input.RoleID]
	if !ok {
		return entity.AgentRoleRuntimeVariableBinding{}, false, adminrepo.ErrNotFound
	}
	variable, ok := store.runtimeVariables[input.VariableID]
	if !ok {
		return entity.AgentRoleRuntimeVariableBinding{}, false, adminrepo.ErrNotFound
	}
	key := fmt.Sprintf("%d:%d", input.RoleID, input.VariableID)
	existing, exists := store.roleRuntimeVariables[key]
	id := existing.ID
	if id == 0 {
		id = int64(len(store.roleRuntimeVariables) + 1)
	}
	binding := entity.AgentRoleRuntimeVariableBinding{
		ID:          id,
		RoleID:      role.ID,
		RoleName:    role.Name,
		VariableID:  variable.ID,
		ProjectID:   variable.ProjectID,
		Name:        variable.Name,
		Slug:        variable.Slug,
		Description: variable.Description,
		SecretRef:   variable.SecretRef,
		SecretKey:   variable.SecretKey,
		Sensitive:   variable.Sensitive,
		Enabled:     variable.Enabled,
	}
	store.roleRuntimeVariables[key] = binding
	return binding, !exists, nil
}

func (store *fakeAdminStore) DeleteAgentRoleRuntimeVariable(_ context.Context, roleID int64, variableID int64) (entity.AgentRoleRuntimeVariableBinding, error) {
	store.ensureRoleRuntimeVariables()
	key := fmt.Sprintf("%d:%d", roleID, variableID)
	binding, ok := store.roleRuntimeVariables[key]
	if !ok {
		return entity.AgentRoleRuntimeVariableBinding{}, adminrepo.ErrNotFound
	}
	delete(store.roleRuntimeVariables, key)
	return binding, nil
}

func (store *fakeAdminStore) ListAgentRoleRuntimeVariables(_ context.Context, roleID int64) ([]entity.AgentRoleRuntimeVariableBinding, error) {
	store.ensureRoleRuntimeVariables()
	items := make([]entity.AgentRoleRuntimeVariableBinding, 0, len(store.roleRuntimeVariables))
	for _, item := range store.roleRuntimeVariables {
		if item.RoleID == roleID {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	return items, nil
}

func (store *fakeAdminStore) UpsertAgentRole(_ context.Context, input adminrepo.UpsertAgentRoleInput) (entity.AgentRole, bool, error) {
	store.ensureAgentRoles()
	for id, role := range store.agentRoles {
		if role.ProjectID == input.ProjectID && role.Name == input.Name {
			if strings.EqualFold(strings.TrimSpace(input.KubernetesAccess), "cluster-admin") && !strings.EqualFold(strings.TrimSpace(role.KubernetesAccess), "cluster-admin") {
				return entity.AgentRole{}, false, adminrepo.ErrClusterAdminAdmissionDenied
			}
			role.RoleType = input.RoleType
			role.Description = input.Description
			role.PromptTemplate = input.PromptTemplate
			role.PromptMode = input.PromptMode
			role.GitHubAccountName = input.GitHubAccountName
			role.OpenAIAccountName = input.OpenAIAccountName
			role.KubernetesAccess = input.KubernetesAccess
			role.SandboxMode = input.SandboxMode
			role.ConfigOverlay = input.ConfigOverlay
			role.AdvancedSettings = input.AdvancedSettings
			role.Enabled = input.Enabled
			role.BotIdentity = input.BotIdentity
			store.agentRoles[id] = role
			return role, false, nil
		}
	}
	if strings.EqualFold(strings.TrimSpace(input.KubernetesAccess), "cluster-admin") {
		return entity.AgentRole{}, false, adminrepo.ErrClusterAdminAdmissionDenied
	}
	role := entity.AgentRole{
		ID:                int64(len(store.agentRoles) + 1),
		ProjectID:         input.ProjectID,
		Name:              input.Name,
		RoleType:          input.RoleType,
		Description:       input.Description,
		PromptTemplate:    input.PromptTemplate,
		PromptMode:        input.PromptMode,
		GitHubAccountName: input.GitHubAccountName,
		OpenAIAccountName: input.OpenAIAccountName,
		KubernetesAccess:  input.KubernetesAccess,
		SandboxMode:       input.SandboxMode,
		ConfigOverlay:     input.ConfigOverlay,
		AdvancedSettings:  input.AdvancedSettings,
		Enabled:           input.Enabled,
		BotIdentity:       input.BotIdentity,
	}
	store.agentRoles[role.ID] = role
	return role, true, nil
}

func (store *fakeAdminStore) GetAgentRole(_ context.Context, id int64) (entity.AgentRole, error) {
	store.ensureAgentRoles()
	role, ok := store.agentRoles[id]
	if !ok {
		return entity.AgentRole{}, adminrepo.ErrNotFound
	}
	return role, nil
}

func (store *fakeAdminStore) ListAgentRoles(_ context.Context, projectID int64) ([]entity.AgentRole, error) {
	store.ensureAgentRoles()
	roles := make([]entity.AgentRole, 0, len(store.agentRoles))
	for _, role := range store.agentRoles {
		if projectID == 0 || role.ProjectID == projectID {
			roles = append(roles, role)
		}
	}
	sort.Slice(roles, func(i, j int) bool {
		return roles[i].ID < roles[j].ID
	})
	return roles, nil
}

func (store *fakeAdminStore) CreateChat(_ context.Context, input adminrepo.CreateChatInput) (entity.Chat, bool, error) {
	store.ensureChats()
	store.ensureChatParticipants()
	store.ensureChatRepositories()
	for id, chat := range store.chats {
		if chat.ProjectID == input.ProjectID && chat.Slug == input.Slug {
			chat.Name = input.Name
			chat.MattermostChannelID = input.MattermostChannelID
			chat.Description = input.Description
			chat.ChatType = input.ChatType
			chat.RootGitHubIssue = input.RootGitHubIssue
			chat.WorkPolicy = input.WorkPolicy
			chat.Settings = input.Settings
			chat.SystemPurpose = input.SystemPurpose
			store.chats[id] = chat
			store.setChatBindings(chat.ID, input.RoleIDs, input.RepositoryIDs)
			return chat, false, nil
		}
	}
	chat := entity.Chat{
		ID:                  int64(len(store.chats) + 1),
		ProjectID:           input.ProjectID,
		MattermostChannelID: input.MattermostChannelID,
		Name:                input.Name,
		Slug:                input.Slug,
		Description:         input.Description,
		ChatType:            input.ChatType,
		RootGitHubIssue:     input.RootGitHubIssue,
		WorkPolicy:          input.WorkPolicy,
		Settings:            input.Settings,
		SystemPurpose:       input.SystemPurpose,
	}
	store.chats[chat.ID] = chat
	store.setChatBindings(chat.ID, input.RoleIDs, input.RepositoryIDs)
	return chat, true, nil
}

func (store *fakeAdminStore) GetChat(_ context.Context, id int64) (entity.Chat, error) {
	store.ensureChats()
	chat, ok := store.chats[id]
	if !ok {
		return entity.Chat{}, adminrepo.ErrNotFound
	}
	return chat, nil
}

func (store *fakeAdminStore) GetChatByMattermostChannelID(_ context.Context, channelID string) (entity.Chat, error) {
	store.ensureChats()
	for _, chat := range store.chats {
		if chat.MattermostChannelID == channelID {
			return chat, nil
		}
	}
	return entity.Chat{}, adminrepo.ErrNotFound
}

func (store *fakeAdminStore) ListChats(_ context.Context, projectID int64) ([]entity.Chat, error) {
	store.ensureChats()
	chats := make([]entity.Chat, 0, len(store.chats))
	for _, chat := range store.chats {
		if projectID == 0 || chat.ProjectID == projectID {
			chats = append(chats, chat)
		}
	}
	sort.Slice(chats, func(i, j int) bool {
		return chats[i].ID < chats[j].ID
	})
	return chats, nil
}

func (store *fakeAdminStore) ListChatParticipants(_ context.Context, chatID int64) ([]entity.ChatParticipant, error) {
	store.ensureChatParticipants()
	return append([]entity.ChatParticipant(nil), store.chatParticipants[chatID]...), nil
}

func (store *fakeAdminStore) ListChatRepositories(_ context.Context, chatID int64) ([]entity.ChatRepositoryBinding, error) {
	store.ensureChatRepositories()
	return append([]entity.ChatRepositoryBinding(nil), store.chatRepositories[chatID]...), nil
}

func (store *fakeAdminStore) GetThreadContext(_ context.Context, chatID int64, rootPostID string) (entity.ThreadContext, error) {
	store.ensureThreadContexts()
	for _, item := range store.threadContexts {
		if item.ChatID == chatID && item.MattermostRootPostID == rootPostID {
			return item, nil
		}
	}
	return entity.ThreadContext{}, adminrepo.ErrNotFound
}

func (store *fakeAdminStore) GetThreadContextByID(_ context.Context, id int64) (entity.ThreadContext, error) {
	store.ensureThreadContexts()
	item, ok := store.threadContexts[id]
	if !ok {
		return entity.ThreadContext{}, adminrepo.ErrNotFound
	}
	return item, nil
}

func (store *fakeAdminStore) UpsertThreadContext(_ context.Context, input adminrepo.UpsertThreadContextInput) (entity.ThreadContext, bool, error) {
	store.ensureThreadContexts()
	for id, item := range store.threadContexts {
		if item.ChatID == input.ChatID && item.MattermostRootPostID == input.MattermostRootPostID {
			item.ProjectID = input.ProjectID
			item.MattermostChannelID = input.MattermostChannelID
			item.RepositoryID = input.RepositoryID
			item.Status = input.Status
			if input.PendingMattermostPostID != "" {
				item.PendingMattermostPostID = input.PendingMattermostPostID
			}
			if input.PendingUserID != "" {
				item.PendingUserID = input.PendingUserID
			}
			if input.PendingUserName != "" {
				item.PendingUserName = input.PendingUserName
			}
			if input.PendingMessage != "" {
				item.PendingMessage = input.PendingMessage
			}
			store.hydrateThreadContextRepository(&item)
			store.threadContexts[id] = item
			return item, false, nil
		}
	}
	item := entity.ThreadContext{
		ID:                      int64(len(store.threadContexts) + 1),
		ProjectID:               input.ProjectID,
		ChatID:                  input.ChatID,
		MattermostChannelID:     input.MattermostChannelID,
		MattermostRootPostID:    input.MattermostRootPostID,
		RepositoryID:            input.RepositoryID,
		Status:                  input.Status,
		PendingMattermostPostID: input.PendingMattermostPostID,
		PendingUserID:           input.PendingUserID,
		PendingUserName:         input.PendingUserName,
		PendingMessage:          input.PendingMessage,
	}
	store.hydrateThreadContextRepository(&item)
	store.threadContexts[item.ID] = item
	return item, true, nil
}

func (store *fakeAdminStore) hydrateThreadContextRepository(context *entity.ThreadContext) {
	if context.RepositoryID == 0 {
		context.RepositoryProvider = ""
		context.RepositoryOwner = ""
		context.RepositoryName = ""
		context.RepositoryDefaultBranch = ""
		return
	}
	if repo, ok := store.findRepositoryByID(context.RepositoryID); ok {
		context.RepositoryProvider = repo.Provider
		context.RepositoryOwner = repo.Owner
		context.RepositoryName = repo.Name
		context.RepositoryDefaultBranch = repo.DefaultBranch
	}
}

func (store *fakeAdminStore) UpsertMattermostBotIdentity(_ context.Context, input adminrepo.UpsertMattermostBotIdentityInput) (entity.MattermostBotIdentity, bool, error) {
	store.ensureBotIdentities()
	if existing, ok := store.botIdentities[input.RoleID]; ok {
		existing.Username = input.Username
		existing.DisplayName = input.DisplayName
		existing.MattermostUserID = input.MattermostUserID
		existing.TokenSecretRef = input.TokenSecretRef
		existing.Status = input.Status
		existing.LastError = input.LastError
		store.botIdentities[input.RoleID] = existing
		return existing, false, nil
	}
	identity := entity.MattermostBotIdentity{
		ID:               int64(len(store.botIdentities) + 1),
		ProjectID:        input.ProjectID,
		RoleID:           input.RoleID,
		Username:         input.Username,
		DisplayName:      input.DisplayName,
		MattermostUserID: input.MattermostUserID,
		TokenSecretRef:   input.TokenSecretRef,
		Status:           input.Status,
		LastError:        input.LastError,
	}
	store.botIdentities[input.RoleID] = identity
	return identity, true, nil
}

func (store *fakeAdminStore) GetMattermostBotIdentityByRoleID(_ context.Context, roleID int64) (entity.MattermostBotIdentity, error) {
	store.ensureBotIdentities()
	if identity, ok := store.botIdentities[roleID]; ok {
		return identity, nil
	}
	return entity.MattermostBotIdentity{}, adminrepo.ErrNotFound
}

func (store *fakeAdminStore) GetMattermostBotIdentityByUserID(_ context.Context, mattermostUserID string) (entity.MattermostBotIdentity, error) {
	store.ensureBotIdentities()
	for _, identity := range store.botIdentities {
		if identity.MattermostUserID == mattermostUserID {
			return identity, nil
		}
	}
	return entity.MattermostBotIdentity{}, adminrepo.ErrNotFound
}

func (store *fakeAdminStore) ListMattermostBotIdentitiesByProject(_ context.Context, projectID int64) ([]entity.MattermostBotIdentity, error) {
	store.ensureBotIdentities()
	var identities []entity.MattermostBotIdentity
	for _, identity := range store.botIdentities {
		if projectID == 0 || identity.ProjectID == projectID {
			identities = append(identities, identity)
		}
	}
	sort.Slice(identities, func(i int, j int) bool {
		return identities[i].ID < identities[j].ID
	})
	return identities, nil
}

func (store *fakeAdminStore) UpsertAgentSession(_ context.Context, input adminrepo.UpsertAgentSessionInput) (entity.AgentSession, bool, error) {
	store.ensureAgentSessions()
	session, exists := store.agentSessions[input.SessionKey]
	if !exists {
		session = entity.AgentSession{
			ID:         int64(len(store.agentSessions) + 1),
			SessionKey: input.SessionKey,
			Status:     agentSessionStatusIdle,
		}
	}
	session.ProjectID = input.ProjectID
	session.ChatID = input.ChatID
	session.RoleID = input.RoleID
	session.SessionScope = input.SessionScope
	session.MattermostChannelID = input.MattermostChannelID
	session.MattermostRootPostID = input.MattermostRootPostID
	session.TTLSeconds = input.TTLSeconds
	session.Capabilities = input.Capabilities
	session.ExpiresAt = time.Now().Add(time.Duration(input.TTLSeconds) * time.Second)
	store.agentSessions[input.SessionKey] = session
	return session, !exists, nil
}

func (store *fakeAdminStore) GetAgentSession(_ context.Context, sessionKey string) (entity.AgentSession, error) {
	store.ensureAgentSessions()
	if session, ok := store.agentSessions[sessionKey]; ok {
		return session, nil
	}
	return entity.AgentSession{}, adminrepo.ErrNotFound
}

func (store *fakeAdminStore) GetAgentSessionByID(_ context.Context, id int64) (entity.AgentSession, error) {
	store.ensureAgentSessions()
	for _, session := range store.agentSessions {
		if session.ID == id {
			return session, nil
		}
	}
	return entity.AgentSession{}, adminrepo.ErrNotFound
}

func (store *fakeAdminStore) ListAgentSessionsByThread(_ context.Context, chatID int64, rootPostID string) ([]entity.AgentSession, error) {
	store.ensureAgentSessions()
	var sessions []entity.AgentSession
	for _, session := range store.agentSessions {
		if session.ChatID == chatID && session.MattermostRootPostID == rootPostID {
			sessions = append(sessions, session)
		}
	}
	return sessions, nil
}

func (store *fakeAdminStore) ListAgentSessionsByChat(_ context.Context, chatID int64) ([]entity.AgentSession, error) {
	store.ensureAgentSessions()
	var sessions []entity.AgentSession
	for _, session := range store.agentSessions {
		if session.ChatID == chatID {
			sessions = append(sessions, session)
		}
	}
	return sessions, nil
}

func (store *fakeAdminStore) ListAgentSessionsByRole(_ context.Context, roleID int64) ([]entity.AgentSession, error) {
	store.ensureAgentSessions()
	var sessions []entity.AgentSession
	for _, session := range store.agentSessions {
		if session.RoleID == roleID {
			sessions = append(sessions, session)
		}
	}
	return sessions, nil
}

func (store *fakeAdminStore) AcquireAgentSessionCapacityLock(_ context.Context) (func(), error) {
	store.capacityMu.Lock()
	return store.capacityMu.Unlock, nil
}

func (store *fakeAdminStore) ListEvictableIdleAgentSessions(_ context.Context, limit int) ([]entity.AgentSession, error) {
	store.ensureAgentSessions()
	if limit <= 0 {
		limit = 20
	}
	var sessions []entity.AgentSession
	for _, session := range store.agentSessions {
		if session.Status != agentSessionStatusIdle || session.ActiveTurnID != 0 || strings.TrimSpace(session.PodName) == "" {
			continue
		}
		busy := false
		for _, turn := range store.sessionTurns {
			if turn.SessionID == session.ID && (turn.Status == agentSessionTurnQueued || turn.Status == agentSessionTurnRunning) {
				busy = true
				break
			}
		}
		if !busy {
			sessions = append(sessions, session)
		}
	}
	sort.Slice(sessions, func(i int, j int) bool {
		if sessions[i].LastActivityAt.Equal(sessions[j].LastActivityAt) {
			return sessions[i].ID < sessions[j].ID
		}
		return sessions[i].LastActivityAt.Before(sessions[j].LastActivityAt)
	})
	if len(sessions) > limit {
		sessions = sessions[:limit]
	}
	return sessions, nil
}

func (store *fakeAdminStore) ListQueuedIdleAgentSessions(_ context.Context, limit int) ([]entity.AgentSession, error) {
	store.ensureAgentSessions()
	if limit <= 0 {
		limit = 20
	}
	var sessions []entity.AgentSession
	seen := map[int64]struct{}{}
	for _, turn := range store.sessionTurns {
		if turn.Status != agentSessionTurnQueued {
			continue
		}
		session, err := store.GetAgentSessionByID(context.Background(), turn.SessionID)
		if err != nil || session.ActiveTurnID != 0 {
			continue
		}
		if _, exists := seen[session.ID]; exists {
			continue
		}
		seen[session.ID] = struct{}{}
		sessions = append(sessions, session)
		if len(sessions) >= limit {
			break
		}
	}
	return sessions, nil
}

func (store *fakeAdminStore) ListStaleActiveAgentSessions(_ context.Context, limit int) ([]entity.AgentSession, error) {
	store.ensureAgentSessions()
	if limit <= 0 {
		limit = 20
	}
	var sessions []entity.AgentSession
	for _, session := range store.agentSessions {
		if session.ActiveTurnID == 0 {
			continue
		}
		turn, err := store.GetAgentSessionTurn(context.Background(), session.ActiveTurnID)
		if err != nil || !agentSessionTurnTerminal(turn.Status) {
			continue
		}
		sessions = append(sessions, session)
		if len(sessions) >= limit {
			break
		}
	}
	return sessions, nil
}

func (store *fakeAdminStore) ListRunningActiveAgentSessions(_ context.Context, limit int) ([]entity.AgentSession, error) {
	store.ensureAgentSessions()
	if limit <= 0 {
		limit = 20
	}
	var sessions []entity.AgentSession
	for _, session := range store.agentSessions {
		if session.ActiveTurnID == 0 {
			continue
		}
		turn, err := store.GetAgentSessionTurn(context.Background(), session.ActiveTurnID)
		if err != nil || turn.Status != agentSessionTurnRunning {
			continue
		}
		sessions = append(sessions, session)
		if len(sessions) >= limit {
			break
		}
	}
	return sessions, nil
}

func (store *fakeAdminStore) UpdateAgentSessionRuntime(_ context.Context, input adminrepo.UpdateAgentSessionRuntimeInput) (entity.AgentSession, error) {
	store.ensureAgentSessions()
	session, ok := store.agentSessions[input.SessionKey]
	if !ok {
		return entity.AgentSession{}, adminrepo.ErrNotFound
	}
	if input.Status != "" && !(input.Status == agentSessionStatusIdle && session.ActiveTurnID != 0) {
		session.Status = input.Status
	}
	if input.ActiveTurnID > 0 {
		session.ActiveTurnID = input.ActiveTurnID
	}
	if input.ActiveRunID != "" {
		session.ActiveRunID = input.ActiveRunID
	}
	if input.MattermostRootPostID != "" {
		session.MattermostRootPostID = input.MattermostRootPostID
	}
	if input.KubernetesNamespace != "" {
		session.KubernetesNamespace = input.KubernetesNamespace
	}
	if input.PodName != "" {
		session.PodName = input.PodName
	}
	if input.PVCName != "" {
		session.PVCName = input.PVCName
	}
	if input.TokenSecretRef != "" {
		session.TokenSecretRef = input.TokenSecretRef
	}
	if input.ExtendTTLSeconds > 0 {
		session.ExpiresAt = time.Now().Add(time.Duration(input.ExtendTTLSeconds) * time.Second)
	}
	store.agentSessions[input.SessionKey] = session
	return session, nil
}

func (store *fakeAdminStore) ClearIdleAgentSessionPod(_ context.Context, sessionKey string, podName string) (entity.AgentSession, error) {
	store.clearIdleCalls++
	store.ensureAgentSessions()
	session, ok := store.agentSessions[sessionKey]
	if !ok || session.Status != agentSessionStatusIdle || session.ActiveTurnID != 0 || session.PodName != podName {
		return entity.AgentSession{}, adminrepo.ErrNotFound
	}
	for _, turn := range store.sessionTurns {
		if turn.SessionID == session.ID && (turn.Status == agentSessionTurnQueued || turn.Status == agentSessionTurnRunning) {
			return entity.AgentSession{}, adminrepo.ErrNotFound
		}
	}
	session.KubernetesNamespace = ""
	session.PodName = ""
	store.agentSessions[sessionKey] = session
	return session, nil
}

func (store *fakeAdminStore) ResetAgentSessionRuntime(_ context.Context, sessionKey string, status string) (entity.AgentSession, error) {
	store.resetSessionCalls++
	store.ensureAgentSessions()
	session, ok := store.agentSessions[sessionKey]
	if !ok {
		return entity.AgentSession{}, adminrepo.ErrNotFound
	}
	if strings.TrimSpace(status) != "" {
		session.Status = status
	}
	session.ActiveTurnID = 0
	session.ActiveRunID = ""
	session.KubernetesNamespace = ""
	session.PodName = ""
	session.PVCName = ""
	session.TokenSecretRef = ""
	store.agentSessions[sessionKey] = session
	return session, nil
}

func (store *fakeAdminStore) UpdateAgentSessionSnapshot(_ context.Context, input adminrepo.UpdateAgentSessionSnapshotInput) (entity.AgentSession, error) {
	store.ensureAgentSessions()
	session, ok := store.agentSessions[input.SessionKey]
	if !ok {
		return entity.AgentSession{}, adminrepo.ErrNotFound
	}
	if input.CodexSessionID != "" {
		session.CodexSessionID = input.CodexSessionID
	}
	if input.SessionArchiveGzipBase64 != "" {
		session.SessionArchiveGzipBase64 = input.SessionArchiveGzipBase64
	}
	if input.Status != "" {
		session.Status = input.Status
	}
	session.ActiveTurnID = 0
	session.ActiveRunID = ""
	store.agentSessions[input.SessionKey] = session
	return session, nil
}

func (store *fakeAdminStore) CreateAgentSessionTurn(_ context.Context, input adminrepo.CreateAgentSessionTurnInput) (entity.AgentSessionTurn, error) {
	turn := entity.AgentSessionTurn{
		ID:                   int64(len(store.sessionTurns) + 1),
		SessionID:            input.SessionID,
		RunID:                input.RunID,
		MattermostChannelID:  input.MattermostChannelID,
		MattermostRootPostID: input.MattermostRootPostID,
		MattermostPostID:     input.MattermostPostID,
		UserID:               input.UserID,
		UserName:             input.UserName,
		Message:              input.Message,
		Status:               agentSessionTurnQueued,
	}
	if input.ParentTurnID > 0 {
		turn.ParentTurnIDs = []int64{input.ParentTurnID}
	}
	if input.MattermostPostID != "" {
		turn.TriggerPostIDs = []string{input.MattermostPostID}
	}
	if input.UserName != "" {
		turn.InitiatorUserNames = []string{input.UserName}
	}
	store.sessionTurns = append(store.sessionTurns, turn)
	return turn, nil
}

func (store *fakeAdminStore) GetAgentSessionTurn(_ context.Context, id int64) (entity.AgentSessionTurn, error) {
	for _, turn := range store.sessionTurns {
		if turn.ID == id {
			return turn, nil
		}
	}
	return entity.AgentSessionTurn{}, adminrepo.ErrNotFound
}

func (store *fakeAdminStore) ClaimNextAgentSessionTurn(_ context.Context, sessionKey string) (entity.AgentSessionTurn, error) {
	session, err := store.GetAgentSession(context.Background(), sessionKey)
	if err != nil {
		return entity.AgentSessionTurn{}, err
	}
	for _, turn := range store.sessionTurns {
		if turn.SessionID == session.ID && turn.Status == agentSessionTurnRunning {
			return turn, nil
		}
	}
	for index, turn := range store.sessionTurns {
		if turn.SessionID == session.ID && turn.Status == agentSessionTurnQueued {
			turn.Status = agentSessionTurnRunning
			store.sessionTurns[index] = turn
			return turn, nil
		}
	}
	return entity.AgentSessionTurn{}, adminrepo.ErrNotFound
}

func (store *fakeAdminStore) UpdateAgentSessionTurnStatusPost(_ context.Context, input adminrepo.UpdateAgentSessionTurnStatusPostInput) (entity.AgentSessionTurn, error) {
	for index, turn := range store.sessionTurns {
		if turn.ID == input.TurnID {
			turn.MattermostStatusPostID = input.StatusPostID
			store.sessionTurns[index] = turn
			return turn, nil
		}
	}
	return entity.AgentSessionTurn{}, adminrepo.ErrNotFound
}

func (store *fakeAdminStore) UpdateAgentSessionTurnRunsPost(_ context.Context, input adminrepo.UpdateAgentSessionTurnRunsPostInput) (entity.AgentSessionTurn, error) {
	for index, turn := range store.sessionTurns {
		if turn.ID == input.TurnID {
			turn.MattermostRunsPostID = input.RunsPostID
			store.sessionTurns[index] = turn
			return turn, nil
		}
	}
	return entity.AgentSessionTurn{}, adminrepo.ErrNotFound
}

func (store *fakeAdminStore) AddAgentSessionTurnOrigin(_ context.Context, input adminrepo.AddAgentSessionTurnOriginInput) (entity.AgentSessionTurn, error) {
	for index, turn := range store.sessionTurns {
		if turn.ID != input.TurnID {
			continue
		}
		if input.ParentTurnID > 0 && input.ParentTurnID != turn.ID && !containsInt64(turn.ParentTurnIDs, input.ParentTurnID) {
			turn.ParentTurnIDs = append(turn.ParentTurnIDs, input.ParentTurnID)
		}
		if input.TriggerPostID != "" && !containsString(turn.TriggerPostIDs, input.TriggerPostID) {
			turn.TriggerPostIDs = append(turn.TriggerPostIDs, input.TriggerPostID)
		}
		if input.InitiatorUserName != "" && !containsString(turn.InitiatorUserNames, input.InitiatorUserName) {
			turn.InitiatorUserNames = append(turn.InitiatorUserNames, input.InitiatorUserName)
		}
		store.sessionTurns[index] = turn
		return turn, nil
	}
	return entity.AgentSessionTurn{}, adminrepo.ErrNotFound
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func (store *fakeAdminStore) UpdateAgentSessionTurnMessage(_ context.Context, input adminrepo.UpdateAgentSessionTurnMessageInput) (entity.AgentSessionTurn, error) {
	for index, turn := range store.sessionTurns {
		if turn.ID == input.TurnID && turn.Status == agentSessionTurnQueued {
			turn.Message = input.Message
			turn.UpdatedAt = time.Now().UTC()
			store.sessionTurns[index] = turn
			return turn, nil
		}
	}
	return entity.AgentSessionTurn{}, adminrepo.ErrNotFound
}

func (store *fakeAdminStore) CompleteAgentSessionTurn(_ context.Context, input adminrepo.CompleteAgentSessionTurnInput) (entity.AgentSessionTurn, error) {
	store.completeTurnCalls++
	for index, turn := range store.sessionTurns {
		if turn.ID == input.TurnID {
			turn.Status = input.Status
			turn.FinalMessage = input.FinalMessage
			turn.ErrorMessage = input.ErrorMessage
			turn.Artifacts = input.Artifacts
			store.sessionTurns[index] = turn
			return turn, nil
		}
	}
	return entity.AgentSessionTurn{}, adminrepo.ErrNotFound
}

func (store *fakeAdminStore) CancelAgentSessionTurn(_ context.Context, input adminrepo.CancelAgentSessionTurnInput) (entity.AgentSessionTurn, error) {
	for index, turn := range store.sessionTurns {
		if turn.ID == input.TurnID && agentSessionTurnStoppable(turn.Status) {
			turn.Status = agentSessionTurnCanceled
			turn.ErrorMessage = input.ErrorMessage
			turn.Artifacts = input.Artifacts
			turn.FinishedAt = time.Now().UTC()
			store.sessionTurns[index] = turn
			return turn, nil
		}
	}
	return entity.AgentSessionTurn{}, adminrepo.ErrNotFound
}

func (store *fakeAdminStore) ListQueuedAgentSessionTurns(_ context.Context, sessionID int64) ([]entity.AgentSessionTurn, error) {
	var turns []entity.AgentSessionTurn
	for _, turn := range store.sessionTurns {
		if turn.SessionID == sessionID && turn.Status == agentSessionTurnQueued {
			turns = append(turns, turn)
		}
	}
	return turns, nil
}

func (store *fakeAdminStore) setChatBindings(chatID int64, roleIDs []int64, repositoryIDs []int64) {
	store.ensureAgentRoles()
	participants := make([]entity.ChatParticipant, 0, len(roleIDs))
	for index, roleID := range roleIDs {
		role := store.agentRoles[roleID]
		participants = append(participants, entity.ChatParticipant{
			ID:       int64(index + 1),
			ChatID:   chatID,
			RoleID:   roleID,
			RoleName: role.Name,
			Enabled:  role.Enabled,
		})
	}
	store.chatParticipants[chatID] = participants

	repositories := make([]entity.ChatRepositoryBinding, 0, len(repositoryIDs))
	for index, repositoryID := range repositoryIDs {
		repo, ok := store.findRepositoryByID(repositoryID)
		if !ok {
			continue
		}
		repositories = append(repositories, entity.ChatRepositoryBinding{
			ID:           int64(index + 1),
			ChatID:       chatID,
			RepositoryID: repositoryID,
			Provider:     repo.Provider,
			Owner:        repo.Owner,
			Name:         repo.Name,
		})
	}
	store.chatRepositories[chatID] = repositories
}

func (store *fakeAdminStore) findRepositoryByID(repositoryID int64) (entity.Repository, bool) {
	store.ensureRepositories()
	for _, repo := range store.repositories {
		if repo.ID == repositoryID {
			return repo, true
		}
	}
	return entity.Repository{}, false
}

func (store *fakeAdminStore) ensureProjects() {
	if store.projects == nil {
		store.projects = map[int64]entity.Project{}
	}
}

func (store *fakeAdminStore) ensureProjectRepositories() {
	if store.projectRepositories == nil {
		store.projectRepositories = map[string]entity.ProjectRepository{}
	}
}

func (store *fakeAdminStore) ensureRuntimeVariables() {
	if store.runtimeVariables == nil {
		store.runtimeVariables = map[int64]entity.ProjectRuntimeVariable{}
	}
}

func (store *fakeAdminStore) ensureRoleRuntimeVariables() {
	store.ensureAgentRoles()
	store.ensureRuntimeVariables()
	if store.roleRuntimeVariables == nil {
		store.roleRuntimeVariables = map[string]entity.AgentRoleRuntimeVariableBinding{}
	}
}

func (store *fakeAdminStore) ensureAgentRoles() {
	if store.agentRoles == nil {
		store.agentRoles = map[int64]entity.AgentRole{}
	}
}

func (store *fakeAdminStore) ensureChats() {
	if store.chats == nil {
		store.chats = map[int64]entity.Chat{}
	}
}

func (store *fakeAdminStore) ensureChatParticipants() {
	if store.chatParticipants == nil {
		store.chatParticipants = map[int64][]entity.ChatParticipant{}
	}
}

func (store *fakeAdminStore) ensureChatRepositories() {
	if store.chatRepositories == nil {
		store.chatRepositories = map[int64][]entity.ChatRepositoryBinding{}
	}
}

func (store *fakeAdminStore) ensureThreadContexts() {
	if store.threadContexts == nil {
		store.threadContexts = map[int64]entity.ThreadContext{}
	}
}

func (store *fakeAdminStore) ensureBotIdentities() {
	if store.botIdentities == nil {
		store.botIdentities = map[int64]entity.MattermostBotIdentity{}
	}
}

func (store *fakeAdminStore) ensureAgentSessions() {
	if store.agentSessions == nil {
		store.agentSessions = map[string]entity.AgentSession{}
	}
}

func repositoryStoreKey(provider string, owner string, name string) string {
	return provider + ":" + owner + "/" + name
}

func (store *fakeAdminStore) ListAgentProfiles(context.Context) ([]entity.AgentProfile, error) {
	return store.profiles, nil
}

func (store *fakeAdminStore) GetAgentProfile(_ context.Context, name string) (entity.AgentProfile, error) {
	for _, profile := range store.profiles {
		if profile.Name == name {
			return profile, nil
		}
	}
	return entity.AgentProfile{}, adminrepo.ErrNotFound
}

func (store *fakeAdminStore) UpsertAgentProfile(_ context.Context, input adminrepo.UpsertAgentProfileInput) (entity.AgentProfile, bool, error) {
	store.profileUpsert = input
	for index, profile := range store.profiles {
		if profile.Name == input.Name {
			if strings.EqualFold(strings.TrimSpace(input.KubernetesAccess), "cluster-admin") && !strings.EqualFold(strings.TrimSpace(profile.KubernetesAccess), "cluster-admin") {
				return entity.AgentProfile{}, false, adminrepo.ErrClusterAdminAdmissionDenied
			}
			profile.Role = input.Role
			profile.Description = input.Description
			profile.Enabled = input.Enabled
			profile.OpenAIAccountName = input.OpenAIAccountName
			profile.GitHubAccountName = input.GitHubAccountName
			profile.KubernetesAccess = input.KubernetesAccess
			profile.SandboxMode = input.SandboxMode
			profile.ConfigOverlay = input.ConfigOverlay
			store.profiles[index] = profile
			return profile, false, nil
		}
	}
	if strings.EqualFold(strings.TrimSpace(input.KubernetesAccess), "cluster-admin") {
		return entity.AgentProfile{}, false, adminrepo.ErrClusterAdminAdmissionDenied
	}
	profile := entity.AgentProfile{
		ID:                int64(len(store.profiles) + 1),
		Name:              input.Name,
		Role:              input.Role,
		Description:       input.Description,
		Enabled:           input.Enabled,
		OpenAIAccountName: input.OpenAIAccountName,
		GitHubAccountName: input.GitHubAccountName,
		KubernetesAccess:  input.KubernetesAccess,
		SandboxMode:       input.SandboxMode,
		ConfigOverlay:     input.ConfigOverlay,
	}
	store.profiles = append(store.profiles, profile)
	return profile, true, nil
}

func (store *fakeAdminStore) ListAgentPromptTemplates(_ context.Context, profileName string) ([]entity.AgentPromptTemplate, error) {
	store.ensurePromptTemplates()
	var items []entity.AgentPromptTemplate
	for _, item := range store.promptTemplates {
		if profileName == "" || item.ProfileName == profileName {
			items = append(items, item)
		}
	}
	return items, nil
}

func (store *fakeAdminStore) CreateAgentDelegation(_ context.Context, input adminrepo.CreateAgentDelegationInput) (entity.AgentDelegation, bool, error) {
	store.ensureAgentDelegations()
	for _, item := range store.agentDelegations {
		if item.SourceTurnID == input.SourceTurnID && item.WorkItemKey == input.WorkItemKey {
			return item, false, nil
		}
	}
	id := int64(len(store.agentDelegations) + 1)
	item := entity.AgentDelegation{
		ID:              id,
		ProjectID:       input.ProjectID,
		SourceSessionID: input.SourceSessionID,
		SourceTurnID:    input.SourceTurnID,
		TargetChatID:    input.TargetChatID,
		TargetRoleID:    input.TargetRoleID,
		WorkItemKey:     input.WorkItemKey,
		Title:           input.Title,
		Status:          agentDelegationStatusCreating,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	store.agentDelegations[id] = item
	return item, true, nil
}

func (store *fakeAdminStore) GetAgentDelegationBySourceTurnKey(_ context.Context, sourceTurnID int64, workItemKey string) (entity.AgentDelegation, error) {
	store.ensureAgentDelegations()
	for _, item := range store.agentDelegations {
		if item.SourceTurnID == sourceTurnID && item.WorkItemKey == workItemKey {
			return item, nil
		}
	}
	return entity.AgentDelegation{}, adminrepo.ErrNotFound
}

func (store *fakeAdminStore) GetAgentDelegationForCallback(_ context.Context, targetSessionID int64) (entity.AgentDelegation, error) {
	store.ensureAgentDelegations()
	var selected entity.AgentDelegation
	for _, item := range store.agentDelegations {
		if item.TargetSessionID == targetSessionID && (selected.ID == 0 || item.ID > selected.ID) {
			selected = item
		}
	}
	if selected.ID == 0 {
		return entity.AgentDelegation{}, adminrepo.ErrNotFound
	}
	return selected, nil
}

func (store *fakeAdminStore) ListAgentDelegationsBySource(_ context.Context, sourceSessionID int64, limit int) ([]entity.AgentDelegation, error) {
	store.ensureAgentDelegations()
	items := make([]entity.AgentDelegation, 0, len(store.agentDelegations))
	for id := int64(len(store.agentDelegations)); id > 0 && len(items) < limit; id-- {
		item, ok := store.agentDelegations[id]
		if ok && item.SourceSessionID == sourceSessionID {
			items = append(items, item)
		}
	}
	return items, nil
}

func (store *fakeAdminStore) SetAgentDelegationRoot(_ context.Context, id int64, rootPostID string) (entity.AgentDelegation, error) {
	return store.updateAgentDelegation(id, func(item *entity.AgentDelegation) {
		item.TargetRootPostID = rootPostID
		item.Status = "thread_created"
	})
}

func (store *fakeAdminStore) SetAgentDelegationTarget(_ context.Context, id int64, targetSessionID int64, targetTurnID int64, targetRunID string) (entity.AgentDelegation, error) {
	return store.updateAgentDelegation(id, func(item *entity.AgentDelegation) {
		item.TargetSessionID = targetSessionID
		item.TargetTurnID = targetTurnID
		item.TargetRunID = targetRunID
		item.Status = agentSessionTurnQueued
	})
}

func (store *fakeAdminStore) SetAgentDelegationFailed(_ context.Context, id int64) (entity.AgentDelegation, error) {
	return store.updateAgentDelegation(id, func(item *entity.AgentDelegation) {
		item.Status = agentDelegationStatusFailed
	})
}

func (store *fakeAdminStore) SetAgentDelegationCallback(_ context.Context, id int64, callbackTurnID int64, callbackRunID string) (entity.AgentDelegation, error) {
	return store.updateAgentDelegation(id, func(item *entity.AgentDelegation) {
		if item.CallbackTurnID == 0 {
			item.CallbackTurnID = callbackTurnID
			item.CallbackRunID = callbackRunID
			item.Status = "callback_queued"
		}
	})
}

func (store *fakeAdminStore) updateAgentDelegation(id int64, update func(*entity.AgentDelegation)) (entity.AgentDelegation, error) {
	store.ensureAgentDelegations()
	item, ok := store.agentDelegations[id]
	if !ok {
		return entity.AgentDelegation{}, adminrepo.ErrNotFound
	}
	update(&item)
	item.UpdatedAt = time.Now().UTC()
	store.agentDelegations[id] = item
	return item, nil
}

func (store *fakeAdminStore) ensureAgentDelegations() {
	if store.agentDelegations == nil {
		store.agentDelegations = map[int64]entity.AgentDelegation{}
	}
}

func (store *fakeAdminStore) GetAgentPromptTemplate(_ context.Context, profileName string, templateKey string) (entity.AgentPromptTemplate, error) {
	store.ensurePromptTemplates()
	item, ok := store.promptTemplates[promptTemplateMapKey(profileName, templateKey)]
	if !ok {
		return entity.AgentPromptTemplate{}, adminrepo.ErrNotFound
	}
	return item, nil
}

func (store *fakeAdminStore) UpsertAgentPromptTemplate(_ context.Context, input adminrepo.UpsertAgentPromptTemplateInput) (entity.AgentPromptTemplate, bool, error) {
	store.ensurePromptTemplates()
	key := promptTemplateMapKey(input.ProfileName, input.TemplateKey)
	_, exists := store.promptTemplates[key]
	item := entity.AgentPromptTemplate{
		ID:          1,
		ProfileName: input.ProfileName,
		TemplateKey: input.TemplateKey,
		Body:        input.Body,
	}
	store.promptTemplates[key] = item
	return item, !exists, nil
}

func (store *fakeAdminStore) ensurePromptTemplates() {
	if store.promptTemplates != nil {
		return
	}
	store.promptTemplates = map[string]entity.AgentPromptTemplate{
		promptTemplateMapKey("developer", developerImplementTaskKey): {
			ID:          2,
			ProfileName: "developer",
			TemplateKey: developerImplementTaskKey,
			Body:        "Implement {{.Task.Title}} for {{.Repository.FullName}} on {{.Task.HeadBranch}} using {{.Locale.Language}}",
		},
		promptTemplateMapKey("reviewer", reviewPRTemplateKey): {
			ID:          4,
			ProfileName: "reviewer",
			TemplateKey: reviewPRTemplateKey,
			Body:        "DECISION: comment\nReview {{.Repository.FullName}} PR #{{.PullRequest.Number}}",
		},
	}
}

func promptTemplateMapKey(profileName string, templateKey string) string {
	return profileName + "/" + templateKey
}

func (store *fakeAdminStore) UpsertOpenAIAccount(_ context.Context, input adminrepo.UpsertOpenAIAccountInput) (entity.OpenAIAccount, bool, error) {
	if store.openAIAccounts == nil {
		store.openAIAccounts = make(map[string]entity.OpenAIAccount)
	}
	_, exists := store.openAIAccounts[input.Name]
	account := entity.OpenAIAccount{
		ID:        1,
		Name:      input.Name,
		SecretRef: input.SecretRef,
		Status:    input.Status,
	}
	store.openAIAccounts[input.Name] = account
	return account, !exists, nil
}

func (store *fakeAdminStore) ListOpenAIAccounts(context.Context, int) ([]entity.OpenAIAccount, error) {
	accounts := make([]entity.OpenAIAccount, 0, len(store.openAIAccounts))
	for _, account := range store.openAIAccounts {
		accounts = append(accounts, account)
	}
	return accounts, nil
}

func (store *fakeAdminStore) GetOpenAIAccount(_ context.Context, name string) (entity.OpenAIAccount, error) {
	if account, ok := store.openAIAccounts[name]; ok {
		return account, nil
	}
	return entity.OpenAIAccount{}, adminrepo.ErrNotFound
}

func (store *fakeAdminStore) GetGitHubAccount(_ context.Context, name string) (entity.GitHubAccount, error) {
	store.ensureGitHubAccounts()
	if account, ok := store.githubAccounts[name]; ok {
		return account, nil
	}
	return entity.GitHubAccount{}, adminrepo.ErrNotFound
}

func (store *fakeAdminStore) ListGitHubAccounts(context.Context, int) ([]entity.GitHubAccount, error) {
	store.ensureGitHubAccounts()
	accounts := make([]entity.GitHubAccount, 0, len(store.githubAccounts))
	for _, account := range store.githubAccounts {
		accounts = append(accounts, account)
	}
	sort.Slice(accounts, func(i int, j int) bool {
		return accounts[i].Name < accounts[j].Name
	})
	return accounts, nil
}

func (store *fakeAdminStore) ensureGitHubAccounts() {
	if store.githubAccounts != nil {
		return
	}
	store.githubAccounts = map[string]entity.GitHubAccount{
		"primary": {Name: "primary", SecretRef: "matter-codex-github", Status: "configured"},
		"agent":   {Name: "agent", SecretRef: "matter-codex-github-agent", Status: "configured"},
	}
}

func (store *fakeAdminStore) UpsertGitHubAccount(_ context.Context, input adminrepo.UpsertGitHubAccountInput) (entity.GitHubAccount, bool, error) {
	store.ensureGitHubAccounts()
	store.githubUpsert = input
	_, exists := store.githubAccounts[input.Name]
	account := entity.GitHubAccount{
		ID:        1,
		Name:      input.Name,
		SecretRef: input.SecretRef,
		Username:  input.Username,
		Email:     input.Email,
		Scopes:    input.Scopes,
		Status:    input.Status,
	}
	store.githubAccounts[input.Name] = account
	return account, !exists, nil
}

func (store *fakeAdminStore) DeleteGitHubAccount(_ context.Context, name string) (entity.GitHubAccount, error) {
	store.ensureGitHubAccounts()
	account, ok := store.githubAccounts[name]
	if !ok {
		return entity.GitHubAccount{}, adminrepo.ErrNotFound
	}
	delete(store.githubAccounts, name)
	store.deletedGitHubAccount = account
	return account, nil
}

func (store *fakeAdminStore) UpdateOpenAIAccountStatus(_ context.Context, input adminrepo.UpdateOpenAIAccountStatusInput) (entity.OpenAIAccount, error) {
	if store.openAIAccounts == nil {
		store.openAIAccounts = make(map[string]entity.OpenAIAccount)
	}
	account := store.openAIAccounts[input.Name]
	account.Name = input.Name
	if input.SecretRef != "" {
		account.SecretRef = input.SecretRef
	}
	account.Status = input.Status
	store.openAIAccounts[input.Name] = account
	return account, nil
}

func (store *fakeAdminStore) DeleteOpenAIAccount(_ context.Context, name string) (entity.OpenAIAccount, error) {
	account, ok := store.openAIAccounts[name]
	if !ok {
		return entity.OpenAIAccount{}, adminrepo.ErrNotFound
	}
	delete(store.openAIAccounts, name)
	store.deletedOpenAIAccount = account
	return account, nil
}

func (store *fakeAdminStore) CreateAgentFlow(_ context.Context, input adminrepo.CreateAgentFlowInput) (entity.AgentFlow, bool, error) {
	store.ensureAgentFlows()
	if flow, ok := store.agentFlows[input.FlowID]; ok {
		return flow, false, nil
	}
	flow := entity.AgentFlow{
		ID:                   int64(len(store.agentFlows) + 1),
		FlowID:               input.FlowID,
		Status:               input.Status,
		Provider:             input.Provider,
		Owner:                input.Owner,
		Name:                 input.Name,
		BaseBranch:           input.BaseBranch,
		HeadBranch:           input.HeadBranch,
		Title:                input.Title,
		Task:                 input.Task,
		Attempt:              input.Attempt,
		MaxAttempts:          input.MaxAttempts,
		DeveloperProfileName: input.DeveloperProfileName,
		ReviewerProfileName:  input.ReviewerProfileName,
		FlowPreset:           input.FlowPreset,
		OwnerUserID:          input.OwnerUserID,
		OwnerUser:            input.OwnerUser,
		ActionToken:          input.ActionToken,
		Summary:              input.Summary,
	}
	store.agentFlows[flow.FlowID] = flow
	return flow, true, nil
}

func (store *fakeAdminStore) GetAgentFlow(_ context.Context, flowID string) (entity.AgentFlow, error) {
	store.ensureAgentFlows()
	flow, ok := store.agentFlows[flowID]
	if !ok {
		return entity.AgentFlow{}, fmt.Errorf("agent flow not found")
	}
	return flow, nil
}

func (store *fakeAdminStore) ListAgentFlows(_ context.Context, status string, limit int) ([]entity.AgentFlow, error) {
	store.ensureAgentFlows()
	flows := make([]entity.AgentFlow, 0, len(store.agentFlows))
	for _, flow := range store.agentFlows {
		if status == "" || flow.Status == status {
			flows = append(flows, flow)
		}
	}
	sort.Slice(flows, func(i, j int) bool {
		return flows[i].FlowID < flows[j].FlowID
	})
	if limit > 0 && len(flows) > limit {
		flows = flows[:limit]
	}
	return flows, nil
}

func (store *fakeAdminStore) UpdateAgentFlow(_ context.Context, input adminrepo.UpdateAgentFlowInput) (entity.AgentFlow, error) {
	store.ensureAgentFlows()
	flow, ok := store.agentFlows[input.FlowID]
	if !ok {
		return entity.AgentFlow{}, fmt.Errorf("agent flow not found")
	}
	if input.Status != "" {
		flow.Status = input.Status
	}
	if input.PRURL != "" {
		flow.PRURL = input.PRURL
	}
	if input.PRNumber > 0 {
		flow.PRNumber = input.PRNumber
	}
	if input.Attempt > 0 {
		flow.Attempt = input.Attempt
	}
	if input.CurrentDeveloperRunID != "" {
		flow.CurrentDeveloperRunID = input.CurrentDeveloperRunID
	}
	if input.CurrentReviewerRunID != "" {
		flow.CurrentReviewerRunID = input.CurrentReviewerRunID
	}
	if input.OwnerUserID != "" {
		flow.OwnerUserID = input.OwnerUserID
	}
	if input.OwnerUser != "" {
		flow.OwnerUser = input.OwnerUser
	}
	if input.ControlChannelID != "" {
		flow.ControlChannelID = input.ControlChannelID
	}
	if input.ControlPostID != "" {
		flow.ControlPostID = input.ControlPostID
	}
	if input.ActionToken != "" {
		flow.ActionToken = input.ActionToken
	}
	if input.OwnerDecision != "" {
		flow.OwnerDecision = input.OwnerDecision
	}
	if input.Summary != "" {
		flow.Summary = input.Summary
	}
	store.agentFlows[flow.FlowID] = flow
	return flow, nil
}

func (store *fakeAdminStore) ensureAgentFlows() {
	if store.agentFlows == nil {
		store.agentFlows = make(map[string]entity.AgentFlow)
	}
}

func (store *fakeAdminStore) CreateAgentRun(_ context.Context, input adminrepo.CreateAgentRunInput) (entity.AgentRun, error) {
	store.agentRun = entity.AgentRun{
		ID:                  1,
		RunID:               input.RunID,
		FlowID:              input.FlowID,
		ProfileName:         input.ProfileName,
		Role:                input.Role,
		Provider:            input.Provider,
		Owner:               input.Owner,
		Name:                input.Name,
		BaseBranch:          input.BaseBranch,
		HeadBranch:          input.HeadBranch,
		Status:              input.Status,
		KubernetesNamespace: input.KubernetesNamespace,
		JobName:             input.JobName,
		PVCName:             input.PVCName,
		Summary:             input.Summary,
	}
	store.ensureAgentRuns()
	store.agentRuns[store.agentRun.RunID] = store.agentRun
	return store.agentRun, nil
}

func (store *fakeAdminStore) GetAgentRun(_ context.Context, runID string) (entity.AgentRun, error) {
	store.ensureAgentRuns()
	if run, ok := store.agentRuns[runID]; ok {
		return run, nil
	}
	if store.agentRun.RunID == "" {
		store.agentRun = entity.AgentRun{
			ID:          1,
			RunID:       runID,
			ProfileName: "developer",
			Role:        "developer",
			Provider:    "github",
			Owner:       "codex-k8s",
			Name:        "matter-codex",
			BaseBranch:  "main",
			HeadBranch:  "matter-codex-dev-" + runID,
			Status:      "started",
		}
	}
	store.agentRuns[store.agentRun.RunID] = store.agentRun
	return store.agentRun, nil
}

func (store *fakeAdminStore) ListAgentRuns(_ context.Context, limit int) ([]entity.AgentRun, error) {
	store.ensureAgentRuns()
	runs := make([]entity.AgentRun, 0, len(store.agentRuns))
	for _, run := range store.agentRuns {
		runs = append(runs, run)
	}
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].RunID < runs[j].RunID
	})
	if limit > 0 && len(runs) > limit {
		runs = runs[:limit]
	}
	return runs, nil
}

func (store *fakeAdminStore) ListAgentRunsByFlowID(_ context.Context, flowID string) ([]entity.AgentRun, error) {
	store.ensureAgentRuns()
	var runs []entity.AgentRun
	for _, run := range store.agentRuns {
		if run.FlowID == flowID {
			runs = append(runs, run)
		}
	}
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].RunID < runs[j].RunID
	})
	return runs, nil
}

func (store *fakeAdminStore) ensureAgentRuns() {
	if store.agentRuns == nil {
		store.agentRuns = make(map[string]entity.AgentRun)
	}
}

func (store *fakeAdminStore) UpdateAgentRunArtifacts(_ context.Context, input adminrepo.UpdateAgentRunArtifactsInput) (entity.AgentRun, error) {
	store.ensureAgentRuns()
	store.updatedRunStatus = input.Status
	store.updatedPRURL = input.PRURL
	run := store.agentRun
	if stored, ok := store.agentRuns[input.RunID]; ok {
		run = stored
	}
	run.RunID = input.RunID
	run.Status = input.Status
	if input.PRURL != "" {
		run.PRURL = input.PRURL
	}
	store.agentRun = run
	store.agentRuns[input.RunID] = run
	return run, nil
}

func (store *fakeAdminStore) RecordAuditEvent(context.Context, adminrepo.AuditEventInput) error {
	store.auditRecorded = true
	store.auditCalls++
	return nil
}

type fakeChannelManager struct {
	channelName        string
	projectTeamName    string
	projectChannelName string
	projectChannelType string
}

func (manager *fakeChannelManager) ResolveMattermostUserID(_ context.Context, username string) (string, error) {
	return "user-" + strings.TrimPrefix(username, "@"), nil
}

func (manager *fakeChannelManager) EnsureRepositoryChannel(_ context.Context, _ string, channelName string, _ string) (bool, error) {
	manager.channelName = channelName
	return true, nil
}

func (manager *fakeChannelManager) EnsureProjectTeam(_ context.Context, teamName string, displayName string, _ string) (MattermostTeamBinding, bool, error) {
	manager.projectTeamName = teamName
	return MattermostTeamBinding{ID: "team-" + teamName, Name: teamName, DisplayName: displayName}, true, nil
}

func (manager *fakeChannelManager) EnsureProjectChannel(_ context.Context, teamName string, channelName string, displayName string, private bool, _ []string) (MattermostChannelBinding, bool, error) {
	manager.projectTeamName = teamName
	manager.projectChannelName = channelName
	manager.channelName = channelName
	channelType := "O"
	if private {
		channelType = "P"
	}
	manager.projectChannelType = channelType
	return MattermostChannelBinding{
		ID:          "channel-" + channelName,
		TeamID:      "team-" + teamName,
		Name:        channelName,
		DisplayName: displayName,
		Type:        channelType,
	}, true, nil
}

type fakeRoleBotManager struct {
	channelMemberTeam      string
	channelMemberChannelID string
	channelMemberUserID    string
}

func (manager *fakeRoleBotManager) EnsureRoleBot(_ context.Context, input MattermostRoleBotInput) (MattermostRoleBotBinding, error) {
	return MattermostRoleBotBinding{
		UserID:      "bot-user-" + input.Username,
		Username:    input.Username,
		DisplayName: input.DisplayName,
		Token:       "bot-token-" + input.Username,
	}, nil
}

func (manager *fakeRoleBotManager) EnsureExistingRoleBot(_ context.Context, _ string) error {
	return nil
}

func (manager *fakeRoleBotManager) EnsureProjectChannelMember(_ context.Context, teamName string, channelID string, userID string) error {
	manager.channelMemberTeam = teamName
	manager.channelMemberChannelID = channelID
	manager.channelMemberUserID = userID
	return nil
}

type fakeRepositoryProvider struct {
	checkedOwner   string
	checkedName    string
	resolvedBranch string
	createdBranch  string
	prNumber       int
	webhookOwner   string
	webhookName    string
}

func (provider *fakeRepositoryProvider) CheckRepository(_ context.Context, owner string, name string) (providerrepo.RepositoryAccess, error) {
	provider.checkedOwner = owner
	provider.checkedName = name
	return providerrepo.RepositoryAccess{
		Provider:      "github",
		Owner:         owner,
		Name:          name,
		DefaultBranch: "main",
		Private:       true,
		CanPull:       true,
		CanPush:       true,
	}, nil
}

func (provider *fakeRepositoryProvider) ResolveBranch(_ context.Context, owner string, name string, branch string) (providerrepo.BranchRef, error) {
	provider.resolvedBranch = branch
	return providerrepo.BranchRef{
		Provider: "github",
		Owner:    owner,
		Name:     name,
		Branch:   branch,
		SHA:      "1234567890abcdef",
	}, nil
}

func (provider *fakeRepositoryProvider) CreateBranch(_ context.Context, owner string, name string, branch string, _ string) (providerrepo.BranchRef, error) {
	provider.createdBranch = branch
	return providerrepo.BranchRef{
		Provider: "github",
		Owner:    owner,
		Name:     name,
		Branch:   branch,
		SHA:      "1234567890abcdef",
		Created:  true,
	}, nil
}

func (provider *fakeRepositoryProvider) PreviewPullRequest(_ context.Context, input providerrepo.PullRequestInput) (providerrepo.PullRequestPreview, error) {
	return providerrepo.PullRequestPreview{
		Provider: "github",
		Owner:    input.Owner,
		Name:     input.Name,
		Head:     input.Head,
		Base:     input.Base,
		Title:    input.Title,
		HeadSHA:  "abcdef1234567890",
		BaseSHA:  "1234567890abcdef",
	}, nil
}

func (provider *fakeRepositoryProvider) CreatePullRequest(_ context.Context, input providerrepo.PullRequestInput) (providerrepo.PullRequestSummary, error) {
	return providerrepo.PullRequestSummary{
		Provider: "github",
		Owner:    input.Owner,
		Name:     input.Name,
		Number:   10,
		Title:    input.Title,
		State:    "open",
		URL:      "https://github.example/pr/10",
		Draft:    true,
	}, nil
}

func (provider *fakeRepositoryProvider) GetPullRequest(_ context.Context, owner string, name string, number int) (providerrepo.PullRequestSummary, error) {
	provider.prNumber = number
	return providerrepo.PullRequestSummary{
		Provider:           "github",
		Owner:              owner,
		Name:               name,
		Number:             number,
		Title:              "test pr",
		State:              "open",
		URL:                "https://github.example/pr/4",
		ReviewCount:        1,
		ReviewCommentCount: 2,
		LatestReviews: []providerrepo.PullRequestReview{
			{State: "APPROVED", Author: "reviewer"},
		},
	}, nil
}

func (provider *fakeRepositoryProvider) EnsureRepositoryWebhook(_ context.Context, owner string, name string) (providerrepo.WebhookRegistration, error) {
	provider.webhookOwner = owner
	provider.webhookName = name
	return providerrepo.WebhookRegistration{
		Provider: "github",
		Owner:    owner,
		Name:     name,
		ID:       99,
		URL:      "https://matter-codex.example/github/webhook",
		Events:   []string{"pull_request", "push"},
		Created:  true,
		Active:   true,
	}, nil
}

type fakeGitHubRepositoryProvider struct {
	listAccount     providerrepo.GitHubAccountRef
	listOwner       string
	listOwnerType   string
	searchAccount   providerrepo.GitHubAccountRef
	searchOwner     string
	searchOwnerType string
	searchQuery     string
	branchesAccount providerrepo.GitHubAccountRef
	webhookAccount  providerrepo.GitHubAccountRef
	checkAccount    providerrepo.GitHubAccountRef
	candidates      []providerrepo.RepositoryCandidate
	branches        []providerrepo.BranchCandidate
}

func (provider *fakeGitHubRepositoryProvider) ListRepositories(_ context.Context, input providerrepo.RepositoryListInput) ([]providerrepo.RepositoryCandidate, error) {
	provider.listAccount = input.Account
	provider.listOwner = input.Owner
	provider.listOwnerType = input.OwnerType
	return provider.repositoryCandidates(), nil
}

func (provider *fakeGitHubRepositoryProvider) SearchRepositories(_ context.Context, input providerrepo.RepositorySearchInput) ([]providerrepo.RepositoryCandidate, error) {
	provider.searchAccount = input.Account
	provider.searchOwner = input.Owner
	provider.searchOwnerType = input.OwnerType
	provider.searchQuery = input.Query
	return provider.repositoryCandidates(), nil
}

func (provider *fakeGitHubRepositoryProvider) ListBranches(_ context.Context, account providerrepo.GitHubAccountRef, owner string, name string, _ int) ([]providerrepo.BranchCandidate, error) {
	provider.branchesAccount = account
	if len(provider.branches) > 0 {
		return provider.branches, nil
	}
	return []providerrepo.BranchCandidate{{Name: "main"}}, nil
}

func (provider *fakeGitHubRepositoryProvider) CheckRepository(_ context.Context, account providerrepo.GitHubAccountRef, owner string, name string) (providerrepo.RepositoryAccess, error) {
	provider.checkAccount = account
	return providerrepo.RepositoryAccess{
		Provider:      "github",
		Owner:         owner,
		Name:          name,
		DefaultBranch: "main",
		Private:       true,
		CanPull:       true,
		CanPush:       true,
	}, nil
}

func (provider *fakeGitHubRepositoryProvider) EnsureRepositoryWebhook(_ context.Context, account providerrepo.GitHubAccountRef, owner string, name string) (providerrepo.WebhookRegistration, error) {
	provider.webhookAccount = account
	return providerrepo.WebhookRegistration{
		Provider: "github",
		Owner:    owner,
		Name:     name,
		ID:       101,
		URL:      "https://matter-codex.example/github/webhook",
		Events:   []string{"pull_request", "push"},
		Created:  true,
		Active:   true,
	}, nil
}

func (provider *fakeGitHubRepositoryProvider) repositoryCandidates() []providerrepo.RepositoryCandidate {
	if len(provider.candidates) > 0 {
		return provider.candidates
	}
	return []providerrepo.RepositoryCandidate{{
		Provider:      "github",
		Owner:         "codex-k8s",
		Name:          "matter-codex",
		FullName:      "codex-k8s/matter-codex",
		DefaultBranch: "main",
		Private:       true,
		Description:   "matter-codex",
	}}
}
