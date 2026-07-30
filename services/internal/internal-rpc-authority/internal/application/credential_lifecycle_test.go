package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/model"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
)

func TestDatabaseCredentialLifecycleReconcilesServerDerivedSet(t *testing.T) {
	t.Parallel()

	registered := testRegisteredSet()
	store := &credentialStoreStub{
		generations: append([]model.DatabaseCredentialGeneration(nil), registered.Generations...),
	}
	vault := &vaultRoleStub{}
	lifecycle, err := NewDatabaseCredentialLifecycle(
		"11111111-1111-4111-8111-111111111111",
		20*time.Second,
		registered,
		store,
		vault,
	)
	if err != nil {
		t.Fatalf("construct credential lifecycle: %v", err)
	}
	result, err := lifecycle.Reconcile(
		context.Background(),
		"22222222-2222-4222-8222-222222222222",
	)
	if err != nil {
		t.Fatalf("reconcile credential lifecycle: %v", err)
	}
	if result.ReceiptID != "22222222-2222-4222-8222-222222222222" ||
		len(result.Generations) != 8 ||
		len(result.CanonicalDigest) != 64 ||
		store.fencingToken != 7 ||
		len(vault.verified) != 6 ||
		len(vault.rotated) != 4 ||
		len(vault.revoked) != 2 {
		t.Fatalf("unexpected credential reconciliation result: %#v", result)
	}
}

func testRegisteredSet() model.DatabaseCredentialRegisteredSet {
	sourceDigest := strings.Repeat("a", 64)
	generation := func(
		capability model.DatabaseCredentialCapability,
		number uint64,
		status model.DatabaseCredentialStatus,
		principal string,
		role string,
	) model.DatabaseCredentialGeneration {
		return model.DatabaseCredentialGeneration{
			Capability: capability, Generation: number, Status: status,
			Principal: principal, VaultStaticRole: role,
			SourceRevision: 3, SourceDigest: sourceDigest,
		}
	}
	return model.DatabaseCredentialRegisteredSet{
		Version:        model.ContractVersion,
		SourceRevision: 3,
		SourceDigest:   sourceDigest,
		Generations: []model.DatabaseCredentialGeneration{
			generation(model.DatabaseCredentialPublisher, 1, model.DatabaseCredentialRetired, "ira_publisher_g1", "internal-rpc-authority-publisher-g1"),
			generation(model.DatabaseCredentialPublisher, 2, model.DatabaseCredentialPrevious, "ira_publisher_g2", "internal-rpc-authority-publisher-g2"),
			generation(model.DatabaseCredentialPublisher, 3, model.DatabaseCredentialCurrent, "ira_publisher_g3", "internal-rpc-authority-publisher-g3"),
			generation(model.DatabaseCredentialPublisher, 4, model.DatabaseCredentialNext, "ira_publisher_g4", "internal-rpc-authority-publisher-g4"),
			generation(model.DatabaseCredentialAttestor, 1, model.DatabaseCredentialRetired, "ira_readback_attestor_g1", "internal-rpc-authority-readback-attestor-g1"),
			generation(model.DatabaseCredentialAttestor, 2, model.DatabaseCredentialPrevious, "ira_readback_attestor_g2", "internal-rpc-authority-readback-attestor-g2"),
			generation(model.DatabaseCredentialAttestor, 3, model.DatabaseCredentialCurrent, "ira_readback_attestor_g3", "internal-rpc-authority-readback-attestor-g3"),
			generation(model.DatabaseCredentialAttestor, 4, model.DatabaseCredentialNext, "ira_readback_attestor_g4", "internal-rpc-authority-readback-attestor-g4"),
		},
	}
}

type credentialStoreStub struct {
	fencingToken uint64
	generations  []model.DatabaseCredentialGeneration
}

func (store *credentialStoreStub) AcquireLease(
	context.Context,
	string,
	time.Duration,
) (uint64, error) {
	store.fencingToken = 7
	return store.fencingToken, nil
}

func (store *credentialStoreStub) ReconcileCredentials(
	_ context.Context,
	_ string,
	fencingToken uint64,
	_ string,
	_ string,
	_ model.DatabaseCredentialRegisteredSet,
) ([]model.DatabaseCredentialGeneration, error) {
	store.fencingToken = fencingToken
	return append([]model.DatabaseCredentialGeneration(nil), store.generations...), nil
}

func (store *credentialStoreStub) ReadCredentialGenerations(
	context.Context,
	model.DatabaseCredentialRegisteredSet,
) ([]model.DatabaseCredentialGeneration, error) {
	return append([]model.DatabaseCredentialGeneration(nil), store.generations...), nil
}

type vaultRoleStub struct {
	verified []repository.VaultStaticRoleExpectation
	rotated  []repository.VaultStaticRoleExpectation
	revoked  []repository.VaultStaticRoleExpectation
}

func (vault *vaultRoleStub) VerifyStaticRoles(
	_ context.Context,
	roles []repository.VaultStaticRoleExpectation,
) error {
	vault.verified = append([]repository.VaultStaticRoleExpectation(nil), roles...)
	return nil
}

func (vault *vaultRoleStub) RotateStaticRoles(
	_ context.Context,
	roles []repository.VaultStaticRoleExpectation,
) error {
	vault.rotated = append([]repository.VaultStaticRoleExpectation(nil), roles...)
	return nil
}

func (vault *vaultRoleStub) RevokeStaticRoles(
	_ context.Context,
	roles []repository.VaultStaticRoleExpectation,
) error {
	vault.revoked = append([]repository.VaultStaticRoleExpectation(nil), roles...)
	return nil
}

func (*vaultRoleStub) VerifyRevokedStaticRoles(
	context.Context,
	[]repository.VaultStaticRoleExpectation,
) error {
	return nil
}
