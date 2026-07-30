package service

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/failure"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
)

func TestRestoreControllerPrepareDirectiveACKAndSemanticRetry(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_800_000_000, 0).UTC()
	controllerKey := testKey(t, "controller-tls-cccccccccccccccccccccccc")
	roleSigner := testKey(t, "restore-role-credential-g1")
	registry := testPublisherRegistry()
	coordination := newMemoryRestoreCoordination()
	fence := &memoryRestoreFence{}
	publisher := &memoryRestorePublisher{}
	controller, err := NewRestoreController(
		"internal-rpc-authority-primary",
		controllerKey,
		1,
		registry,
		map[string]VerificationKeyRecord{
			roleSigner.KeyID: {
				Key:        roleSigner.PublicOnly(),
				Issuer:     restorePublisherSPIFFE,
				Generation: 1,
				Status:     "CURRENT",
				Purpose:    restoreRoleCredentialPurpose,
				Audiences:  map[string]struct{}{restoreControllerAudience: {}},
				NotBefore:  now.Add(-time.Minute),
				NotAfter:   now.Add(time.Hour),
			},
		},
		model.RestoreRoleTrustMetadata{
			SourceRevision:   1,
			SourceDigest:     strings.Repeat("d", 64),
			KeySetRevision:   1,
			SignerGeneration: 1,
		},
		coordination,
		fence,
		publisher,
		&memoryRestoreEvidence{now: now},
	)
	if err != nil {
		t.Fatalf("construct restore controller: %v", err)
	}
	controller.now = func() time.Time { return now }
	discovery, err := controller.GetDirective(
		context.Background(),
		RestorePeer{
			SPIFFEID: "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane",
		},
		"",
		0,
	)
	if err != nil || !discovery.NoDirective || discovery.State.Phase != "OPEN" {
		t.Fatalf("OPEN discovery must not require a role credential: %#v %v", discovery, err)
	}
	state, err := controller.Prepare(
		context.Background(),
		model.PrepareRestoreCommand{
			RestoreID:            "55555555-5555-4555-8555-555555555555",
			DatabaseClusterID:    "internal-rpc-authority-primary",
			BackupManifestDigest: strings.Repeat("e", 64),
			RecoveryTarget:       now.Add(-time.Hour),
			IdempotencyKey:       "66666666-6666-4666-8666-666666666666",
			SemanticDigest:       strings.Repeat("f", 64),
		},
	)
	if err != nil {
		t.Fatalf("prepare restore: %v", err)
	}
	if state.Phase != "QUIESCING" ||
		len(state.Deliveries) != 1 ||
		fence.last.Phase != "QUIESCING" ||
		publisher.calls != 1 {
		t.Fatalf("prepare did not reach durable QUIESCING: %#v", state)
	}
	if _, err := controller.GetDirective(
		context.Background(),
		RestorePeer{
			SPIFFEID: "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane",
		},
		"",
		0,
	); err == nil {
		t.Fatal("QUIESCING directive was disclosed without role credential")
	}
	target := registry.Targets["control-plane.authorization-verifier"]
	ackKey := testKey(t, "control-plane-ack-g1")
	ackPublic, err := internalrpcauth.MarshalPublicJWK(ackKey.PublicOnly())
	if err != nil {
		t.Fatal(err)
	}
	ackThumbprint, err := internalrpcauth.PublicJWKThumbprintSHA256(ackKey.PublicOnly())
	if err != nil {
		t.Fatal(err)
	}
	roleClaims := model.RestoreRoleCredentialClaims{
		Version:                  model.ContractVersion,
		Issuer:                   restorePublisherSPIFFE,
		Audience:                 restoreControllerAudience,
		Subject:                  target.WorkloadID,
		JTI:                      "77777777-7777-4777-8777-777777777777",
		WorkloadID:               target.WorkloadID,
		WorkloadSPIFFEID:         target.WorkloadSPIFFEID,
		Role:                     target.Role,
		WorkloadGeneration:       target.WorkloadGeneration,
		CredentialGeneration:     target.CredentialGeneration,
		RestoreID:                state.RestoreID,
		RestoreEpoch:             state.RestoreEpoch,
		CoordinationRevision:     state.CoordinationRevision,
		SignerSourceRevision:     1,
		SignerSourceDigestSHA256: strings.Repeat("d", 64),
		SignerKeySetRevision:     1,
		SignerGeneration:         1,
		SignerKeyID:              roleSigner.KeyID,
		ACKKeyID:                 ackKey.KeyID,
		ACKKeyGeneration:         1,
		ACKPublicJWK:             ackPublic,
		ACKKeyThumbprintSHA256:   ackThumbprint,
		IssuedAt:                 now.Unix(),
		NotBefore:                now.Unix(),
		ExpiresAt:                now.Add(restoreRoleCredentialTTL).Unix(),
	}
	roleCompact, err := internalrpcauth.SignCanonicalJSON(
		roleClaims,
		roleSigner,
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  restoreRoleCredentialType,
			KeyID: roleSigner.KeyID,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	coordination.mu.Lock()
	delivery := coordination.state.Deliveries[target.TargetID]
	delivery.RoleCredentialDigestSHA256 = digestCompact(roleCompact)
	coordination.state.Deliveries[target.TargetID] = delivery
	coordination.mu.Unlock()
	directive, err := controller.GetDirective(
		context.Background(),
		RestorePeer{SPIFFEID: target.WorkloadSPIFFEID},
		roleCompact,
		0,
	)
	if err != nil || directive.NoDirective {
		t.Fatalf("get restore directive: result=%#v err=%v", directive, err)
	}
	verifiedDirective, err := internalrpcauth.VerifyCanonicalJSON(
		directive.DirectiveCompact,
		controllerKey.PublicOnly(),
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  restoreDirectiveType,
			KeyID: controllerKey.KeyID,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	var directiveClaims model.RoleBoundRestoreDirectiveClaims
	if err := internalrpcauth.DecodeCanonicalJSON(
		verifiedDirective.CanonicalPayload,
		&directiveClaims,
	); err != nil {
		t.Fatal(err)
	}
	ackClaims := model.QuiescenceACKClaims{
		Version:                    model.ContractVersion,
		Issuer:                     target.WorkloadSPIFFEID,
		Audience:                   restoreControllerAudience,
		Subject:                    target.WorkloadID,
		JTI:                        "88888888-8888-4888-8888-888888888888",
		DirectiveJTI:               directiveClaims.JTI,
		RestoreID:                  state.RestoreID,
		RestoreEpoch:               state.RestoreEpoch,
		CoordinationRevision:       state.CoordinationRevision,
		WorkloadID:                 target.WorkloadID,
		WorkloadSPIFFEID:           target.WorkloadSPIFFEID,
		Role:                       target.Role,
		WorkloadGeneration:         target.WorkloadGeneration,
		CredentialGeneration:       target.CredentialGeneration,
		ACKKeyID:                   ackKey.KeyID,
		ACKKeyGeneration:           1,
		ACKKeyThumbprintSHA256:     ackThumbprint,
		RoleCredentialDigestSHA256: digestCompact(roleCompact),
		ServedSnapshotDigest:       strings.Repeat("a", 64),
		AcceptingStopped:           true,
		InflightDrained:            true,
		InflightCount:              0,
		IssuedAt:                   now.Unix(),
		NotBefore:                  now.Unix(),
		ExpiresAt:                  now.Add(restoreACKTTL).Unix(),
	}
	ackCompact, err := internalrpcauth.SignCanonicalJSON(
		ackClaims,
		ackKey,
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  restoreACKType,
			KeyID: ackKey.KeyID,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.GetDirective(
		context.Background(),
		RestorePeer{SPIFFEID: "spiffe://mattercodex.local/ns/mattercodex-system/sa/other"},
		roleCompact,
		0,
	); !failure.IsKind(err, failure.Unauthenticated) {
		t.Fatalf("opposite workload SPIFFE was accepted: %v", err)
	}
	first, err := controller.Acknowledge(
		context.Background(),
		RestorePeer{SPIFFEID: target.WorkloadSPIFFEID},
		roleCompact,
		directive.DirectiveCompact,
		ackCompact,
		"99999999-9999-4999-8999-999999999999",
	)
	if err != nil {
		t.Fatalf("acknowledge restore: %v", err)
	}
	retried, err := controller.Acknowledge(
		context.Background(),
		RestorePeer{SPIFFEID: target.WorkloadSPIFFEID},
		roleCompact,
		directive.DirectiveCompact,
		ackCompact,
		"99999999-9999-4999-8999-999999999999",
	)
	if err != nil {
		t.Fatalf("retry ACK: %v", err)
	}
	if first.State.Phase != "PREPARED" ||
		first.Receipt.ReceiptID != retried.Receipt.ReceiptID ||
		fence.last.Phase != "PREPARED" {
		t.Fatalf("ACK did not produce stable PREPARED receipt: %#v %#v", first, retried)
	}
}

func TestRestoreOperatorCredentialСвязанСТочнымRPCИОдноразовойСемантикой(
	t *testing.T,
) {
	t.Parallel()
	now := time.Unix(1_800_000_000, 0).UTC()
	controllerKey := testKey(t, "controller-tls-operator-binding")
	roleSigner := testKey(t, "restore-role-operator-binding")
	store := newMemoryRestoreCoordination()
	controller, err := NewRestoreController(
		"internal-rpc-authority-primary",
		controllerKey,
		1,
		testPublisherRegistry(),
		map[string]VerificationKeyRecord{
			roleSigner.KeyID: {
				Key: roleSigner.PublicOnly(), Issuer: restorePublisherSPIFFE,
				Generation: 1, Status: "CURRENT",
				Purpose:   restoreRoleCredentialPurpose,
				Audiences: map[string]struct{}{restoreControllerAudience: {}},
				NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour),
			},
		},
		model.RestoreRoleTrustMetadata{
			SourceRevision: 1, SourceDigest: strings.Repeat("d", 64),
			KeySetRevision: 1, SignerGeneration: 1,
		},
		store,
		&memoryRestoreFence{},
		&memoryRestorePublisher{},
		&memoryRestoreEvidence{now: now},
	)
	if err != nil {
		t.Fatal(err)
	}
	controller.now = func() time.Time { return now }
	credential := model.RestoreOperatorCredential{
		Subject:           "system:serviceaccount:mattercodex-system:internal-rpc-authority-restore-operator",
		Namespace:         "mattercodex-system",
		ServiceAccount:    "internal-rpc-authority-restore-operator",
		Audience:          restoreOperatorAudience,
		TokenDigestSHA256: strings.Repeat("a", 64),
	}
	idempotencyKey := "11111111-1111-4111-8111-111111111111"
	semanticDigest := strings.Repeat("b", 64)
	method := "/internalrpcauthority.v1.RestoreControllerService/PrepareRestore"
	if err := controller.AuthorizeOperator(
		context.Background(), credential, method, idempotencyKey, semanticDigest,
	); err != nil {
		t.Fatalf("authorize exact operator command: %v", err)
	}
	if err := controller.AuthorizeOperator(
		context.Background(), credential, method, idempotencyKey, semanticDigest,
	); err != nil {
		t.Fatalf("semantic retry rejected: %v", err)
	}
	if err := controller.AuthorizeOperator(
		context.Background(),
		credential,
		"/internalrpcauthority.v1.RestoreControllerService/CompleteRestore",
		idempotencyKey,
		semanticDigest,
	); !failure.IsKind(err, failure.ReplayDetected) {
		t.Fatalf("cross-RPC token replay accepted: %v", err)
	}
	if len(store.state.OperatorAuthorizations) != 1 {
		t.Fatalf(
			"operator authorization records = %d",
			len(store.state.OperatorAuthorizations),
		)
	}
}

type memoryRestoreCoordination struct {
	mu    sync.Mutex
	state model.RestoreState
}

type memoryRestoreEvidence struct {
	now time.Time
}

func (evidence *memoryRestoreEvidence) VerifyCompletedEvidence(
	_ context.Context,
	state model.RestoreState,
) (model.RestoreCompletionEvidence, error) {
	return model.RestoreCompletionEvidence{
		CompactJWSDigestSHA256: strings.Repeat("e", 64),
		AnchorRevision:         state.AnchorRevision + 1,
		RestoreEpoch:           state.RestoreEpoch,
		RestoredClusterUID:     "restored-cluster-uid",
		RestoredTimelineID:     2,
		RestoreCompletedAt:     evidence.now,
	}, nil
}

func newMemoryRestoreCoordination() *memoryRestoreCoordination {
	return &memoryRestoreCoordination{state: model.RestoreState{
		Version:              model.ContractVersion,
		RestoreID:            "00000000-0000-4000-8000-000000000001",
		DatabaseClusterID:    "internal-rpc-authority-primary",
		BackupManifestDigest: strings.Repeat("0", 64),
		Phase:                "OPEN",
		RestoreEpoch:         1,
		CoordinationRevision: 1,
		AnchorRevision:       1,
		EvidenceDigest:       strings.Repeat("0", 64),
		ExpectedTargets:      map[string]model.RestoreExpectedTarget{"bootstrap": {TargetID: "bootstrap"}},
		Issuances:            map[string]model.RestoreIssuanceRecord{},
		Deliveries:           map[string]model.RestoreDeliveryRecord{},
		Directives:           map[string]model.RestoreDirectiveRecord{},
		ACKs:                 map[string]model.RestoreACKRecord{},
	}}
}

func (store *memoryRestoreCoordination) Prepare(
	_ context.Context,
	command model.PrepareRestoreCommand,
) (model.RestoreState, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.state = model.RestoreState{
		Version:               model.ContractVersion,
		RestoreID:             command.RestoreID,
		DatabaseClusterID:     command.DatabaseClusterID,
		BackupManifestDigest:  command.BackupManifestDigest,
		RecoveryTargetUnix:    command.RecoveryTarget.Unix(),
		Phase:                 "QUIESCING",
		RestoreEpoch:          store.state.RestoreEpoch + 1,
		CoordinationRevision:  store.state.CoordinationRevision + 1,
		AnchorRevision:        store.state.AnchorRevision + 1,
		EvidenceDigest:        command.SemanticDigest,
		PrepareIdempotencyKey: command.IdempotencyKey,
		PrepareSemanticDigest: command.SemanticDigest,
		ExpectedTargets:       command.ExpectedTargets,
		Issuances:             map[string]model.RestoreIssuanceRecord{},
		Deliveries:            map[string]model.RestoreDeliveryRecord{},
		Directives:            map[string]model.RestoreDirectiveRecord{},
		ACKs:                  map[string]model.RestoreACKRecord{},
	}
	return store.state, nil
}

func (store *memoryRestoreCoordination) Load(context.Context) (model.RestoreState, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.state, nil
}

func (store *memoryRestoreCoordination) EnsureIssuance(
	_ context.Context,
	_ string,
	record model.RestoreIssuanceRecord,
) (model.RestoreIssuanceRecord, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if existing, ok := store.state.Issuances[record.TargetID]; ok {
		return existing, nil
	}
	store.state.Issuances[record.TargetID] = record
	return record, nil
}

func (store *memoryRestoreCoordination) RecordDelivery(
	_ context.Context,
	_ string,
	record model.RestoreDeliveryRecord,
) (model.RestoreState, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.state.Deliveries[record.TargetID] = record
	return store.state, nil
}

func (store *memoryRestoreCoordination) SaveDirective(
	_ context.Context,
	_ string,
	record model.RestoreDirectiveRecord,
) (model.RestoreDirectiveRecord, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.state.Directives[record.TargetID] = record
	return record, nil
}

func (store *memoryRestoreCoordination) RecordACK(
	_ context.Context,
	_ string,
	record model.RestoreACKRecord,
) (model.RestoreState, model.RestoreACKRecord, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, existing := range store.state.ACKs {
		if existing.IdempotencyKey == record.IdempotencyKey {
			if existing.SemanticRequestDigest != record.SemanticRequestDigest {
				return model.RestoreState{}, model.RestoreACKRecord{}, repository.ErrReplay
			}
			return store.state, existing, nil
		}
	}
	record.ResultingPhase = "PREPARED"
	store.state.ACKs[record.TargetID] = record
	store.state.Phase = "PREPARED"
	store.state.CoordinationRevision++
	return store.state, record, nil
}

func (store *memoryRestoreCoordination) AuthorizeOperator(
	_ context.Context,
	record model.RestoreOperatorAuthorizationRecord,
) error {
	if store.state.OperatorAuthorizations == nil {
		store.state.OperatorAuthorizations =
			make(map[string]model.RestoreOperatorAuthorizationRecord)
	}
	if existing, ok := store.state.OperatorAuthorizations[record.TokenDigestSHA256]; ok &&
		(existing.FullMethod != record.FullMethod ||
			existing.IdempotencyKey != record.IdempotencyKey ||
			existing.SemanticDigestSHA256 != record.SemanticDigestSHA256) {
		return repository.ErrReplay
	}
	store.state.OperatorAuthorizations[record.TokenDigestSHA256] = record
	return nil
}

func (store *memoryRestoreCoordination) Complete(
	_ context.Context,
	command model.CompleteRestoreCommand,
) (model.RestoreState, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.state.Phase = "COMPLETED"
	store.state.SafeWindowNotBefore = command.Now.Add(40 * time.Second).Unix()
	return store.state, nil
}

func (*memoryRestoreCoordination) CoordinationReady(context.Context) error { return nil }

type memoryRestoreFence struct {
	last model.RestoreState
}

func (fence *memoryRestoreFence) ApplyRestoreFence(
	_ context.Context,
	state model.RestoreState,
) error {
	fence.last = state
	return nil
}

func (fence *memoryRestoreFence) RestoreFenceReady(
	_ context.Context,
	state model.RestoreState,
) error {
	if fence.last.Phase != state.Phase {
		return repository.ErrNotReady
	}
	return nil
}

type memoryRestorePublisher struct {
	calls int
}

func (publisher *memoryRestorePublisher) PublishRoleCredential(
	_ context.Context,
	_ string,
	_ string,
) (model.RestoreDeliveryRecord, error) {
	publisher.calls++
	return model.RestoreDeliveryRecord{
		DeliveryReceiptCompactJWS:  "header.payload.signature",
		RoleCredentialDigestSHA256: strings.Repeat("b", 64),
		CredentialGeneration:       1,
		ACKKeyGeneration:           1,
	}, nil
}

func (*memoryRestorePublisher) PublisherReady(context.Context) error { return nil }
