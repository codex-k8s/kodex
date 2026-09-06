package controlplane

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/recovery"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/recoverycursor"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	recoveryPageSize     = 100
	maximumRecoveryPages = 10
	recoveryListTimeout  = 10 * time.Second
)

var errRecoveryPaginationCycle = errors.New("runtime secret recovery pagination cycled")

type Owner struct {
	client         *controlplaneclient.Client
	claimantID     string
	recoveryReader chan struct{}
	recoveryCursor string
	recoveryCycle  recoverycursor.Cycle
}

func New(client *controlplaneclient.Client, claimantID string) (*Owner, error) {
	if client == nil || client.RuntimeSecrets == nil || claimantID == "" {
		return nil, errors.New("runtime secret owner client is required")
	}
	return &Owner{client: client, claimantID: claimantID, recoveryReader: make(chan struct{}, 1)}, nil
}

func (owner *Owner) Check(ctx context.Context) error {
	if err := owner.client.CheckLocalAuthority(ctx); err != nil {
		return err
	}
	result, err := owner.client.RuntimeSecrets.CheckRuntimeSecretWorkReadiness(ctx, &controlplanev1.CheckRuntimeSecretWorkReadinessRequest{})
	if err != nil {
		return fmt.Errorf("check runtime secret owner readiness: %w", err)
	}
	if !result.GetReady() {
		return errors.New("runtime secret owner is unavailable")
	}
	projection, err := owner.client.RuntimeSecrets.CheckCredentialProjectionWorkReadiness(ctx, &controlplanev1.CheckCredentialProjectionWorkReadinessRequest{})
	if err != nil {
		return fmt.Errorf("check credential projection owner readiness: %w", err)
	}
	if !projection.GetReady() {
		return errors.New("credential projection owner is unavailable")
	}
	return nil
}

func (owner *Owner) CheckCredentialProjection(ctx context.Context) error {
	if err := owner.client.CheckLocalAuthority(ctx); err != nil {
		return err
	}
	result, err := owner.client.RuntimeSecrets.CheckCredentialProjectionWorkReadiness(ctx, &controlplanev1.CheckCredentialProjectionWorkReadinessRequest{})
	if err != nil {
		return fmt.Errorf("check credential projection owner readiness: %w", err)
	}
	if !result.GetReady() {
		return errors.New("credential projection owner is unavailable")
	}
	return nil
}

func (owner *Owner) ResolveRuntimeCredentialProjection(ctx context.Context, request *controlplanev1.ResolveRuntimeCredentialProjectionRequest) (*controlplanev1.ResolveRuntimeCredentialProjectionResponse, error) {
	return owner.client.RuntimeSecrets.ResolveRuntimeCredentialProjection(ctx, request)
}

func (owner *Owner) ValidateRuntimeCredentialProjection(ctx context.Context, request *controlplanev1.ValidateRuntimeCredentialProjectionRequest) (bool, error) {
	result, err := owner.client.RuntimeSecrets.ValidateRuntimeCredentialProjection(ctx, request)
	if err != nil {
		switch status.Code(err) {
		case codes.PermissionDenied, codes.NotFound, codes.FailedPrecondition:
			return false, nil
		}
		return false, err
	}
	return result.GetValid(), nil
}

func (owner *Owner) ResolveTranscriptionCredentialProjection(ctx context.Context, request *controlplanev1.ResolveTranscriptionCredentialProjectionRequest) (*controlplanev1.ResolveTranscriptionCredentialProjectionResponse, error) {
	return owner.client.RuntimeSecrets.ResolveTranscriptionCredentialProjection(ctx, request)
}

// ListRecoveryWork читает ограниченную порцию просроченных CLAIMED operation.
// Pagination token остаётся внутри owner adapter и не попадает в recovery logs.
func (owner *Owner) ListRecoveryWork(ctx context.Context) (work []recovery.RecoveryWork, resultErr error) {
	ctx, cancel := context.WithTimeout(ctx, recoveryListTimeout)
	defer cancel()
	select {
	case owner.recoveryReader <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() {
		if resultErr != nil {
			owner.recoveryCursor = ""
			owner.recoveryCycle.Reset()
		}
		<-owner.recoveryReader
	}()
	result := make([]recovery.RecoveryWork, 0, recoveryPageSize)
	pageToken := owner.recoveryCursor
	seenTokens := map[string]struct{}{pageToken: {}}
	seenOperations := make(map[string]struct{}, recoveryPageSize*maximumRecoveryPages)
	for pageIndex := 0; pageIndex < maximumRecoveryPages; pageIndex++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		response, err := owner.client.RuntimeSecrets.ListRuntimeSecretRecoveryWork(ctx, &controlplanev1.ListRuntimeSecretRecoveryWorkRequest{
			Page: &controlplanev1.PageRequest{PageSize: recoveryPageSize, PageToken: pageToken},
		})
		if err != nil {
			return nil, err
		}
		if response == nil || len(response.GetOperations()) > recoveryPageSize {
			return nil, errors.New("runtime secret recovery work response is invalid")
		}
		for _, item := range response.GetOperations() {
			if item == nil || item.GetOperationRef() == "" {
				return nil, errors.New("runtime secret recovery work item is invalid")
			}
			if _, duplicate := seenOperations[item.GetOperationRef()]; duplicate {
				return nil, errors.New("runtime secret recovery operation is repeated")
			}
			seenOperations[item.GetOperationRef()] = struct{}{}
			result = append(result, recovery.RecoveryWork{
				OperationRef: item.GetOperationRef(), Kind: item.GetKind(), ClaimantID: item.GetClaimantId(),
				ClaimGeneration: item.GetClaimGeneration(), Namespace: item.GetNamespace(), SecretRef: item.GetSecretRef(),
				TargetRevision: item.GetTargetRevision(), SecretKey: item.GetSecretKey(), ExpectedContentSHA256: item.GetExpectedContentSha256(),
			})
		}
		nextToken := response.GetPage().GetNextPageToken()
		if nextToken == "" {
			owner.recoveryCursor = ""
			owner.recoveryCycle.Reset()
			return result, nil
		}
		if len(response.GetOperations()) == 0 || len(nextToken) > 512 || nextToken == pageToken ||
			!utf8.ValidString(nextToken) || strings.ContainsRune(nextToken, '\x00') {
			return nil, errors.New("runtime secret recovery pagination did not advance")
		}
		if _, exists := seenTokens[nextToken]; exists {
			return nil, errRecoveryPaginationCycle
		}
		if !owner.recoveryCycle.Advance(nextToken) {
			return nil, errRecoveryPaginationCycle
		}
		seenTokens[nextToken] = struct{}{}
		pageToken = nextToken
	}
	owner.recoveryCursor = pageToken
	return result, nil
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
