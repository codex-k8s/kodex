// Package authorization maps accepted protected RPC or owner-resolved trusted context into method-bound STT domain principals.
// RU: Пакет authorization преобразует принятый защищённый RPC либо разрешённый владельцем trusted context в субъекта домена STT с привязкой к методу.
package authorization

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"github.com/google/uuid"
)

const (
	// expectedAudience pins verified tokens to this service rather than another internal RPC audience.
	// RU: expectedAudience закрепляет проверенные токены за этим сервисом, а не иной аудиторией внутреннего RPC.
	expectedAudience = "urn:kodex:internal-rpc:stt-tts-service"
	// expectedWorkloadID identifies the only target workload accepted by protected principal mapping.
	// RU: expectedWorkloadID задаёт единственный целевой workload, принимаемый защищённым преобразованием субъекта.
	expectedWorkloadID = "stt-tts-service"
	// expectedCaller limits the protected STT entrypoint to the registered control API gateway workload.
	// RU: expectedCaller ограничивает защищённый вход STT зарегистрированным workload control API gateway.
	expectedCaller = "control-api-gateway"
	// transcribeOperation names the exact streamed user operation and does not match catalog or wildcard permissions.
	// RU: transcribeOperation задаёт точную потоковую пользовательскую операцию, не совпадая с правом каталога или wildcard.
	transcribeOperation = "platform.stt.transcribe"
	// modelCatalogOperation names the separate organization-level configuration read operation.
	// RU: modelCatalogOperation задаёт отдельную операцию чтения конфигурации уровня организации.
	modelCatalogOperation = "platform.stt.model-catalog.get"
	// emptyCatalogRequestSHA256 binds protected catalog authority to the canonical empty protobuf request bytes.
	// RU: emptyCatalogRequestSHA256 связывает защищённые полномочия каталога с каноническими пустыми байтами protobuf запроса.
	emptyCatalogRequestSHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	// maximumAuthorityRevision bounds source and provenance generations to the exact integer range used by the shared authority contract.
	// RU: maximumAuthorityRevision ограничивает поколения source и provenance точным целым диапазоном общего authority контракта.
	maximumAuthorityRevision = uint64(1<<53 - 1)
)

// Principal accepts only a private trusted principal for the same live method or the verifier-installed protected context with exact workload/operation/permission pins.
// RU: Principal принимает только приватного trusted-субъекта того же живого метода либо установленный verifier защищённый context с точными workload/operation/permission координатами.
// Protected catalog access has no project or continuation and requires an empty-request digest; cryptographic verification belongs to the earlier interceptor.
// RU: Защищённый доступ к каталогу не имеет проекта или continuation и требует дайджест пустого запроса; криптографическая проверка принадлежит предыдущему перехватчику.
// Protected expiry shape is checked here and current request eligibility is rechecked downstream; callers must supply a nonnil context.
// RU: Здесь проверяется форма защищённого срока, а текущий допуск запроса повторно проверяется ниже; вызывающая сторона обязана передать ненулевой контекст.
func Principal(ctx context.Context, fullMethod string) (value.Principal, error) {
	if resolved, ok := ctx.Value(trustedPrincipalKey{}).(trustedPrincipal); ok {
		if resolved.method != fullMethod || ctx.Err() != nil || !resolved.principal.ExpiresAt.After(time.Now()) {
			return value.Principal{}, errors.New("trusted STT principal is invalid")
		}
		return resolved.principal, nil
	}
	operation, permission, binding, domainPermission := transcribeOperation, value.TransportPermissionTranscribe, internalrpcauth.RequestBindingStream, value.PermissionTranscribe
	switch fullMethod {
	case sttv1.SpeechToTextService_Transcribe_FullMethodName:
	case sttv1.SpeechToTextService_GetModelCatalog_FullMethodName:
		operation, permission, binding, domainPermission = modelCatalogOperation, value.PermissionManageConfiguration, internalrpcauth.RequestBindingUnary, value.PermissionManageConfiguration
	default:
		return value.Principal{}, errors.New("verified STT method is invalid")
	}
	verified, ok := authorityclient.VerifiedAuthorizationContext(ctx)
	if !ok ||
		verified.GetContractVersion() != 1 || verified.GetAudience() != expectedAudience ||
		verified.GetAuthorityAbiVersion() != internalrpcauth.AuthorityABIVersion ||
		verified.GetRequestBindingMode() != binding ||
		verified.GetTargetWorkloadId() != expectedWorkloadID || verified.GetCallerWorkloadId() != expectedCaller ||
		verified.GetFullMethod() != fullMethod || verified.GetOperationId() != operation ||
		verified.GetPermission() != permission || verified.GetAuthority() == nil ||
		verified.GetExpiresAt() == nil || !verified.GetExpiresAt().IsValid() ||
		verified.GetSourceRevision() == 0 || verified.GetSourceRevision() > maximumAuthorityRevision ||
		!validSHA256(verified.GetSourceDigestSha256()) || uuid.Validate(verified.GetJti()) != nil {
		return value.Principal{}, errors.New("verified STT authorization context is invalid")
	}
	authority := verified.GetAuthority()
	if fullMethod == sttv1.SpeechToTextService_GetModelCatalog_FullMethodName &&
		(authority.GetProject() != nil || verified.GetContinuation() != nil ||
			verified.GetRequestDigestSha256() != emptyCatalogRequestSHA256) {
		return value.Principal{}, errors.New("verified STT catalog authority is invalid")
	}
	actor, err := identity(authority.GetActor())
	if err != nil {
		return value.Principal{}, errors.New("verified STT actor is invalid")
	}
	tenant, err := identity(authority.GetTenant())
	if err != nil {
		return value.Principal{}, errors.New("verified STT tenant is invalid")
	}
	var project verifiedIdentity
	if authority.GetProject() != nil {
		project, err = identity(authority.GetProject())
		if err != nil {
			return value.Principal{}, errors.New("verified STT project is invalid")
		}
	}
	return value.Principal{
		ActorID: actor.id, TenantID: tenant.id, ProjectID: project.id,
		Actor: actor.provenance, Tenant: tenant.provenance, Project: project.provenance,
		RequestID: verified.GetJti(), Permission: domainPermission,
		AuthorityRevision: verified.GetSourceRevision(), AuthorityDigestSHA256: verified.GetSourceDigestSha256(),
		ExpiresAt: verified.GetExpiresAt().AsTime().UTC(),
	}, nil
}

// verifiedIdentity pairs an owner identity with its complete source provenance instead of treating the bare identifier as authority.
// RU: verifiedIdentity связывает идентичность владельца с полным происхождением источника, не считая один идентификатор полномочием.
type verifiedIdentity struct {
	id         string
	provenance value.AuthorityProvenance
}

// identity trims identity/reference text and validates known provenance source, nonzero bounded revision and lowercase SHA-256 before domain mapping.
// RU: identity обрезает пробелы идентичности/ссылки и проверяет известный source происхождения, ненулевую ограниченную ревизию и строчный SHA-256 до доменного преобразования.
func identity(input *internalrpcauthorityv1.AuthorityIdentity) (verifiedIdentity, error) {
	if input == nil || input.GetProvenance() == nil {
		return verifiedIdentity{}, errors.New("authority identity is incomplete")
	}
	provenance := input.GetProvenance()
	id := strings.TrimSpace(input.GetId())
	reference := strings.TrimSpace(provenance.GetReference())
	_, knownSource := internalrpcauthorityv1.AuthoritySource_name[int32(provenance.GetSource())]
	if id == "" || reference == "" || !knownSource || provenance.GetSource() == internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_UNSPECIFIED ||
		provenance.GetRevision() == 0 || provenance.GetRevision() > maximumAuthorityRevision || !validSHA256(provenance.GetDigestSha256()) {
		return verifiedIdentity{}, errors.New("authority identity provenance is invalid")
	}
	return verifiedIdentity{id: id, provenance: value.AuthorityProvenance{
		Source: int32(provenance.GetSource()), Reference: reference, Revision: provenance.GetRevision(), DigestSHA256: provenance.GetDigestSha256(),
	}}, nil
}

// validSHA256 accepts exactly 64 lowercase hexadecimal characters without hashing input or proving snapshot provenance itself.
// RU: validSHA256 принимает ровно 64 строчных шестнадцатеричных символа без хеширования ввода или самостоятельного доказательства происхождения снимка.
func validSHA256(input string) bool {
	if len(input) != 64 {
		return false
	}
	for _, character := range input {
		if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return false
		}
	}
	return true
}
