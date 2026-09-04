package controlplane

import (
	"context"
	"errors"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"google.golang.org/grpc"
)

type fakeRuntimeSecretClient struct {
	consumeRequest  *controlplanev1.ConsumeRuntimeSecretOperationRequest
	completeRequest *controlplanev1.CompleteRuntimeSecretOperationRequest
	failRequests    []*controlplanev1.FailRuntimeSecretOperationRequest
	recoverRequest  *controlplanev1.RecoverRuntimeSecretMaterializationRequest
	listRequests    []*controlplanev1.ListRuntimeSecretRecoveryWorkRequest
}

func (*fakeRuntimeSecretClient) CheckRuntimeSecretWorkReadiness(context.Context, *controlplanev1.CheckRuntimeSecretWorkReadinessRequest, ...grpc.CallOption) (*controlplanev1.CheckRuntimeSecretWorkReadinessResponse, error) {
	return &controlplanev1.CheckRuntimeSecretWorkReadinessResponse{Ready: true}, nil
}

func (*fakeRuntimeSecretClient) CheckCredentialProjectionWorkReadiness(context.Context, *controlplanev1.CheckCredentialProjectionWorkReadinessRequest, ...grpc.CallOption) (*controlplanev1.CheckCredentialProjectionWorkReadinessResponse, error) {
	return &controlplanev1.CheckCredentialProjectionWorkReadinessResponse{Ready: true}, nil
}

func (*fakeRuntimeSecretClient) ResolveRuntimeCredentialProjection(context.Context, *controlplanev1.ResolveRuntimeCredentialProjectionRequest, ...grpc.CallOption) (*controlplanev1.ResolveRuntimeCredentialProjectionResponse, error) {
	return nil, errors.New("unexpected runtime credential projection")
}

func (*fakeRuntimeSecretClient) ValidateRuntimeCredentialProjection(context.Context, *controlplanev1.ValidateRuntimeCredentialProjectionRequest, ...grpc.CallOption) (*controlplanev1.ValidateRuntimeCredentialProjectionResponse, error) {
	return nil, errors.New("unexpected runtime credential projection validation")
}

func (*fakeRuntimeSecretClient) ResolveTranscriptionCredentialProjection(context.Context, *controlplanev1.ResolveTranscriptionCredentialProjectionRequest, ...grpc.CallOption) (*controlplanev1.ResolveTranscriptionCredentialProjectionResponse, error) {
	return nil, errors.New("unexpected transcription credential projection")
}

func (client *fakeRuntimeSecretClient) ListRuntimeSecretRecoveryWork(_ context.Context, request *controlplanev1.ListRuntimeSecretRecoveryWorkRequest, _ ...grpc.CallOption) (*controlplanev1.ListRuntimeSecretRecoveryWorkResponse, error) {
	client.listRequests = append(client.listRequests, request)
	if request.GetPage().GetPageToken() == "" {
		return &controlplanev1.ListRuntimeSecretRecoveryWorkResponse{
			Operations: []*controlplanev1.RuntimeSecretRecoveryWork{{
				OperationRef: "secop_expired_create", Kind: controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_CREATE,
				ClaimantId: "old-pod-uid", ClaimGeneration: 5, Namespace: "kodex-runtime", SecretRef: "sec_test",
				TargetRevision: 2, SecretKey: "value", ExpectedContentSha256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			}},
			Page: &controlplanev1.PageInfo{NextPageToken: "next"},
		}, nil
	}
	return &controlplanev1.ListRuntimeSecretRecoveryWorkResponse{
		Operations: []*controlplanev1.RuntimeSecretRecoveryWork{{
			OperationRef: "secop_expired_reveal", Kind: controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_REVEAL,
			ClaimantId: "other-old-pod-uid", ClaimGeneration: 9, Namespace: "kodex-runtime", SecretRef: "sec_other",
		}},
		Page: &controlplanev1.PageInfo{},
	}, nil
}

func (client *fakeRuntimeSecretClient) ConsumeRuntimeSecretOperation(_ context.Context, request *controlplanev1.ConsumeRuntimeSecretOperationRequest, _ ...grpc.CallOption) (*controlplanev1.ConsumeRuntimeSecretOperationResponse, error) {
	client.consumeRequest = request
	return &controlplanev1.ConsumeRuntimeSecretOperationResponse{OperationRef: "secop_test", ClaimGeneration: 7}, nil
}

func (client *fakeRuntimeSecretClient) CompleteRuntimeSecretOperation(_ context.Context, request *controlplanev1.CompleteRuntimeSecretOperationRequest, _ ...grpc.CallOption) (*controlplanev1.CompleteRuntimeSecretOperationResponse, error) {
	client.completeRequest = request
	return &controlplanev1.CompleteRuntimeSecretOperationResponse{Secret: &controlplanev1.RuntimeSecret{Ref: "sec_test"}}, nil
}

func (client *fakeRuntimeSecretClient) FailRuntimeSecretOperation(_ context.Context, request *controlplanev1.FailRuntimeSecretOperationRequest, _ ...grpc.CallOption) (*controlplanev1.FailRuntimeSecretOperationResponse, error) {
	client.failRequests = append(client.failRequests, request)
	return &controlplanev1.FailRuntimeSecretOperationResponse{
		OperationRef: request.GetOperationRef(), State: controlplanev1.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_FAILED,
		FailureCode: request.GetFailureCode(),
	}, nil
}

func (client *fakeRuntimeSecretClient) RecoverRuntimeSecretMaterialization(_ context.Context, request *controlplanev1.RecoverRuntimeSecretMaterializationRequest, _ ...grpc.CallOption) (*controlplanev1.RecoverRuntimeSecretMaterializationResponse, error) {
	client.recoverRequest = request
	return &controlplanev1.RecoverRuntimeSecretMaterializationResponse{
		Action:         controlplanev1.RuntimeSecretRecoveryAction_RUNTIME_SECRET_RECOVERY_ACTION_KEEP,
		OperationState: controlplanev1.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_COMPLETED,
	}, nil
}

func TestOwnerSendsStableClaimantAndFence(t *testing.T) {
	t.Parallel()
	client := &fakeRuntimeSecretClient{}
	owner, err := New(&controlplaneclient.Client{RuntimeSecrets: client}, "pod-uid-test")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := owner.Consume(ctx, "grant"); err != nil {
		t.Fatal(err)
	}
	recoveryWork, err := owner.ListRecoveryWork(ctx)
	if err != nil {
		t.Fatal(err)
	}
	materialization := &controlplanev1.RuntimeSecretMaterialization{Namespace: "kodex-runtime", SecretName: "runtime-secret-test-r1"}
	if _, err := owner.Complete(ctx, "secop_test", 7, materialization); err != nil {
		t.Fatal(err)
	}
	failureCode := controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_MATERIALIZATION_INVALID
	if err := owner.Fail(ctx, "secop_test", 7, failureCode); err != nil {
		t.Fatal(err)
	}
	if err := owner.FailExpiredClaim(ctx, "secop_expired", "old-pod-uid", 5, controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_RECONCILIATION_FAILED); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Recover(ctx, "secop_test", materialization); err != nil {
		t.Fatal(err)
	}
	if len(recoveryWork) != 2 || recoveryWork[0].OperationRef != "secop_expired_create" || recoveryWork[1].ClaimGeneration != 9 ||
		len(client.listRequests) != 2 || client.listRequests[0].GetPage().GetPageSize() != recoveryPageSize ||
		client.listRequests[1].GetPage().GetPageToken() != "next" || len(client.failRequests) != 2 ||
		client.consumeRequest.GetClaimantId() != "pod-uid-test" || client.completeRequest.GetClaimantId() != "pod-uid-test" ||
		client.completeRequest.GetClaimGeneration() != 7 || client.failRequests[0].GetClaimantId() != "pod-uid-test" ||
		client.failRequests[0].GetClaimGeneration() != 7 || client.failRequests[1].GetOperationRef() != "secop_expired" ||
		client.failRequests[1].GetClaimantId() != "old-pod-uid" || client.failRequests[1].GetClaimGeneration() != 5 ||
		client.failRequests[1].GetFailureCode() != controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_RECONCILIATION_FAILED ||
		client.recoverRequest.GetOperationRef() != "secop_test" {
		t.Fatalf("owner lifecycle request lost claimant or fence: consume=%#v complete=%#v fail=%#v recover=%#v",
			client.consumeRequest, client.completeRequest, client.failRequests, client.recoverRequest)
	}
}
