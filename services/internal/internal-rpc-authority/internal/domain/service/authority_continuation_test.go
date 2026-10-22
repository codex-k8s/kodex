package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/failure"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

// TestIssueContinuationInheritsVerifiedRootAndReservesChild signs a local synthetic parent and checks inherited authority/authentication, parent-capped expiry, captured reservation/registration and rejection after uncertain registration. Real ES256 runs, but the capture store does not enforce durable parent acceptance or replay.
// TestIssueContinuationInheritsVerifiedRootAndReservesChild подписывает локального синтетического родителя и проверяет наследование полномочий и аутентификации, ограничение срока родителем, захваченные reservation и регистрацию и отказ при неопределённой регистрации. ES256 настоящий, но capture store не обеспечивает устойчивое принятие родителя и replay.
func TestIssueContinuationInheritsVerifiedRootAndReservesChild(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	parentKey, err := internalrpcauth.GenerateES256Key("control-api-gateway-g1")
	if err != nil {
		t.Fatalf("создать parent key: %v", err)
	}
	childKey, err := internalrpcauth.GenerateES256Key("stt-tts-service-g1")
	if err != nil {
		t.Fatalf("создать child key: %v", err)
	}
	proofKey, err := internalrpcauth.GenerateES256Key("control-plane-proof-g1")
	if err != nil {
		t.Fatalf("создать proof key: %v", err)
	}

	const (
		// parentOperation selects the synthetic upstream operation; no STT runs.
		// parentOperation выбирает синтетическую upstream-операцию без запуска STT.
		parentOperation = "platform.stt.transcribe"
		// parentMethod pins the parent RPC spelling in local signed claims.
		// parentMethod закрепляет имя RPC родителя в локальных подписанных claims.
		parentMethod = "/stt.v1.SpeechToTextService/Transcribe"
		// childOperation selects the configured continuation edge in the fixture.
		// childOperation выбирает настроенное ребро continuation в эталоне.
		childOperation = "platform.stt.policy.resolve"
		// childMethod pins the child RPC without calling its real service.
		// childMethod закрепляет RPC ребёнка без вызова настоящего сервиса.
		childMethod = "/stt.v1.TranscriptionPolicyProjectionService/ResolveTranscriptionPolicy"
		// gatewaySPIFFE is the synthetic parent's signed caller identity.
		// gatewaySPIFFE — подписанная идентичность caller синтетического родителя.
		gatewaySPIFFE = "spiffe://kodex.local/ns/kodex-system/sa/control-api-gateway"
		// sttSPIFFE is the parent target and child issuer/caller in the fixture.
		// sttSPIFFE — цель родителя и issuer/caller ребёнка в эталоне.
		sttSPIFFE = "spiffe://kodex.local/ns/kodex-system/sa/stt-tts-service"
		// controlSPIFFE pins the synthetic child target, not a live peer.
		// controlSPIFFE закрепляет синтетическую цель ребёнка, а не живой peer.
		controlSPIFFE = "spiffe://kodex.local/ns/kodex-system/sa/control-plane"
		// sttAudience binds the signed parent to the local upstream profile.
		// sttAudience связывает подписанного родителя с локальным upstream-профилем.
		sttAudience = "urn:kodex:internal-rpc:stt-tts-service"
		// controlAudience binds the generated child to the downstream profile.
		// controlAudience связывает созданного ребёнка с downstream-профилем.
		controlAudience = "urn:kodex:internal-rpc:control-plane"
		// parentJTI fixes one local parent identity for lineage and digest assertions.
		// parentJTI задаёт локальную идентичность родителя для проверок lineage и digest.
		parentJTI = "31f5dff2-73e8-4053-aa60-b2916705ce43"
		// requestID pins the child derivation input across simulated repeats.
		// requestID закрепляет вход вывода ребёнка при имитируемых повторах.
		requestID = "1cf1a64f-8563-4d45-9ef9-33c30679de48"
		// correlationID is trace linkage and does not confer parent authority.
		// correlationID связывает трассировку и не даёт полномочий родителя.
		correlationID = "7d07bc69-bf60-403e-aa05-d5066848369b"
	)
	requestDigest := strings.Repeat("c", 64)
	policyDigest := strings.Repeat("a", 64)
	profile := func(mode string) model.RequestProfile {
		return model.RequestProfile{
			Mode: mode, Resource: model.ProfileBindingRequired,
			Version: model.ProfileBindingRequired, Attempt: model.ProfileBindingForbidden,
			Idempotency: model.ProfileBindingRequired,
		}
	}
	parentBinding := model.OperationBinding{
		OperationID: parentOperation, CallerWorkloadID: "control-api-gateway", CallerSPIFFEID: gatewaySPIFFE,
		Issuer: gatewaySPIFFE, TargetWorkloadID: "stt-tts-service", TargetSPIFFEID: sttSPIFFE,
		Audience: sttAudience, FullMethod: parentMethod, Permission: "speech.transcribe",
		AuthorityProofIssuer: controlSPIFFE, AuthorityProofAudience: "urn:kodex:internal-rpc-authority-issuer:control-api-gateway",
		AuthoritySources: []string{"DOMAIN_STATE"}, ProjectRequired: true, TokenTTLSeconds: 30,
		RequestProfile: profile(model.RequestBindingStream),
	}
	childBinding := model.OperationBinding{
		OperationID: childOperation, CallerWorkloadID: "stt-tts-service", CallerSPIFFEID: sttSPIFFE,
		Issuer: sttSPIFFE, TargetWorkloadID: "control-plane", TargetSPIFFEID: controlSPIFFE,
		Audience: controlAudience, FullMethod: childMethod, Permission: childOperation,
		AuthoritySources: []string{"DOMAIN_STATE"}, ProjectRequired: true, TokenTTLSeconds: 30,
		RequestProfile: profile(model.RequestBindingUnary), Continuation: &model.ContinuationProfile{
			ParentOperationID: parentOperation, ParentFullMethod: parentMethod,
		},
	}
	policy := model.PolicySnapshot{
		Version: model.ContractVersion, AuthorityABIVersion: model.AuthorityABIVersion,
		DefaultDecision: "DENY", TokenTTLSeconds: 30, AllowedClockSkewSeconds: 5,
		SourceRevision: 7, SourceDigestSHA256: policyDigest, PredecessorRevision: 6,
		PredecessorDigestSHA256: strings.Repeat("9", 64), KeySetRevision: 7,
		PolicyRevision: 43, SignerGeneration: 1, Issuer: sttSPIFFE,
		SignerKeyID: childKey.KeyID, OperationBindings: []model.OperationBinding{parentBinding, childBinding},
	}
	keyRecord := func(key internalrpcauth.ES256Key, issuer, purpose, audience string) VerificationKeyRecord {
		return VerificationKeyRecord{
			Key: key.PublicOnly(), Issuer: issuer, Generation: 1, Status: keyStatusCurrent,
			Purpose: purpose, Audiences: map[string]struct{}{audience: {}},
			NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		}
	}
	store := &continuationCaptureStore{}
	authority, err := NewAuthority(policy, KeyMaterial{
		SigningKey: childKey,
		VerificationKeys: map[string]VerificationKeyRecord{
			parentKey.KeyID: keyRecord(parentKey, gatewaySPIFFE, contextKeyPurpose, sttAudience),
			childKey.KeyID:  keyRecord(childKey, sttSPIFFE, contextKeyPurpose, controlAudience),
		},
		ProofKeys: map[string]VerificationKeyRecord{
			proofKey.KeyID: keyRecord(proofKey, controlSPIFFE, proofKeyPurpose, "urn:kodex:internal-rpc-authority-issuer:stt-tts-service"),
		},
	}, store)
	if err != nil {
		t.Fatalf("создать authority: %v", err)
	}
	authority.now = func() time.Time { return now }
	if err := authority.ActivateSnapshot(t.Context(), "f946a874-c9bd-4a71-98d5-2d4369e8f3f4"); err != nil {
		t.Fatalf("активировать snapshot: %v", err)
	}

	identity := func(id, reference string) model.Identity {
		return model.Identity{ID: id, Provenance: model.Provenance{
			Source: "DOMAIN_STATE", Reference: reference, Revision: 3, DigestSHA256: strings.Repeat("b", 64),
		}}
	}
	rootAuthority := model.Authority{
		ActorKind: "HUMAN",
		Actor:     identity("a02f560c-cdf7-44a8-b2fb-cbd38530ca23", "39bb8e2c-d5e9-4acb-9b2a-5f3ae598f43e"),
		Tenant:    identity("9c8da0d4-55f3-47b8-90f1-8ba0e607044f", "b9160544-b644-4dd2-a676-64baedf37afb"),
	}
	project := identity("9229da3f-622b-4222-84bd-b80eaf3845e4", "6c64dcaa-293d-4814-80d5-ee8079cceab5")
	rootAuthority.Project = &project
	parentClaims := model.AuthorizationClaims{
		Version: model.ContractVersion, AuthorityABIVersion: model.AuthorityABIVersion,
		Issuer: gatewaySPIFFE, Audience: sttAudience, Subject: gatewaySPIFFE,
		Caller:     model.Workload{WorkloadID: "control-api-gateway", SPIFFEID: gatewaySPIFFE},
		Target:     model.Workload{WorkloadID: "stt-tts-service", SPIFFEID: sttSPIFFE},
		FullMethod: parentMethod, OperationID: parentOperation, Permission: "speech.transcribe",
		Authority: rootAuthority, JTI: parentJTI, IssuedAt: now.Unix(), NotBefore: now.Unix(),
		ExpiresAt: now.Add(30 * time.Second).Unix(), ReplayMode: model.ReplayModeOneTime,
		SourceRevision: 7, SourceDigestSHA256: policyDigest, KeySetRevision: 7,
		PolicyRevision: 43, SignerGeneration: 1, CallerCredentialRevision: 12,
		CredentialAuthentication: &model.CredentialAuthentication{AuthenticatedAt: now.Add(-time.Minute).Unix(), ACR: "urn:kodex:mfa", AMR: []string{"pwd", "otp"}},
		RequestBindingMode:       model.RequestBindingStream,
	}
	parentCompact, err := internalrpcauth.SignCanonicalJSON(parentClaims, parentKey, internalrpcauth.ProtectedHeaderExpectation{
		Type: internalrpcauth.AuthorizationContextProtectedType, KeyID: parentKey.KeyID,
	})
	if err != nil {
		t.Fatalf("подписать parent context: %v", err)
	}
	authority.now = func() time.Time { return now.Add(5 * time.Second) }
	// Загруженная parent policy не разрешает STT подписывать вызовы за gateway.
	if _, _, err := authority.Issue(t.Context(), parentOperation, "synthetic-proof", requestDigest); !failure.IsKind(err, failure.OperationNotAllowed) {
		t.Fatal("foreign issuer operation was not rejected before proof processing")
	}

	childCompact, childClaims, err := authority.IssueContinuation(
		t.Context(), childOperation, parentCompact, requestID, correlationID, requestDigest,
	)
	if err != nil {
		t.Fatalf("выпустить continuation: %v", err)
	}
	if childClaims.JTI != "0d651862-d9ad-5210-8972-6a937ff788c5" {
		t.Fatalf("continuation JTI encoding changed: %s", childClaims.JTI)
	}
	if childClaims.JTI != deterministicContinuationJTI(parentJTI, childOperation, requestID) ||
		childClaims.Continuation == nil || childClaims.Continuation.RootJTI != parentJTI ||
		childClaims.Continuation.ParentJTI != parentJTI || childClaims.Continuation.RequestID != requestID ||
		childClaims.Continuation.CorrelationID != correlationID ||
		!reflect.DeepEqual(childClaims.Authority, rootAuthority) ||
		!reflect.DeepEqual(childClaims.CredentialAuthentication, parentClaims.CredentialAuthentication) {
		t.Fatalf("continuation потеряла server-owned lineage: %+v", childClaims)
	}
	if childClaims.ExpiresAt != parentClaims.ExpiresAt || childClaims.ExpiresAt-childClaims.IssuedAt != 25 {
		t.Fatalf("continuation не ограничена parent expiry: iat=%d exp=%d", childClaims.IssuedAt, childClaims.ExpiresAt)
	}
	parentDigest := sha256.Sum256([]byte(parentCompact))
	if store.calls != 1 || store.parent.ScopeID != "stt-tts-service" || store.parent.JTI != parentJTI ||
		store.parent.Digest != hex.EncodeToString(parentDigest[:]) || store.child.JTI != childClaims.JTI {
		t.Fatalf("continuation reservation не связана с parent/child: parent=%+v child=%+v", store.parent, store.child)
	}
	if store.binding.JTI != childClaims.JTI || store.binding.ParentJTI != parentJTI || store.binding.ParentDigest != hex.EncodeToString(parentDigest[:]) || store.binding.CallerWorkloadID != "stt-tts-service" || store.binding.TargetWorkloadID != "control-plane" {
		t.Fatal("durable continuation binding lost parent authority")
	}
	if _, err := authority.Verify(t.Context(), childCompact, childMethod, sttSPIFFE, requestDigest); err != nil {
		t.Fatalf("проверить child context: %v", err)
	}
	store.registrationError = errors.New("database response lost")
	if compact, _, err := authority.IssueContinuation(t.Context(), childOperation, parentCompact, requestID, correlationID, requestDigest); err == nil || compact != "" {
		t.Fatal("uncertain registration returned a usable context")
	}
}

// continuationCaptureStore records parent/child reservations and issued bindings and can fail registration for the local continuation unit. Other methods return synthetic success and do not implement database transactions or replay protection.
// continuationCaptureStore записывает reservations родителя и ребёнка и выданные привязки и может отклонить регистрацию в локальном unit continuation. Остальные методы синтетически успешны и не реализуют транзакции БД и replay protection.
type continuationCaptureStore struct {
	parent, child     repository.Reservation
	calls             int
	binding           repository.IssuedContextBinding
	registrationError error
}

// Reserve succeeds without storing or rejecting a reused proof in this fixture; it is not evidence of one-use proof enforcement.
// Reserve в этом эталоне успешен без сохранения proof и отклонения повтора; одноразовость proof не доказывается.
func (*continuationCaptureStore) Reserve(context.Context, repository.Reservation) error { return nil }

// ReserveContinuation captures the exact proposed parent/child values and increments a counter without checking parent acceptance, expiry or duplicate child identity.
// ReserveContinuation сохраняет точные предложенные значения родителя и ребёнка и увеличивает счётчик без проверки принятия родителя, срока и повторной идентичности ребёнка.
func (store *continuationCaptureStore) ReserveContinuation(
	_ context.Context,
	parent repository.Reservation,
	child repository.Reservation,
) error {
	store.parent, store.child, store.calls = parent, child, store.calls+1
	return nil
}

// ActivateSnapshot accepts the fixture snapshot without writing a durable watermark, validating a receipt or executing independent readback.
// ActivateSnapshot принимает эталонный снимок без записи устойчивой отметки, проверки квитанции и независимого readback.
func (*continuationCaptureStore) ActivateSnapshot(context.Context, repository.SnapshotState) error {
	return nil
}

// AcceptVerification always succeeds in the capture store; the enclosing unit exercises local token verification but not atomic snapshot acceptance or replay reservation.
// AcceptVerification всегда успешен в capture store; основной unit проверяет локальную проверку токена, но не атомарное принятие снимка и replay reservation.
func (*continuationCaptureStore) AcceptVerification(context.Context, repository.SnapshotState, repository.Reservation) error {
	return nil
}

// Ready returns synthetic success without opening a database connection, probing RLS or testing a real replay write path.
// Ready возвращает синтетический успех без подключения к БД, проверки RLS и реального пути записи replay.
func (*continuationCaptureStore) Ready(context.Context, repository.SnapshotState) error { return nil }

// Close is a no-op because this fixture owns only in-memory captured values and no pool, connection or worker lifecycle.
// Close ничего не делает: эталон владеет лишь захваченными значениями в памяти, а не пулом, соединением и жизненным циклом worker.
func (*continuationCaptureStore) Close() {}

// Freshness returns a synthetic thirty-second receipt window using real wall-clock observation time and the supplied receipt ID. It does not establish database-clock provenance or persist a lease.
// Freshness возвращает синтетическое тридцатисекундное окно квитанции с реальными настенными часами наблюдения и переданным ID. Происхождение часов БД не доказывается и аренда не сохраняется.
func (*continuationCaptureStore) Freshness(_ context.Context, state repository.SnapshotState) (repository.SnapshotFreshness, error) {
	now := time.Now()
	return repository.SnapshotFreshness{ReceiptID: state.AttestationReceiptID, ObservedAt: now, ValidUntil: now.Add(30 * time.Second)}, nil
}

// RegisterIssuedContext captures the binding then returns the configured fixture error, modeling an uncertain registration response without committing anything to a database.
// RegisterIssuedContext сохраняет привязку в памяти и возвращает настроенную ошибку эталона, моделируя неопределённый ответ регистрации без фиксации в БД.
func (store *continuationCaptureStore) RegisterIssuedContext(_ context.Context, _ repository.SnapshotState, binding repository.IssuedContextBinding) error {
	store.binding = binding
	return store.registrationError
}
