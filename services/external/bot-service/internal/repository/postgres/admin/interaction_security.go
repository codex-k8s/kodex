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
