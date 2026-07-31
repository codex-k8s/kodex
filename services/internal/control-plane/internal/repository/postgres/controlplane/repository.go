// Package controlplane реализует адаптер PostgreSQL для доменного порта репозитория.
package controlplane

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/eventing"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/event"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	domainquery "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
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

var _ domainrepo.Repository = (*Repository)(nil)
var _ domainrepo.Transaction = (*transaction)(nil)

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
		nullActorID,
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
		nullActorID,
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
	if version != 20260731000400 || !member || !nonSuperuser || !noBypassRLS ||
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

func (wrapped *transaction) ExpiredClaimedTurns(
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
			"attempt":                            occurrence.Attempt,
			"available_at":                       occurrence.AvailableAt,
			"outcome":                            occurrence.Outcome,
			"result_artifact_id":                 occurrence.ResultArtifactID,
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
			"organization_id": organizationID,
			"project_id":      projectID,
			"schedule_id":     scheduleID,
		},
	).Scan(&found)
	return found, mapError(err)
}

func (wrapped *transaction) SkipOverlappedScheduleOccurrences(
	ctx context.Context,
	organizationID, projectID string,
	now time.Time,
) ([]domainrepo.ScheduleOccurrence, error) {
	rows, err := wrapped.tx.Query(
		ctx,
		sqlScheduleOccurrenceSkipOverlap,
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

func (wrapped *transaction) RecoverExpiredScheduleOccurrences(
	ctx context.Context,
	organizationID, projectID string,
	now time.Time,
) ([]domainrepo.ScheduleOccurrence, error) {
	rows, err := wrapped.tx.Query(
		ctx,
		sqlScheduleOccurrenceRecoverExpired,
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
			"organization_id": organizationID,
			"project_id":      projectID,
			"now":             now,
		},
	))
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
		pgx.StrictNamedArgs{
			"id":                                 occurrence.ID,
			"state":                              occurrence.State,
			"attempt":                            occurrence.Attempt,
			"claimant_workload_id":               occurrence.ClaimantWorkloadID,
			"authority_generation":               occurrence.AuthorityGeneration,
			"token_hash":                         occurrence.TokenHash,
			"lease_expires_at":                   occurrence.LeaseExpiresAt,
			"available_at":                       occurrence.AvailableAt,
			"outcome":                            occurrence.Outcome,
			"result_artifact_id":                 occurrence.ResultArtifactID,
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

func (wrapped *transaction) SaveScheduledRun(
	ctx context.Context,
	run domainrepo.ScheduledRun,
) error {
	tag, err := wrapped.tx.Exec(ctx, sqlScheduledRunSave, pgx.StrictNamedArgs{
		"occurrence_id": run.OccurrenceID, "attempt": run.Attempt,
		"session_id": run.SessionID, "session_version": run.SessionVersion,
		"turn_id": run.TurnID, "turn_version": run.TurnVersion,
		"process_run_id": run.ProcessRunID, "process_version": run.ProcessVersion,
		"runtime_revision_id":      run.RuntimeRevisionID,
		"runtime_revision_version": run.RuntimeRevisionVersion,
		"effective_input_sha256":   run.EffectiveInputSHA256,
		"state":                    run.State, "created_at": run.CreatedAt,
	})
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

type rowScanner interface {
	Scan(...any) error
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
		&occurrence.Attempt,
		&occurrence.ClaimantWorkloadID,
		&occurrence.AuthorityGeneration,
		&occurrence.TokenHash,
		&occurrence.LeaseExpiresAt,
		&occurrence.AvailableAt,
		&occurrence.Outcome,
		&occurrence.ResultArtifactID,
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
		occurrence.TargetVersion == 0 ||
		!validDigest(occurrence.EffectiveInputSHA256) ||
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
