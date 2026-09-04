package providercredentialclient

import (
	"context"
	"errors"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/grpc"
)

const (
	cleanupTaskRef    = "pcct_61000000-0000-4000-8000-000000000001"
	cleanupAccountRef = "pacc_cleanup_Account9Z"
	cleanupReceipt    = "provider-credential-cleanup:61000000-0000-4000-8000-000000000001:g7"
)

var cleanupCredential = entity.ProviderCredentialDescriptor{
	SecretName: "provider-credential-1", SecretUID: "61000000-0000-4000-8000-000000000002",
	SecretResourceVersion: "cleanup-7", ContentSHA256: strings.Repeat("a", 64),
}

type providerCredentialClientStub struct {
	controlplanev1.ProviderCredentialMaterializerServiceClient
	response *controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialResponse
	err      error
	request  *controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest
	calls    int
}

func (client *providerCredentialClientStub) CleanupProviderCredential(
	_ context.Context,
	request *controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest,
	_ ...grpc.CallOption,
) (*controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialResponse, error) {
	client.calls++
	client.request = request
	return client.response, client.err
}

func cleanupClient(t *testing.T, stub *providerCredentialClientStub) *Client {
	t.Helper()
	client, err := New(&controlplaneclient.Client{ProviderCredentials: stub})
	if err != nil {
		t.Fatalf("construct provider credential client: %v", err)
	}
	return client
}

func TestCleanupProviderCredentialSendsExactDescriptorAndReturnsReceipt(t *testing.T) {
	t.Parallel()
	stub := &providerCredentialClientStub{
		response: &controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialResponse{
			TerminalReceipt: cleanupReceipt,
		},
	}
	receipt, err := cleanupClient(t, stub).CleanupProviderCredential(
		context.Background(), cleanupTaskRef, cleanupAccountRef, 7, cleanupCredential,
	)
	if err != nil || receipt != cleanupReceipt {
		t.Fatalf("cleanup result: receipt=%q err=%v", receipt, err)
	}
	request := stub.request
	if stub.calls != 1 || request.GetTaskRef() != cleanupTaskRef || request.GetAccountRef() != cleanupAccountRef ||
		request.GetLeaseGeneration() != 7 || request.GetCredential().GetSecretName() != cleanupCredential.SecretName ||
		request.GetCredential().GetSecretUid() != cleanupCredential.SecretUID ||
		request.GetCredential().GetSecretResourceVersion() != cleanupCredential.SecretResourceVersion ||
		request.GetCredential().GetContentSha256() != cleanupCredential.ContentSHA256 {
		t.Fatalf("cleanup request does not preserve exact task: %#v", request)
	}
}

func TestCleanupProviderCredentialPropagatesRPCError(t *testing.T) {
	t.Parallel()
	want := errors.New("cleanup unavailable")
	stub := &providerCredentialClientStub{err: want}
	_, err := cleanupClient(t, stub).CleanupProviderCredential(
		context.Background(), cleanupTaskRef, cleanupAccountRef, 1, cleanupCredential,
	)
	if !errors.Is(err, want) || stub.calls != 1 {
		t.Fatalf("RPC error was not propagated: calls=%d err=%v", stub.calls, err)
	}
}

func TestCleanupProviderCredentialRejectsInvalidRequestBeforeRPC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		taskRef    string
		accountRef string
		generation int64
		credential entity.ProviderCredentialDescriptor
	}{
		{name: "task prefix", taskRef: "task_cleanup", accountRef: cleanupAccountRef, generation: 1, credential: cleanupCredential},
		{name: "task suffix", taskRef: "pcct_", accountRef: cleanupAccountRef, generation: 1, credential: cleanupCredential},
		{name: "task short suffix", taskRef: "pcct_short", accountRef: cleanupAccountRef, generation: 1, credential: cleanupCredential},
		{name: "account prefix", taskRef: cleanupTaskRef, accountRef: "account_cleanup", generation: 1, credential: cleanupCredential},
		{name: "account short suffix", taskRef: cleanupTaskRef, accountRef: "pacc_short", generation: 1, credential: cleanupCredential},
		{name: "account character", taskRef: cleanupTaskRef, accountRef: "pacc_cleanup/account", generation: 1, credential: cleanupCredential},
		{name: "lease generation", taskRef: cleanupTaskRef, accountRef: cleanupAccountRef, credential: cleanupCredential},
		{name: "secret name", taskRef: cleanupTaskRef, accountRef: cleanupAccountRef, generation: 1, credential: changedCredential(func(value *entity.ProviderCredentialDescriptor) { value.SecretName = "Invalid" })},
		{name: "secret UID", taskRef: cleanupTaskRef, accountRef: cleanupAccountRef, generation: 1, credential: changedCredential(func(value *entity.ProviderCredentialDescriptor) { value.SecretUID = "not-a-uuid" })},
		{name: "resource version", taskRef: cleanupTaskRef, accountRef: cleanupAccountRef, generation: 1, credential: changedCredential(func(value *entity.ProviderCredentialDescriptor) { value.SecretResourceVersion = " cleanup-7" })},
		{name: "digest length", taskRef: cleanupTaskRef, accountRef: cleanupAccountRef, generation: 1, credential: changedCredential(func(value *entity.ProviderCredentialDescriptor) { value.ContentSHA256 = "a" })},
		{name: "digest alphabet", taskRef: cleanupTaskRef, accountRef: cleanupAccountRef, generation: 1, credential: changedCredential(func(value *entity.ProviderCredentialDescriptor) { value.ContentSHA256 = strings.Repeat("A", 64) })},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			stub := &providerCredentialClientStub{}
			_, err := cleanupClient(t, stub).CleanupProviderCredential(
				context.Background(), test.taskRef, test.accountRef, test.generation, test.credential,
			)
			if err == nil || stub.calls != 0 {
				t.Fatalf("invalid request reached RPC: calls=%d err=%v", stub.calls, err)
			}
		})
	}
}

func TestCleanupProviderCredentialRejectsInvalidTerminalReceipt(t *testing.T) {
	t.Parallel()
	for _, receipt := range []string{"", " receipt", "receipt\n", strings.Repeat("r", 513)} {
		receipt := receipt
		t.Run("invalid receipt", func(t *testing.T) {
			t.Parallel()
			stub := &providerCredentialClientStub{
				response: &controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialResponse{
					TerminalReceipt: receipt,
				},
			}
			if _, err := cleanupClient(t, stub).CleanupProviderCredential(
				context.Background(), cleanupTaskRef, cleanupAccountRef, 1, cleanupCredential,
			); err == nil || stub.calls != 1 {
				t.Fatalf("invalid receipt was accepted: calls=%d", stub.calls)
			}
		})
	}
}

func changedCredential(change func(*entity.ProviderCredentialDescriptor)) entity.ProviderCredentialDescriptor {
	result := cleanupCredential
	change(&result)
	return result
}
