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
	// recoveryPageSize requests at most 100 owner recovery items per page; oversized responses fail closed.
	// recoveryPageSize запрашивает до 100 owner recovery items на страницу; слишком большой ответ закрыто
	// отклоняется.
	recoveryPageSize = 100
	// maximumRecoveryPages bounds one recovery read to ten pages, preserving an internal continuation cursor
	// for the next batch.
	// maximumRecoveryPages ограничивает один recovery read десятью страницами с сохранением внутреннего
	// continuation cursor для следующего batch.
	maximumRecoveryPages = 10
	// recoveryListTimeout gives the serialized recovery read a ten-second budget including waiting for the
	// reader slot.
	// recoveryListTimeout задаёт десятисекундный бюджет serialized recovery read, включая ожидание reader
	// slot.
	recoveryListTimeout = 10 * time.Second
)

// errRecoveryPaginationCycle reports repeated cursor traversal without including opaque token contents in
// diagnostics.
// errRecoveryPaginationCycle сообщает о повторном обходе cursor без включения opaque token contents в
// diagnostic.
var errRecoveryPaginationCycle = errors.New("runtime secret recovery pagination cycled")

// Owner adapts the authenticated control-plane client with a stable claimant and serialized recovery
// cursor; it forwards domain authority rather than creating grants or owning lifecycle state locally.
// Owner адаптирует authenticated control-plane client со стабильным claimant и serialized recovery cursor;
// передаёт domain authority, не создавая grants и local lifecycle state.
type Owner struct {
	client         *controlplaneclient.Client
	claimantID     string
	recoveryReader chan struct{}
	recoveryCursor string
	recoveryCycle  recoverycursor.Cycle
}

// New requires a nonnil client/RuntimeSecrets route and nonempty claimant, but does not validate other
// optional routes, claimant syntax or remote readiness.
// New требует nonnil client/RuntimeSecrets route и непустой claimant, не проверяя остальные optional
// routes, claimant syntax и remote readiness.
func New(client *controlplaneclient.Client, claimantID string) (*Owner, error) {
	if client == nil || client.RuntimeSecrets == nil || claimantID == "" {
		return nil, errors.New("runtime secret owner client is required")
	}
	return &Owner{client: client, claimantID: claimantID, recoveryReader: make(chan struct{}, 1)}, nil
}

// Check verifies local client authority, then secret-work and projection-work readiness in order; RPC
// errors remain wrapped, not redacted, and this method does not claim work.
// Check проверяет local client authority, затем secret-work и projection-work readiness в таком порядке;
// RPC errors оборачиваются, не очищаются, и work не забирается.
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

// CheckCredentialProjection verifies local client authority and only the projection readiness route,
// without consuming grants or resolving credentials.
// CheckCredentialProjection проверяет local client authority и лишь projection readiness route, без
// потребления grants и разрешения credentials.
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

// ResolveRuntimeCredentialProjection forwards the exact request/context to the protected client;
// eligibility and descriptor validation are not newly implemented in this wrapper.
// ResolveRuntimeCredentialProjection передаёт exact request/context защищённому клиенту; eligibility и
// descriptor validation заново в wrapper не реализуются.
func (owner *Owner) ResolveRuntimeCredentialProjection(ctx context.Context, request *controlplanev1.ResolveRuntimeCredentialProjectionRequest) (*controlplanev1.ResolveRuntimeCredentialProjectionResponse, error) {
	return owner.client.RuntimeSecrets.ResolveRuntimeCredentialProjection(ctx, request)
}

// ValidateRuntimeCredentialProjection maps PermissionDenied/NotFound/FailedPrecondition to false without
// error, preserves other failures and returns the response validity flag on success.
// ValidateRuntimeCredentialProjection отображает PermissionDenied/NotFound/FailedPrecondition в false без
// ошибки, сохраняет остальные failures и возвращает response validity flag при успехе.
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

// ResolveTranscriptionCredentialProjection forwards the exact transcription request through the configured
// protected client without selecting an alternate credential source.
// ResolveTranscriptionCredentialProjection передаёт exact transcription request через настроенный
// защищённый клиент без выбора альтернативного credential source.
func (owner *Owner) ResolveTranscriptionCredentialProjection(ctx context.Context, request *controlplanev1.ResolveTranscriptionCredentialProjectionRequest) (*controlplanev1.ResolveTranscriptionCredentialProjectionResponse, error) {
	return owner.client.RuntimeSecrets.ResolveTranscriptionCredentialProjection(ctx, request)
}

// ListRecoveryWork serializes only recovery reads and returns at most ten validated pages, retaining
// cursor progress across batches. Once the reader slot is held, errors discard partial work and reset
// cursor/cycle state; earlier waiting cancellation leaves it untouched. Complete item eligibility remains
// owner/reconciler work, beyond the local ref-uniqueness/page checks.
// ListRecoveryWork сериализует лишь recovery reads и возвращает до десяти проверенных страниц с
// сохранением cursor progress между batches. После получения reader slot ошибка отбрасывает partial work
// и сбрасывает cursor/cycle state; отмена ожидания до slot не меняет их. Полная eligibility остаётся
// owner/reconciler, сверх локальных проверок ref uniqueness и pages.
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

// Consume forwards the opaque grant with the configured current claimant, not a caller-selected claimant;
// it does not independently validate the returned work DTO.
// Consume передаёт opaque grant с настроенным current claimant, не claimant по выбору caller; returned
// work DTO самостоятельно не проверяет.
func (owner *Owner) Consume(ctx context.Context, grant string) (*controlplanev1.ConsumeRuntimeSecretOperationResponse, error) {
	return owner.client.RuntimeSecrets.ConsumeRuntimeSecretOperation(ctx, &controlplanev1.ConsumeRuntimeSecretOperationRequest{
		OperationGrant: grant,
		ClaimantId:     owner.claimantID,
	})
}

// Complete forwards the exact operation/generation/materialization with the configured claimant, requires
// a nonnil returned Secret and leaves detailed metadata verification to its caller.
// Complete передаёт exact operation/generation/materialization с настроенным claimant, требует nonnil
// returned Secret и оставляет подробную metadata verification caller.
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

// Fail uses the configured current claimant and exact supplied generation/code through the shared
// receipt-checked failure request.
// Fail использует настроенный current claimant и exact переданные generation/code через общий failure
// request с проверкой receipt.
func (owner *Owner) Fail(ctx context.Context, operationRef string, claimGeneration int64, failureCode controlplanev1.RuntimeSecretFailureCode) error {
	return owner.failClaim(ctx, operationRef, owner.claimantID, claimGeneration, failureCode)
}

// FailExpiredClaim requires and forwards the original expired claimant/fence instead of replacing it with
// this pod's claimant; no new claim is taken.
// FailExpiredClaim требует и передаёт original expired claimant/fence без замены на claimant текущего Pod;
// новый claim не создаёт.
func (owner *Owner) FailExpiredClaim(ctx context.Context, operationRef, claimantID string, claimGeneration int64, failureCode controlplanev1.RuntimeSecretFailureCode) error {
	if claimantID == "" {
		return errors.New("runtime secret expired claimant is required")
	}
	return owner.failClaim(ctx, operationRef, claimantID, claimGeneration, failureCode)
}

// failClaim forwards operation/claimant/generation/code and accepts only a receipt matching operation,
// FAILED state and code; transport failures are preserved unchanged.
// failClaim передаёт operation/claimant/generation/code и принимает лишь receipt с совпавшими operation,
// FAILED state и code; transport failures сохраняет без изменений.
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

// Recover refuses a nil materialization then forwards operation and observed descriptor to the owner; it
// neither chooses KEEP/DELETE locally nor validates the returned decision here.
// Recover отклоняет nil materialization, затем передаёт operation и observed descriptor владельцу;
// локально KEEP/DELETE не выбирает и returned decision здесь не проверяет.
func (owner *Owner) Recover(ctx context.Context, operationRef string, materialization *controlplanev1.RuntimeSecretMaterialization) (*controlplanev1.RecoverRuntimeSecretMaterializationResponse, error) {
	if materialization == nil {
		return nil, errors.New("runtime secret recovery materialization is required")
	}
	return owner.client.RuntimeSecrets.RecoverRuntimeSecretMaterialization(ctx, &controlplanev1.RecoverRuntimeSecretMaterializationRequest{
		OperationRef: operationRef, Materialization: materialization,
	})
}
