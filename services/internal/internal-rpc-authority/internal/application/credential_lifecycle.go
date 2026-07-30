package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/model"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
)

var lifecycleUUIDPattern = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
)

type DatabaseCredentialLifecycle struct {
	holderID      string
	leaseDuration time.Duration
	registered    model.DatabaseCredentialRegisteredSet
	store         repository.CredentialLifecycleStore
	vault         repository.VaultStaticRoleReader
}

type DatabaseCredentialReconcileResult struct {
	ReceiptID       string
	CanonicalDigest string
	Generations     []model.DatabaseCredentialGeneration
}

func NewDatabaseCredentialLifecycle(
	holderID string,
	leaseDuration time.Duration,
	registered model.DatabaseCredentialRegisteredSet,
	store repository.CredentialLifecycleStore,
	vault repository.VaultStaticRoleReader,
) (*DatabaseCredentialLifecycle, error) {
	if !lifecycleUUIDPattern.MatchString(holderID) ||
		leaseDuration < 5*time.Second ||
		leaseDuration > time.Minute ||
		registered.Version != model.ContractVersion ||
		registered.SourceRevision == 0 ||
		len(registered.SourceDigest) != 64 ||
		len(registered.Generations) != 4 ||
		store == nil ||
		vault == nil {
		return nil, errors.New("invalid database credential lifecycle configuration")
	}
	return &DatabaseCredentialLifecycle{
		holderID:      holderID,
		leaseDuration: leaseDuration,
		registered:    registered,
		store:         store,
		vault:         vault,
	}, nil
}

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
	roles := make(
		[]repository.VaultStaticRoleExpectation,
		0,
		len(lifecycle.registered.Generations),
	)
	for _, generation := range lifecycle.registered.Generations {
		roles = append(roles, repository.VaultStaticRoleExpectation{
			Role:      generation.VaultStaticRole,
			Principal: generation.Principal,
		})
	}
	if err := lifecycle.vault.VerifyStaticRoles(ctx, roles); err != nil {
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
	return DatabaseCredentialReconcileResult{
		ReceiptID:       idempotencyKey,
		CanonicalDigest: canonicalDigest,
		Generations:     generations,
	}, nil
}

func (lifecycle *DatabaseCredentialLifecycle) Ready(ctx context.Context) (
	[]model.DatabaseCredentialGeneration,
	error,
) {
	roles := make(
		[]repository.VaultStaticRoleExpectation,
		0,
		len(lifecycle.registered.Generations),
	)
	for _, generation := range lifecycle.registered.Generations {
		roles = append(roles, repository.VaultStaticRoleExpectation{
			Role:      generation.VaultStaticRole,
			Principal: generation.Principal,
		})
	}
	if err := lifecycle.vault.VerifyStaticRoles(ctx, roles); err != nil {
		return nil, err
	}
	return lifecycle.store.ReadCredentialGenerations(ctx, lifecycle.registered)
}

func registeredSetDigest(registered model.DatabaseCredentialRegisteredSet) (string, error) {
	encoded, err := json.Marshal(registered)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func (lifecycle *DatabaseCredentialLifecycle) RegisteredSet() model.DatabaseCredentialRegisteredSet {
	return lifecycle.registered
}
