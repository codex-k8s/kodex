package grpc

import (
	"context"
	"errors"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/providercredential"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// cleanupProviderCredentialMaterializerStub records credential-cleanup inputs and returns configured
// receipt/error while unused provider operations deliberately fail; it is not a storage or authority
// implementation.
// cleanupProviderCredentialMaterializerStub записывает credential-cleanup inputs и возвращает настроенные
// receipt/error, намеренно отклоняя unused provider operations; storage и authority не реализует.
type cleanupProviderCredentialMaterializerStub struct {
	ProviderCredentialMaterializer
	taskRef      string
	accountRef   string
	generation   int64
	descriptor   kubernetesstore.ProviderCredentialDescriptor
	receipt      string
	err          error
	cleanupCalls int
	recovery     kubernetesstore.ProviderCleanupRecoveryIdentity
}

// Check returns synthetic readiness without probing provider, Kubernetes or credential state.
// Check возвращает synthetic readiness без проверки provider, Kubernetes и credential state.
func (stub *cleanupProviderCredentialMaterializerStub) Check(context.Context) error { return nil }

// ObserveModelCatalog refuses unexpected catalog access in cleanup-only fixtures.
// ObserveModelCatalog отклоняет неожиданный catalog access в cleanup-only fixtures.
func (stub *cleanupProviderCredentialMaterializerStub) ObserveModelCatalog(context.Context, string, kubernetesstore.ProviderCredentialDescriptor, string) (providercredential.ModelCatalog, error) {
	return providercredential.ModelCatalog{}, errors.New("unexpected catalog observation")
}

// StartDeviceAuthorization returns an unexpected-call error without starting a login flow.
// StartDeviceAuthorization возвращает unexpected-call error без запуска login flow.
func (stub *cleanupProviderCredentialMaterializerStub) StartDeviceAuthorization(
	context.Context,
	string,
	string,
) (providercredential.DeviceAuthorization, error) {
	return providercredential.DeviceAuthorization{}, errors.New("unexpected device authorization start")
}

// ObserveDeviceAuthorization rejects accidental polling rather than mutating or inspecting a real
// authorization attempt.
// ObserveDeviceAuthorization отклоняет случайный polling без изменения и чтения реальной authorization
// attempt.
func (stub *cleanupProviderCredentialMaterializerStub) ObserveDeviceAuthorization(
	context.Context,
	string,
) (kubernetesstore.ProviderAuthorizationAttempt, error) {
	return kubernetesstore.ProviderAuthorizationAttempt{}, errors.New("unexpected device authorization observation")
}

// MaterializeAPIKey refuses unexpected key handling and returns no credential or account label.
// MaterializeAPIKey отклоняет неожиданную обработку ключа без возврата credential и account label.
func (stub *cleanupProviderCredentialMaterializerStub) MaterializeAPIKey(
	context.Context,
	string,
	string,
	[]byte,
) (kubernetesstore.ProviderCredentialDescriptor, string, error) {
	return kubernetesstore.ProviderCredentialDescriptor{}, "", errors.New("unexpected API key materialization")
}

// Discard refuses unrelated materialization deletion so cleanup tests cannot pass through an alternate
// destructive path.
// Discard отклоняет постороннее materialization deletion, чтобы cleanup tests не проходили через alternate
// destructive path.
func (stub *cleanupProviderCredentialMaterializerStub) Discard(
	context.Context,
	providercredential.DiscardMaterialization,
) error {
	return errors.New("unexpected materialization discard")
}

// CleanupProviderCredentialWithRecovery counts and records task/account/generation/exact
// descriptor/origin, then returns the configured synthetic receipt/error without deleting anything.
// CleanupProviderCredentialWithRecovery учитывает и записывает task/account/generation/exact
// descriptor/origin, затем возвращает настроенные synthetic receipt/error без удаления.
func (stub *cleanupProviderCredentialMaterializerStub) CleanupProviderCredentialWithRecovery(
	_ context.Context,
	taskRef, accountRef string,
	generation int64,
	descriptor kubernetesstore.ProviderCredentialDescriptor,
	recovery kubernetesstore.ProviderCleanupRecoveryIdentity,
) (kubernetesstore.ProviderCredentialCleanupResult, error) {
	stub.cleanupCalls++
	stub.recovery = recovery
	stub.taskRef = taskRef
	stub.accountRef = accountRef
	stub.generation = generation
	stub.descriptor = descriptor
	return kubernetesstore.ProviderCredentialCleanupResult{TerminalReceipt: stub.receipt}, stub.err
}

// TestCleanupProviderCredentialHandlerPreservesExactTarget requires exactly one materializer call and
// unchanged task/account/generation/object pins plus returned receipt, using a synthetic owner-origin
// identity.
// TestCleanupProviderCredentialHandlerPreservesExactTarget требует один materializer call, неизменные
// task/account/generation/object pins и returned receipt с synthetic owner-origin identity.
func TestCleanupProviderCredentialHandlerPreservesExactTarget(t *testing.T) {
	t.Parallel()
	// receipt is a fixed synthetic terminal string for forwarding assertions, not an actual committed owner receipt.
	// receipt — fixed synthetic terminal string для forwarding assertions, не реальный committed owner receipt.
	const receipt = "provider-credential-cleanup:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	stub := &cleanupProviderCredentialMaterializerStub{receipt: receipt}
	server := &Server{providerCredentials: stub}
	request := &controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest{
		TaskRef: "pcct_cleanup1234", AccountRef: "pacc_cleanup1234", LeaseGeneration: 7,
		RecoveryIdentity: &controlplanev1.ProviderCredentialCleanupRecoveryIdentity{TaskRef: "pcct_origin1234", LeaseGeneration: 2},
		TargetKind:       controlplanev1.ProviderCredentialCleanupTargetKind_PROVIDER_CREDENTIAL_CLEANUP_TARGET_KIND_CREDENTIAL,
		Credential: &controlplanev1.ProviderCredentialDescriptor{
			SecretName:            "provider-credential-cleanup",
			SecretUid:             "61000000-0000-4000-8000-000000000002",
			SecretResourceVersion: "cleanup-7",
			ContentSha256:         "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
	}
	response, err := server.CleanupProviderCredential(context.Background(), request)
	if err != nil || response.GetTerminalReceipt() != receipt {
		t.Fatalf("cleanup response: response=%#v err=%v", response, err)
	}
	if stub.cleanupCalls != 1 || stub.taskRef != request.GetTaskRef() || stub.accountRef != request.GetAccountRef() ||
		stub.generation != request.GetLeaseGeneration() || stub.descriptor.SecretName != request.GetCredential().GetSecretName() ||
		stub.descriptor.SecretUID != request.GetCredential().GetSecretUid() ||
		stub.descriptor.SecretResourceVersion != request.GetCredential().GetSecretResourceVersion() ||
		stub.descriptor.ContentSHA256 != request.GetCredential().GetContentSha256() {
		t.Fatalf("cleanup handler changed exact target: %#v", stub)
	}
}

// TestCleanupProviderCredentialHandlerClassifiesErrors checks invalid/conflict/arbitrary failure status
// mapping after one stub cleanup call, not live Kubernetes error behavior.
// TestCleanupProviderCredentialHandlerClassifiesErrors проверяет invalid/conflict/arbitrary failure status
// mapping после одного stub cleanup call, не live Kubernetes error behavior.
func TestCleanupProviderCredentialHandlerClassifiesErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		code codes.Code
	}{
		{name: "invalid", err: providercredential.ErrInvalidInput, code: codes.InvalidArgument},
		{name: "conflict", err: kubernetesstore.ErrProviderCredentialCleanupConflict, code: codes.FailedPrecondition},
		{name: "temporary API failure", err: errors.New("temporary Kubernetes API failure"), code: codes.Unavailable},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			stub := &cleanupProviderCredentialMaterializerStub{err: test.err}
			server := &Server{providerCredentials: stub}
			_, err := server.CleanupProviderCredential(
				context.Background(),
				&controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest{
					TaskRef: "pcct_cleanup1234", LeaseGeneration: 7,
					RecoveryIdentity: &controlplanev1.ProviderCredentialCleanupRecoveryIdentity{TaskRef: "pcct_origin1234", LeaseGeneration: 2},
					TargetKind:       controlplanev1.ProviderCredentialCleanupTargetKind_PROVIDER_CREDENTIAL_CLEANUP_TARGET_KIND_CREDENTIAL,
					Credential:       &controlplanev1.ProviderCredentialDescriptor{},
				},
			)
			if status.Code(err) != test.code || stub.cleanupCalls != 1 {
				t.Fatalf("cleanup error code = %s, want %s; calls=%d", status.Code(err), test.code, stub.cleanupCalls)
			}
		})
	}
}

// TestCleanupProviderCredentialHandlerRequiresConfiguredMaterializer requires Unavailable before cleanup
// when no provider capability is installed.
// TestCleanupProviderCredentialHandlerRequiresConfiguredMaterializer требует Unavailable до cleanup, если
// provider capability не установлена.
func TestCleanupProviderCredentialHandlerRequiresConfiguredMaterializer(t *testing.T) {
	t.Parallel()
	_, err := (&Server{}).CleanupProviderCredential(
		context.Background(),
		&controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest{},
	)
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("missing materializer code = %s, want Unavailable", status.Code(err))
	}
}
