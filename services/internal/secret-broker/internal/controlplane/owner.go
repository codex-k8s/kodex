package controlplane

import (
	"context"
	"errors"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/recovery"
)

const (
	recoveryPageSize     = 100
	maximumRecoveryPages = 10
)

type Owner struct {
	client     *controlplaneclient.Client
	claimantID string
}

func New(client *controlplaneclient.Client, claimantID string) (*Owner, error) {
	if client == nil || client.RuntimeSecrets == nil || claimantID == "" {
		return nil, errors.New("runtime secret owner client is required")
	}
	return &Owner{client: client, claimantID: claimantID}, nil
}

func (owner *Owner) Check(ctx context.Context) error {
	if err := owner.client.CheckLocalAuthority(ctx); err != nil {
		return err
	}
	result, err := owner.client.RuntimeSecrets.CheckRuntimeSecretWorkReadiness(ctx, &controlplanev1.CheckRuntimeSecretWorkReadinessRequest{})
	if err != nil || !result.GetReady() {
		return errors.New("runtime secret owner is unavailable")
	}
	return nil
}

// ListRecoveryWork читает bounded snapshot просроченных CLAIMED operation.
// Pagination token остаётся внутри owner adapter и не попадает в recovery logs.
func (owner *Owner) ListRecoveryWork(ctx context.Context) ([]recovery.RecoveryWork, error) {
	result := make([]recovery.RecoveryWork, 0, recoveryPageSize)
	pageToken := ""
	seenTokens := make(map[string]struct{}, maximumRecoveryPages)
	for pageIndex := 0; pageIndex < maximumRecoveryPages; pageIndex++ {
		response, err := owner.client.RuntimeSecrets.ListRuntimeSecretRecoveryWork(ctx, &controlplanev1.ListRuntimeSecretRecoveryWorkRequest{
			Page: &controlplanev1.PageRequest{PageSize: recoveryPageSize, PageToken: pageToken},
		})
		if err != nil {
			return nil, err
		}
		if response == nil || len(result)+len(response.GetOperations()) > recoveryPageSize*maximumRecoveryPages {
			return nil, errors.New("runtime secret recovery work response is invalid")
		}
		for _, item := range response.GetOperations() {
			if item == nil {
				return nil, errors.New("runtime secret recovery work item is invalid")
			}
			result = append(result, recovery.RecoveryWork{
				OperationRef: item.GetOperationRef(), Kind: item.GetKind(), ClaimantID: item.GetClaimantId(),
				ClaimGeneration: item.GetClaimGeneration(), Namespace: item.GetNamespace(), SecretRef: item.GetSecretRef(),
				TargetRevision: item.GetTargetRevision(), SecretKey: item.GetSecretKey(), ExpectedContentSHA256: item.GetExpectedContentSha256(),
			})
		}
		nextToken := response.GetPage().GetNextPageToken()
		if nextToken == "" {
			return result, nil
		}
		if nextToken == pageToken {
			return nil, errors.New("runtime secret recovery pagination did not advance")
		}
		if _, exists := seenTokens[nextToken]; exists {
			return nil, errors.New("runtime secret recovery pagination cycled")
		}
		seenTokens[nextToken] = struct{}{}
		pageToken = nextToken
	}
	return nil, errors.New("runtime secret recovery work exceeds bounded pagination")
}

func (owner *Owner) Consume(ctx context.Context, grant string) (*controlplanev1.ConsumeRuntimeSecretOperationResponse, error) {
	return owner.client.RuntimeSecrets.ConsumeRuntimeSecretOperation(ctx, &controlplanev1.ConsumeRuntimeSecretOperationRequest{
		OperationGrant: grant,
		ClaimantId:     owner.claimantID,
	})
}

func (owner *Owner) Complete(ctx context.Context, operationRef string, claimGeneration int64, materialization *controlplanev1.RuntimeSecretMaterialization) (*controlplanev1.RuntimeSecret, error) {
	result, err := owner.client.RuntimeSecrets.CompleteRuntimeSecretOperation(ctx, &controlplanev1.CompleteRuntimeSecretOperationRequest{
		OperationRef: operationRef, Materialization: materialization,
		ClaimantId: owner.claimantID, ClaimGeneration: claimGeneration,
	})
	if err != nil {
		return nil, err
	}
	if result.GetSecret() == nil {
		return nil, errors.New("runtime secret completion is incomplete")
	}
	return result.GetSecret(), nil
}

func (owner *Owner) Fail(ctx context.Context, operationRef string, claimGeneration int64, failureCode controlplanev1.RuntimeSecretFailureCode) error {
	return owner.failClaim(ctx, operationRef, owner.claimantID, claimGeneration, failureCode)
}

// FailExpiredClaim закрывает просроченную claim её исходным fence. Текущий
// POD_UID здесь намеренно не используется: новый pod не является claimant.
func (owner *Owner) FailExpiredClaim(ctx context.Context, operationRef, claimantID string, claimGeneration int64, failureCode controlplanev1.RuntimeSecretFailureCode) error {
	if claimantID == "" {
		return errors.New("runtime secret expired claimant is required")
	}
	return owner.failClaim(ctx, operationRef, claimantID, claimGeneration, failureCode)
}

func (owner *Owner) failClaim(ctx context.Context, operationRef, claimantID string, claimGeneration int64, failureCode controlplanev1.RuntimeSecretFailureCode) error {
	result, err := owner.client.RuntimeSecrets.FailRuntimeSecretOperation(ctx, &controlplanev1.FailRuntimeSecretOperationRequest{
		OperationRef: operationRef, ClaimantId: claimantID,
		ClaimGeneration: claimGeneration, FailureCode: failureCode,
	})
	if err != nil {
		return err
	}
	if result.GetOperationRef() != operationRef || result.GetState() != controlplanev1.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_FAILED || result.GetFailureCode() != failureCode {
		return errors.New("runtime secret failure receipt is invalid")
	}
	return nil
}

func (owner *Owner) Recover(ctx context.Context, operationRef string, materialization *controlplanev1.RuntimeSecretMaterialization) (*controlplanev1.RecoverRuntimeSecretMaterializationResponse, error) {
	if materialization == nil {
		return nil, errors.New("runtime secret recovery materialization is required")
	}
	return owner.client.RuntimeSecrets.RecoverRuntimeSecretMaterialization(ctx, &controlplanev1.RecoverRuntimeSecretMaterializationRequest{
		OperationRef: operationRef, Materialization: materialization,
	})
}
