package workspaces

import (
	"context"
	"errors"

	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
)

var ErrManagedByGit = errors.New("workspace configuration is managed by git")

type UpsertWorkspaceInput struct {
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

type UpsertWorkspaceResult struct {
	Workspace entity.Workspace
	Legacy    entity.Project
	Created   bool
}

type UpsertRoomInput struct {
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

type UpsertRoomResult struct {
	Room    entity.Room
	Legacy  entity.Chat
	Created bool
}

type Repository interface {
	UpsertWorkspace(ctx context.Context, input UpsertWorkspaceInput) (UpsertWorkspaceResult, error)
	GetWorkspaceByLegacyProjectID(ctx context.Context, legacyProjectID int64) (entity.Workspace, error)
	UpsertRoom(ctx context.Context, input UpsertRoomInput) (UpsertRoomResult, error)
	GetRoomByLegacyChatID(ctx context.Context, legacyChatID int64) (entity.Room, error)
}
