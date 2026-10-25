package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

// CheckRuntimeSecretWorkReadiness checks the exact work RPC principal and returns transport readiness; this endpoint does not probe Kubernetes materialization, secret bytes or the broker itself.
// CheckRuntimeSecretWorkReadiness проверяет principal точного рабочего RPC и возвращает готовность транспорта; этот endpoint не проверяет материализацию Kubernetes, байты секретов и сам брокер.
func (server *Server) CheckRuntimeSecretWorkReadiness(ctx context.Context, _ *controlplanev1.CheckRuntimeSecretWorkReadinessRequest) (*controlplanev1.CheckRuntimeSecretWorkReadinessResponse, error) {
	if _, err := principal(ctx, controlplanev1.RuntimeSecretWorkService_CheckRuntimeSecretWorkReadiness_FullMethodName); err != nil {
		return nil, err
	}
	return &controlplanev1.CheckRuntimeSecretWorkReadinessResponse{Ready: true}, nil
}

// CheckCredentialProjectionWorkReadiness checks the projection readiness RPC identity and domain service readiness before returning Ready; dependency failure is translated through the common transport mapper.
// CheckCredentialProjectionWorkReadiness проверяет identity RPC готовности проекций и готовность доменного сервиса перед Ready; отказ зависимости преобразует общий mapper транспорта.
func (server *Server) CheckCredentialProjectionWorkReadiness(ctx context.Context, _ *controlplanev1.CheckCredentialProjectionWorkReadinessRequest) (*controlplanev1.CheckCredentialProjectionWorkReadinessResponse, error) {
	if _, err := principal(ctx, controlplanev1.RuntimeSecretWorkService_CheckCredentialProjectionWorkReadiness_FullMethodName); err != nil {
		return nil, err
	}
	if err := server.service.Ready(ctx); err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.CheckCredentialProjectionWorkReadinessResponse{Ready: true}, nil
}

// ResolveRuntimeCredentialProjection passes the verified RPC principal and the exact request lease/fence/generation/execution pins to the domain resolver, then maps only its returned descriptors and authority proof. Payload identifiers alone do not authorize a projection.
// ResolveRuntimeCredentialProjection передаёт проверенный RPC principal и точные lease/fence/generation/execution pins запроса доменному разрешителю, затем переносит только возвращённые дескрипторы и proof полномочий. Одних идентификаторов payload недостаточно для разрешения проекции.
func (server *Server) ResolveRuntimeCredentialProjection(ctx context.Context, request *controlplanev1.ResolveRuntimeCredentialProjectionRequest) (*controlplanev1.ResolveRuntimeCredentialProjectionResponse, error) {
	p, err := principal(ctx, controlplanev1.RuntimeSecretWorkService_ResolveRuntimeCredentialProjection_FullMethodName)
	if err != nil {
		return nil, err
	}
	input := runtimeCredentialProjectionInput(request.GetAuthority(), request.GetWorkloadInstance(), request.GetLeaseRef(), request.GetFence(),
		request.GetGeneration(), request.GetRuntimeRevisionRef(), request.GetRuntimeRevisionDigest(), request.GetSessionRef(), request.GetTurnRef(), request.GetAttempt(), request.GetInputDigest())
	result, err := server.service.ResolveRuntimeCredentialProjection(ctx, p, input)
	if err != nil {
		return nil, transportError(err)
	}
	return castRuntimeCredentialProjection(result), nil
}

// ValidateRuntimeCredentialProjection maps the caller projection and provider/runtime-secret descriptors for domain validation under the exact RPC principal. This validation request intentionally has no fence field in the constructed input; the domain owns eligibility and binding checks.
// ValidateRuntimeCredentialProjection переносит проекцию вызывающего и дескрипторы провайдера/runtime-секретов для проверки доменом с точным RPC principal. В построенном входе этого запроса намеренно нет fence; доступностью и привязкой владеет домен.
func (server *Server) ValidateRuntimeCredentialProjection(ctx context.Context, request *controlplanev1.ValidateRuntimeCredentialProjectionRequest) (*controlplanev1.ValidateRuntimeCredentialProjectionResponse, error) {
	p, err := principal(ctx, controlplanev1.RuntimeSecretWorkService_ValidateRuntimeCredentialProjection_FullMethodName)
	if err != nil {
		return nil, err
	}
	input := runtimeCredentialProjectionInput(request.GetAuthority(), request.GetWorkloadInstance(), request.GetLeaseRef(), "",
		request.GetGeneration(), request.GetRuntimeRevisionRef(), request.GetRuntimeRevisionDigest(), request.GetSessionRef(), request.GetTurnRef(), request.GetAttempt(), request.GetInputDigest())
	input.ProviderCredential = providerCredentialBinding(request.GetProviderCredential())
	for _, item := range request.GetRuntimeSecrets() {
		input.RuntimeSecrets = append(input.RuntimeSecrets, runtimeSecretProjectionBinding(item))
	}
	valid, err := server.service.ValidateRuntimeCredentialProjection(ctx, p, input)
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.ValidateRuntimeCredentialProjectionResponse{Valid: valid}, nil
}

// ResolveTranscriptionCredentialProjection resolves the transcription provider descriptor using the verified RPC identity, authority proof and exact account/config generation pins; only the returned descriptor and expiry enter the response.
// ResolveTranscriptionCredentialProjection разрешает дескриптор провайдера распознавания с проверенной RPC identity, proof полномочий и точными account/config generation pins; в ответ попадают лишь возвращённый дескриптор и срок.
func (server *Server) ResolveTranscriptionCredentialProjection(ctx context.Context, request *controlplanev1.ResolveTranscriptionCredentialProjectionRequest) (*controlplanev1.ResolveTranscriptionCredentialProjectionResponse, error) {
	p, err := principal(ctx, controlplanev1.RuntimeSecretWorkService_ResolveTranscriptionCredentialProjection_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.ResolveTranscriptionCredentialProjection(ctx, p, platformrepo.TranscriptionCredentialProjectionInput{
		Authority: credentialProjectionAuthority(request.GetAuthority()), ProviderAccountRef: request.GetProviderAccountRef(),
		ProviderCredentialGeneration: request.GetProviderCredentialGeneration(), ConfigRevision: request.GetConfigRevision(),
		ConfigDigestSHA256: request.GetConfigDigestSha256(),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.ResolveTranscriptionCredentialProjectionResponse{
		ProviderCredential: castProviderCredentialBinding(result.ProviderCredential), ExpiresAt: timestamp(result.ExpiresAt),
	}, nil
}

// runtimeCredentialProjectionInput copies authority and immutable workload/lease/revision/session/turn/attempt/input pins into the domain input without normalizing or verifying them; the service must validate provenance and current eligibility.
// runtimeCredentialProjectionInput копирует полномочия и неизменяемые workload/lease/revision/session/turn/attempt/input pins в доменный вход без нормализации и проверки; происхождение и текущую доступность проверяет сервис.
func runtimeCredentialProjectionInput(authority *controlplanev1.CredentialProjectionAuthority, workload, lease, fence string, generation int64, revisionRef, revisionDigest, sessionRef, turnRef string, attempt int32, inputDigest string) platformrepo.RuntimeCredentialProjectionInput {
	return platformrepo.RuntimeCredentialProjectionInput{Authority: credentialProjectionAuthority(authority), WorkloadInstance: workload,
		LeaseRef: lease, Fence: fence, Generation: generation, RuntimeRevisionRef: revisionRef, RuntimeRevisionDigest: revisionDigest,
		SessionRef: sessionRef, TurnRef: turnRef, Attempt: attempt, InputDigest: inputDigest}
}

// credentialProjectionAuthority maps the proof DTO, returning an empty value for nil or an invalid explicit expiry and normalizing valid expiry to UTC. Copying actor/tenant/project fields is not authority verification; the domain validates the proof against trusted context.
// credentialProjectionAuthority переносит DTO proof, возвращая пустое значение при nil или явно неверном сроке и приводя допустимый срок к UTC. Копирование actor/tenant/project не проверяет полномочия: домен сверяет proof с доверенным контекстом.
func credentialProjectionAuthority(input *controlplanev1.CredentialProjectionAuthority) platformrepo.CredentialProjectionAuthority {
	if input == nil || (input.GetExpiresAt() != nil && input.GetExpiresAt().CheckValid() != nil) {
		return platformrepo.CredentialProjectionAuthority{}
	}
	result := platformrepo.CredentialProjectionAuthority{RPCProfile: input.GetRpcProfile(), ActorID: input.GetActorId(), TenantID: input.GetTenantId(), ProjectID: input.GetProjectId(),
		SourceRevision: input.GetSourceRevision(), SourceDigestSHA256: input.GetSourceDigestSha256(), ProofJTI: input.GetProofJti(),
		CallerWorkloadID: input.GetCallerWorkloadId(), CallerFullMethod: input.GetCallerFullMethod(),
		CallerCredentialRevision: input.GetCallerCredentialRevision()}
	if input.GetExpiresAt() != nil {
		result.ExpiresAt = input.GetExpiresAt().AsTime().UTC()
	}
	return result
}

// providerCredentialBinding maps an optional provider revision descriptor to its domain value, using zero for nil and copying identity/digest pins without reading or validating a credential.
// providerCredentialBinding переносит необязательный дескриптор ревизии провайдера в доменное значение, подставляя ноль при nil и копируя identity/digest pins без чтения и проверки учётных данных.
func providerCredentialBinding(input *controlplanev1.ProviderCredentialBinding) platformrepo.ProviderCredentialBinding {
	if input == nil {
		return platformrepo.ProviderCredentialBinding{}
	}
	return platformrepo.ProviderCredentialBinding{AccountRef: input.GetAccountRef(), CredentialRevisionRef: input.GetCredentialRevisionRef(),
		CredentialRevision: input.GetCredentialRevision(), SecretName: input.GetSecretName(), SecretUID: input.GetSecretUid(),
		SecretResourceVersion: input.GetSecretResourceVersion(), ContentSHA256: input.GetContentSha256()}
}

// castProviderCredentialBinding encodes domain-issued provider account/revision and Kubernetes object/digest pins into a descriptor; it never fetches or includes provider credential bytes.
// castProviderCredentialBinding кодирует выданные доменом account/revision и Kubernetes object/digest pins в дескриптор; не запрашивает и не включает байты учётных данных провайдера.
func castProviderCredentialBinding(input platformrepo.ProviderCredentialBinding) *controlplanev1.ProviderCredentialBinding {
	return &controlplanev1.ProviderCredentialBinding{AccountRef: input.AccountRef, CredentialRevisionRef: input.CredentialRevisionRef,
		CredentialRevision: input.CredentialRevision, SecretName: input.SecretName, SecretUid: input.SecretUID,
		SecretResourceVersion: input.SecretResourceVersion, ContentSha256: input.ContentSHA256}
}

// runtimeSecretProjectionBinding maps an optional named runtime-secret projection to its exact stored revision descriptor, preserving nil as a zero domain value for later validation.
// runtimeSecretProjectionBinding переносит необязательную именованную проекцию runtime-секрета в точный дескриптор сохранённой ревизии, сохраняя nil как нулевое доменное значение для последующей проверки.
func runtimeSecretProjectionBinding(input *controlplanev1.RuntimeSecretDescriptor) platformrepo.RuntimeSecretProjectionBinding {
	if input == nil {
		return platformrepo.RuntimeSecretProjectionBinding{}
	}
	return platformrepo.RuntimeSecretProjectionBinding{Name: input.GetName(), SecretRef: input.GetSecretRef(), Descriptor: entity.RuntimeSecretRevisionDescriptor{
		Revision: input.GetRevision(), Namespace: input.GetNamespace(), SecretName: input.GetSecretName(), SecretKey: input.GetSecretKey(),
		SecretUID: input.GetSecretUid(), SecretResourceVersion: input.GetSecretResourceVersion(), ContentSHA256: input.GetContentSha256(),
	}}
}

// castRuntimeSecretProjectionBinding encodes the domain projection name/ref and exact revision/object/digest tuple without inferring ownership or exposing the referenced secret value.
// castRuntimeSecretProjectionBinding кодирует доменные name/ref проекции и точную комбинацию revision/object/digest без предположения о владении и раскрытия значения секрета.
func castRuntimeSecretProjectionBinding(input platformrepo.RuntimeSecretProjectionBinding) *controlplanev1.RuntimeSecretDescriptor {
	descriptor := input.Descriptor
	return &controlplanev1.RuntimeSecretDescriptor{Name: input.Name, SecretRef: input.SecretRef, Revision: descriptor.Revision,
		Namespace: descriptor.Namespace, SecretName: descriptor.SecretName, SecretKey: descriptor.SecretKey,
		SecretUid: descriptor.SecretUID, SecretResourceVersion: descriptor.SecretResourceVersion, ContentSha256: descriptor.ContentSHA256}
}

// castRuntimeCredentialProjection maps the resolved provider and runtime-secret descriptors plus expiry, including authority only when its RPC profile is present. It does not manufacture a proof for an empty domain authority.
// castRuntimeCredentialProjection переносит разрешённые дескрипторы провайдера/runtime-секретов и срок, включая полномочия лишь при наличии RPC profile. Для пустых доменных полномочий proof не создаёт.
func castRuntimeCredentialProjection(input platformrepo.RuntimeCredentialProjection) *controlplanev1.ResolveRuntimeCredentialProjectionResponse {
	response := &controlplanev1.ResolveRuntimeCredentialProjectionResponse{ProviderCredential: castProviderCredentialBinding(input.ProviderCredential), ExpiresAt: timestamp(input.ExpiresAt)}
	if authority := input.Authority; authority.RPCProfile != "" {
		response.Authority = &controlplanev1.CredentialProjectionAuthority{
			RpcProfile: authority.RPCProfile, ActorId: authority.ActorID, TenantId: authority.TenantID, ProjectId: authority.ProjectID,
			SourceRevision: authority.SourceRevision, SourceDigestSha256: authority.SourceDigestSHA256,
			ProofJti: authority.ProofJTI, CallerWorkloadId: authority.CallerWorkloadID, CallerFullMethod: authority.CallerFullMethod,
			CallerCredentialRevision: authority.CallerCredentialRevision, ExpiresAt: timestamp(authority.ExpiresAt),
		}
	}
	for _, item := range input.RuntimeSecrets {
		response.RuntimeSecrets = append(response.RuntimeSecrets, castRuntimeSecretProjectionBinding(item))
	}
	return response
}

// ListRuntimeSecretRecoveryWork reads optional recovery pagination under the exact broker RPC principal and maps the domain page to operation descriptors. It does not claim work, choose a recovery action or reveal a secret value.
// ListRuntimeSecretRecoveryWork читает необязательную страницу восстановления с principal точного RPC брокера и переносит доменную страницу в дескрипторы операций. Не захватывает работу, не выбирает действие восстановления и не раскрывает значение секрета.
func (server *Server) ListRuntimeSecretRecoveryWork(ctx context.Context, request *controlplanev1.ListRuntimeSecretRecoveryWorkRequest) (*controlplanev1.ListRuntimeSecretRecoveryWorkResponse, error) {
	p, err := principal(ctx, controlplanev1.RuntimeSecretWorkService_ListRuntimeSecretRecoveryWork_FullMethodName)
	if err != nil {
		return nil, err
	}
	requestedPage := request.GetPage()
	page := platformrepo.RuntimeSecretRecoveryPage{}
	if requestedPage != nil {
		page.Size = requestedPage.GetPageSize()
		page.Token = requestedPage.GetPageToken()
	}
	items, next, err := server.service.ListRuntimeSecretRecoveryWork(ctx, p, page)
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListRuntimeSecretRecoveryWorkResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Operations = append(response.Operations, &controlplanev1.RuntimeSecretRecoveryWork{
			OperationRef: item.OperationRef, Kind: runtimeSecretOperationKind(item.Kind),
			ClaimantId: item.ClaimantID, ClaimGeneration: item.ClaimGeneration,
			Namespace: item.Namespace, SecretRef: item.SecretRef, TargetRevision: item.TargetRevision,
			SecretKey: item.SecretKey, ExpectedContentSha256: item.ExpectedContentSHA256,
		})
	}
	return response, nil
}

// ListRuntimeSecrets maps project/query/page to the domain metadata catalog under its exact query principal and returns typed secrets with the next cursor; visibility and action eligibility remain domain-owned.
// ListRuntimeSecrets переносит project/query/page в доменный каталог метаданных с principal точного query RPC и возвращает типизированные секреты с курсором; видимость и доступность действий определяет домен.
func (server *Server) ListRuntimeSecrets(ctx context.Context, request *controlplanev1.ListRuntimeSecretsRequest) (*controlplanev1.ListRuntimeSecretsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListRuntimeSecrets_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListRuntimeSecrets(ctx, p, query.Filter{ProjectRef: request.GetProjectRef(), Query: request.GetQuery(), Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListRuntimeSecretsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	response.Secrets = castResponseItems(items, castRuntimeSecret)
	return response, nil
}

// GetRuntimeSecret uses the exact query RPC principal to read one secret metadata record and translates domain errors; obtaining metadata does not reveal its stored value.
// GetRuntimeSecret с principal точного query RPC читает метаданные одного секрета и преобразует доменные ошибки; получение метаданных не раскрывает сохранённое значение.
func (server *Server) GetRuntimeSecret(ctx context.Context, request *controlplanev1.GetRuntimeSecretRequest) (*controlplanev1.GetRuntimeSecretResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetRuntimeSecret_FullMethodName)
	if err != nil {
		return nil, err
	}
	item, err := server.service.GetRuntimeSecret(ctx, p, request.GetSecretRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetRuntimeSecretResponse{Secret: castRuntimeSecret(item)}, nil
}

// PrepareCreateRuntimeSecret constructs a CREATE preparation with project metadata, type, expected content digest and mutation receipt, delegating identity/error mapping to the shared prepare helper. It does not create Kubernetes secret bytes.
// PrepareCreateRuntimeSecret строит подготовку CREATE с метаданными проекта, типом, ожидаемым digest и квитанцией изменения, передавая identity/ошибки общему helper подготовки. Байты секрета Kubernetes не создаёт.
func (server *Server) PrepareCreateRuntimeSecret(ctx context.Context, request *controlplanev1.PrepareCreateRuntimeSecretRequest) (*controlplanev1.PrepareCreateRuntimeSecretResponse, error) {
	operation, err := server.prepareRuntimeSecret(ctx, controlplanev1.PlatformCommandService_PrepareCreateRuntimeSecret_FullMethodName, platformrepo.RuntimeSecretPrepareInput{
		Kind: "CREATE", ProjectRef: request.GetProjectRef(), Name: request.GetName(), Description: request.GetDescription(),
		ValueType: runtimeSecretValueTypeName(request.GetValueType()), ExpectedContentSHA256: request.GetExpectedContentSha256(), Mutation: mutation(request.GetMutation()),
	})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.PrepareCreateRuntimeSecretResponse{Operation: operation}, err
}

// PrepareRotateRuntimeSecret constructs a ROTATE preparation for one secret with its type, expected content digest and mutation receipt; the shared domain path validates ownership and revision before issuing a grant.
// PrepareRotateRuntimeSecret строит подготовку ROTATE для одного секрета с типом, ожидаемым digest и квитанцией изменения; общий доменный путь проверяет владение и ревизию до выдачи grant.
func (server *Server) PrepareRotateRuntimeSecret(ctx context.Context, request *controlplanev1.PrepareRotateRuntimeSecretRequest) (*controlplanev1.PrepareRotateRuntimeSecretResponse, error) {
	operation, err := server.prepareRuntimeSecret(ctx, controlplanev1.PlatformCommandService_PrepareRotateRuntimeSecret_FullMethodName, platformrepo.RuntimeSecretPrepareInput{
		Kind: "ROTATE", SecretRef: request.GetSecretRef(), ValueType: runtimeSecretValueTypeName(request.GetValueType()),
		ExpectedContentSHA256: request.GetExpectedContentSha256(), Mutation: mutation(request.GetMutation()),
	})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.PrepareRotateRuntimeSecretResponse{Operation: operation}, err
}

// PrepareRevealRuntimeSecret constructs a REVEAL preparation under its distinct RPC method and mutation receipt; fresh credential authentication and reveal authorization are checked by the domain, not inferred from the ref.
// PrepareRevealRuntimeSecret строит подготовку REVEAL с отдельным RPC методом и квитанцией изменения; свежесть аутентификации и полномочие раскрытия проверяет домен, а не ссылка.
func (server *Server) PrepareRevealRuntimeSecret(ctx context.Context, request *controlplanev1.PrepareRevealRuntimeSecretRequest) (*controlplanev1.PrepareRevealRuntimeSecretResponse, error) {
	operation, err := server.prepareRuntimeSecret(ctx, controlplanev1.PlatformCommandService_PrepareRevealRuntimeSecret_FullMethodName, platformrepo.RuntimeSecretPrepareInput{
		Kind: "REVEAL", SecretRef: request.GetSecretRef(), Mutation: mutation(request.GetMutation()),
	})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.PrepareRevealRuntimeSecretResponse{Operation: operation}, err
}

// PrepareRevokeRuntimeSecret constructs a REVOKE preparation under its distinct RPC method; reference eligibility, versions and actual broker revocation remain separate domain/work commands.
// PrepareRevokeRuntimeSecret строит подготовку REVOKE с отдельным RPC методом; доступность по ссылкам, версии и фактический отзыв брокером остаются отдельными доменными/рабочими командами.
func (server *Server) PrepareRevokeRuntimeSecret(ctx context.Context, request *controlplanev1.PrepareRevokeRuntimeSecretRequest) (*controlplanev1.PrepareRevokeRuntimeSecretResponse, error) {
	operation, err := server.prepareRuntimeSecret(ctx, controlplanev1.PlatformCommandService_PrepareRevokeRuntimeSecret_FullMethodName, platformrepo.RuntimeSecretPrepareInput{
		Kind: "REVOKE", SecretRef: request.GetSecretRef(), Mutation: mutation(request.GetMutation()),
	})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.PrepareRevokeRuntimeSecretResponse{Operation: operation}, err
}

// prepareRuntimeSecret checks the concrete preparation RPC principal, delegates to the domain and maps the short-lived grant/state/failure receipt with optional terminal metadata. It is shared transport shaping, not a generic owner transition or a secret store.
// prepareRuntimeSecret проверяет principal конкретного RPC подготовки, вызывает домен и переносит короткий grant/state/failure receipt с необязательными итоговыми метаданными. Это общий транспортный mapper, а не универсальный переход владельца или хранилище секрета.
func (server *Server) prepareRuntimeSecret(ctx context.Context, method string, input platformrepo.RuntimeSecretPrepareInput) (*controlplanev1.RuntimeSecretOperationReceipt, error) {
	p, err := principal(ctx, method)
	if err != nil {
		return nil, err
	}
	result, err := server.service.PrepareRuntimeSecretOperation(ctx, p, input)
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.RuntimeSecretOperationReceipt{
		OperationRef: result.OperationRef, OperationGrant: result.OperationGrant, ExpiresAt: timestamp(result.ExpiresAt),
		ValueType: runtimeSecretValueType(result.ValueType), State: runtimeSecretOperationState(result.State),
		FailureCode: runtimeSecretFailureCode(result.FailureCode),
	}
	if result.TerminalSecret != nil {
		response.TerminalSecret = castRuntimeSecret(*result.TerminalSecret)
	}
	return response, nil
}

// ConsumeRuntimeSecretOperation passes the broker grant and claimant through the exact consume RPC identity, then maps the domain-issued claim lease/generation, names and revision descriptors. It copies the names slice and exposes no secret body.
// ConsumeRuntimeSecretOperation передаёт grant брокера и claimant с identity точного consume RPC, затем переносит выданные доменом аренду/поколение захвата, имена и дескрипторы ревизий. Копирует срез имён и не раскрывает тело секрета.
func (server *Server) ConsumeRuntimeSecretOperation(ctx context.Context, request *controlplanev1.ConsumeRuntimeSecretOperationRequest) (*controlplanev1.ConsumeRuntimeSecretOperationResponse, error) {
	p, err := principal(ctx, controlplanev1.RuntimeSecretWorkService_ConsumeRuntimeSecretOperation_FullMethodName)
	if err != nil {
		return nil, err
	}
	item, err := server.service.ConsumeRuntimeSecretOperation(ctx, p, platformrepo.RuntimeSecretConsumeInput{
		OperationGrant: request.GetOperationGrant(), ClaimantID: request.GetClaimantId(),
	})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ConsumeRuntimeSecretOperationResponse{
		ScopeKind: runtimeSecretScopeKind(item.ScopeKind), OrganizationRef: item.OrganizationRef,
		OperationRef: item.Ref, Kind: runtimeSecretOperationKind(item.Kind), ProjectRef: item.ProjectRef,
		SecretRef: item.SecretRef, Name: item.Name, Description: item.Description,
		ValueType: runtimeSecretValueType(item.ValueType), Namespace: item.Namespace,
		TargetRevision: item.TargetRevision, VersionedSecretNames: append([]string(nil), item.VersionedSecretNames...),
		SecretKey: item.SecretKey, ExpiresAt: timestamp(item.ExpiresAt), ClaimGeneration: item.ClaimGeneration,
		LeaseDeadline: timestamp(item.LeaseDeadline), ExpectedContentSha256: item.ExpectedContentSHA256,
	}
	for _, descriptor := range item.RevisionDescriptors {
		response.RevisionDescriptors = append(response.RevisionDescriptors, castRuntimeSecretRevisionDescriptor(descriptor))
	}
	return response, nil
}

// CompleteRuntimeSecretOperation maps the exact broker completion principal, operation/claim generation and optional materialization to the domain command, returning only the resulting typed secret metadata.
// CompleteRuntimeSecretOperation переносит точный completion principal брокера, операцию/поколение захвата и необязательную материализацию в доменную команду, возвращая лишь итоговые метаданные секрета.
func (server *Server) CompleteRuntimeSecretOperation(ctx context.Context, request *controlplanev1.CompleteRuntimeSecretOperationRequest) (*controlplanev1.CompleteRuntimeSecretOperationResponse, error) {
	p, err := principal(ctx, controlplanev1.RuntimeSecretWorkService_CompleteRuntimeSecretOperation_FullMethodName)
	if err != nil {
		return nil, err
	}
	item, err := server.service.CompleteRuntimeSecretOperation(ctx, p, platformrepo.RuntimeSecretCompleteInput{
		OperationRef: request.GetOperationRef(), ClaimantID: request.GetClaimantId(), ClaimGeneration: request.GetClaimGeneration(),
		Materialization: runtimeSecretMaterialization(request.GetMaterialization()),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.CompleteRuntimeSecretOperationResponse{Secret: castRuntimeSecret(item)}, nil
}

// FailRuntimeSecretOperation maps the broker failure principal, operation/claim pins and enum reason to the domain command; the response reflects its state/reason receipt without performing Kubernetes cleanup here.
// FailRuntimeSecretOperation переносит principal ошибки брокера, operation/claim pins и причину enum в доменную команду; ответ отражает квитанцию state/reason без очистки Kubernetes в этом методе.
func (server *Server) FailRuntimeSecretOperation(ctx context.Context, request *controlplanev1.FailRuntimeSecretOperationRequest) (*controlplanev1.FailRuntimeSecretOperationResponse, error) {
	p, err := principal(ctx, controlplanev1.RuntimeSecretWorkService_FailRuntimeSecretOperation_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.FailRuntimeSecretOperation(ctx, p, platformrepo.RuntimeSecretFailInput{
		OperationRef: request.GetOperationRef(), ClaimantID: request.GetClaimantId(), ClaimGeneration: request.GetClaimGeneration(),
		FailureCode: runtimeSecretFailureCodeName(request.GetFailureCode()),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.FailRuntimeSecretOperationResponse{
		OperationRef: result.OperationRef, State: runtimeSecretOperationState(result.State),
		FailureCode: runtimeSecretFailureCode(result.FailureCode),
	}, nil
}

// RecoverRuntimeSecretMaterialization rejects an absent materialization after exact RPC authentication, delegates recovery of one operation and maps the server-selected KEEP/DELETE action with optional secret metadata; the caller cannot assign the action.
// RecoverRuntimeSecretMaterialization после точной RPC-аутентификации отклоняет отсутствующую материализацию, передаёт восстановление одной операции и переносит назначенное сервером KEEP/DELETE с необязательными метаданными; вызывающий действие не назначает.
func (server *Server) RecoverRuntimeSecretMaterialization(ctx context.Context, request *controlplanev1.RecoverRuntimeSecretMaterializationRequest) (*controlplanev1.RecoverRuntimeSecretMaterializationResponse, error) {
	p, err := principal(ctx, controlplanev1.RuntimeSecretWorkService_RecoverRuntimeSecretMaterialization_FullMethodName)
	if err != nil {
		return nil, err
	}
	materialization := runtimeSecretMaterialization(request.GetMaterialization())
	if materialization == nil {
		return nil, transportError(errs.ErrInvalid)
	}
	result, err := server.service.RecoverRuntimeSecretMaterialization(ctx, p, platformrepo.RuntimeSecretRecoveryInput{
		OperationRef: request.GetOperationRef(), Materialization: *materialization,
	})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.RecoverRuntimeSecretMaterializationResponse{
		Action: runtimeSecretRecoveryAction(result.Action), OperationState: runtimeSecretOperationState(result.OperationState),
	}
	if result.Secret != nil {
		response.Secret = castRuntimeSecret(*result.Secret)
	}
	return response, nil
}

// runtimeSecretScopeKind maps the prefixed generated scope enum by name; an unrecognized string yields the generated zero value rather than a guessed project/organization scope.
// runtimeSecretScopeKind сопоставляет область с generated enum по имени с префиксом; неизвестная строка даёт нулевое значение, а не предполагаемую область проекта/организации.
func runtimeSecretScopeKind(kind string) controlplanev1.RuntimeResourceScopeKind {
	return controlplanev1.RuntimeResourceScopeKind(controlplanev1.RuntimeResourceScopeKind_value["RUNTIME_RESOURCE_SCOPE_KIND_"+kind])
}

// ListOrganizationRuntimeSecrets assigns ORGANIZATION scope in the server-owned domain filter and uses its distinct query RPC principal; only authorized metadata and the domain next cursor are returned.
// ListOrganizationRuntimeSecrets назначает ORGANIZATION в принадлежащем серверу доменном фильтре и использует principal отдельного query RPC; возвращает лишь разрешённые метаданные и доменный курсор.
func (server *Server) ListOrganizationRuntimeSecrets(ctx context.Context, request *controlplanev1.ListOrganizationRuntimeSecretsRequest) (*controlplanev1.ListOrganizationRuntimeSecretsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListOrganizationRuntimeSecrets_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListRuntimeSecrets(ctx, p, query.Filter{RuntimeResourceScopeKind: "ORGANIZATION", Query: request.GetQuery(), Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListOrganizationRuntimeSecretsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	response.Secrets = castResponseItems(items, castRuntimeSecret)
	return response, nil
}

// castRuntimeSecret encodes secret metadata, optional hints/current revision descriptor and action hints without a plaintext value. Returned nextActions are presentation data, not a substitute for command authorization.
// castRuntimeSecret кодирует метаданные секрета, необязательные подсказки/дескриптор текущей ревизии и действия без открытого значения. NextActions — данные представления, а не замена авторизации команды.
func castRuntimeSecret(value entity.RuntimeSecret) *controlplanev1.RuntimeSecret {
	result := &controlplanev1.RuntimeSecret{
		ScopeKind: runtimeSecretScopeKind(value.ScopeKind), OrganizationRef: value.OrganizationRef,
		Ref: value.Ref, Version: value.Version, ProjectRef: value.ProjectRef, Name: value.Name,
		Description: value.Description, ValueType: runtimeSecretValueType(value.ValueType), State: value.State,
		CurrentRevision: value.CurrentRevision, CreatedAt: timestamp(value.CreatedAt), UpdatedAt: timestamp(value.UpdatedAt), Namespace: value.Namespace,
		NextActions: nextActions(value.NextActions),
	}
	if value.DisplayHint != nil {
		result.DisplayHint = &controlplanev1.RuntimeSecretDisplayHint{Prefix: value.DisplayHint.Prefix, Suffix: value.DisplayHint.Suffix}
	}
	if value.CurrentRevisionDescriptor != nil {
		result.CurrentRevisionDescriptor = castRuntimeSecretRevisionDescriptor(*value.CurrentRevisionDescriptor)
	}
	return result
}

// castRuntimeSecretRevisionDescriptor copies the exact revision, Kubernetes namespace/name/key/UID/resourceVersion and digest into the wire descriptor; it performs neither readback nor validation.
// castRuntimeSecretRevisionDescriptor копирует точные revision, Kubernetes namespace/name/key/UID/resourceVersion и digest в wire-дескриптор; чтение и проверку не выполняет.
func castRuntimeSecretRevisionDescriptor(value entity.RuntimeSecretRevisionDescriptor) *controlplanev1.RuntimeSecretRevisionDescriptor {
	return &controlplanev1.RuntimeSecretRevisionDescriptor{
		Revision: value.Revision, Namespace: value.Namespace, SecretName: value.SecretName, SecretKey: value.SecretKey,
		SecretUid: value.SecretUID, SecretResourceVersion: value.SecretResourceVersion, ContentSha256: value.ContentSHA256,
	}
}

// runtimeSecretMaterialization maps optional broker materialization and display hints to domain data, preserving nil. Object identity, expected digest and hint limits are validated by the owner command.
// runtimeSecretMaterialization переносит необязательную материализацию брокера и display hints в доменные данные, сохраняя nil. Identity объекта, ожидаемый digest и пределы подсказок проверяет команда владельца.
func runtimeSecretMaterialization(value *controlplanev1.RuntimeSecretMaterialization) *entity.RuntimeSecretMaterialization {
	if value == nil {
		return nil
	}
	result := &entity.RuntimeSecretMaterialization{
		Namespace: value.GetNamespace(), SecretName: value.GetSecretName(), SecretKey: value.GetSecretKey(),
		SecretUID: value.GetSecretUid(), SecretResourceVersion: value.GetSecretResourceVersion(), ContentSHA256: value.GetContentSha256(),
	}
	if hint := value.GetDisplayHint(); hint != nil {
		result.DisplayHint = &entity.RuntimeSecretDisplayHint{Prefix: hint.GetPrefix(), Suffix: hint.GetSuffix()}
	}
	return result
}

// runtimeSecretValueType maps only STRING/BINARY/JSON domain strings to the corresponding generated enum and returns UNSPECIFIED for all other values.
// runtimeSecretValueType переносит лишь доменные STRING/BINARY/JSON в соответствующий generated enum, возвращая UNSPECIFIED для прочих значений.
func runtimeSecretValueType(value string) controlplanev1.RuntimeSecretValueType {
	switch value {
	case "STRING":
		return controlplanev1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_STRING
	case "BINARY":
		return controlplanev1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_BINARY
	case "JSON":
		return controlplanev1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_JSON
	default:
		return controlplanev1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_UNSPECIFIED
	}
}

// runtimeSecretValueTypeName removes the known generated value-type prefix through enumSuffix without treating an unknown numeric enum or UNSPECIFIED as a supported secret type.
// runtimeSecretValueTypeName удаляет известный префикс generated типа через enumSuffix, не превращая неизвестный числовой enum или UNSPECIFIED в поддержанный тип секрета.
func runtimeSecretValueTypeName(value controlplanev1.RuntimeSecretValueType) string {
	return enumSuffix(value, "RUNTIME_SECRET_VALUE_TYPE_")
}

// runtimeSecretOperationKind maps CREATE/ROTATE/REVEAL/REVOKE to typed wire operation kinds, using UNSPECIFIED for an unsupported domain string.
// runtimeSecretOperationKind переносит CREATE/ROTATE/REVEAL/REVOKE в типизированные wire виды операции, подставляя UNSPECIFIED для неподдержанной строки.
func runtimeSecretOperationKind(value string) controlplanev1.RuntimeSecretOperationKind {
	switch value {
	case "CREATE":
		return controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_CREATE
	case "ROTATE":
		return controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_ROTATE
	case "REVEAL":
		return controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_REVEAL
	case "REVOKE":
		return controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_REVOKE
	default:
		return controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_UNSPECIFIED
	}
}

// runtimeSecretOperationState maps the four supported operation states to generated enum values without guessing a transition for an unknown state.
// runtimeSecretOperationState переносит четыре поддержанных состояния операции в generated enum без предположения о переходе для неизвестного состояния.
func runtimeSecretOperationState(value string) controlplanev1.RuntimeSecretOperationState {
	switch value {
	case "PREPARED":
		return controlplanev1.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_PREPARED
	case "CLAIMED":
		return controlplanev1.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_CLAIMED
	case "COMPLETED":
		return controlplanev1.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_COMPLETED
	case "FAILED":
		return controlplanev1.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_FAILED
	default:
		return controlplanev1.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_UNSPECIFIED
	}
}

// runtimeSecretFailureCode maps the supported broker and owner-expiry failure strings to the wire enum, preserving unknown input as UNSPECIFIED rather than accepting an arbitrary diagnostic.
// runtimeSecretFailureCode переносит поддержанные ошибки брокера и истечения владельца в wire enum, сохраняя неизвестный вход как UNSPECIFIED вместо произвольной диагностики.
func runtimeSecretFailureCode(value string) controlplanev1.RuntimeSecretFailureCode {
	switch value {
	case "KUBERNETES_UNAVAILABLE":
		return controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_KUBERNETES_UNAVAILABLE
	case "MATERIALIZATION_CONFLICT":
		return controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_MATERIALIZATION_CONFLICT
	case "MATERIALIZATION_INVALID":
		return controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_MATERIALIZATION_INVALID
	case "STALE_SECRET_VERSION":
		return controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_STALE_SECRET_VERSION
	case "RECONCILIATION_FAILED":
		return controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_RECONCILIATION_FAILED
	case "GRANT_EXPIRED":
		return controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_GRANT_EXPIRED
	default:
		return controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_UNSPECIFIED
	}
}

// runtimeSecretFailureCodeName extracts the generated failure enum suffix for domain validation; it does not independently accept unknown numbers or expand the broker failure allowlist.
// runtimeSecretFailureCodeName извлекает суффикс generated enum ошибки для доменной проверки; самостоятельно неизвестные числа не принимает и разрешённый набор ошибок брокера не расширяет.
func runtimeSecretFailureCodeName(value controlplanev1.RuntimeSecretFailureCode) string {
	return enumSuffix(value, "RUNTIME_SECRET_FAILURE_CODE_")
}

// runtimeSecretRecoveryAction maps only KEEP and DELETE to wire recovery actions, defaulting to UNSPECIFIED so unknown domain actions are not interpreted as permission to retain or delete.
// runtimeSecretRecoveryAction переносит лишь KEEP и DELETE в wire действия восстановления, иначе возвращая UNSPECIFIED без трактовки неизвестного действия как разрешения сохранить или удалить.
func runtimeSecretRecoveryAction(value string) controlplanev1.RuntimeSecretRecoveryAction {
	switch value {
	case "KEEP":
		return controlplanev1.RuntimeSecretRecoveryAction_RUNTIME_SECRET_RECOVERY_ACTION_KEEP
	case "DELETE":
		return controlplanev1.RuntimeSecretRecoveryAction_RUNTIME_SECRET_RECOVERY_ACTION_DELETE
	default:
		return controlplanev1.RuntimeSecretRecoveryAction_RUNTIME_SECRET_RECOVERY_ACTION_UNSPECIFIED
	}
}
