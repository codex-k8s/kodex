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
	registered    model.DatabaseCredentialRegisteredSet
	store         repository.CredentialLifecycleStore
	vault         repository.VaultStaticRoleManager
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
	registered model.DatabaseCredentialRegisteredSet,
	store repository.CredentialLifecycleStore,
	vault repository.VaultStaticRoleManager,
) (*DatabaseCredentialLifecycle, error) {
	if !lifecycleUUIDPattern.MatchString(holderID) ||
		leaseDuration < 5*time.Second ||
		leaseDuration > time.Minute ||
		registered.Version != model.ContractVersion ||
		registered.SourceRevision == 0 ||
		len(registered.SourceDigest) != 64 ||
		len(registered.Generations) < 6 ||
		len(registered.Generations) > 16 ||
		store == nil ||
		vault == nil {
		return nil, errors.New("invalid database credential lifecycle configuration")
	}
	if err := validateRegisteredLifecycle(registered); err != nil {
		return nil, err
	}
	return &DatabaseCredentialLifecycle{
		holderID:      holderID,
		leaseDuration: leaseDuration,
		registered:    registered,
		store:         store,
		vault:         vault,
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
	activeRoles := make(
		[]repository.VaultStaticRoleExpectation,
		0,
		len(lifecycle.registered.Generations),
	)
	for _, generation := range lifecycle.registered.Generations {
		if generation.Status == model.DatabaseCredentialRetired {
			continue
		}
		activeRoles = append(activeRoles, repository.VaultStaticRoleExpectation{
			Role:         generation.VaultStaticRole,
			Principal:    generation.Principal,
			DatabaseName: vaultDatabaseName,
		})
	}
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
	rotateRoles := filterVaultRoles(
		lifecycle.registered,
		model.DatabaseCredentialCurrent,
		model.DatabaseCredentialNext,
	)
	if err := lifecycle.vault.RotateStaticRoles(ctx, rotateRoles); err != nil {
		return DatabaseCredentialReconcileResult{}, err
	}
	generations, err := lifecycle.store.ReconcileCredentials(
		ctx,
		lifecycle.holderID,
		fencingToken,
		idempotencyKey,
		canonicalDigest,
		lifecycle.registered,
	)
	if err != nil {
		return DatabaseCredentialReconcileResult{}, err
	}
	retiredRoles := filterVaultRoles(
		lifecycle.registered,
		model.DatabaseCredentialRetired,
	)
	if err := lifecycle.vault.RevokeStaticRoles(ctx, retiredRoles); err != nil {
		return DatabaseCredentialReconcileResult{}, err
	}
	return DatabaseCredentialReconcileResult{
		ReceiptID:       idempotencyKey,
		CanonicalDigest: canonicalDigest,
		Generations:     generations,
	}, nil
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
