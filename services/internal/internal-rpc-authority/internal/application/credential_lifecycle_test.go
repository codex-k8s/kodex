package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
)

const (
	testHolder  = "11111111-1111-4111-8111-111111111111"
	testRequest = "22222222-2222-4222-8222-222222222222"
)

func TestDatabaseCredentialLifecycleCrashRetryDoesNotRotateTwice(t *testing.T) {
	t.Parallel()

	baseline, target := testRegisteredSets()
	store := newCredentialStoreStub(target)
	vault := newVaultRoleStub(target)
	rollout := &credentialRolloutStub{}
	lifecycle, err := NewDatabaseCredentialLifecycle(
		testHolder, 20*time.Second, baseline, target, store, vault, rollout,
	)
	if err != nil {
		t.Fatalf("construct credential lifecycle: %v", err)
	}

	if _, err := lifecycle.Reconcile(context.Background(), testRequest); err == nil ||
		err.Error() != "NEXT database credential consumer readback is incomplete" {
		t.Fatalf("expected incomplete NEXT readback, got %v", err)
	}
	if vault.rotateCalls != 1 {
		t.Fatalf("expected one Vault rotation, got %d", vault.rotateCalls)
	}

	// Имитируем потерянный ответ после внешней ротации и повтор leader-а.
	store.intent.Phase = model.DatabaseCredentialRotationPreRotated
	if _, err := lifecycle.Reconcile(context.Background(), testRequest); err == nil ||
		err.Error() != "NEXT database credential consumer readback is incomplete" {
		t.Fatalf("expected durable retry wait, got %v", err)
	}
	if vault.rotateCalls != 1 {
		t.Fatalf("semantic retry repeated Vault rotation: %d", vault.rotateCalls)
	}
}

func TestDatabaseCredentialLifecyclePromotesAfterExactReadbacks(t *testing.T) {
	t.Parallel()

	baseline, target := testRegisteredSets()
	store := newCredentialStoreStub(target)
	vault := newVaultRoleStub(target)
	rollout := &credentialRolloutStub{}
	lifecycle, err := NewDatabaseCredentialLifecycle(
		testHolder, 20*time.Second, baseline, target, store, vault, rollout,
	)
	if err != nil {
		t.Fatalf("construct credential lifecycle: %v", err)
	}
	if _, err := lifecycle.Reconcile(context.Background(), testRequest); err == nil {
		t.Fatal("expected initial NEXT readback wait")
	}
	store.readbacks = readbacksFor(
		baseline,
		vault.digests,
		model.DatabaseCredentialNext,
	)
	if _, err := lifecycle.Reconcile(context.Background(), testRequest); err == nil {
		t.Fatal("expected post-rollout CURRENT/NEXT readback wait")
	}
	if rollout.nextCalls < 1 || rollout.currentCalls != 1 ||
		store.intent.Phase != model.DatabaseCredentialRotationRolledOut {
		t.Fatalf("promotion did not reach rollout: %#v / %#v", store.intent, rollout)
	}
	store.readbacks = readbacksFor(
		target,
		vault.digests,
		model.DatabaseCredentialCurrent,
		model.DatabaseCredentialNext,
	)
	result, err := lifecycle.Reconcile(context.Background(), testRequest)
	if err != nil {
		t.Fatalf("complete credential lifecycle: %v", err)
	}
	if len(result.Generations) != 10 ||
		store.intent.Phase != model.DatabaseCredentialRotationCompleted ||
		vault.rotateCalls != 1 ||
		vault.revokeCalls != 1 {
		t.Fatalf("unexpected completed lifecycle: %#v", result)
	}
	if _, err := lifecycle.Reconcile(context.Background(), testRequest); err != nil {
		t.Fatalf("completed semantic retry failed: %v", err)
	}
	if vault.rotateCalls != 1 || rollout.currentCalls != 1 ||
		vault.revokeCalls != 1 {
		t.Fatal("completed semantic retry repeated external effects")
	}
}

func testRegisteredSets() (
	model.DatabaseCredentialRegisteredSet,
	model.DatabaseCredentialRegisteredSet,
) {
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
	set := func(generations []model.DatabaseCredentialGeneration) model.DatabaseCredentialRegisteredSet {
		return model.DatabaseCredentialRegisteredSet{
			Version: model.ContractVersion, SourceRevision: 3,
			SourceDigest: sourceDigest, Generations: generations,
		}
	}
	baseline := set([]model.DatabaseCredentialGeneration{
		generation(model.DatabaseCredentialPublisher, 1, model.DatabaseCredentialRetired, "ira_publisher_g1", "internal-rpc-authority-publisher-g1"),
		generation(model.DatabaseCredentialPublisher, 2, model.DatabaseCredentialPrevious, "ira_publisher_g2", "internal-rpc-authority-publisher-g2"),
		generation(model.DatabaseCredentialPublisher, 3, model.DatabaseCredentialCurrent, "ira_publisher_g3", "internal-rpc-authority-publisher-g3"),
		generation(model.DatabaseCredentialPublisher, 4, model.DatabaseCredentialNext, "ira_publisher_g4", "internal-rpc-authority-publisher-g4"),
		generation(model.DatabaseCredentialAttestor, 1, model.DatabaseCredentialRetired, "ira_readback_attestor_g1", "internal-rpc-authority-readback-attestor-g1"),
		generation(model.DatabaseCredentialAttestor, 2, model.DatabaseCredentialPrevious, "ira_readback_attestor_g2", "internal-rpc-authority-readback-attestor-g2"),
		generation(model.DatabaseCredentialAttestor, 3, model.DatabaseCredentialCurrent, "ira_readback_attestor_g3", "internal-rpc-authority-readback-attestor-g3"),
		generation(model.DatabaseCredentialAttestor, 4, model.DatabaseCredentialNext, "ira_readback_attestor_g4", "internal-rpc-authority-readback-attestor-g4"),
	})
	target := set([]model.DatabaseCredentialGeneration{
		generation(model.DatabaseCredentialPublisher, 1, model.DatabaseCredentialRetired, "ira_publisher_g1", "internal-rpc-authority-publisher-g1"),
		generation(model.DatabaseCredentialPublisher, 2, model.DatabaseCredentialRetired, "ira_publisher_g2", "internal-rpc-authority-publisher-g2"),
		generation(model.DatabaseCredentialPublisher, 3, model.DatabaseCredentialPrevious, "ira_publisher_g3", "internal-rpc-authority-publisher-g3"),
		generation(model.DatabaseCredentialPublisher, 4, model.DatabaseCredentialCurrent, "ira_publisher_g4", "internal-rpc-authority-publisher-g4"),
		generation(model.DatabaseCredentialPublisher, 5, model.DatabaseCredentialNext, "ira_publisher_g5", "internal-rpc-authority-publisher-g5"),
		generation(model.DatabaseCredentialAttestor, 1, model.DatabaseCredentialRetired, "ira_readback_attestor_g1", "internal-rpc-authority-readback-attestor-g1"),
		generation(model.DatabaseCredentialAttestor, 2, model.DatabaseCredentialRetired, "ira_readback_attestor_g2", "internal-rpc-authority-readback-attestor-g2"),
		generation(model.DatabaseCredentialAttestor, 3, model.DatabaseCredentialPrevious, "ira_readback_attestor_g3", "internal-rpc-authority-readback-attestor-g3"),
		generation(model.DatabaseCredentialAttestor, 4, model.DatabaseCredentialCurrent, "ira_readback_attestor_g4", "internal-rpc-authority-readback-attestor-g4"),
		generation(model.DatabaseCredentialAttestor, 5, model.DatabaseCredentialNext, "ira_readback_attestor_g5", "internal-rpc-authority-readback-attestor-g5"),
	})
	return baseline, target
}

type credentialStoreStub struct {
	fencingToken uint64
	generations  []model.DatabaseCredentialGeneration
	intent       model.DatabaseCredentialRotationIntent
	readbacks    []model.DatabaseCredentialSessionReadback
}

func newCredentialStoreStub(target model.DatabaseCredentialRegisteredSet) *credentialStoreStub {
	return &credentialStoreStub{
		fencingToken: 7,
		generations:  append([]model.DatabaseCredentialGeneration(nil), target.Generations...),
	}
}

func (store *credentialStoreStub) AcquireLease(context.Context, string, time.Duration) (uint64, error) {
	return store.fencingToken, nil
}

func (store *credentialStoreStub) ReconcileCredentials(
	_ context.Context, _ string, _ uint64, _ string, _ string,
	registered model.DatabaseCredentialRegisteredSet,
) ([]model.DatabaseCredentialGeneration, error) {
	store.generations = append([]model.DatabaseCredentialGeneration(nil), registered.Generations...)
	return store.generations, nil
}

func (store *credentialStoreStub) ReadCredentialGenerations(
	_ context.Context,
	_ model.DatabaseCredentialRegisteredSet,
) ([]model.DatabaseCredentialGeneration, error) {
	return append([]model.DatabaseCredentialGeneration(nil), store.generations...), nil
}

func (store *credentialStoreStub) LoadOrCreateRotationIntent(
	_ context.Context, _ string, _ uint64, requestID string, digest string,
) (model.DatabaseCredentialRotationIntent, error) {
	if store.intent.RequestID == "" {
		store.intent = model.DatabaseCredentialRotationIntent{
			RequestID: requestID, CanonicalDigestSHA256: digest,
			Phase: model.DatabaseCredentialRotationCreated,
		}
	}
	return store.intent, nil
}

func (store *credentialStoreStub) AdvanceRotationIntent(
	_ context.Context, _ string, _ uint64, _ string, _ string,
	expected model.DatabaseCredentialRotationPhase,
	next model.DatabaseCredentialRotationPhase,
	pre map[string]string,
	staged map[string]string,
) (model.DatabaseCredentialRotationIntent, error) {
	if store.intent.Phase != expected {
		return model.DatabaseCredentialRotationIntent{}, context.Canceled
	}
	store.intent.Phase = next
	store.intent.PreRotationDigests = cloneDigests(pre)
	store.intent.StagedDigests = cloneDigests(staged)
	return store.intent, nil
}

func (store *credentialStoreStub) ReadSessionReadbacks(context.Context) (
	[]model.DatabaseCredentialSessionReadback,
	error,
) {
	return append([]model.DatabaseCredentialSessionReadback(nil), store.readbacks...), nil
}

type vaultRoleStub struct {
	digests     map[string]string
	rotateCalls int
	revokeCalls int
	verified    []repository.VaultStaticRoleExpectation
}

func newVaultRoleStub(target model.DatabaseCredentialRegisteredSet) *vaultRoleStub {
	digests := make(map[string]string)
	for _, generation := range target.Generations {
		digests[generation.VaultStaticRole] = strings.Repeat(
			string('a'+rune(generation.Generation)),
			64,
		)
	}
	return &vaultRoleStub{digests: digests}
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
	vault.rotateCalls++
	for _, role := range roles {
		vault.digests[role.Role] = strings.Repeat("f", 64)
	}
	return nil
}

func (vault *vaultRoleStub) RevokeStaticRoles(
	context.Context,
	[]repository.VaultStaticRoleExpectation,
) error {
	vault.revokeCalls++
	return nil
}

func (*vaultRoleStub) VerifyRevokedStaticRoles(
	context.Context,
	[]repository.VaultStaticRoleExpectation,
) error {
	return nil
}

func (vault *vaultRoleStub) ReadStaticCredentialDigests(
	_ context.Context,
	roles []repository.VaultStaticRoleExpectation,
) (map[string]string, error) {
	result := make(map[string]string, len(roles))
	for _, role := range roles {
		result[role.Role] = vault.digests[role.Role]
	}
	return result, nil
}

type credentialRolloutStub struct {
	nextCalls    int
	currentCalls int
}

func (rollout *credentialRolloutStub) RolloutNext(
	context.Context,
	string,
	string,
) error {
	rollout.nextCalls++
	return nil
}

func (rollout *credentialRolloutStub) RolloutCurrent(
	context.Context,
	string,
	string,
) error {
	rollout.currentCalls++
	return nil
}

func readbacksFor(
	registered model.DatabaseCredentialRegisteredSet,
	digests map[string]string,
	statuses ...model.DatabaseCredentialStatus,
) []model.DatabaseCredentialSessionReadback {
	var result []model.DatabaseCredentialSessionReadback
	for _, generation := range registered.Generations {
		for _, status := range statuses {
			if generation.Status != status {
				continue
			}
			result = append(result, model.DatabaseCredentialSessionReadback{
				Capability: generation.Capability, Generation: generation.Generation,
				Status: status, Principal: generation.Principal,
				CredentialDigestSHA256: digests[generation.VaultStaticRole],
				PodUID:                 testHolder,
				ObservedAt:             time.Now(),
			})
		}
	}
	return result
}

func cloneDigests(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}
