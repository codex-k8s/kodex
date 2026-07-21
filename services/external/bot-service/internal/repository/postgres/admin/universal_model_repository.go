package admin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	instructionsrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/instructions"
	workspacesrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/workspaces"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

var _ workspacesrepo.Repository = (*Repository)(nil)
var _ instructionsrepo.Repository = (*Repository)(nil)

func (repo *Repository) UpsertWorkspace(ctx context.Context, input workspacesrepo.UpsertWorkspaceInput) (workspacesrepo.UpsertWorkspaceResult, error) {
	tx, err := repo.db.Begin(ctx)
	if err != nil {
		return workspacesrepo.UpsertWorkspaceResult{}, fmt.Errorf("begin workspace upsert: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	legacy, created, err := upsertLegacyProject(ctx, tx, adminrepo.UpsertProjectInput{
		Name: input.Name, Slug: input.Slug, MattermostTeamID: input.MattermostTeamID,
		GitHubAccountName: input.GitHubAccountName, GitHubOwner: input.GitHubOwner,
		GitHubOwnerType: input.GitHubOwnerType, Description: input.Description,
		AdvancedSettings: input.AdvancedSettings,
	})
	if err != nil {
		return workspacesrepo.UpsertWorkspaceResult{}, err
	}
	workspace, err := scanWorkspace(tx.QueryRow(ctx, query("universal_workspaces__upsert.sql"),
		legacy.ID, legacy.Name, legacy.Slug, legacy.Description, legacy.MattermostTeamID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return workspacesrepo.UpsertWorkspaceResult{}, workspacesrepo.ErrManagedByGit
	}
	if err != nil {
		return workspacesrepo.UpsertWorkspaceResult{}, fmt.Errorf("upsert workspace projection: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return workspacesrepo.UpsertWorkspaceResult{}, fmt.Errorf("commit workspace upsert: %w", err)
	}
	return workspacesrepo.UpsertWorkspaceResult{Workspace: workspace, Legacy: legacy, Created: created}, nil
}

func upsertLegacyProject(ctx context.Context, db repositoryDB, input adminrepo.UpsertProjectInput) (entity.Project, bool, error) {
	item, created, err := scanProjectWithCreated(db.QueryRow(ctx, query("projects__upsert.sql"),
		input.Name,
		input.Slug,
		input.MattermostTeamID,
		input.GitHubAccountName,
		input.GitHubOwner,
		input.GitHubOwnerType,
		input.Description,
		input.AdvancedSettings,
	))
	if err != nil {
		return entity.Project{}, false, fmt.Errorf("upsert legacy project projection: %w", err)
	}
	return item, created, nil
}

func (repo *Repository) GetWorkspaceByLegacyProjectID(ctx context.Context, legacyProjectID int64) (entity.Workspace, error) {
	item, err := scanWorkspace(repo.db.QueryRow(ctx, query("universal_workspaces__get_by_legacy.sql"), legacyProjectID))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.Workspace{}, adminrepo.ErrNotFound
	}
	if err != nil {
		return entity.Workspace{}, fmt.Errorf("get workspace by legacy project: %w", err)
	}
	return item, nil
}

func (repo *Repository) UpsertRoom(ctx context.Context, input workspacesrepo.UpsertRoomInput) (workspacesrepo.UpsertRoomResult, error) {
	legacy, created, err := repo.CreateChat(ctx, adminrepo.CreateChatInput{
		ProjectID: input.ProjectID, MattermostChannelID: input.MattermostChannelID,
		Name: input.Name, Slug: input.Slug, Description: input.Description,
		ChatType: input.RoomType, RootGitHubIssue: input.RootGitHubIssue,
		WorkPolicy: input.WorkPolicy, Settings: input.Settings, SystemPurpose: input.SystemPurpose,
		RoleIDs: input.RoleIDs, RepositoryIDs: input.RepositoryIDs,
	})
	if err != nil {
		return workspacesrepo.UpsertRoomResult{}, err
	}
	room, err := repo.GetRoomByLegacyChatID(ctx, legacy.ID)
	if err != nil {
		return workspacesrepo.UpsertRoomResult{}, err
	}
	return workspacesrepo.UpsertRoomResult{Room: room, Legacy: legacy, Created: created}, nil
}

func (repo *Repository) GetRoomByLegacyChatID(ctx context.Context, legacyChatID int64) (entity.Room, error) {
	item, err := scanRoom(repo.db.QueryRow(ctx, query("universal_rooms__get_by_legacy.sql"), legacyChatID))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.Room{}, adminrepo.ErrNotFound
	}
	if err != nil {
		return entity.Room{}, fmt.Errorf("get room by legacy chat: %w", err)
	}
	return item, nil
}

func (repo *Repository) UpsertAgent(ctx context.Context, input instructionsrepo.UpsertAgentInput) (instructionsrepo.UpsertAgentResult, error) {
	legacy, created, err := repo.upsertAgentRoleWithActor(ctx, adminrepo.UpsertAgentRoleInput{
		ProjectID: input.ProjectID, Name: input.Name, RoleType: input.RoleType,
		Description: input.Description, PromptTemplate: input.PromptTemplate, PromptMode: input.PromptMode,
		GitHubAccountName: input.GitHubAccountName, OpenAIAccountName: input.OpenAIAccountName,
		KubernetesAccess: input.KubernetesAccess, SandboxMode: input.SandboxMode,
		ConfigOverlay: input.ConfigOverlay, AdvancedSettings: input.AdvancedSettings,
		Enabled: input.Enabled, BotIdentity: input.BotIdentity,
	}, input.ActorRef)
	if err != nil {
		return instructionsrepo.UpsertAgentResult{}, err
	}
	snapshot, err := repo.GetAgentInstructionSnapshot(ctx, legacy.ID)
	if err != nil {
		return instructionsrepo.UpsertAgentResult{}, err
	}
	return instructionsrepo.UpsertAgentResult{Snapshot: snapshot, Legacy: legacy, Created: created}, nil
}

func (repo *Repository) upsertAgentRoleWithActor(ctx context.Context, input adminrepo.UpsertAgentRoleInput, actorRef string) (entity.AgentRole, bool, error) {
	if strings.EqualFold(strings.TrimSpace(input.KubernetesAccess), "cluster-admin") {
		return repo.upsertFrozenClusterAdminRole(ctx, input, actorRef)
	}
	tx, err := repo.db.Begin(ctx)
	if err != nil {
		return entity.AgentRole{}, false, fmt.Errorf("begin agent upsert: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	item, created, err := scanAgentRoleWithCreated(tx.QueryRow(ctx, query("agent_roles__upsert.sql"),
		input.ProjectID, input.Name, input.RoleType, input.Description, input.PromptTemplate,
		input.PromptMode, input.GitHubAccountName, input.OpenAIAccountName,
		input.KubernetesAccess, input.SandboxMode, input.ConfigOverlay,
		input.AdvancedSettings, input.Enabled, input.BotIdentity,
	))
	if err != nil {
		return entity.AgentRole{}, false, fmt.Errorf("upsert legacy agent role projection: %w", err)
	}
	if err := syncUniversalAgent(ctx, tx, item, actorRef); err != nil {
		return entity.AgentRole{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.AgentRole{}, false, fmt.Errorf("commit agent upsert: %w", err)
	}
	return item, created, nil
}

func syncUniversalAgent(ctx context.Context, tx pgx.Tx, role entity.AgentRole, actorRef string) error {
	status := "disabled"
	if role.Enabled {
		status = "active"
	}
	var roleDefinitionID int64
	err := tx.QueryRow(ctx, query("universal_role_definitions__upsert.sql"),
		role.ID, role.Name, role.RoleType, role.Description, role.PromptMode, status,
	).Scan(&roleDefinitionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return instructionsrepo.ErrManagedByGit
	}
	if err != nil {
		return fmt.Errorf("upsert role definition projection: %w", err)
	}
	_ = roleDefinitionID

	instructionSetID, err := resolveInstructionSet(ctx, tx, role, actorRef, status)
	if err != nil {
		return err
	}
	var agentID int64
	if err := tx.QueryRow(ctx, query("universal_agents__upsert.sql"),
		role.ID, nullablePositiveID(instructionSetID), role.Name, status,
	).Scan(&agentID); errors.Is(err, pgx.ErrNoRows) {
		return instructionsrepo.ErrManagedByGit
	} else if err != nil {
		return fmt.Errorf("upsert agent projection: %w", err)
	}
	if _, err := tx.Exec(ctx, query("universal_agent_assignments__workspace_upsert.sql"), role.ID, role.ProjectID, role.Enabled); err != nil {
		return fmt.Errorf("upsert workspace agent assignment: %w", err)
	}
	return nil
}

func resolveInstructionSet(ctx context.Context, tx pgx.Tx, role entity.AgentRole, actorRef string, status string) (int64, error) {
	var instructionSetID int64
	var managedBy string
	var recordVersion int64
	err := tx.QueryRow(ctx, query("universal_instruction_sets__lock.sql"), role.ID).Scan(&instructionSetID, &managedBy, &recordVersion)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("lock instruction set: %w", err)
	}
	if err == nil && managedBy == string(entity.ConfigurationOwnerGit) {
		return 0, instructionsrepo.ErrManagedByGit
	}
	if strings.TrimSpace(role.PromptTemplate) == "" {
		return 0, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.QueryRow(ctx, query("universal_instruction_sets__insert.sql"), role.ID, role.Name, status).Scan(&instructionSetID); err != nil {
			return 0, fmt.Errorf("insert instruction set: %w", err)
		}
		recordVersion = 1
	}
	digest := sha256.Sum256([]byte(role.PromptTemplate))
	var existingVersionID int64
	var existingVersion int64
	var existingDigest []byte
	err = tx.QueryRow(ctx, query("universal_instruction_versions__current.sql"), instructionSetID).Scan(&existingVersionID, &existingVersion, &existingDigest)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("read current instruction version: %w", err)
	}
	if err == nil && bytes.Equal(existingDigest, digest[:]) {
		if err := setCurrentInstructionVersion(ctx, tx, instructionSetID, existingVersionID, role, status, existingVersion, recordVersion); err != nil {
			return 0, err
		}
		return instructionSetID, nil
	}
	var versionID int64
	var version int64
	if err := tx.QueryRow(ctx, query("universal_instruction_versions__insert.sql"),
		instructionSetID, role.PromptTemplate, digest[:], normalizedRepositoryActor(actorRef),
	).Scan(&versionID, &version); err != nil {
		return 0, fmt.Errorf("publish instruction version: %w", err)
	}
	if err := setCurrentInstructionVersion(ctx, tx, instructionSetID, versionID, role, status, version, recordVersion); err != nil {
		return 0, err
	}
	return instructionSetID, nil
}

func setCurrentInstructionVersion(
	ctx context.Context,
	tx pgx.Tx,
	instructionSetID int64,
	versionID int64,
	role entity.AgentRole,
	status string,
	version int64,
	recordVersion int64,
) error {
	tag, err := tx.Exec(ctx, query("universal_instruction_sets__set_current.sql"),
		instructionSetID,
		versionID,
		role.Name,
		status,
		fmt.Sprintf("legacy-agent-role:%d:version:%d", role.ID, version),
		recordVersion,
	)
	if err != nil {
		return fmt.Errorf("set current instruction version: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return instructionsrepo.ErrInstructionWriteConflict
	}
	return nil
}

func (repo *Repository) GetAgentInstructionSnapshot(ctx context.Context, legacyAgentRoleID int64) (entity.AgentInstructionSnapshot, error) {
	snapshot, _, err := scanAgentInstructionSnapshot(repo.db.QueryRow(ctx, query("universal_agent_snapshots__get.sql"), legacyAgentRoleID))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.AgentInstructionSnapshot{}, adminrepo.ErrNotFound
	}
	if err != nil {
		return entity.AgentInstructionSnapshot{}, fmt.Errorf("get agent instruction snapshot: %w", err)
	}
	return snapshot, nil
}

func (repo *Repository) DetachInstructionSet(ctx context.Context, input instructionsrepo.DetachInstructionSetInput) (entity.AgentInstructionSnapshot, error) {
	tx, err := repo.db.Begin(ctx)
	if err != nil {
		return entity.AgentInstructionSnapshot{}, fmt.Errorf("begin instruction set detach: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var detachedCount int64
	if err := tx.QueryRow(ctx, query("universal_instruction_sets__detach.sql"), input.LegacyAgentRoleID, normalizedRepositoryActor(input.ActorRef)).Scan(&detachedCount); err != nil {
		return entity.AgentInstructionSnapshot{}, fmt.Errorf("detach instruction set: %w", err)
	}
	if detachedCount > 0 {
		if _, err := tx.Exec(ctx, query("audit_events__insert.sql"),
			"instruction_set.detached", "", normalizedRepositoryActor(input.ActorRef),
			"instruction_set", fmt.Sprintf("legacy-agent-role:%d", input.LegacyAgentRoleID),
			"instruction set detached from git management",
		); err != nil {
			return entity.AgentInstructionSnapshot{}, fmt.Errorf("audit instruction set detach: %w", err)
		}
	}
	snapshot, hasInstructionSet, err := scanAgentInstructionSnapshot(tx.QueryRow(ctx, query("universal_agent_snapshots__get.sql"), input.LegacyAgentRoleID))
	if err != nil {
		return entity.AgentInstructionSnapshot{}, fmt.Errorf("read detached instruction set: %w", err)
	}
	if !hasInstructionSet {
		return entity.AgentInstructionSnapshot{}, adminrepo.ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.AgentInstructionSnapshot{}, fmt.Errorf("commit instruction set detach: %w", err)
	}
	return snapshot, nil
}

func normalizedRepositoryActor(actorRef string) string {
	actorRef = strings.TrimSpace(actorRef)
	if actorRef == "" {
		return "server-owned-writer"
	}
	if len(actorRef) > 160 {
		return actorRef[:160]
	}
	return actorRef
}

func nullablePositiveID(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}

func scanWorkspace(row pgx.Row) (entity.Workspace, error) {
	var item entity.Workspace
	err := row.Scan(
		&item.ID, &item.OrganizationScope, &item.LegacyProjectID, &item.Name, &item.Slug,
		&item.Description, &item.MattermostTeamID, &item.Status, &item.ManagedBy,
		&item.SourceRevision, &item.Provenance, &item.RecordVersion, &item.CreatedAt, &item.UpdatedAt,
	)
	return item, err
}

func scanRoom(row pgx.Row) (entity.Room, error) {
	var item entity.Room
	err := row.Scan(
		&item.ID, &item.OrganizationScope, &item.WorkspaceID, &item.LegacyChatID,
		&item.Name, &item.Slug, &item.Description, &item.RoomType, &item.Purpose,
		&item.WorkPolicy, &item.MattermostChannelID, &item.Status, &item.ManagedBy,
		&item.SourceRevision, &item.Provenance, &item.RecordVersion, &item.CreatedAt, &item.UpdatedAt,
	)
	return item, err
}

func scanAgentInstructionSnapshot(row pgx.Row) (entity.AgentInstructionSnapshot, bool, error) {
	var snapshot entity.AgentInstructionSnapshot
	var hasInstructionSet bool
	err := row.Scan(
		&snapshot.RoleDefinition.ID, &snapshot.RoleDefinition.OrganizationScope,
		&snapshot.RoleDefinition.LegacyAgentRoleID, &snapshot.RoleDefinition.Name,
		&snapshot.RoleDefinition.Slug, &snapshot.RoleDefinition.RoleType,
		&snapshot.RoleDefinition.Description, &snapshot.RoleDefinition.DefaultPolicy,
		&snapshot.RoleDefinition.Status, &snapshot.RoleDefinition.ManagedBy,
		&snapshot.RoleDefinition.SourceRevision, &snapshot.RoleDefinition.Provenance,
		&snapshot.RoleDefinition.RecordVersion, &snapshot.RoleDefinition.CreatedAt,
		&snapshot.RoleDefinition.UpdatedAt,
		&snapshot.Agent.ID, &snapshot.Agent.OrganizationScope, &snapshot.Agent.LegacyAgentRoleID,
		&snapshot.Agent.RoleDefinitionID, &snapshot.Agent.InstructionSetID,
		&snapshot.Agent.BotIdentityID, &snapshot.Agent.Name, &snapshot.Agent.Slug,
		&snapshot.Agent.Status, &snapshot.Agent.ManagedBy, &snapshot.Agent.SourceRevision,
		&snapshot.Agent.Provenance, &snapshot.Agent.RecordVersion, &snapshot.Agent.CreatedAt,
		&snapshot.Agent.UpdatedAt,
		&hasInstructionSet,
		&snapshot.InstructionSet.ID, &snapshot.InstructionSet.OrganizationScope,
		&snapshot.InstructionSet.Name, &snapshot.InstructionSet.Slug,
		&snapshot.InstructionSet.SourceType, &snapshot.InstructionSet.ManagedBy,
		&snapshot.InstructionSet.SourceRevision, &snapshot.InstructionSet.Provenance,
		&snapshot.InstructionSet.CurrentVersionID, &snapshot.InstructionSet.Status,
		&snapshot.InstructionSet.RecordVersion, &snapshot.InstructionSet.CreatedAt,
		&snapshot.InstructionSet.UpdatedAt,
		&snapshot.InstructionVersion.ID, &snapshot.InstructionVersion.OrganizationScope,
		&snapshot.InstructionVersion.InstructionSetID, &snapshot.InstructionVersion.Version,
		&snapshot.InstructionVersion.Markdown, &snapshot.InstructionVersion.ContentSHA256,
		&snapshot.InstructionVersion.ActorRef, &snapshot.InstructionVersion.CreatedAt,
	)
	return snapshot, hasInstructionSet, err
}
