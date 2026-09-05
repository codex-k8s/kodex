package draftowner

import (
	"context"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/secretdrafts"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const operationTimeout = 10 * time.Second

type Owner struct {
	client     cp.RuntimeSecretDraftWorkServiceClient
	claimantID string
}

var _ secretdrafts.Owner = (*Owner)(nil)

func New(client cp.RuntimeSecretDraftWorkServiceClient, claimantID string) (*Owner, error) {
	if client == nil || !reference(claimantID) {
		return nil, secretdrafts.ErrInvalid
	}
	return &Owner{client: client, claimantID: claimantID}, nil
}

func (owner *Owner) Check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	response, err := owner.client.CheckRuntimeSecretDraftWorkReadiness(ctx, &cp.CheckRuntimeSecretDraftWorkReadinessRequest{})
	if err != nil {
		return rpcError(ctx, err)
	}
	if !response.GetReady() {
		return secretdrafts.ErrUnavailable
	}
	return nil
}

func (owner *Owner) Consume(ctx context.Context, grant string) (value.DraftWork, error) {
	if err := ctx.Err(); err != nil {
		return value.DraftWork{}, err
	}
	if grant == "" || len(grant) > 16384 {
		return value.DraftWork{}, secretdrafts.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	response, err := owner.client.ConsumeRuntimeSecretDraftOperation(ctx, &cp.ConsumeRuntimeSecretDraftOperationRequest{OperationGrant: grant, ClaimantId: owner.claimantID})
	if err != nil {
		return value.DraftWork{}, rpcError(ctx, err)
	}
	work, err := castWork(response.GetWork(), false)
	if err != nil || work.ClaimantID != owner.claimantID {
		return value.DraftWork{}, secretdrafts.ErrConflict
	}
	return work, nil
}

func (owner *Owner) Complete(ctx context.Context, work value.DraftWork, encrypted *value.DraftEncryptedDescriptor, materialized *value.DraftMaterialization) (value.DraftResult, error) {
	if err := ctx.Err(); err != nil {
		return value.DraftResult{}, err
	}
	e, m, err := requestDescriptors(work, encrypted, materialized, false)
	if err != nil {
		return value.DraftResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	response, err := owner.client.CompleteRuntimeSecretDraftOperation(ctx, &cp.CompleteRuntimeSecretDraftOperationRequest{OperationRef: work.OperationRef, ClaimantId: work.ClaimantID, ClaimGeneration: work.ClaimGeneration, Encrypted: e, Materialization: m})
	if err != nil {
		return value.DraftResult{}, rpcError(ctx, err)
	}
	return castResult(response.GetDraft(), response.GetSecret(), work)
}

func (owner *Owner) Fail(ctx context.Context, work value.DraftWork) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validNativeWork(work, false) {
		return secretdrafts.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	response, err := owner.client.FailRuntimeSecretDraftOperation(ctx, &cp.FailRuntimeSecretDraftOperationRequest{OperationRef: work.OperationRef, ClaimantId: work.ClaimantID, ClaimGeneration: work.ClaimGeneration, FailureCode: cp.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_RECONCILIATION_FAILED})
	if err != nil {
		return rpcError(ctx, err)
	}
	draft, err := castDraft(response.GetDraft())
	if err != nil || !sameDraft(draft, work.Draft) || response.GetState() != cp.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_FAILED {
		return secretdrafts.ErrConflict
	}
	switch work.Kind {
	case value.DraftSave:
		if draft.State != "FAILED" {
			return secretdrafts.ErrConflict
		}
	case value.DraftPublish:
		if draft.State != "VALID" {
			return secretdrafts.ErrConflict
		}
	case value.DraftValidate:
		if draft.State != "DRAFT" && draft.State != "VALID" {
			return secretdrafts.ErrConflict
		}
	case value.DraftDiscard:
		if draft.State != "DISCARDED" {
			return secretdrafts.ErrConflict
		}
	}
	return nil
}

func (owner *Owner) ListRecovery(ctx context.Context) ([]value.DraftWork, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	result := []value.DraftWork{}
	token := ""
	seenTokens := map[string]bool{}
	seenOperations := map[string]bool{}
	for page := 0; page < 10; page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		response, err := owner.client.ListRuntimeSecretDraftRecoveryWork(ctx, &cp.ListRuntimeSecretDraftRecoveryWorkRequest{Page: &cp.PageRequest{PageSize: 100, PageToken: token}})
		if err != nil {
			return nil, rpcError(ctx, err)
		}
		if response == nil || len(response.GetOperations()) > 100 {
			return nil, secretdrafts.ErrConflict
		}
		for _, item := range response.GetOperations() {
			work, err := castWork(item, true)
			if err != nil || seenOperations[work.OperationRef] {
				return nil, secretdrafts.ErrConflict
			}
			seenOperations[work.OperationRef] = true
			result = append(result, work)
		}
		next := response.GetPage().GetNextPageToken()
		if next == "" {
			return result, nil
		}
		if !boundedText(next, 512, false) || seenTokens[next] {
			return nil, secretdrafts.ErrConflict
		}
		seenTokens[next] = true
		token = next
	}
	return nil, secretdrafts.ErrUnavailable
}

func (owner *Owner) Recover(ctx context.Context, work value.DraftWork, encrypted *value.DraftEncryptedDescriptor, materialized *value.DraftMaterialization) (value.DraftRecoveryDecision, error) {
	if err := ctx.Err(); err != nil {
		return value.DraftRecoveryDecision{}, err
	}
	e, m, err := requestDescriptors(work, encrypted, materialized, true)
	if err != nil {
		return value.DraftRecoveryDecision{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	response, err := owner.client.RecoverRuntimeSecretDraftMaterialization(ctx, &cp.RecoverRuntimeSecretDraftMaterializationRequest{OperationRef: work.OperationRef, ClaimantId: work.ClaimantID, ClaimGeneration: work.ClaimGeneration, Encrypted: e, Materialization: m})
	if err != nil {
		return value.DraftRecoveryDecision{}, rpcError(ctx, err)
	}
	draft, err := castDraft(response.GetDraft())
	encryptedAction, eOK := recoveryAction(response.GetEncryptedAction())
	materializedAction, mOK := recoveryAction(response.GetMaterializationAction())
	if err != nil || !sameDraft(draft, work.Draft) || !eOK || !mOK || (response.GetOperationState() != cp.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_COMPLETED && response.GetOperationState() != cp.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_FAILED) {
		return value.DraftRecoveryDecision{}, secretdrafts.ErrConflict
	}
	return value.DraftRecoveryDecision{EncryptedAction: encryptedAction, MaterializationAction: materializedAction}, nil
}

func (owner *Owner) CompleteCleanup(ctx context.Context, work value.DraftWork, encrypted *value.DraftEncryptedDescriptor, materialized *value.DraftMaterialization) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	e, m, err := requestDescriptors(work, encrypted, materialized, true)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	response, err := owner.client.CompleteRuntimeSecretDraftCleanup(ctx, &cp.CompleteRuntimeSecretDraftCleanupRequest{OperationRef: work.OperationRef, ClaimantId: work.ClaimantID, ClaimGeneration: work.ClaimGeneration, Encrypted: e, Materialization: m})
	if err != nil {
		return rpcError(ctx, err)
	}
	if !response.GetCompleted() {
		return secretdrafts.ErrConflict
	}
	return nil
}

func recoveryAction(action cp.RuntimeSecretRecoveryAction) (value.DraftRecoveryAction, bool) {
	switch action {
	case cp.RuntimeSecretRecoveryAction_RUNTIME_SECRET_RECOVERY_ACTION_KEEP:
		return value.DraftRecoveryKeep, true
	case cp.RuntimeSecretRecoveryAction_RUNTIME_SECRET_RECOVERY_ACTION_DELETE:
		return value.DraftRecoveryDelete, true
	default:
		return "", false
	}
}
func rpcError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	switch status.Code(err) {
	case codes.NotFound:
		return secretdrafts.ErrNotFound
	case codes.InvalidArgument:
		return secretdrafts.ErrInvalid
	case codes.AlreadyExists, codes.Aborted, codes.FailedPrecondition, codes.PermissionDenied, codes.Unauthenticated:
		return secretdrafts.ErrConflict
	default:
		return secretdrafts.ErrUnavailable
	}
}
