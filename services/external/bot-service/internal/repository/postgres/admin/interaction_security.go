package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	securityrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/security"
	"github.com/jackc/pgx/v5"
)

var _ securityrepo.Repository = (*Repository)(nil)
var _ securityrepo.InteractionResourceAdmissionRepository = (*Repository)(nil)
var _ securityrepo.ClusterAdminBindingRepository = (*Repository)(nil)
var _ securityrepo.ClusterAdminRuntimeGuardRepository = (*Repository)(nil)
var _ securityrepo.CapabilityCleanupRepository = (*Repository)(nil)

func (repo *Repository) IssueInteractionCapability(ctx context.Context, input securityrepo.IssueCapabilityInput) error {
	state := input.State
	if state == "" {
		state = securityrepo.CapabilityStateUnused
	}
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
		state,
	); err != nil {
		return fmt.Errorf("issue interaction capability: %w", err)
	}
	return nil
}

func (repo *Repository) CheckInteractionCapability(ctx context.Context, input securityrepo.ConsumeCapabilityInput) (securityrepo.Capability, error) {
	var capability securityrepo.Capability
	err := repo.pool.QueryRow(ctx, query("interaction_capabilities__check.sql"),
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
	).Scan(
		&capability.State,
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
	)
	if err == nil {
		return capability, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return securityrepo.Capability{}, fmt.Errorf("check interaction capability: %w", err)
	}
	return securityrepo.Capability{}, repo.interactionCapabilityStateError(ctx, input.TokenHash, input.Now)
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
		capability.State = securityrepo.CapabilityStateConsumed
		capability.ConsumedAt = consumedAt
		return capability, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return securityrepo.Capability{}, fmt.Errorf("consume interaction capability: %w", err)
	}

	return securityrepo.Capability{}, repo.interactionCapabilityStateError(ctx, input.TokenHash, input.Now)
}

func (repo *Repository) interactionCapabilityStateError(ctx context.Context, tokenHash []byte, now time.Time) error {
	var status securityrepo.CapabilityState
	var expiresAt time.Time
	if stateErr := repo.pool.QueryRow(ctx, query("interaction_capabilities__state.sql"), tokenHash).Scan(&status, &expiresAt); stateErr != nil {
		if errors.Is(stateErr, pgx.ErrNoRows) {
			return securityrepo.ErrCapabilityNotFound
		}
		return fmt.Errorf("read interaction capability state: %w", stateErr)
	}
	if status == securityrepo.CapabilityStateConsumed {
		return securityrepo.ErrCapabilityConsumed
	}
	if !expiresAt.After(now) {
		return securityrepo.ErrCapabilityExpired
	}
	if status == securityrepo.CapabilityStatePending || status == securityrepo.CapabilityStateRevoked {
		return securityrepo.ErrCapabilityInactive
	}
	return securityrepo.ErrCapabilityBinding
}

func (repo *Repository) TransitionInteractionCapabilities(ctx context.Context, input securityrepo.TransitionCapabilitiesInput) error {
	if len(input.TokenHashes) == 0 || input.From == "" || input.To == "" || input.From == input.To {
		return fmt.Errorf("interaction capability transition is invalid")
	}
	seen := make(map[string]struct{}, len(input.TokenHashes))
	for _, tokenHash := range input.TokenHashes {
		key := string(tokenHash)
		if len(tokenHash) == 0 {
			return fmt.Errorf("interaction capability transition contains an empty token hash")
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("interaction capability transition contains duplicate token hashes")
		}
		seen[key] = struct{}{}
	}
	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin interaction capability transition: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, query("interaction_capabilities__transition_lock.sql"), input.TokenHashes)
	if err != nil {
		return fmt.Errorf("lock interaction capabilities for transition: %w", err)
	}
	locked := 0
	for rows.Next() {
		var state securityrepo.CapabilityState
		if err := rows.Scan(&state); err != nil {
			rows.Close()
			return fmt.Errorf("scan interaction capability transition lock: %w", err)
		}
		if state != input.From {
			rows.Close()
			return fmt.Errorf("transition interaction capabilities: %w", securityrepo.ErrCapabilityInactive)
		}
		locked++
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read interaction capability transition locks: %w", err)
	}
	rows.Close()
	if locked != len(input.TokenHashes) {
		return fmt.Errorf("transition interaction capabilities: %w", securityrepo.ErrCapabilityInactive)
	}
	command, err := tx.Exec(ctx, query("interaction_capabilities__transition.sql"), input.TokenHashes, input.From, input.To)
	if err != nil {
		return fmt.Errorf("transition interaction capabilities: %w", err)
	}
	if command.RowsAffected() != int64(len(input.TokenHashes)) {
		return fmt.Errorf("transition interaction capabilities: expected %d rows, changed %d", len(input.TokenHashes), command.RowsAffected())
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit interaction capability transition: %w", err)
	}
	return nil
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
	if err := repo.pool.QueryRow(ctx, query("cluster_admin_binding__admit.sql"),
		input.RoleID, input.ProjectID, input.ChatID, input.ChatSlug, input.MattermostChannelID, input.SessionKey,
	).Scan(&allowed); err != nil {
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

func (repo *Repository) WithExistingClusterAdminRuntimeGuard(ctx context.Context, input securityrepo.ClusterAdminBindingInput, sideEffect func() error) error {
	if sideEffect == nil {
		return fmt.Errorf("cluster-admin runtime side effect is required")
	}
	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin cluster-admin runtime guard: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queryName := "cluster_admin_runtime_guard__lock.sql"
	if strings.TrimSpace(input.SessionKey) != "" {
		queryName = "cluster_admin_runtime_guard__lock_session.sql"
	}
	var allowed bool
	err = tx.QueryRow(ctx, query(queryName),
		input.RoleID, input.ProjectID, input.ChatID, input.ChatSlug, input.MattermostChannelID, input.SessionKey,
	).Scan(&allowed)
	if errors.Is(err, pgx.ErrNoRows) {
		allowed = false
		err = nil
	}
	if err != nil {
		return fmt.Errorf("lock cluster-admin runtime admission: %w", err)
	}
	outcome := "denied"
	if allowed {
		outcome = "allowed"
	}
	if _, err := tx.Exec(ctx, query("audit_events__insert.sql"),
		"cluster_admin.runtime."+outcome,
		input.ActorUserID,
		input.ActorUser,
		"agent_role_runtime",
		fmt.Sprintf("%d:%d:%s", input.RoleID, input.ChatID, input.SessionKey),
		input.Operation+": "+outcome,
	); err != nil {
		return fmt.Errorf("record cluster-admin runtime guard audit: %w", err)
	}
	if !allowed {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit denied cluster-admin runtime guard audit: %w", err)
		}
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	sideEffectErr := sideEffect()
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit cluster-admin runtime guard: %w", err)
	}
	return sideEffectErr
}
