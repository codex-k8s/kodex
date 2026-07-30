package service

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/failure"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/model"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
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

func testPublisherRegistry() model.DeliveryTargetRegistry {
	target := model.DeliveryTarget{
		TargetID:              "control-plane.authorization-verifier",
		WorkloadID:            "control-plane",
		WorkloadSPIFFEID:      "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane",
		Role:                  "AUTHORIZATION_VERIFIER",
		WorkloadGeneration:    1,
		CredentialGeneration:  1,
		RestoreCredentialPath: "kv/data/mattercodex/internal-rpc-authority/control-plane/verifier/restore-credential",
		RestoreACKKeyPath:     "kv/data/mattercodex/internal-rpc-authority/control-plane/verifier/restore-ack",
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
	mu    sync.Mutex
	value *model.PublishedCredential
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

func (*memoryPublisherStore) PublisherReady(context.Context) error { return nil }

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
	if existing, ok := delivery.values[path]; ok {
		return existing, nil
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

var _ repository.PublisherStore = (*memoryPublisherStore)(nil)
var _ repository.SecretDelivery = (*memorySecretDelivery)(nil)
