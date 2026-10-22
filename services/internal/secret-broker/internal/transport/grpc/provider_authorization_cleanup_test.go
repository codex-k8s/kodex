package grpc

import (
	"context"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// authorizationCleanupMaterializer is a programmable metadata/cleanup stub recording typed target,
// recovery identity and call counts; it does not fence or delete real objects.
// authorizationCleanupMaterializer — programmable metadata/cleanup подмена, записывающая typed target,
// recovery identity и счётчики; реальные объекты не ограждает и не удаляет.
type authorizationCleanupMaterializer struct {
	cleanupProviderCredentialMaterializerStub
	target            kubernetesstore.ProviderAuthorizationCleanupTarget
	observation       kubernetesstore.ProviderAuthorizationCleanupObservation
	result            kubernetesstore.ProviderCredentialCleanupResult
	observed, cleaned int
}

// ObserveAuthorizationCleanup records the target and returns configured metadata without polling device
// authorization or executing storage work.
// ObserveAuthorizationCleanup записывает target и возвращает настроенные metadata без device authorization
// polling и storage work.
func (stub *authorizationCleanupMaterializer) ObserveAuthorizationCleanup(_ context.Context, target kubernetesstore.ProviderAuthorizationCleanupTarget) (kubernetesstore.ProviderAuthorizationCleanupObservation, error) {
	stub.target, stub.observed = target, stub.observed+1
	return stub.observation, nil
}

// CleanupAuthorizationWithRecovery records the exact target/origin and returns a programmed receipt
// without enforcing real CAS or owner authority.
// CleanupAuthorizationWithRecovery записывает exact target/origin и возвращает настроенный receipt без
// реальных CAS и owner authority checks.
func (stub *authorizationCleanupMaterializer) CleanupAuthorizationWithRecovery(_ context.Context, target kubernetesstore.ProviderAuthorizationCleanupTarget, recovery kubernetesstore.ProviderCleanupRecoveryIdentity) (kubernetesstore.ProviderCredentialCleanupResult, error) {
	stub.target, stub.cleaned = target, stub.cleaned+1
	stub.recovery = recovery
	return stub.result, nil
}

// TestAuthorizationCleanupHandlerStrictTargetAndProducedReceipt checks pending/absence target forwarding,
// recovery-origin fields and produced descriptors, then rejects mixed/unknown/foreign targets before a
// second materializer call.
// TestAuthorizationCleanupHandlerStrictTargetAndProducedReceipt проверяет pending/absence target
// forwarding, recovery-origin fields и produced descriptors, затем отклоняет mixed/unknown/foreign targets
// до повторного materializer call.
func TestAuthorizationCleanupHandlerStrictTargetAndProducedReceipt(t *testing.T) {
	t.Parallel()
	pending := &cp.ProviderAuthorizationObjectDescriptor{AccountRef: "pacc_cleanup123456", AuthorizationAttemptRef: "pauth_cleanup123456",
		MaterializerAttemptRef: "pmat_metadata123456", Uid: "61000000-0000-4000-8000-000000000001", ResourceVersion: "127"}
	absent := &cp.ProviderAuthorizationAbsenceDescriptor{AccountRef: pending.AccountRef, AuthorizationAttemptRef: pending.AuthorizationAttemptRef, MaterializerAttemptRef: pending.MaterializerAttemptRef}
	produced := &kubernetesstore.ProviderCredentialDescriptor{SecretName: "provider-credential-test", SecretUID: pending.Uid, SecretResourceVersion: "128", ContentSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	for _, kind := range []cp.ProviderCredentialCleanupTargetKind{
		cp.ProviderCredentialCleanupTargetKind_PROVIDER_CREDENTIAL_CLEANUP_TARGET_KIND_AUTHORIZATION_ATTEMPT,
		cp.ProviderCredentialCleanupTargetKind_PROVIDER_CREDENTIAL_CLEANUP_TARGET_KIND_AUTHORIZATION_ABSENCE,
	} {
		request := &cp.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest{TaskRef: "pcct_cleanup123456", AccountRef: pending.AccountRef, LeaseGeneration: 7, TargetKind: kind}
		request.RecoveryIdentity = &cp.ProviderCredentialCleanupRecoveryIdentity{TaskRef: "pcct_origin123456", LeaseGeneration: 1, LegacyLastGeneration: 4}
		if kind == cp.ProviderCredentialCleanupTargetKind_PROVIDER_CREDENTIAL_CLEANUP_TARGET_KIND_AUTHORIZATION_ATTEMPT {
			request.PendingObject = pending
		} else {
			request.AbsentObject = absent
		}
		stub := &authorizationCleanupMaterializer{result: kubernetesstore.ProviderCredentialCleanupResult{TerminalReceipt: "synthetic-exact-receipt", ProducedCredential: produced}}
		server := &Server{providerCredentials: stub}
		response, err := server.CleanupProviderCredential(t.Context(), request)
		if err != nil || stub.cleaned != 1 || stub.recovery.TaskRef != request.RecoveryIdentity.TaskRef || stub.recovery.Generation != 1 || stub.recovery.LegacyLastGeneration != 4 || stub.target.TaskRef != request.TaskRef || stub.target.AccountRef != request.AccountRef ||
			stub.target.Generation != request.LeaseGeneration || stub.target.MaterializerAttemptRef != pending.MaterializerAttemptRef ||
			response.GetTerminalReceipt() != stub.result.TerminalReceipt || !proto.Equal(response.GetProducedCredential(), providerCredentialDescriptor(produced)) {
			t.Fatalf("typed cleanup target or produced receipt lost: %v", err)
		}
		for _, mutate := range []func(*cp.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest){
			func(v *cp.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest) {
				v.RecoveryIdentity = nil
			},
			func(v *cp.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest) {
				v.RecoveryIdentity.TaskRef = "foreign"
			},
			func(v *cp.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest) {
				v.RecoveryIdentity.LeaseGeneration = 0
			},
			func(v *cp.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest) {
				v.RecoveryIdentity.LegacyLastGeneration = 33
			},
			func(v *cp.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest) {
				v.RecoveryIdentity.LeaseGeneration = 2
			},
			func(v *cp.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest) { v.TargetKind = 99 },
			func(v *cp.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest) { v.TargetKind = 0 },
			func(v *cp.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest) {
				v.Credential = &cp.ProviderCredentialDescriptor{}
			},
			func(v *cp.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest) {
				v.AccountRef = "pacc_other123456"
			},
			func(v *cp.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest) {
				v.PendingObject, v.AbsentObject = pending, absent
			},
			func(v *cp.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest) {
				v.PendingObject, v.AbsentObject = nil, nil
			},
		} {
			wrong := proto.Clone(request).(*cp.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest)
			mutate(wrong)
			if _, err := server.CleanupProviderCredential(t.Context(), wrong); status.Code(err) != codes.InvalidArgument || stub.cleaned != 1 {
				t.Fatal("mixed, unknown or owner-mismatched cleanup target reached materializer")
			}
		}
	}
}

// TestAuthorizationCleanupMetadataHandlerNeverReturnsPollingState exercises present/absent/fenced metadata
// and requires exclusive object descriptors without poll state, account diagnostics or caller UID,
// rejecting mixed/unknown poll modes.
// TestAuthorizationCleanupMetadataHandlerNeverReturnsPollingState проверяет present/absent/fenced metadata
// и exclusive object descriptors без poll state, account diagnostics и caller UID, отклоняя mixed/unknown
// poll modes.
func TestAuthorizationCleanupMetadataHandlerNeverReturnsPollingState(t *testing.T) {
	t.Parallel()
	target := kubernetesstore.ProviderAuthorizationCleanupTarget{TaskRef: "pcct_cleanup123456", AccountRef: "pacc_cleanup123456",
		AuthorizationAttemptRef: "pauth_cleanup123456", MaterializerAttemptRef: "pmat_metadata123456", UID: "61000000-0000-4000-8000-000000000001", ResourceVersion: "127", Generation: 7}
	for _, state := range []string{kubernetesstore.ProviderAuthorizationPresent, kubernetesstore.ProviderAuthorizationAbsent, kubernetesstore.ProviderAuthorizationFenced} {
		stub := &authorizationCleanupMaterializer{observation: kubernetesstore.ProviderAuthorizationCleanupObservation{State: state, Target: target}}
		server := &Server{providerCredentials: stub}
		request := &cp.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationRequest{Mode: cp.ProviderAuthorizationObservationMode_PROVIDER_AUTHORIZATION_OBSERVATION_MODE_METADATA_ONLY,
			TaskRef: target.TaskRef, AccountRef: target.AccountRef, AuthorizationAttemptRef: target.AuthorizationAttemptRef, MaterializerAttemptRef: target.MaterializerAttemptRef, LeaseGeneration: target.Generation}
		response, err := server.ObserveDeviceAuthorization(t.Context(), request)
		if err != nil || stub.observed != 1 || response.GetState() != 0 || response.GetExternalAccountMasked() != "" || response.GetSafeFailureCode() != "" ||
			(response.GetPendingObject() == nil) == (response.GetAbsentObject() == nil) || stub.target.TaskRef != target.TaskRef || stub.target.Generation != target.Generation || stub.target.UID != "" {
			t.Fatalf("metadata request crossed polling boundary: %v", err)
		}
		for _, mode := range []cp.ProviderAuthorizationObservationMode{0, 99, cp.ProviderAuthorizationObservationMode_PROVIDER_AUTHORIZATION_OBSERVATION_MODE_POLL} {
			request.Mode = mode
			if _, err := server.ObserveDeviceAuthorization(t.Context(), request); status.Code(err) != codes.InvalidArgument || stub.observed != 1 {
				t.Fatal("unknown or mixed observation mode was accepted")
			}
		}
	}
}
