package repository

import (
	"context"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
)

// CredentialLifecycleStore сохраняет поколения и координирует fenced lease.
type CredentialLifecycleStore interface {
	AcquireLease(
		ctx context.Context,
		holderID string,
		leaseDuration time.Duration,
	) (uint64, error)
	ReconcileCredentials(
		ctx context.Context,
		holderID string,
		fencingToken uint64,
		requestID string,
		canonicalDigest string,
		registered model.DatabaseCredentialRegisteredSet,
	) ([]model.DatabaseCredentialGeneration, error)
	ReadCredentialGenerations(
		ctx context.Context,
		registered model.DatabaseCredentialRegisteredSet,
	) ([]model.DatabaseCredentialGeneration, error)
	LoadOrCreateRotationIntent(
		ctx context.Context,
		holderID string,
		fencingToken uint64,
		requestID string,
		canonicalDigest string,
	) (model.DatabaseCredentialRotationIntent, error)
	AdvanceRotationIntent(
		ctx context.Context,
		holderID string,
		fencingToken uint64,
		requestID string,
		canonicalDigest string,
		expectedPhase model.DatabaseCredentialRotationPhase,
		nextPhase model.DatabaseCredentialRotationPhase,
		preRotationDigests map[string]string,
		stagedDigests map[string]string,
	) (model.DatabaseCredentialRotationIntent, error)
	ReadSessionReadbacks(
		ctx context.Context,
	) ([]model.DatabaseCredentialSessionReadback, error)
}

// VaultStaticRoleManager управляет жизненным циклом статических ролей Vault.
type VaultStaticRoleManager interface {
	VerifyStaticRoles(ctx context.Context, roles []VaultStaticRoleExpectation) error
	RotateStaticRoles(ctx context.Context, roles []VaultStaticRoleExpectation) error
	RevokeStaticRoles(ctx context.Context, roles []VaultStaticRoleExpectation) error
	VerifyRevokedStaticRoles(ctx context.Context, roles []VaultStaticRoleExpectation) error
	ReadStaticCredentialDigests(
		ctx context.Context,
		roles []VaultStaticRoleExpectation,
	) (map[string]string, error)
}

// CredentialRollout переводит consumers на уже продвинутое поколение.
type CredentialRollout interface {
	RolloutNext(
		ctx context.Context,
		requestID string,
		canonicalDigest string,
	) error
	RolloutCurrent(
		ctx context.Context,
		requestID string,
		canonicalDigest string,
	) error
}

// VaultStaticRoleExpectation задаёт точную связь роли Vault и principal.
type VaultStaticRoleExpectation struct {
	Role         string
	Principal    string
	DatabaseName string
}
