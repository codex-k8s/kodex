package admin

import (
	"context"
	"errors"
	"fmt"
	"time"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	securityrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/security"
	"github.com/jackc/pgx/v5"
)

var _ securityrepo.Repository = (*Repository)(nil)
var _ securityrepo.InteractionResourceAdmissionRepository = (*Repository)(nil)
var _ securityrepo.ClusterAdminBindingRepository = (*Repository)(nil)
var _ securityrepo.CapabilityCleanupRepository = (*Repository)(nil)

func (repo *Repository) IssueInteractionCapability(ctx context.Context, input securityrepo.IssueCapabilityInput) error {
	if _, err := repo.pool.Exec(ctx, query("interaction_capabilities__insert.sql"),
		input.TokenHash,
		input.Kind,
		input.Operation,
		input.ResourceType,
		input.ResourceID,
		input.ChannelID,
		input.PostBinding,
		input.ActorUserID,
		input.ActorUserName,
		input.InstallationScope,
		input.WorkspaceScope,
		input.SessionScope,
		input.ContextHash,
		input.IssuedAt,
		input.ExpiresAt,
	); err != nil {
		return fmt.Errorf("issue interaction capability: %w", err)
	}
	return nil
}

func (repo *Repository) ConsumeInteractionCapability(ctx context.Context, input securityrepo.ConsumeCapabilityInput) (securityrepo.Capability, error) {
	var capability securityrepo.Capability
	var consumedAt time.Time
	err := repo.pool.QueryRow(ctx, query("interaction_capabilities__consume.sql"),
		input.TokenHash,
		input.Kind,
		input.Operation,
		input.ResourceType,
		input.ResourceID,
		input.ChannelID,
		input.PostBinding,
		input.ActorUserID,
		input.ContextHash,
		input.Now,
		input.Now,
	).Scan(
		&capability.Kind,
		&capability.Operation,
		&capability.ResourceType,
		&capability.ResourceID,
		&capability.ChannelID,
		&capability.PostBinding,
		&capability.ActorUserID,
		&capability.ActorUserName,
		&capability.InstallationScope,
		&capability.WorkspaceScope,
		&capability.SessionScope,
		&capability.IssuedAt,
		&capability.ExpiresAt,
		&consumedAt,
	)
	if err == nil {
		capability.ConsumedAt = consumedAt
		return capability, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return securityrepo.Capability{}, fmt.Errorf("consume interaction capability: %w", err)
	}

	var status string
	var expiresAt time.Time
	if stateErr := repo.pool.QueryRow(ctx, query("interaction_capabilities__state.sql"), input.TokenHash).Scan(&status, &expiresAt); stateErr != nil {
		if errors.Is(stateErr, pgx.ErrNoRows) {
			return securityrepo.Capability{}, securityrepo.ErrCapabilityNotFound
		}
		return securityrepo.Capability{}, fmt.Errorf("read interaction capability state: %w", stateErr)
	}
	if status == "consumed" {
		return securityrepo.Capability{}, securityrepo.ErrCapabilityConsumed
	}
	if !expiresAt.After(input.Now) {
		return securityrepo.Capability{}, securityrepo.ErrCapabilityExpired
	}
	return securityrepo.Capability{}, securityrepo.ErrCapabilityBinding
}

func (repo *Repository) AdmitInteractionResource(ctx context.Context, input securityrepo.InteractionResourceAdmissionInput) (bool, error) {
	var allowed bool
	if err := repo.pool.QueryRow(ctx, query("interaction_admission__resource.sql"),
		input.ActionKey,
		input.Operation,
		input.ResourceType,
		input.ResourceID,
		input.ActorUserID,
		input.ChannelID,
		input.PostID,
		input.Installation,
		input.Workspace,
		input.Session,
	).Scan(&allowed); err != nil {
		return false, fmt.Errorf("read interaction resource admission: %w", err)
	}
	return allowed, nil
}

func (repo *Repository) CleanupInteractionCapabilities(ctx context.Context, input securityrepo.CapabilityCleanupInput) (int64, error) {
	limit := input.Limit
	if limit <= 0 || limit > 10000 {
		return 0, fmt.Errorf("interaction capability cleanup limit is invalid")
	}
	command, err := repo.pool.Exec(ctx, query("interaction_capabilities__cleanup.sql"), input.DeleteBefore, limit)
	if err != nil {
		return 0, fmt.Errorf("cleanup interaction capabilities: %w", err)
	}
	return command.RowsAffected(), nil
}

func (repo *Repository) AdmitExistingClusterAdmin(ctx context.Context, input securityrepo.ClusterAdminAdmissionInput) (bool, error) {
	var allowed bool
	var err error
	switch input.SubjectType {
	case "agent_profile":
		err = repo.pool.QueryRow(ctx, query("cluster_admin_admission__profile.sql"), input.SubjectKey, input.ProfileName).Scan(&allowed)
	case "agent_role":
		err = repo.pool.QueryRow(ctx, query("cluster_admin_admission__role.sql"), input.SubjectKey, input.ProjectID, input.ProfileName).Scan(&allowed)
	default:
		allowed = false
	}
	if err != nil {
		return false, fmt.Errorf("read cluster-admin admission: %w", err)
	}
	outcome := "denied"
	if allowed {
		outcome = "allowed"
	}
	auditErr := repo.RecordAuditEvent(ctx, adminrepo.AuditEventInput{
		EventType:    "cluster_admin.admission." + outcome,
		ActorUserID:  input.ActorUserID,
		ActorUser:    input.ActorUser,
		ResourceType: input.SubjectType,
		ResourceName: input.ProfileName,
		Summary:      input.Operation + ": " + outcome,
	})
	if auditErr != nil {
		return false, fmt.Errorf("record cluster-admin admission audit: %w", auditErr)
	}
	return allowed, nil
}

func (repo *Repository) AdmitExistingClusterAdminBinding(ctx context.Context, input securityrepo.ClusterAdminBindingInput) (bool, error) {
	var allowed bool
	if err := repo.pool.QueryRow(ctx, query("cluster_admin_binding__admit.sql"), input.RoleID, input.ProjectID, input.ChatID, input.ChatSlug).Scan(&allowed); err != nil {
		return false, fmt.Errorf("read cluster-admin binding admission: %w", err)
	}
	outcome := "denied"
	if allowed {
		outcome = "allowed"
	}
	if err := repo.RecordAuditEvent(ctx, adminrepo.AuditEventInput{
		EventType:    "cluster_admin.binding." + outcome,
		ActorUserID:  input.ActorUserID,
		ActorUser:    input.ActorUser,
		ResourceType: "agent_role_chat_binding",
		ResourceName: fmt.Sprintf("%d:%s", input.RoleID, input.ChatSlug),
		Summary:      input.Operation + ": " + outcome,
	}); err != nil {
		return false, fmt.Errorf("record cluster-admin binding audit: %w", err)
	}
	return allowed, nil
}
