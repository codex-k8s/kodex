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
}

// VaultStaticRoleManager управляет жизненным циклом статических ролей Vault.
type VaultStaticRoleManager interface {
	VerifyStaticRoles(ctx context.Context, roles []VaultStaticRoleExpectation) error
	RotateStaticRoles(ctx context.Context, roles []VaultStaticRoleExpectation) error
	RevokeStaticRoles(ctx context.Context, roles []VaultStaticRoleExpectation) error
	VerifyRevokedStaticRoles(ctx context.Context, roles []VaultStaticRoleExpectation) error
}

// VaultStaticRoleExpectation задаёт точную связь роли Vault и principal.
type VaultStaticRoleExpectation struct {
	Role         string
	Principal    string
	DatabaseName string
}
