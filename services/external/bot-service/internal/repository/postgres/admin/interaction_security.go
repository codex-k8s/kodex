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
var _ securityrepo.AtomicDialogRepository = (*Repository)(nil)
var _ securityrepo.ClusterAdminBindingRepository = (*Repository)(nil)
var _ securityrepo.ClusterAdminRuntimeGuardRepository = (*Repository)(nil)
var _ securityrepo.ClusterAdminSecretIntegrityRepository = (*Repository)(nil)
var _ securityrepo.ClusterAdminAccountDependencyRepository = (*Repository)(nil)
var _ securityrepo.CapabilityCleanupRepository = (*Repository)(nil)

func (repo *Repository) IssueInteractionCapability(ctx context.Context, input securityrepo.IssueCapabilityInput) error {
	state := input.State
	if state == "" {
		state = securityrepo.CapabilityStateUnused
	}
	if _, err := repo.db.Exec(ctx, query("interaction_capabilities__insert.sql"),
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
	err := repo.db.QueryRow(ctx, query("interaction_capabilities__check.sql"),
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
	err := repo.db.QueryRow(ctx, query("interaction_capabilities__consume.sql"),
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

func (repo *Repository) ConsumeInteractionCapabilityWithMutation(
	ctx context.Context,
	input securityrepo.ConsumeCapabilityInput,
	mutation func(adminrepo.Repository) error,
) (securityrepo.Capability, error) {
	if mutation == nil {
		return securityrepo.Capability{}, fmt.Errorf("atomic dialog mutation is required")
	}
	tx, err := repo.db.Begin(ctx)
	if err != nil {
		return securityrepo.Capability{}, fmt.Errorf("begin atomic dialog mutation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	txRepository := newTransactionalRepository(tx)
	capability, err := txRepository.ConsumeInteractionCapability(ctx, input)
	if err != nil {
		return securityrepo.Capability{}, err
	}
	if err := mutation(txRepository); err != nil {
		return securityrepo.Capability{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return securityrepo.Capability{}, fmt.Errorf("commit atomic dialog mutation: %w", err)
	}
	return capability, nil
}

func (repo *Repository) interactionCapabilityStateError(ctx context.Context, tokenHash []byte, now time.Time) error {
	var status securityrepo.CapabilityState
	var expiresAt time.Time
	if stateErr := repo.db.QueryRow(ctx, query("interaction_capabilities__state.sql"), tokenHash).Scan(&status, &expiresAt); stateErr != nil {
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
	tx, err := repo.db.Begin(ctx)
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
	if err := repo.db.QueryRow(ctx, query("interaction_admission__resource.sql"),
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
	command, err := repo.db.Exec(ctx, query("interaction_capabilities__cleanup.sql"), input.DeleteBefore, limit)
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
		err = repo.db.QueryRow(ctx, query("cluster_admin_admission__profile.sql"), input.SubjectKey, input.ProfileName).Scan(&allowed)
	case "agent_role":
		err = repo.db.QueryRow(ctx, query("cluster_admin_admission__role.sql"), input.SubjectKey, input.ProjectID, input.ProfileName).Scan(&allowed)
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
	if err := repo.db.QueryRow(ctx, query("cluster_admin_binding__admit.sql"),
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
	return repo.withExistingClusterAdminGuard(ctx, input, false, sideEffect)
}

func (repo *Repository) WithExistingClusterAdminPersistenceGuard(ctx context.Context, input securityrepo.ClusterAdminBindingInput, sideEffect func() error) error {
	return repo.withExistingClusterAdminGuard(ctx, input, true, sideEffect)
}

func (repo *Repository) withExistingClusterAdminGuard(ctx context.Context, input securityrepo.ClusterAdminBindingInput, persistence bool, sideEffect func() error) error {
	if sideEffect == nil {
		return fmt.Errorf("cluster-admin runtime side effect is required")
	}
	tx, err := repo.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin cluster-admin runtime guard: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queryName := "cluster_admin_runtime_guard__lock.sql"
	queryArgs := []any{input.RoleID, input.ProjectID, input.ChatID, input.ChatSlug, input.MattermostChannelID}
	if strings.TrimSpace(input.SessionKey) != "" {
		queryName = "cluster_admin_runtime_guard__lock_session.sql"
		if persistence {
			queryName = "cluster_admin_runtime_guard__lock_session_persistence.sql"
		}
		queryArgs = append(queryArgs, input.SessionKey)
	}
	var allowed bool
	err = tx.QueryRow(ctx, query(queryName), queryArgs...).Scan(&allowed)
	if errors.Is(err, pgx.ErrNoRows) {
		allowed = false
		err = nil
	}
	if err != nil {
		return fmt.Errorf("lock cluster-admin runtime admission: %w", err)
	}
	if allowed {
		if err := lockClusterAdminRuntimeVariables(ctx, tx, input.RoleID); err != nil {
			return err
		}
		if err := lockClusterAdminRuntimeDependencies(ctx, tx, input.RoleID); err != nil {
			return err
		}
		err = tx.QueryRow(ctx, query(queryName), queryArgs...).Scan(&allowed)
		if errors.Is(err, pgx.ErrNoRows) {
			allowed = false
			err = nil
		}
		if err != nil {
			return fmt.Errorf("recheck cluster-admin runtime admission: %w", err)
		}
	}
	var sideEffectErr error
	if allowed {
		sideEffectErr = sideEffect()
		if errors.Is(sideEffectErr, adminrepo.ErrClusterAdminAdmissionDenied) {
			allowed = false
		}
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
		if sideEffectErr != nil {
			return sideEffectErr
		}
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit cluster-admin runtime guard: %w", err)
	}
	return sideEffectErr
}

func (repo *Repository) ListClusterAdminSecretIntegrity(ctx context.Context, roleID int64, sessionKey string) ([]securityrepo.SecretIntegrityBinding, error) {
	rows, err := repo.db.Query(ctx, query("cluster_admin_secret_integrity__list.sql"), roleID, sessionKey)
	if err != nil {
		return nil, fmt.Errorf("list cluster-admin secret integrity: %w", err)
	}
	defer rows.Close()
	var bindings []securityrepo.SecretIntegrityBinding
	for rows.Next() {
		var binding securityrepo.SecretIntegrityBinding
		if err := rows.Scan(
			&binding.Kind, &binding.SecretRef, &binding.SecretKey,
			&binding.ContentSHA256, &binding.ResourceUID, &binding.ResourceVersion,
		); err != nil {
			return nil, fmt.Errorf("scan cluster-admin secret integrity: %w", err)
		}
		bindings = append(bindings, binding)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read cluster-admin secret integrity: %w", err)
	}
	return bindings, nil
}

func lockClusterAdminRuntimeDependencies(ctx context.Context, tx pgx.Tx, roleID int64) error {
	for _, queryName := range []string{
		"cluster_admin_dependencies__lock_frozen.sql",
		"cluster_admin_openai_accounts__lock.sql",
		"cluster_admin_openai_credentials__lock.sql",
		"cluster_admin_github_accounts__lock.sql",
		"cluster_admin_github_credentials__lock.sql",
		"cluster_admin_repositories__lock.sql",
		"cluster_admin_project_repositories__lock.sql",
		"cluster_admin_chat_repositories__lock.sql",
	} {
		rows, err := tx.Query(ctx, query(queryName), roleID)
		if err != nil {
			return fmt.Errorf("lock cluster-admin runtime dependency with %s: %w", queryName, err)
		}
		for rows.Next() {
			var dependencyID int64
			if err := rows.Scan(&dependencyID); err != nil {
				rows.Close()
				return fmt.Errorf("scan cluster-admin runtime dependency with %s: %w", queryName, err)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("read cluster-admin runtime dependencies with %s: %w", queryName, err)
		}
		rows.Close()
	}
	return nil
}

func lockClusterAdminRuntimeVariables(ctx context.Context, tx pgx.Tx, roleID int64) error {
	frozenRows, err := tx.Query(ctx, query("cluster_admin_runtime_variables__lock_frozen.sql"), roleID)
	if err != nil {
		return fmt.Errorf("lock frozen cluster-admin runtime variables: %w", err)
	}
	for frozenRows.Next() {
		var variableID int64
		if err := frozenRows.Scan(&variableID); err != nil {
			frozenRows.Close()
			return fmt.Errorf("scan frozen cluster-admin runtime variable: %w", err)
		}
	}
	if err := frozenRows.Err(); err != nil {
		frozenRows.Close()
		return fmt.Errorf("read frozen cluster-admin runtime variables: %w", err)
	}
	frozenRows.Close()

	currentRows, err := tx.Query(ctx, query("cluster_admin_runtime_variables__lock_current.sql"), roleID)
	if err != nil {
		return fmt.Errorf("lock current cluster-admin runtime variables: %w", err)
	}
	for currentRows.Next() {
		var bindingID int64
		var variableID int64
		if err := currentRows.Scan(&bindingID, &variableID); err != nil {
			currentRows.Close()
			return fmt.Errorf("scan current cluster-admin runtime variable: %w", err)
		}
	}
	if err := currentRows.Err(); err != nil {
		currentRows.Close()
		return fmt.Errorf("read current cluster-admin runtime variables: %w", err)
	}
	currentRows.Close()
	return nil
}
