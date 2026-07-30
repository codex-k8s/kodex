package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/failure"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
)

func TestPublisherDeliveryRetryAndConflict(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_800_000_000, 0).UTC()
	controllerKey := testKey(t, "controller-tls-aaaaaaaaaaaaaaaaaaaaaaaa")
	signerKey := testKey(t, "restore-credential-signer-g1")
	registry := testPublisherRegistry()
	store := &memoryPublisherStore{}
	vault := newMemorySecretDelivery()
	publisher, err := NewPublisher(
		registry,
		RestoreCredentialSigner{
			Key: signerKey, SourceRevision: 1,
			SourceDigest:   strings.Repeat("b", 64),
			KeySetRevision: 1, Generation: 1,
		},
		ReadbackCredentialSigner{
			Key:            testKey(t, "readback-credential-signer-g1"),
			SourceRevision: 1, SourceDigest: strings.Repeat("c", 64),
			KeySetRevision: 1, Generation: 1,
		},
		store,
		vault,
	)
	if err != nil {
		t.Fatalf("construct publisher: %v", err)
	}
	publisher.now = func() time.Time { return now }
	directive := testIssuanceDirective(registry, now)
	compact := signIssuanceDirective(t, controllerKey, directive)
	controller := ControllerIdentity{
		SPIFFEID:   restoreControllerSPIFFE,
		Key:        controllerKey.PublicOnly(),
		Generation: 1,
	}
	first, err := publisher.Publish(
		context.Background(),
		controller,
		compact,
		"22222222-2222-4222-8222-222222222222",
	)
	if err != nil {
		t.Fatalf("publish restore credential: %v", err)
	}
	retried, err := publisher.Publish(
		context.Background(),
		controller,
		compact,
		"22222222-2222-4222-8222-222222222222",
	)
	if err != nil {
		t.Fatalf("retry restore credential: %v", err)
	}
	if first.DeliveryReceiptJWS != retried.DeliveryReceiptJWS ||
		first.RoleCredentialDigest != retried.RoleCredentialDigest {
		t.Fatal("publisher retry did not return persisted delivery")
	}
	directive.JTI = "33333333-3333-4333-8333-333333333333"
	conflict := signIssuanceDirective(t, controllerKey, directive)
	if _, err := publisher.Publish(
		context.Background(),
		controller,
		conflict,
		"22222222-2222-4222-8222-222222222222",
	); !failure.IsKind(err, failure.ReplayDetected) {
		t.Fatalf("expected publication idempotency conflict, got %v", err)
	}
	if len(vault.values) != 2 {
		t.Fatalf("publisher wrote %d Vault paths, expected exact role and ACK paths", len(vault.values))
	}
}

func TestPublisherRejectsWrongTargetAndAudience(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_800_000_000, 0).UTC()
	controllerKey := testKey(t, "controller-tls-bbbbbbbbbbbbbbbbbbbbbbbb")
	signerKey := testKey(t, "restore-credential-signer-g1")
	registry := testPublisherRegistry()
	publisher, err := NewPublisher(
		registry,
		RestoreCredentialSigner{
			Key: signerKey, SourceRevision: 1,
			SourceDigest:   strings.Repeat("b", 64),
			KeySetRevision: 1, Generation: 1,
		},
		ReadbackCredentialSigner{
			Key:            testKey(t, "readback-credential-signer-g1"),
			SourceRevision: 1, SourceDigest: strings.Repeat("c", 64),
			KeySetRevision: 1, Generation: 1,
		},
		&memoryPublisherStore{},
		newMemorySecretDelivery(),
	)
	if err != nil {
		t.Fatalf("construct publisher: %v", err)
	}
	publisher.now = func() time.Time { return now }
	controller := ControllerIdentity{
		SPIFFEID:   restoreControllerSPIFFE,
		Key:        controllerKey.PublicOnly(),
		Generation: 1,
	}
	directive := testIssuanceDirective(registry, now)
	directive.WorkloadGeneration++
	if _, err := publisher.Publish(
		context.Background(),
		controller,
		signIssuanceDirective(t, controllerKey, directive),
		"22222222-2222-4222-8222-222222222222",
	); !failure.IsKind(err, failure.BindingMismatch) {
		t.Fatalf("expected target registry binding rejection, got %v", err)
	}
	directive = testIssuanceDirective(registry, now)
	directive.Audience = restoreControllerAudience
	if _, err := publisher.Publish(
		context.Background(),
		controller,
		signIssuanceDirective(t, controllerKey, directive),
		"44444444-4444-4444-8444-444444444444",
	); !failure.IsKind(err, failure.Unauthenticated) {
		t.Fatalf("expected cross-audience rejection, got %v", err)
	}
}

func TestPublisherRestoreFenceRejectsExternalWrites(t *testing.T) {
	t.Parallel()
	store := &memoryPublisherStore{readyErr: errors.New("restore fence closed")}
	vault := newMemorySecretDelivery()
	publisher, err := NewPublisher(
		testPublisherRegistry(),
		RestoreCredentialSigner{
			Key:            testKey(t, "restore-credential-signer-g1"),
			SourceRevision: 1, SourceDigest: strings.Repeat("b", 64),
			KeySetRevision: 1, Generation: 1,
		},
		ReadbackCredentialSigner{
			Key:            testKey(t, "readback-credential-signer-g1"),
			SourceRevision: 1, SourceDigest: strings.Repeat("c", 64),
			KeySetRevision: 1, Generation: 1,
		},
		store,
		vault,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := publisher.PublishReadbackMaterials(context.Background()); err == nil {
		t.Fatal("restore fence accepted readback material publication")
	}
	if len(vault.values) != 0 {
		t.Fatal("restore fence rejection happened after external Vault mutation")
	}
}

func TestPublisherPinsAndRotatesIndependentReadbackMaterial(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_800_000_000, 0).UTC()
	restoreSigner := testKey(t, "restore-credential-signer-g1")
	readbackSigner := testKey(t, "readback-credential-signer-g1")
	store := &memoryPublisherStore{}
	vault := newMemorySecretDelivery()
	publisher, err := NewPublisher(
		testPublisherRegistry(),
		RestoreCredentialSigner{
			Key: restoreSigner, SourceRevision: 1,
			SourceDigest:   strings.Repeat("b", 64),
			KeySetRevision: 1, Generation: 1,
		},
		ReadbackCredentialSigner{
			Key: readbackSigner, SourceRevision: 2,
			SourceDigest:   strings.Repeat("c", 64),
			KeySetRevision: 2, Generation: 2,
		},
		store,
		vault,
	)
	if err != nil {
		t.Fatalf("construct publisher: %v", err)
	}
	publisher.now = func() time.Time { return now }
	first, err := publisher.PublishReadbackMaterials(context.Background())
	if err != nil {
		t.Fatalf("publish readback material: %v", err)
	}
	if len(first) != 1 ||
		first[0].Intent.Kind != "SNAPSHOT" ||
		first[0].CredentialVaultVersion != 1 ||
		first[0].PossessionVaultVersion != 1 {
		t.Fatalf("unexpected first readback publication: %#v", first)
	}
	verified, err := internalrpcauth.VerifyCanonicalJSON(
		first[0].ReadbackCredentialJWS,
		readbackSigner.PublicOnly(),
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  readbackCredentialType,
			KeyID: readbackSigner.KeyID,
		},
	)
	if err != nil {
		t.Fatalf("verify independently signed readback credential: %v", err)
	}
	var claims model.ReadbackCredentialClaims
	if err := internalrpcauth.DecodeCanonicalJSON(
		verified.CanonicalPayload,
		&claims,
	); err != nil {
		t.Fatalf("decode readback credential: %v", err)
	}
	if claims.IntentID != first[0].Intent.IntentID ||
		claims.PossessionKeyThumbprint != first[0].Intent.PossessionKeyThumbprint {
		t.Fatal("readback credential does not bind the persisted intent")
	}
	now = now.Add(11 * time.Second)
	second, err := publisher.PublishReadbackMaterials(context.Background())
	if err != nil {
		t.Fatalf("rotate readback material: %v", err)
	}
	if second[0].Intent.IntentID == first[0].Intent.IntentID ||
		second[0].CredentialVaultVersion != 2 ||
		second[0].PossessionVaultVersion != 1 ||
		second[0].PossessionPrivateJWK != first[0].PossessionPrivateJWK {
		t.Fatalf("readback rotation did not preserve possession and advance credential: %#v", second[0])
	}
	target := publisher.registry.Targets["control-plane.authorization-verifier"]
	target.ReadbackMaterialGeneration = 2
	publisher.registry.Targets[target.TargetID] = target
	now = now.Add(11 * time.Second)
	third, err := publisher.PublishReadbackMaterials(context.Background())
	if err != nil {
		t.Fatalf("rotate possession generation: %v", err)
	}
	if third[0].PossessionVaultVersion != 2 ||
		third[0].PossessionPrivateJWK == second[0].PossessionPrivateJWK {
		t.Fatal("readback possession generation did not advance by CAS")
	}
	target.ReadbackMaterialGeneration = 4
	publisher.registry.Targets[target.TargetID] = target
	if _, err := publisher.PublishReadbackMaterials(context.Background()); err == nil {
		t.Fatal("gapped readback possession generation was accepted")
	}
	stored, found, err := vault.ReadKV2(
		context.Background(),
		target.ReadbackPossessionKeyPath,
	)
	if err != nil || !found || stored.Version != 2 {
		t.Fatal("rejected possession generation gap mutated Vault state")
	}
}

func TestPublisherConcurrentReadbackReturnsPersistedCredential(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_800_000_000, 0).UTC()
	store := &memoryPublisherStore{}
	vault := newMemorySecretDelivery()
	publisher, err := NewPublisher(
		testPublisherRegistry(),
		RestoreCredentialSigner{
			Key:            testKey(t, "restore-credential-signer-g1"),
			SourceRevision: 1, SourceDigest: strings.Repeat("b", 64),
			KeySetRevision: 1, Generation: 1,
		},
		ReadbackCredentialSigner{
			Key:            testKey(t, "readback-credential-signer-g1"),
			SourceRevision: 1, SourceDigest: strings.Repeat("c", 64),
			KeySetRevision: 1, Generation: 1,
		},
		store,
		vault,
	)
	if err != nil {
		t.Fatal(err)
	}
	publisher.now = func() time.Time { return now.Add(7 * time.Second) }
	const replicas = 8
	results := make(chan model.PublishedReadbackMaterial, replicas)
	failures := make(chan error, replicas)
	var group sync.WaitGroup
	for replica := 0; replica < replicas; replica++ {
		group.Add(1)
		go func() {
			defer group.Done()
			values, publishErr := publisher.PublishReadbackMaterials(
				context.Background(),
			)
			if publishErr != nil {
				failures <- publishErr
				return
			}
			results <- values[0]
		}()
	}
	group.Wait()
	close(results)
	close(failures)
	for publishErr := range failures {
		t.Fatalf("concurrent readback publication failed: %v", publishErr)
	}
	var expected model.PublishedReadbackMaterial
	for value := range results {
		if expected.ReadbackCredentialJWS == "" {
			expected = value
			continue
		}
		if value.ReadbackCredentialJWS != expected.ReadbackCredentialJWS ||
			value.PossessionPrivateJWK != expected.PossessionPrivateJWK ||
			value.Intent.IntentDigestSHA256 != expected.Intent.IntentDigestSHA256 {
			t.Fatal("concurrent publisher did not return persisted material")
		}
	}
}

func testPublisherRegistry() model.DeliveryTargetRegistry {
	target := model.DeliveryTarget{
		TargetID:                   "control-plane.authorization-verifier",
		WorkloadID:                 "control-plane",
		WorkloadSPIFFEID:           "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane",
		Role:                       "AUTHORIZATION_VERIFIER",
		WorkloadGeneration:         1,
		CredentialGeneration:       1,
		RestoreCredentialPath:      "kv/data/mattercodex/internal-rpc-authority/control-plane/verifier/restore-credential",
		RestoreACKKeyPath:          "kv/data/mattercodex/internal-rpc-authority/control-plane/verifier/restore-ack",
		ReadbackCredentialPath:     "kv/data/mattercodex/internal-rpc-authority/control-plane/verifier/readback-credential",
		ReadbackPossessionKeyPath:  "kv/data/mattercodex/internal-rpc-authority/control-plane/verifier/readback-possession",
		ReadbackIntentRevision:     1,
		ReadbackMaterialGeneration: 1,
		ReadbackSourceRevision:     1,
		ReadbackServedStateDigest:  strings.Repeat("d", 64),
	}
	return model.DeliveryTargetRegistry{
		Version: model.ContractVersion, SourceRevision: 1,
		SourceDigest: strings.Repeat("a", 64),
		Targets:      map[string]model.DeliveryTarget{target.TargetID: target},
	}
}

func testIssuanceDirective(
	registry model.DeliveryTargetRegistry,
	now time.Time,
) model.CredentialIssuanceDirective {
	target := registry.Targets["control-plane.authorization-verifier"]
	return model.CredentialIssuanceDirective{
		Version: model.ContractVersion, Issuer: restoreControllerSPIFFE,
		Audience: restorePublisherAudience, Subject: target.WorkloadID,
		JTI:          "11111111-1111-4111-8111-111111111111",
		RestoreID:    "55555555-5555-4555-8555-555555555555",
		RestoreEpoch: 2, CoordinationRevision: 3,
		TargetRegistryRevision: registry.SourceRevision,
		TargetRegistryDigest:   registry.SourceDigest,
		DeliveryTargetID:       target.TargetID,
		WorkloadID:             target.WorkloadID,
		WorkloadSPIFFEID:       target.WorkloadSPIFFEID,
		Role:                   target.Role, WorkloadGeneration: target.WorkloadGeneration,
		CredentialGeneration: target.CredentialGeneration,
		ACKKeyGeneration:     1,
		IssuedAt:             now.Unix(), NotBefore: now.Unix(),
		ExpiresAt: now.Add(restoreIssuanceTTL).Unix(),
	}
}

func signIssuanceDirective(
	t *testing.T,
	key internalrpcauth.ES256Key,
	claims model.CredentialIssuanceDirective,
) string {
	t.Helper()
	compact, err := internalrpcauth.SignCanonicalJSON(
		claims,
		key,
		internalrpcauth.ProtectedHeaderExpectation{
			Type: restoreIssuanceType, KeyID: key.KeyID,
		},
	)
	if err != nil {
		t.Fatalf("sign issuance directive: %v", err)
	}
	return compact
}

type memoryPublisherStore struct {
	mu          sync.Mutex
	value       *model.PublishedCredential
	intents     map[string]model.ReadbackIntent
	publication *model.AuthoritySnapshotPublication
	readyErr    error
}

func (store *memoryPublisherStore) PinReadbackIntent(
	_ context.Context,
	value model.ReadbackIntent,
) (model.ReadbackIntent, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.intents == nil {
		store.intents = make(map[string]model.ReadbackIntent)
	}
	if existing, ok := store.intents[value.IntentID]; ok {
		if existing.IntentDigestSHA256 != value.IntentDigestSHA256 {
			return model.ReadbackIntent{}, repository.ErrIdempotencyConflict
		}
		return existing, nil
	}
	store.intents[value.IntentID] = value
	return value, nil
}

func (store *memoryPublisherStore) LoadPublishedCredential(
	_ context.Context,
	idempotencyKey string,
) (model.PublishedCredential, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.value == nil || store.value.IdempotencyKey != idempotencyKey {
		return model.PublishedCredential{}, false, nil
	}
	return *store.value, true, nil
}

func (store *memoryPublisherStore) SavePublishedCredential(
	_ context.Context,
	value model.PublishedCredential,
) (model.PublishedCredential, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.value != nil {
		if store.value.DirectiveDigest != value.DirectiveDigest {
			return model.PublishedCredential{}, repository.ErrIdempotencyConflict
		}
		return *store.value, nil
	}
	store.value = &value
	return value, nil
}

func (store *memoryPublisherStore) PublisherReady(context.Context) error {
	return store.readyErr
}

func (*memoryPublisherStore) LoadSnapshotHistory(
	context.Context,
) (model.AuthoritySnapshotHistory, error) {
	return model.AuthoritySnapshotHistory{}, nil
}

func (store *memoryPublisherStore) LoadSnapshotPublication(
	_ context.Context,
	sourceRevision uint64,
	inputDigest string,
) (model.AuthoritySnapshotPublication, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.publication == nil ||
		store.publication.SourceRevision != sourceRevision ||
		store.publication.InputDigestSHA256 != inputDigest {
		return model.AuthoritySnapshotPublication{}, false, nil
	}
	return *store.publication, true, nil
}

func (store *memoryPublisherStore) AppendSnapshot(
	_ context.Context,
	value model.AuthoritySnapshotPublication,
	_ int,
) (model.AuthoritySnapshotPublication, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.publication != nil {
		return *store.publication, nil
	}
	store.publication = &value
	return value, nil
}

func (*memoryPublisherStore) SnapshotPublicationReady(
	context.Context,
	model.AuthoritySnapshotPublication,
	int,
) error {
	return nil
}

type memorySecretDelivery struct {
	mu     sync.Mutex
	values map[string]repository.SecretMaterial
}

func newMemorySecretDelivery() *memorySecretDelivery {
	return &memorySecretDelivery{values: make(map[string]repository.SecretMaterial)}
}

func (delivery *memorySecretDelivery) ReadKV2(
	_ context.Context,
	path string,
) (repository.SecretMaterial, bool, error) {
	delivery.mu.Lock()
	defer delivery.mu.Unlock()
	value, ok := delivery.values[path]
	return value, ok, nil
}

func (delivery *memorySecretDelivery) CreateKV2(
	_ context.Context,
	path string,
	data map[string]string,
) (repository.SecretMaterial, error) {
	delivery.mu.Lock()
	defer delivery.mu.Unlock()
	if _, ok := delivery.values[path]; ok {
		return repository.SecretMaterial{}, repository.ErrIdempotencyConflict
	}
	digest, err := internalrpcauth.CanonicalJSONSHA256(data)
	if err != nil {
		return repository.SecretMaterial{}, err
	}
	value := repository.SecretMaterial{
		Version: 1, Data: data, Digest: digest,
	}
	delivery.values[path] = value
	return value, nil
}

func (delivery *memorySecretDelivery) WriteKV2CAS(
	_ context.Context,
	path string,
	expectedVersion uint64,
	data map[string]string,
) (repository.SecretMaterial, error) {
	delivery.mu.Lock()
	defer delivery.mu.Unlock()
	existing, ok := delivery.values[path]
	if !ok || existing.Version != expectedVersion {
		return repository.SecretMaterial{}, repository.ErrIdempotencyConflict
	}
	digest, err := internalrpcauth.CanonicalJSONSHA256(data)
	if err != nil {
		return repository.SecretMaterial{}, err
	}
	value := repository.SecretMaterial{
		Version: existing.Version + 1,
		Data:    data,
		Digest:  digest,
	}
	delivery.values[path] = value
	return value, nil
}

var _ repository.PublisherStore = (*memoryPublisherStore)(nil)
var _ repository.SecretDelivery = (*memorySecretDelivery)(nil)
