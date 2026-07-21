package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	instructionsrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/instructions"
	workspacesrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/workspaces"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	mattermostmodel "github.com/mattermost/mattermost/server/public/model"
)

type universalModelRepositoryStub struct {
	workspaceInput workspacesrepo.UpsertWorkspaceInput
	roomInput      workspacesrepo.UpsertRoomInput
	agentInput     instructionsrepo.UpsertAgentInput
	agentError     error
	snapshot       entity.AgentInstructionSnapshot
}

func (stub *universalModelRepositoryStub) UpsertWorkspace(_ context.Context, input workspacesrepo.UpsertWorkspaceInput) (workspacesrepo.UpsertWorkspaceResult, error) {
	stub.workspaceInput = input
	return workspacesrepo.UpsertWorkspaceResult{Legacy: entity.Project{Name: input.Name, Slug: input.Slug}}, nil
}

func (stub *universalModelRepositoryStub) GetWorkspaceByLegacyProjectID(context.Context, int64) (entity.Workspace, error) {
	return entity.Workspace{}, nil
}

func (stub *universalModelRepositoryStub) UpsertRoom(_ context.Context, input workspacesrepo.UpsertRoomInput) (workspacesrepo.UpsertRoomResult, error) {
	stub.roomInput = input
	return workspacesrepo.UpsertRoomResult{Legacy: entity.Chat{Name: input.Name, Slug: input.Slug}}, nil
}

func (stub *universalModelRepositoryStub) GetRoomByLegacyChatID(context.Context, int64) (entity.Room, error) {
	return entity.Room{}, nil
}

func (stub *universalModelRepositoryStub) UpsertAgent(_ context.Context, input instructionsrepo.UpsertAgentInput) (instructionsrepo.UpsertAgentResult, error) {
	stub.agentInput = input
	if stub.agentError != nil {
		return instructionsrepo.UpsertAgentResult{}, stub.agentError
	}
	return instructionsrepo.UpsertAgentResult{Snapshot: stub.snapshot, Legacy: entity.AgentRole{Name: input.Name}}, nil
}

func (stub *universalModelRepositoryStub) GetAgentInstructionSnapshot(context.Context, int64) (entity.AgentInstructionSnapshot, error) {
	return stub.snapshot, nil
}

func (stub *universalModelRepositoryStub) DetachInstructionSet(_ context.Context, input instructionsrepo.DetachInstructionSetInput) (entity.AgentInstructionSnapshot, error) {
	stub.snapshot.InstructionSet.ManagedBy = entity.ConfigurationOwnerUI
	stub.snapshot.InstructionSet.Provenance = input.ActorRef
	return stub.snapshot, nil
}

func TestUniversalModelCommandsKeepGitOptionalAndNormalizeActor(t *testing.T) {
	stub := &universalModelRepositoryStub{}
	svc := NewUniversalModelService(stub)
	if svc == nil {
		t.Fatal("NewUniversalModelService() вернул nil для двух узких портов")
	}
	workspace, err := svc.UpsertWorkspace(context.Background(), UpsertWorkspaceCommand{Name: "Legal", Slug: "legal"})
	if err != nil {
		t.Fatalf("UpsertWorkspace() error = %v", err)
	}
	if workspace.Legacy.Name != "Legal" || stub.workspaceInput.GitHubAccountName != "" || stub.workspaceInput.GitHubOwner != "" {
		t.Fatalf("no-Git workspace = %#v, input = %#v", workspace.Legacy, stub.workspaceInput)
	}
	if stub.workspaceInput.ActorRef != "server-owned-writer" {
		t.Fatalf("workspace actor = %q", stub.workspaceInput.ActorRef)
	}
	_, err = svc.UpsertAgent(context.Background(), UpsertAgentCommand{
		ProjectID: 1, Name: "translator", RoleType: "worker", OpenAIAccountName: "codex-primary",
		PromptTemplate: "# Инструкции\n\nРаботай без репозитория.", Enabled: true, ActorRef: "owner",
	})
	if err != nil {
		t.Fatalf("UpsertAgent() error = %v", err)
	}
	if stub.agentInput.GitHubAccountName != "" || stub.agentInput.ActorRef != "owner" {
		t.Fatalf("no-Git agent input = %#v", stub.agentInput)
	}
}

func TestUniversalModelRejectsInstructionAboveByteLimitBeforeRepository(t *testing.T) {
	exactStub := &universalModelRepositoryStub{}
	exactService := NewUniversalModelService(exactStub)
	exactMarkdown := strings.Repeat("я", MaxAgentsDialogInstructionMarkdownBytes/2)
	if _, err := exactService.UpsertAgent(context.Background(), UpsertAgentCommand{
		Name: "bounded", PromptTemplate: exactMarkdown,
	}); err != nil {
		t.Fatalf("UpsertAgent() rejected exact byte limit: %v", err)
	}
	if len(exactStub.agentInput.PromptTemplate) != MaxAgentsDialogInstructionMarkdownBytes {
		t.Fatalf("exact byte limit passed to repository = %d", len(exactStub.agentInput.PromptTemplate))
	}

	stub := &universalModelRepositoryStub{}
	svc := NewUniversalModelService(stub)
	_, err := svc.UpsertAgent(context.Background(), UpsertAgentCommand{
		Name: "oversized", PromptTemplate: exactMarkdown + "я",
	})
	if !errors.Is(err, ErrInstructionTooLarge) {
		t.Fatalf("UpsertAgent() error = %v", err)
	}
	if stub.agentInput.Name != "" {
		t.Fatalf("репозиторий вызван для oversized Markdown: %#v", stub.agentInput)
	}
	_, err = svc.UpsertAgent(context.Background(), UpsertAgentCommand{
		Name: "invalid-utf8", PromptTemplate: string([]byte{0xff}),
	})
	if !errors.Is(err, ErrInstructionInvalidUTF8) {
		t.Fatalf("UpsertAgent() invalid UTF-8 error = %v", err)
	}
}

func TestUniversalModelPreservesManagedByGitFailureAndExplicitDetach(t *testing.T) {
	stub := &universalModelRepositoryStub{
		agentError: instructionsrepo.ErrManagedByGit,
		snapshot: entity.AgentInstructionSnapshot{
			Agent:          entity.Agent{Name: "managed-agent"},
			InstructionSet: entity.InstructionSet{ID: 7, ManagedBy: entity.ConfigurationOwnerGit},
		},
	}
	svc := NewUniversalModelService(stub)
	if _, err := svc.UpsertAgent(context.Background(), UpsertAgentCommand{Name: "managed-agent"}); !errors.Is(err, instructionsrepo.ErrManagedByGit) {
		t.Fatalf("UpsertAgent() error = %v", err)
	}
	detached, err := svc.DetachInstructionSet(context.Background(), 42, "owner")
	if err != nil {
		t.Fatalf("DetachInstructionSet() error = %v", err)
	}
	if detached.InstructionSet.ManagedBy != entity.ConfigurationOwnerUI || detached.InstructionSet.Provenance != "owner" {
		t.Fatalf("detached snapshot = %#v", detached.InstructionSet)
	}
}

func TestUniversalModelMattermostCardShowsVersionAndRequiresDetach(t *testing.T) {
	store := &fakeAdminStore{
		projects: map[int64]entity.Project{1: {ID: 1, Name: "Legal"}},
		agentRoles: map[int64]entity.AgentRole{7: {
			ID: 7, ProjectID: 1, Name: "translator", RoleType: "worker", Enabled: true,
		}},
	}
	stub := &universalModelRepositoryStub{snapshot: entity.AgentInstructionSnapshot{
		Agent: entity.Agent{Name: "translator"},
		InstructionSet: entity.InstructionSet{
			ID: 11, ManagedBy: entity.ConfigurationOwnerGit,
		},
		InstructionVersion: entity.InstructionVersion{Version: 3, ContentSHA256: []byte{0xaa, 0xbb}},
	}}
	localizer := testLocalizer(t, "en")
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer: localizer, StatusService: testStatusService(localizer), Store: store,
		UniversalModel: NewUniversalModelService(stub), StorageReady: true,
	})

	card := svc.roleEntityCard(context.Background(), MenuActionCommand{
		View: menuViewRoles, Resource: menuResourceAgentRole, ID: "7",
	})
	if got := cardFieldValue(card.Fields, "Instruction version"); got != "`V3`" {
		t.Fatalf("instruction version field = %q", got)
	}
	if got := cardFieldValue(card.Fields, "Instruction SHA-256"); got != "`aabb`" {
		t.Fatalf("instruction digest field = %q", got)
	}
	edit, ok := mattermostActionByID(card.Actions, "dialogroleedit")
	if !ok || !edit.Disabled {
		t.Fatalf("Git-managed edit action = %#v", edit)
	}
	detach, ok := mattermostActionByID(card.Actions, "instructiondetach")
	if !ok || detach.Context["action"] != menuActionInstructionDetach {
		t.Fatalf("detach action = %#v", detach)
	}
	if dialog, errorText := svc.agentRoleDialog(context.Background(), MenuActionCommand{
		Resource: menuResourceAgentRole, ID: "7",
	}); dialog != nil || !strings.Contains(errorText, "read-only") {
		t.Fatalf("Git-managed edit dialog = %#v, error=%q", dialog, errorText)
	}

	result := svc.detachInstructionSetFromMenu(context.Background(), MenuActionCommand{ID: "7", UserName: "owner"})
	if !strings.Contains(result, "now managed by the UI") || stub.snapshot.InstructionSet.Provenance != "owner" {
		t.Fatalf("detach result = %q, snapshot=%#v", result, stub.snapshot.InstructionSet)
	}
}

func TestAgentRoleDialogInputEnforcesInstructionByteLimit(t *testing.T) {
	localizer := testLocalizer(t, "ru")
	svc := NewSlashCommandService(SlashCommandServiceConfig{Localizer: localizer})
	_, fieldErrors := svc.agentRoleDialogInput(map[string]any{
		dialogFieldProjectID:        "1",
		dialogFieldRole:             "translator",
		dialogFieldRoleType:         "worker",
		dialogFieldPromptMode:       "raw",
		dialogFieldPromptTemplate:   strings.Repeat("я", MaxAgentsDialogInstructionMarkdownBytes/2+1),
		dialogFieldKubernetesAccess: "read-only",
		dialogFieldSandboxMode:      "danger-full-access",
	})
	if !strings.Contains(fieldErrors[dialogFieldPromptTemplate], "3000") {
		t.Fatalf("oversized prompt error = %#v", fieldErrors)
	}
	_, fieldErrors = svc.agentRoleDialogInput(map[string]any{
		dialogFieldProjectID:        "1",
		dialogFieldRole:             "translator",
		dialogFieldRoleType:         "worker",
		dialogFieldPromptMode:       "raw",
		dialogFieldPromptTemplate:   string([]byte{0xff}),
		dialogFieldKubernetesAccess: "read-only",
		dialogFieldSandboxMode:      "danger-full-access",
	})
	if !strings.Contains(fieldErrors[dialogFieldPromptTemplate], "UTF-8") {
		t.Fatalf("invalid UTF-8 prompt error = %#v", fieldErrors)
	}
}

func TestAgentRoleDialogCreatesAndReopensExactUTF8ByteBoundary(t *testing.T) {
	exactMarkdown := strings.Repeat("я", MaxAgentsDialogInstructionMarkdownBytes/2)
	localizer := testLocalizer(t, "ru")
	store := &fakeAdminStore{
		projects: map[int64]entity.Project{1: {ID: 1, Name: "Legal"}},
		agentRoles: map[int64]entity.AgentRole{7: {
			ID: 7, ProjectID: 1, Name: "translator", RoleType: "worker",
			PromptTemplate: exactMarkdown, PromptMode: "raw", Enabled: true,
		}},
	}
	stub := &universalModelRepositoryStub{snapshot: entity.AgentInstructionSnapshot{
		RoleDefinition: entity.RoleDefinition{ManagedBy: entity.ConfigurationOwnerUI},
		Agent:          entity.Agent{ManagedBy: entity.ConfigurationOwnerUI},
		InstructionSet: entity.InstructionSet{ID: 11, ManagedBy: entity.ConfigurationOwnerUI},
	}}
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer: localizer, StatusService: testStatusService(localizer), Store: store,
		UniversalModel: NewUniversalModelService(stub), StorageReady: true,
	})

	input, fieldErrors := svc.agentRoleDialogInput(map[string]any{
		dialogFieldProjectID:        "1",
		dialogFieldRole:             "translator",
		dialogFieldRoleType:         "worker",
		dialogFieldPromptMode:       "raw",
		dialogFieldPromptTemplate:   exactMarkdown,
		dialogFieldKubernetesAccess: "read-only",
		dialogFieldSandboxMode:      "danger-full-access",
	})
	if len(fieldErrors) != 0 || input.PromptTemplate != exactMarkdown {
		t.Fatalf("create boundary input=%#v errors=%#v", input, fieldErrors)
	}

	dialog, errorText := svc.agentRoleDialog(context.Background(), MenuActionCommand{
		Resource: menuResourceAgentRole,
		ID:       "7",
	})
	if errorText != "" || dialog == nil {
		t.Fatalf("edit boundary dialog=%#v error=%q", dialog, errorText)
	}
	for _, element := range dialog.Elements {
		if element.Name != dialogFieldPromptTemplate {
			continue
		}
		if element.Default != exactMarkdown || len(element.Default) != MaxAgentsDialogInstructionMarkdownBytes {
			t.Fatalf("reopened prompt bytes=%d", len(element.Default))
		}
		if element.MaxLength != MaxAgentsDialogInstructionMarkdownBytes {
			t.Fatalf("textarea max length=%d", element.MaxLength)
		}
		store.agentRoles[7] = entity.AgentRole{
			ID: 7, ProjectID: 1, Name: "translator", RoleType: "worker",
			PromptTemplate: exactMarkdown + "я", PromptMode: "raw", Enabled: true,
		}
		oversizedDialog, oversizedError := svc.agentRoleDialog(context.Background(), MenuActionCommand{
			Resource: menuResourceAgentRole,
			ID:       "7",
		})
		if oversizedDialog != nil || !strings.Contains(oversizedError, "3000") {
			t.Fatalf("oversized reopen dialog=%#v error=%q", oversizedDialog, oversizedError)
		}
		return
	}
	t.Fatal("edit dialog не содержит поле инструкций")
}

func TestUniversalModelRussianDialogLabelsFitMattermostLimits(t *testing.T) {
	localizer := testLocalizer(t, "ru")
	svc := NewSlashCommandService(SlashCommandServiceConfig{Localizer: localizer})
	for _, messageID := range []string{
		"dialog.project.add.title",
		"dialog.project.edit.title",
		"dialog.role.add.title",
		"dialog.role.edit.title",
		"dialog.chat.create.title",
	} {
		if value := svc.t(messageID, nil); len(value) > mattermostmodel.DialogTitleMaxLength {
			t.Fatalf("title %s = %q (%d bytes), want <= %d", messageID, value, len(value), mattermostmodel.DialogTitleMaxLength)
		}
	}
	for _, messageID := range []string{"dialog.project.field.project", "dialog.role.field.prompt"} {
		if value := svc.t(messageID, nil); len(value) > mattermostmodel.DialogElementDisplayNameMaxLength {
			t.Fatalf("display name %s = %q (%d bytes), want <= %d", messageID, value, len(value), mattermostmodel.DialogElementDisplayNameMaxLength)
		}
	}
	if value := svc.t("dialog.role.field.prompt.help", nil); len(value) > mattermostmodel.DialogElementHelpTextMaxLength {
		t.Fatalf("instruction help = %q (%d bytes), want <= %d", value, len(value), mattermostmodel.DialogElementHelpTextMaxLength)
	}
}
