// Package team реализует RLS-scoped PostgreSQL provider checkpoints.
package team

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	domainrepo "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/repository/team"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/enum"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	selectorNamespace = uuid.MustParse("dad102bc-3e5a-5e8c-b980-e80842288f89")
	cursorNamespace   = uuid.MustParse("7b9850cc-b44f-502d-a34f-21815a5298b0")
)

type Config struct {
	PrincipalGeneration uint64
	OrganizationID      string
	AllowedProjectIDs   []string
}

type Repository struct {
	pool   *pgxpool.Pool
	config Config
}

type rowScanner interface {
	Scan(...any) error
}

func New(pool *pgxpool.Pool, config Config) (*Repository, error) {
	if pool == nil || config.PrincipalGeneration == 0 || !validUUID(config.OrganizationID) || len(config.AllowedProjectIDs) == 0 {
		return nil, errors.New("mattermost team repository configuration is invalid")
	}
	for _, projectID := range config.AllowedProjectIDs {
		if !validUUID(projectID) {
			return nil, errors.New("mattermost team repository project scope is invalid")
		}
	}
	return &Repository{pool: pool, config: config}, nil
}

func validUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed != uuid.Nil
}

func (repository *Repository) Check(ctx context.Context) error {
	projects, err := json.Marshal(repository.config.AllowedProjectIDs)
	if err != nil {
		return errors.New("encode Mattermost team repository project scope")
	}
	var schemaVersion uint64
	var identityReady bool
	if err := repository.pool.QueryRow(ctx, readinessCheckSQL, repository.config.PrincipalGeneration,
		repository.config.OrganizationID, projects).Scan(&schemaVersion, &identityReady); err != nil || schemaVersion != 2 || !identityReady {
		return errors.New("mattermost team repository is not ready")
	}
	principal := entity.TeamPrincipal{
		OrganizationID: repository.config.OrganizationID,
		ProjectID:      repository.config.AllowedProjectIDs[0], ActorID: uuid.NewString(),
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable, AccessMode: pgx.ReadWrite})
	if err != nil {
		return errors.New("begin Mattermost team readiness transaction")
	}
	defer tx.Rollback(ctx)
	if err := activateScope(ctx, tx, principal); err != nil {
		return err
	}
	negative, err := tx.Begin(ctx)
	if err != nil {
		return errors.New("begin Mattermost team negative readiness probe")
	}
	var organizationID, projectID, actorID string
	var offset uint32
	negativeErr := negative.QueryRow(ctx, readinessProbeCursorSQL, uuid.NewString(), uuid.NewString(),
		principal.ProjectID, principal.ActorID).Scan(&organizationID, &projectID, &actorID, &offset)
	rollbackErr := negative.Rollback(ctx)
	if negativeErr == nil || (rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed)) {
		return errors.New("cross-tenant Mattermost team readiness probe was not rejected")
	}
	if err := tx.QueryRow(ctx, readinessProbeCursorSQL, uuid.NewString(), principal.OrganizationID,
		principal.ProjectID, principal.ActorID).Scan(&organizationID, &projectID, &actorID, &offset); err != nil ||
		organizationID != principal.OrganizationID || projectID != principal.ProjectID || actorID != principal.ActorID || offset != 1 {
		return errors.New("scoped Mattermost team readiness DML is not ready")
	}
	return nil
}

func (repository *Repository) ResolveCatalogOffset(ctx context.Context, principal entity.TeamPrincipal, cursor string, pageSize uint32) (uint32, error) {
	if cursor == "" {
		return 0, nil
	}
	if !validUUID(cursor) {
		return 0, domainrepo.ErrNotFound
	}
	var offset uint32
	err := repository.withScope(ctx, principal, pgx.ReadOnly, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, catalogCursorResolveSQL, cursor, principal.OrganizationID, principal.ProjectID,
			principal.ActorID, pageSize).Scan(&offset)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, domainrepo.ErrNotFound
	}
	if err != nil {
		return 0, errors.New("resolve Mattermost team catalog cursor")
	}
	return offset, nil
}

func (repository *Repository) SaveCatalogPage(ctx context.Context, principal entity.TeamPrincipal,
	teams []entity.MattermostTeam, offset, pageSize uint32, hasMore bool, ttl time.Duration,
) ([]entity.MattermostTeam, string, error) {
	result := make([]entity.MattermostTeam, len(teams))
	nextCursor := ""
	err := repository.withScope(ctx, principal, pgx.ReadWrite, func(tx pgx.Tx) error {
		for index, team := range teams {
			stored, err := upsertSelector(ctx, tx, principal, team, ttl)
			if err != nil {
				return err
			}
			result[index] = stored
		}
		if !hasMore {
			return nil
		}
		nextOffset := offset + pageSize
		cursorID := uuid.NewSHA1(cursorNamespace, []byte(strings.Join([]string{
			principal.OrganizationID, principal.ProjectID, principal.ActorID,
			fmt.Sprint(nextOffset), fmt.Sprint(pageSize),
		}, "\x00"))).String()
		return tx.QueryRow(ctx, catalogCursorUpsertSQL, cursorID, principal.OrganizationID, principal.ProjectID,
			principal.ActorID, nextOffset, pageSize, interval(ttl)).Scan(&nextCursor)
	})
	if err != nil {
		return nil, "", errors.New("save Mattermost team catalog page")
	}
	return result, nextCursor, nil
}

func (repository *Repository) ResolveSelector(ctx context.Context, principal entity.TeamPrincipal, selector string) (string, error) {
	var providerTeamID string
	err := repository.withScope(ctx, principal, pgx.ReadOnly, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, selectorResolveSQL, selector, principal.OrganizationID,
			principal.ProjectID, principal.ActorID).Scan(&providerTeamID)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domainrepo.ErrNotFound
	}
	if err != nil {
		return "", errors.New("resolve Mattermost team selector")
	}
	return providerTeamID, nil
}

func (repository *Repository) RefreshSelector(ctx context.Context, principal entity.TeamPrincipal,
	team entity.MattermostTeam, ttl time.Duration,
) (entity.MattermostTeam, error) {
	var stored entity.MattermostTeam
	err := repository.withScope(ctx, principal, pgx.ReadWrite, func(tx pgx.Tx) error {
		var err error
		stored, err = upsertSelector(ctx, tx, principal, team, ttl)
		return err
	})
	if err != nil {
		return entity.MattermostTeam{}, errors.New("refresh Mattermost team selector")
	}
	return stored, nil
}

func (repository *Repository) BeginCreate(ctx context.Context, operation entity.MattermostTeamOperation,
	owner string, lease time.Duration,
) (entity.MattermostTeamOperation, domainrepo.CreateDisposition, error) {
	token, err := newLeaseToken()
	if err != nil {
		return entity.MattermostTeamOperation{}, 0, err
	}
	disposition := domainrepo.CreateClaimed
	var stored entity.MattermostTeamOperation
	err = repository.withScope(ctx, operation.Principal, pgx.ReadWrite, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, operationInsertSQL, operation.ID, operation.Principal.OrganizationID,
			operation.Principal.ProjectID, operation.Principal.ActorID, operation.Intent.IdempotencyKey,
			operation.Intent.RequestSHA256, operation.Intent.DisplayName, operation.Intent.Slug,
			owner, digest(token), interval(lease))
		if err != nil {
			return err
		}
		stored, leaseActive, err := scanOperation(tx.QueryRow(ctx, operationLockSQL, operation.ID))
		if err != nil {
			return err
		}
		if stored.Intent.RequestSHA256 != operation.Intent.RequestSHA256 || stored.ID != operation.ID {
			return domainrepo.ErrIdempotencyConflict
		}
		if tag.RowsAffected() == 1 {
			stored.LeaseToken = token
			return nil
		}
		if stored.State == enum.TeamOperationPending && !leaseActive {
			tag, err = tx.Exec(ctx, operationReclaimSQL, operation.ID, owner, digest(token), interval(lease))
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 1 {
				stored, _, err = scanOperation(tx.QueryRow(ctx, operationLockSQL, operation.ID))
				stored.LeaseToken = token
				return err
			}
		}
		if stored.State == enum.TeamOperationPending {
			disposition = domainrepo.CreateBusy
		} else {
			disposition = domainrepo.CreateReplay
		}
		return nil
	})
	if errors.Is(err, domainrepo.ErrIdempotencyConflict) {
		return entity.MattermostTeamOperation{}, 0, err
	}
	if err != nil {
		return entity.MattermostTeamOperation{}, 0, errors.New("begin Mattermost team create")
	}
	return stored, disposition, nil
}

func (repository *Repository) MarkEffectStarted(ctx context.Context, operation entity.MattermostTeamOperation) (entity.MattermostTeamOperation, error) {
	var started time.Time
	err := repository.withScope(ctx, operation.Principal, pgx.ReadWrite, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, operationMarkEffectSQL, operation.ID, operation.Fence,
			digest(operation.LeaseToken)).Scan(&started)
	})
	if err != nil {
		return entity.MattermostTeamOperation{}, errors.New("mark Mattermost team provider effect")
	}
	operation.State, operation.EffectStartedAt = enum.TeamOperationEffectPending, started
	return operation, nil
}

func (repository *Repository) MarkAmbiguous(ctx context.Context, operation entity.MattermostTeamOperation,
	code string, retry time.Time,
) error {
	return repository.updateOperation(ctx, operation, operationMarkAmbiguousSQL,
		operation.ID, code, retry, operation.Fence, digest(operation.LeaseToken))
}

func (repository *Repository) MarkRepairRequired(ctx context.Context, operation entity.MattermostTeamOperation, code string) error {
	return repository.updateOperation(ctx, operation, operationMarkRepairSQL,
		operation.ID, code, operation.Fence, digest(operation.LeaseToken))
}

func (repository *Repository) AcceptProvider(ctx context.Context, operation entity.MattermostTeamOperation,
	team entity.MattermostTeam, receipt string, ttl time.Duration,
) (entity.MattermostTeamOperation, error) {
	var accepted entity.MattermostTeamOperation
	err := repository.withScope(ctx, operation.Principal, pgx.ReadWrite, func(tx pgx.Tx) error {
		storedTeam, err := upsertSelector(ctx, tx, operation.Principal, team, ttl)
		if err != nil {
			return err
		}
		var generation uint64
		if err := tx.QueryRow(ctx, providerWatermarkAdvanceSQL, operation.Principal.OrganizationID,
			operation.Principal.ProjectID).Scan(&generation); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, operationAcceptSQL, operation.ID, storedTeam.Selector, team.ProviderTeamID,
			team.Status, team.ProviderSnapshotSHA256, receipt, generation, team.CreatedAt, team.UpdatedAt,
			team.ObservedAt, operation.Fence, digest(operation.LeaseToken))
		if err != nil || tag.RowsAffected() != 1 {
			return errors.New("accept Mattermost team provider checkpoint")
		}
		accepted, _, err = scanOperation(tx.QueryRow(ctx, operationLockSQL, operation.ID))
		return err
	})
	if err != nil {
		return entity.MattermostTeamOperation{}, errors.New("save Mattermost team provider checkpoint")
	}
	return accepted, nil
}

func (repository *Repository) ClaimRecovery(ctx context.Context, owner string, lease time.Duration) (entity.MattermostTeamOperation, bool, error) {
	var organizationID, projectID sql.NullString
	if err := repository.pool.QueryRow(ctx, nextWorkScopeSQL).Scan(&organizationID, &projectID); err != nil {
		return entity.MattermostTeamOperation{}, false, errors.New("resolve Mattermost team recovery scope")
	}
	if !organizationID.Valid || !projectID.Valid {
		return entity.MattermostTeamOperation{}, false, nil
	}
	principal := entity.TeamPrincipal{
		OrganizationID: organizationID.String, ProjectID: projectID.String,
		ActorID: "system:interaction-team-recovery",
	}
	token, err := newLeaseToken()
	if err != nil {
		return entity.MattermostTeamOperation{}, false, err
	}
	var operation entity.MattermostTeamOperation
	err = repository.withScope(ctx, principal, pgx.ReadWrite, func(tx pgx.Tx) error {
		var scanErr error
		operation, _, scanErr = scanOperation(tx.QueryRow(ctx, operationClaimRecoverySQL,
			owner, digest(token), interval(lease)))
		return scanErr
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.MattermostTeamOperation{}, false, nil
	}
	if err != nil {
		return entity.MattermostTeamOperation{}, false, errors.New("claim Mattermost team recovery")
	}
	operation.LeaseToken = token
	return operation, true, nil
}

func (repository *Repository) AdvanceProviderGeneration(ctx context.Context, principal entity.TeamPrincipal) (uint64, error) {
	var generation uint64
	err := repository.withScope(ctx, principal, pgx.ReadWrite, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, providerWatermarkAdvanceSQL, principal.OrganizationID, principal.ProjectID).Scan(&generation)
	})
	if err != nil || generation == 0 {
		return 0, errors.New("advance Mattermost provider observation generation")
	}
	return generation, nil
}

func (repository *Repository) BeginMapping(ctx context.Context, operation entity.WorkspaceMappingOperation,
	owner string, lease time.Duration,
) (entity.WorkspaceMappingOperation, domainrepo.MappingDisposition, error) {
	token, err := newLeaseToken()
	if err != nil {
		return entity.WorkspaceMappingOperation{}, 0, err
	}
	disposition := domainrepo.MappingClaimed
	var stored entity.WorkspaceMappingOperation
	err = repository.withScope(ctx, operation.Principal, pgx.ReadWrite, func(tx pgx.Tx) error {
		var generation uint64
		if err := tx.QueryRow(ctx, providerWatermarkAdvanceSQL, operation.Principal.OrganizationID,
			operation.Principal.ProjectID).Scan(&generation); err != nil {
			return err
		}
		receiptID := uuid.NewString()
		tag, err := tx.Exec(ctx, mappingOperationInsertSQL, operation.ID, operation.Principal.OrganizationID,
			operation.Principal.ProjectID, operation.Principal.ActorID, operation.Action,
			operation.IdempotencyKey, operation.RequestSHA256, operation.MappingID,
			operation.ExpectedVersion, operation.ExpectedGeneration, operation.DisplayName,
			operation.Team.Selector, operation.Team.ProviderTeamID, operation.Team.Status,
			operation.Team.ProviderSnapshotSHA256, operation.Team.CreatedAt, operation.Team.UpdatedAt,
			operation.Team.ObservedAt, generation, receiptID, owner, digest(token), interval(lease))
		if err != nil {
			return err
		}
		stored, leaseActive, err := scanMappingOperation(tx.QueryRow(ctx, mappingOperationLockSQL, operation.ID))
		if err != nil {
			return err
		}
		if stored.RequestSHA256 != operation.RequestSHA256 || stored.Action != operation.Action ||
			stored.Principal.ActorID != operation.Principal.ActorID {
			return domainrepo.ErrIdempotencyConflict
		}
		if tag.RowsAffected() == 1 {
			stored.LeaseToken = token
			return nil
		}
		if stored.State != enum.WorkspaceMappingOperationPending &&
			stored.State != enum.WorkspaceMappingOperationAmbiguous {
			disposition = domainrepo.MappingReplay
			return nil
		}
		if leaseActive {
			disposition = domainrepo.MappingBusy
			return nil
		}
		tag, err = tx.Exec(ctx, mappingOperationReclaimSQL, stored.ID, owner, digest(token), interval(lease))
		if err != nil || tag.RowsAffected() != 1 {
			disposition = domainrepo.MappingBusy
			return err
		}
		stored, _, err = scanMappingOperation(tx.QueryRow(ctx, mappingOperationLockSQL, stored.ID))
		stored.LeaseToken = token
		return err
	})
	if errors.Is(err, domainrepo.ErrIdempotencyConflict) {
		return entity.WorkspaceMappingOperation{}, 0, err
	}
	if err != nil {
		return entity.WorkspaceMappingOperation{}, 0, errors.New("begin Workspace Mattermost mapping operation")
	}
	return stored, disposition, nil
}

// RefreshMappingReceipt выдаёт неоднозначной операции новый одноразовый JTI и
// монотонное provider generation только после owner readback её predecessor.
// Watermark и operation checkpoint меняются одной транзакцией.
func (repository *Repository) RefreshMappingReceipt(ctx context.Context,
	operation entity.WorkspaceMappingOperation,
) (entity.WorkspaceMappingOperation, error) {
	var refreshed entity.WorkspaceMappingOperation
	err := repository.withScope(ctx, operation.Principal, pgx.ReadWrite, func(tx pgx.Tx) error {
		var generation uint64
		if err := tx.QueryRow(ctx, providerWatermarkAdvanceSQL, operation.Principal.OrganizationID,
			operation.Principal.ProjectID).Scan(&generation); err != nil {
			return err
		}
		if generation <= operation.EffectGeneration {
			return errors.New("provider observation generation did not advance")
		}
		tag, err := tx.Exec(ctx, mappingOperationRefreshReceiptSQL, operation.ID,
			operation.Fence, digest(operation.LeaseToken), generation, uuid.NewString())
		if err != nil || tag.RowsAffected() != 1 {
			return errors.New("refresh Workspace mapping provider receipt")
		}
		refreshed, _, err = scanMappingOperation(tx.QueryRow(ctx, mappingOperationLockSQL, operation.ID))
		if err == nil {
			refreshed.LeaseToken = operation.LeaseToken
		}
		return err
	})
	if err != nil {
		return entity.WorkspaceMappingOperation{}, errors.New("refresh Workspace mapping provider receipt")
	}
	return refreshed, nil
}

func (repository *Repository) MarkMappingAmbiguous(ctx context.Context, operation entity.WorkspaceMappingOperation,
	code string, retry time.Time,
) error {
	return repository.updateMappingOperation(ctx, operation, mappingOperationMarkAmbiguousSQL,
		operation.ID, code, retry, operation.Fence, digest(operation.LeaseToken))
}

func (repository *Repository) MarkMappingTerminal(ctx context.Context, operation entity.WorkspaceMappingOperation,
	mapping entity.WorkspaceMattermostMapping,
) error {
	state := enum.WorkspaceMappingOperationBound
	if mapping.State == "UNLINKED" {
		state = enum.WorkspaceMappingOperationUnlinked
	}
	return repository.updateMappingOperation(ctx, operation, mappingOperationMarkTerminalSQL,
		operation.ID, state, mapping.ID, mapping.Version, mapping.Generation,
		mapping.ProviderEffectVersion, mapping.ProviderEffectGeneration, mapping.ProviderObservedAt,
		mapping.UpdatedAt, operation.Fence, digest(operation.LeaseToken))
}

func (repository *Repository) MarkMappingRepairRequired(ctx context.Context,
	operation entity.WorkspaceMappingOperation, code string,
) error {
	return repository.updateMappingOperation(ctx, operation, mappingOperationMarkRepairSQL,
		operation.ID, code, operation.Fence, digest(operation.LeaseToken))
}

func (repository *Repository) ClaimMappingRecovery(ctx context.Context, owner string,
	lease time.Duration,
) (entity.WorkspaceMappingOperation, bool, error) {
	var organizationID, projectID sql.NullString
	if err := repository.pool.QueryRow(ctx, mappingNextWorkScopeSQL).Scan(&organizationID, &projectID); err != nil {
		return entity.WorkspaceMappingOperation{}, false, errors.New("resolve Workspace mapping recovery scope")
	}
	if !organizationID.Valid || !projectID.Valid {
		return entity.WorkspaceMappingOperation{}, false, nil
	}
	principal := entity.TeamPrincipal{
		OrganizationID: organizationID.String, ProjectID: projectID.String,
		ActorID: "system:interaction-mapping-recovery",
	}
	token, err := newLeaseToken()
	if err != nil {
		return entity.WorkspaceMappingOperation{}, false, err
	}
	var operation entity.WorkspaceMappingOperation
	err = repository.withScope(ctx, principal, pgx.ReadWrite, func(tx pgx.Tx) error {
		var operationID string
		if err := tx.QueryRow(ctx, mappingOperationClaimRecoverySQL, owner, digest(token), interval(lease)).Scan(&operationID); err != nil {
			return err
		}
		var scanErr error
		operation, _, scanErr = scanMappingOperation(tx.QueryRow(ctx, mappingOperationLockSQL, operationID))
		return scanErr
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.WorkspaceMappingOperation{}, false, nil
	}
	if err != nil {
		return entity.WorkspaceMappingOperation{}, false, errors.New("claim Workspace mapping recovery")
	}
	operation.LeaseToken = token
	return operation, true, nil
}

func (repository *Repository) updateMappingOperation(ctx context.Context,
	operation entity.WorkspaceMappingOperation, query string, arguments ...any,
) error {
	return repository.withScope(ctx, operation.Principal, pgx.ReadWrite, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, query, arguments...)
		if err != nil || tag.RowsAffected() != 1 {
			return errors.New("update Workspace Mattermost mapping operation")
		}
		return nil
	})
}

func (repository *Repository) updateOperation(ctx context.Context, operation entity.MattermostTeamOperation,
	query string, arguments ...any,
) error {
	return repository.withScope(ctx, operation.Principal, pgx.ReadWrite, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, query, arguments...)
		if err != nil || tag.RowsAffected() != 1 {
			return errors.New("update Mattermost team operation")
		}
		return nil
	})
}

func (repository *Repository) withScope(ctx context.Context, principal entity.TeamPrincipal,
	access pgx.TxAccessMode, run func(pgx.Tx) error,
) error {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable, AccessMode: access})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := activateScope(ctx, tx, principal); err != nil {
		return err
	}
	if err := run(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func activateScope(ctx context.Context, tx pgx.Tx, principal entity.TeamPrincipal) error {
	if _, err := tx.Exec(ctx, activateScopeSQL, principal.OrganizationID, principal.ProjectID, principal.ActorID); err != nil {
		return errors.New("activate Mattermost team repository scope")
	}
	return nil
}

func upsertSelector(ctx context.Context, tx pgx.Tx, principal entity.TeamPrincipal,
	team entity.MattermostTeam, ttl time.Duration,
) (entity.MattermostTeam, error) {
	if team.ProviderTeamID == "" || team.ProviderSnapshotSHA256 == "" || team.ObservedAt.IsZero() || ttl <= 0 {
		return entity.MattermostTeam{}, errors.New("mattermost team selector input is invalid")
	}
	selectorID := team.Selector
	if !validUUID(selectorID) {
		selectorID = uuid.NewSHA1(selectorNamespace, []byte(strings.Join([]string{
			principal.OrganizationID, principal.ProjectID, principal.ActorID, team.ProviderTeamID,
		}, "\x00"))).String()
	}
	if err := tx.QueryRow(ctx, selectorUpsertSQL, selectorID, principal.OrganizationID, principal.ProjectID,
		principal.ActorID, team.ProviderTeamID, team.DisplayName, team.Slug, team.Status,
		team.ProviderSnapshotSHA256, team.CreatedAt, team.UpdatedAt, team.ObservedAt, interval(ttl)).Scan(&selectorID); err != nil {
		return entity.MattermostTeam{}, err
	}
	team.Selector = selectorID
	return team, nil
}

func scanOperation(row rowScanner) (entity.MattermostTeamOperation, bool, error) {
	var operation entity.MattermostTeamOperation
	var state, status string
	var leaseActive bool
	if err := row.Scan(&operation.ID, &operation.Principal.OrganizationID, &operation.Principal.ProjectID,
		&operation.Principal.ActorID, &operation.Intent.IdempotencyKey, &operation.Intent.RequestSHA256,
		&operation.Intent.DisplayName, &operation.Intent.Slug, &state, &operation.Team.Selector,
		&operation.Team.ProviderTeamID, &status, &operation.Team.ProviderSnapshotSHA256,
		&operation.ProviderReceiptSHA256, &operation.ProviderGeneration, &operation.FailureCode,
		&operation.Fence, &operation.EffectStartedAt, &operation.RetryNotBefore,
		&operation.CreatedAt, &operation.UpdatedAt, &operation.Team.CreatedAt,
		&operation.Team.UpdatedAt, &operation.Team.ObservedAt, &leaseActive); err != nil {
		return entity.MattermostTeamOperation{}, false, err
	}
	operation.State = enum.MattermostTeamOperationState(state)
	operation.Team.Status = enum.MattermostTeamStatus(status)
	if operation.EffectStartedAt.Equal(time.Unix(0, 0)) {
		operation.EffectStartedAt = time.Time{}
	}
	if operation.Team.CreatedAt.Equal(time.Unix(0, 0)) {
		operation.Team.CreatedAt, operation.Team.UpdatedAt, operation.Team.ObservedAt = time.Time{}, time.Time{}, time.Time{}
	}
	return operation, leaseActive, nil
}

func scanMappingOperation(row rowScanner) (entity.WorkspaceMappingOperation, bool, error) {
	var operation entity.WorkspaceMappingOperation
	var status string
	var resultObservedAt, resultUpdatedAt time.Time
	var leaseActive bool
	if err := row.Scan(&operation.ID, &operation.Principal.OrganizationID, &operation.Principal.ProjectID,
		&operation.Principal.ActorID, &operation.Action, &operation.IdempotencyKey, &operation.RequestSHA256,
		&operation.MappingID, &operation.ExpectedVersion, &operation.ExpectedGeneration, &operation.DisplayName,
		&operation.Team.Selector, &operation.Team.ProviderTeamID, &status, &operation.Team.ProviderSnapshotSHA256,
		&operation.Team.CreatedAt, &operation.Team.UpdatedAt, &operation.Team.ObservedAt,
		&operation.EffectGeneration, &operation.ReceiptID, &operation.State, &operation.FailureCode,
		&operation.Fence, &operation.RetryNotBefore, &operation.CreatedAt, &operation.UpdatedAt,
		&operation.Result.ID, &operation.Result.Version, &operation.Result.Generation,
		&operation.Result.ProviderEffectVersion, &operation.Result.ProviderEffectGeneration,
		&resultObservedAt, &resultUpdatedAt, &leaseActive); err != nil {
		return entity.WorkspaceMappingOperation{}, false, err
	}
	operation.Team.Status = enum.MattermostTeamStatus(status)
	operation.Result.ProviderTeamID = operation.Team.ProviderTeamID
	if operation.State == enum.WorkspaceMappingOperationUnlinked {
		operation.Result.State = "UNLINKED"
	} else if operation.State == enum.WorkspaceMappingOperationBound {
		operation.Result.State = "BOUND"
	}
	if !resultObservedAt.Equal(time.Unix(0, 0)) {
		operation.Result.ProviderObservedAt, operation.Result.UpdatedAt = resultObservedAt, resultUpdatedAt
	}
	return operation, leaseActive, nil
}

func newLeaseToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", errors.New("generate Mattermost team lease token")
	}
	return hex.EncodeToString(raw), nil
}

func digest(value string) string {
	valueDigest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(valueDigest[:])
}

func interval(value time.Duration) string {
	return fmt.Sprintf("%d microseconds", value.Microseconds())
}
