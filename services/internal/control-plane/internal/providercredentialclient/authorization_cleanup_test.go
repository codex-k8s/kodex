package providercredentialclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/grpc"
)

type authorizationMetadataStub struct {
	providerCredentialClientStub
	metadata        *controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationResponse
	metadataRequest *controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationRequest
	metadataCalls   int
}

func (stub *authorizationMetadataStub) ObserveDeviceAuthorization(_ context.Context, request *controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationRequest, _ ...grpc.CallOption) (*controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationResponse, error) {
	stub.metadataCalls++
	stub.metadataRequest = request
	return stub.metadata, stub.err
}

func authorizationCleanupTarget() entity.ProviderAuthorizationCleanupTarget {
	target := entity.ProviderAuthorizationCleanupTarget{TaskRef: cleanupTaskRef, AccountRef: cleanupAccountRef, AuthorizationAttemptRef: "pauth_" + strings.Repeat("a", 32), Generation: 7}
	digest := sha256.Sum256([]byte(target.AuthorizationAttemptRef + "\x00" + target.AccountRef))
	target.MaterializerAttemptRef = "pmat_" + hex.EncodeToString(digest[:16])
	return target
}

func TestAuthorizationMetadataPreservesExactOwnerBinding(t *testing.T) {
	t.Parallel()
	target := authorizationCleanupTarget()
	for _, state := range []controlplanev1.ProviderAuthorizationObjectState{
		controlplanev1.ProviderAuthorizationObjectState_PROVIDER_AUTHORIZATION_OBJECT_STATE_ABSENT_UNFENCED,
		controlplanev1.ProviderAuthorizationObjectState_PROVIDER_AUTHORIZATION_OBJECT_STATE_CONFIRMED_ABSENT,
	} {
		stub := &authorizationMetadataStub{metadata: &controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationResponse{
			ObjectState: state, AbsentObject: &controlplanev1.ProviderAuthorizationAbsenceDescriptor{
				MaterializerAttemptRef: target.MaterializerAttemptRef, AccountRef: target.AccountRef, AuthorizationAttemptRef: target.AuthorizationAttemptRef,
			},
		}}
		client, err := New(&controlplaneclient.Client{ProviderCredentials: stub})
		if err != nil {
			t.Fatal(err)
		}
		result, err := client.ObserveAuthorizationCleanup(context.Background(), target)
		if err != nil || result.Target.Kind != "AUTHORIZATION_ABSENCE" || result.Target.UID != "" || result.Target.Generation != 7 {
			t.Fatalf("metadata read failed: result=%+v err=%v", result, err)
		}
		request := stub.metadataRequest
		if request.GetMode() != controlplanev1.ProviderAuthorizationObservationMode_PROVIDER_AUTHORIZATION_OBSERVATION_MODE_METADATA_ONLY || request.GetTaskRef() != target.TaskRef || request.GetAccountRef() != target.AccountRef || request.GetAuthorizationAttemptRef() != target.AuthorizationAttemptRef || request.GetLeaseGeneration() != target.Generation {
			t.Fatal("metadata request lost owner binding")
		}
		if (state == controlplanev1.ProviderAuthorizationObjectState_PROVIDER_AUTHORIZATION_OBJECT_STATE_CONFIRMED_ABSENT) != (result.State == "CONFIRMED_ABSENT") {
			t.Fatal("unfenced absence was promoted to terminal")
		}
		stub.metadata.AbsentObject.AccountRef = "pacc_other_account"
		if _, err := client.ObserveAuthorizationCleanup(context.Background(), target); err == nil {
			t.Fatal("foreign account metadata accepted")
		}
	}
}

func TestAuthorizationCleanupRejectsMixedPinsBeforeRPC(t *testing.T) {
	t.Parallel()
	base := authorizationCleanupTarget()
	for _, change := range []func(*entity.ProviderAuthorizationCleanupTarget){
		func(v *entity.ProviderAuthorizationCleanupTarget) { v.Kind = "UNKNOWN" },
		func(v *entity.ProviderAuthorizationCleanupTarget) {
			v.Kind = "AUTHORIZATION_ABSENCE"
			v.UID = cleanupCredential.SecretUID
		},
		func(v *entity.ProviderAuthorizationCleanupTarget) { v.Kind = "AUTHORIZATION_ATTEMPT" },
		func(v *entity.ProviderAuthorizationCleanupTarget) {
			v.Kind = "AUTHORIZATION_ABSENCE"
			v.AccountRef = "pacc_foreign_account"
		},
		func(v *entity.ProviderAuthorizationCleanupTarget) { v.Kind = "AUTHORIZATION_ABSENCE"; v.Generation = 0 },
	} {
		target := base
		change(&target)
		stub := &providerCredentialClientStub{}
		if _, err := cleanupClient(t, stub).CleanupAuthorization(context.Background(), target); err == nil || stub.calls != 0 {
			t.Fatal("invalid cleanup reached broker")
		}
	}
}

func TestAuthorizationMetadataPresentAndClosedResponse(t *testing.T) {
	t.Parallel()
	target := authorizationCleanupTarget()
	fixture := func() *controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationResponse {
		return &controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationResponse{
			ObjectState: controlplanev1.ProviderAuthorizationObjectState_PROVIDER_AUTHORIZATION_OBJECT_STATE_PRESENT,
			PendingObject: &controlplanev1.ProviderAuthorizationObjectDescriptor{
				MaterializerAttemptRef: target.MaterializerAttemptRef, AccountRef: target.AccountRef,
				AuthorizationAttemptRef: target.AuthorizationAttemptRef, Uid: cleanupCredential.SecretUID, ResourceVersion: "rv-17",
			},
		}
	}
	stub := &authorizationMetadataStub{metadata: fixture()}
	client, err := New(&controlplaneclient.Client{ProviderCredentials: stub})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.ObserveAuthorizationCleanup(context.Background(), target)
	if err != nil || result.State != "PRESENT" || result.Target.UID != cleanupCredential.SecretUID || result.Target.ResourceVersion != "rv-17" {
		t.Fatalf("present metadata lost: %v", err)
	}
	for _, mutate := range []func(*controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationResponse){
		func(v *controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationResponse) {
			v.ObjectState = 99
		},
		func(v *controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationResponse) {
			v.PendingObject.Uid = "invalid"
		},
		func(v *controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationResponse) {
			v.AbsentObject = &controlplanev1.ProviderAuthorizationAbsenceDescriptor{}
		},
		func(v *controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationResponse) {
			v.ExternalAccountMasked = "unexpected"
		},
		func(v *controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationResponse) {
			v.State = controlplanev1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_PENDING
		},
	} {
		stub.metadata = fixture()
		mutate(stub.metadata)
		if _, err := client.ObserveAuthorizationCleanup(context.Background(), target); err == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
}

func TestAuthorizationCleanupPreservesProducedCredentialForSeparateTask(t *testing.T) {
	t.Parallel()
	target := authorizationCleanupTarget()
	target.Kind = "AUTHORIZATION_ABSENCE"
	stub := &providerCredentialClientStub{response: &controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialResponse{
		TerminalReceipt: cleanupReceipt, ProducedCredential: &controlplanev1.ProviderCredentialDescriptor{
			SecretName: cleanupCredential.SecretName, SecretUid: cleanupCredential.SecretUID, SecretResourceVersion: cleanupCredential.SecretResourceVersion, ContentSha256: cleanupCredential.ContentSHA256,
		},
	}}
	result, err := cleanupClient(t, stub).CleanupAuthorization(context.Background(), target)
	if err != nil || result.ProducedCredential == nil || *result.ProducedCredential != cleanupCredential {
		t.Fatalf("produced credential lost: %v", err)
	}
	if stub.request.GetCredential() != nil || stub.request.GetPendingObject() != nil || stub.request.GetAbsentObject() == nil || stub.request.GetTargetKind() != controlplanev1.ProviderCredentialCleanupTargetKind_PROVIDER_CREDENTIAL_CLEANUP_TARGET_KIND_AUTHORIZATION_ABSENCE {
		t.Fatal("absence cleanup was encoded as delete")
	}
	stub.response.ProducedCredential.ContentSha256 = "invalid"
	if _, err := cleanupClient(t, stub).CleanupAuthorization(context.Background(), target); err == nil {
		t.Fatal("invalid produced credential accepted")
	}
}
