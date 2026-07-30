package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
)

var lifecycleUUIDPattern = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
)

const vaultDatabaseName = "internal-rpc-authority"

// DatabaseCredentialLifecycle координирует поколения PostgreSQL и Vault.
type DatabaseCredentialLifecycle struct {
	holderID      string
	leaseDuration time.Duration
	baseline      model.DatabaseCredentialRegisteredSet
	registered    model.DatabaseCredentialRegisteredSet
	store         repository.CredentialLifecycleStore
	vault         repository.VaultStaticRoleManager
	rollout       repository.CredentialRollout
}

// DatabaseCredentialReconcileResult содержит устойчивый результат сверки.
type DatabaseCredentialReconcileResult struct {
	ReceiptID       string
	CanonicalDigest string
	Generations     []model.DatabaseCredentialGeneration
}

// NewDatabaseCredentialLifecycle проверяет реестр и создаёт вариант использования.
func NewDatabaseCredentialLifecycle(
	holderID string,
	leaseDuration time.Duration,
	baseline model.DatabaseCredentialRegisteredSet,
	registered model.DatabaseCredentialRegisteredSet,
	store repository.CredentialLifecycleStore,
	vault repository.VaultStaticRoleManager,
	rollout repository.CredentialRollout,
) (*DatabaseCredentialLifecycle, error) {
	if !lifecycleUUIDPattern.MatchString(holderID) ||
		leaseDuration < 5*time.Second ||
		leaseDuration > time.Minute ||
		registered.Version != model.ContractVersion ||
		registered.SourceRevision == 0 ||
		len(registered.SourceDigest) != 64 ||
		len(registered.Generations) < 6 ||
		len(registered.Generations) > 16 ||
		baseline.Version != registered.Version ||
		baseline.SourceRevision != registered.SourceRevision ||
		baseline.SourceDigest != registered.SourceDigest ||
		store == nil ||
		vault == nil ||
		rollout == nil {
		return nil, errors.New("invalid database credential lifecycle configuration")
	}
	if err := validateRegisteredLifecycle(registered); err != nil {
		return nil, err
	}
	if err := validateRegisteredLifecycle(baseline); err != nil {
		return nil, err
	}
	return &DatabaseCredentialLifecycle{
		holderID:      holderID,
		leaseDuration: leaseDuration,
		baseline:      baseline,
		registered:    registered,
		store:         store,
		vault:         vault,
		rollout:       rollout,
	}, nil
}

// Reconcile выполняет fenced переход поколений и действия Vault.
func (lifecycle *DatabaseCredentialLifecycle) Reconcile(
	ctx context.Context,
	idempotencyKey string,
) (DatabaseCredentialReconcileResult, error) {
	if !lifecycleUUIDPattern.MatchString(idempotencyKey) {
		return DatabaseCredentialReconcileResult{}, errors.New("invalid database credential idempotency key")
	}
	canonicalDigest, err := registeredSetDigest(lifecycle.registered)
	if err != nil {
		return DatabaseCredentialReconcileResult{}, err
	}
	activeRoles := filterVaultRoles(
		lifecycle.registered,
		model.DatabaseCredentialCurrent,
		model.DatabaseCredentialNext,
		model.DatabaseCredentialPrevious,
	)
	if err := lifecycle.vault.VerifyStaticRoles(ctx, activeRoles); err != nil {
		return DatabaseCredentialReconcileResult{}, err
	}
	fencingToken, err := lifecycle.store.AcquireLease(
		ctx,
		lifecycle.holderID,
		lifecycle.leaseDuration,
	)
	if err != nil {
		return DatabaseCredentialReconcileResult{}, err
	}
	intent, err := lifecycle.store.LoadOrCreateRotationIntent(
		ctx,
		lifecycle.holderID,
		fencingToken,
		idempotencyKey,
		canonicalDigest,
	)
	if err != nil {
		return DatabaseCredentialReconcileResult{}, err
	}
	observedRoles := filterVaultRoles(
		lifecycle.registered,
		model.DatabaseCredentialCurrent,
		model.DatabaseCredentialNext,
	)
	rotationRoles := rotationRoles(lifecycle.baseline, lifecycle.registered)
	switch intent.Phase {
	case model.DatabaseCredentialRotationCreated:
		if _, err := lifecycle.store.ReconcileCredentials(
			ctx,
			lifecycle.holderID,
			fencingToken,
			idempotencyKey,
			canonicalDigest,
			lifecycle.baseline,
		); err != nil {
			return DatabaseCredentialReconcileResult{}, err
		}
		digests, err := lifecycle.vault.ReadStaticCredentialDigests(ctx, observedRoles)
		if err != nil {
			return DatabaseCredentialReconcileResult{}, err
		}
		intent, err = lifecycle.store.AdvanceRotationIntent(
			ctx,
			lifecycle.holderID,
			fencingToken,
			idempotencyKey,
			canonicalDigest,
			model.DatabaseCredentialRotationCreated,
			model.DatabaseCredentialRotationPreRotated,
			digests,
			nil,
		)
		if err != nil {
			return DatabaseCredentialReconcileResult{}, err
		}
		fallthrough
	case model.DatabaseCredentialRotationPreRotated:
		digests, err := lifecycle.vault.ReadStaticCredentialDigests(ctx, observedRoles)
		if err != nil {
			return DatabaseCredentialReconcileResult{}, err
		}
		pendingRoles := unchangedRotationRoles(
			digests,
			intent.PreRotationDigests,
			rotationRoles,
		)
		if len(pendingRoles) > 0 {
			if err := lifecycle.vault.RotateStaticRoles(ctx, pendingRoles); err != nil {
				return DatabaseCredentialReconcileResult{}, err
			}
			digests, err = lifecycle.vault.ReadStaticCredentialDigests(ctx, observedRoles)
			if err != nil {
				return DatabaseCredentialReconcileResult{}, err
			}
		}
		if !completeChangedDigestSet(
			intent.PreRotationDigests,
			digests,
			rotationRoles,
		) {
			return DatabaseCredentialReconcileResult{}, errors.New(
				"staged database credentials were not rotated exactly once",
			)
		}
		intent, err = lifecycle.store.AdvanceRotationIntent(
			ctx,
			lifecycle.holderID,
			fencingToken,
			idempotencyKey,
			canonicalDigest,
			model.DatabaseCredentialRotationPreRotated,
			model.DatabaseCredentialRotationStaged,
			intent.PreRotationDigests,
			digests,
		)
		if err != nil {
			return DatabaseCredentialReconcileResult{}, err
		}
		fallthrough
	case model.DatabaseCredentialRotationStaged:
		if err := lifecycle.rollout.RolloutNext(
			ctx,
			idempotencyKey,
			canonicalDigest,
		); err != nil {
			return DatabaseCredentialReconcileResult{}, err
		}
		readbacks, err := lifecycle.store.ReadSessionReadbacks(ctx)
		if err != nil {
			return DatabaseCredentialReconcileResult{}, err
		}
		if !sessionReadbacksMatch(
			readbacks,
			lifecycle.baseline,
			intent.StagedDigests,
			model.DatabaseCredentialNext,
		) {
			return DatabaseCredentialReconcileResult{}, errors.New(
				"NEXT database credential consumer readback is incomplete",
			)
		}
		intent, err = lifecycle.store.AdvanceRotationIntent(
			ctx,
			lifecycle.holderID,
			fencingToken,
			idempotencyKey,
			canonicalDigest,
			model.DatabaseCredentialRotationStaged,
			model.DatabaseCredentialRotationReadBack,
			intent.PreRotationDigests,
			intent.StagedDigests,
		)
		if err != nil {
			return DatabaseCredentialReconcileResult{}, err
		}
		fallthrough
	case model.DatabaseCredentialRotationReadBack:
		if _, err := lifecycle.store.ReconcileCredentials(
			ctx,
			lifecycle.holderID,
			fencingToken,
			idempotencyKey,
			canonicalDigest,
			lifecycle.registered,
		); err != nil {
			return DatabaseCredentialReconcileResult{}, err
		}
		intent, err = lifecycle.store.AdvanceRotationIntent(
			ctx,
			lifecycle.holderID,
			fencingToken,
			idempotencyKey,
			canonicalDigest,
			model.DatabaseCredentialRotationReadBack,
			model.DatabaseCredentialRotationPromoted,
			intent.PreRotationDigests,
			intent.StagedDigests,
		)
		if err != nil {
			return DatabaseCredentialReconcileResult{}, err
		}
		fallthrough
	case model.DatabaseCredentialRotationPromoted:
		if err := lifecycle.rollout.RolloutCurrent(
			ctx,
			idempotencyKey,
			canonicalDigest,
		); err != nil {
			return DatabaseCredentialReconcileResult{}, err
		}
		intent, err = lifecycle.store.AdvanceRotationIntent(
			ctx,
			lifecycle.holderID,
			fencingToken,
			idempotencyKey,
			canonicalDigest,
			model.DatabaseCredentialRotationPromoted,
			model.DatabaseCredentialRotationRolledOut,
			intent.PreRotationDigests,
			intent.StagedDigests,
		)
		if err != nil {
			return DatabaseCredentialReconcileResult{}, err
		}
		fallthrough
	case model.DatabaseCredentialRotationRolledOut:
		readbacks, err := lifecycle.store.ReadSessionReadbacks(ctx)
		if err != nil {
			return DatabaseCredentialReconcileResult{}, err
		}
		if !sessionReadbacksMatch(
			readbacks,
			lifecycle.registered,
			intent.StagedDigests,
			model.DatabaseCredentialCurrent,
			model.DatabaseCredentialNext,
		) {
			return DatabaseCredentialReconcileResult{}, errors.New(
				"CURRENT database credential consumer readback is incomplete",
			)
		}
		retiredRoles := filterVaultRoles(
			lifecycle.registered,
			model.DatabaseCredentialRetired,
		)
		if err := lifecycle.vault.RevokeStaticRoles(ctx, retiredRoles); err != nil {
			return DatabaseCredentialReconcileResult{}, err
		}
		if err := lifecycle.vault.VerifyRevokedStaticRoles(
			ctx,
			retiredRoles,
		); err != nil {
			return DatabaseCredentialReconcileResult{}, err
		}
		intent, err = lifecycle.store.AdvanceRotationIntent(
			ctx,
			lifecycle.holderID,
			fencingToken,
			idempotencyKey,
			canonicalDigest,
			model.DatabaseCredentialRotationRolledOut,
			model.DatabaseCredentialRotationCompleted,
			intent.PreRotationDigests,
			intent.StagedDigests,
		)
		if err != nil {
			return DatabaseCredentialReconcileResult{}, err
		}
	case model.DatabaseCredentialRotationCompleted:
	default:
		return DatabaseCredentialReconcileResult{}, errors.New(
			"database credential rotation phase is invalid",
		)
	}
	generations, err := lifecycle.store.ReadCredentialGenerations(
		ctx,
		lifecycle.registered,
	)
	if err != nil {
		return DatabaseCredentialReconcileResult{}, err
	}
	return DatabaseCredentialReconcileResult{
		ReceiptID:       idempotencyKey,
		CanonicalDigest: canonicalDigest,
		Generations:     generations,
	}, nil
}

func rotationRoles(
	baseline model.DatabaseCredentialRegisteredSet,
	target model.DatabaseCredentialRegisteredSet,
) []repository.VaultStaticRoleExpectation {
	roles := make([]repository.VaultStaticRoleExpectation, 0, 2)
	for _, current := range baseline.Generations {
		if current.Status != model.DatabaseCredentialNext {
			continue
		}
		for _, promoted := range target.Generations {
			if promoted.Capability != current.Capability ||
				promoted.Generation != current.Generation ||
				promoted.Status != model.DatabaseCredentialCurrent ||
				promoted.Principal != current.Principal ||
				promoted.VaultStaticRole != current.VaultStaticRole {
				continue
			}
			roles = append(roles, repository.VaultStaticRoleExpectation{
				Role: current.VaultStaticRole, Principal: current.Principal,
				DatabaseName: vaultDatabaseName,
			})
		}
	}
	return roles
}

func unchangedRotationRoles(
	current map[string]string,
	before map[string]string,
	roles []repository.VaultStaticRoleExpectation,
) []repository.VaultStaticRoleExpectation {
	result := make([]repository.VaultStaticRoleExpectation, 0, len(roles))
	for _, role := range roles {
		if current[role.Role] == before[role.Role] {
			result = append(result, role)
		}
	}
	return result
}

func completeChangedDigestSet(
	before map[string]string,
	after map[string]string,
	roles []repository.VaultStaticRoleExpectation,
) bool {
	if len(before) == 0 || len(before) != len(after) || len(roles) == 0 {
		return false
	}
	for _, role := range roles {
		if before[role.Role] == "" ||
			after[role.Role] == "" ||
			before[role.Role] == after[role.Role] {
			return false
		}
	}
	return true
}

func sessionReadbacksMatch(
	readbacks []model.DatabaseCredentialSessionReadback,
	registered model.DatabaseCredentialRegisteredSet,
	digests map[string]string,
	statuses ...model.DatabaseCredentialStatus,
) bool {
	expected := make(map[string]model.DatabaseCredentialGeneration)
	for _, generation := range registered.Generations {
		for _, status := range statuses {
			if generation.Status == status {
				expected[string(generation.Capability)+":"+string(status)] = generation
			}
		}
	}
	for _, readback := range readbacks {
		key := string(readback.Capability) + ":" + string(readback.Status)
		generation, ok := expected[key]
		if !ok ||
			readback.Generation != generation.Generation ||
			readback.Principal != generation.Principal ||
			readback.CredentialDigestSHA256 != digests[generation.VaultStaticRole] {
			continue
		}
		delete(expected, key)
	}
	return len(expected) == 0
}

// Ready сверяет роли Vault и фактически сохранённые поколения.
func (lifecycle *DatabaseCredentialLifecycle) Ready(ctx context.Context) (
	[]model.DatabaseCredentialGeneration,
	error,
) {
	roles := filterVaultRoles(
		lifecycle.registered,
		model.DatabaseCredentialCurrent,
		model.DatabaseCredentialNext,
		model.DatabaseCredentialPrevious,
	)
	if err := lifecycle.vault.VerifyStaticRoles(ctx, roles); err != nil {
		return nil, err
	}
	retired := filterVaultRoles(
		lifecycle.registered,
		model.DatabaseCredentialRetired,
	)
	if err := lifecycle.vault.VerifyRevokedStaticRoles(ctx, retired); err != nil {
		return nil, err
	}
	return lifecycle.store.ReadCredentialGenerations(ctx, lifecycle.registered)
}

func filterVaultRoles(
	registered model.DatabaseCredentialRegisteredSet,
	statuses ...model.DatabaseCredentialStatus,
) []repository.VaultStaticRoleExpectation {
	allowed := make(map[model.DatabaseCredentialStatus]struct{}, len(statuses))
	for _, status := range statuses {
		allowed[status] = struct{}{}
	}
	roles := make([]repository.VaultStaticRoleExpectation, 0, len(registered.Generations))
	for _, generation := range registered.Generations {
		if _, ok := allowed[generation.Status]; !ok {
			continue
		}
		roles = append(roles, repository.VaultStaticRoleExpectation{
			Role:         generation.VaultStaticRole,
			Principal:    generation.Principal,
			DatabaseName: vaultDatabaseName,
		})
	}
	return roles
}

func validateRegisteredLifecycle(
	registered model.DatabaseCredentialRegisteredSet,
) error {
	type counts struct {
		current  int
		next     int
		previous int
	}
	byCapability := make(map[model.DatabaseCredentialCapability]*counts)
	seenTuple := make(map[string]struct{}, len(registered.Generations))
	for _, generation := range registered.Generations {
		if generation.SourceRevision != registered.SourceRevision ||
			generation.SourceDigest != registered.SourceDigest ||
			generation.Generation == 0 ||
			generation.Principal == "" ||
			generation.VaultStaticRole == "" {
			return errors.New("database credential generation registry binding is invalid")
		}
		key := string(generation.Capability) + "\x00" +
			generation.Principal + "\x00" +
			generation.VaultStaticRole
		if _, duplicate := seenTuple[key]; duplicate {
			return errors.New("duplicate database credential generation")
		}
		seenTuple[key] = struct{}{}
		count := byCapability[generation.Capability]
		if count == nil {
			count = &counts{}
			byCapability[generation.Capability] = count
		}
		switch generation.Status {
		case model.DatabaseCredentialCurrent:
			count.current++
		case model.DatabaseCredentialNext:
			count.next++
		case model.DatabaseCredentialPrevious:
			count.previous++
		case model.DatabaseCredentialRetired:
		default:
			return errors.New("database credential lifecycle status is invalid")
		}
	}
	if len(byCapability) != 2 {
		return errors.New("database credential capabilities are incomplete")
	}
	for _, count := range byCapability {
		if count.current != 1 || count.next != 1 || count.previous > 1 {
			return errors.New("database credential overlap is outside the bounded lifecycle")
		}
	}
	return nil
}

func registeredSetDigest(registered model.DatabaseCredentialRegisteredSet) (string, error) {
	encoded, err := json.Marshal(registered)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// RegisteredSet возвращает неизменяемый зарегистрированный набор.
func (lifecycle *DatabaseCredentialLifecycle) RegisteredSet() model.DatabaseCredentialRegisteredSet {
	return lifecycle.registered
}
