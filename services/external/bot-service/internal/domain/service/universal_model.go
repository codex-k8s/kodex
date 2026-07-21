package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	instructionsrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/instructions"
	workspacesrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/workspaces"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
)

const (
	MaxInstructionMarkdownBytes             = 64 * 1024
	MaxAgentsDialogInstructionMarkdownBytes = 3000
)

var (
	ErrUniversalModelUnavailable = errors.New("universal model repository is unavailable")
	ErrInstructionTooLarge       = errors.New("instruction markdown exceeds the byte limit")
	ErrInstructionInvalidUTF8    = errors.New("instruction markdown is not valid UTF-8")
)

type UpsertWorkspaceCommand struct {
	Name              string
	Slug              string
	MattermostTeamID  string
	GitHubAccountName string
	GitHubOwner       string
	GitHubOwnerType   string
	Description       string
	AdvancedSettings  string
	ActorRef          string
}

type UpsertRoomCommand struct {
	ProjectID           int64
	MattermostChannelID string
	Name                string
	Slug                string
	Description         string
	RoomType            string
	RootGitHubIssue     string
	WorkPolicy          string
	Settings            string
	SystemPurpose       string
	RoleIDs             []int64
	RepositoryIDs       []int64
	ActorRef            string
}

type UpsertAgentCommand struct {
	ProjectID         int64
	Name              string
	RoleType          string
	Description       string
	PromptTemplate    string
	PromptMode        string
	GitHubAccountName string
	OpenAIAccountName string
	KubernetesAccess  string
	SandboxMode       string
	ConfigOverlay     string
	AdvancedSettings  string
	Enabled           bool
	BotIdentity       string
	ActorRef          string
}

type UniversalModelService struct {
	workspaces   workspacesrepo.Repository
	instructions instructionsrepo.Repository
}

func NewUniversalModelService(repository any) *UniversalModelService {
	workspaces, workspacesOK := repository.(workspacesrepo.Repository)
	instructions, instructionsOK := repository.(instructionsrepo.Repository)
	if !workspacesOK || !instructionsOK {
		return nil
	}
	return &UniversalModelService{workspaces: workspaces, instructions: instructions}
}

func (svc *UniversalModelService) UpsertWorkspace(ctx context.Context, command UpsertWorkspaceCommand) (workspacesrepo.UpsertWorkspaceResult, error) {
	if svc == nil || svc.workspaces == nil {
		return workspacesrepo.UpsertWorkspaceResult{}, ErrUniversalModelUnavailable
	}
	result, err := svc.workspaces.UpsertWorkspace(ctx, workspacesrepo.UpsertWorkspaceInput{
		Name: command.Name, Slug: command.Slug, MattermostTeamID: command.MattermostTeamID,
		GitHubAccountName: command.GitHubAccountName, GitHubOwner: command.GitHubOwner,
		GitHubOwnerType: command.GitHubOwnerType, Description: command.Description,
		AdvancedSettings: command.AdvancedSettings, ActorRef: normalizedUniversalActor(command.ActorRef),
	})
	if err != nil {
		return workspacesrepo.UpsertWorkspaceResult{}, fmt.Errorf("upsert workspace command: %w", err)
	}
	return result, nil
}

func (svc *UniversalModelService) UpsertRoom(ctx context.Context, command UpsertRoomCommand) (workspacesrepo.UpsertRoomResult, error) {
	if svc == nil || svc.workspaces == nil {
		return workspacesrepo.UpsertRoomResult{}, ErrUniversalModelUnavailable
	}
	result, err := svc.workspaces.UpsertRoom(ctx, workspacesrepo.UpsertRoomInput{
		ProjectID: command.ProjectID, MattermostChannelID: command.MattermostChannelID,
		Name: command.Name, Slug: command.Slug, Description: command.Description,
		RoomType: command.RoomType, RootGitHubIssue: command.RootGitHubIssue,
		WorkPolicy: command.WorkPolicy, Settings: command.Settings, SystemPurpose: command.SystemPurpose,
		RoleIDs: command.RoleIDs, RepositoryIDs: command.RepositoryIDs,
		ActorRef: normalizedUniversalActor(command.ActorRef),
	})
	if err != nil {
		return workspacesrepo.UpsertRoomResult{}, fmt.Errorf("upsert room command: %w", err)
	}
	return result, nil
}

func (svc *UniversalModelService) UpsertAgent(ctx context.Context, command UpsertAgentCommand) (instructionsrepo.UpsertAgentResult, error) {
	if svc == nil || svc.instructions == nil {
		return instructionsrepo.UpsertAgentResult{}, ErrUniversalModelUnavailable
	}
	if !utf8.ValidString(command.PromptTemplate) {
		return instructionsrepo.UpsertAgentResult{}, ErrInstructionInvalidUTF8
	}
	if len(command.PromptTemplate) > MaxAgentsDialogInstructionMarkdownBytes {
		return instructionsrepo.UpsertAgentResult{}, ErrInstructionTooLarge
	}
	result, err := svc.instructions.UpsertAgent(ctx, instructionsrepo.UpsertAgentInput{
		ProjectID: command.ProjectID, Name: command.Name, RoleType: command.RoleType,
		Description: command.Description, PromptTemplate: command.PromptTemplate, PromptMode: command.PromptMode,
		GitHubAccountName: command.GitHubAccountName, OpenAIAccountName: command.OpenAIAccountName,
		KubernetesAccess: command.KubernetesAccess, SandboxMode: command.SandboxMode,
		ConfigOverlay: command.ConfigOverlay, AdvancedSettings: command.AdvancedSettings,
		Enabled: command.Enabled, BotIdentity: command.BotIdentity,
		ActorRef: normalizedUniversalActor(command.ActorRef),
	})
	if err != nil {
		return instructionsrepo.UpsertAgentResult{}, fmt.Errorf("upsert agent command: %w", err)
	}
	return result, nil
}

func (svc *UniversalModelService) GetAgentInstructionSnapshot(ctx context.Context, legacyAgentRoleID int64) (entity.AgentInstructionSnapshot, error) {
	if svc == nil || svc.instructions == nil {
		return entity.AgentInstructionSnapshot{}, ErrUniversalModelUnavailable
	}
	return svc.instructions.GetAgentInstructionSnapshot(ctx, legacyAgentRoleID)
}

func (svc *UniversalModelService) DetachInstructionSet(ctx context.Context, legacyAgentRoleID int64, actorRef string) (entity.AgentInstructionSnapshot, error) {
	if svc == nil || svc.instructions == nil {
		return entity.AgentInstructionSnapshot{}, ErrUniversalModelUnavailable
	}
	result, err := svc.instructions.DetachInstructionSet(ctx, instructionsrepo.DetachInstructionSetInput{
		LegacyAgentRoleID: legacyAgentRoleID,
		ActorRef:          normalizedUniversalActor(actorRef),
	})
	if err != nil {
		return entity.AgentInstructionSnapshot{}, fmt.Errorf("detach instruction set command: %w", err)
	}
	return result, nil
}

func normalizedUniversalActor(actorRef string) string {
	actorRef = strings.TrimSpace(actorRef)
	if actorRef == "" {
		return "server-owned-writer"
	}
	if len(actorRef) > 160 {
		return actorRef[:160]
	}
	return actorRef
}
