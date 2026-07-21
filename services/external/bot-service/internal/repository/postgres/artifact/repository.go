package artifact

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"strings"

	domainartifact "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/artifact"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed sql/*.sql
var queryFiles embed.FS

type Repository struct {
	pool *pgxpool.Pool
}

var _ domainartifact.Repository = (*Repository)(nil)

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (repo *Repository) FindInbound(ctx context.Context, scope domainartifact.Scope, postID string, fileID string) (domainartifact.Version, error) {
	return scanVersion(repo.pool.QueryRow(ctx, query("artifact_versions__find_inbound.sql"),
		scope.ProjectID, scope.ChatID, scope.SessionID, scope.TurnID, postID, fileID,
	))
}

func (repo *Repository) CreateInbound(ctx context.Context, input domainartifact.CreateVersionInput) error {
	return repo.createVersion(ctx, input, false)
}

func (repo *Repository) BindInbound(ctx context.Context, versionID string, scope domainartifact.Scope, postID string, fileID string, ordinal int) error {
	tx, err := repo.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin artifact binding transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, query("artifact_turn__lock.sql"), scope.ProjectID, scope.ChatID, scope.SessionID, scope.TurnID); err != nil {
		return mapError("lock artifact turn", err)
	}
	_, err = tx.Exec(ctx, query("message_artifact_bindings__insert.sql"),
		versionID, scope.ProjectID, scope.ChatID, scope.SessionID, scope.TurnID,
		postID, fileID, domainartifact.DirectionInbound, ordinal,
	)
	if err != nil {
		return mapError("bind inbound artifact", err)
	}
	return mapError("commit artifact binding transaction", tx.Commit(ctx))
}

func (repo *Repository) ListTurn(ctx context.Context, scope domainartifact.Scope) ([]domainartifact.Version, error) {
	rows, err := repo.pool.Query(ctx, query("artifact_versions__list_turn.sql"), scope.ProjectID, scope.ChatID, scope.SessionID, scope.TurnID)
	if err != nil {
		return nil, fmt.Errorf("list turn artifacts: %w", err)
	}
	defer rows.Close()
	versions := make([]domainartifact.Version, 0)
	for rows.Next() {
		version, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list turn artifacts: %w", err)
	}
	return versions, nil
}

func (repo *Repository) GetAvailable(ctx context.Context, scope domainartifact.Scope, versionID string) (domainartifact.Version, error) {
	return scanVersion(repo.pool.QueryRow(ctx, query("artifact_versions__get_available.sql"),
		scope.ProjectID, scope.ChatID, scope.SessionID, scope.TurnID, versionID,
	))
}

func (repo *Repository) SetVersionState(ctx context.Context, versionID string, from domainartifact.VersionState, to domainartifact.VersionState, errorCode string) error {
	var returned string
	err := repo.pool.QueryRow(ctx, query("artifact_versions__set_state.sql"), versionID, from, to, errorCode).Scan(&returned)
	if errors.Is(err, pgx.ErrNoRows) {
		return domainartifact.ErrConflict
	}
	return mapError("set artifact version state", err)
}

func (repo *Repository) FindDelivery(ctx context.Context, scope domainartifact.Scope, idempotencyKey string) (domainartifact.Delivery, error) {
	row := repo.pool.QueryRow(ctx, query("artifact_deliveries__find.sql"), scope.ProjectID, scope.ChatID, scope.SessionID, scope.TurnID, idempotencyKey)
	var delivery domainartifact.Delivery
	err := row.Scan(
		&delivery.DeliveryID, &delivery.IdempotencyKey, &delivery.BotTokenSecretRef, &delivery.State,
		&delivery.MattermostFileID, &delivery.MattermostPostID, &delivery.ErrorCode, &delivery.Attempts,
		&delivery.ArtifactVersion.ArtifactID, &delivery.ArtifactVersion.VersionID,
		&delivery.ArtifactVersion.Scope.ProjectID, &delivery.ArtifactVersion.Scope.ChatID, &delivery.ArtifactVersion.Scope.SessionID,
		&delivery.ArtifactVersion.Scope.TurnID, &delivery.ArtifactVersion.Direction,
		&delivery.ArtifactVersion.State, &delivery.ArtifactVersion.ErrorCode, &delivery.ArtifactVersion.StorageKey, &delivery.ArtifactVersion.OriginalName,
		&delivery.ArtifactVersion.SafeName, &delivery.ArtifactVersion.MediaType, &delivery.ArtifactVersion.DeclaredMediaType,
		&delivery.ArtifactVersion.Size, &delivery.ArtifactVersion.SHA256, &delivery.ArtifactVersion.SourcePostID,
		&delivery.ArtifactVersion.SourceFileID, &delivery.ArtifactVersion.Ordinal,
		&delivery.ArtifactVersion.RetentionUntil, &delivery.ArtifactVersion.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domainartifact.Delivery{}, domainartifact.ErrNotFound
	}
	if err != nil {
		return domainartifact.Delivery{}, fmt.Errorf("find artifact delivery: %w", err)
	}
	delivery.Scope = scope
	delivery.ArtifactVersion.Scope = scope
	return delivery, nil
}

func (repo *Repository) CreateOutbound(ctx context.Context, input domainartifact.CreateVersionInput) error {
	return repo.createVersion(ctx, input, true)
}

func (repo *Repository) SetDeliveryResult(ctx context.Context, deliveryID string, state domainartifact.DeliveryState, fileID string, postID string, errorCode string) error {
	var returned string
	err := repo.pool.QueryRow(ctx, query("artifact_deliveries__set_result.sql"), deliveryID, state, fileID, postID, errorCode).Scan(&returned)
	if errors.Is(err, pgx.ErrNoRows) {
		return domainartifact.ErrConflict
	}
	return mapError("set artifact delivery result", err)
}

func (repo *Repository) createVersion(ctx context.Context, input domainartifact.CreateVersionInput, outbound bool) error {
	tx, err := repo.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin artifact transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	version := input.Version
	if _, err := tx.Exec(ctx, query("artifact_turn__lock.sql"), version.Scope.ProjectID, version.Scope.ChatID, version.Scope.SessionID, version.Scope.TurnID); err != nil {
		return mapError("lock artifact turn", err)
	}
	if _, err := tx.Exec(ctx, query("artifacts__insert.sql"),
		version.ArtifactID, version.Scope.ProjectID, version.Scope.ChatID, version.Scope.SessionID,
		version.Scope.TurnID, version.Direction, version.SourcePostID, version.SourceFileID, version.RetentionUntil,
	); err != nil {
		return mapError("insert artifact", err)
	}
	if _, err := tx.Exec(ctx, query("artifact_versions__insert.sql"),
		version.VersionID, version.ArtifactID, version.StorageKey, version.OriginalName, version.SafeName,
		version.MediaType, version.DeclaredMediaType, version.Size, version.SHA256, version.State, version.ErrorCode, version.CreatedAt,
	); err != nil {
		return mapError("insert artifact version", err)
	}
	if _, err := tx.Exec(ctx, query("message_artifact_bindings__insert.sql"),
		version.VersionID, version.Scope.ProjectID, version.Scope.ChatID, version.Scope.SessionID,
		version.Scope.TurnID, version.SourcePostID, version.SourceFileID, version.Direction, max(version.Ordinal, 1),
	); err != nil {
		return mapError("insert artifact binding", err)
	}
	if outbound {
		if _, err := tx.Exec(ctx, query("artifact_deliveries__insert.sql"),
			input.DeliveryID, version.VersionID, version.Scope.ProjectID, version.Scope.ChatID, version.Scope.SessionID,
			version.Scope.TurnID, input.IdempotencyKey, input.BotTokenSecretRef, input.DeliveryState, version.ErrorCode,
		); err != nil {
			return mapError("insert artifact delivery", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return mapError("commit artifact transaction", err)
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanVersion(row rowScanner) (domainartifact.Version, error) {
	var version domainartifact.Version
	err := row.Scan(
		&version.ArtifactID, &version.VersionID, &version.Scope.ProjectID, &version.Scope.ChatID,
		&version.Scope.SessionID, &version.Scope.TurnID, &version.Direction, &version.State, &version.ErrorCode,
		&version.StorageKey, &version.OriginalName, &version.SafeName, &version.MediaType,
		&version.DeclaredMediaType, &version.Size, &version.SHA256, &version.SourcePostID,
		&version.SourceFileID, &version.Ordinal, &version.RetentionUntil, &version.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domainartifact.Version{}, domainartifact.ErrNotFound
	}
	if err != nil {
		return domainartifact.Version{}, fmt.Errorf("scan artifact version: %w", err)
	}
	return version, nil
}

func query(name string) string {
	body, err := queryFiles.ReadFile("sql/" + name)
	if err != nil {
		panic(err)
	}
	return string(body)
}

func mapError(operation string, err error) error {
	if err == nil {
		return nil
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		return domainartifact.ErrConflict
	}
	if errors.As(err, &postgresError) && postgresError.Code == "23514" && strings.HasPrefix(postgresError.Message, "artifact turn ") {
		return domainartifact.ErrLimitExceeded
	}
	return fmt.Errorf("%s: %w", strings.TrimSpace(operation), err)
}
