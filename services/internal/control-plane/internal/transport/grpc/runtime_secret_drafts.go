package grpc

import (
	"context"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// castSecretDraft maps draft identity/version/scope/state and lifecycle timestamps to metadata
// without copying secret values or independently validating the owner state.
// castSecretDraft преобразует draft identity, version, scope, state и lifecycle timestamps в
// metadata без копирования secret values либо самостоятельной проверки owner state.
func castSecretDraft(d entity.RuntimeSecretDraft) *cp.RuntimeSecretDraft {
	return &cp.RuntimeSecretDraft{ScopeKind: runtimeSecretScopeKind(d.ScopeKind), OrganizationRef: d.OrganizationRef, Ref: d.Ref, Version: d.Version, Generation: d.Generation, ProjectRef: d.ProjectRef, SecretRef: d.SecretRef, SecretVersion: d.SecretVersion, Name: d.Name, Description: d.Description, ValueType: runtimeSecretValueType(d.ValueType), State: cp.RuntimeSecretDraftState(cp.RuntimeSecretDraftState_value["RUNTIME_SECRET_DRAFT_STATE_"+d.State]), PublishedRevision: d.PublishedRevision, CreatedAt: timestamp(d.CreatedAt), UpdatedAt: timestamp(d.UpdatedAt), ExpiresAt: timestamp(d.ExpiresAt)}
}

// castDraftEncrypted preserves nil descriptor presence and copies exact encrypted Secret
// location/UID/version, ciphertext digest and encryption-key generation pins.
// castDraftEncrypted сохраняет nil presence descriptor и копирует точные encrypted Secret
// location, UID, version, ciphertext digest и encryption-key generation pins.
func castDraftEncrypted(d *entity.RuntimeSecretDraftEncryptedDescriptor) *cp.RuntimeSecretDraftEncryptedDescriptor {
	if d == nil {
		return nil
	}
	return &cp.RuntimeSecretDraftEncryptedDescriptor{Namespace: d.Namespace, SecretName: d.SecretName, SecretKey: d.SecretKey, SecretUid: d.SecretUID, SecretResourceVersion: d.SecretResourceVersion, CiphertextSha256: d.CiphertextSHA256, EncryptionKeyId: d.EncryptionKeyID, EncryptionKeyGeneration: d.EncryptionKeyGeneration}
}

// draftEncrypted copies an optional worker encrypted descriptor into the domain input without
// resolving the Secret or treating caller pins as proof of ownership.
// draftEncrypted копирует необязательный worker encrypted descriptor в доменный input без
// разрешения Secret либо признания caller pins доказательством ownership.
func draftEncrypted(d *cp.RuntimeSecretDraftEncryptedDescriptor) *entity.RuntimeSecretDraftEncryptedDescriptor {
	if d == nil {
		return nil
	}
	return &entity.RuntimeSecretDraftEncryptedDescriptor{Namespace: d.GetNamespace(), SecretName: d.GetSecretName(), SecretKey: d.GetSecretKey(), SecretUID: d.GetSecretUid(), SecretResourceVersion: d.GetSecretResourceVersion(), CiphertextSHA256: d.GetCiphertextSha256(), EncryptionKeyID: d.GetEncryptionKeyId(), EncryptionKeyGeneration: d.GetEncryptionKeyGeneration()}
}

// castDraftMaterialization maps an optional owner materialization descriptor with exact Secret
// metadata/content digest while omitting the materialized value.
// castDraftMaterialization преобразует необязательный owner materialization descriptor с точными
// Secret metadata и content digest без материализованного значения.
func castDraftMaterialization(d *entity.RuntimeSecretMaterialization) *cp.RuntimeSecretMaterialization {
	if d == nil {
		return nil
	}
	return &cp.RuntimeSecretMaterialization{Namespace: d.Namespace, SecretName: d.SecretName, SecretKey: d.SecretKey, SecretUid: d.SecretUID, SecretResourceVersion: d.SecretResourceVersion, ContentSha256: d.ContentSHA256}
}

// castDraftWork serializes owner operation/draft/claim/expiry and encrypted/materialization
// recovery descriptors without reading Secret values or minting a new claim.
// castDraftWork сериализует owner operation, draft, claim, expiry и encrypted и materialization
// recovery descriptors без чтения Secret values либо создания нового claim.
func castDraftWork(w entity.RuntimeSecretDraftWork) *cp.RuntimeSecretDraftWork {
	return &cp.RuntimeSecretDraftWork{OperationRef: w.OperationRef, Kind: cp.RuntimeSecretDraftOperationKind(cp.RuntimeSecretDraftOperationKind_value["RUNTIME_SECRET_DRAFT_OPERATION_KIND_"+w.Kind]), Draft: castSecretDraft(w.Draft), ExpectedContentSha256: w.ExpectedContentSHA256, Namespace: w.Namespace, StagedNamespace: w.StagedNamespace, StagedSecretName: w.StagedSecretName, StagedSecretKey: w.StagedSecretKey, TargetRevision: w.TargetRevision, Encrypted: castDraftEncrypted(w.Encrypted), ClaimantId: w.ClaimantID, ClaimGeneration: w.ClaimGeneration, LeaseDeadline: draftOptionalTime(w.LeaseDeadline), ExpiresAt: timestamp(w.ExpiresAt), RecoveryEncrypted: castDraftEncrypted(w.RecoveryEncrypted), RecoveryMaterialization: castDraftMaterialization(w.RecoveryMaterialization)}
}

// GetRuntimeSecretDraft reads the draft through the exact-method principal and owner service,
// returning only the authorized draft projection.
// GetRuntimeSecretDraft читает draft через principal точного метода и owner service и возвращает
// только разрешённую draft projection.
func (server *Server) GetRuntimeSecretDraft(ctx context.Context, request *cp.GetRuntimeSecretDraftRequest) (*cp.GetRuntimeSecretDraftResponse, error) {
	return queryResponse(ctx, cp.PlatformQueryService_GetRuntimeSecretDraft_FullMethodName, func(p value.Principal) (*cp.GetRuntimeSecretDraftResponse, error) {
		d, err := server.service.GetRuntimeSecretDraft(ctx, p, request.GetDraftRef())
		if err != nil {
			return nil, err
		}
		return &cp.GetRuntimeSecretDraftResponse{Draft: castSecretDraft(d)}, nil
	})
}

// prepareSecretDraft resolves the exact command principal and delegates a specialized prepare
// input to the owner before casting its privileged operation receipt.
// prepareSecretDraft разрешает principal точной команды и передаёт специализированный prepare
// input владельцу до преобразования privileged operation receipt.
func (server *Server) prepareSecretDraft(ctx context.Context, method string, input port.RuntimeSecretDraftPrepareInput) (*cp.RuntimeSecretDraftOperationReceipt, error) {
	p, err := principal(ctx, method)
	if err != nil {
		return nil, err
	}
	result, err := server.service.PrepareRuntimeSecretDraft(ctx, p, input)
	if err != nil {
		return nil, transportError(err)
	}
	return castSecretDraftReceipt(result), nil
}

// castSecretDraftReceipt maps the owner operation grant/ref/state/expiry and optional terminal
// Secret metadata; the grant remains an authorization-bearing value for the intended recipient.
// castSecretDraftReceipt преобразует owner operation grant, ref, state, expiry и необязательные
// terminal Secret metadata; grant остаётся несущим полномочия значением для назначенного
// получателя.
func castSecretDraftReceipt(result entity.RuntimeSecretDraftOperationReceipt) *cp.RuntimeSecretDraftOperationReceipt {
	response := &cp.RuntimeSecretDraftOperationReceipt{OperationGrant: result.OperationGrant, OperationRef: result.OperationRef, State: runtimeSecretOperationState(result.State), ExpiresAt: draftOptionalTime(result.ExpiresAt), Draft: castSecretDraft(result.Draft), FailureCode: runtimeSecretFailureCode(result.FailureCode)}
	if result.TerminalSecret != nil {
		response.TerminalSecret = castRuntimeSecret(*result.TerminalSecret)
	}
	return response
}

// PrepareOrganizationRuntimeSecretDraft passes organization SAVE intent, mutation, metadata/type
// and expected content hash to the owner without accepting secret bytes in this RPC.
// PrepareOrganizationRuntimeSecretDraft передаёт organization SAVE intent, mutation, metadata,
// type и expected content hash владельцу без приёма secret bytes в этом RPC.
func (server *Server) PrepareOrganizationRuntimeSecretDraft(ctx context.Context, request *cp.PrepareOrganizationRuntimeSecretDraftRequest) (*cp.PrepareOrganizationRuntimeSecretDraftResponse, error) {
	p, err := principal(ctx, cp.PlatformCommandService_PrepareOrganizationRuntimeSecretDraft_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.PrepareOrganizationRuntimeSecretDraft(ctx, p, port.RuntimeSecretDraftPrepareInput{Kind: "SAVE", Mutation: mutation(request.GetMutation()), Name: request.GetName(), Description: request.GetDescription(), ValueType: runtimeSecretValueTypeName(request.GetValueType()), ExpectedContentSHA256: request.GetExpectedContentSha256()})
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.PrepareOrganizationRuntimeSecretDraftResponse{Operation: castSecretDraftReceipt(result)}, nil
}

// PrepareSaveRuntimeSecretDraft requests SAVE preparation with secret/project locators and
// expected content digest under the exact RPC principal.
// PrepareSaveRuntimeSecretDraft запрашивает SAVE preparation с secret и project locators и
// expected content digest под principal точного RPC.
func (server *Server) PrepareSaveRuntimeSecretDraft(ctx context.Context, request *cp.PrepareSaveRuntimeSecretDraftRequest) (*cp.PrepareSaveRuntimeSecretDraftResponse, error) {
	operation, err := server.prepareSecretDraft(ctx, cp.PlatformCommandService_PrepareSaveRuntimeSecretDraft_FullMethodName, port.RuntimeSecretDraftPrepareInput{Kind: "SAVE", Mutation: mutation(request.GetMutation()), SecretRef: request.GetSecretRef(), ProjectRef: request.GetProjectRef(), Name: request.GetName(), Description: request.GetDescription(), ValueType: runtimeSecretValueTypeName(request.GetValueType()), ExpectedContentSHA256: request.GetExpectedContentSha256()})
	if err != nil {
		return nil, err
	}
	return &cp.PrepareSaveRuntimeSecretDraftResponse{Operation: operation}, nil
}

// PrepareValidateRuntimeSecretDraft requests VALIDATE preparation for the draft using the caller
// mutation and owner-resolved scope.
// PrepareValidateRuntimeSecretDraft запрашивает VALIDATE preparation draft с caller mutation и
// разрешённым владельцем scope.
func (server *Server) PrepareValidateRuntimeSecretDraft(ctx context.Context, request *cp.PrepareValidateRuntimeSecretDraftRequest) (*cp.PrepareValidateRuntimeSecretDraftResponse, error) {
	operation, err := server.prepareSecretDraft(ctx, cp.PlatformCommandService_PrepareValidateRuntimeSecretDraft_FullMethodName, port.RuntimeSecretDraftPrepareInput{Kind: "VALIDATE", Mutation: mutation(request.GetMutation()), DraftRef: request.GetDraftRef()})
	if err != nil {
		return nil, err
	}
	return &cp.PrepareValidateRuntimeSecretDraftResponse{Operation: operation}, nil
}

// PreparePublishRuntimeSecretDraft passes draft locator, expected Secret version and impact
// selection pins to owner PUBLISH preparation without performing publication in the transport.
// PreparePublishRuntimeSecretDraft передаёт draft locator, expected Secret version и impact
// selection pins owner PUBLISH preparation без публикации в transport.
func (server *Server) PreparePublishRuntimeSecretDraft(ctx context.Context, request *cp.PreparePublishRuntimeSecretDraftRequest) (*cp.PreparePublishRuntimeSecretDraftResponse, error) {
	operation, err := server.prepareSecretDraft(ctx, cp.PlatformCommandService_PreparePublishRuntimeSecretDraft_FullMethodName, port.RuntimeSecretDraftPrepareInput{Kind: "PUBLISH", Mutation: mutation(request.GetMutation()), DraftRef: request.GetDraftRef(), ExpectedSecretVersion: request.GetExpectedSecretVersion(), ImpactPlanRef: request.GetImpactPlanRef(), SelectedItemRefs: request.GetSelectedItemRefs()})
	if err != nil {
		return nil, err
	}
	return &cp.PreparePublishRuntimeSecretDraftResponse{Operation: operation}, nil
}

// PrepareDiscardRuntimeSecretDraft requests DISCARD preparation for the draft rather than deleting
// Kubernetes state directly.
// PrepareDiscardRuntimeSecretDraft запрашивает DISCARD preparation draft вместо прямого удаления
// Kubernetes state.
func (server *Server) PrepareDiscardRuntimeSecretDraft(ctx context.Context, request *cp.PrepareDiscardRuntimeSecretDraftRequest) (*cp.PrepareDiscardRuntimeSecretDraftResponse, error) {
	operation, err := server.prepareSecretDraft(ctx, cp.PlatformCommandService_PrepareDiscardRuntimeSecretDraft_FullMethodName, port.RuntimeSecretDraftPrepareInput{Kind: "DISCARD", Mutation: mutation(request.GetMutation()), DraftRef: request.GetDraftRef()})
	if err != nil {
		return nil, err
	}
	return &cp.PrepareDiscardRuntimeSecretDraftResponse{Operation: operation}, nil
}

// CheckRuntimeSecretDraftWorkReadiness returns ready only after the exact worker principal passes
// the owner draft-work readiness operation; it does not probe a live Secret publisher here.
// CheckRuntimeSecretDraftWorkReadiness возвращает ready только после owner draft-work readiness
// operation principal точного worker; живой Secret publisher здесь не проверяет.
func (server *Server) CheckRuntimeSecretDraftWorkReadiness(ctx context.Context, _ *cp.CheckRuntimeSecretDraftWorkReadinessRequest) (*cp.CheckRuntimeSecretDraftWorkReadinessResponse, error) {
	p, err := principal(ctx, cp.RuntimeSecretDraftWorkService_CheckRuntimeSecretDraftWorkReadiness_FullMethodName)
	if err != nil {
		return nil, err
	}
	if err = server.service.CheckRuntimeSecretDraftWork(ctx, p); err != nil {
		return nil, transportError(err)
	}
	return &cp.CheckRuntimeSecretDraftWorkReadinessResponse{Ready: true}, nil
}

// ConsumeRuntimeSecretDraftOperation passes the operation grant and claimant locator to owner
// consumption, then projects the granted exact work descriptors.
// ConsumeRuntimeSecretDraftOperation передаёт operation grant и claimant locator owner
// consumption, затем проецирует выданные точные work descriptors.
func (server *Server) ConsumeRuntimeSecretDraftOperation(ctx context.Context, request *cp.ConsumeRuntimeSecretDraftOperationRequest) (*cp.ConsumeRuntimeSecretDraftOperationResponse, error) {
	p, err := principal(ctx, cp.RuntimeSecretDraftWorkService_ConsumeRuntimeSecretDraftOperation_FullMethodName)
	if err != nil {
		return nil, err
	}
	work, err := server.service.ConsumeRuntimeSecretDraft(ctx, p, port.RuntimeSecretDraftWorkInput{OperationGrant: request.GetOperationGrant(), ClaimantID: request.GetClaimantId()})
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.ConsumeRuntimeSecretDraftOperationResponse{Work: castDraftWork(work)}, nil
}

// ListRuntimeSecretDraftRecoveryWork reads the owner recovery page under the exact worker
// principal and preserves its next cursor without issuing cleanup effects.
// ListRuntimeSecretDraftRecoveryWork читает owner recovery page под principal точного worker и
// сохраняет next cursor без cleanup effects.
func (server *Server) ListRuntimeSecretDraftRecoveryWork(ctx context.Context, request *cp.ListRuntimeSecretDraftRecoveryWorkRequest) (*cp.ListRuntimeSecretDraftRecoveryWorkResponse, error) {
	p, err := principal(ctx, cp.RuntimeSecretDraftWorkService_ListRuntimeSecretDraftRecoveryWork_FullMethodName)
	if err != nil {
		return nil, err
	}
	works, next, err := server.service.ListRuntimeSecretDraftRecovery(ctx, p, page(request.GetPage()))
	if err != nil {
		return nil, transportError(err)
	}
	response := &cp.ListRuntimeSecretDraftRecoveryWorkResponse{Page: &cp.PageInfo{NextPageToken: next}}
	response.Operations = castResponseItems(works, castDraftWork)
	return response, nil
}

// CompleteRuntimeSecretDraftOperation reports COMPLETE with operation/claim generation and
// encrypted/materialization descriptors to owner finalization, returning draft plus optional
// Secret metadata.
// CompleteRuntimeSecretDraftOperation передаёт COMPLETE с operation, claim generation и encrypted
// и materialization descriptors owner finalization, возвращая draft и необязательные Secret
// metadata.
func (server *Server) CompleteRuntimeSecretDraftOperation(ctx context.Context, request *cp.CompleteRuntimeSecretDraftOperationRequest) (*cp.CompleteRuntimeSecretDraftOperationResponse, error) {
	p, err := principal(ctx, cp.RuntimeSecretDraftWorkService_CompleteRuntimeSecretDraftOperation_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.FinishRuntimeSecretDraft(ctx, p, port.RuntimeSecretDraftWorkInput{Action: "COMPLETE", OperationRef: request.GetOperationRef(), ClaimantID: request.GetClaimantId(), ClaimGeneration: request.GetClaimGeneration(), Encrypted: draftEncrypted(request.GetEncrypted()), Materialization: runtimeSecretMaterialization(request.GetMaterialization())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &cp.CompleteRuntimeSecretDraftOperationResponse{Draft: castSecretDraft(result.Draft)}
	if result.Secret != nil {
		response.Secret = castRuntimeSecret(*result.Secret)
	}
	return response, nil
}

// FailRuntimeSecretDraftOperation reports FAIL with exact operation/claim pins and failure-code
// suffix while the owner decides the terminal/recovery transition.
// FailRuntimeSecretDraftOperation передаёт FAIL с точными operation и claim pins и failure-code
// suffix, оставляя terminal и recovery переход владельцу.
func (server *Server) FailRuntimeSecretDraftOperation(ctx context.Context, request *cp.FailRuntimeSecretDraftOperationRequest) (*cp.FailRuntimeSecretDraftOperationResponse, error) {
	p, err := principal(ctx, cp.RuntimeSecretDraftWorkService_FailRuntimeSecretDraftOperation_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.FinishRuntimeSecretDraft(ctx, p, port.RuntimeSecretDraftWorkInput{Action: "FAIL", OperationRef: request.GetOperationRef(), ClaimantID: request.GetClaimantId(), ClaimGeneration: request.GetClaimGeneration(), FailureCode: enumSuffix(request.GetFailureCode(), "RUNTIME_SECRET_FAILURE_CODE_")})
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.FailRuntimeSecretDraftOperationResponse{Draft: castSecretDraft(result.Draft), State: runtimeSecretOperationState(result.State)}, nil
}

// RecoverRuntimeSecretDraftMaterialization reports RECOVER with exact claim/descriptors and
// returns owner-selected encrypted/materialization recovery actions without choosing those actions
// from the request.
// RecoverRuntimeSecretDraftMaterialization передаёт RECOVER с точными claim и descriptors и
// возвращает выбранные владельцем encrypted и materialization recovery actions без выбора действий
// из запроса.
func (server *Server) RecoverRuntimeSecretDraftMaterialization(ctx context.Context, request *cp.RecoverRuntimeSecretDraftMaterializationRequest) (*cp.RecoverRuntimeSecretDraftMaterializationResponse, error) {
	p, err := principal(ctx, cp.RuntimeSecretDraftWorkService_RecoverRuntimeSecretDraftMaterialization_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.FinishRuntimeSecretDraft(ctx, p, port.RuntimeSecretDraftWorkInput{Action: "RECOVER", OperationRef: request.GetOperationRef(), ClaimantID: request.GetClaimantId(), ClaimGeneration: request.GetClaimGeneration(), Encrypted: draftEncrypted(request.GetEncrypted()), Materialization: runtimeSecretMaterialization(request.GetMaterialization())})
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.RecoverRuntimeSecretDraftMaterializationResponse{Draft: castSecretDraft(result.Draft), OperationState: runtimeSecretOperationState(result.State), EncryptedAction: cp.RuntimeSecretRecoveryAction(cp.RuntimeSecretRecoveryAction_value["RUNTIME_SECRET_RECOVERY_ACTION_"+result.EncryptedAction]), MaterializationAction: cp.RuntimeSecretRecoveryAction(cp.RuntimeSecretRecoveryAction_value["RUNTIME_SECRET_RECOVERY_ACTION_"+result.MaterializationAction])}, nil
}

// CompleteRuntimeSecretDraftCleanup reports CLEANUP under exact operation/claim/descriptors and
// returns only the owner completion flag.
// CompleteRuntimeSecretDraftCleanup передаёт CLEANUP под точными operation, claim и descriptors и
// возвращает только owner completion flag.
func (server *Server) CompleteRuntimeSecretDraftCleanup(ctx context.Context, request *cp.CompleteRuntimeSecretDraftCleanupRequest) (*cp.CompleteRuntimeSecretDraftCleanupResponse, error) {
	p, err := principal(ctx, cp.RuntimeSecretDraftWorkService_CompleteRuntimeSecretDraftCleanup_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.FinishRuntimeSecretDraft(ctx, p, port.RuntimeSecretDraftWorkInput{Action: "CLEANUP", OperationRef: request.GetOperationRef(), ClaimantID: request.GetClaimantId(), ClaimGeneration: request.GetClaimGeneration(), Encrypted: draftEncrypted(request.GetEncrypted()), Materialization: runtimeSecretMaterialization(request.GetMaterialization())})
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.CompleteRuntimeSecretDraftCleanupResponse{Completed: result.Completed}, nil
}

// draftOptionalTime preserves zero-time absence as nil and otherwise delegates timestamp encoding
// without adding an independent time-range check.
// draftOptionalTime сохраняет отсутствие zero time как nil и иначе передаёт timestamp encoding без
// отдельной time-range проверки.
func draftOptionalTime(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}
	return timestamp(value)
}
