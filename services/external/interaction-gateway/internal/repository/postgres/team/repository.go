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
	"github.com/jackc/pgx/v5/pgconn"
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

type namedQueryer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func strictNamed(arguments ...any) pgx.StrictNamedArgs {
	result := make(pgx.StrictNamedArgs, len(arguments))
	for index, argument := range arguments {
		result[fmt.Sprintf("arg%d", index+1)] = argument
	}
	return result
}

func execNamed(ctx context.Context, queryer namedQueryer, query string,
	arguments ...any,
) (pgconn.CommandTag, error) {
	return queryer.Exec(ctx, query, strictNamed(arguments...))
}

func queryNamed(ctx context.Context, queryer namedQueryer, query string,
	arguments ...any,
) (pgx.Rows, error) {
	return queryer.Query(ctx, query, strictNamed(arguments...))
}

func queryRow(ctx context.Context, queryer namedQueryer, query string,
	arguments ...any,
) pgx.Row {
	return queryer.QueryRow(ctx, query, strictNamed(arguments...))
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
	if err := validateTeamQueries(); err != nil {
		return nil, err
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
	if err := queryRow(ctx, repository.pool, readinessCheckSQL, repository.config.PrincipalGeneration,
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
	negativeErr := queryRow(ctx, negative, readinessProbeCursorSQL, uuid.NewString(), uuid.NewString(),
		principal.ProjectID, principal.ActorID).Scan(&organizationID, &projectID, &actorID, &offset)
	rollbackErr := negative.Rollback(ctx)
	if negativeErr == nil || (rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed)) {
		return errors.New("cross-tenant Mattermost team readiness probe was not rejected")
	}
	if err := queryRow(ctx, tx, readinessProbeCursorSQL, uuid.NewString(), principal.OrganizationID,
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
		return queryRow(ctx, tx, catalogCursorResolveSQL, cursor, principal.OrganizationID, principal.ProjectID,
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
		return queryRow(ctx, tx, catalogCursorUpsertSQL, cursorID, principal.OrganizationID, principal.ProjectID,
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
		return queryRow(ctx, tx, selectorResolveSQL, selector, principal.OrganizationID,
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
	owner string, lease, recoveryWindow time.Duration,
) (entity.MattermostTeamOperation, domainrepo.CreateDisposition, error) {
	token, err := newLeaseToken()
	if err != nil {
		return entity.MattermostTeamOperation{}, 0, err
	}
	disposition := domainrepo.CreateClaimed
	var stored entity.MattermostTeamOperation
	err = repository.withScope(ctx, operation.Principal, pgx.ReadWrite, func(tx pgx.Tx) error {
		tag, err := execNamed(ctx, tx, operationInsertSQL, operation.ID, operation.Principal.OrganizationID,
			operation.Principal.ProjectID, operation.Principal.ActorID, operation.Intent.IdempotencyKey,
			operation.Intent.RequestSHA256, operation.Intent.ProviderCorrelation, operation.Intent.DisplayName,
			operation.Intent.Slug, owner, digest(token), interval(lease), interval(recoveryWindow))
		if err != nil {
			return err
		}
		fenceTag, err := execNamed(ctx, tx, createFenceAcquireSQL, operation.Principal.OrganizationID,
			operation.Principal.ProjectID, operation.ID, operation.Intent.RequestSHA256)
		if err != nil {
			return err
		}
		if fenceTag.RowsAffected() == 0 {
			var fenceOperationID, fenceRequestSHA, fenceState, providerTeamID string
			if err := queryRow(ctx, tx, createFenceLockSQL, operation.Principal.OrganizationID,
				operation.Principal.ProjectID).Scan(&fenceOperationID, &fenceRequestSHA, &fenceState, &providerTeamID); err != nil {
				return err
			}
			if fenceState == "UNLINKED" {
				replaced, replaceErr := execNamed(ctx, tx, createFenceReplaceUnlinkedSQL, operation.Principal.OrganizationID,
					operation.Principal.ProjectID, operation.ID, operation.Intent.RequestSHA256)
				if replaceErr != nil || replaced.RowsAffected() != 1 {
					return domainrepo.ErrCreateFenceConflict
				}
			} else if fenceOperationID != operation.ID || fenceRequestSHA != operation.Intent.RequestSHA256 {
				_ = providerTeamID
				return domainrepo.ErrCreateFenceConflict
			}
		}
		var leaseActive bool
		stored, leaseActive, err = scanOperation(queryRow(ctx, tx, operationLockSQL, operation.ID))
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
			tag, err = execNamed(ctx, tx, operationReclaimSQL, operation.ID, owner, digest(token), interval(lease))
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 1 {
				stored, _, err = scanOperation(queryRow(ctx, tx, operationLockSQL, operation.ID))
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
	if errors.Is(err, domainrepo.ErrIdempotencyConflict) || errors.Is(err, domainrepo.ErrCreateFenceConflict) {
		return entity.MattermostTeamOperation{}, 0, err
	}
	if err != nil {
		return entity.MattermostTeamOperation{}, 0, errors.New("begin Mattermost team create")
	}
	if stored.ID != operation.ID || stored.Principal != operation.Principal ||
		stored.Intent.IdempotencyKey != operation.Intent.IdempotencyKey {
		return entity.MattermostTeamOperation{}, 0, errors.New("Mattermost team create checkpoint is invalid")
	}
	return stored, disposition, nil
}

func (repository *Repository) GetCreateOperation(ctx context.Context, principal entity.TeamPrincipal,
	operationID string,
) (entity.MattermostTeamOperation, error) {
	var operation entity.MattermostTeamOperation
	err := repository.withScope(ctx, principal, pgx.ReadOnly, func(tx pgx.Tx) error {
		var scanErr error
		operation, _, scanErr = scanOperation(queryRow(ctx, tx, operationGetSQL,
			principal.OrganizationID, principal.ProjectID, principal.ActorID, operationID))
		return scanErr
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.MattermostTeamOperation{}, domainrepo.ErrNotFound
	}
	if err != nil {
		return entity.MattermostTeamOperation{}, errors.New("read Mattermost team create operation")
	}
	return operation, nil
}

func (repository *Repository) MarkEffectStarted(ctx context.Context, operation entity.MattermostTeamOperation) (entity.MattermostTeamOperation, error) {
	var started time.Time
	err := repository.withScope(ctx, operation.Principal, pgx.ReadWrite, func(tx pgx.Tx) error {
		return queryRow(ctx, tx, operationMarkEffectSQL, operation.ID, operation.Fence,
			digest(operation.LeaseToken)).Scan(&started)
	})
	if err != nil {
		return entity.MattermostTeamOperation{}, errors.New("mark Mattermost team provider effect")
	}
	operation.State, operation.EffectStartedAt = enum.TeamOperationEffectPending, started
	return operation, nil
}

func (repository *Repository) DeferCreateRecovery(ctx context.Context, operation entity.MattermostTeamOperation,
	code string, retryDelay time.Duration,
) (entity.MattermostTeamOperation, error) {
	var state, failureCode string
	var retryNotBefore, recoveryDeadline, updatedAt time.Time
	err := repository.withScope(ctx, operation.Principal, pgx.ReadWrite, func(tx pgx.Tx) error {
		if err := queryRow(ctx, tx, operationMarkAmbiguousSQL, operation.ID, code, interval(retryDelay),
			operation.Fence, digest(operation.LeaseToken)).Scan(&state, &failureCode, &retryNotBefore,
			&recoveryDeadline, &updatedAt); err != nil {
			return err
		}
		if state == string(enum.TeamOperationRepairRequired) {
			tag, err := execNamed(ctx, tx, createFenceTerminalSQL, operation.Principal.OrganizationID,
				operation.Principal.ProjectID, operation.ID, "REPAIR_REQUIRED")
			if err != nil || tag.RowsAffected() != 1 {
				return errors.New("update expired Mattermost team create fence")
			}
		}
		return nil
	})
	if err != nil {
		return entity.MattermostTeamOperation{}, errors.New("defer Mattermost team create recovery")
	}
	operation.State = enum.MattermostTeamOperationState(state)
	operation.FailureCode, operation.RetryNotBefore = failureCode, retryNotBefore
	operation.RecoveryDeadline, operation.UpdatedAt, operation.LeaseToken = recoveryDeadline, updatedAt, ""
	return operation, nil
}

func (repository *Repository) MarkRepairRequired(ctx context.Context, operation entity.MattermostTeamOperation, code string) error {
	return repository.withScope(ctx, operation.Principal, pgx.ReadWrite, func(tx pgx.Tx) error {
		tag, err := execNamed(ctx, tx, operationMarkRepairSQL, operation.ID, code, operation.Fence,
			digest(operation.LeaseToken))
		if err != nil || tag.RowsAffected() != 1 {
			return errors.New("update Mattermost team operation")
		}
		fenceTag, fenceErr := execNamed(ctx, tx, createFenceTerminalSQL, operation.Principal.OrganizationID,
			operation.Principal.ProjectID, operation.ID, "REPAIR_REQUIRED")
		if fenceErr != nil || fenceTag.RowsAffected() != 1 {
			return errors.New("update Mattermost team create fence")
		}
		return nil
	})
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
		if err := queryRow(ctx, tx, providerWatermarkAdvanceSQL, operation.Principal.OrganizationID,
			operation.Principal.ProjectID).Scan(&generation); err != nil {
			return err
		}
		tag, err := execNamed(ctx, tx, operationAcceptSQL, operation.ID, storedTeam.Selector, team.ProviderTeamID,
			team.Status, team.ProviderSnapshotSHA256, team.ProviderCausalitySHA256, receipt, generation, team.CreatedAt, team.UpdatedAt,
			team.ObservedAt, operation.Fence, digest(operation.LeaseToken))
		if err != nil || tag.RowsAffected() != 1 {
			return errors.New("accept Mattermost team provider checkpoint")
		}
		fenceTag, fenceErr := execNamed(ctx, tx, createFenceAcceptSQL, operation.Principal.OrganizationID,
			operation.Principal.ProjectID, operation.ID, team.ProviderTeamID)
		if fenceErr != nil || fenceTag.RowsAffected() != 1 {
			return errors.New("accept Mattermost team create fence")
		}
		accepted, _, err = scanOperation(queryRow(ctx, tx, operationLockSQL, operation.ID))
		return err
	})
	if err != nil {
		return entity.MattermostTeamOperation{}, errors.New("save Mattermost team provider checkpoint")
	}
	return accepted, nil
}

func (repository *Repository) ClaimRecovery(ctx context.Context, owner string, lease time.Duration) (entity.MattermostTeamOperation, bool, error) {
	var organizationID, projectID sql.NullString
	if err := queryRow(ctx, repository.pool, nextWorkScopeSQL).Scan(&organizationID, &projectID); err != nil {
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
		operation, _, scanErr = scanOperation(queryRow(ctx, tx, operationClaimRecoverySQL,
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
		return queryRow(ctx, tx, providerWatermarkAdvanceSQL, principal.OrganizationID, principal.ProjectID).Scan(&generation)
	})
	if err != nil || generation == 0 {
		return 0, errors.New("advance Mattermost provider observation generation")
	}
	return generation, nil
}

func (repository *Repository) BeginMapping(ctx context.Context, operation entity.WorkspaceMappingOperation,
	owner string, lease, recoveryWindow time.Duration,
) (entity.WorkspaceMappingOperation, domainrepo.MappingDisposition, error) {
	token, err := newLeaseToken()
	if err != nil {
		return entity.WorkspaceMappingOperation{}, 0, err
	}
	disposition := domainrepo.MappingClaimed
	var stored entity.WorkspaceMappingOperation
	err = repository.withScope(ctx, operation.Principal, pgx.ReadWrite, func(tx pgx.Tx) error {
		tag, err := execNamed(ctx, tx, mappingOperationInsertSQL, operation.ID, operation.Principal.OrganizationID,
			operation.Principal.ProjectID, operation.Principal.ActorID, operation.Action,
			operation.IdempotencyKey, operation.RequestSHA256, operation.MappingID,
			operation.ExpectedVersion, operation.ExpectedGeneration, operation.DisplayName,
			operation.Team.Selector, operation.Team.ProviderTeamID, operation.Team.Status,
			operation.Team.ProviderSnapshotSHA256, operation.Team.CreatedAt, operation.Team.UpdatedAt,
			operation.Team.ObservedAt, owner, digest(token), interval(lease), interval(recoveryWindow),
			operation.CreateOperationID)
		if err != nil {
			return err
		}
		var leaseActive bool
		stored, leaseActive, err = scanMappingOperation(queryRow(ctx, tx, mappingOperationLockSQL, operation.ID))
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
		tag, err = execNamed(ctx, tx, mappingOperationReclaimSQL, stored.ID, owner, digest(token), interval(lease))
		if err != nil || tag.RowsAffected() != 1 {
			disposition = domainrepo.MappingBusy
			return err
		}
		stored, _, err = scanMappingOperation(queryRow(ctx, tx, mappingOperationLockSQL, stored.ID))
		stored.LeaseToken = token
		return err
	})
	if errors.Is(err, domainrepo.ErrIdempotencyConflict) {
		return entity.WorkspaceMappingOperation{}, 0, err
	}
	if err != nil {
		return entity.WorkspaceMappingOperation{}, 0, errors.New("begin Workspace Mattermost mapping operation")
	}
	if stored.ID != operation.ID || stored.Principal != operation.Principal ||
		stored.IdempotencyKey != operation.IdempotencyKey || stored.Action != operation.Action {
		return entity.WorkspaceMappingOperation{}, 0, errors.New("Workspace Mattermost mapping checkpoint is invalid")
	}
	return stored, disposition, nil
}

func (repository *Repository) GetMappingOperation(ctx context.Context, principal entity.TeamPrincipal,
	action, idempotencyKey string,
) (entity.WorkspaceMappingOperation, error) {
	var operation entity.WorkspaceMappingOperation
	err := repository.withScope(ctx, principal, pgx.ReadOnly, func(tx pgx.Tx) error {
		var scanErr error
		operation, _, scanErr = scanMappingOperation(queryRow(ctx, tx, mappingOperationGetSQL,
			principal.OrganizationID, principal.ProjectID, principal.ActorID, action, idempotencyKey))
		return scanErr
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.WorkspaceMappingOperation{}, domainrepo.ErrNotFound
	}
	if err != nil {
		return entity.WorkspaceMappingOperation{}, errors.New("read Workspace mapping operation")
	}
	return operation, nil
}

// PrepareMappingAttempt атомарно фиксирует fresh Team/member snapshot,
// monotonic generation и новый one-use JTI непосредственно перед owner RPC.
func (repository *Repository) PrepareMappingAttempt(ctx context.Context,
	operation entity.WorkspaceMappingOperation, team entity.MattermostTeam, _ time.Duration,
) (entity.WorkspaceMappingOperation, error) {
	var refreshed entity.WorkspaceMappingOperation
	err := repository.withScope(ctx, operation.Principal, pgx.ReadWrite, func(tx pgx.Tx) error {
		var generation uint64
		if err := queryRow(ctx, tx, providerWatermarkAdvanceSQL, operation.Principal.OrganizationID,
			operation.Principal.ProjectID).Scan(&generation); err != nil {
			return err
		}
		if generation <= operation.EffectGeneration {
			return errors.New("provider observation generation did not advance")
		}
		tag, err := execNamed(ctx, tx, mappingOperationPrepareSQL, operation.ID,
			operation.Fence, digest(operation.LeaseToken), team.Selector, team.ProviderTeamID,
			team.Status, team.ProviderSnapshotSHA256, team.CreatedAt, team.UpdatedAt, team.ObservedAt,
			generation, uuid.NewString())
		if err != nil || tag.RowsAffected() != 1 {
			return errors.New("refresh Workspace mapping provider receipt")
		}
		refreshed, _, err = scanMappingOperation(queryRow(ctx, tx, mappingOperationLockSQL, operation.ID))
		if err == nil {
			refreshed.LeaseToken = operation.LeaseToken
		}
		return err
	})
	if err != nil {
		return entity.WorkspaceMappingOperation{}, errors.New("prepare Workspace mapping provider receipt")
	}
	return refreshed, nil
}

func (repository *Repository) DeferMappingRecovery(ctx context.Context, operation entity.WorkspaceMappingOperation,
	code string, retryDelay time.Duration,
) (entity.WorkspaceMappingOperation, error) {
	var state, failureCode string
	var retryNotBefore, recoveryDeadline, updatedAt time.Time
	err := repository.withScope(ctx, operation.Principal, pgx.ReadWrite, func(tx pgx.Tx) error {
		if err := queryRow(ctx, tx, mappingOperationMarkAmbiguousSQL, operation.ID, code, interval(retryDelay),
			operation.Fence, digest(operation.LeaseToken)).Scan(&state, &failureCode, &retryNotBefore,
			&recoveryDeadline, &updatedAt); err != nil {
			return err
		}
		if state == enum.WorkspaceMappingOperationRepairRequired && operation.CreateOperationID != "" {
			tag, err := execNamed(ctx, tx, createFenceTerminalSQL, operation.Principal.OrganizationID,
				operation.Principal.ProjectID, operation.CreateOperationID, "REPAIR_REQUIRED")
			if err != nil || tag.RowsAffected() != 1 {
				return errors.New("update expired Mattermost team create fence")
			}
		}
		return nil
	})
	if err != nil {
		return entity.WorkspaceMappingOperation{}, errors.New("defer Workspace mapping recovery")
	}
	operation.State, operation.FailureCode = state, failureCode
	operation.RetryNotBefore, operation.RecoveryDeadline = retryNotBefore, recoveryDeadline
	operation.UpdatedAt, operation.LeaseToken = updatedAt, ""
	return operation, nil
}

func (repository *Repository) MarkMappingTerminal(ctx context.Context, operation entity.WorkspaceMappingOperation,
	mapping entity.WorkspaceMattermostMapping, routes []entity.MattermostRuntimeRoute,
) error {
	state := enum.WorkspaceMappingOperationBound
	if mapping.State == "UNLINKED" {
		state = enum.WorkspaceMappingOperationUnlinked
	}
	return repository.withScope(ctx, operation.Principal, pgx.ReadWrite, func(tx pgx.Tx) error {
		if mapping.State == "UNLINKED" {
			if err := deleteRuntimeRoutes(ctx, tx, operation.Principal, mapping); err != nil {
				return err
			}
		} else if err := replaceRuntimeRoutes(ctx, tx, operation.Principal, mapping, routes); err != nil {
			return err
		}
		tag, err := execNamed(ctx, tx, mappingOperationMarkTerminalSQL,
			operation.ID, state, mapping.ID, mapping.Version, mapping.Generation,
			mapping.ProviderEffectVersion, mapping.ProviderEffectGeneration, mapping.ProviderObservedAt,
			mapping.UpdatedAt, operation.Fence, digest(operation.LeaseToken))
		if err != nil || tag.RowsAffected() != 1 {
			return errors.New("update Workspace Mattermost mapping operation")
		}
		if operation.CreateOperationID != "" {
			fenceState := "BOUND"
			if mapping.State == "UNLINKED" {
				fenceState = "UNLINKED"
			}
			fenceTag, fenceErr := execNamed(ctx, tx, createFenceTerminalSQL, operation.Principal.OrganizationID,
				operation.Principal.ProjectID, operation.CreateOperationID, fenceState)
			if fenceErr != nil || fenceTag.RowsAffected() != 1 {
				return errors.New("update Mattermost team create fence")
			}
		}
		if mapping.State == "UNLINKED" {
			if _, err := execNamed(ctx, tx, createFenceUnlinkSQL, operation.Principal.OrganizationID,
				operation.Principal.ProjectID, operation.Team.ProviderTeamID); err != nil {
				return errors.New("unlink Mattermost team create fence")
			}
		}
		return nil
	})
}

func (repository *Repository) ReconcileRuntimeRoutes(ctx context.Context, principal entity.TeamPrincipal,
	mapping entity.WorkspaceMattermostMapping, routes []entity.MattermostRuntimeRoute,
) error {
	return repository.withScope(ctx, principal, pgx.ReadWrite, func(tx pgx.Tx) error {
		return replaceRuntimeRoutes(ctx, tx, principal, mapping, routes)
	})
}

func replaceRuntimeRoutes(ctx context.Context, tx pgx.Tx, principal entity.TeamPrincipal,
	mapping entity.WorkspaceMattermostMapping, routes []entity.MattermostRuntimeRoute,
) error {
	if mapping.State != "BOUND" || len(routes) == 0 {
		return errors.New("Mattermost joined runtime route input is invalid")
	}
	if err := lockRuntimeRouteGeneration(ctx, tx, principal, mapping); err != nil {
		return err
	}
	if _, err := execNamed(ctx, tx, runtimeRouteDeleteProjectSQL, principal.OrganizationID,
		principal.ProjectID); err != nil {
		return err
	}
	for _, route := range routes {
		if route.Principal != principal || route.MappingID != mapping.ID ||
			route.MappingVersion != mapping.Version || route.MappingGeneration != mapping.Generation ||
			route.MappingDigestSHA256 != runtimeMappingDigest(mapping) {
			return errors.New("Mattermost joined runtime route mismatch")
		}
		if _, err := execNamed(ctx, tx, runtimeRouteInsertSQL, route.TemplateKey,
			route.Principal.OrganizationID, route.Principal.ProjectID, route.Principal.ActorID,
			route.MappingID, route.MappingVersion, route.MappingGeneration, route.MappingDigestSHA256,
			route.ProviderTeamID, route.ProviderSnapshotSHA256, route.Boundary.ChatID,
			route.Boundary.RoleID, route.Boundary.Locale, route.Boundary.BotStableKey,
			route.Boundary.ChannelID, route.Boundary.SessionID, route.OwnerDelivery,
			route.RouteDigestSHA256); err != nil {
			return err
		}
	}
	return saveRuntimeCheckpoint(ctx, tx, principal, mapping)
}

func deleteRuntimeRoutes(ctx context.Context, tx pgx.Tx, principal entity.TeamPrincipal,
	mapping entity.WorkspaceMattermostMapping,
) error {
	if err := lockRuntimeRouteGeneration(ctx, tx, principal, mapping); err != nil {
		return err
	}
	if _, err := execNamed(ctx, tx, runtimeRouteDeleteProjectSQL, principal.OrganizationID, principal.ProjectID); err != nil {
		return err
	}
	return saveRuntimeCheckpoint(ctx, tx, principal, mapping)
}

func saveRuntimeCheckpoint(ctx context.Context, tx pgx.Tx, principal entity.TeamPrincipal,
	mapping entity.WorkspaceMattermostMapping,
) error {
	tag, err := execNamed(ctx, tx, runtimeCheckpointUpsertSQL, principal.OrganizationID,
		principal.ProjectID, mapping.ID, mapping.Version, mapping.Generation, mapping.State,
		runtimeMappingDigest(mapping))
	if err != nil || tag.RowsAffected() != 1 {
		return errors.New("save Mattermost runtime route checkpoint")
	}
	return nil
}

func lockRuntimeRouteGeneration(ctx context.Context, tx pgx.Tx, principal entity.TeamPrincipal,
	mapping entity.WorkspaceMattermostMapping,
) error {
	var currentGeneration uint64
	var currentDigest string
	err := queryRow(ctx, tx, runtimeRouteLockProjectSQL, principal.OrganizationID, principal.ProjectID).
		Scan(&currentGeneration, &currentDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	incomingDigest := runtimeMappingDigest(mapping)
	if currentGeneration > mapping.Generation || currentGeneration == mapping.Generation && currentDigest != incomingDigest {
		return errors.New("Mattermost runtime route generation is stale")
	}
	return nil
}

func (repository *Repository) MarkMappingRepairRequired(ctx context.Context,
	operation entity.WorkspaceMappingOperation, code string,
) error {
	return repository.withScope(ctx, operation.Principal, pgx.ReadWrite, func(tx pgx.Tx) error {
		tag, err := execNamed(ctx, tx, mappingOperationMarkRepairSQL, operation.ID, code, operation.Fence,
			digest(operation.LeaseToken))
		if err != nil || tag.RowsAffected() != 1 {
			return errors.New("update Workspace Mattermost mapping operation")
		}
		if operation.CreateOperationID != "" {
			fenceTag, fenceErr := execNamed(ctx, tx, createFenceTerminalSQL, operation.Principal.OrganizationID,
				operation.Principal.ProjectID, operation.CreateOperationID, "REPAIR_REQUIRED")
			if fenceErr != nil || fenceTag.RowsAffected() != 1 {
				return errors.New("update Mattermost team create fence")
			}
		}
		return nil
	})
}

func (repository *Repository) ClaimMappingRecovery(ctx context.Context, owner string,
	lease time.Duration,
) (entity.WorkspaceMappingOperation, bool, error) {
	var organizationID, projectID sql.NullString
	if err := queryRow(ctx, repository.pool, mappingNextWorkScopeSQL).Scan(&organizationID, &projectID); err != nil {
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
		if err := queryRow(ctx, tx, mappingOperationClaimRecoverySQL, owner, digest(token), interval(lease)).Scan(&operationID); err != nil {
			return err
		}
		var scanErr error
		operation, _, scanErr = scanMappingOperation(queryRow(ctx, tx, mappingOperationLockSQL, operationID))
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

func (repository *Repository) ResolveRuntimeRoute(ctx context.Context, teamID,
	channelID string,
) (entity.MattermostRuntimeRoute, error) {
	var organizationID, projectID string
	if err := queryRow(ctx, repository.pool, runtimeRouteScopeSQL, teamID, channelID).Scan(&organizationID, &projectID); err != nil {
		return entity.MattermostRuntimeRoute{}, domainrepo.ErrNotFound
	}
	if organizationID != repository.config.OrganizationID || !validUUID(projectID) {
		return entity.MattermostRuntimeRoute{}, domainrepo.ErrNotFound
	}
	principal := entity.TeamPrincipal{OrganizationID: organizationID, ProjectID: projectID,
		ActorID: "system:interaction-runtime-route"}
	var route entity.MattermostRuntimeRoute
	err := repository.withScope(ctx, principal, pgx.ReadOnly, func(tx pgx.Tx) error {
		return scanRuntimeRoute(queryRow(ctx, tx, runtimeRouteResolveSQL, teamID, channelID), &route)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.MattermostRuntimeRoute{}, domainrepo.ErrNotFound
	}
	if err != nil {
		return entity.MattermostRuntimeRoute{}, errors.New("resolve Mattermost runtime route")
	}
	return route, nil
}

func (repository *Repository) ResolveRuntimeDelivery(ctx context.Context, projectID, chatID,
	_ string,
) (entity.MattermostRuntimeRoute, error) {
	if !validUUID(projectID) {
		return entity.MattermostRuntimeRoute{}, domainrepo.ErrNotFound
	}
	principal := entity.TeamPrincipal{OrganizationID: repository.config.OrganizationID, ProjectID: projectID,
		ActorID: "system:interaction-runtime-delivery"}
	var route entity.MattermostRuntimeRoute
	err := repository.withScope(ctx, principal, pgx.ReadOnly, func(tx pgx.Tx) error {
		return scanRuntimeRoute(queryRow(ctx, tx, runtimeRouteDeliverySQL, projectID, chatID), &route)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.MattermostRuntimeRoute{}, domainrepo.ErrNotFound
	}
	if err != nil {
		return entity.MattermostRuntimeRoute{}, errors.New("resolve Mattermost runtime delivery")
	}
	return route, nil
}

func (repository *Repository) ListRuntimeRoutes(ctx context.Context) ([]entity.MattermostRuntimeRoute, error) {
	result := make([]entity.MattermostRuntimeRoute, 0)
	for _, projectID := range repository.config.AllowedProjectIDs {
		principal := entity.TeamPrincipal{OrganizationID: repository.config.OrganizationID, ProjectID: projectID,
			ActorID: "system:interaction-runtime-readiness"}
		err := repository.withScope(ctx, principal, pgx.ReadOnly, func(tx pgx.Tx) error {
			rows, err := queryNamed(ctx, tx, runtimeRouteListSQL)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var route entity.MattermostRuntimeRoute
				if err := scanRuntimeRoute(rows, &route); err != nil {
					return err
				}
				result = append(result, route)
			}
			return rows.Err()
		})
		if err != nil {
			return nil, errors.New("list Mattermost runtime routes")
		}
	}
	return result, nil
}

func (repository *Repository) GetRuntimeAdmission(ctx context.Context, principal entity.TeamPrincipal,
	providerTeamID string,
) (entity.MattermostRuntimeRoute, error) {
	var route entity.MattermostRuntimeRoute
	err := repository.withScope(ctx, principal, pgx.ReadOnly, func(tx pgx.Tx) error {
		return scanRuntimeRoute(queryRow(ctx, tx, runtimeRouteAdmissionSQL, principal.OrganizationID,
			principal.ProjectID, principal.ActorID, providerTeamID), &route)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.MattermostRuntimeRoute{}, domainrepo.ErrNotFound
	}
	if err != nil {
		return entity.MattermostRuntimeRoute{}, errors.New("read Mattermost runtime admission")
	}
	return route, nil
}

func scanRuntimeRoute(row rowScanner, route *entity.MattermostRuntimeRoute) error {
	if err := row.Scan(&route.TemplateKey, &route.Principal.OrganizationID, &route.Principal.ProjectID,
		&route.Principal.ActorID, &route.MappingID, &route.MappingVersion, &route.MappingGeneration,
		&route.MappingDigestSHA256, &route.ProviderTeamID, &route.ProviderSnapshotSHA256,
		&route.Boundary.ChatID, &route.Boundary.RoleID, &route.Boundary.Locale,
		&route.Boundary.BotStableKey, &route.Boundary.ChannelID, &route.Boundary.SessionID,
		&route.OwnerDelivery, &route.RouteDigestSHA256, &route.CreatedAt, &route.UpdatedAt); err != nil {
		return err
	}
	route.Boundary.OrganizationID = route.Principal.OrganizationID
	route.Boundary.ProjectID = route.Principal.ProjectID
	route.Boundary.MappingOwnerActorID = route.Principal.ActorID
	route.Boundary.TeamID = route.ProviderTeamID
	return nil
}

func (repository *Repository) withScope(ctx context.Context, principal entity.TeamPrincipal,
	access pgx.TxAccessMode, run func(pgx.Tx) error,
) error {
	if access == pgx.ReadOnly {
		// Durable RLS scope activation writes a transaction-bound context row.
		access = pgx.ReadWrite
	}
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
	if _, err := execNamed(ctx, tx, activateScopeSQL, principal.OrganizationID, principal.ProjectID, principal.ActorID); err != nil {
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
	if err := queryRow(ctx, tx, selectorUpsertSQL, selectorID, principal.OrganizationID, principal.ProjectID,
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
		&operation.Intent.ProviderCorrelation, &operation.Intent.DisplayName, &operation.Intent.Slug, &state, &operation.Team.Selector,
		&operation.Team.ProviderTeamID, &status, &operation.Team.ProviderSnapshotSHA256,
		&operation.ProviderCausalitySHA256, &operation.ProviderReceiptSHA256, &operation.ProviderGeneration, &operation.FailureCode,
		&operation.Fence, &operation.EffectStartedAt, &operation.RetryNotBefore,
		&operation.RecoveryDeadline, &operation.CreatedAt, &operation.UpdatedAt, &operation.Team.CreatedAt,
		&operation.Team.UpdatedAt, &operation.Team.ObservedAt, &leaseActive); err != nil {
		return entity.MattermostTeamOperation{}, false, err
	}
	operation.State = enum.MattermostTeamOperationState(state)
	operation.Team.Status = enum.MattermostTeamStatus(status)
	operation.Team.ProviderCausalitySHA256 = operation.ProviderCausalitySHA256
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
		&operation.Fence, &operation.RetryNotBefore, &operation.RecoveryDeadline,
		&operation.CreateOperationID, &operation.CreatedAt, &operation.UpdatedAt,
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

func runtimeMappingDigest(mapping entity.WorkspaceMattermostMapping) string {
	return digest(strings.Join([]string{
		"workspace-mattermost-mapping-state-v1", mapping.ID, mapping.State, mapping.ProviderTeamID,
		fmt.Sprint(mapping.Version), fmt.Sprint(mapping.Generation),
		fmt.Sprint(mapping.ProviderEffectVersion), fmt.Sprint(mapping.ProviderEffectGeneration),
	}, "\x00"))
}

func interval(value time.Duration) string {
	return fmt.Sprintf("%d microseconds", value.Microseconds())
}
