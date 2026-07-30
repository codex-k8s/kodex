package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/model"
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
		len(result.Generations) != 4 ||
		len(result.CanonicalDigest) != 64 ||
		store.fencingToken != 7 ||
		len(vault.roles) != 4 {
		t.Fatalf("unexpected credential reconciliation result: %#v", result)
	}
}

func testRegisteredSet() model.DatabaseCredentialRegisteredSet {
	sourceDigest := strings.Repeat("a", 64)
	return model.DatabaseCredentialRegisteredSet{
		Version:        model.ContractVersion,
		SourceRevision: 3,
		SourceDigest:   sourceDigest,
		Generations: []model.DatabaseCredentialGeneration{
			{
				Capability:      model.DatabaseCredentialPublisher,
				Generation:      1,
				Status:          model.DatabaseCredentialCurrent,
				Principal:       "ira_publisher_g1",
				VaultStaticRole: "internal-rpc-authority-publisher-g1",
				SourceRevision:  3,
				SourceDigest:    sourceDigest,
			},
			{
				Capability:      model.DatabaseCredentialPublisher,
				Generation:      2,
				Status:          model.DatabaseCredentialNext,
				Principal:       "ira_publisher_g2",
				VaultStaticRole: "internal-rpc-authority-publisher-g2",
				SourceRevision:  3,
				SourceDigest:    sourceDigest,
			},
			{
				Capability:      model.DatabaseCredentialAttestor,
				Generation:      1,
				Status:          model.DatabaseCredentialCurrent,
				Principal:       "ira_readback_attestor_g1",
				VaultStaticRole: "internal-rpc-authority-readback-attestor-g1",
				SourceRevision:  3,
				SourceDigest:    sourceDigest,
			},
			{
				Capability:      model.DatabaseCredentialAttestor,
				Generation:      2,
				Status:          model.DatabaseCredentialNext,
				Principal:       "ira_readback_attestor_g2",
				VaultStaticRole: "internal-rpc-authority-readback-attestor-g2",
				SourceRevision:  3,
				SourceDigest:    sourceDigest,
			},
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
	roles []string
}

func (vault *vaultRoleStub) VerifyStaticRoles(_ context.Context, roles []string) error {
	vault.roles = append([]string(nil), roles...)
	return nil
}
