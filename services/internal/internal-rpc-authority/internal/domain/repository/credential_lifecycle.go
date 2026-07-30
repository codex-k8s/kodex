package repository

import (
	"context"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
)

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

type VaultStaticRoleManager interface {
	VerifyStaticRoles(ctx context.Context, roles []VaultStaticRoleExpectation) error
	RotateStaticRoles(ctx context.Context, roles []VaultStaticRoleExpectation) error
	RevokeStaticRoles(ctx context.Context, roles []VaultStaticRoleExpectation) error
	VerifyRevokedStaticRoles(ctx context.Context, roles []VaultStaticRoleExpectation) error
}

type VaultStaticRoleExpectation struct {
	Role         string
	Principal    string
	DatabaseName string
}
