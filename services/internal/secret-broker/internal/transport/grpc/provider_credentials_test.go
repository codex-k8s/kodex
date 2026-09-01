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

type cleanupProviderCredentialMaterializerStub struct {
	taskRef      string
	accountRef   string
	generation   int64
	descriptor   kubernetesstore.ProviderCredentialDescriptor
	receipt      string
	err          error
	cleanupCalls int
}

func (stub *cleanupProviderCredentialMaterializerStub) Check(context.Context) error { return nil }

func (stub *cleanupProviderCredentialMaterializerStub) StartDeviceAuthorization(
	context.Context,
	string,
	string,
) (providercredential.DeviceAuthorization, error) {
	return providercredential.DeviceAuthorization{}, errors.New("unexpected device authorization start")
}

func (stub *cleanupProviderCredentialMaterializerStub) ObserveDeviceAuthorization(
	context.Context,
	string,
) (kubernetesstore.ProviderAuthorizationAttempt, error) {
	return kubernetesstore.ProviderAuthorizationAttempt{}, errors.New("unexpected device authorization observation")
}

func (stub *cleanupProviderCredentialMaterializerStub) MaterializeAPIKey(
	context.Context,
	string,
	string,
	[]byte,
) (kubernetesstore.ProviderCredentialDescriptor, string, error) {
	return kubernetesstore.ProviderCredentialDescriptor{}, "", errors.New("unexpected API key materialization")
}

func (stub *cleanupProviderCredentialMaterializerStub) Discard(
	context.Context,
	providercredential.DiscardMaterialization,
) error {
	return errors.New("unexpected materialization discard")
}

func (stub *cleanupProviderCredentialMaterializerStub) CleanupProviderCredential(
	_ context.Context,
	taskRef, accountRef string,
	generation int64,
	descriptor kubernetesstore.ProviderCredentialDescriptor,
) (string, error) {
	stub.cleanupCalls++
	stub.taskRef = taskRef
	stub.accountRef = accountRef
	stub.generation = generation
	stub.descriptor = descriptor
	return stub.receipt, stub.err
}

func TestCleanupProviderCredentialHandlerPreservesExactTarget(t *testing.T) {
	t.Parallel()
	const receipt = "provider-credential-cleanup:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	stub := &cleanupProviderCredentialMaterializerStub{receipt: receipt}
	server := &Server{providerCredentials: stub}
	request := &controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest{
		TaskRef: "pcct_cleanup1234", AccountRef: "pacc_cleanup1234", LeaseGeneration: 7,
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
				&controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest{},
			)
			if status.Code(err) != test.code || stub.cleanupCalls != 1 {
				t.Fatalf("cleanup error code = %s, want %s; calls=%d", status.Code(err), test.code, stub.cleanupCalls)
			}
		})
	}
}

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
