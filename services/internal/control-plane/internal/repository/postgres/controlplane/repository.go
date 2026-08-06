// Package controlplane реализует адаптер PostgreSQL для доменного порта репозитория.
package controlplane

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/eventing"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/authorization/mattermostevent"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/authorization/readbackgrant"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/event"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	domainquery "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/schema"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	transactionAttempts = 3
	nullActorID         = "00000000-0000-0000-0000-000000000000"
)

// Config связывает каждый оператор SQL с дополнительным контекстом выполнения.
type Config struct {
	PrincipalName       string
	PrincipalGeneration uint64
	ContextKeyID        string
	ContextSigningKey   []byte
	ContextTTL          time.Duration
}

// Repository владеет пулом выполнения; вызывающая сторона закрывает его после
// завершения обработчиков.
type Repository struct {
	pool   *pgxpool.Pool
	config Config
}

type transaction struct {
	tx             pgx.Tx
	organizationID string
	projectID      string
}

// AdmitMattermostEventKeyset фиксирует verifier-owned monotonic fence до
// открытия рабочего MATTERMOST_SIGNED_EVENT пути.
func (repository *Repository) AdmitMattermostEventKeyset(
	ctx context.Context,
	producerID string,
	revision, highWatermark, servedGeneration uint64,
	digest string,
	retired, active []int64,
	identities []mattermostevent.KeyIdentity,
) error {
	identityJSON, err := json.Marshal(identities)
	if err != nil {
		return errors.New("encode Mattermost event key identities")
	}
	var storedRevision, storedHighWatermark, storedServedGeneration uint64
	var storedDigest string
	var storedRetired []int64
	err = repository.pool.QueryRow(ctx, sqlMattermostEventKeysetAdmit, pgx.StrictNamedArgs{
		"producer_id": producerID, "keyset_revision": revision,
		"high_watermark": highWatermark, "served_generation": servedGeneration,
		"keyset_sha256": digest, "retired_generations": retired, "active_generations": active,
		"key_identities": identityJSON,
	}).Scan(&storedRevision, &storedHighWatermark, &storedServedGeneration, &storedDigest, &storedRetired)
	if err != nil || storedRevision != revision || storedHighWatermark != highWatermark ||
		storedServedGeneration != servedGeneration || storedDigest != digest ||
		!sliceContainsAll(storedRetired, retired) {
		return errors.New("admit Mattermost event keyset")
	}
	return nil
}

func sliceContainsAll(values, required []int64) bool {
	for _, candidate := range required {
		if !slices.Contains(values, candidate) {
			return false
		}
	}
	return true
}

// AdmitInteractionReadbackKeyset фиксирует issuer-owned served state и
// immutable key identities до выдачи первого credential.
func (repository *Repository) AdmitInteractionReadbackKeyset(ctx context.Context, revision, highWatermark,
	servedGeneration uint64, digest string, identities []readbackgrant.KeyIdentity) error {
	encoded, err := json.Marshal(identities)
	if err != nil {
		return errors.New("encode interaction readback key identities")
	}
	var storedRevision, storedHighWatermark, storedServed uint64
	var storedDigest string
	if err := repository.pool.QueryRow(ctx, sqlInteractionDeliveryReadbackKeysetAdmit, pgx.StrictNamedArgs{
		"keyset_revision": revision, "high_watermark": highWatermark,
		"served_generation": servedGeneration, "keyset_sha256": digest, "key_identities": encoded,
	}).Scan(&storedRevision, &storedHighWatermark, &storedServed, &storedDigest); err != nil ||
		storedRevision != revision || storedHighWatermark != highWatermark ||
		storedServed != servedGeneration || storedDigest != digest {
		return errors.New("admit interaction delivery readback keyset")
	}
	return nil
}

func (wrapped *transaction) CurrentTime(ctx context.Context) (time.Time, error) {
	var current time.Time
	if err := wrapped.tx.QueryRow(ctx, sqlClockGet).Scan(&current); err != nil {
		return time.Time{}, mapError(err)
	}
	return current.UTC().Truncate(time.Microsecond), nil
}

var (
	_ domainrepo.Repository  = (*Repository)(nil)
	_ domainrepo.Transaction = (*transaction)(nil)
)

// New создаёт репозиторий выполнения.
func New(pool *pgxpool.Pool, config Config) (*Repository, error) {
	if pool == nil || config.PrincipalName == "" ||
		config.PrincipalGeneration == 0 || config.ContextKeyID == "" ||
		len(config.ContextSigningKey) < 32 ||
		config.ContextTTL < time.Second || config.ContextTTL > 10*time.Second {
		return nil, errors.New("control-plane PostgreSQL pool is required")
	}
	return &Repository{pool: pool, config: config}, nil
}

// Transact выполняет сериализуемую команду с локальной для транзакции областью RLS.
func (repository *Repository) Transact(
	ctx context.Context,
	scope domainrepo.Scope,
	callback func(domainrepo.Transaction) error,
) error {
	if callback == nil {
		return errs.ErrInternal
	}
	var last error
	for attempt := 0; attempt < transactionAttempts; attempt++ {
		tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{
			IsoLevel:   pgx.Serializable,
			AccessMode: pgx.ReadWrite,
		})
		if err != nil {
			return mapError(err)
		}
		wrapped := &transaction{
			tx:             tx,
			organizationID: scope.OrganizationID,
			projectID:      scope.ProjectID,
		}
		if err := repository.setScope(ctx, tx, scope); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		err = callback(wrapped)
		if err == nil {
			err = tx.Commit(ctx)
		} else {
			_ = tx.Rollback(ctx)
		}
		if !retryableTransaction(err) {
			return mapError(err)
		}
		last = err
	}
	return fmt.Errorf("%w: transaction retry exhausted: %v", errs.ErrUnavailable, last)
}

// Get выполняет авторитетный поиск с фильтром владения.
func (repository *Repository) Get(
	ctx context.Context,
	organizationID, projectID, resourceID string,
	expectedKind enum.Kind,
) (entity.Resource, error) {
	var resource entity.Resource
	err := repository.read(ctx, organizationID, projectID, nullActorID, func(tx pgx.Tx) error {
		var scanErr error
		resource, scanErr = scanResource(tx.QueryRow(
			ctx,
			sqlResourceGet,
			pgx.StrictNamedArgs{
				"organization_id": organizationID,
				"project_id":      projectID,
				"resource_id":     resourceID,
			},
		))
		if scanErr == nil && resource.Kind != expectedKind {
			return errs.ErrNotFound
		}
		return scanErr
	})
	return resource, err
}

// GetIncludingDeleted доступен специализированному lifecycle path для
// idempotency readback уже завершённого delete.
func (repository *Repository) GetIncludingDeleted(
	ctx context.Context,
	organizationID, projectID, resourceID string,
	expectedKind enum.Kind,
) (entity.Resource, error) {
	var resource entity.Resource
	err := repository.read(ctx, organizationID, projectID, nullActorID, func(tx pgx.Tx) error {
		var scanErr error
		resource, scanErr = scanResource(tx.QueryRow(ctx, sqlResourceGetIncludingDeleted, pgx.StrictNamedArgs{
			"organization_id": organizationID, "project_id": projectID, "resource_id": resourceID,
		}))
		if scanErr == nil && resource.Kind != expectedKind {
			return errs.ErrNotFound
		}
		return scanErr
	})
	return resource, err
}

// List возвращает страницу с устойчивым курсором UUID.
func (repository *Repository) List(
	ctx context.Context,
	filter domainquery.ResourceFilter,
) ([]entity.Resource, error) {
	var resources []entity.Resource
	err := repository.read(
		ctx,
		filter.OrganizationID,
		filter.ProjectID,
		filter.ActorID,
		func(tx pgx.Tx) error {
			states := make([]string, 0, len(filter.States))
			for _, state := range filter.States {
				states = append(states, string(state))
			}
			rows, err := tx.Query(
				ctx,
				sqlResourceList,
				pgx.StrictNamedArgs{
					"organization_id": filter.OrganizationID,
					"project_id":      filter.ProjectID,
					"actor_id":        filter.ActorID,
					"kind":            string(filter.Kind),
					"parent_id":       filter.ParentID,
					"states":          states,
					"after_id":        filter.AfterID,
					"limit":           filter.Limit,
				},
			)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				resource, err := scanResource(rows)
				if err != nil {
					return err
				}
				resources = append(resources, resource)
			}
			return rows.Err()
		},
	)
	return resources, err
}

func (repository *Repository) Search(
	ctx context.Context,
	filter domainquery.ResourceSearch,
) ([]entity.Resource, error) {
	var resources []entity.Resource
	err := repository.read(
		ctx,
		filter.OrganizationID,
		filter.ProjectID,
		filter.ActorID,
		func(tx pgx.Tx) error {
			states := make([]string, 0, len(filter.States))
			for _, state := range filter.States {
				states = append(states, string(state))
			}
			rows, err := tx.Query(
				ctx,
				sqlResourceSearch,
				pgx.StrictNamedArgs{
					"organization_id": filter.OrganizationID,
					"project_id":      filter.ProjectID,
					"actor_id":        filter.ActorID,
					"kind":            string(filter.Kind),
					"states":          states,
					"query":           filter.Query,
					"after_id":        filter.AfterID,
					"limit":           filter.Limit,
				},
			)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				resource, err := scanResource(rows)
				if err != nil {
					return err
				}
				resources = append(resources, resource)
			}
			return rows.Err()
		},
	)
	return resources, err
}

func (wrapped *transaction) SearchMemory(
	ctx context.Context,
	search domainrepo.MemorySearch,
) ([]domainrepo.MemorySearchHit, error) {
	var hits []domainrepo.MemorySearchHit
	rows, err := wrapped.tx.Query(
		ctx,
		sqlMemorySearch,
		pgx.StrictNamedArgs{
			"organization_id":       search.OrganizationID,
			"project_id":            search.ProjectID,
			"scope":                 search.Scope,
			"role_id":               search.RoleID,
			"query":                 search.Query,
			"query_embedding":       formatVector(search.QueryEmbedding),
			"model_id":              search.ModelID,
			"model_revision":        search.ModelRevision,
			"model_sha256":          search.ModelSHA256,
			"after_id":              search.AfterID,
			"after_text_rank":       search.AfterTextRank,
			"after_vector_distance": search.AfterVectorDistance,
			"after_vector_used":     search.AfterVectorUsed,
			"limit":                 search.Limit,
			"can_read_project":      search.CanReadProject,
			"actor_role_ids":        search.ActorRoleIDs,
			"parent_id":             search.ParentID,
			"states":                statesAsStrings(search.States),
			"generic_order":         search.GenericOrder,
		},
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	for rows.Next() {
		hit, err := scanMemorySearchHit(rows)
		if err != nil {
			return nil, err
		}
		hits = append(hits, hit)
	}
	return hits, mapError(rows.Err())
}

func statesAsStrings(states []enum.State) []string {
	result := make([]string, 0, len(states))
	for _, state := range states {
		result = append(result, string(state))
	}
	return result
}

// ListEligibleProjects возвращает только проекты, видимые владельцу или участнику.
func (repository *Repository) ListEligibleProjects(
	ctx context.Context,
	organizationID, actorID, afterID string,
	limit int,
) ([]entity.Resource, error) {
	var resources []entity.Resource
	err := repository.read(ctx, organizationID, "", actorID, func(tx pgx.Tx) error {
		rows, err := tx.Query(
			ctx,
			sqlProjectListEligible,
			pgx.StrictNamedArgs{
				"organization_id": organizationID,
				"actor_id":        actorID,
				"after_id":        afterID,
				"limit":           limit,
			},
		)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			resource, err := scanResource(rows)
			if err != nil {
				return err
			}
			resources = append(resources, resource)
		}
		return rows.Err()
	})
	return resources, err
}

func (repository *Repository) ListAudit(
	ctx context.Context,
	filter domainquery.AuditFilter,
) ([]domainrepo.Audit, error) {
	var events []domainrepo.Audit
	err := repository.read(
		ctx,
		filter.OrganizationID,
		filter.ProjectID,
		nullActorID,
		func(tx pgx.Tx) error {
			rows, err := tx.Query(
				ctx,
				sqlAuditList,
				pgx.StrictNamedArgs{
					"organization_id": filter.OrganizationID,
					"project_id":      filter.ProjectID,
					"resource_kind":   string(filter.ResourceKind),
					"resource_id":     filter.ResourceID,
					"action":          filter.Action,
					"after_id":        filter.AfterID,
					"limit":           filter.Limit,
				},
			)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var event domainrepo.Audit
				if err := rows.Scan(
					&event.ID,
					&event.OrganizationID,
					&event.ProjectID,
					&event.ActorID,
					&event.Action,
					&event.ResourceID,
					&event.ResourceKind,
					&event.ResourceVersion,
					&event.Outcome,
					&event.CorrelationID,
					&event.PolicyRevision,
					&event.OccurredAt,
				); err != nil {
					return err
				}
				events = append(events, event)
			}
			return rows.Err()
		},
	)
	return events, err
}

func (repository *Repository) ListRuntimeIncidents(
	ctx context.Context,
	filter domainquery.RuntimeIncidentFilter,
) ([]domainrepo.RuntimeIncident, error) {
	var incidents []domainrepo.RuntimeIncident
	err := repository.read(ctx, filter.OrganizationID, filter.ProjectID, filter.ActorID,
		func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, sqlRuntimeIncidentList, pgx.StrictNamedArgs{
				"organization_id": filter.OrganizationID, "project_id": filter.ProjectID,
				"actor_id": filter.ActorID, "after_id": filter.AfterID, "limit": filter.Limit,
			})
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var incident domainrepo.RuntimeIncident
				if err := rows.Scan(&incident.ID, &incident.OrganizationID, &incident.ProjectID,
					&incident.ExecutionID, &incident.ExecutionFence, &incident.Kind,
					&incident.EvidenceSHA256, &incident.WorkloadID, &incident.OccurredAt); err != nil {
					return err
				}
				incident.OccurredAt = incident.OccurredAt.UTC()
				incidents = append(incidents, incident)
			}
			return rows.Err()
		})
	return incidents, err
}

func (repository *Repository) ListBackups(
	ctx context.Context,
	organizationID, projectID, actorID, afterID string,
	limit int,
) ([]domainrepo.Backup, error) {
	var backups []domainrepo.Backup
	err := repository.read(ctx, organizationID, projectID, actorID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, sqlRuntimeBackupList, pgx.StrictNamedArgs{
			"organization_id": organizationID,
			"project_id":      projectID,
			"actor_id":        actorID,
			"after_id":        afterID,
			"limit":           limit,
		})
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			backup, scanErr := scanBackup(rows)
			if scanErr != nil {
				return scanErr
			}
			backups = append(backups, backup)
		}
		return rows.Err()
	})
	return backups, err
}

func (repository *Repository) GetBackup(
	ctx context.Context,
	organizationID, projectID, actorID, backupID string,
) (domainrepo.Backup, error) {
	var backup domainrepo.Backup
	err := repository.read(ctx, organizationID, projectID, actorID, func(tx pgx.Tx) error {
		var scanErr error
		backup, scanErr = scanBackup(tx.QueryRow(ctx, sqlRuntimeBackupGet, pgx.StrictNamedArgs{
			"organization_id": organizationID,
			"project_id":      projectID,
			"actor_id":        actorID,
			"backup_id":       backupID,
		}))
		return scanErr
	})
	return backup, err
}

func (repository *Repository) GetRuntimeRestoreOperation(
	ctx context.Context,
	organizationID, projectID, actorID, operationID string,
) (domainrepo.RuntimeRestoreOperation, error) {
	var operation domainrepo.RuntimeRestoreOperation
	err := repository.read(ctx, organizationID, projectID, actorID, func(tx pgx.Tx) error {
		var scanErr error
		operation, scanErr = scanRuntimeRestoreOperation(tx.QueryRow(
			ctx, sqlRuntimeRestoreOperationOwnerGet, pgx.StrictNamedArgs{
				"organization_id": organizationID,
				"project_id":      projectID,
				"actor_id":        actorID,
				"id":              operationID,
			},
		))
		return scanErr
	})
	return operation, err
}

func (repository *Repository) ListRuntimeRestoreOperations(
	ctx context.Context,
	organizationID, projectID, actorID, backupID, afterID string,
	limit int,
) ([]domainrepo.RuntimeRestoreOperation, error) {
	var operations []domainrepo.RuntimeRestoreOperation
	err := repository.read(ctx, organizationID, projectID, actorID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, sqlRuntimeRestoreOperationOwnerList, pgx.StrictNamedArgs{
			"organization_id": organizationID, "project_id": projectID, "actor_id": actorID,
			"backup_id": backupID, "after_id": afterID, "limit": limit,
		})
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			operation, scanErr := scanRuntimeRestoreOperation(rows)
			if scanErr != nil {
				return scanErr
			}
			operations = append(operations, operation)
		}
		return rows.Err()
	})
	return operations, err
}

func (repository *Repository) ListTombstones(
	ctx context.Context,
	filter domainquery.TombstoneFilter,
) ([]domainrepo.Tombstone, error) {
	var tombstones []domainrepo.Tombstone
	err := repository.read(
		ctx,
		filter.OrganizationID,
		filter.ProjectID,
		nullActorID,
		func(tx pgx.Tx) error {
			rows, err := tx.Query(
				ctx,
				sqlResourceListTombstones,
				pgx.StrictNamedArgs{
					"organization_id": filter.OrganizationID,
					"project_id":      filter.ProjectID,
					"kind":            string(filter.Kind),
					"after_id":        filter.AfterID,
					"limit":           filter.Limit,
				},
			)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				resource, err := scanResource(rows)
				if err != nil {
					return err
				}
				digest, err := entity.ProjectionSHA256(resource)
				if err != nil {
					return err
				}
				tombstones = append(tombstones, domainrepo.Tombstone{
					ResourceID:       resource.ID,
					Kind:             resource.Kind,
					Version:          resource.Version,
					ProjectionSHA256: digest,
					DeletedAt:        resource.UpdatedAt,
				})
			}
			return rows.Err()
		},
	)
	return tombstones, err
}

func (repository *Repository) ListScheduleOccurrences(
	ctx context.Context,
	filter domainquery.ScheduleOccurrenceFilter,
) ([]domainrepo.ScheduleOccurrence, error) {
	var occurrences []domainrepo.ScheduleOccurrence
	err := repository.read(
		ctx,
		filter.OrganizationID,
		filter.ProjectID,
		nullActorID,
		func(tx pgx.Tx) error {
			rows, err := tx.Query(
				ctx,
				sqlScheduleOccurrenceList,
				pgx.StrictNamedArgs{
					"organization_id": filter.OrganizationID,
					"project_id":      filter.ProjectID,
					"schedule_id":     filter.ScheduleID,
					"states":          filter.States,
					"after_id":        filter.AfterID,
					"limit":           filter.Limit,
				},
			)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				occurrence, err := scanScheduleOccurrence(rows)
				if err != nil {
					return err
				}
				occurrences = append(occurrences, occurrence)
			}
			return rows.Err()
		},
	)
	return occurrences, err
}

func (repository *Repository) Diagnostics(
	ctx context.Context,
	scope domainrepo.Scope,
) (domainrepo.Diagnostics, error) {
	var diagnostics domainrepo.Diagnostics
	var oldestSeconds float64
	err := repository.read(
		ctx,
		scope.OrganizationID,
		scope.ProjectID,
		scope.ActorID,
		func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, sqlDiagnosticsGet).Scan(
				&diagnostics.SchemaVersion,
				&diagnostics.PendingOutboxEvents,
				&diagnostics.TerminalOutboxEvents,
				&oldestSeconds,
				&diagnostics.ActiveTurnLeases,
				&diagnostics.QueuedScheduleOccurrences,
				&diagnostics.RuntimePrincipalStatus,
				&diagnostics.RuntimePrincipalGeneration,
			)
		},
	)
	diagnostics.OldestPendingAge = time.Duration(oldestSeconds * float64(time.Second))
	return diagnostics, err
}

// Check проверяет версию схемы и действующую непривилегированную роль выполнения.
func (repository *Repository) Check(ctx context.Context) error {
	var version uint64
	var generation uint64
	var status string
	var member, nonSuperuser, noBypassRLS, loginEnabled bool
	if err := repository.pool.QueryRow(ctx, sqlReadinessCheck).Scan(
		&version,
		&member,
		&nonSuperuser,
		&noBypassRLS,
		&status,
		&generation,
		&loginEnabled,
	); err != nil {
		return mapError(err)
	}
	if version != uint64(schema.CurrentVersion) || !member || !nonSuperuser || !noBypassRLS ||
		!loginEnabled || generation != repository.config.PrincipalGeneration ||
		(status != "CURRENT" && status != "NEXT" && status != "PREVIOUS") {
		return errs.ErrUnavailable
	}
	return nil
}

// CacheEpoch возвращает принадлежащую PostgreSQL версию инвалидации.
func (repository *Repository) CacheEpoch(
	ctx context.Context,
	organizationID, projectID string,
) (uint64, error) {
	var epoch uint64
	err := repository.read(ctx, organizationID, projectID, nullActorID, func(tx pgx.Tx) error {
		scanErr := tx.QueryRow(
			ctx,
			sqlCacheEpochGet,
			pgx.StrictNamedArgs{
				"organization_id": organizationID,
				"project_id":      projectID,
			},
		).Scan(&epoch)
		if errors.Is(scanErr, pgx.ErrNoRows) {
			epoch = 0
			return nil
		}
		return scanErr
	})
	return epoch, err
}

// Close освобождает пул выполнения.
func (repository *Repository) Close() {
	repository.pool.Close()
}

func (repository *Repository) read(
	ctx context.Context,
	organizationID, projectID, actorID string,
	callback func(pgx.Tx) error,
) error {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.ReadCommitted,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return mapError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.setScope(ctx, tx, domainrepo.Scope{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		ActorID:        actorID,
	}); err != nil {
		return err
	}
	if err := callback(tx); err != nil {
		return mapError(err)
	}
	return mapError(tx.Commit(ctx))
}

func (repository *Repository) setScope(
	ctx context.Context,
	tx pgx.Tx,
	scope domainrepo.Scope,
) error {
	actorID := scope.ActorID
	if actorID == "" {
		actorID = nullActorID
	}
	nonce := uuid.NewString()
	expiresAt := time.Now().UTC().Add(repository.config.ContextTTL)
	expiresUnixMicro := expiresAt.UnixMicro()
	canonical := "v1\n" +
		repository.config.PrincipalName + "\n" +
		strconv.FormatUint(repository.config.PrincipalGeneration, 10) + "\n" +
		scope.OrganizationID + "\n" +
		scope.ProjectID + "\n" +
		actorID + "\n" +
		nonce + "\n" +
		strconv.FormatInt(expiresUnixMicro, 10)
	mac := hmac.New(sha256.New, repository.config.ContextSigningKey)
	_, _ = mac.Write([]byte(canonical))
	if _, err := tx.Exec(
		ctx,
		sqlTransactionSetScope,
		pgx.StrictNamedArgs{
			"organization_id":      scope.OrganizationID,
			"project_id":           scope.ProjectID,
			"actor_id":             actorID,
			"principal_name":       repository.config.PrincipalName,
			"principal_generation": repository.config.PrincipalGeneration,
			"context_key_id":       repository.config.ContextKeyID,
			"nonce":                nonce,
			"expires_unix_micro":   expiresUnixMicro,
			"signature":            mac.Sum(nil),
		},
	); err != nil {
		return mapError(err)
	}
	return nil
}

func (wrapped *transaction) GetReceipt(
	ctx context.Context,
	organizationID, scope, keyHash string,
) (domainrepo.Receipt, error) {
	var receipt domainrepo.Receipt
	var resultRaw, payloadRaw []byte
	err := wrapped.tx.QueryRow(
		ctx,
		sqlReceiptGet,
		pgx.StrictNamedArgs{
			"organization_id": organizationID,
			"scope":           scope,
			"key_hash":        keyHash,
		},
	).Scan(
		&receipt.OrganizationID,
		&receipt.ProjectID,
		&receipt.Scope,
		&receipt.KeyHash,
		&receipt.RequestHash,
		&resultRaw,
		&payloadRaw,
		&receipt.CreatedAt,
	)
	if err != nil {
		return domainrepo.Receipt{}, mapError(err)
	}
	if len(resultRaw) > 0 && string(resultRaw) != "null" {
		receipt.Result, err = unmarshalResource(resultRaw)
		if err != nil {
			return domainrepo.Receipt{}, errs.ErrInternal
		}
	}
	if len(payloadRaw) > 0 && string(payloadRaw) != "null" {
		receipt.Payload = append([]byte(nil), payloadRaw...)
	}
	return receipt, nil
}

func (wrapped *transaction) SaveReceipt(
	ctx context.Context,
	receipt domainrepo.Receipt,
) error {
	result := "null"
	if receipt.Result.ID != "" {
		raw, err := marshalResource(receipt.Result)
		if err != nil {
			return errs.ErrInternal
		}
		result = string(raw)
	}
	payload := "null"
	if len(receipt.Payload) > 0 {
		if !json.Valid(receipt.Payload) {
			return errs.ErrInternal
		}
		payload = string(receipt.Payload)
	}
	_, err := wrapped.tx.Exec(
		ctx,
		sqlReceiptSave,
		pgx.StrictNamedArgs{
			"organization_id": receipt.OrganizationID,
			"project_id":      receipt.ProjectID,
			"scope":           receipt.Scope,
			"key_hash":        receipt.KeyHash,
			"request_hash":    receipt.RequestHash,
			"result":          result,
			"payload":         payload,
			"created_at":      receipt.CreatedAt,
		},
	)
	return mapError(err)
}

func (wrapped *transaction) GetForUpdate(
	ctx context.Context,
	organizationID, projectID, resourceID string,
) (entity.Resource, error) {
	return scanResource(wrapped.tx.QueryRow(
		ctx,
		sqlResourceGetForUpdate,
		pgx.StrictNamedArgs{
			"organization_id": organizationID,
			"project_id":      projectID,
			"resource_id":     resourceID,
		},
	))
}

func (wrapped *transaction) GetForUpdateIncludingDeleted(
	ctx context.Context,
	organizationID, projectID, resourceID string,
) (entity.Resource, error) {
	return scanResource(wrapped.tx.QueryRow(
		ctx,
		sqlResourceGetIncludingDeletedForUpdate,
		pgx.StrictNamedArgs{
			"organization_id": organizationID,
			"project_id":      projectID,
			"resource_id":     resourceID,
		},
	))
}

func (wrapped *transaction) ProjectHasLiveResources(
	ctx context.Context,
	organizationID, projectID string,
) (bool, error) {
	var live bool
	err := wrapped.tx.QueryRow(ctx, sqlProjectHasLiveResources, pgx.StrictNamedArgs{
		"organization_id": organizationID,
		"project_id":      projectID,
	}).Scan(&live)
	return live, mapError(err)
}

func (wrapped *transaction) Get(
	ctx context.Context,
	organizationID, projectID, resourceID string,
) (entity.Resource, error) {
	return scanResource(wrapped.tx.QueryRow(
		ctx,
		sqlResourceGet,
		pgx.StrictNamedArgs{
			"organization_id": organizationID,
			"project_id":      projectID,
			"resource_id":     resourceID,
		},
	))
}

func (wrapped *transaction) Insert(ctx context.Context, resource entity.Resource) error {
	spec, err := marshalSpec(resource.Spec)
	if err != nil {
		return errs.ErrInvalidInput
	}
	tag, err := wrapped.tx.Exec(
		ctx,
		sqlResourceInsert,
		pgx.StrictNamedArgs{
			"id":                   resource.ID,
			"organization_id":      resource.OrganizationID,
			"project_id":           resource.ProjectID,
			"parent_id":            resource.ParentID,
			"owner_actor_id":       resource.OwnerActorID,
			"kind":                 string(resource.Kind),
			"name":                 resource.Name,
			"state":                string(resource.State),
			"version":              resource.Version,
			"spec":                 string(spec),
			"schedule_next_run_at": scheduleNextRun(resource.Spec),
			"created_at":           resource.CreatedAt,
			"updated_at":           resource.UpdatedAt,
		},
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	if err := wrapped.rebuildPermissionIndex(ctx, resource); err != nil {
		return err
	}
	return wrapped.bumpCacheEpoch(ctx, resource.Kind == enum.KindProject)
}

func (wrapped *transaction) Update(
	ctx context.Context,
	resource entity.Resource,
	expectedVersion uint64,
) error {
	spec, err := marshalSpec(resource.Spec)
	if err != nil {
		return errs.ErrInvalidInput
	}
	tag, err := wrapped.tx.Exec(
		ctx,
		sqlResourceUpdate,
		pgx.StrictNamedArgs{
			"id":                   resource.ID,
			"organization_id":      resource.OrganizationID,
			"project_id":           resource.ProjectID,
			"name":                 resource.Name,
			"state":                string(resource.State),
			"new_version":          resource.Version,
			"spec":                 string(spec),
			"schedule_next_run_at": scheduleNextRun(resource.Spec),
			"updated_at":           resource.UpdatedAt,
			"expected_version":     expectedVersion,
		},
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrVersionMismatch
	}
	if err := wrapped.rebuildPermissionIndex(ctx, resource); err != nil {
		return err
	}
	return wrapped.bumpCacheEpoch(ctx, resource.Kind == enum.KindProject)
}

func (wrapped *transaction) AppendAudit(ctx context.Context, audit domainrepo.Audit) error {
	_, err := wrapped.tx.Exec(
		ctx,
		sqlAuditAppend,
		pgx.StrictNamedArgs{
			"id":               audit.ID,
			"organization_id":  audit.OrganizationID,
			"project_id":       audit.ProjectID,
			"actor_id":         audit.ActorID,
			"action":           audit.Action,
			"resource_id":      audit.ResourceID,
			"resource_kind":    audit.ResourceKind,
			"resource_version": audit.ResourceVersion,
			"outcome":          audit.Outcome,
			"correlation_id":   audit.CorrelationID,
			"policy_revision":  audit.PolicyRevision,
			"occurred_at":      audit.OccurredAt,
		},
	)
	return mapError(err)
}

func (wrapped *transaction) AppendEvent(ctx context.Context, change event.Change) error {
	data, err := json.Marshal(map[string]any{
		"projectId":       change.ProjectID,
		"resourceId":      change.ResourceID,
		"resourceKind":    change.ResourceKind,
		"resourceState":   change.ResourceState,
		"resourceVersion": change.ResourceVersion,
	})
	if err != nil {
		return errs.ErrInternal
	}
	envelope := eventing.Envelope{
		EventID:          change.EventID,
		EventName:        change.EventName,
		EventVersion:     1,
		SchemaVersion:    1,
		OccurredAt:       change.OccurredAt.UTC().Truncate(time.Microsecond),
		AggregateType:    string(change.ResourceKind),
		AggregateID:      change.ResourceID,
		AggregateVersion: change.ResourceVersion,
		EventSequence:    change.EventSequence,
		CorrelationID:    change.CorrelationID,
		CausationID:      change.CausationID,
		OrganizationID:   change.OrganizationID,
		Data:             data,
	}
	raw, err := envelope.Marshal()
	if err != nil {
		return errs.ErrInternal
	}
	_, err = wrapped.tx.Exec(
		ctx,
		sqlOutboxAppend,
		pgx.StrictNamedArgs{
			"event_id":          change.EventID,
			"organization_id":   change.OrganizationID,
			"project_id":        change.ProjectID,
			"event_name":        change.EventName,
			"aggregate_type":    string(change.ResourceKind),
			"aggregate_id":      change.ResourceID,
			"aggregate_version": change.ResourceVersion,
			"event_sequence":    change.EventSequence,
			"correlation_id":    change.CorrelationID,
			"causation_id":      change.CausationID,
			"envelope":          string(raw),
			"occurred_at":       change.OccurredAt,
		},
	)
	return mapError(err)
}

func (wrapped *transaction) ActorPermissions(
	ctx context.Context,
	organizationID, projectID, actorID string,
) ([]string, error) {
	rows, err := wrapped.tx.Query(
		ctx,
		sqlPermissionIndexActorList,
		pgx.StrictNamedArgs{
			"organization_id": organizationID,
			"project_id":      projectID,
			"actor_id":        actorID,
		},
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var permissions []string
	for rows.Next() {
		var permission string
		if err := rows.Scan(&permission); err != nil {
			return nil, mapError(err)
		}
		permissions = append(permissions, permission)
	}
	return permissions, mapError(rows.Err())
}

func (wrapped *transaction) ActorRoleIDs(
	ctx context.Context,
	organizationID, projectID, actorID string,
) ([]string, error) {
	rows, err := wrapped.tx.Query(
		ctx,
		sqlPermissionIndexActorRoles,
		pgx.StrictNamedArgs{
			"organization_id": organizationID,
			"project_id":      projectID,
			"actor_id":        actorID,
		},
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var roleIDs []string
	for rows.Next() {
		var roleID string
		if err := rows.Scan(&roleID); err != nil {
			return nil, mapError(err)
		}
		roleIDs = append(roleIDs, roleID)
	}
	return roleIDs, mapError(rows.Err())
}

func (wrapped *transaction) ListSnapshotResources(
	ctx context.Context,
	organizationID, projectID string,
) ([]entity.Resource, error) {
	rows, err := wrapped.tx.Query(
		ctx,
		sqlRuntimeRevisionComponents,
		pgx.StrictNamedArgs{
			"organization_id": organizationID,
			"project_id":      projectID,
		},
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var resources []entity.Resource
	for rows.Next() {
		resource, err := scanResource(rows)
		if err != nil {
			return nil, err
		}
		resources = append(resources, resource)
	}
	return resources, mapError(rows.Err())
}

func (wrapped *transaction) LatestRuntimeRevision(
	ctx context.Context,
	organizationID, projectID string,
) (entity.Resource, error) {
	return scanResource(wrapped.tx.QueryRow(
		ctx,
		sqlRuntimeRevisionLatest,
		pgx.StrictNamedArgs{
			"organization_id": organizationID,
			"project_id":      projectID,
		},
	))
}

func (wrapped *transaction) NextQueuedTurn(
	ctx context.Context,
	organizationID, projectID, turnID string,
) (entity.Resource, error) {
	return scanResource(wrapped.tx.QueryRow(
		ctx,
		sqlTurnNextQueued,
		pgx.StrictNamedArgs{
			"organization_id": organizationID,
			"project_id":      projectID,
			"turn_id":         turnID,
		},
	))
}

func (wrapped *transaction) ExpiredClaimedTurnCandidates(
	ctx context.Context,
	organizationID, projectID, turnID string,
	limit int,
	now time.Time,
) ([]domainrepo.ExpiredTurn, error) {
	rows, err := wrapped.tx.Query(
		ctx,
		sqlTurnExpiredClaimed,
		pgx.StrictNamedArgs{
			"organization_id": organizationID,
			"project_id":      projectID,
			"turn_id":         turnID,
			"limit":           limit,
			"now":             now,
		},
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	expired := make([]domainrepo.ExpiredTurn, 0, limit)
	for rows.Next() {
		var item domainrepo.ExpiredTurn
		var kind, state string
		var specRaw []byte
		if err := rows.Scan(
			&item.Turn.ID,
			&item.Turn.OrganizationID,
			&item.Turn.ProjectID,
			&item.Turn.ParentID,
			&item.Turn.OwnerActorID,
			&kind,
			&item.Turn.Name,
			&state,
			&item.Turn.Version,
			&specRaw,
			&item.Turn.CreatedAt,
			&item.Turn.UpdatedAt,
			&item.Lease.TurnID,
			&item.Lease.TokenHash,
			&item.Lease.WorkloadID,
			&item.Lease.AuthorityGeneration,
			&item.Lease.Attempt,
			&item.Lease.ExpiresAt,
			&item.Lease.Fence,
		); err != nil {
			return nil, mapError(err)
		}
		item.Turn.Kind = enum.Kind(kind)
		item.Turn.State = enum.State(state)
		item.Turn.Spec, err = unmarshalSpec(item.Turn.Kind, specRaw)
		if err != nil || item.Turn.Validate() != nil ||
			item.Turn.Kind != enum.KindTurn ||
			item.Turn.State != enum.StateClaimed ||
			item.Lease.TurnID != item.Turn.ID ||
			item.Lease.Fence != item.Turn.Version {
			return nil, errs.ErrInternal
		}
		expired = append(expired, item)
	}
	return expired, mapError(rows.Err())
}

func (wrapped *transaction) OpenSessionTurns(
	ctx context.Context,
	organizationID, projectID, sessionID string,
) ([]domainrepo.SessionTurn, error) {
	rows, err := wrapped.tx.Query(
		ctx,
		sqlSessionOpenTurns,
		pgx.StrictNamedArgs{
			"organization_id": organizationID,
			"project_id":      projectID,
			"session_id":      sessionID,
		},
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var result []domainrepo.SessionTurn
	for rows.Next() {
		var item domainrepo.SessionTurn
		var kind, state string
		var specRaw []byte
		if err := rows.Scan(
			&item.Turn.ID, &item.Turn.OrganizationID, &item.Turn.ProjectID,
			&item.Turn.ParentID, &item.Turn.OwnerActorID, &kind,
			&item.Turn.Name, &state, &item.Turn.Version, &specRaw,
			&item.Turn.CreatedAt, &item.Turn.UpdatedAt,
			&item.Lease.TurnID, &item.Lease.TokenHash, &item.Lease.WorkloadID,
			&item.Lease.AuthorityGeneration, &item.Lease.Attempt,
			&item.Lease.ExpiresAt, &item.Lease.Fence,
			&item.Attempt.TurnID, &item.Attempt.Attempt,
			&item.Attempt.WorkloadID, &item.Attempt.AuthorityGeneration,
			&item.Attempt.State, &item.Attempt.InputSHA256,
			&item.Attempt.LeaseFence, &item.Attempt.StartedAt,
			&item.Attempt.FinishedAt, &item.Attempt.Outcome,
		); err != nil {
			return nil, mapError(err)
		}
		item.Turn.Kind = enum.Kind(kind)
		item.Turn.State = enum.State(state)
		item.Turn.Spec, err = unmarshalSpec(item.Turn.Kind, specRaw)
		if err != nil || item.Turn.Validate() != nil || item.Turn.Kind != enum.KindTurn {
			return nil, errs.ErrInternal
		}
		result = append(result, item)
	}
	return result, mapError(rows.Err())
}

func (wrapped *transaction) SessionHasLiveRuntimeExecution(
	ctx context.Context,
	organizationID, projectID, sessionID string,
) (bool, error) {
	var live bool
	err := wrapped.tx.QueryRow(
		ctx,
		sqlRuntimeExecutionSessionHasLive,
		pgx.StrictNamedArgs{
			"organization_id": organizationID,
			"project_id":      projectID,
			"session_id":      sessionID,
		},
	).Scan(&live)
	return live, mapError(err)
}

func (wrapped *transaction) SessionHasUnverifiedRuntimeArchive(
	ctx context.Context,
	organizationID, projectID, sessionID string,
) (bool, error) {
	var blocked bool
	err := wrapped.tx.QueryRow(ctx, sqlRuntimeExecutionSessionHasUnverifiedArchive, pgx.StrictNamedArgs{
		"organization_id": organizationID,
		"project_id":      projectID,
		"session_id":      sessionID,
	}).Scan(&blocked)
	return blocked, mapError(err)
}

func (wrapped *transaction) SessionHasActiveRuntimeCleanup(
	ctx context.Context,
	organizationID, projectID, sessionID string,
) (bool, error) {
	var active bool
	err := wrapped.tx.QueryRow(ctx, sqlRuntimeExecutionSessionHasActiveCleanup, pgx.StrictNamedArgs{
		"organization_id": organizationID,
		"project_id":      projectID,
		"session_id":      sessionID,
	}).Scan(&active)
	return active, mapError(err)
}

func (wrapped *transaction) SessionBlocksRuntimeCleanup(
	ctx context.Context,
	organizationID string,
	projectID string,
	sessionID string,
) (bool, error) {
	var blocked bool
	err := wrapped.tx.QueryRow(ctx, sqlSessionBlocksRuntimeCleanup, pgx.StrictNamedArgs{
		"organization_id": organizationID,
		"project_id":      projectID,
		"session_id":      sessionID,
	}).Scan(&blocked)
	if err != nil {
		return false, mapError(err)
	}
	return blocked, nil
}

func (wrapped *transaction) LatestSessionRuntimeArchiveForRestore(
	ctx context.Context,
	organizationID, projectID, sessionID string,
) (domainrepo.RuntimeExecution, error) {
	return scanRuntimeExecution(wrapped.tx.QueryRow(
		ctx, sqlRuntimeExecutionLatestSessionArchiveForRestore, pgx.StrictNamedArgs{
			"organization_id": organizationID,
			"project_id":      projectID,
			"session_id":      sessionID,
		},
	))
}

func (wrapped *transaction) LatestSessionCodexLineage(
	ctx context.Context,
	organizationID, projectID, sessionID string,
) (domainrepo.CodexLineage, error) {
	var lineage domainrepo.CodexLineage
	err := wrapped.tx.QueryRow(ctx, sqlRuntimeExecutionLatestSessionCodexLineage, pgx.StrictNamedArgs{
		"organization_id": organizationID,
		"project_id":      projectID,
		"session_id":      sessionID,
	}).Scan(&lineage.ExecutionID, &lineage.ProviderBindingID, &lineage.SessionID,
		&lineage.ArchiveRelativePath, &lineage.ArchiveSHA256, &lineage.ArchiveProvenance,
		&lineage.TerminalOutcome, &lineage.TerminalReference)
	return lineage, mapError(err)
}

func (wrapped *transaction) SaveTurnLease(
	ctx context.Context,
	lease domainrepo.TurnLease,
) error {
	tag, err := wrapped.tx.Exec(
		ctx,
		sqlTurnLeaseSave,
		pgx.StrictNamedArgs{
			"turn_id":              lease.TurnID,
			"token_hash":           lease.TokenHash,
			"workload_id":          lease.WorkloadID,
			"authority_generation": lease.AuthorityGeneration,
			"attempt":              lease.Attempt,
			"expires_at":           lease.ExpiresAt,
			"fence":                lease.Fence,
		},
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) RenewTurnLease(
	ctx context.Context,
	lease domainrepo.TurnLease,
	now time.Time,
) (domainrepo.TurnLease, error) {
	var renewed domainrepo.TurnLease
	err := wrapped.tx.QueryRow(
		ctx,
		sqlTurnLeaseRenew,
		pgx.StrictNamedArgs{
			"turn_id":              lease.TurnID,
			"token_hash":           lease.TokenHash,
			"workload_id":          lease.WorkloadID,
			"authority_generation": lease.AuthorityGeneration,
			"attempt":              lease.Attempt,
			"fence":                lease.Fence,
			"now":                  now,
			"new_expires_at":       lease.ExpiresAt,
		},
	).Scan(
		&renewed.TurnID,
		&renewed.TokenHash,
		&renewed.WorkloadID,
		&renewed.AuthorityGeneration,
		&renewed.Attempt,
		&renewed.ExpiresAt,
		&renewed.Fence,
	)
	if err != nil {
		return domainrepo.TurnLease{}, mapError(err)
	}
	return renewed, nil
}

func (wrapped *transaction) ValidateTurnLease(
	ctx context.Context,
	turnID, tokenHash, workloadID string,
	authorityGeneration uint64,
	attempt uint32,
	now time.Time,
) (domainrepo.TurnLease, error) {
	var lease domainrepo.TurnLease
	err := wrapped.tx.QueryRow(
		ctx,
		sqlTurnLeaseValidate,
		pgx.StrictNamedArgs{
			"turn_id":              turnID,
			"token_hash":           tokenHash,
			"workload_id":          workloadID,
			"authority_generation": authorityGeneration,
			"attempt":              attempt,
			"now":                  now,
		},
	).Scan(
		&lease.TurnID,
		&lease.TokenHash,
		&lease.WorkloadID,
		&lease.AuthorityGeneration,
		&lease.Attempt,
		&lease.ExpiresAt,
		&lease.Fence,
	)
	if err != nil {
		return domainrepo.TurnLease{}, mapError(err)
	}
	return lease, nil
}

func (wrapped *transaction) GetTurnLeaseForUpdate(
	ctx context.Context,
	turnID string,
) (domainrepo.TurnLease, error) {
	var lease domainrepo.TurnLease
	err := wrapped.tx.QueryRow(
		ctx,
		sqlTurnLeaseGetForUpdate,
		pgx.StrictNamedArgs{"turn_id": turnID},
	).Scan(
		&lease.TurnID,
		&lease.TokenHash,
		&lease.WorkloadID,
		&lease.AuthorityGeneration,
		&lease.Attempt,
		&lease.ExpiresAt,
		&lease.Fence,
	)
	if err != nil {
		return domainrepo.TurnLease{}, mapError(err)
	}
	return lease, nil
}

func (wrapped *transaction) SaveTurnAttempt(
	ctx context.Context,
	attempt domainrepo.TurnAttempt,
) error {
	tag, err := wrapped.tx.Exec(
		ctx,
		sqlTurnAttemptSave,
		pgx.StrictNamedArgs{
			"turn_id":              attempt.TurnID,
			"attempt":              attempt.Attempt,
			"workload_id":          attempt.WorkloadID,
			"authority_generation": attempt.AuthorityGeneration,
			"state":                attempt.State,
			"input_sha256":         attempt.InputSHA256,
			"lease_fence":          attempt.LeaseFence,
			"started_at":           attempt.StartedAt,
		},
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) FinishTurnAttempt(
	ctx context.Context,
	attempt domainrepo.TurnAttempt,
) error {
	tag, err := wrapped.tx.Exec(
		ctx,
		sqlTurnAttemptFinish,
		pgx.StrictNamedArgs{
			"turn_id":              attempt.TurnID,
			"attempt":              attempt.Attempt,
			"workload_id":          attempt.WorkloadID,
			"authority_generation": attempt.AuthorityGeneration,
			"state":                attempt.State,
			"lease_fence":          attempt.LeaseFence,
			"finished_at":          attempt.FinishedAt,
			"outcome":              attempt.Outcome,
		},
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) EnqueueInteractionDelivery(
	ctx context.Context,
	work domainrepo.InteractionDeliveryWork,
) error {
	tag, err := wrapped.tx.Exec(ctx, sqlInteractionDeliveryEnqueue, pgx.StrictNamedArgs{
		"id": work.ID, "organization_id": work.OrganizationID, "project_id": work.ProjectID,
		"actor_id": work.ActorID, "session_id": work.SessionID, "session_version": work.SessionVersion,
		"turn_id": work.TurnID, "turn_version": work.TurnVersion, "attempt": work.Attempt,
		"runtime_revision_id": work.RuntimeRevisionID, "runtime_revision_version": work.RuntimeRevisionVersion,
		"immutable_input_sha256": work.ImmutableInputSHA256, "kind": work.Kind,
		"lifecycle_state": work.LifecycleState, "outcome": work.Outcome,
		"artifact_id": work.ArtifactID, "artifact_version": work.ArtifactVersion,
		"artifact_sha256": work.ArtifactSHA256, "artifact_name": work.ArtifactName,
		"artifact_storage_ref": work.ArtifactStorageRef, "artifact_size_bytes": work.ArtifactSizeBytes,
		"artifact_media_type":  work.ArtifactMediaType,
		"inline_payload":       work.InlinePayload,
		"notification_room_id": work.NotificationRoomID,
		"notification_policy":  work.NotificationPolicy,
		"scheduled_outcome":    work.ScheduledOutcome,
	})
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() > 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) ClaimInteractionDelivery(ctx context.Context, organizationID, projectID,
	leaseOwner, leaseTokenSHA256 string, leaseDuration time.Duration) (domainrepo.InteractionDeliveryWork, error) {
	var work domainrepo.InteractionDeliveryWork
	err := wrapped.tx.QueryRow(ctx, sqlInteractionDeliveryClaim, pgx.StrictNamedArgs{
		"organization_id": organizationID, "project_id": projectID, "lease_owner": leaseOwner,
		"lease_token_sha256": leaseTokenSHA256, "lease_duration": leaseDuration.String(),
	}).Scan(&work.ID, &work.OrganizationID, &work.ProjectID, &work.ActorID, &work.SessionID,
		&work.SessionVersion, &work.TurnID, &work.TurnVersion, &work.Attempt, &work.RuntimeRevisionID,
		&work.RuntimeRevisionVersion, &work.ImmutableInputSHA256, &work.Kind, &work.LifecycleState,
		&work.Outcome, &work.ArtifactID, &work.ArtifactVersion, &work.ArtifactSHA256,
		&work.ArtifactName, &work.ArtifactStorageRef, &work.ArtifactSizeBytes, &work.ArtifactMediaType,
		&work.InlinePayload, &work.NotificationRoomID, &work.NotificationPolicy, &work.ScheduledOutcome,
		&work.Fence, &work.LeaseExpiresAt)
	return work, mapError(err)
}

func (wrapped *transaction) CompleteInteractionDelivery(ctx context.Context, organizationID, projectID,
	id string, fence uint64, leaseTokenSHA256, providerReceiptSHA256 string) error {
	tag, err := wrapped.tx.Exec(ctx, sqlInteractionDeliveryComplete, pgx.StrictNamedArgs{
		"organization_id": organizationID, "project_id": projectID, "id": id, "fence": fence,
		"lease_token_sha256": leaseTokenSHA256, "provider_receipt_sha256": providerReceiptSHA256,
	})
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) SaveInteractionDeliveryReadbackGrant(ctx context.Context,
	grant domainrepo.InteractionDeliveryReadbackGrant) error {
	tag, err := wrapped.tx.Exec(ctx, sqlInteractionDeliveryReadbackInsert, pgx.StrictNamedArgs{
		"id": grant.ID, "organization_id": grant.OrganizationID, "project_id": grant.ProjectID,
		"actor_id": grant.ActorID, "delivery_id": grant.DeliveryID, "producer_id": grant.ProducerID,
		"purpose": grant.Purpose, "workload_id": grant.WorkloadID, "caller_spiffe_id": grant.CallerSPIFFEID,
		"operation": grant.Operation, "permission": grant.Permission, "credential_sha256": grant.CredentialSHA256,
		"generation": grant.Generation, "keyset_revision": grant.KeysetRevision,
		"keyset_high_watermark": grant.KeysetHighWatermark, "keyset_sha256": grant.KeysetSHA256,
		"issued_at": grant.IssuedAt, "expires_at": grant.ExpiresAt, "readiness": grant.Readiness,
	})
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) ValidateInteractionDeliveryReadbackGrant(ctx context.Context,
	id, deliveryID, organizationID, projectID, credentialSHA256 string, generation uint64) (bool, error) {
	var active bool
	err := wrapped.tx.QueryRow(ctx, sqlInteractionDeliveryReadbackValidate, pgx.StrictNamedArgs{
		"id": id, "delivery_id": deliveryID, "organization_id": organizationID, "project_id": projectID,
		"credential_sha256": credentialSHA256, "generation": generation,
	}).Scan(&active)
	return active, mapError(err)
}

func (wrapped *transaction) GetTurnAttemptForUpdate(
	ctx context.Context,
	turnID string,
	attemptNumber uint32,
) (domainrepo.TurnAttempt, error) {
	var attempt domainrepo.TurnAttempt
	err := wrapped.tx.QueryRow(
		ctx,
		sqlTurnAttemptGetForUpdate,
		pgx.StrictNamedArgs{"turn_id": turnID, "attempt": attemptNumber},
	).Scan(
		&attempt.TurnID,
		&attempt.Attempt,
		&attempt.WorkloadID,
		&attempt.AuthorityGeneration,
		&attempt.State,
		&attempt.InputSHA256,
		&attempt.LeaseFence,
		&attempt.StartedAt,
		&attempt.FinishedAt,
		&attempt.Outcome,
	)
	return attempt, mapError(err)
}

func (wrapped *transaction) SaveDelegationEdge(
	ctx context.Context,
	edge domainrepo.DelegationEdge,
) error {
	tag, err := wrapped.tx.Exec(ctx, sqlDelegationEdgeSave, pgx.StrictNamedArgs{
		"id": edge.ID, "organization_id": edge.OrganizationID,
		"project_id": edge.ProjectID, "parent_process_run_id": edge.ParentProcessRunID,
		"source_session_id": edge.SourceSessionID, "source_turn_id": edge.SourceTurnID,
		"source_attempt": edge.SourceAttempt, "source_input_sha256": edge.SourceInputSHA256,
		"target_session_id": edge.TargetSessionID, "target_role_id": edge.TargetRoleID,
		"target_turn_id": edge.TargetTurnID, "target_attempt": edge.TargetAttempt,
		"target_input_sha256":     edge.TargetInputSHA256,
		"root_initiator_actor_id": edge.RootInitiatorActorID,
		"grant_generation":        edge.GrantGeneration, "created_at": edge.CreatedAt,
	})
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) GetDelegationEdgeByTargetTurn(
	ctx context.Context,
	organizationID, projectID, targetTurnID string,
) (domainrepo.DelegationEdge, error) {
	var edge domainrepo.DelegationEdge
	err := wrapped.tx.QueryRow(ctx, sqlDelegationEdgeGetByTargetTurn, pgx.StrictNamedArgs{
		"organization_id": organizationID,
		"project_id":      projectID,
		"target_turn_id":  targetTurnID,
	}).Scan(
		&edge.ID, &edge.OrganizationID, &edge.ProjectID,
		&edge.ParentProcessRunID, &edge.SourceSessionID, &edge.SourceTurnID,
		&edge.SourceAttempt, &edge.SourceInputSHA256, &edge.TargetSessionID,
		&edge.TargetRoleID, &edge.TargetTurnID, &edge.TargetAttempt,
		&edge.TargetInputSHA256, &edge.RootInitiatorActorID,
		&edge.GrantGeneration, &edge.CreatedAt,
	)
	return edge, mapError(err)
}

func (wrapped *transaction) DeleteTurnLease(
	ctx context.Context,
	turnID string,
	fence uint64,
) error {
	tag, err := wrapped.tx.Exec(
		ctx,
		sqlTurnLeaseDelete,
		pgx.StrictNamedArgs{"turn_id": turnID, "fence": fence},
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) DueSchedules(
	ctx context.Context,
	organizationID, projectID string,
	limit int,
	now time.Time,
) ([]entity.Resource, error) {
	rows, err := wrapped.tx.Query(
		ctx,
		sqlScheduleDue,
		pgx.StrictNamedArgs{
			"organization_id": organizationID,
			"project_id":      projectID,
			"limit":           limit,
			"now":             now,
		},
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var resources []entity.Resource
	for rows.Next() {
		resource, err := scanResource(rows)
		if err != nil {
			return nil, err
		}
		resources = append(resources, resource)
	}
	return resources, mapError(rows.Err())
}

func (wrapped *transaction) NextAutomationProject(
	ctx context.Context, organizationID, operation string,
) (string, error) {
	var projectID string
	err := wrapped.tx.QueryRow(ctx, sqlAutomationSchedulerProjectNext, pgx.StrictNamedArgs{
		"organization_id": organizationID, "operation": operation,
	}).Scan(&projectID)
	return projectID, mapError(err)
}

func (wrapped *transaction) SaveScheduleOccurrence(
	ctx context.Context,
	occurrence domainrepo.ScheduleOccurrence,
) error {
	tag, err := wrapped.tx.Exec(
		ctx,
		sqlScheduleOccurrenceSave,
		pgx.StrictNamedArgs{
			"id":                                 occurrence.ID,
			"schedule_id":                        occurrence.ScheduleID,
			"organization_id":                    occurrence.OrganizationID,
			"project_id":                         occurrence.ProjectID,
			"scheduled_for":                      occurrence.ScheduledFor,
			"target_resource_id":                 occurrence.TargetResourceID,
			"target_kind":                        string(occurrence.TargetKind),
			"target_version":                     occurrence.TargetVersion,
			"effective_input_sha256":             occurrence.EffectiveInputSHA256,
			"prompt_profile_id":                  occurrence.PromptProfileID,
			"prompt_revision":                    occurrence.PromptRevision,
			"runtime_revision_id":                occurrence.RuntimeRevisionID,
			"session_policy":                     occurrence.SessionPolicy,
			"room_id":                            occurrence.RoomID,
			"notification_policy":                occurrence.NotificationPolicy,
			"maximum_execution_duration_ms":      occurrence.MaximumExecution.Milliseconds(),
			"coalesce":                           occurrence.Coalesce,
			"overlap_policy":                     occurrence.OverlapPolicy,
			"maximum_attempts":                   occurrence.MaximumAttempts,
			"initial_backoff_ms":                 occurrence.InitialBackoff.Milliseconds(),
			"maximum_backoff_ms":                 occurrence.MaximumBackoff.Milliseconds(),
			"dead_letter_at":                     occurrence.DeadLetterAt,
			"state":                              occurrence.State,
			"version":                            occurrence.Version,
			"attempt":                            occurrence.Attempt,
			"available_at":                       occurrence.AvailableAt,
			"outcome":                            occurrence.Outcome,
			"result_artifact_id":                 occurrence.ResultArtifactID,
			"recovery_evidence_sha256":           occurrence.RecoveryEvidenceSHA256,
			"recovery_blocked_at":                occurrence.RecoveryBlockedAt,
			"execution_session_id":               occurrence.ExecutionSessionID,
			"execution_session_version":          occurrence.ExecutionSessionVersion,
			"execution_turn_id":                  occurrence.ExecutionTurnID,
			"execution_turn_version":             occurrence.ExecutionTurnVersion,
			"execution_process_run_id":           occurrence.ExecutionProcessRunID,
			"execution_process_version":          occurrence.ExecutionProcessVersion,
			"execution_runtime_revision_id":      occurrence.ExecutionRuntimeRevisionID,
			"execution_runtime_revision_version": occurrence.ExecutionRuntimeRevisionVersion,
			"updated_at":                         occurrence.UpdatedAt,
		},
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) HasOpenScheduleOccurrence(
	ctx context.Context,
	organizationID, projectID, scheduleID string,
) (bool, error) {
	var found bool
	err := wrapped.tx.QueryRow(
		ctx,
		sqlScheduleOccurrenceHasOpen,
		pgx.StrictNamedArgs{
			"organization_id":       organizationID,
			"project_id":            projectID,
			"schedule_id":           scheduleID,
			"open_execution_states": scheduleOpenExecutionStates(),
		},
	).Scan(&found)
	return found, mapError(err)
}

func (wrapped *transaction) HasBlockingScheduleExecution(
	ctx context.Context,
	organizationID, projectID, scheduleID, candidateOccurrenceID string,
) (bool, error) {
	var found bool
	err := wrapped.tx.QueryRow(
		ctx,
		sqlScheduleOccurrenceHasBlockingExecution,
		pgx.StrictNamedArgs{
			"organization_id":         organizationID,
			"project_id":              projectID,
			"schedule_id":             scheduleID,
			"candidate_occurrence_id": candidateOccurrenceID,
			"open_execution_states":   scheduleOpenExecutionStates(),
		},
	).Scan(&found)
	return found, mapError(err)
}

func (wrapped *transaction) SkipOverlappedScheduleOccurrences(
	ctx context.Context,
	organizationID, projectID string,
	now time.Time,
	limit int,
) ([]domainrepo.ScheduleOccurrence, error) {
	rows, err := wrapped.tx.Query(
		ctx,
		sqlScheduleOccurrenceSkipOverlap,
		pgx.StrictNamedArgs{
			"organization_id":       organizationID,
			"project_id":            projectID,
			"now":                   now,
			"limit":                 limit,
			"open_execution_states": scheduleOpenExecutionStates(),
		},
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var occurrences []domainrepo.ScheduleOccurrence
	for rows.Next() {
		occurrence, err := scanScheduleOccurrence(rows)
		if err != nil {
			return nil, err
		}
		occurrences = append(occurrences, occurrence)
	}
	return occurrences, mapError(rows.Err())
}

func (wrapped *transaction) ExpiredScheduleOccurrenceCandidates(
	ctx context.Context,
	organizationID, projectID string,
	now time.Time,
) ([]domainrepo.ScheduleOccurrence, error) {
	rows, err := wrapped.tx.Query(
		ctx,
		sqlScheduleOccurrenceExpiredCandidates,
		pgx.StrictNamedArgs{
			"organization_id": organizationID,
			"project_id":      projectID,
			"now":             now,
		},
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var occurrences []domainrepo.ScheduleOccurrence
	for rows.Next() {
		occurrence, err := scanScheduleOccurrence(rows)
		if err != nil {
			return nil, err
		}
		occurrences = append(occurrences, occurrence)
	}
	return occurrences, mapError(rows.Err())
}

func (wrapped *transaction) NextScheduleOccurrence(
	ctx context.Context,
	organizationID, projectID string,
	now time.Time,
) (domainrepo.ScheduleOccurrence, error) {
	return scanScheduleOccurrence(wrapped.tx.QueryRow(
		ctx,
		sqlScheduleOccurrenceNext,
		pgx.StrictNamedArgs{
			"organization_id":       organizationID,
			"project_id":            projectID,
			"now":                   now,
			"open_execution_states": scheduleOpenExecutionStates(),
		},
	))
}

func scheduleOpenExecutionStates() []string {
	return []string{"RESERVED", "CLAIMED", "WAITING_OWNER", "CONTINUATION"}
}

func (wrapped *transaction) UpdateScheduleOccurrence(
	ctx context.Context,
	occurrence domainrepo.ScheduleOccurrence,
	expectedAttempt uint32,
	expectedTokenHash string,
) error {
	tag, err := wrapped.tx.Exec(
		ctx,
		sqlScheduleOccurrenceUpdate,
		scheduleOccurrenceUpdateArgs(occurrence, expectedAttempt, expectedTokenHash),
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func scheduleOccurrenceUpdateArgs(
	occurrence domainrepo.ScheduleOccurrence,
	expectedAttempt uint32,
	expectedTokenHash string,
) pgx.StrictNamedArgs {
	return pgx.StrictNamedArgs{
		"id":                                 occurrence.ID,
		"state":                              occurrence.State,
		"version":                            occurrence.Version,
		"attempt":                            occurrence.Attempt,
		"effective_input_sha256":             occurrence.EffectiveInputSHA256,
		"claimant_workload_id":               occurrence.ClaimantWorkloadID,
		"authority_generation":               occurrence.AuthorityGeneration,
		"token_hash":                         occurrence.TokenHash,
		"claim_key_sha256":                   occurrence.ClaimKeySHA256,
		"lease_expires_at":                   occurrence.LeaseExpiresAt,
		"available_at":                       occurrence.AvailableAt,
		"outcome":                            occurrence.Outcome,
		"result_artifact_id":                 occurrence.ResultArtifactID,
		"recovery_evidence_sha256":           occurrence.RecoveryEvidenceSHA256,
		"recovery_blocked_at":                occurrence.RecoveryBlockedAt,
		"execution_session_id":               occurrence.ExecutionSessionID,
		"execution_session_version":          occurrence.ExecutionSessionVersion,
		"execution_turn_id":                  occurrence.ExecutionTurnID,
		"execution_turn_version":             occurrence.ExecutionTurnVersion,
		"execution_process_run_id":           occurrence.ExecutionProcessRunID,
		"execution_process_version":          occurrence.ExecutionProcessVersion,
		"execution_runtime_revision_id":      occurrence.ExecutionRuntimeRevisionID,
		"execution_runtime_revision_version": occurrence.ExecutionRuntimeRevisionVersion,
		"updated_at":                         occurrence.UpdatedAt,
		"expected_attempt":                   expectedAttempt,
		"expected_token_hash":                expectedTokenHash,
	}
}

func (wrapped *transaction) GetScheduleOccurrenceForUpdate(
	ctx context.Context,
	organizationID, projectID, occurrenceID string,
) (domainrepo.ScheduleOccurrence, error) {
	return scanScheduleOccurrence(wrapped.tx.QueryRow(
		ctx,
		sqlScheduleOccurrenceGetForUpdate,
		pgx.StrictNamedArgs{
			"organization_id": organizationID,
			"project_id":      projectID,
			"id":              occurrenceID,
		},
	))
}

func (wrapped *transaction) GetScheduleOccurrence(
	ctx context.Context,
	organizationID, projectID, occurrenceID string,
) (domainrepo.ScheduleOccurrence, error) {
	return scanScheduleOccurrence(wrapped.tx.QueryRow(
		ctx,
		sqlScheduleOccurrenceGet,
		pgx.StrictNamedArgs{
			"organization_id": organizationID,
			"project_id":      projectID,
			"id":              occurrenceID,
		},
	))
}

func (wrapped *transaction) GetScheduleOccurrenceByCurrentTurn(
	ctx context.Context,
	organizationID, projectID, turnID string,
) (domainrepo.ScheduleOccurrence, error) {
	return scanScheduleOccurrence(wrapped.tx.QueryRow(
		ctx,
		sqlScheduleOccurrenceGetByCurrentTurn,
		pgx.StrictNamedArgs{
			"organization_id": organizationID,
			"project_id":      projectID,
			"turn_id":         turnID,
		},
	))
}

func (wrapped *transaction) GetScheduleOccurrenceByClaimKey(
	ctx context.Context,
	organizationID, projectID, claimKeySHA256 string,
) (domainrepo.ScheduleOccurrence, error) {
	return scanScheduleOccurrence(wrapped.tx.QueryRow(
		ctx,
		sqlScheduleOccurrenceGetByClaimKey,
		pgx.StrictNamedArgs{
			"organization_id":  organizationID,
			"project_id":       projectID,
			"claim_key_sha256": claimKeySHA256,
		},
	))
}

func (wrapped *transaction) InsertScheduleOccurrenceCapability(
	ctx context.Context, capability domainrepo.ScheduleOccurrenceCapability,
) error {
	tag, err := wrapped.tx.Exec(ctx, sqlScheduleCapabilityInsert, pgx.StrictNamedArgs{
		"id": capability.ID, "organization_id": capability.OrganizationID,
		"project_id": capability.ProjectID, "occurrence_id": capability.OccurrenceID,
		"attempt": capability.Attempt, "immutable_input_sha256": capability.ImmutableInputSHA256,
		"authority_generation": capability.AuthorityGeneration, "full_method": capability.FullMethod,
		"workload_id": capability.WorkloadID, "caller_spiffe_id": capability.CallerSPIFFEID,
		"token_sha256": capability.TokenSHA256, "state": capability.State,
		"issued_at": capability.IssuedAt, "expires_at": capability.ExpiresAt,
	})
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) GetScheduleOccurrenceCapabilityForUpdate(
	ctx context.Context, tokenSHA256 string,
) (domainrepo.ScheduleOccurrenceCapability, error) {
	return scanScheduleOccurrenceCapability(wrapped.tx.QueryRow(ctx, sqlScheduleCapabilityGetForUpdate,
		pgx.StrictNamedArgs{"token_sha256": tokenSHA256}))
}

func (wrapped *transaction) GetScheduleOccurrenceCapabilityByOccurrenceForUpdate(
	ctx context.Context, occurrenceID string, attempt uint32, fullMethod string, generation uint64,
) (domainrepo.ScheduleOccurrenceCapability, error) {
	return scanScheduleOccurrenceCapability(wrapped.tx.QueryRow(ctx, sqlScheduleCapabilityGetByOccurrenceForUpdate,
		pgx.StrictNamedArgs{"occurrence_id": occurrenceID, "attempt": attempt,
			"full_method": fullMethod, "authority_generation": generation}))
}

func (wrapped *transaction) UpdateScheduleOccurrenceCapability(ctx context.Context,
	capability domainrepo.ScheduleOccurrenceCapability, expectedState string,
) error {
	tag, err := wrapped.tx.Exec(ctx, sqlScheduleCapabilityUpdate, pgx.StrictNamedArgs{
		"id": capability.ID, "state": capability.State, "consumed_at": capability.ConsumedAt,
		"revoked_at": capability.RevokedAt, "expected_state": expectedState,
	})
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) SaveScheduledRun(
	ctx context.Context,
	run domainrepo.ScheduledRun,
) error {
	tag, err := wrapped.tx.Exec(ctx, sqlScheduledRunSave, pgx.StrictNamedArgs{
		"occurrence_id": run.OccurrenceID, "attempt": run.Attempt,
		"session_id": run.SessionID, "session_version": run.SessionVersion,
		"turn_id": run.TurnID, "turn_version": run.TurnVersion,
		"process_run_id": run.ProcessRunID, "process_version": run.ProcessVersion,
		"runtime_revision_id":              run.RuntimeRevisionID,
		"runtime_revision_version":         run.RuntimeRevisionVersion,
		"effective_input_sha256":           run.EffectiveInputSHA256,
		"current_session_id":               run.CurrentSessionID,
		"current_session_version":          run.CurrentSessionVersion,
		"current_turn_id":                  run.CurrentTurnID,
		"current_turn_version":             run.CurrentTurnVersion,
		"current_turn_attempt":             run.CurrentTurnAttempt,
		"current_process_run_id":           run.CurrentProcessRunID,
		"current_process_version":          run.CurrentProcessVersion,
		"current_runtime_revision_id":      run.CurrentRuntimeRevisionID,
		"current_runtime_revision_version": run.CurrentRuntimeRevisionVersion,
		"current_input_sha256":             run.CurrentInputSHA256,
		"state":                            run.State, "created_at": run.CreatedAt,
	})
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) GetScheduledRunForUpdate(
	ctx context.Context,
	occurrenceID string,
	attempt uint32,
) (domainrepo.ScheduledRun, error) {
	return scanScheduledRun(wrapped.tx.QueryRow(
		ctx,
		sqlScheduledRunGetForUpdate,
		pgx.StrictNamedArgs{"occurrence_id": occurrenceID, "attempt": attempt},
	))
}

func (wrapped *transaction) GetScheduledRunByCurrentTurnForUpdate(
	ctx context.Context,
	turnID string,
) (domainrepo.ScheduledRun, error) {
	return scanScheduledRun(wrapped.tx.QueryRow(
		ctx,
		sqlScheduledRunGetByCurrentTurnForUpdate,
		pgx.StrictNamedArgs{"current_turn_id": turnID},
	))
}

func scanScheduledRun(row pgx.Row) (domainrepo.ScheduledRun, error) {
	var run domainrepo.ScheduledRun
	err := row.Scan(
		&run.OccurrenceID,
		&run.Attempt,
		&run.SessionID,
		&run.SessionVersion,
		&run.TurnID,
		&run.TurnVersion,
		&run.ProcessRunID,
		&run.ProcessVersion,
		&run.RuntimeRevisionID,
		&run.RuntimeRevisionVersion,
		&run.EffectiveInputSHA256,
		&run.State,
		&run.Outcome,
		&run.ResultArtifactID,
		&run.CreatedAt,
		&run.FinishedAt,
		&run.ContinuationTurnID,
		&run.ContinuationTurnVersion,
		&run.ContinuationRuntimeRevisionID,
		&run.ContinuationRuntimeRevisionVersion,
		&run.ContinuationInputSHA256,
		&run.OwnerFeedbackSHA256,
		&run.CurrentSessionID,
		&run.CurrentSessionVersion,
		&run.CurrentTurnID,
		&run.CurrentTurnVersion,
		&run.CurrentTurnAttempt,
		&run.CurrentProcessRunID,
		&run.CurrentProcessVersion,
		&run.CurrentRuntimeRevisionID,
		&run.CurrentRuntimeRevisionVersion,
		&run.CurrentInputSHA256,
	)
	return run, mapError(err)
}

func (wrapped *transaction) WaitScheduledRun(
	ctx context.Context,
	run domainrepo.ScheduledRun,
) error {
	tag, err := wrapped.tx.Exec(
		ctx,
		sqlScheduledRunWaitOwner,
		pgx.StrictNamedArgs{"occurrence_id": run.OccurrenceID, "attempt": run.Attempt,
			"outcome": run.Outcome, "result_artifact_id": run.ResultArtifactID},
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) SuspendScheduledRun(
	ctx context.Context,
	run domainrepo.ScheduledRun,
	expectedTurnID string,
	expectedTurnAttempt uint32,
) error {
	tag, err := wrapped.tx.Exec(
		ctx,
		sqlScheduledRunSuspendExternal,
		scheduledRunSuspendArgs(run, expectedTurnID, expectedTurnAttempt),
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func scheduledRunSuspendArgs(
	run domainrepo.ScheduledRun,
	expectedTurnID string,
	expectedTurnAttempt uint32,
) pgx.StrictNamedArgs {
	return pgx.StrictNamedArgs{
		"occurrence_id": run.OccurrenceID, "attempt": run.Attempt,
		"expected_turn_id": expectedTurnID, "expected_turn_attempt": expectedTurnAttempt,
		"current_session_id":               run.CurrentSessionID,
		"current_session_version":          run.CurrentSessionVersion,
		"current_turn_id":                  run.CurrentTurnID,
		"current_turn_version":             run.CurrentTurnVersion,
		"current_turn_attempt":             run.CurrentTurnAttempt,
		"current_process_run_id":           run.CurrentProcessRunID,
		"current_process_version":          run.CurrentProcessVersion,
		"current_runtime_revision_id":      run.CurrentRuntimeRevisionID,
		"current_runtime_revision_version": run.CurrentRuntimeRevisionVersion,
		"current_input_sha256":             run.CurrentInputSHA256,
	}
}

func (wrapped *transaction) ContinueScheduledRun(
	ctx context.Context,
	run domainrepo.ScheduledRun,
) error {
	tag, err := wrapped.tx.Exec(
		ctx,
		sqlScheduledRunContinue,
		pgx.StrictNamedArgs{
			"occurrence_id":                         run.OccurrenceID,
			"attempt":                               run.Attempt,
			"outcome":                               run.Outcome,
			"continuation_turn_id":                  run.ContinuationTurnID,
			"continuation_turn_version":             run.ContinuationTurnVersion,
			"continuation_runtime_revision_id":      run.ContinuationRuntimeRevisionID,
			"continuation_runtime_revision_version": run.ContinuationRuntimeRevisionVersion,
			"continuation_input_sha256":             run.ContinuationInputSHA256,
			"owner_feedback_sha256":                 run.OwnerFeedbackSHA256,
			"current_session_id":                    run.CurrentSessionID,
			"current_session_version":               run.CurrentSessionVersion,
			"current_turn_id":                       run.CurrentTurnID,
			"current_turn_version":                  run.CurrentTurnVersion,
			"current_turn_attempt":                  run.CurrentTurnAttempt,
			"current_process_run_id":                run.CurrentProcessRunID,
			"current_process_version":               run.CurrentProcessVersion,
			"current_runtime_revision_id":           run.CurrentRuntimeRevisionID,
			"current_runtime_revision_version":      run.CurrentRuntimeRevisionVersion,
			"current_input_sha256":                  run.CurrentInputSHA256,
		},
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) RebindScheduledRun(
	ctx context.Context,
	run domainrepo.ScheduledRun,
	expectedTurnID string,
	expectedTurnAttempt uint32,
) error {
	tag, err := wrapped.tx.Exec(
		ctx,
		sqlScheduledRunRebind,
		pgx.StrictNamedArgs{
			"occurrence_id":                         run.OccurrenceID,
			"attempt":                               run.Attempt,
			"expected_turn_id":                      expectedTurnID,
			"expected_turn_attempt":                 expectedTurnAttempt,
			"current_session_id":                    run.CurrentSessionID,
			"current_session_version":               run.CurrentSessionVersion,
			"current_turn_id":                       run.CurrentTurnID,
			"current_turn_version":                  run.CurrentTurnVersion,
			"current_turn_attempt":                  run.CurrentTurnAttempt,
			"current_process_run_id":                run.CurrentProcessRunID,
			"current_process_version":               run.CurrentProcessVersion,
			"current_runtime_revision_id":           run.CurrentRuntimeRevisionID,
			"current_runtime_revision_version":      run.CurrentRuntimeRevisionVersion,
			"current_input_sha256":                  run.CurrentInputSHA256,
			"continuation_turn_id":                  run.ContinuationTurnID,
			"continuation_turn_version":             run.ContinuationTurnVersion,
			"continuation_runtime_revision_id":      run.ContinuationRuntimeRevisionID,
			"continuation_runtime_revision_version": run.ContinuationRuntimeRevisionVersion,
			"continuation_input_sha256":             run.ContinuationInputSHA256,
			"owner_feedback_sha256":                 run.OwnerFeedbackSHA256,
		},
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) FinishScheduledRun(
	ctx context.Context,
	run domainrepo.ScheduledRun,
) error {
	tag, err := wrapped.tx.Exec(ctx, sqlScheduledRunFinish, pgx.StrictNamedArgs{
		"occurrence_id": run.OccurrenceID, "attempt": run.Attempt,
		"state": run.State, "outcome": run.Outcome,
		"result_artifact_id": run.ResultArtifactID, "finished_at": run.FinishedAt,
	})
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) AuthorizeProject(
	ctx context.Context,
	organizationID, projectID, actorID, permission, resourceReference string,
) (entity.Resource, error) {
	return scanResource(wrapped.tx.QueryRow(
		ctx,
		sqlProjectAuthorize,
		pgx.StrictNamedArgs{
			"organization_id":    organizationID,
			"project_id":         projectID,
			"actor_id":           actorID,
			"permission":         permission,
			"resource_reference": resourceReference,
		},
	))
}

func (wrapped *transaction) NextProofRevision(ctx context.Context) (uint64, error) {
	var revision uint64
	err := wrapped.tx.QueryRow(ctx, sqlProofRevisionNext).Scan(&revision)
	return revision, mapError(err)
}

func (wrapped *transaction) SaveMemoryProjection(
	ctx context.Context,
	projection domainrepo.MemoryProjection,
) error {
	tag, err := wrapped.tx.Exec(
		ctx,
		sqlMemoryProjectionUpsert,
		pgx.StrictNamedArgs{
			"resource_id":       projection.ResourceID,
			"organization_id":   projection.OrganizationID,
			"project_id":        projection.ProjectID,
			"resource_version":  projection.ResourceVersion,
			"content_sha256":    projection.ContentSHA256,
			"model_id":          projection.ModelID,
			"model_revision":    projection.ModelRevision,
			"model_sha256":      projection.ModelSHA256,
			"embedding":         formatVector(projection.Embedding),
			"projection_sha256": projection.ProjectionSHA256,
			"updated_at":        projection.UpdatedAt,
		},
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrVersionMismatch
	}
	return nil
}

func (wrapped *transaction) HasActiveChildProcesses(
	ctx context.Context,
	organizationID, projectID, processRunID string,
) (bool, error) {
	var exists bool
	err := wrapped.tx.QueryRow(
		ctx,
		sqlProcessHasActiveChildren,
		pgx.StrictNamedArgs{
			"organization_id": organizationID,
			"project_id":      projectID,
			"process_run_id":  processRunID,
		},
	).Scan(&exists)
	return exists, mapError(err)
}

func (wrapped *transaction) ProcessHasOpenWork(
	ctx context.Context,
	organizationID, projectID, processID, excludeTurnID, excludeGateID string,
) (bool, error) {
	var found bool
	err := wrapped.tx.QueryRow(ctx, sqlProcessHasOpenWork, pgx.StrictNamedArgs{
		"organization_id": organizationID, "project_id": projectID,
		"process_id": processID, "exclude_turn_id": excludeTurnID,
		"exclude_gate_id": excludeGateID,
	}).Scan(&found)
	return found, mapError(err)
}

func (wrapped *transaction) ActiveProviderSessions(
	ctx context.Context,
	organizationID, projectID, bindingID string,
) (uint64, error) {
	var count uint64
	err := wrapped.tx.QueryRow(ctx, sqlProviderBindingActiveSessions, pgx.StrictNamedArgs{
		"organization_id": organizationID,
		"project_id":      projectID,
		"binding_id":      bindingID,
	}).Scan(&count)
	return count, mapError(err)
}

func (wrapped *transaction) NextProviderPoolSlot(
	ctx context.Context,
	cursor domainrepo.ProviderPoolCursor,
) (uint64, error) {
	var slot uint64
	err := wrapped.tx.QueryRow(ctx, sqlProviderPoolNextSlot, pgx.StrictNamedArgs{
		"role_id": cursor.RoleID, "policy_revision": cursor.PolicyRevision,
		"snapshot_sha256": cursor.SnapshotSHA256, "total_weight": cursor.TotalWeight,
	}).Scan(&slot)
	return slot, mapError(err)
}

func (wrapped *transaction) ActiveWorkClaimsForUpdate(
	ctx context.Context,
	organizationID, projectID, processRunID, turnID string,
) ([]entity.Resource, error) {
	rows, err := wrapped.tx.Query(ctx, sqlWorkClaimActiveForUpdate, pgx.StrictNamedArgs{
		"organization_id": organizationID, "project_id": projectID,
		"process_run_id": processRunID, "turn_id": turnID,
	})
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var result []entity.Resource
	for rows.Next() {
		item, scanErr := scanResource(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, item)
	}
	return result, mapError(rows.Err())
}

func (wrapped *transaction) ActiveOwnerGateForProcess(
	ctx context.Context,
	organizationID, projectID, processRunID string,
) (entity.Resource, error) {
	return scanResource(wrapped.tx.QueryRow(
		ctx, sqlOwnerGateActiveByProcess, pgx.StrictNamedArgs{
			"organization_id": organizationID, "project_id": projectID,
			"process_run_id": processRunID,
		},
	))
}

func (wrapped *transaction) ActiveProcessTurnCandidates(
	ctx context.Context,
	organizationID, projectID, processRunID string,
) ([]entity.Resource, error) {
	rows, err := wrapped.tx.Query(ctx, sqlProcessTurnActiveCandidates, pgx.StrictNamedArgs{
		"organization_id": organizationID, "project_id": projectID,
		"process_run_id": processRunID,
	})
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var result []entity.Resource
	for rows.Next() {
		item, scanErr := scanResource(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, item)
	}
	return result, mapError(rows.Err())
}

func (wrapped *transaction) ListTerminalOutbox(
	ctx context.Context,
	organizationID, projectID, afterEventID string,
	limit int,
) ([]domainrepo.OutboxFailure, error) {
	rows, err := wrapped.tx.Query(ctx, sqlOutboxTerminalList, pgx.StrictNamedArgs{
		"organization_id": organizationID, "project_id": projectID,
		"after_event_id": afterEventID, "limit": limit,
	})
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var result []domainrepo.OutboxFailure
	for rows.Next() {
		item, err := scanOutboxFailure(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, mapError(rows.Err())
}

func (wrapped *transaction) RepairTerminalOutbox(
	ctx context.Context,
	repair domainrepo.OutboxRepair,
) (domainrepo.OutboxFailure, error) {
	return scanOutboxFailure(wrapped.tx.QueryRow(
		ctx, sqlOutboxTerminalRepair, pgx.StrictNamedArgs{
			"event_id": repair.EventID, "expected_sequence": repair.ExpectedSequence,
			"expected_attempts": repair.ExpectedAttempts,
			"reason_code":       repair.ReasonCode, "evidence_sha256": repair.EvidenceSHA256,
			"actor_id": repair.ActorID, "correlation_id": repair.CorrelationID,
			"policy_revision":      repair.PolicyRevision,
			"idempotency_key_hash": repair.IdempotencyKeyHash,
			"request_hash":         repair.RequestHash, "repaired_at": repair.RepairedAt,
		},
	))
}

func (wrapped *transaction) NextOwnerGateDelivery(
	ctx context.Context,
	organizationID, projectID string,
	now time.Time,
) (entity.Resource, error) {
	return scanResource(wrapped.tx.QueryRow(
		ctx,
		sqlOwnerGateNextDelivery,
		pgx.StrictNamedArgs{
			"organization_id": organizationID,
			"project_id":      projectID,
			"now":             now,
		},
	))
}

func (wrapped *transaction) OwnerGateByDeliveryClaimKey(
	ctx context.Context,
	organizationID, projectID, deliveryClaimKeySHA256 string,
) (entity.Resource, error) {
	return scanResource(wrapped.tx.QueryRow(
		ctx,
		sqlOwnerGateByDeliveryClaimKey,
		pgx.StrictNamedArgs{
			"organization_id":           organizationID,
			"project_id":                projectID,
			"delivery_claim_key_sha256": deliveryClaimKeySHA256,
		},
	))
}

func (wrapped *transaction) NextExpiredOwnerGateCandidate(
	ctx context.Context,
	organizationID, projectID string,
) (entity.Resource, error) {
	return scanResource(wrapped.tx.QueryRow(
		ctx,
		sqlOwnerGateNextExpired,
		pgx.StrictNamedArgs{
			"organization_id": organizationID,
			"project_id":      projectID,
		},
	))
}

func (wrapped *transaction) GetRuntimeExecutionForUpdate(
	ctx context.Context,
	executionID string,
) (domainrepo.RuntimeExecution, error) {
	return scanRuntimeExecution(wrapped.tx.QueryRow(
		ctx, sqlRuntimeExecutionGetForUpdate,
		pgx.StrictNamedArgs{"execution_id": executionID},
	))
}

func (wrapped *transaction) GetRuntimeExecutionByTurnForUpdate(
	ctx context.Context,
	turnID string,
	attempt uint32,
) (domainrepo.RuntimeExecution, error) {
	return scanRuntimeExecution(wrapped.tx.QueryRow(
		ctx, sqlRuntimeExecutionGetByTurnForUpdate,
		pgx.StrictNamedArgs{"turn_id": turnID, "attempt": attempt},
	))
}

func (wrapped *transaction) GetRuntimeExecutionByTurn(
	ctx context.Context,
	turnID string,
	attempt uint32,
) (domainrepo.RuntimeExecution, error) {
	return scanRuntimeExecution(wrapped.tx.QueryRow(
		ctx, sqlRuntimeExecutionGetByTurn,
		pgx.StrictNamedArgs{"turn_id": turnID, "attempt": attempt},
	))
}

func (wrapped *transaction) GetCurrentResourceRetentionPolicy(
	ctx context.Context,
	organizationID string,
	projectID string,
) (domainrepo.ResourceRetentionPolicy, error) {
	var policy domainrepo.ResourceRetentionPolicy
	err := wrapped.tx.QueryRow(ctx, sqlResourceRetentionPolicyCurrent, pgx.StrictNamedArgs{
		"organization_id": organizationID,
		"project_id":      projectID,
	}).Scan(
		&policy.ID,
		&policy.Version,
		&policy.PVCRetentionSeconds,
		&policy.ArchiveRetentionSeconds,
		&policy.EffectiveFrom,
	)
	if err != nil {
		return domainrepo.ResourceRetentionPolicy{}, mapError(err)
	}
	if policy.RetiredAt.Equal(time.Unix(0, 0).UTC()) {
		policy.RetiredAt = time.Time{}
	}
	return policy, nil
}

func (wrapped *transaction) GetResourceRetentionPolicyForUpdate(
	ctx context.Context,
	organizationID, projectID string,
) (domainrepo.ResourceRetentionPolicy, error) {
	var policy domainrepo.ResourceRetentionPolicy
	err := wrapped.tx.QueryRow(ctx, sqlResourceRetentionPolicyGetForUpdate, pgx.StrictNamedArgs{
		"organization_id": organizationID,
		"project_id":      projectID,
	}).Scan(
		&policy.ID, &policy.Version, &policy.PVCRetentionSeconds,
		&policy.ArchiveRetentionSeconds, &policy.EffectiveFrom, &policy.RetiredAt,
	)
	if err != nil {
		return domainrepo.ResourceRetentionPolicy{}, mapError(err)
	}
	if policy.RetiredAt.Equal(time.Unix(0, 0).UTC()) {
		policy.RetiredAt = time.Time{}
	}
	return policy, nil
}

func (wrapped *transaction) GetResourceRetentionPolicyVersionForUpdate(
	ctx context.Context,
	organizationID, projectID, policyID string,
	version uint64,
) (domainrepo.ResourceRetentionPolicy, error) {
	var policy domainrepo.ResourceRetentionPolicy
	err := wrapped.tx.QueryRow(ctx, sqlResourceRetentionPolicyGetVersionForUpdate, pgx.StrictNamedArgs{
		"organization_id": organizationID, "project_id": projectID,
		"policy_id": policyID, "version": version,
	}).Scan(
		&policy.ID, &policy.Version, &policy.PVCRetentionSeconds,
		&policy.ArchiveRetentionSeconds, &policy.EffectiveFrom, &policy.RetiredAt,
	)
	if err != nil {
		return domainrepo.ResourceRetentionPolicy{}, mapError(err)
	}
	if policy.RetiredAt.Equal(time.Unix(0, 0).UTC()) {
		policy.RetiredAt = time.Time{}
	}
	return policy, nil
}

func (wrapped *transaction) InsertResourceRetentionPolicy(
	ctx context.Context,
	policy domainrepo.ResourceRetentionPolicy,
	actorID, reasonCode, idempotencyKeySHA256, requestSHA256 string,
) error {
	_, err := wrapped.tx.Exec(ctx, sqlResourceRetentionPolicyInsert, pgx.StrictNamedArgs{
		"organization_id": wrapped.organizationID,
		"project_id":      wrapped.projectID, "policy_id": policy.ID,
		"version": policy.Version, "pvc_retention_seconds": policy.PVCRetentionSeconds,
		"archive_retention_seconds": policy.ArchiveRetentionSeconds,
		"effective_at":              policy.EffectiveFrom, "actor_id": actorID,
		"reason_code": reasonCode, "idempotency_key_sha256": idempotencyKeySHA256,
		"request_sha256": requestSHA256, "supersedes_version": policy.Version - 1,
		"created_at": policy.EffectiveFrom,
	})
	return mapError(err)
}

func (wrapped *transaction) RetireResourceRetentionPolicy(
	ctx context.Context,
	policy domainrepo.ResourceRetentionPolicy,
	retiredAt time.Time,
) error {
	tag, err := wrapped.tx.Exec(ctx, sqlResourceRetentionPolicyRetire, pgx.StrictNamedArgs{
		"organization_id": wrapped.organizationID, "project_id": wrapped.projectID,
		"policy_id": policy.ID, "version": policy.Version, "retired_at": retiredAt,
	})
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func scanRuntimeRetentionHold(row pgx.Row) (domainrepo.RuntimeRetentionHold, error) {
	var hold domainrepo.RuntimeRetentionHold
	err := row.Scan(
		&hold.ID, &hold.OrganizationID, &hold.ProjectID, &hold.SessionID,
		&hold.Kind, &hold.State, &hold.Version, &hold.ActorID, &hold.ReasonCode,
		&hold.CreatedAt, &hold.UpdatedAt, &hold.ReleasedAt,
	)
	if err != nil {
		return domainrepo.RuntimeRetentionHold{}, mapError(err)
	}
	return hold, nil
}

func (wrapped *transaction) GetActiveRuntimeRetentionHoldForUpdate(
	ctx context.Context,
	sessionID, kind string,
) (domainrepo.RuntimeRetentionHold, error) {
	return scanRuntimeRetentionHold(wrapped.tx.QueryRow(
		ctx, sqlRuntimeRetentionHoldActiveForUpdate, pgx.StrictNamedArgs{
			"organization_id": wrapped.organizationID, "project_id": wrapped.projectID,
			"session_id": sessionID, "kind": kind,
		},
	))
}

func (wrapped *transaction) GetRuntimeRetentionHoldForUpdate(
	ctx context.Context,
	holdID string,
) (domainrepo.RuntimeRetentionHold, error) {
	return scanRuntimeRetentionHold(wrapped.tx.QueryRow(
		ctx, sqlRuntimeRetentionHoldGetForUpdate, pgx.StrictNamedArgs{
			"organization_id": wrapped.organizationID, "project_id": wrapped.projectID,
			"hold_id": holdID,
		},
	))
}

func (wrapped *transaction) InsertRuntimeRetentionHold(
	ctx context.Context,
	hold domainrepo.RuntimeRetentionHold,
	idempotencyKeySHA256, requestSHA256 string,
) error {
	_, err := wrapped.tx.Exec(ctx, sqlRuntimeRetentionHoldInsert, pgx.StrictNamedArgs{
		"organization_id": hold.OrganizationID, "project_id": hold.ProjectID,
		"session_id": hold.SessionID, "hold_id": hold.ID, "kind": hold.Kind,
		"actor_id": hold.ActorID, "reason_code": hold.ReasonCode,
		"idempotency_key_sha256": idempotencyKeySHA256, "request_sha256": requestSHA256,
		"created_at": hold.CreatedAt,
	})
	return mapError(err)
}

func (wrapped *transaction) ReleaseRuntimeRetentionHold(
	ctx context.Context,
	hold domainrepo.RuntimeRetentionHold,
	reasonCode string,
	releasedAt time.Time,
) error {
	tag, err := wrapped.tx.Exec(ctx, sqlRuntimeRetentionHoldRelease, pgx.StrictNamedArgs{
		"organization_id": hold.OrganizationID, "project_id": hold.ProjectID,
		"hold_id": hold.ID, "session_id": hold.SessionID,
		"expected_version": hold.Version, "reason_code": reasonCode,
		"released_at": releasedAt,
	})
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) InsertRuntimeExecution(
	ctx context.Context,
	execution domainrepo.RuntimeExecution,
) error {
	tag, err := wrapped.tx.Exec(ctx, sqlRuntimeExecutionInsert, runtimeExecutionArgs(execution))
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) UpdateRuntimeExecution(
	ctx context.Context,
	execution domainrepo.RuntimeExecution,
	expectedVersion, expectedFence uint64,
) error {
	arguments := runtimeExecutionUpdateArgs(execution, expectedVersion, expectedFence)
	tag, err := wrapped.tx.Exec(ctx, sqlRuntimeExecutionUpdate, arguments)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrVersionMismatch
	}
	return nil
}

func (wrapped *transaction) InsertRuntimeRestoreOperation(
	ctx context.Context,
	operation domainrepo.RuntimeRestoreOperation,
) error {
	tag, err := wrapped.tx.Exec(ctx, sqlRuntimeRestoreOperationInsert, pgx.StrictNamedArgs{
		"id":                      operation.ID,
		"organization_id":         operation.OrganizationID,
		"project_id":              operation.ProjectID,
		"owner_actor_id":          operation.OwnerActorID,
		"backup_execution_id":     operation.BackupID,
		"source_version":          operation.SourceVersion,
		"source_fence":            operation.SourceFence,
		"archive_sha256":          operation.ArchiveSHA256,
		"provenance_sha256":       operation.ProvenanceSHA256,
		"source_authority_sha256": operation.SourceAuthoritySHA256,
		"session_id":              operation.SessionID,
		"generation":              operation.Generation,
		"consumed_generation":     operation.ConsumedGeneration,
		"revoked_generation":      operation.RevokedGeneration,
		"target_turn_id":          operation.TargetTurnID,
		"target_attempt":          operation.TargetAttempt,
		"target_execution_id":     operation.TargetExecutionID,
		"created_at":              operation.CreatedAt,
		"updated_at":              operation.UpdatedAt,
	})
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) AdvanceRuntimeRestoreOperation(
	ctx context.Context,
	operation domainrepo.RuntimeRestoreOperation,
	expectedGeneration uint64,
) error {
	tag, err := wrapped.tx.Exec(ctx, sqlRuntimeRestoreOperationAdvance, pgx.StrictNamedArgs{
		"id": operation.ID, "generation": operation.Generation,
		"expected_generation": expectedGeneration, "target_turn_id": operation.TargetTurnID,
		"target_attempt": operation.TargetAttempt, "target_execution_id": operation.TargetExecutionID,
		"updated_at": operation.UpdatedAt,
	})
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) ConsumeRuntimeRestoreOperation(
	ctx context.Context,
	operationID string,
	generation uint64,
	targetTurnID string,
	targetAttempt uint32,
	sourceAuthoritySHA256 string,
	updatedAt time.Time,
) error {
	tag, err := wrapped.tx.Exec(ctx, sqlRuntimeRestoreOperationConsume, pgx.StrictNamedArgs{
		"id": operationID, "generation": generation, "target_turn_id": targetTurnID,
		"target_attempt": targetAttempt, "source_authority_sha256": sourceAuthoritySHA256,
		"updated_at": updatedAt,
	})
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) RevokeRuntimeRestoreOperation(
	ctx context.Context,
	operationID string,
	generation uint64,
	updatedAt time.Time,
) error {
	tag, err := wrapped.tx.Exec(ctx, sqlRuntimeRestoreOperationRevoke, pgx.StrictNamedArgs{
		"id": operationID, "generation": generation, "updated_at": updatedAt,
	})
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) AuthorizeRuntimeRestoreEffect(
	ctx context.Context,
	operationID, targetExecutionID string,
	generation uint64,
	sourceAuthoritySHA256, effect, effectSHA256 string,
	updatedAt time.Time,
) (bool, error) {
	var applied bool
	err := wrapped.tx.QueryRow(ctx, sqlRuntimeRestoreEffectAuthorize, pgx.StrictNamedArgs{
		"id": operationID, "target_execution_id": targetExecutionID,
		"generation": generation, "source_authority_sha256": sourceAuthoritySHA256,
		"effect": effect, "effect_sha256": effectSHA256, "updated_at": updatedAt,
	}).Scan(&applied)
	if err != nil {
		return false, mapError(err)
	}
	return applied, nil
}

func (wrapped *transaction) GetRuntimeRestoreOperation(
	ctx context.Context,
	operationID string,
) (domainrepo.RuntimeRestoreOperation, error) {
	return scanRuntimeRestoreOperation(wrapped.tx.QueryRow(
		ctx, sqlRuntimeRestoreOperationGet, pgx.StrictNamedArgs{"id": operationID},
	))
}

func (wrapped *transaction) GetRuntimeRestoreOperationByBackup(
	ctx context.Context,
	backupID string,
) (domainrepo.RuntimeRestoreOperation, error) {
	return scanRuntimeRestoreOperation(wrapped.tx.QueryRow(
		ctx, sqlRuntimeRestoreOperationGetByBackup,
		pgx.StrictNamedArgs{"backup_execution_id": backupID},
	))
}

func (wrapped *transaction) NextExpiredRuntimeExecution(
	ctx context.Context,
	organizationID, projectID, turnID string,
	attempt uint32,
) (domainrepo.RuntimeExecution, error) {
	return scanRuntimeExecution(wrapped.tx.QueryRow(
		ctx, sqlRuntimeExecutionNextExpired,
		pgx.StrictNamedArgs{
			"organization_id": organizationID,
			"project_id":      projectID,
			"turn_id":         turnID,
			"attempt":         attempt,
		},
	))
}

func (wrapped *transaction) InsertRuntimeIncident(
	ctx context.Context,
	incident domainrepo.RuntimeIncident,
) error {
	_, err := wrapped.tx.Exec(ctx, sqlRuntimeIncidentInsert, pgx.StrictNamedArgs{
		"id":              incident.ID,
		"organization_id": incident.OrganizationID,
		"project_id":      incident.ProjectID,
		"execution_id":    incident.ExecutionID,
		"execution_fence": incident.ExecutionFence,
		"kind":            incident.Kind,
		"evidence_sha256": incident.EvidenceSHA256,
		"workload_id":     incident.WorkloadID,
		"occurred_at":     incident.OccurredAt,
	})
	return mapError(err)
}

func (wrapped *transaction) AdmitOwnerSession(
	ctx context.Context,
	state domainrepo.OwnerSessionState,
) (domainrepo.OwnerSessionState, error) {
	result, err := scanOwnerSession(wrapped.tx.QueryRow(ctx, sqlOwnerSessionAdmit, pgx.StrictNamedArgs{
		"organization_id": state.OrganizationID, "actor_id": state.ActorID,
		"session_id": state.SessionID, "credential_digest_sha256": state.CredentialDigestSHA256,
		"current_revision": state.CurrentRevision, "updated_at": state.UpdatedAt,
	}))
	if errors.Is(err, errs.ErrNotFound) {
		return domainrepo.OwnerSessionState{}, errs.ErrPermissionDenied
	}
	return result, err
}

func (wrapped *transaction) RequireOwnerSession(
	ctx context.Context,
	state domainrepo.OwnerSessionState,
	allowRevoked bool,
) error {
	var admitted bool
	err := wrapped.tx.QueryRow(ctx, sqlOwnerSessionRequire, pgx.StrictNamedArgs{
		"organization_id": state.OrganizationID, "actor_id": state.ActorID,
		"session_id": state.SessionID, "credential_digest_sha256": state.CredentialDigestSHA256,
		"current_revision": state.CurrentRevision, "allow_revoked": allowRevoked,
	}).Scan(&admitted)
	if err != nil {
		return mapError(err)
	}
	if !admitted {
		return errs.ErrPermissionDenied
	}
	return nil
}

func (wrapped *transaction) RevokeOwnerSession(
	ctx context.Context,
	state domainrepo.OwnerSessionState,
) (domainrepo.OwnerSessionState, error) {
	result, err := scanOwnerSession(wrapped.tx.QueryRow(ctx, sqlOwnerSessionRevoke, pgx.StrictNamedArgs{
		"organization_id": state.OrganizationID, "actor_id": state.ActorID,
		"session_id": state.SessionID, "credential_digest_sha256": state.CredentialDigestSHA256,
		"current_revision": state.CurrentRevision, "updated_at": state.UpdatedAt,
	}))
	if errors.Is(err, errs.ErrNotFound) {
		return domainrepo.OwnerSessionState{}, errs.ErrPermissionDenied
	}
	return result, err
}

func scanOwnerSession(row pgx.Row) (domainrepo.OwnerSessionState, error) {
	var result domainrepo.OwnerSessionState
	var revokedAt *time.Time
	if err := row.Scan(&result.OrganizationID, &result.ActorID, &result.SessionID,
		&result.CredentialDigestSHA256, &result.CurrentRevision, &revokedAt,
		&result.UpdatedAt); err != nil {
		return domainrepo.OwnerSessionState{}, mapError(err)
	}
	if revokedAt != nil {
		result.RevokedAt = revokedAt.UTC()
	}
	result.UpdatedAt = result.UpdatedAt.UTC()
	return result, nil
}

func (wrapped *transaction) PrepareGatewayPublicTLS(
	ctx context.Context,
	state domainrepo.GatewayPublicTLSState,
	candidate domainrepo.GatewayPublicTLSMaterial,
	predecessorGeneration uint64,
	predecessorSHA256 string,
	updatedAt time.Time,
) (domainrepo.GatewayPublicTLSState, error) {
	result, err := scanGatewayPublicTLS(wrapped.tx.QueryRow(ctx, sqlGatewayPublicTLSPrepare, pgx.StrictNamedArgs{
		"organization_id": state.OrganizationID, "project_id": state.ProjectID,
		"workload_id": state.WorkloadID, "generation": candidate.Generation,
		"certificate_sha256": candidate.CertificateSHA256, "not_before": candidate.NotBefore,
		"not_after": candidate.NotAfter, "updated_at": updatedAt,
		"predecessor_generation":         predecessorGeneration,
		"predecessor_certificate_sha256": predecessorSHA256,
	}))
	if errors.Is(err, errs.ErrNotFound) {
		return domainrepo.GatewayPublicTLSState{}, errs.ErrPermissionDenied
	}
	return result, err
}

func (wrapped *transaction) ConfirmGatewayPublicTLS(
	ctx context.Context,
	state domainrepo.GatewayPublicTLSState,
	generation uint64,
	certificateSHA256 string,
	updatedAt time.Time,
	overlapExpiresAt time.Time,
) (domainrepo.GatewayPublicTLSState, error) {
	result, err := scanGatewayPublicTLS(wrapped.tx.QueryRow(ctx, sqlGatewayPublicTLSConfirm, pgx.StrictNamedArgs{
		"organization_id": state.OrganizationID, "project_id": state.ProjectID,
		"workload_id": state.WorkloadID, "generation": generation,
		"certificate_sha256": certificateSHA256, "updated_at": updatedAt,
		"overlap_expires_at": overlapExpiresAt,
	}))
	if errors.Is(err, errs.ErrNotFound) {
		return domainrepo.GatewayPublicTLSState{}, errs.ErrPermissionDenied
	}
	return result, err
}

func (wrapped *transaction) CheckGatewayPublicTLS(
	ctx context.Context,
	state domainrepo.GatewayPublicTLSState,
	generation uint64,
	certificateSHA256 string,
	checkedAt time.Time,
) (domainrepo.GatewayPublicTLSState, error) {
	result, err := scanGatewayPublicTLS(wrapped.tx.QueryRow(ctx, sqlGatewayPublicTLSCheck, pgx.StrictNamedArgs{
		"organization_id": state.OrganizationID, "project_id": state.ProjectID,
		"workload_id": state.WorkloadID, "generation": generation,
		"certificate_sha256": certificateSHA256, "checked_at": checkedAt,
	}))
	if errors.Is(err, errs.ErrNotFound) {
		return domainrepo.GatewayPublicTLSState{}, errs.ErrPermissionDenied
	}
	return result, err
}

func scanGatewayPublicTLS(row pgx.Row) (domainrepo.GatewayPublicTLSState, error) {
	var result domainrepo.GatewayPublicTLSState
	var appliedGeneration, pendingGeneration, previousGeneration *uint64
	var appliedSHA256, pendingSHA256, previousSHA256 *string
	var appliedNotBefore, appliedNotAfter, pendingNotBefore, pendingNotAfter *time.Time
	var previousNotBefore, previousNotAfter, overlapExpiresAt *time.Time
	if err := row.Scan(&result.OrganizationID, &result.ProjectID, &result.WorkloadID,
		&appliedGeneration, &appliedSHA256, &appliedNotBefore, &appliedNotAfter,
		&pendingGeneration, &pendingSHA256, &pendingNotBefore, &pendingNotAfter,
		&previousGeneration, &previousSHA256, &previousNotBefore, &previousNotAfter,
		&overlapExpiresAt, &result.UpdatedAt); err != nil {
		return domainrepo.GatewayPublicTLSState{}, mapError(err)
	}
	if appliedGeneration != nil {
		result.Applied = domainrepo.GatewayPublicTLSMaterial{
			Generation:        *appliedGeneration,
			CertificateSHA256: *appliedSHA256, NotBefore: appliedNotBefore.UTC(), NotAfter: appliedNotAfter.UTC(),
		}
	}
	if pendingGeneration != nil {
		result.Pending = domainrepo.GatewayPublicTLSMaterial{
			Generation:        *pendingGeneration,
			CertificateSHA256: *pendingSHA256, NotBefore: pendingNotBefore.UTC(), NotAfter: pendingNotAfter.UTC(),
		}
	}
	if previousGeneration != nil {
		result.Previous = domainrepo.GatewayPublicTLSMaterial{
			Generation:        *previousGeneration,
			CertificateSHA256: *previousSHA256, NotBefore: previousNotBefore.UTC(), NotAfter: previousNotAfter.UTC(),
		}
		result.OverlapExpiresAt = overlapExpiresAt.UTC()
	}
	result.UpdatedAt = result.UpdatedAt.UTC()
	return result, nil
}

func (wrapped *transaction) GetIntegrationContinuationForUpdate(
	ctx context.Context,
	continuationID string,
) (domainrepo.IntegrationContinuation, error) {
	return scanIntegrationContinuation(wrapped.tx.QueryRow(
		ctx, sqlIntegrationContinuationGetForUpdate,
		pgx.StrictNamedArgs{"continuation_id": continuationID},
	))
}

func (wrapped *transaction) AdmitContinuationGrantVerifierState(
	ctx context.Context,
	keysetRevision, highWatermark, servedGeneration uint64,
	keysetSHA256 string,
	signerGeneration uint64,
) error {
	var admitted bool
	if err := wrapped.tx.QueryRow(ctx, sqlContinuationGrantKeysetAdmit, pgx.StrictNamedArgs{
		"keyset_revision": keysetRevision, "high_watermark": highWatermark,
		"served_generation": servedGeneration, "keyset_sha256": keysetSHA256,
		"signer_generation": signerGeneration,
	}).Scan(&admitted); err != nil {
		return mapError(err)
	}
	if !admitted {
		return errs.ErrPermissionDenied
	}
	return nil
}

func (wrapped *transaction) GetIntegrationContinuation(
	ctx context.Context,
	continuationID string,
) (domainrepo.IntegrationContinuation, error) {
	return scanIntegrationContinuation(wrapped.tx.QueryRow(
		ctx, sqlIntegrationContinuationGet,
		pgx.StrictNamedArgs{"continuation_id": continuationID},
	))
}

func (wrapped *transaction) GetIntegrationContinuationByContinuationTurn(
	ctx context.Context,
	turnID string,
) (domainrepo.IntegrationContinuation, error) {
	return scanIntegrationContinuation(wrapped.tx.QueryRow(
		ctx, sqlIntegrationContinuationGetByContinuationTurn,
		pgx.StrictNamedArgs{"turn_id": turnID},
	))
}

func (wrapped *transaction) IntegrationContinuationBlocksCleanup(
	ctx context.Context,
	organizationID, projectID, sessionID string,
) (bool, error) {
	var blocked bool
	err := wrapped.tx.QueryRow(
		ctx,
		sqlIntegrationContinuationBlocksCleanup,
		pgx.StrictNamedArgs{
			"organization_id": organizationID,
			"project_id":      projectID,
			"session_id":      sessionID,
		},
	).Scan(&blocked)
	return blocked, mapError(err)
}

func (wrapped *transaction) NextExpiredIntegrationContinuation(
	ctx context.Context,
	organizationID, projectID, turnID string,
	attempt uint32,
) (domainrepo.IntegrationContinuation, error) {
	return scanIntegrationContinuation(wrapped.tx.QueryRow(
		ctx, sqlIntegrationContinuationNextExpired,
		pgx.StrictNamedArgs{
			"organization_id": organizationID,
			"project_id":      projectID,
			"turn_id":         turnID,
			"attempt":         attempt,
		},
	))
}

func (wrapped *transaction) InsertIntegrationContinuation(
	ctx context.Context,
	continuation domainrepo.IntegrationContinuation,
) error {
	arguments, err := integrationContinuationArgs(continuation)
	if err != nil {
		return err
	}
	tag, err := wrapped.tx.Exec(
		ctx, sqlIntegrationContinuationInsert, arguments,
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) UpdateIntegrationContinuation(
	ctx context.Context,
	continuation domainrepo.IntegrationContinuation,
	expectedVersion, expectedFence uint64,
) error {
	arguments := integrationContinuationUpdateArgs(
		continuation, expectedVersion, expectedFence,
	)
	tag, err := wrapped.tx.Exec(ctx, sqlIntegrationContinuationUpdate, arguments)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrVersionMismatch
	}
	return nil
}

func runtimeExecutionArgs(execution domainrepo.RuntimeExecution) pgx.StrictNamedArgs {
	return pgx.StrictNamedArgs{
		"id": execution.ID, "organization_id": execution.OrganizationID,
		"project_id": execution.ProjectID, "process_id": execution.ProcessID,
		"session_id": execution.SessionID, "thread_id": execution.ThreadID,
		"role_id": execution.RoleID, "turn_id": execution.TurnID,
		"schedule_occurrence_id": execution.ScheduleOccurrenceID,
		"attempt":                execution.Attempt, "runtime_revision_id": execution.RuntimeRevisionID,
		"runtime_revision_version": execution.RuntimeRevisionVersion,
		"runtime_revision_sha256":  execution.RuntimeRevisionSHA256,
		"immutable_input_sha256":   execution.ImmutableInputSHA256,
		"resource_class":           execution.ResourceClass,
		"cluster_access_profile":   execution.ClusterAccessProfile,
		"workload_id":              execution.WorkloadID,
		"workload_spiffe_id":       execution.WorkloadSPIFFEID,
		"grant_generation":         execution.GrantGeneration,
		"version":                  execution.Version, "fence": execution.Fence,
		"state": execution.State, "lease_id": execution.LeaseID,
		"lease_token_sha256":                          execution.LeaseTokenSHA256,
		"lease_expires_at":                            execution.LeaseExpiresAt,
		"terminal_outcome":                            execution.TerminalOutcome,
		"terminal_reference":                          execution.TerminalReference,
		"terminal_sha256":                             execution.TerminalSHA256,
		"archive_reference":                           execution.ArchiveReference,
		"archive_sha256":                              execution.ArchiveSHA256,
		"archive_object_key":                          execution.ArchiveObjectKey,
		"archive_version_id":                          execution.ArchiveVersionID,
		"archive_kms_key_arn":                         execution.ArchiveKMSKeyARN,
		"archive_object_lock_mode":                    execution.ArchiveObjectLockMode,
		"archive_provenance_sha256":                   execution.ArchiveProvenanceSHA256,
		"restore_proof_reference":                     execution.RestoreProofReference,
		"restore_proof_sha256":                        execution.RestoreProofSHA256,
		"restore_verifier_workload_id":                execution.RestoreVerifierWorkload,
		"restore_verifier_spiffe_id":                  execution.RestoreVerifierSPIFFEID,
		"restore_verifier_generation":                 execution.RestoreVerifierGeneration,
		"cleanup_authorization_id":                    execution.CleanupAuthorizationID,
		"cleanup_authorization_expires_at":            execution.CleanupAuthorizationExpiresAt,
		"cleanup_authorization_state":                 execution.CleanupAuthorizationState,
		"cleanup_authorization_generation":            execution.CleanupAuthorizationGeneration,
		"cleanup_consumed_at":                         execution.CleanupConsumedAt,
		"cleanup_pvc_name":                            execution.CleanupPVCName,
		"cleanup_pvc_uid":                             execution.CleanupPVCUID,
		"cleanup_pvc_resource_version":                execution.CleanupPVCResourceVersion,
		"cleanup_claimed_at":                          execution.CleanupClaimedAt,
		"cleanup_eligible_at":                         execution.CleanupEligibleAt,
		"cleanup_not_found_at":                        execution.CleanupNotFoundAt,
		"cleanup_deletion_proof_sha256":               execution.CleanupDeletionProofSHA256,
		"restore_source_execution_id":                 execution.RestoreSourceExecutionID,
		"restore_source_archive_reference":            execution.RestoreSourceArchiveReference,
		"restore_source_archive_sha256":               execution.RestoreSourceArchiveSHA256,
		"restore_source_runtime_revision_sha256":      execution.RestoreSourceRuntimeRevisionSHA256,
		"restore_source_immutable_input_sha256":       execution.RestoreSourceImmutableInputSHA256,
		"restore_source_proof_reference":              execution.RestoreSourceProofReference,
		"restore_source_proof_sha256":                 execution.RestoreSourceProofSHA256,
		"restore_source_version":                      execution.RestoreSourceVersion,
		"restore_source_fence":                        execution.RestoreSourceFence,
		"restore_source_archive_object_key":           execution.RestoreSourceArchiveObjectKey,
		"restore_source_archive_version_id":           execution.RestoreSourceArchiveVersionID,
		"restore_source_archive_kms_key_arn":          execution.RestoreSourceArchiveKMSKeyARN,
		"restore_source_archive_object_lock_mode":     execution.RestoreSourceArchiveObjectLockMode,
		"restore_source_archive_retain_until":         execution.RestoreSourceArchiveRetainUntil,
		"restore_source_retention_policy_id":          execution.RestoreSourceRetentionPolicyID,
		"restore_source_retention_policy_version":     execution.RestoreSourceRetentionPolicyVersion,
		"restore_source_provenance_sha256":            execution.RestoreSourceProvenanceSHA256,
		"effective_runtime_sha256":                    execution.EffectiveRuntimeSHA256,
		"agent_session_key":                           execution.AgentSessionKey,
		"agent_session_id":                            execution.AgentSessionID,
		"agent_session_turn_id":                       execution.AgentSessionTurnID,
		"agent_run_id":                                execution.AgentRunID,
		"agent_binding_sha256":                        execution.AgentBindingSHA256,
		"retention_policy_id":                         execution.RetentionPolicyID,
		"retention_policy_version":                    execution.RetentionPolicyVersion,
		"pvc_retention_seconds":                       execution.PVCRetentionSeconds,
		"archive_retention_seconds":                   execution.ArchiveRetentionSeconds,
		"archive_retain_until":                        execution.ArchiveRetainUntil,
		"pvc_cleanup_eligible_at":                     execution.PVCCleanupEligibleAt,
		"capacity_observation_expires_at":             execution.CapacityObservationExpiresAt,
		"reschedule_after":                            execution.RescheduleAfter,
		"restore_assignment_state":                    execution.RestoreAssignmentState,
		"restore_assignment_generation":               execution.RestoreAssignmentGeneration,
		"restore_target_pvc_name":                     execution.RestoreTargetPVCName,
		"restore_target_pvc_uid":                      execution.RestoreTargetPVCUID,
		"restore_target_pvc_resource_version":         execution.RestoreTargetPVCResourceVersion,
		"rehydrate_proof_reference":                   execution.RehydrateProofReference,
		"rehydrate_proof_sha256":                      execution.RehydrateProofSHA256,
		"credential_snapshot_sha256":                  execution.CredentialSnapshotSHA256,
		"workload_ticket_sha256":                      execution.WorkloadTicketSHA256,
		"provider_binding_id":                         execution.ProviderBindingID,
		"provider_binding_version":                    execution.ProviderBindingVersion,
		"provider_binding_sha256":                     execution.ProviderBindingSHA256,
		"codex_session_id":                            execution.CodexSessionID,
		"codex_archive_relative_path":                 execution.CodexArchiveRelativePath,
		"codex_archive_sha256":                        execution.CodexArchiveSHA256,
		"codex_archive_provenance":                    execution.CodexArchiveProvenance,
		"codex_delivery_recovery_source_execution_id": execution.CodexDeliveryRecoverySourceExecutionID,
		"materializations":                            execution.Materializations,
		"created_at":                                  execution.CreatedAt, "updated_at": execution.UpdatedAt,
	}
}

func runtimeExecutionUpdateArgs(
	execution domainrepo.RuntimeExecution,
	expectedVersion, expectedFence uint64,
) pgx.StrictNamedArgs {
	return pgx.StrictNamedArgs{
		"id": execution.ID, "version": execution.Version, "fence": execution.Fence,
		"state": execution.State, "lease_id": execution.LeaseID,
		"lease_token_sha256":                          execution.LeaseTokenSHA256,
		"lease_expires_at":                            execution.LeaseExpiresAt,
		"terminal_outcome":                            execution.TerminalOutcome,
		"terminal_reference":                          execution.TerminalReference,
		"terminal_sha256":                             execution.TerminalSHA256,
		"archive_reference":                           execution.ArchiveReference,
		"archive_sha256":                              execution.ArchiveSHA256,
		"archive_object_key":                          execution.ArchiveObjectKey,
		"archive_version_id":                          execution.ArchiveVersionID,
		"archive_kms_key_arn":                         execution.ArchiveKMSKeyARN,
		"archive_object_lock_mode":                    execution.ArchiveObjectLockMode,
		"archive_provenance_sha256":                   execution.ArchiveProvenanceSHA256,
		"restore_proof_reference":                     execution.RestoreProofReference,
		"restore_proof_sha256":                        execution.RestoreProofSHA256,
		"restore_verifier_workload_id":                execution.RestoreVerifierWorkload,
		"restore_verifier_spiffe_id":                  execution.RestoreVerifierSPIFFEID,
		"restore_verifier_generation":                 execution.RestoreVerifierGeneration,
		"cleanup_authorization_id":                    execution.CleanupAuthorizationID,
		"cleanup_authorization_expires_at":            execution.CleanupAuthorizationExpiresAt,
		"cleanup_authorization_state":                 execution.CleanupAuthorizationState,
		"cleanup_authorization_generation":            execution.CleanupAuthorizationGeneration,
		"cleanup_consumed_at":                         execution.CleanupConsumedAt,
		"cleanup_pvc_name":                            execution.CleanupPVCName,
		"cleanup_pvc_uid":                             execution.CleanupPVCUID,
		"cleanup_pvc_resource_version":                execution.CleanupPVCResourceVersion,
		"cleanup_claimed_at":                          execution.CleanupClaimedAt,
		"cleanup_eligible_at":                         execution.CleanupEligibleAt,
		"cleanup_not_found_at":                        execution.CleanupNotFoundAt,
		"cleanup_deletion_proof_sha256":               execution.CleanupDeletionProofSHA256,
		"restore_assignment_state":                    execution.RestoreAssignmentState,
		"restore_assignment_generation":               execution.RestoreAssignmentGeneration,
		"restore_target_pvc_name":                     execution.RestoreTargetPVCName,
		"restore_target_pvc_uid":                      execution.RestoreTargetPVCUID,
		"restore_target_pvc_resource_version":         execution.RestoreTargetPVCResourceVersion,
		"rehydrate_proof_reference":                   execution.RehydrateProofReference,
		"rehydrate_proof_sha256":                      execution.RehydrateProofSHA256,
		"archive_retain_until":                        execution.ArchiveRetainUntil,
		"pvc_cleanup_eligible_at":                     execution.PVCCleanupEligibleAt,
		"codex_session_id":                            execution.CodexSessionID,
		"codex_archive_relative_path":                 execution.CodexArchiveRelativePath,
		"codex_archive_sha256":                        execution.CodexArchiveSHA256,
		"codex_archive_provenance":                    execution.CodexArchiveProvenance,
		"codex_delivery_recovery_source_execution_id": execution.CodexDeliveryRecoverySourceExecutionID,
		"updated_at":                                  execution.UpdatedAt,
		"expected_version":                            expectedVersion, "expected_fence": expectedFence,
	}
}

func integrationContinuationArgs(
	continuation domainrepo.IntegrationContinuation,
) (pgx.StrictNamedArgs, error) {
	bindings := continuation.CredentialBindings
	if bindings == nil {
		bindings = []domainrepo.PinnedIntegrationResource{}
	}
	credentialBindings, err := json.Marshal(bindings)
	if err != nil {
		return nil, errs.ErrInternal
	}
	return pgx.StrictNamedArgs{
		"id": continuation.ID, "organization_id": continuation.OrganizationID,
		"project_id": continuation.ProjectID, "process_id": continuation.ProcessID,
		"session_id":      continuation.SessionID,
		"session_version": continuation.SessionVersion,
		"thread_id":       continuation.ThreadID, "role_id": continuation.RoleID,
		"turn_id": continuation.TurnID, "turn_version": continuation.TurnVersion,
		"attempt":                  continuation.Attempt,
		"runtime_revision_id":      continuation.RuntimeRevisionID,
		"runtime_revision_version": continuation.RuntimeRevisionVersion,
		"runtime_revision_sha256":  continuation.RuntimeRevisionSHA256,
		"immutable_input_sha256":   continuation.ImmutableInputSHA256,
		"grant_generation":         continuation.GrantGeneration,
		"invocation_id":            continuation.InvocationID, "approval_id": continuation.ApprovalID,
		"integration_id":      continuation.IntegrationID,
		"integration_version": continuation.IntegrationVersion,
		"integration_sha256":  continuation.IntegrationSHA256,
		"credential_bindings": credentialBindings,
		"request_sha256":      continuation.RequestSHA256,
		"approval_state":      continuation.ApprovalState,
		"execution_state":     continuation.ExecutionState,
		"continuation_state":  continuation.ContinuationState,
		"version":             continuation.Version, "fence": continuation.Fence,
		"approval_expires_at":                   continuation.ApprovalExpiresAt,
		"decision_reference":                    continuation.DecisionReference,
		"decision_sha256":                       continuation.DecisionSHA256,
		"result_reference":                      continuation.ResultReference,
		"result_sha256":                         continuation.ResultSHA256,
		"error_code":                            continuation.ErrorCode,
		"error_reference":                       continuation.ErrorReference,
		"error_sha256":                          continuation.ErrorSHA256,
		"continuation_turn_id":                  continuation.ContinuationTurnID,
		"continuation_turn_version":             continuation.ContinuationTurnVersion,
		"continuation_attempt":                  continuation.ContinuationAttempt,
		"continuation_runtime_revision_id":      continuation.ContinuationRuntimeRevisionID,
		"continuation_runtime_revision_version": continuation.ContinuationRuntimeRevisionVersion,
		"continuation_input_sha256":             continuation.ContinuationInputSHA256,
		"created_at":                            continuation.CreatedAt, "updated_at": continuation.UpdatedAt,
	}, nil
}

func integrationContinuationUpdateArgs(
	continuation domainrepo.IntegrationContinuation,
	expectedVersion, expectedFence uint64,
) pgx.StrictNamedArgs {
	return pgx.StrictNamedArgs{
		"id":                 continuation.ID,
		"approval_state":     continuation.ApprovalState,
		"execution_state":    continuation.ExecutionState,
		"continuation_state": continuation.ContinuationState,
		"version":            continuation.Version, "fence": continuation.Fence,
		"decision_reference":                    continuation.DecisionReference,
		"decision_sha256":                       continuation.DecisionSHA256,
		"result_reference":                      continuation.ResultReference,
		"result_sha256":                         continuation.ResultSHA256,
		"error_code":                            continuation.ErrorCode,
		"error_reference":                       continuation.ErrorReference,
		"error_sha256":                          continuation.ErrorSHA256,
		"continuation_turn_id":                  continuation.ContinuationTurnID,
		"continuation_turn_version":             continuation.ContinuationTurnVersion,
		"continuation_attempt":                  continuation.ContinuationAttempt,
		"continuation_runtime_revision_id":      continuation.ContinuationRuntimeRevisionID,
		"continuation_runtime_revision_version": continuation.ContinuationRuntimeRevisionVersion,
		"continuation_input_sha256":             continuation.ContinuationInputSHA256,
		"updated_at":                            continuation.UpdatedAt,
		"expected_version":                      expectedVersion, "expected_fence": expectedFence,
	}
}

type rowScanner interface {
	Scan(...any) error
}

func scanBackup(row rowScanner) (domainrepo.Backup, error) {
	var backup domainrepo.Backup
	var freshnessBase, latestExpired time.Time
	var archiveCount, expiredCount int64
	err := row.Scan(
		&backup.ID, &backup.OrganizationID, &backup.ProjectID, &backup.SessionID,
		&backup.SourceVersion, &backup.SourceFence,
		&freshnessBase, &latestExpired, &archiveCount, &expiredCount,
		&backup.SourceRuntimeRevisionSHA256, &backup.SourceImmutableInputSHA256,
		&backup.ArchiveSHA256, &backup.ProvenanceSHA256,
		&backup.RuntimeState, &backup.State, &backup.Restorable, &backup.RestoreOperationID,
		&backup.CreatedAt, &backup.AvailableAt, &backup.RetainUntil,
	)
	if err != nil {
		return domainrepo.Backup{}, mapError(err)
	}
	backup.CreatedAt = backup.CreatedAt.UTC()
	backup.AvailableAt = nullableEpoch(backup.AvailableAt)
	backup.RetainUntil = nullableEpoch(backup.RetainUntil)
	if archiveCount <= 0 || expiredCount < 0 {
		return domainrepo.Backup{}, errs.ErrStateConflict
	}
	backup.Version, backup.UpdatedAt, err = backupProjectionFreshness(
		freshnessBase, nullableEpoch(latestExpired), uint64(archiveCount), uint64(expiredCount),
	)
	if err != nil {
		return domainrepo.Backup{}, err
	}
	return backup, nil
}

// backupProjectionFreshness превращает единый session-scoped timestamp,
// монотонное число архивов и retention deadlines в согласованные version/updatedAt.
func backupProjectionFreshness(
	base, latestExpired time.Time,
	archiveCount, expiredCount uint64,
) (uint64, time.Time, error) {
	base = base.UTC()
	if latestExpired.After(base) {
		base = latestExpired.UTC()
	}
	maximumCounter := uint64((24 * time.Hour) / time.Microsecond)
	if base.IsZero() || archiveCount == 0 || archiveCount > maximumCounter ||
		expiredCount > maximumCounter-archiveCount {
		return 0, time.Time{}, errs.ErrStateConflict
	}
	updatedAt := base.Add(time.Duration(archiveCount+expiredCount) * time.Microsecond)
	version := updatedAt.UnixMicro()
	if version <= 0 || version > 9007199254740991 {
		return 0, time.Time{}, errs.ErrStateConflict
	}
	return uint64(version), updatedAt, nil
}

func scanRuntimeRestoreOperation(row rowScanner) (domainrepo.RuntimeRestoreOperation, error) {
	var operation domainrepo.RuntimeRestoreOperation
	err := row.Scan(
		&operation.ID, &operation.OrganizationID, &operation.ProjectID,
		&operation.OwnerActorID, &operation.BackupID, &operation.SourceVersion,
		&operation.SourceFence,
		&operation.ArchiveSHA256, &operation.ProvenanceSHA256, &operation.SourceAuthoritySHA256,
		&operation.SessionID, &operation.Generation, &operation.ConsumedGeneration,
		&operation.RevokedGeneration, &operation.TargetTurnID, &operation.TargetAttempt,
		&operation.TargetExecutionID, &operation.TargetExecutionVersion,
		&operation.TargetTurnVersion,
		&operation.TargetExecutionState, &operation.TargetRestoreAssignmentState, &operation.TargetTurnState,
		&operation.CreatedAt, &operation.UpdatedAt,
	)
	if err != nil {
		return domainrepo.RuntimeRestoreOperation{}, mapError(err)
	}
	operation.CreatedAt = operation.CreatedAt.UTC()
	operation.UpdatedAt = operation.UpdatedAt.UTC()
	return operation, nil
}

func nullableEpoch(value time.Time) time.Time {
	if value.Equal(time.Unix(0, 0).UTC()) {
		return time.Time{}
	}
	return value.UTC()
}

func scanRuntimeExecution(row rowScanner) (domainrepo.RuntimeExecution, error) {
	var execution domainrepo.RuntimeExecution
	var materializationsRaw []byte
	err := row.Scan(
		&execution.ID, &execution.OrganizationID, &execution.ProjectID,
		&execution.ProcessID, &execution.SessionID, &execution.ThreadID,
		&execution.RoleID, &execution.TurnID, &execution.ScheduleOccurrenceID, &execution.Attempt,
		&execution.RuntimeRevisionID, &execution.RuntimeRevisionVersion,
		&execution.RuntimeRevisionSHA256, &execution.ImmutableInputSHA256,
		&execution.ResourceClass, &execution.ClusterAccessProfile,
		&execution.WorkloadID, &execution.WorkloadSPIFFEID,
		&execution.GrantGeneration, &execution.Version, &execution.Fence,
		&execution.State, &execution.LeaseID, &execution.LeaseTokenSHA256,
		&execution.LeaseExpiresAt, &execution.TerminalOutcome,
		&execution.TerminalReference, &execution.TerminalSHA256,
		&execution.ArchiveReference, &execution.ArchiveSHA256,
		&execution.ArchiveObjectKey, &execution.ArchiveVersionID,
		&execution.ArchiveKMSKeyARN, &execution.ArchiveObjectLockMode,
		&execution.ArchiveProvenanceSHA256,
		&execution.RestoreProofReference, &execution.RestoreProofSHA256,
		&execution.RestoreVerifierWorkload, &execution.RestoreVerifierSPIFFEID,
		&execution.RestoreVerifierGeneration, &execution.CleanupAuthorizationID,
		&execution.CleanupAuthorizationExpiresAt, &execution.CleanupAuthorizationState,
		&execution.CleanupAuthorizationGeneration, &execution.CleanupConsumedAt,
		&execution.CleanupPVCName, &execution.CleanupPVCUID,
		&execution.CleanupPVCResourceVersion, &execution.CleanupClaimedAt,
		&execution.CleanupEligibleAt, &execution.CleanupNotFoundAt,
		&execution.CleanupDeletionProofSHA256,
		&execution.RestoreSourceExecutionID, &execution.RestoreSourceArchiveReference,
		&execution.RestoreSourceArchiveSHA256, &execution.RestoreSourceRuntimeRevisionSHA256,
		&execution.RestoreSourceImmutableInputSHA256, &execution.RestoreSourceProofReference,
		&execution.RestoreSourceProofSHA256, &execution.RestoreSourceVersion,
		&execution.RestoreSourceFence, &execution.RestoreSourceArchiveObjectKey,
		&execution.RestoreSourceArchiveVersionID, &execution.RestoreSourceArchiveKMSKeyARN,
		&execution.RestoreSourceArchiveObjectLockMode, &execution.RestoreSourceArchiveRetainUntil,
		&execution.RestoreSourceRetentionPolicyID, &execution.RestoreSourceRetentionPolicyVersion,
		&execution.RestoreSourceProvenanceSHA256,
		&execution.EffectiveRuntimeSHA256, &execution.AgentSessionKey,
		&execution.AgentSessionID, &execution.AgentSessionTurnID,
		&execution.AgentRunID, &execution.AgentBindingSHA256,
		&execution.RetentionPolicyID, &execution.RetentionPolicyVersion,
		&execution.PVCRetentionSeconds, &execution.ArchiveRetentionSeconds,
		&execution.ArchiveRetainUntil,
		&execution.PVCCleanupEligibleAt, &execution.CapacityObservationExpiresAt,
		&execution.RescheduleAfter, &execution.RestoreAssignmentState,
		&execution.RestoreAssignmentGeneration, &execution.RestoreTargetPVCName,
		&execution.RestoreTargetPVCUID, &execution.RestoreTargetPVCResourceVersion,
		&execution.RehydrateProofReference, &execution.RehydrateProofSHA256,
		&execution.CredentialSnapshotSHA256, &execution.WorkloadTicketSHA256,
		&execution.ProviderBindingID, &execution.ProviderBindingVersion,
		&execution.ProviderBindingSHA256, &execution.CodexSessionID,
		&execution.CodexArchiveRelativePath, &execution.CodexArchiveSHA256,
		&execution.CodexArchiveProvenance, &execution.CodexDeliveryRecoverySourceExecutionID,
		&materializationsRaw,
		&execution.CreatedAt, &execution.UpdatedAt,
	)
	if err != nil {
		return domainrepo.RuntimeExecution{}, mapError(err)
	}
	if json.Unmarshal(materializationsRaw, &execution.Materializations) != nil {
		return domainrepo.RuntimeExecution{}, errs.ErrInternal
	}
	return execution, nil
}

func scanIntegrationContinuation(
	row rowScanner,
) (domainrepo.IntegrationContinuation, error) {
	var continuation domainrepo.IntegrationContinuation
	var credentialBindings []byte
	err := row.Scan(
		&continuation.ID, &continuation.OrganizationID, &continuation.ProjectID,
		&continuation.ProcessID, &continuation.SessionID,
		&continuation.SessionVersion, &continuation.ThreadID, &continuation.RoleID,
		&continuation.TurnID, &continuation.TurnVersion, &continuation.Attempt,
		&continuation.RuntimeRevisionID, &continuation.RuntimeRevisionVersion,
		&continuation.RuntimeRevisionSHA256, &continuation.ImmutableInputSHA256,
		&continuation.GrantGeneration, &continuation.InvocationID,
		&continuation.ApprovalID, &continuation.IntegrationID,
		&continuation.IntegrationVersion, &continuation.IntegrationSHA256,
		&credentialBindings,
		&continuation.RequestSHA256, &continuation.ApprovalState,
		&continuation.ExecutionState, &continuation.ContinuationState,
		&continuation.Version, &continuation.Fence,
		&continuation.ApprovalExpiresAt, &continuation.DecisionReference,
		&continuation.DecisionSHA256, &continuation.ResultReference,
		&continuation.ResultSHA256, &continuation.ErrorCode,
		&continuation.ErrorReference, &continuation.ErrorSHA256,
		&continuation.ContinuationTurnID, &continuation.ContinuationTurnVersion,
		&continuation.ContinuationAttempt,
		&continuation.ContinuationRuntimeRevisionID,
		&continuation.ContinuationRuntimeRevisionVersion,
		&continuation.ContinuationInputSHA256, &continuation.CreatedAt,
		&continuation.UpdatedAt,
	)
	if err != nil {
		return domainrepo.IntegrationContinuation{}, mapError(err)
	}
	if json.Unmarshal(credentialBindings, &continuation.CredentialBindings) != nil ||
		len(continuation.CredentialBindings) > 16 {
		return domainrepo.IntegrationContinuation{}, errs.ErrInternal
	}
	return continuation, nil
}

func scanOutboxFailure(row rowScanner) (domainrepo.OutboxFailure, error) {
	var item domainrepo.OutboxFailure
	err := row.Scan(
		&item.EventID, &item.OrderingKey, &item.EventSequence, &item.EventName,
		&item.AggregateID, &item.Attempts, &item.RepairCount,
		&item.LastErrorClass, &item.OccurredAt, &item.UpdatedAt,
	)
	if err != nil {
		return domainrepo.OutboxFailure{}, mapError(err)
	}
	return item, nil
}

func scanResource(row rowScanner) (entity.Resource, error) {
	var resource entity.Resource
	var kind, state string
	var specRaw []byte
	err := row.Scan(
		&resource.ID,
		&resource.OrganizationID,
		&resource.ProjectID,
		&resource.ParentID,
		&resource.OwnerActorID,
		&kind,
		&resource.Name,
		&state,
		&resource.Version,
		&specRaw,
		&resource.CreatedAt,
		&resource.UpdatedAt,
	)
	if err != nil {
		return entity.Resource{}, mapError(err)
	}
	resource.Kind = enum.Kind(kind)
	resource.State = enum.State(state)
	resource.Spec, err = unmarshalSpec(resource.Kind, specRaw)
	if err != nil || resource.Validate() != nil {
		return entity.Resource{}, errs.ErrInternal
	}
	return resource, nil
}

func scanMemorySearchHit(row rowScanner) (domainrepo.MemorySearchHit, error) {
	var hit domainrepo.MemorySearchHit
	var kind, state string
	var specRaw []byte
	err := row.Scan(
		&hit.Resource.ID,
		&hit.Resource.OrganizationID,
		&hit.Resource.ProjectID,
		&hit.Resource.ParentID,
		&hit.Resource.OwnerActorID,
		&kind,
		&hit.Resource.Name,
		&state,
		&hit.Resource.Version,
		&specRaw,
		&hit.Resource.CreatedAt,
		&hit.Resource.UpdatedAt,
		&hit.TextRank,
		&hit.VectorDistance,
		&hit.VectorProjectionUsed,
	)
	if err != nil {
		return domainrepo.MemorySearchHit{}, mapError(err)
	}
	hit.Resource.Kind = enum.Kind(kind)
	hit.Resource.State = enum.State(state)
	hit.Resource.Spec, err = unmarshalSpec(hit.Resource.Kind, specRaw)
	if err != nil || hit.Resource.Validate() != nil {
		return domainrepo.MemorySearchHit{}, errs.ErrInternal
	}
	return hit, nil
}

func formatVector(values []float32) string {
	if len(values) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteByte('[')
	for index, value := range values {
		if index > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(strconv.FormatFloat(float64(value), 'g', -1, 32))
	}
	builder.WriteByte(']')
	return builder.String()
}

func scanScheduleOccurrence(row rowScanner) (domainrepo.ScheduleOccurrence, error) {
	var occurrence domainrepo.ScheduleOccurrence
	var targetKind string
	var initialBackoffMS, maximumBackoffMS, maximumExecutionMS int64
	err := row.Scan(
		&occurrence.ID,
		&occurrence.ScheduleID,
		&occurrence.OrganizationID,
		&occurrence.ProjectID,
		&occurrence.ScheduledFor,
		&occurrence.TargetResourceID,
		&targetKind,
		&occurrence.TargetVersion,
		&occurrence.EffectiveInputSHA256,
		&occurrence.PromptProfileID,
		&occurrence.PromptRevision,
		&occurrence.RuntimeRevisionID,
		&occurrence.SessionPolicy,
		&occurrence.RoomID,
		&occurrence.NotificationPolicy,
		&maximumExecutionMS,
		&occurrence.Coalesce,
		&occurrence.OverlapPolicy,
		&occurrence.MaximumAttempts,
		&initialBackoffMS,
		&maximumBackoffMS,
		&occurrence.DeadLetterAt,
		&occurrence.State,
		&occurrence.Version,
		&occurrence.Attempt,
		&occurrence.ClaimantWorkloadID,
		&occurrence.AuthorityGeneration,
		&occurrence.TokenHash,
		&occurrence.ClaimKeySHA256,
		&occurrence.LeaseExpiresAt,
		&occurrence.AvailableAt,
		&occurrence.Outcome,
		&occurrence.ResultArtifactID,
		&occurrence.RecoveryEvidenceSHA256,
		&occurrence.RecoveryBlockedAt,
		&occurrence.ExecutionSessionID,
		&occurrence.ExecutionSessionVersion,
		&occurrence.ExecutionTurnID,
		&occurrence.ExecutionTurnVersion,
		&occurrence.ExecutionProcessRunID,
		&occurrence.ExecutionProcessVersion,
		&occurrence.ExecutionRuntimeRevisionID,
		&occurrence.ExecutionRuntimeRevisionVersion,
		&occurrence.CreatedAt,
		&occurrence.UpdatedAt,
	)
	occurrence.TargetKind = enum.Kind(targetKind)
	occurrence.InitialBackoff = time.Duration(initialBackoffMS) * time.Millisecond
	occurrence.MaximumBackoff = time.Duration(maximumBackoffMS) * time.Millisecond
	occurrence.MaximumExecution = time.Duration(maximumExecutionMS) * time.Millisecond
	if err != nil || !occurrence.TargetKind.Valid() ||
		occurrence.ID == "" || occurrence.ScheduleID == "" ||
		occurrence.TargetVersion == 0 || occurrence.Version == 0 ||
		!validDigest(occurrence.EffectiveInputSHA256) ||
		(occurrence.ClaimKeySHA256 != "" && !validDigest(occurrence.ClaimKeySHA256)) ||
		(occurrence.RecoveryEvidenceSHA256 != "" && !validDigest(occurrence.RecoveryEvidenceSHA256)) ||
		value.ValidateID(occurrence.PromptProfileID) != nil ||
		occurrence.PromptRevision == 0 ||
		value.ValidateID(occurrence.RuntimeRevisionID) != nil ||
		(occurrence.ExecutionSessionID != "" &&
			(value.ValidateID(occurrence.ExecutionSessionID) != nil ||
				occurrence.ExecutionSessionVersion == 0 ||
				value.ValidateID(occurrence.ExecutionTurnID) != nil ||
				occurrence.ExecutionTurnVersion == 0 ||
				value.ValidateID(occurrence.ExecutionRuntimeRevisionID) != nil ||
				occurrence.ExecutionRuntimeRevisionVersion == 0)) ||
		(occurrence.ExecutionProcessRunID == "") !=
			(occurrence.ExecutionProcessVersion == 0) ||
		(occurrence.SessionPolicy != "NEW" &&
			occurrence.SessionPolicy != "PERSISTENT" &&
			occurrence.SessionPolicy != "ROLLING") ||
		(occurrence.RoomID != "" && value.ValidateID(occurrence.RoomID) != nil) ||
		(occurrence.NotificationPolicy != "ALWAYS" &&
			occurrence.NotificationPolicy != "ON_ACTION" &&
			occurrence.NotificationPolicy != "ON_FAILURE" &&
			occurrence.NotificationPolicy != "ON_ACTION_OR_FAILURE" &&
			occurrence.NotificationPolicy != "AUDIT_ONLY") ||
		occurrence.MaximumExecution < time.Minute ||
		occurrence.MaximumExecution > 24*time.Hour ||
		(occurrence.OverlapPolicy == "QUEUE" && occurrence.Coalesce) ||
		(occurrence.OverlapPolicy != "QUEUE" && !occurrence.Coalesce) ||
		(occurrence.OverlapPolicy != "FORBID" &&
			occurrence.OverlapPolicy != "SKIP" &&
			occurrence.OverlapPolicy != "QUEUE") ||
		occurrence.MaximumAttempts == 0 ||
		occurrence.InitialBackoff < time.Second ||
		occurrence.MaximumBackoff < occurrence.InitialBackoff ||
		occurrence.DeadLetterAt.IsZero() {
		return domainrepo.ScheduleOccurrence{}, errs.ErrInternal
	}
	return occurrence, nil
}

func scanScheduleOccurrenceCapability(row rowScanner) (domainrepo.ScheduleOccurrenceCapability, error) {
	var capability domainrepo.ScheduleOccurrenceCapability
	err := row.Scan(&capability.ID, &capability.OrganizationID, &capability.ProjectID,
		&capability.OccurrenceID, &capability.Attempt, &capability.ImmutableInputSHA256,
		&capability.AuthorityGeneration, &capability.FullMethod, &capability.WorkloadID,
		&capability.CallerSPIFFEID, &capability.TokenSHA256, &capability.State,
		&capability.IssuedAt, &capability.ExpiresAt, &capability.ConsumedAt, &capability.RevokedAt)
	if err != nil || value.ValidateID(capability.ID) != nil || value.ValidateID(capability.OccurrenceID) != nil ||
		capability.Attempt == 0 || capability.AuthorityGeneration == 0 ||
		!validDigest(capability.ImmutableInputSHA256) || !validDigest(capability.TokenSHA256) {
		if err != nil {
			return domainrepo.ScheduleOccurrenceCapability{}, mapError(err)
		}
		return domainrepo.ScheduleOccurrenceCapability{}, errs.ErrInternal
	}
	return capability, nil
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, symbol := range value {
		if (symbol < '0' || symbol > '9') && (symbol < 'a' || symbol > 'f') {
			return false
		}
	}
	return true
}

func scheduleNextRun(spec entity.Spec) any {
	if schedule, ok := spec.(entity.ScheduleSpec); ok {
		return schedule.NextRunAt
	}
	return nil
}

func (wrapped *transaction) bumpCacheEpoch(
	ctx context.Context,
	includeTenant bool,
) error {
	if _, err := wrapped.bumpOneCacheEpoch(
		ctx,
		wrapped.organizationID,
		wrapped.projectID,
	); err != nil {
		return err
	}
	if includeTenant && wrapped.projectID != "" {
		if _, err := wrapped.bumpOneCacheEpoch(
			ctx,
			wrapped.organizationID,
			"",
		); err != nil {
			return err
		}
	}
	return nil
}

func (wrapped *transaction) bumpOneCacheEpoch(
	ctx context.Context,
	organizationID, projectID string,
) (uint64, error) {
	var epoch uint64
	err := wrapped.tx.QueryRow(
		ctx,
		sqlCacheEpochBump,
		pgx.StrictNamedArgs{
			"organization_id": organizationID,
			"project_id":      projectID,
		},
	).Scan(&epoch)
	return epoch, mapError(err)
}

func (wrapped *transaction) rebuildPermissionIndex(
	ctx context.Context,
	resource entity.Resource,
) error {
	if resource.Kind != enum.KindProject &&
		resource.Kind != enum.KindTeam &&
		resource.Kind != enum.KindRole {
		return nil
	}
	projectID := resource.ProjectID
	if projectID == "" {
		return errs.ErrInternal
	}
	_, err := wrapped.tx.Exec(
		ctx,
		sqlPermissionIndexRebuild,
		pgx.StrictNamedArgs{
			"organization_id": resource.OrganizationID,
			"project_id":      projectID,
		},
	)
	return mapError(err)
}

func retryableTransaction(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) &&
		(postgresError.Code == "40001" || postgresError.Code == "40P01")
}

func mapError(err error) error {
	if err == nil {
		return nil
	}
	for _, domainError := range []error{
		errs.ErrInvalidInput,
		errs.ErrUnauthenticated,
		errs.ErrPermissionDenied,
		errs.ErrNotFound,
		errs.ErrStateConflict,
		errs.ErrIdempotencyConflict,
		errs.ErrVersionMismatch,
		errs.ErrUnavailable,
		errs.ErrInternal,
	} {
		if errors.Is(err, domainError) {
			return err
		}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505", "23503", "23514":
			return errs.ErrStateConflict
		case "40001", "40P01", "53300", "57P01", "57P02", "57P03":
			return errs.ErrUnavailable
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return errs.ErrUnavailable
	}
	return fmt.Errorf("%w: PostgreSQL operation failed", errs.ErrInternal)
}
