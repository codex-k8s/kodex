// Package botidentity реализует RLS-scoped PostgreSQL Agent bot checkpoints.
package botidentity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	domainrepo "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/repository/botidentity"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/enum"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	selectorNamespace = uuid.MustParse("c2769df5-232a-5264-87fd-d4bfa51e4f35")
	cursorNamespace   = uuid.MustParse("bfcb70b9-8e13-56ea-8520-472973822c5f")
	identityNamespace = uuid.MustParse("05687f90-df38-5868-b67b-2b87fa3ee925")
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

type rowScanner interface{ Scan(...any) error }

type namedQueryer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func New(pool *pgxpool.Pool, config Config) (*Repository, error) {
	if pool == nil || config.PrincipalGeneration == 0 || uuid.Validate(config.OrganizationID) != nil ||
		len(config.AllowedProjectIDs) == 0 {
		return nil, errors.New("Agent bot identity repository configuration is invalid")
	}
	for _, projectID := range config.AllowedProjectIDs {
		if uuid.Validate(projectID) != nil {
			return nil, errors.New("Agent bot identity repository project scope is invalid")
		}
	}
	if err := validateQueries(); err != nil {
		return nil, err
	}
	return &Repository{pool: pool, config: config}, nil
}

func (repository *Repository) Check(ctx context.Context) error {
	projects, err := json.Marshal(repository.config.AllowedProjectIDs)
	if err != nil {
		return errors.New("encode Agent bot identity project scope")
	}
	var schemaVersion uint64
	var identityReady bool
	if err := queryRow(ctx, repository.pool, readinessCheckSQL, repository.config.PrincipalGeneration,
		repository.config.OrganizationID, projects).Scan(&schemaVersion, &identityReady); err != nil ||
		schemaVersion != 1 || !identityReady {
		return errors.New("Agent bot identity repository is not ready")
	}
	principal := entity.TeamPrincipal{
		OrganizationID: repository.config.OrganizationID,
		ProjectID:      repository.config.AllowedProjectIDs[0],
		ActorID:        uuid.NewString(),
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable, AccessMode: pgx.ReadWrite})
	if err != nil {
		return errors.New("begin Agent bot identity readiness transaction")
	}
	defer tx.Rollback(ctx)
	if _, err := execNamed(ctx, tx, activateScopeSQL, principal.OrganizationID, principal.ProjectID,
		principal.ActorID); err != nil {
		return errors.New("activate Agent bot identity readiness scope")
	}
	negative, err := tx.Begin(ctx)
	if err != nil {
		return errors.New("begin Agent bot identity negative readiness probe")
	}
	var organizationID, projectID, actorID string
	var offset uint32
	negativeErr := queryRow(ctx, negative, readinessProbeCursorSQL, uuid.NewString(), uuid.NewString(),
		principal.ProjectID, principal.ActorID).Scan(&organizationID, &projectID, &actorID, &offset)
	rollbackErr := negative.Rollback(ctx)
	if negativeErr == nil || (rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed)) {
		return errors.New("cross-tenant Agent bot identity readiness probe was not rejected")
	}
	if err := queryRow(ctx, tx, readinessProbeCursorSQL, uuid.NewString(), principal.OrganizationID,
		principal.ProjectID, principal.ActorID).Scan(&organizationID, &projectID, &actorID, &offset); err != nil ||
		organizationID != principal.OrganizationID || projectID != principal.ProjectID ||
		actorID != principal.ActorID || offset != 1 {
		return errors.New("scoped Agent bot identity readiness DML is not ready")
	}
	return nil
}

func (repository *Repository) ResolveCatalogOffset(ctx context.Context, principal entity.TeamPrincipal,
	cursor string, pageSize uint32,
) (uint32, error) {
	if cursor == "" {
		return 0, nil
	}
	if uuid.Validate(cursor) != nil {
		return 0, domainrepo.ErrNotFound
	}
	var offset uint32
	err := repository.withScope(ctx, principal, pgx.ReadOnly, func(tx pgx.Tx) error {
		return queryRow(ctx, tx, catalogCursorResolveSQL, cursor, principal.OrganizationID,
			principal.ProjectID, principal.ActorID, pageSize).Scan(&offset)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, domainrepo.ErrNotFound
	}
	if err != nil {
		return 0, errors.New("resolve Agent bot identity catalog cursor")
	}
	return offset, nil
}

func (repository *Repository) SaveCatalogPage(ctx context.Context, principal entity.TeamPrincipal,
	identities []entity.AgentMattermostBotIdentity, offset, pageSize uint32, hasMore bool, ttl time.Duration,
) ([]entity.AgentMattermostBotIdentity, string, error) {
	result := make([]entity.AgentMattermostBotIdentity, 0, len(identities))
	nextCursor := ""
	err := repository.withScope(ctx, principal, pgx.ReadWrite, func(tx pgx.Tx) error {
		for _, identity := range identities {
			identity.IdentityRef = uuid.NewSHA1(identityNamespace, []byte(strings.Join([]string{
				principal.OrganizationID, principal.ProjectID, identity.ProviderUserID,
			}, "\x00"))).String()
			identity.ProviderObjectRef = identity.IdentityRef
			stored, err := upsertIdentity(ctx, tx, principal, identity)
			if err != nil {
				return err
			}
			var available bool
			if err := queryRow(ctx, tx, ownershipAvailableSQL, principal.OrganizationID,
				principal.ProjectID, stored.ProviderObjectRef).Scan(&available); err != nil {
				return err
			}
			if !available {
				continue
			}
			selectorID := uuid.NewSHA1(selectorNamespace, []byte(strings.Join([]string{
				principal.OrganizationID, principal.ProjectID, principal.ActorID, stored.IdentityRef,
			}, "\x00"))).String()
			if err := queryRow(ctx, tx, selectorUpsertSQL, selectorID, principal.OrganizationID,
				principal.ProjectID, principal.ActorID, stored.IdentityRef,
				stored.ProviderSnapshotSHA256, interval(ttl)).Scan(&stored.Selector); err != nil {
				return err
			}
			result = append(result, stored)
		}
		if !hasMore {
			return nil
		}
		nextOffset := offset + pageSize
		cursorID := uuid.NewSHA1(cursorNamespace, []byte(fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%d",
			principal.OrganizationID, principal.ProjectID, principal.ActorID, nextOffset, pageSize))).String()
		return queryRow(ctx, tx, catalogCursorUpsertSQL, cursorID, principal.OrganizationID,
			principal.ProjectID, principal.ActorID, nextOffset, pageSize, interval(ttl)).Scan(&nextCursor)
	})
	if err != nil {
		return nil, "", errors.New("save Agent bot identity catalog page")
	}
	return result, nextCursor, nil
}

func (repository *Repository) ReserveProviderObject(ctx context.Context,
	operation entity.AgentMattermostBotOperation, providerObjectRef string,
) error {
	return repository.withScope(ctx, operation.Principal, pgx.ReadWrite, func(tx pgx.Tx) error {
		var storedAgentRef string
		if err := queryRow(ctx, tx, ownershipReserveOperationSQL, operation.ID,
			operation.Principal.OrganizationID, operation.Principal.ProjectID, providerObjectRef,
			operation.AgentRef, operation.Fence, digest(operation.LeaseToken)).Scan(&storedAgentRef); err != nil ||
			storedAgentRef != operation.AgentRef {
			return domainrepo.ErrGenerationConflict
		}
		return nil
	})
}

func (repository *Repository) ResolveSelector(ctx context.Context, principal entity.TeamPrincipal,
	selector string,
) (entity.AgentMattermostBotIdentity, error) {
	var identity entity.AgentMattermostBotIdentity
	err := repository.withScope(ctx, principal, pgx.ReadOnly, func(tx pgx.Tx) error {
		return scanIdentity(queryRow(ctx, tx, selectorResolveSQL, selector, principal.OrganizationID,
			principal.ProjectID, principal.ActorID), &identity)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.AgentMattermostBotIdentity{}, domainrepo.ErrNotFound
	}
	if err != nil {
		return entity.AgentMattermostBotIdentity{}, errors.New("resolve Agent bot identity selector")
	}
	return identity, nil
}

func (repository *Repository) BeginOperation(ctx context.Context, operation entity.AgentMattermostBotOperation,
	owner string, lease, recoveryWindow time.Duration,
) (entity.AgentMattermostBotOperation, domainrepo.Disposition, error) {
	token, err := newLeaseToken()
	if err != nil {
		return entity.AgentMattermostBotOperation{}, 0, err
	}
	disposition := domainrepo.Claimed
	var stored entity.AgentMattermostBotOperation
	err = repository.withScope(ctx, operation.Principal, pgx.ReadWrite, func(tx pgx.Tx) error {
		tag, err := execNamed(ctx, tx, operationInsertSQL, operation.ID, operation.Principal.OrganizationID,
			operation.Principal.ProjectID, operation.Principal.ActorID, operation.Action, operation.IdempotencyKey,
			operation.AgentRef, operation.ExpectedAgentVersion, operation.PredecessorGeneration,
			operation.IdentityRef, operation.Selector, operation.RequestSHA256, operation.Intent.Username,
			operation.Intent.DisplayName, operation.Intent.ProviderCorrelation, string(operation.State), owner,
			digest(token), interval(lease), interval(recoveryWindow))
		if err != nil {
			return err
		}
		var leaseActive bool
		stored, leaseActive, err = scanOperation(queryRow(ctx, tx, operationLockSQL, operation.ID))
		if errors.Is(err, pgx.ErrNoRows) && tag.RowsAffected() == 0 {
			return domainrepo.ErrIdempotencyConflict
		}
		if err != nil {
			return err
		}
		if stored.IdempotencyKey != operation.IdempotencyKey || stored.RequestSHA256 != operation.RequestSHA256 ||
			stored.Action != operation.Action ||
			stored.AgentRef != operation.AgentRef {
			return domainrepo.ErrIdempotencyConflict
		}
		if tag.RowsAffected() == 1 {
			stored.LeaseToken = token
			stored.Identity = operation.Identity
			return nil
		}
		if !leaseActive && !terminalState(stored.State) {
			reclaimed, err := execNamed(ctx, tx, operationReclaimSQL, operation.ID, owner, digest(token), interval(lease))
			if err != nil {
				return err
			}
			if reclaimed.RowsAffected() == 1 {
				stored, _, err = scanOperation(queryRow(ctx, tx, operationLockSQL, operation.ID))
				stored.LeaseToken = token
				return err
			}
		}
		if terminalState(stored.State) {
			disposition = domainrepo.Replay
		} else {
			disposition = domainrepo.Busy
		}
		return nil
	})
	if errors.Is(err, domainrepo.ErrIdempotencyConflict) {
		return entity.AgentMattermostBotOperation{}, 0, err
	}
	if err != nil {
		return entity.AgentMattermostBotOperation{}, 0, errors.New("begin Agent bot identity operation")
	}
	return stored, disposition, nil
}

func (repository *Repository) GetOperation(ctx context.Context, principal entity.TeamPrincipal,
	agentRef, action, idempotencyKey string,
) (entity.AgentMattermostBotOperation, error) {
	var operation entity.AgentMattermostBotOperation
	err := repository.withScope(ctx, principal, pgx.ReadOnly, func(tx pgx.Tx) error {
		var scanErr error
		operation, _, scanErr = scanOperation(queryRow(ctx, tx, operationGetSQL,
			principal.OrganizationID, principal.ProjectID, principal.ActorID, agentRef, action, idempotencyKey))
		return scanErr
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.AgentMattermostBotOperation{}, domainrepo.ErrNotFound
	}
	if err != nil {
		return entity.AgentMattermostBotOperation{}, errors.New("read Agent bot identity operation")
	}
	return operation, nil
}

func (repository *Repository) MarkEffectStarted(ctx context.Context,
	operation entity.AgentMattermostBotOperation,
) (entity.AgentMattermostBotOperation, error) {
	err := repository.withScope(ctx, operation.Principal, pgx.ReadWrite, func(tx pgx.Tx) error {
		return queryRow(ctx, tx, operationMarkEffectSQL, operation.ID, operation.Fence,
			digest(operation.LeaseToken)).Scan(&operation.EffectStartedAt, &operation.UpdatedAt)
	})
	if err != nil {
		return entity.AgentMattermostBotOperation{}, errors.New("mark Agent bot provider effect")
	}
	return operation, nil
}

func (repository *Repository) MarkMembershipPending(ctx context.Context, operation entity.AgentMattermostBotOperation,
	identity entity.AgentMattermostBotIdentity,
) (entity.AgentMattermostBotOperation, error) {
	err := repository.withScope(ctx, operation.Principal, pgx.ReadWrite, func(tx pgx.Tx) error {
		var reservedAgentRef string
		if err := queryRow(ctx, tx, ownershipReserveSQL, operation.Principal.OrganizationID,
			operation.Principal.ProjectID, identity.ProviderObjectRef, operation.AgentRef).Scan(&reservedAgentRef); err != nil ||
			reservedAgentRef != operation.AgentRef {
			return domainrepo.ErrGenerationConflict
		}
		stored, err := upsertIdentity(ctx, tx, operation.Principal, identity)
		if err != nil {
			return err
		}
		tag, err := execNamed(ctx, tx, operationMembershipSQL, operation.ID, stored.IdentityRef,
			operation.Fence, digest(operation.LeaseToken))
		if err != nil || tag.RowsAffected() != 1 {
			return errors.New("membership checkpoint was not updated")
		}
		identity = stored
		return nil
	})
	if err != nil {
		return entity.AgentMattermostBotOperation{}, errors.New("mark Agent bot membership checkpoint")
	}
	operation.State, operation.Identity = enum.AgentBotOperationMembershipPending, identity
	return operation, nil
}

func (repository *Repository) DeferRecovery(ctx context.Context, operation entity.AgentMattermostBotOperation,
	code string, delay time.Duration,
) (entity.AgentMattermostBotOperation, error) {
	var state string
	err := repository.withScope(ctx, operation.Principal, pgx.ReadWrite, func(tx pgx.Tx) error {
		return queryRow(ctx, tx, operationDeferSQL, operation.ID, code, interval(delay), operation.Fence,
			digest(operation.LeaseToken)).Scan(&state, &operation.FailureCode, &operation.RetryNotBefore,
			&operation.RecoveryDeadline, &operation.UpdatedAt)
	})
	if err != nil {
		return entity.AgentMattermostBotOperation{}, errors.New("defer Agent bot identity recovery")
	}
	operation.State, operation.LeaseToken = enum.AgentBotIdentityOperationState(state), ""
	return operation, nil
}

func (repository *Repository) AcceptProvider(ctx context.Context, operation entity.AgentMattermostBotOperation,
	identity entity.AgentMattermostBotIdentity,
) (entity.AgentMattermostBotOperation, error) {
	err := repository.withScope(ctx, operation.Principal, pgx.ReadWrite, func(tx pgx.Tx) error {
		var generation uint64
		if err := queryRow(ctx, tx, watermarkAdvanceSQL, operation.Principal.OrganizationID,
			operation.Principal.ProjectID, operation.AgentRef).Scan(&generation); err != nil || generation == 0 {
			return errors.New("Agent bot identity generation was not advanced")
		}
		identity.ProviderGeneration = generation
		stored, err := upsertIdentity(ctx, tx, operation.Principal, identity)
		if err != nil {
			return err
		}
		tag, err := execNamed(ctx, tx, operationAcceptSQL, operation.ID, stored.IdentityRef,
			operation.Fence, digest(operation.LeaseToken))
		if err != nil || tag.RowsAffected() != 1 {
			return errors.New("provider checkpoint was not accepted")
		}
		identity = stored
		return nil
	})
	if err != nil {
		return entity.AgentMattermostBotOperation{}, errors.New("accept Agent bot provider checkpoint")
	}
	operation.State, operation.Identity = enum.AgentBotOperationProviderAccepted, identity
	return operation, nil
}

func (repository *Repository) Finish(ctx context.Context, operation entity.AgentMattermostBotOperation,
	binding entity.AgentMattermostBotBinding,
) error {
	return repository.withScope(ctx, operation.Principal, pgx.ReadWrite, func(tx pgx.Tx) error {
		state := string(enum.AgentBotOperationBound)
		admitted := true
		if binding.Identity.Status == enum.AgentBotIdentityRevoked {
			state, admitted = string(enum.AgentBotOperationRevoked), false
		}
		tag, err := execNamed(ctx, tx, bindingUpsertSQL, operation.Principal.OrganizationID,
			operation.Principal.ProjectID, binding.AgentRef, operation.Principal.ActorID,
			binding.Identity.AgentStableKey, binding.AgentVersion, binding.Identity.IdentityRef,
			binding.Identity.ProviderGeneration, string(binding.Identity.Status), binding.ReceiptSHA256)
		if err != nil || tag.RowsAffected() != 1 {
			return errors.New("Agent bot binding was not advanced")
		}
		tag, err = execNamed(ctx, tx, watermarkAdmitSQL, operation.Principal.OrganizationID,
			operation.Principal.ProjectID, binding.AgentRef, binding.Identity.ProviderGeneration, admitted)
		if err != nil || tag.RowsAffected() != 1 {
			return errors.New("Agent bot generation admission was not updated")
		}
		tag, err = execNamed(ctx, tx, operationFinishSQL, operation.ID, state, operation.ReceiptID,
			operation.ReceiptRevision, operation.ReceiptSHA256, operation.CommandIntentSHA256,
			binding.AgentVersion, operation.Fence, digest(operation.LeaseToken))
		if err != nil || tag.RowsAffected() != 1 {
			return errors.New("Agent bot operation was not completed")
		}
		return nil
	})
}

func (repository *Repository) MarkRepairRequired(ctx context.Context,
	operation entity.AgentMattermostBotOperation, code string,
) error {
	return repository.withScope(ctx, operation.Principal, pgx.ReadWrite, func(tx pgx.Tx) error {
		tag, err := execNamed(ctx, tx, operationRepairSQL, operation.ID, code,
			operation.Fence, digest(operation.LeaseToken))
		if err == nil && tag.RowsAffected() != 1 {
			return domainrepo.ErrGenerationConflict
		}
		return err
	})
}

func (repository *Repository) ClaimRecovery(ctx context.Context, owner string,
	lease time.Duration,
) (entity.AgentMattermostBotOperation, bool, error) {
	var organizationID, projectID string
	if err := queryRow(ctx, repository.pool, workScopeNextSQL).Scan(&organizationID, &projectID); errors.Is(err, pgx.ErrNoRows) {
		return entity.AgentMattermostBotOperation{}, false, nil
	} else if err != nil {
		return entity.AgentMattermostBotOperation{}, false, errors.New("discover Agent bot identity recovery scope")
	}
	principal := entity.TeamPrincipal{OrganizationID: organizationID, ProjectID: projectID, ActorID: uuid.NewString()}
	token, err := newLeaseToken()
	if err != nil {
		return entity.AgentMattermostBotOperation{}, false, err
	}
	var operation entity.AgentMattermostBotOperation
	err = repository.withScope(ctx, principal, pgx.ReadWrite, func(tx pgx.Tx) error {
		var operationID string
		if err := queryRow(ctx, tx, operationClaimSQL, owner, digest(token), interval(lease)).Scan(&operationID); err != nil {
			return err
		}
		var scanErr error
		operation, _, scanErr = scanOperation(queryRow(ctx, tx, operationLockSQL, operationID))
		operation.LeaseToken = token
		return scanErr
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.AgentMattermostBotOperation{}, false, nil
	}
	if err != nil {
		return entity.AgentMattermostBotOperation{}, false, errors.New("claim Agent bot identity recovery")
	}
	return operation, true, nil
}

func (repository *Repository) GetBinding(ctx context.Context, principal entity.TeamPrincipal,
	agentRef string,
) (entity.AgentMattermostBotBinding, error) {
	var binding entity.AgentMattermostBotBinding
	err := repository.withScope(ctx, principal, pgx.ReadOnly, func(tx pgx.Tx) error {
		return scanBinding(queryRow(ctx, tx, bindingGetSQL, principal.OrganizationID, principal.ProjectID, agentRef), &binding)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.AgentMattermostBotBinding{}, domainrepo.ErrNotFound
	}
	if err != nil {
		return entity.AgentMattermostBotBinding{}, errors.New("read Agent bot identity binding")
	}
	return binding, nil
}

func (repository *Repository) CloseGeneration(ctx context.Context,
	operation entity.AgentMattermostBotOperation, generation uint64,
) error {
	return repository.withScope(ctx, operation.Principal, pgx.ReadWrite, func(tx pgx.Tx) error {
		tag, err := execNamed(ctx, tx, watermarkCloseSQL, operation.Principal.OrganizationID,
			operation.Principal.ProjectID, operation.AgentRef, generation, operation.ID,
			operation.Fence, digest(operation.LeaseToken))
		if err != nil || tag.RowsAffected() != 1 {
			return domainrepo.ErrGenerationConflict
		}
		return nil
	})
}

func (repository *Repository) AdmitRuntimeIdentity(ctx context.Context, principal entity.TeamPrincipal,
	agentStableKey, providerUserID string, generation uint64,
) (entity.AgentMattermostBotIdentity, error) {
	var identity entity.AgentMattermostBotIdentity
	err := repository.withScope(ctx, principal, pgx.ReadOnly, func(tx pgx.Tx) error {
		return scanIdentity(queryRow(ctx, tx, runtimeAdmitSQL, principal.OrganizationID,
			principal.ProjectID, agentStableKey, providerUserID, generation), &identity)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.AgentMattermostBotIdentity{}, domainrepo.ErrNotFound
	}
	if err != nil {
		return entity.AgentMattermostBotIdentity{}, errors.New("admit Agent bot runtime identity")
	}
	return identity, nil
}

func (repository *Repository) ResolveRuntimeIdentity(ctx context.Context, principal entity.TeamPrincipal,
	agentStableKey, providerUserID string,
) (entity.AgentMattermostBotIdentity, error) {
	var identity entity.AgentMattermostBotIdentity
	err := repository.withScope(ctx, principal, pgx.ReadOnly, func(tx pgx.Tx) error {
		return scanIdentity(queryRow(ctx, tx, runtimeResolveSQL, principal.OrganizationID,
			principal.ProjectID, agentStableKey, providerUserID), &identity)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.AgentMattermostBotIdentity{}, domainrepo.ErrNotFound
	}
	if err != nil {
		return entity.AgentMattermostBotIdentity{}, errors.New("resolve current Agent bot runtime identity")
	}
	return identity, nil
}

func upsertIdentity(ctx context.Context, tx pgx.Tx, principal entity.TeamPrincipal,
	identity entity.AgentMattermostBotIdentity,
) (entity.AgentMattermostBotIdentity, error) {
	createdAt := identity.CreatedAt
	if createdAt.IsZero() {
		createdAt = identity.ObservedAt
	}
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	var stored entity.AgentMattermostBotIdentity
	err := scanIdentity(queryRow(ctx, tx, identityUpsertSQL, identity.IdentityRef,
		identity.ProviderObjectRef, principal.OrganizationID, principal.ProjectID, identity.AgentRef, identity.AgentStableKey,
		identity.ProviderBotID, identity.ProviderUserID, identity.ProviderTeamID, identity.ProviderTokenID,
		identity.CredentialBindingID, identity.CredentialSecretRef, identity.CredentialSecretVersion,
		identity.CredentialSHA256, identity.Username, identity.DisplayName, string(identity.Status),
		identity.ProviderVersion, identity.ProviderGeneration, identity.ProviderSnapshotSHA256,
		identity.ProviderCausalitySHA256, identity.ObservedAt, createdAt), &stored)
	if err != nil {
		return entity.AgentMattermostBotIdentity{}, err
	}
	stored.Selector = identity.Selector
	return stored, nil
}

func scanIdentity(row rowScanner, identity *entity.AgentMattermostBotIdentity) error {
	var status string
	if err := row.Scan(&identity.IdentityRef, &identity.Selector, &identity.ProviderObjectRef,
		&identity.AgentRef, &identity.AgentStableKey,
		&identity.ProviderBotID, &identity.ProviderUserID, &identity.ProviderTeamID, &identity.ProviderTokenID,
		&identity.CredentialBindingID, &identity.CredentialSecretRef, &identity.CredentialSecretVersion,
		&identity.CredentialSHA256, &identity.Username, &identity.DisplayName, &status,
		&identity.ProviderVersion, &identity.ProviderGeneration, &identity.ProviderSnapshotSHA256,
		&identity.ProviderCausalitySHA256, &identity.ObservedAt, &identity.CreatedAt, &identity.UpdatedAt); err != nil {
		return err
	}
	identity.Status = enum.AgentBotIdentityStatus(status)
	return nil
}

func scanOperation(row rowScanner) (entity.AgentMattermostBotOperation, bool, error) {
	var operation entity.AgentMattermostBotOperation
	var state, identityStatus string
	var correlation string
	var leaseActive bool
	identity := &operation.Identity
	err := row.Scan(&operation.ID, &operation.Principal.OrganizationID, &operation.Principal.ProjectID,
		&operation.Principal.ActorID, &operation.Action, &operation.IdempotencyKey, &operation.AgentRef,
		&operation.ExpectedAgentVersion, &operation.PredecessorGeneration, &operation.IdentityRef,
		&operation.Selector, &operation.RequestSHA256, &operation.Intent.Username, &operation.Intent.DisplayName,
		&correlation, &state, &operation.ReceiptID, &operation.ReceiptRevision, &operation.ReceiptSHA256,
		&operation.CommandIntentSHA256, &operation.Result.AgentVersion, &operation.FailureCode, &operation.Fence,
		&operation.EffectStartedAt, &operation.RetryNotBefore, &operation.RecoveryDeadline,
		&operation.CreatedAt, &operation.UpdatedAt,
		&identity.IdentityRef, &identity.ProviderObjectRef, &identity.AgentRef, &identity.AgentStableKey, &identity.ProviderBotID,
		&identity.ProviderUserID, &identity.ProviderTeamID, &identity.ProviderTokenID,
		&identity.CredentialBindingID, &identity.CredentialSecretRef, &identity.CredentialSecretVersion,
		&identity.CredentialSHA256, &identity.Username, &identity.DisplayName, &identityStatus,
		&identity.ProviderVersion, &identity.ProviderGeneration, &identity.ProviderSnapshotSHA256,
		&identity.ProviderCausalitySHA256, &identity.ObservedAt, &identity.CreatedAt, &identity.UpdatedAt,
		&operation.Result.AgentVersion, &operation.Result.ReceiptSHA256, &operation.Result.UpdatedAt, &leaseActive)
	if err != nil {
		return entity.AgentMattermostBotOperation{}, false, err
	}
	operation.State = enum.AgentBotIdentityOperationState(state)
	identity.Status = enum.AgentBotIdentityStatus(identityStatus)
	identity.Selector = operation.Selector
	operation.Intent = entity.AgentMattermostBotCreateIntent{AgentRef: operation.AgentRef,
		ExpectedAgentVersion: operation.ExpectedAgentVersion, Username: operation.Intent.Username,
		DisplayName: operation.Intent.DisplayName, ProviderCorrelation: correlation,
		RequestSHA256: operation.RequestSHA256}
	operation.Result.AgentRef, operation.Result.Identity = operation.AgentRef, *identity
	return operation, leaseActive, nil
}

func scanBinding(row rowScanner, binding *entity.AgentMattermostBotBinding) error {
	var status string
	if err := row.Scan(&binding.AgentRef, &binding.Identity.AgentStableKey, &binding.AgentVersion,
		&binding.ReceiptSHA256, &binding.UpdatedAt,
		&binding.Identity.IdentityRef, &binding.Identity.Selector, &binding.Identity.ProviderObjectRef,
		&binding.Identity.AgentRef,
		&binding.Identity.AgentStableKey, &binding.Identity.ProviderBotID,
		&binding.Identity.ProviderUserID, &binding.Identity.ProviderTeamID,
		&binding.Identity.ProviderTokenID, &binding.Identity.CredentialBindingID,
		&binding.Identity.CredentialSecretRef, &binding.Identity.CredentialSecretVersion,
		&binding.Identity.CredentialSHA256, &binding.Identity.Username, &binding.Identity.DisplayName,
		&status, &binding.Identity.ProviderVersion,
		&binding.Identity.ProviderGeneration, &binding.Identity.ProviderSnapshotSHA256,
		&binding.Identity.ProviderCausalitySHA256, &binding.Identity.ObservedAt,
		&binding.Identity.CreatedAt, &binding.Identity.UpdatedAt); err != nil {
		return err
	}
	binding.Identity.Status = enum.AgentBotIdentityStatus(status)
	return nil
}

func (repository *Repository) withScope(ctx context.Context, principal entity.TeamPrincipal,
	access pgx.TxAccessMode, run func(pgx.Tx) error,
) error {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable, AccessMode: access})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := execNamed(ctx, tx, activateScopeSQL, principal.OrganizationID, principal.ProjectID,
		principal.ActorID); err != nil {
		return err
	}
	if err := run(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
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

func queryRow(ctx context.Context, queryer namedQueryer, query string,
	arguments ...any,
) pgx.Row {
	return queryer.QueryRow(ctx, query, strictNamed(arguments...))
}

func newLeaseToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", errors.New("generate Agent bot identity lease token")
	}
	return hex.EncodeToString(raw), nil
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func interval(value time.Duration) string {
	return fmt.Sprintf("%d microseconds", value.Microseconds())
}

func terminalState(state enum.AgentBotIdentityOperationState) bool {
	return state == enum.AgentBotOperationBound || state == enum.AgentBotOperationRevoked ||
		state == enum.AgentBotOperationRepairRequired
}
