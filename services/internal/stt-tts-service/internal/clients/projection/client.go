// Package projection adapts generated owner policy and credential RPCs to STT domain ports while checking exact delegated-authority readback.
// RU: Пакет projection адаптирует generated RPC владельцев политики и реквизитов к портам домена STT с проверкой точного ответа делегированных полномочий.
package projection

import (
	"context"
	"errors"
	"time"

	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/libs/go/sttapi/modelprofile"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	// credentialProjectionIncomplete provides a fixed diagnostic for missing or incorrectly bound credential response metadata.
	// RU: credentialProjectionIncomplete задаёт фиксированную диагностику отсутствующих или неверно привязанных метаданных ответа реквизитов.
	credentialProjectionIncomplete = "transcription credential projection is incomplete"
	// policyProjectionIncomplete provides a fixed diagnostic for invalid policy readback or numeric limits outside the shared model profile.
	// RU: policyProjectionIncomplete задаёт фиксированную диагностику неверного ответа политики или числовых лимитов вне общего профиля моделей.
	policyProjectionIncomplete = "transcription policy projection is incomplete"
	// minimumAudioBytes is the unsigned lower input-size boundary checked before conversion into domain integers.
	// RU: minimumAudioBytes задаёт беззнаковую нижнюю границу размера ввода до преобразования в доменные целые числа.
	minimumAudioBytes = uint64(modelprofile.MinimumAudioBytes)
	// maximumAudioBytes is the shared unsigned upper audio-size boundary, preventing overflow during signed conversion.
	// RU: maximumAudioBytes задаёт общую беззнаковую верхнюю границу размера аудио, предотвращая переполнение при знаковом преобразовании.
	maximumAudioBytes = uint64(modelprofile.MaximumAudioBytes)
	// minimumAudioDurationMillis expresses the shared minimum audio duration in the projection wire unit.
	// RU: minimumAudioDurationMillis выражает общую минимальную длительность аудио в единице wire-проекции.
	minimumAudioDurationMillis = uint64(modelprofile.MinimumAudioDuration / time.Millisecond)
	// maximumAudioDurationMillis caps unsigned duration values before multiplying them into a time.Duration.
	// RU: maximumAudioDurationMillis ограничивает беззнаковую длительность до её умножения в time.Duration.
	maximumAudioDurationMillis = uint64(modelprofile.MaximumAudioDuration / time.Millisecond)
	// minimumProviderTimeoutMillis preserves the shared minimum provider timeout when accepting owner policy.
	// RU: minimumProviderTimeoutMillis сохраняет общий минимальный timeout провайдера при приёме политики владельца.
	minimumProviderTimeoutMillis = uint64(modelprofile.MinimumProviderTimeout / time.Millisecond)
	// maximumProviderTimeoutMillis rejects out-of-range unsigned provider budgets before domain duration conversion.
	// RU: maximumProviderTimeoutMillis отклоняет чрезмерные беззнаковые бюджеты провайдера до преобразования в доменную длительность.
	maximumProviderTimeoutMillis = uint64(modelprofile.MaximumProviderTimeout / time.Millisecond)
)

// DelegatedBinder requires each owner projection to derive outbound authorization from the accepted inbound request rather than payload identity.
// RU: DelegatedBinder требует выводить исходящую авторизацию каждой owner-проекции из принятого входящего запроса, а не идентичности в payload.
type DelegatedBinder interface {
	// BindDelegated validates principal/request binding for the exact method and operation, returning its authorized child context or an error.
	// RU: BindDelegated проверяет привязку субъекта и запроса к точным методу и операции, возвращая разрешённый дочерний контекст либо ошибку.
	BindDelegated(context.Context, value.Principal, string, string, string, string) (context.Context, error)
}

// Policy combines a typed control-plane projection client with the binder that owns delegated request authority.
// RU: Policy объединяет типизированный клиент проекции control-plane с binder, владеющим делегированными полномочиями запроса.
type Policy struct {
	client sttv1.TranscriptionPolicyProjectionServiceClient
	binder DelegatedBinder
}

// protectedPathChecker is the optional binder readiness port; lack of it means delegated proof is pending, not ready.
// RU: protectedPathChecker задаёт необязательный readiness порт binder; его отсутствие означает ожидание delegated proof, а не готовность.
type protectedPathChecker interface {
	// Check probes the binder's protected infrastructure path without performing a business transcription.
	// RU: Check проверяет защищённый инфраструктурный путь binder без выполнения пользовательского распознавания.
	Check(context.Context) error
}

// NewPolicy rejects absent RPC/binder dependencies and performs no owner request during construction.
// RU: NewPolicy отклоняет отсутствующие зависимости RPC/binder и не обращается к владельцу при создании.
func NewPolicy(client sttv1.TranscriptionPolicyProjectionServiceClient, binder DelegatedBinder) (*Policy, error) {
	if client == nil || binder == nil {
		return nil, errors.New("transcription policy projection dependencies are required")
	}
	return &Policy{client: client, binder: binder}, nil
}

// Check delegates to the binder's protected-path checker or reports pending proof when that optional port is unavailable.
// RU: Check вызывает проверку защищённого пути binder либо сообщает ожидание proof при отсутствии необязательного порта.
func (client *Policy) Check(ctx context.Context) error {
	checker, ok := client.binder.(protectedPathChecker)
	if !ok {
		return errs.ErrDelegatedProofPending
	}
	return checker.Check(ctx)
}

// Resolve binds the exact policy operation, sends one authority locator and verifies its complete echo, expiry shape and bounded unsigned policy limits.
// RU: Resolve привязывает точную операцию политики, передаёт один authority locator и проверяет его полный повтор в ответе, форму срока и ограниченные беззнаковые лимиты политики.
// It copies parameter slices into domain values; downstream domain validation still checks semantic policy completeness and current eligibility.
// RU: Функция копирует срезы параметров в доменные значения; доменная проверка по-прежнему отвечает за смысловую полноту политики и текущий допуск.
func (client *Policy) Resolve(ctx context.Context, principal value.Principal, requestID, correlationID string) (value.Policy, error) {
	bound, err := client.binder.BindDelegated(ctx, principal, requestID, correlationID,
		sttv1.TranscriptionPolicyProjectionService_ResolveTranscriptionPolicy_FullMethodName, "platform.stt.policy.resolve")
	if err != nil {
		return value.Policy{}, err
	}
	locator := authorityLocator(principal, requestID, correlationID)
	response, err := client.client.ResolveTranscriptionPolicy(bound, &sttv1.ResolveTranscriptionPolicyRequest{Authority: locator})
	if err != nil {
		return value.Policy{}, classifyProjectionError(bound, err)
	}
	if response == nil || response.GetExpiresAt() == nil || response.GetExpiresAt().CheckValid() != nil ||
		!sameAuthority(response.GetAuthority(), locator) {
		return value.Policy{}, errors.New(policyProjectionIncomplete)
	}
	maximumBytes := response.GetMaximumAudioBytes()
	maximumDuration := response.GetMaximumAudioDurationMilliseconds()
	providerTimeout := response.GetProviderTimeoutMilliseconds()
	if maximumBytes < minimumAudioBytes || maximumBytes > maximumAudioBytes ||
		maximumDuration < minimumAudioDurationMillis || maximumDuration > maximumAudioDurationMillis ||
		providerTimeout < minimumProviderTimeoutMillis || providerTimeout > maximumProviderTimeoutMillis {
		return value.Policy{}, errors.New(policyProjectionIncomplete)
	}
	return value.Policy{
		Revision: response.GetConfigRevision(), DigestSHA256: response.GetConfigDigestSha256(),
		Model: response.GetModel(), Language: response.GetLanguage(), MaximumAudioBytes: int64(maximumBytes),
		Parameters: modelprofile.Parameters{Languages: append([]string(nil), response.GetParameters().GetLanguages()...),
			Keywords: append([]string(nil), response.GetParameters().GetKeywords()...), Prompt: response.GetParameters().GetPrompt(),
			Temperature: response.GetParameters().GetTemperature(), ChunkingStrategy: response.GetParameters().GetChunkingStrategy(), Stream: response.GetParameters().GetStream()},
		MaximumAudioDuration:         time.Duration(maximumDuration) * time.Millisecond,
		ProviderTimeout:              time.Duration(providerTimeout) * time.Millisecond,
		ProviderAccountRef:           response.GetProviderAccountRef(),
		ProviderCredentialGeneration: response.GetProviderCredentialGeneration(),
		ExpiresAt:                    response.GetExpiresAt().AsTime().UTC(),
	}, nil
}

// Credential projects short-lived key bytes through the secret-broker client under the same delegated principal and request binding as policy resolution.
// RU: Credential получает краткоживущие байты ключа через клиент secret-broker с той же делегированной привязкой субъекта и запроса, что у политики.
type Credential struct {
	client sttv1.TranscriptionCredentialProjectionServiceClient
	binder DelegatedBinder
}

// NewCredential requires both the typed secret projection client and the delegated binder without reading a credential during construction.
// RU: NewCredential требует типизированный клиент проекции секрета и delegated binder без чтения реквизита при создании.
func NewCredential(client sttv1.TranscriptionCredentialProjectionServiceClient, binder DelegatedBinder) (*Credential, error) {
	if client == nil || binder == nil {
		return nil, errors.New("transcription credential projection dependencies are required")
	}
	return &Credential{client: client, binder: binder}, nil
}

// Project binds the credential operation, requests the selected account/generation/config pins and verifies authority echo, config revision and expiry shape.
// RU: Project привязывает операцию реквизитов, запрашивает выбранные account/generation/config координаты и проверяет authority echo, ревизию конфигурации и форму срока.
// It copies returned key bytes then clears the RPC buffer; account/generation/digest equality and current expiry are checked again by the domain service.
// RU: Функция копирует байты ключа и очищает RPC буфер; совпадение account/generation/digest и текущий срок повторно проверяет доменный сервис.
func (client *Credential) Project(ctx context.Context, principal value.Principal, requestID, correlationID string, policy value.Policy) (value.Credential, error) {
	bound, err := client.binder.BindDelegated(ctx, principal, requestID, correlationID,
		sttv1.TranscriptionCredentialProjectionService_ProjectTranscriptionCredential_FullMethodName, "platform.stt.credential.project")
	if err != nil {
		return value.Credential{}, err
	}
	locator := authorityLocator(principal, requestID, correlationID)
	response, err := client.client.ProjectTranscriptionCredential(bound, &sttv1.ProjectTranscriptionCredentialRequest{
		Authority: locator, ProviderAccountRef: policy.ProviderAccountRef,
		ProviderCredentialGeneration: policy.ProviderCredentialGeneration,
		ConfigRevision:               policy.Revision, ConfigDigestSha256: policy.DigestSHA256,
	})
	if err != nil {
		return value.Credential{}, classifyProjectionError(bound, err)
	}
	if response == nil {
		return value.Credential{}, errors.New(credentialProjectionIncomplete)
	}
	projectedKey := response.GetApiKey()
	defer clear(projectedKey)
	if response.GetExpiresAt() == nil || response.GetExpiresAt().CheckValid() != nil ||
		response.GetConfigRevision() != policy.Revision || !sameAuthority(response.GetAuthority(), locator) {
		return value.Credential{}, errors.New(credentialProjectionIncomplete)
	}
	return value.Credential{
		APIKey: append([]byte(nil), projectedKey...), ProviderAccountRef: response.GetProviderAccountRef(),
		ProviderCredentialGeneration: response.GetProviderCredentialGeneration(), ConfigDigestSHA256: response.GetConfigDigestSha256(),
		ExpiresAt: response.GetExpiresAt().AsTime().UTC(),
	}, nil
}

// authorityLocator maps the verified principal's identities, provenance and expiry into an exact request/correlation locator without issuing authority.
// RU: authorityLocator переносит идентичности, provenance и срок проверенного субъекта в точный request/correlation locator без выдачи полномочий.
func authorityLocator(principal value.Principal, requestID, correlationID string) *sttv1.DelegatedAuthorityLocator {
	return &sttv1.DelegatedAuthorityLocator{
		RequestId: requestID, CorrelationId: correlationID, RootActorId: principal.ActorID,
		TenantId: principal.TenantID, ProjectId: principal.ProjectID,
		SourceRevision: principal.AuthorityRevision, SourceDigestSha256: principal.AuthorityDigestSHA256,
		Actor: provenance(principal.Actor), Tenant: provenance(principal.Tenant), Project: provenance(principal.Project),
		ExpiresAt: timestamppb.New(principal.ExpiresAt),
	}
}

// provenance omits an absent domain provenance value and otherwise copies all source/ref/revision/digest fields into the RPC shape.
// RU: provenance опускает отсутствующее доменное происхождение, иначе копируя все source/ref/revision/digest поля в RPC форму.
func provenance(input value.AuthorityProvenance) *sttv1.AuthorityIdentityProvenance {
	if input == (value.AuthorityProvenance{}) {
		return nil
	}
	return &sttv1.AuthorityIdentityProvenance{
		Source: input.Source, Reference: input.Reference,
		Revision: input.Revision, DigestSha256: input.DigestSHA256,
	}
}

// sameAuthority compares every mapped identity, request/correlation pin, source revision/digest, optional provenance and expiry instant, rejecting absent locators or expiry.
// RU: sameAuthority сравнивает все перенесённые идентичности, request/correlation координаты, source revision/digest, необязательные provenance и момент срока, отклоняя отсутствие locator или срока.
// This echo comparison is not signature verification and does not independently validate timestamp grammar or freshness.
// RU: Это сравнение ответа, а не проверка подписи; оно самостоятельно не проверяет грамматику timestamp или свежесть.
func sameAuthority(actual, expected *sttv1.DelegatedAuthorityLocator) bool {
	if actual == nil || expected == nil || actual.GetExpiresAt() == nil || expected.GetExpiresAt() == nil {
		return false
	}
	return actual.GetRequestId() == expected.GetRequestId() && actual.GetCorrelationId() == expected.GetCorrelationId() &&
		actual.GetRootActorId() == expected.GetRootActorId() && actual.GetTenantId() == expected.GetTenantId() &&
		actual.GetProjectId() == expected.GetProjectId() && actual.GetSourceRevision() == expected.GetSourceRevision() &&
		actual.GetSourceDigestSha256() == expected.GetSourceDigestSha256() &&
		sameProvenance(actual.GetActor(), expected.GetActor()) && sameProvenance(actual.GetTenant(), expected.GetTenant()) &&
		sameProvenance(actual.GetProject(), expected.GetProject()) && actual.GetExpiresAt().AsTime().Equal(expected.GetExpiresAt().AsTime())
}

// sameProvenance requires matching absence or exact source, reference, revision and digest fields for the two identity provenance records.
// RU: sameProvenance требует совпадающего отсутствия либо точных source, reference, revision и digest у двух записей происхождения идентичности.
func sameProvenance(actual, expected *sttv1.AuthorityIdentityProvenance) bool {
	if actual == nil || expected == nil {
		return actual == nil && expected == nil
	}
	return actual != nil && expected != nil && actual.GetSource() == expected.GetSource() &&
		actual.GetReference() == expected.GetReference() && actual.GetRevision() == expected.GetRevision() &&
		actual.GetDigestSha256() == expected.GetDigestSha256()
}

// classifyProjectionError preserves local cancellation, classifies closed owner rejections as grant revocation and hides other RPC details behind a fixed failure.
// RU: classifyProjectionError сохраняет локальную отмену, классифицирует заданные отказы владельца как отзыв grant и скрывает остальные RPC данные фиксированным отказом.
func classifyProjectionError(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	switch status.Code(err) {
	case codes.PermissionDenied, codes.Unauthenticated, codes.FailedPrecondition, codes.NotFound:
		return errs.ErrGrantRevoked
	default:
		return errors.New("transcription projection request failed")
	}
}
