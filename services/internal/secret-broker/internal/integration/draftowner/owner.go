package draftowner

import (
	"context"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/secretdrafts"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/recoverycursor"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// operationTimeout caps each owner RPC/list operation at ten seconds, shortened by caller cancellation or
	// an earlier deadline.
	// operationTimeout ограничивает каждую owner RPC/list operation десятью секундами с учётом caller
	// cancellation и более раннего deadline.
	operationTimeout = 10 * time.Second
	// recoveryPageSize requests at most 100 draft recovery works per page and is checked against returned page
	// length.
	// recoveryPageSize запрашивает до 100 draft recovery works на страницу и сверяется с длиной возвращённой
	// страницы.
	recoveryPageSize = 100
	// recoveryPageBudget limits one traversal batch to ten pages before saving its internal continuation hint.
	// recoveryPageBudget ограничивает один traversal batch десятью страницами до сохранения внутреннего
	// continuation hint.
	recoveryPageBudget = 10
)

// Owner translates protected draft-work RPCs into domain ports with strict metadata and bounded error
// mapping; only recovery listing is locally serialized, and authority remains server-owned.
// Owner переводит защищённые draft-work RPC в domain ports со strict metadata и bounded error mapping;
// локально сериализует лишь recovery listing, authority остаётся server-owned.
type Owner struct {
	client         cp.RuntimeSecretDraftWorkServiceClient
	claimantID     string
	recoveryReader chan struct{}
	recoveryCursor string
	recoveryCycle  recoverycursor.Cycle
}

var _ secretdrafts.Owner = (*Owner)(nil)

// New checks a nonnil generated client interface and bounded claimant reference, initializes recovery
// serialization and does not call readiness or detect typed nils.
// New проверяет nonnil generated client interface и bounded claimant reference, создаёт recovery
// serialization без readiness call и проверки typed nil.
func New(client cp.RuntimeSecretDraftWorkServiceClient, claimantID string) (*Owner, error) {
	if client == nil || !reference(claimantID) {
		return nil, secretdrafts.ErrInvalid
	}
	return &Owner{client: client, claimantID: claimantID, recoveryReader: make(chan struct{}, 1)}, nil
}

// Check refuses cancelled context, makes a bounded readiness RPC and accepts only Ready; provider
// diagnostics are mapped to domain sentinels.
// Check отклоняет cancelled context, выполняет bounded readiness RPC и принимает лишь Ready; provider
// diagnostics отображает в domain sentinels.
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

// Consume bounds the opaque grant, forwards configured claimant, validates native work and requires the
// returned claimant to match; this adapter does not itself own grant issuance or lease renewal.
// Consume ограничивает opaque grant, передаёт настроенный claimant, проверяет native work и совпадение
// returned claimant; выдачей grants и продлением lease adapter не владеет.
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

// Complete validates supplied work/descriptors, forwards that exact claim tuple and checks the
// command-specific result; it does not retry uncertain completion or perform a storage effect.
// Complete проверяет supplied work/descriptors, передаёт exact claim tuple и проверяет command-specific
// result; неопределённую completion не повторяет, storage effect не выполняет.
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

// Fail sends fixed RECONCILIATION_FAILED for validated claimed work and requires matching draft/FAILED
// operation plus the kind-specific draft state; it uses the work's claimant, not a replacement claimant.
// Fail передаёт fixed RECONCILIATION_FAILED для проверенной claimed work и требует matching draft/FAILED
// operation с kind-specific draft state; использует claimant из work, не replacement claimant.
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

// ListRecovery bounds/serializes only list traversal, validates each recovery work and rejects
// duplicates/cycles without partial results. Errors after slot acquisition reset progress; cancellation
// before acquiring it leaves the hint unchanged, and a full traversal wraps.
// ListRecovery ограничивает/сериализует лишь list traversal, проверяет каждую recovery work и отклоняет
// duplicates/cycles без partial results. Ошибки после получения slot сбрасывают progress; отмена до slot
// не меняет hint, полный обход возвращается в начало.
func (owner *Owner) ListRecovery(ctx context.Context) (work []value.DraftWork, resultErr error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
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
	result := []value.DraftWork{}
	token := owner.recoveryCursor
	seenTokens := map[string]bool{token: true}
	seenOperations := map[string]bool{}
	for page := 0; page < recoveryPageBudget; page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		response, err := owner.client.ListRuntimeSecretDraftRecoveryWork(ctx, &cp.ListRuntimeSecretDraftRecoveryWorkRequest{Page: &cp.PageRequest{PageSize: recoveryPageSize, PageToken: token}})
		if err != nil {
			return nil, rpcError(ctx, err)
		}
		if response == nil || len(response.GetOperations()) > recoveryPageSize {
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
			owner.recoveryCursor = ""
			owner.recoveryCycle.Reset()
			return result, nil
		}
		if len(response.GetOperations()) == 0 || !boundedText(next, 512, false) || seenTokens[next] {
			return nil, secretdrafts.ErrConflict
		}
		if !owner.recoveryCycle.Advance(next) {
			return nil, secretdrafts.ErrConflict
		}
		seenTokens[next] = true
		token = next
	}
	// Cursor — только подсказка обхода. Даже после отмены обработки batch
	// незавершённая операция остаётся у владельца и попадёт в следующий обход.
	owner.recoveryCursor = token
	return result, nil
}

// Recover validates recovery work/descriptors, forwards the original zero or claimed tuple and accepts
// only matching draft, terminal operation and independently known KEEP/DELETE actions.
// Recover проверяет recovery work/descriptors, передаёт original zero либо claimed tuple и принимает лишь
// matching draft, terminal operation и раздельно известные KEEP/DELETE actions.
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

// CompleteCleanup validates exact recovery descriptors, sends their original claim tuple and requires
// Completed; descriptor/RPC/response failures get bounded CleanupAckError stages without raw diagnostics.
// CompleteCleanup проверяет exact recovery descriptors, отправляет original claim tuple и требует
// Completed; descriptor/RPC/response failures получают bounded CleanupAckError stages без raw diagnostics.
func (owner *Owner) CompleteCleanup(ctx context.Context, work value.DraftWork, encrypted *value.DraftEncryptedDescriptor, materialized *value.DraftMaterialization) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	e, m, err := requestDescriptors(work, encrypted, materialized, true)
	if err != nil {
		return secretdrafts.CleanupAckError{Stage: "descriptor", Cause: err}
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	response, err := owner.client.CompleteRuntimeSecretDraftCleanup(ctx, &cp.CompleteRuntimeSecretDraftCleanupRequest{OperationRef: work.OperationRef, ClaimantId: work.ClaimantID, ClaimGeneration: work.ClaimGeneration, Encrypted: e, Materialization: m})
	if err != nil {
		return secretdrafts.CleanupAckError{Stage: "rpc_" + status.Code(err).String(), Cause: rpcError(ctx, err)}
	}
	if !response.GetCompleted() {
		return secretdrafts.CleanupAckError{Stage: "response", Cause: secretdrafts.ErrConflict}
	}
	return nil
}

// recoveryAction maps only KEEP/DELETE to domain actions and explicitly rejects every other enum value.
// recoveryAction отображает лишь KEEP/DELETE в domain actions и явно отклоняет остальные enum values.
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

// rpcError maps RPC failure codes to fixed domain sentinels, preferring current cancellation; it is used
// on failure paths and hides arbitrary upstream diagnostics, not a nil-success pass-through.
// rpcError отображает RPC failure codes в fixed domain sentinels с приоритетом current cancellation;
// применяется при отказе и скрывает arbitrary upstream diagnostics, не является nil-success pass-through.
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
