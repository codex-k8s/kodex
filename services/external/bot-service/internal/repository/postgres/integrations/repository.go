package integrations

import (
	"context"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	domain "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/integrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed sql/*.sql
var queryFiles embed.FS

type repositoryDB interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Begin(context.Context) (pgx.Tx, error)
}

// Repository реализует PostgreSQL-владение домена интеграций.
type Repository struct {
	db repositoryDB
}

var _ domain.Repository = (*Repository)(nil)
var _ domain.RecordingStore = (*Repository)(nil)

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{db: pool}
}

func newTransactionalRepository(tx pgx.Tx) *Repository {
	return &Repository{db: tx}
}

// NewTransactionalRepository связывает integration repository с уже открытой
// транзакцией одноразового Mattermost callback.
func NewTransactionalRepository(tx pgx.Tx) *Repository {
	return newTransactionalRepository(tx)
}

var queryCache sync.Map

func query(name string) string {
	if value, ok := queryCache.Load(name); ok {
		return value.(string)
	}
	value, err := queryFiles.ReadFile("sql/" + name)
	if err != nil {
		panic("integration SQL is missing: " + name)
	}
	text := string(value)
	queryCache.Store(name, text)
	return text
}

func (repo *Repository) ListCatalog(ctx context.Context, session domain.SessionContext, now time.Time) ([]domain.CatalogEntry, error) {
	rows, err := repo.db.Query(ctx, query("catalog__list.sql"),
		session.SessionID, session.SessionKey, session.TurnID, session.ProjectID, session.ChatID, session.RoleID,
		session.MattermostChannelID, session.MattermostRootPostID, session.SessionTokenSecretRef, now,
		session.InstallationScope, session.WorkspaceScope, session.SubjectKind, session.SubjectRef,
	)
	if err != nil {
		return nil, fmt.Errorf("list integration catalog: %w", err)
	}
	defer rows.Close()
	entries := make([]domain.CatalogEntry, 0, 1)
	for rows.Next() {
		var entry domain.CatalogEntry
		if err := rows.Scan(&entry.CapabilityKey, &entry.Version); err != nil {
			return nil, fmt.Errorf("scan integration catalog: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read integration catalog: %w", err)
	}
	return entries, nil
}

type existingBinding struct {
	invocationID        int64
	argumentsHash       string
	approvalBindingHash string
	binding             domain.Binding
	subjectKind         string
	subjectRef          string
	installationScope   string
	workspaceScope      string
	sessionScope        string
}

type auditMetadata struct {
	InvocationID  string `json:"invocation_id"`
	ApprovalID    string `json:"approval_id,omitempty"`
	ArgumentsHash string `json:"arguments_sha256,omitempty"`
	ExecutionID   string `json:"execution_id,omitempty"`
}

func (repo *Repository) CreateOrReplayInvocation(ctx context.Context, input domain.CreateInvocationInput) (domain.Invocation, bool, error) {
	tx, err := repo.db.Begin(ctx)
	if err != nil {
		return domain.Invocation{}, false, fmt.Errorf("begin integration invocation: %w", err)
	}
	defer rollback(ctx, tx)
	txRepo := newTransactionalRepository(tx)
	lockKey := fmt.Sprintf("%d\x1f%s\x1f%s", input.Session.SessionID, input.CapabilityKey, input.IdempotencyKey)
	if err := tx.QueryRow(ctx, query("invocation__idempotency_lock.sql"), lockKey).Scan(new(any)); err != nil {
		return domain.Invocation{}, false, fmt.Errorf("lock integration idempotency key: %w", err)
	}
	binding, err := authorizeBinding(ctx, tx, input)
	if err != nil {
		return domain.Invocation{}, false, err
	}
	existing, found, err := loadExistingBinding(ctx, tx, input.Session.SessionID, binding.CapabilityID, input.IdempotencyKey)
	if err != nil {
		return domain.Invocation{}, false, err
	}
	if found {
		if !sameReplayBinding(existing, input, binding) {
			return domain.Invocation{}, false, domain.ErrIdempotencyConflict
		}
		invocation, err := txRepo.invocationByID(ctx, existing.invocationID)
		if err != nil {
			return domain.Invocation{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.Invocation{}, false, fmt.Errorf("commit integration replay: %w", err)
		}
		return invocation, false, nil
	}
	approvalBindingHash, err := domain.ApprovalBindingHash(input.Session, binding, input.InvocationPublicID, input.ArgumentsHash)
	if err != nil {
		return domain.Invocation{}, false, err
	}
	argumentsJSON, err := json.Marshal(input.Arguments)
	if err != nil {
		return domain.Invocation{}, false, fmt.Errorf("encode integration arguments: %w", err)
	}
	argumentsHashBytes, err := decodeHash(input.ArgumentsHash)
	if err != nil {
		return domain.Invocation{}, false, err
	}
	approvalHashBytes, err := decodeHash(approvalBindingHash)
	if err != nil {
		return domain.Invocation{}, false, err
	}
	var invocationID int64
	if err := tx.QueryRow(ctx, query("invocation__insert.sql"),
		input.InvocationPublicID, input.Session.SessionID, input.Session.TurnID,
		input.Session.ProjectID, input.Session.ChatID, input.Session.RoleID,
		input.Session.SubjectKind, input.Session.SubjectRef, input.Session.InstallationScope,
		input.Session.WorkspaceScope, input.Session.SessionKey, input.Session.SessionTokenSecretRef,
		binding.CapabilityID, binding.CapabilityRevision, binding.ConnectionID, binding.ConnectionRevision,
		binding.GrantID, binding.GrantRevision, input.IdempotencyKey, argumentsJSON,
		argumentsHashBytes, approvalHashBytes, input.CorrelationID, input.Now,
	).Scan(&invocationID); err != nil {
		return domain.Invocation{}, false, fmt.Errorf("insert integration invocation: %w", err)
	}
	var approvalID int64
	if err := tx.QueryRow(ctx, query("approval__insert.sql"),
		input.ApprovalPublicID, invocationID, approvalHashBytes, argumentsJSON,
		input.Session.ApproverUserID, input.Session.ApproverUserName, input.ApprovalExpiresAt,
		input.Session.MattermostChannelID, input.Session.MattermostRootPostID, input.Now,
	).Scan(&approvalID); err != nil {
		return domain.Invocation{}, false, fmt.Errorf("insert integration approval: %w", err)
	}
	if err := txRepo.appendAudit(ctx, auditInput{
		EventType: "integration.invocation.requested", ActorUserID: input.Session.SubjectRef, ActorUser: "agent_session",
		ResourceType: "tool_invocation", ResourceName: input.InvocationPublicID,
		Summary: "Запрошено согласование опасной capability.", CorrelationID: input.CorrelationID,
		InstallationScope: input.Session.InstallationScope, WorkspaceScope: input.Session.WorkspaceScope,
		SessionScope: input.Session.SessionKey, Outcome: "pending", ReasonCode: "approval.required",
		Metadata: auditMetadata{InvocationID: input.InvocationPublicID, ApprovalID: input.ApprovalPublicID, ArgumentsHash: input.ArgumentsHash},
		Now:      input.Now,
	}); err != nil {
		return domain.Invocation{}, false, err
	}
	invocation, err := txRepo.invocationByID(ctx, invocationID)
	if err != nil {
		return domain.Invocation{}, false, err
	}
	if invocation.ApprovalID != approvalID {
		return domain.Invocation{}, false, fmt.Errorf("integration approval identity mismatch")
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Invocation{}, false, fmt.Errorf("commit integration invocation: %w", err)
	}
	return invocation, true, nil
}

func authorizeBinding(ctx context.Context, tx pgx.Tx, input domain.CreateInvocationInput) (domain.Binding, error) {
	var binding domain.Binding
	err := tx.QueryRow(ctx, query("binding__authorize.sql"),
		input.Session.SessionID, input.Session.SessionKey, input.Session.ProjectID, input.Session.ChatID,
		input.Session.RoleID, input.Session.MattermostChannelID, input.Session.MattermostRootPostID,
		input.Session.SessionTokenSecretRef, input.Session.TurnID, input.Now,
		input.ConnectionPublicID, input.Session.InstallationScope, input.Session.WorkspaceScope,
		input.CapabilityKey, input.Session.SubjectKind, input.Session.SubjectRef,
		input.Arguments.Namespace, input.Arguments.WorkloadKind, input.Arguments.WorkloadName,
	).Scan(
		&binding.CapabilityID, &binding.CapabilityPublicID, &binding.CapabilityKey,
		&binding.CapabilityVersion, &binding.CapabilityRevision,
		&binding.ConnectionID, &binding.ConnectionPublicID, &binding.ConnectionRevision,
		&binding.GrantID, &binding.GrantPublicID, &binding.GrantRevision,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Binding{}, domain.ErrUnauthorized
	}
	if err != nil {
		return domain.Binding{}, fmt.Errorf("authorize integration binding: %w", err)
	}
	return binding, nil
}

func loadExistingBinding(ctx context.Context, tx pgx.Tx, sessionID int64, capabilityID int64, idempotencyKey string) (existingBinding, bool, error) {
	var item existingBinding
	err := tx.QueryRow(ctx, query("invocation__existing_binding.sql"), sessionID, capabilityID, idempotencyKey).Scan(
		&item.invocationID, &item.argumentsHash, &item.approvalBindingHash,
		&item.binding.CapabilityID, &item.binding.CapabilityRevision,
		&item.binding.ConnectionID, &item.binding.ConnectionRevision,
		&item.binding.GrantID, &item.binding.GrantRevision,
		&item.subjectKind, &item.subjectRef, &item.installationScope, &item.workspaceScope, &item.sessionScope,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return existingBinding{}, false, nil
	}
	if err != nil {
		return existingBinding{}, false, fmt.Errorf("read integration idempotency binding: %w", err)
	}
	return item, true, nil
}

func sameReplayBinding(existing existingBinding, input domain.CreateInvocationInput, binding domain.Binding) bool {
	return existing.argumentsHash == input.ArgumentsHash &&
		existing.binding.CapabilityID == binding.CapabilityID && existing.binding.CapabilityRevision == binding.CapabilityRevision &&
		existing.binding.ConnectionID == binding.ConnectionID && existing.binding.ConnectionRevision == binding.ConnectionRevision &&
		existing.binding.GrantID == binding.GrantID && existing.binding.GrantRevision == binding.GrantRevision &&
		existing.subjectKind == input.Session.SubjectKind && existing.subjectRef == input.Session.SubjectRef &&
		existing.installationScope == input.Session.InstallationScope && existing.workspaceScope == input.Session.WorkspaceScope &&
		existing.sessionScope == input.Session.SessionKey
}

func (repo *Repository) invocationByID(ctx context.Context, invocationID int64) (domain.Invocation, error) {
	return scanInvocation(repo.db.QueryRow(ctx, query("invocation__by_id.sql"), invocationID))
}

func scanInvocation(row pgx.Row) (domain.Invocation, error) {
	var item domain.Invocation
	var execution domain.ExecutionResult
	err := row.Scan(
		&item.ID, &item.PublicID, &item.Status, &item.ReasonCode,
		&item.Arguments.Namespace, &item.Arguments.WorkloadKind, &item.Arguments.WorkloadName,
		&item.ArgumentsHash, &item.ApprovalBindingHash, &item.CorrelationID,
		&item.ApprovalID, &item.ApprovalPublicID, &item.MattermostPostID,
		&execution.ExecutionID, &execution.Namespace, &execution.WorkloadKind, &execution.WorkloadName, &execution.RecordedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Invocation{}, domain.ErrUnauthorized
		}
		return domain.Invocation{}, fmt.Errorf("scan integration invocation: %w", err)
	}
	if execution.ExecutionID != "" {
		item.Execution = &execution
	}
	return item, nil
}

func decodeHash(value string) ([]byte, error) {
	decoded, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil || len(decoded) != sha256Size {
		return nil, domain.ErrApprovalBinding
	}
	return decoded, nil
}

const sha256Size = 32

func rollback(ctx context.Context, tx pgx.Tx) {
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	_ = tx.Rollback(rollbackCtx)
}

type auditInput struct {
	EventType         string
	ActorUserID       string
	ActorUser         string
	ResourceType      string
	ResourceName      string
	Summary           string
	CorrelationID     string
	InstallationScope string
	WorkspaceScope    string
	SessionScope      string
	Outcome           string
	ReasonCode        string
	Metadata          auditMetadata
	Now               time.Time
}

func (repo *Repository) appendAudit(ctx context.Context, input auditInput) error {
	metadata, err := json.Marshal(input.Metadata)
	if err != nil {
		return fmt.Errorf("encode integration audit metadata: %w", err)
	}
	if _, err := repo.db.Exec(ctx, query("audit__insert.sql"),
		input.EventType, input.ActorUserID, input.ActorUser, input.ResourceType, input.ResourceName,
		input.Summary, input.CorrelationID, input.InstallationScope, input.WorkspaceScope,
		input.SessionScope, input.Outcome, input.ReasonCode, metadata, input.Now,
	); err != nil {
		return fmt.Errorf("append integration audit: %w", err)
	}
	return nil
}
