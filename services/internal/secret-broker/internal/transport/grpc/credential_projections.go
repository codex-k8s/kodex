package grpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	secretbrokerv1 "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	// runtimeProjectionFailureMessage is the fixed projection warning text; structured diagnostics must not
	// include credential bytes or arbitrary upstream error text.
	// runtimeProjectionFailureMessage — фиксированный текст projection warning; structured diagnostics не
	// должны включать credential bytes и произвольный upstream error text.
	runtimeProjectionFailureMessage = "runtime credential projection failed"
	// credentialProjectionAudience pins the expected verified-authorization audience for the broker,
	// independently of RPC method and caller identity.
	// credentialProjectionAudience закрепляет ожидаемую verified-authorization audience broker отдельно от RPC
	// method и caller identity.
	credentialProjectionAudience = "urn:kodex:internal-rpc:secret-broker"
	// secretBrokerWorkloadID is the exact verifier target-workload claim expected for projection
	// authorization.
	// secretBrokerWorkloadID — exact verifier target-workload claim, ожидаемый для projection authorization.
	secretBrokerWorkloadID = "secret-broker"
	// secretBrokerSPIFFEID pins the projection target's verified SPIFFE identity; it is a public identity
	// string, not a key or bearer credential.
	// secretBrokerSPIFFEID закрепляет verified SPIFFE identity target проекции; это public identity string, не
	// ключ и не bearer credential.
	secretBrokerSPIFFEID = "spiffe://kodex.local/ns/kodex-system/sa/secret-broker"
	// runtimeControllerWorkloadID selects the expected runtime projection caller workload, not a
	// caller-supplied actor or tenant.
	// runtimeControllerWorkloadID задаёт ожидаемый runtime projection caller workload, не actor либо tenant по
	// выбору caller.
	runtimeControllerWorkloadID = "runtime-controller"
	// runtimeControllerSPIFFEID is the exact runtime-controller peer/subject identity checked in both
	// admission profiles.
	// runtimeControllerSPIFFEID — exact runtime-controller peer/subject identity, проверяемый в обоих
	// admission profiles.
	runtimeControllerSPIFFEID = "spiffe://kodex.local/ns/kodex-system/sa/runtime-controller"
	// sttWorkloadID selects the STT caller workload for transcription credential authority, separately from
	// runtime projection callers.
	// sttWorkloadID задаёт STT caller workload для transcription credential authority отдельно от runtime
	// projection callers.
	sttWorkloadID = "stt-tts-service"
	// sttSPIFFEID pins the expected STT service peer/subject for credential projection, not the mailbox or
	// human actor identity.
	// sttSPIFFEID закрепляет ожидаемый STT service peer/subject для credential projection, не mailbox и не
	// human actor identity.
	sttSPIFFEID = "spiffe://kodex.local/ns/kodex-system/sa/stt-tts-service"
	// runtimeProjectionOperation is the exact operation/permission required for execution runtime
	// materialization, not a wildcard projection grant.
	// runtimeProjectionOperation — exact operation/permission для execution runtime materialization, не
	// wildcard projection grant.
	runtimeProjectionOperation = "platform.runtime.credentials.materialize"
	// assistantProjectionOperation identifies the separate system-assistant projection permission whose
	// project-scope rules differ from runtime execution materialization.
	// assistantProjectionOperation обозначает отдельную system-assistant projection permission с project-scope
	// rules, отличными от runtime execution materialization.
	assistantProjectionOperation = "platform.runtime.credentials.system-assistant.materialize"
	// runtimeReadinessOperation protects projection-path readiness without authorizing credential
	// materialization itself.
	// runtimeReadinessOperation защищает projection-path readiness без разрешения самой credential
	// materialization.
	runtimeReadinessOperation = "platform.runtime.credentials.readiness.check"
	// sttCredentialOperation is the exact operation/permission required to project an owner-authorized
	// transcription credential.
	// sttCredentialOperation — exact operation/permission для проекции owner-authorized transcription
	// credential.
	sttCredentialOperation = "platform.stt.credential.project"
	// maximumAuthorityRevision bounds positive source/provenance/credential revisions to 2^53-1, preserving
	// exact integer representation across transport consumers.
	// maximumAuthorityRevision ограничивает положительные source/provenance/credential revisions до 2^53-1 для
	// точного целочисленного представления между transport consumers.
	maximumAuthorityRevision = uint64(1<<53 - 1)
	// maximumProjectedAPIKeyBytes limits the extracted API-key byte length to 16 KiB; it is not a bound on raw
	// JSON document allocation.
	// maximumProjectedAPIKeyBytes ограничивает длину извлечённого API key до 16 КиБ; это не ограничение
	// выделения памяти под raw JSON document.
	maximumProjectedAPIKeyBytes = 16 << 10
	// minimumProjectedAPIKeyBytes rejects extracted keys shorter than eight bytes without claiming
	// provider-side key validity.
	// minimumProjectedAPIKeyBytes отклоняет извлечённые keys короче восьми байтов без гарантии provider-side
	// key validity.
	minimumProjectedAPIKeyBytes = 8
)

// MaterializeRuntimeCredentials derives authority for the fixed runtime method/operation, logs authority
// failure without its text and delegates owner resolution/materialization; request identity fields do not
// create authority.
// MaterializeRuntimeCredentials получает authority для fixed runtime method/operation, логирует authority
// failure без его текста и делегирует owner resolution/materialization; request identity fields не создают
// authority.
func (server *Server) MaterializeRuntimeCredentials(ctx context.Context, request *secretbrokerv1.MaterializeRuntimeCredentialsRequest) (*secretbrokerv1.MaterializeRuntimeCredentialsResponse, error) {
	authority, err := runtimeProjectionAuthority(ctx,
		secretbrokerv1.RuntimeCredentialProjectionService_MaterializeRuntimeCredentials_FullMethodName, runtimeProjectionOperation)
	if err != nil {
		server.logRuntimeProjectionFailure(ctx, "authority", err)
		return nil, err
	}
	return server.materializeRuntimeCredentials(ctx, request, authority)
}

// MaterializeSystemAssistantCredentials first requires the separate assistant-method authority, then a
// nested execution request, and returns only the common materializer's projection descriptor.
// MaterializeSystemAssistantCredentials сначала требует отдельную assistant-method authority, затем nested
// execution request, возвращая лишь projection descriptor общего materializer.
func (server *Server) MaterializeSystemAssistantCredentials(ctx context.Context, request *secretbrokerv1.MaterializeSystemAssistantCredentialsRequest) (*secretbrokerv1.MaterializeSystemAssistantCredentialsResponse, error) {
	authority, err := runtimeProjectionAuthority(ctx,
		secretbrokerv1.RuntimeCredentialProjectionService_MaterializeSystemAssistantCredentials_FullMethodName, assistantProjectionOperation)
	if err != nil {
		return nil, err
	}
	if request.GetExecution() == nil {
		return nil, status.Error(codes.InvalidArgument, "assistant execution is required")
	}
	response, err := server.materializeRuntimeCredentials(ctx, request.GetExecution(), authority)
	if err != nil {
		return nil, err
	}
	return &secretbrokerv1.MaterializeSystemAssistantCredentialsResponse{Projection: response.GetProjection()}, nil
}

// materializeRuntimeCredentials forwards exact execution pins to the owner, validates the resulting
// manifest and invokes the exact storage materializer before returning metadata/key names only. Storage
// errors are mapped; existing owner gRPC errors are preserved, not universally redacted.
// materializeRuntimeCredentials передаёт exact execution pins владельцу, проверяет полученный manifest и
// вызывает exact storage materializer до возврата лишь metadata/key names. Storage errors отображаются;
// существующие owner gRPC errors сохраняются, не очищаются универсально.
func (server *Server) materializeRuntimeCredentials(ctx context.Context, request *secretbrokerv1.MaterializeRuntimeCredentialsRequest, authority *controlplanev1.CredentialProjectionAuthority) (*secretbrokerv1.MaterializeRuntimeCredentialsResponse, error) {
	resolved, err := server.owner.ResolveRuntimeCredentialProjection(ctx, &controlplanev1.ResolveRuntimeCredentialProjectionRequest{
		Authority: authority, WorkloadInstance: request.GetWorkloadInstance(), LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(),
		Generation: request.GetGeneration(), RuntimeRevisionRef: request.GetRuntimeRevisionRef(),
		RuntimeRevisionDigest: request.GetRuntimeRevisionDigest(), SessionRef: request.GetSessionRef(), TurnRef: request.GetTurnRef(),
		Attempt: request.GetAttempt(), InputDigest: request.GetInputDigest(),
	})
	if err != nil {
		server.logRuntimeProjectionFailure(ctx, "owner", err)
		return nil, preserveOwnerError(err)
	}
	manifest, err := runtimeProjectionManifest(authority, request, resolved)
	if err != nil {
		server.logRuntimeProjectionFailure(ctx, "manifest", err)
		return nil, err
	}
	projection, err := server.store.MaterializeRuntimeCredentialProjection(ctx, manifest)
	if err != nil {
		server.logRuntimeProjectionFailure(ctx, "storage", err)
		return nil, projectionStorageError(err)
	}
	descriptor := &secretbrokerv1.RuntimeCredentialProjectionDescriptor{
		Namespace: projection.Namespace, SecretName: projection.SecretName, SecretUid: projection.SecretUID,
		SecretResourceVersion: projection.SecretResourceVersion, ContentSha256: projection.ContentSHA256,
		ProviderAuthKey: "provider-auth.json", LeaseRef: request.GetLeaseRef(), Generation: request.GetGeneration(),
		RuntimeRevisionRef: request.GetRuntimeRevisionRef(), RuntimeRevisionDigest: request.GetRuntimeRevisionDigest(),
		SessionRef: request.GetSessionRef(), TurnRef: request.GetTurnRef(), Attempt: request.GetAttempt(),
		InputDigest: request.GetInputDigest(), ExpiresAt: timestamppb.New(manifest.ExpiresAt),
	}
	for _, item := range manifest.RuntimeSecrets {
		descriptor.RuntimeSecretKeys = append(descriptor.RuntimeSecretKeys, &secretbrokerv1.RuntimeCredentialProjectionKey{
			Name: item.Name, SecretKey: item.Name,
		})
	}
	return &secretbrokerv1.MaterializeRuntimeCredentialsResponse{Projection: descriptor}, nil
}

// logRuntimeProjectionFailure emits only the supplied stage and gRPC code when a logger exists; current
// private callsites supply fixed stages, and the error message itself is not logged.
// logRuntimeProjectionFailure при наличии logger выводит лишь переданные stage и gRPC code; текущие
// private callsites задают fixed stages, сам error message не логируется.
func (server *Server) logRuntimeProjectionFailure(ctx context.Context, stage string, err error) {
	if server.catalogLogger != nil {
		server.catalogLogger.WarnContext(ctx, runtimeProjectionFailureMessage, "stage", stage, "grpc_code", status.Code(err).String())
	}
}

// CheckRuntimeCredentialProjectionReadiness requires its exact authority, then probes owner projection
// support, local store and last-scan recovery health; any joined failure yields fixed Unavailable.
// CheckRuntimeCredentialProjectionReadiness требует exact authority, затем проверяет owner projection
// support, local store и last-scan recovery health; любой joined failure даёт fixed Unavailable.
func (server *Server) CheckRuntimeCredentialProjectionReadiness(ctx context.Context, _ *secretbrokerv1.CheckRuntimeCredentialProjectionReadinessRequest) (*secretbrokerv1.CheckRuntimeCredentialProjectionReadinessResponse, error) {
	if _, err := runtimeProjectionAuthority(ctx,
		secretbrokerv1.RuntimeCredentialProjectionService_CheckRuntimeCredentialProjectionReadiness_FullMethodName, runtimeReadinessOperation); err != nil {
		return nil, err
	}
	if err := errors.Join(server.owner.CheckCredentialProjection(ctx), server.store.Check(ctx), server.recovery.Check(ctx)); err != nil {
		return nil, status.Error(codes.Unavailable, "runtime credential projection path is not ready")
	}
	return &secretbrokerv1.CheckRuntimeCredentialProjectionReadinessResponse{Ready: true}, nil
}

// ProjectTranscriptionCredential binds transport/locator authority, obtains an exact owner
// account/generation/expiry descriptor and reads only that credential. It clears raw/key byte copies,
// returns an independent API-key copy plus a cloned locator and never starts audio transcription or
// validates the key with a provider.
// ProjectTranscriptionCredential связывает transport/locator authority, получает exact owner
// account/generation/expiry descriptor и читает лишь этот credential. Очищает raw/key byte copies,
// возвращает независимую API-key copy и locator clone без запуска audio transcription и проверки ключа у
// provider.
func (server *Server) ProjectTranscriptionCredential(ctx context.Context, request *sttv1.ProjectTranscriptionCredentialRequest) (*sttv1.ProjectTranscriptionCredentialResponse, error) {
	authority, ctx, err := transcriptionProjectionAuthority(ctx, request.GetAuthority())
	if err != nil {
		return nil, err
	}
	resolved, err := server.owner.ResolveTranscriptionCredentialProjection(ctx, &controlplanev1.ResolveTranscriptionCredentialProjectionRequest{
		Authority: authority, ProviderAccountRef: request.GetProviderAccountRef(),
		ProviderCredentialGeneration: request.GetProviderCredentialGeneration(), ConfigRevision: request.GetConfigRevision(),
		ConfigDigestSha256: request.GetConfigDigestSha256(),
	})
	if err != nil {
		return nil, preserveOwnerError(err)
	}
	binding := resolved.GetProviderCredential()
	if binding == nil || binding.GetAccountRef() != request.GetProviderAccountRef() || binding.GetCredentialRevision() < 1 ||
		uint64(binding.GetCredentialRevision()) != request.GetProviderCredentialGeneration() || resolved.GetExpiresAt() == nil ||
		resolved.GetExpiresAt().CheckValid() != nil || !resolved.GetExpiresAt().AsTime().After(time.Now()) ||
		resolved.GetExpiresAt().AsTime().After(authority.GetExpiresAt().AsTime()) {
		return nil, status.Error(codes.FailedPrecondition, "transcription credential projection binding is invalid")
	}
	raw, err := server.store.ReadProviderCredentialExact(ctx, binding.GetAccountRef(), kubernetesstore.ProviderCredentialDescriptor{
		SecretName: binding.GetSecretName(), SecretUID: binding.GetSecretUid(), SecretResourceVersion: binding.GetSecretResourceVersion(),
		ContentSHA256: binding.GetContentSha256(),
	})
	if err != nil {
		return nil, projectionStorageError(err)
	}
	defer clear(raw)
	apiKey, err := projectedAPIKey(raw)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, "transcription credential materialization is invalid")
	}
	defer clear(apiKey)
	return &sttv1.ProjectTranscriptionCredentialResponse{
		ApiKey: append([]byte(nil), apiKey...), ProviderAccountRef: binding.GetAccountRef(),
		ProviderCredentialGeneration: request.GetProviderCredentialGeneration(), ConfigRevision: request.GetConfigRevision(),
		ConfigDigestSha256: request.GetConfigDigestSha256(), ExpiresAt: resolved.GetExpiresAt(),
		Authority: proto.Clone(request.GetAuthority()).(*sttv1.DelegatedAuthorityLocator),
	}, nil
}

// verifiedProjectionAuthority checks exact audience/caller/target/method/operation, provenance
// shape/bounded revisions and live expiry on a context already verified by middleware. It maps those
// claims without performing signature/replay verification itself or trusting a request-supplied snapshot.
// verifiedProjectionAuthority проверяет exact audience/caller/target/method/operation, provenance
// shape/ограниченные revisions и live expiry в context, уже проверенном middleware. Отображает claims без
// самостоятельной signature/replay verification и без доверия request-supplied snapshot.
func verifiedProjectionAuthority(ctx context.Context, callerID, callerSPIFFEID, fullMethod, operation string) (*controlplanev1.CredentialProjectionAuthority, *internalrpcauthorityv1.VerifiedAuthorizationContext, error) {
	verified, ok := authorityclient.VerifiedAuthorizationContext(ctx)
	if !ok || verified.GetContractVersion() != 1 || verified.GetAudience() != credentialProjectionAudience ||
		verified.GetSubject() != callerSPIFFEID || verified.GetCallerWorkloadId() != callerID || verified.GetCallerSpiffeId() != callerSPIFFEID ||
		verified.GetTargetWorkloadId() != secretBrokerWorkloadID || verified.GetTargetSpiffeId() != secretBrokerSPIFFEID ||
		verified.GetFullMethod() != fullMethod || verified.GetOperationId() != operation || verified.GetPermission() != operation ||
		verified.GetAuthority() == nil || verified.GetExpiresAt() == nil || verified.GetExpiresAt().CheckValid() != nil ||
		!verified.GetExpiresAt().AsTime().After(time.Now()) || verified.GetSourceRevision() == 0 ||
		verified.GetSourceRevision() > maximumAuthorityRevision || !validProjectionSHA256(verified.GetSourceDigestSha256()) ||
		verified.GetCallerCredentialRevision() == 0 || verified.GetCallerCredentialRevision() > maximumAuthorityRevision ||
		uuid.Validate(verified.GetJti()) != nil || !validProjectionIdentity(verified.GetAuthority().GetActor()) ||
		!validProjectionIdentity(verified.GetAuthority().GetTenant()) || !validProjectionProject(fullMethod, verified.GetAuthority().GetProject()) {
		return nil, nil, status.Error(codes.PermissionDenied, "verified credential projection authority is invalid")
	}
	return &controlplanev1.CredentialProjectionAuthority{
		ActorId: verified.GetAuthority().GetActor().GetId(), TenantId: verified.GetAuthority().GetTenant().GetId(),
		ProjectId: verified.GetAuthority().GetProject().GetId(), SourceRevision: verified.GetSourceRevision(),
		SourceDigestSha256: verified.GetSourceDigestSha256(), ProofJti: verified.GetJti(), CallerWorkloadId: callerID,
		CallerFullMethod: fullMethod, CallerCredentialRevision: verified.GetCallerCredentialRevision(), ExpiresAt: verified.GetExpiresAt(),
	}, verified, nil
}

// validProjectionProject requires no project for catalog/system-assistant routes, permits absent or valid
// project for STT and requires a valid identity for other methods.
// validProjectionProject требует отсутствия project для catalog/system-assistant routes, допускает absent
// либо valid project для STT и требует valid identity для остальных methods.
func validProjectionProject(method string, project *internalrpcauthorityv1.AuthorityIdentity) bool {
	if method == controlplanev1.ProviderCredentialMaterializerService_ObserveProviderModelCatalog_FullMethodName {
		return project == nil
	}
	if method == secretbrokerv1.RuntimeCredentialProjectionService_MaterializeSystemAssistantCredentials_FullMethodName {
		return project == nil
	}
	if method == sttv1.TranscriptionCredentialProjectionService_ProjectTranscriptionCredential_FullMethodName && project == nil {
		return true
	}
	return validProjectionIdentity(project)
}

// validProjectionIdentity checks UUID syntax plus known nonzero source, nonempty trimmed reference,
// bounded positive revision and lowercase digest; it does not resolve the identity or impose a
// reference-length limit here.
// validProjectionIdentity проверяет UUID syntax, known nonzero source, nonempty trimmed reference, bounded
// positive revision и lowercase digest; identity не разрешает и reference-length limit здесь не задаёт.
func validProjectionIdentity(identity *internalrpcauthorityv1.AuthorityIdentity) bool {
	if identity == nil || identity.GetProvenance() == nil || uuid.Validate(identity.GetId()) != nil {
		return false
	}
	provenance := identity.GetProvenance()
	_, knownSource := internalrpcauthorityv1.AuthoritySource_name[int32(provenance.GetSource())]
	return knownSource && provenance.GetSource() != internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_UNSPECIFIED &&
		strings.TrimSpace(provenance.GetReference()) == provenance.GetReference() && provenance.GetReference() != "" &&
		provenance.GetRevision() > 0 && provenance.GetRevision() <= maximumAuthorityRevision && validProjectionSHA256(provenance.GetDigestSha256())
}

// sameDelegatedAuthorityLocator requires matching root identities/source/provenance and a valid
// request/correlation shape; verified continuation expiry may shorten but never exceed the parent locator.
// It compares already-verified metadata, not an independent proof signature.
// sameDelegatedAuthorityLocator требует совпадения root identities/source/provenance и valid
// request/correlation shape; verified continuation expiry может сокращать, но не превышать parent locator.
// Сверяет уже проверенные metadata, не самостоятельную proof signature.
func sameDelegatedAuthorityLocator(locator *sttv1.DelegatedAuthorityLocator, verified *internalrpcauthorityv1.VerifiedAuthorizationContext) bool {
	if locator == nil || verified == nil || locator.GetExpiresAt() == nil || locator.GetExpiresAt().CheckValid() != nil ||
		verified.GetExpiresAt() == nil || verified.GetExpiresAt().CheckValid() != nil ||
		!verified.GetExpiresAt().AsTime().After(time.Now()) || verified.GetExpiresAt().AsTime().After(locator.GetExpiresAt().AsTime()) ||
		uuid.Validate(locator.GetRequestId()) != nil || !validCorrelation(locator.GetCorrelationId()) {
		return false
	}
	// Locator относится к родителю; issuer может сократить срок continuation.
	// Проекция использует срок verified, поэтому locator не продлевает полномочия.
	authority := verified.GetAuthority()
	return locator.GetRootActorId() == authority.GetActor().GetId() && locator.GetTenantId() == authority.GetTenant().GetId() &&
		locator.GetProjectId() == authority.GetProject().GetId() && locator.GetSourceRevision() == verified.GetSourceRevision() &&
		locator.GetSourceDigestSha256() == verified.GetSourceDigestSha256() &&
		sameProjectionProvenance(locator.GetActor(), authority.GetActor().GetProvenance()) &&
		sameProjectionProvenance(locator.GetTenant(), authority.GetTenant().GetProvenance()) &&
		((authority.GetProject() == nil && locator.GetProject() == nil) || sameProjectionProvenance(locator.GetProject(), authority.GetProject().GetProvenance()))
}

// sameProjectionProvenance requires both values and exact source/reference/revision/digest equality
// without resolving or refreshing either source.
// sameProjectionProvenance требует оба значения и exact source/reference/revision/digest equality без
// разрешения и обновления sources.
func sameProjectionProvenance(locator *sttv1.AuthorityIdentityProvenance, verified *internalrpcauthorityv1.AuthorityProvenance) bool {
	return locator != nil && verified != nil && locator.GetSource() == int32(verified.GetSource()) &&
		locator.GetReference() == verified.GetReference() && locator.GetRevision() == verified.GetRevision() &&
		locator.GetDigestSha256() == verified.GetDigestSha256()
}

// validCorrelation accepts one to 128 ASCII letters/digits or dot/underscore/colon/hyphen, not arbitrary
// request metadata or authorization authority.
// validCorrelation принимает от 1 до 128 ASCII letters/digits либо dot/underscore/colon/hyphen, не
// arbitrary request metadata и не authorization authority.
func validCorrelation(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || strings.ContainsRune("._:-", character) {
			continue
		}
		return false
	}
	return true
}

// validProjectionSHA256 accepts exactly 64 lowercase hexadecimal bytes, including an all-zero value; it
// checks representation, not content authenticity.
// validProjectionSHA256 принимает ровно 64 lowercase hexadecimal bytes, включая all-zero value; проверяет
// representation, не content authenticity.
func validProjectionSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return false
		}
	}
	return true
}

// runtimeProjectionManifest binds the explicit transport profile and expiry, replacing a trusted selector
// only with a matching owner snapshot, then copies execution/provider/runtime pins. It rejects mixed
// profiles and nil runtime entries; full source eligibility and storage-shape validation stay with
// owner/store.
// runtimeProjectionManifest связывает explicit transport profile и expiry, заменяя trusted selector лишь
// совпавшим owner snapshot, затем копирует execution/provider/runtime pins. Отклоняет mixed profiles и nil
// runtime entries; полная source eligibility и storage-shape validation остаются у owner/store.
func runtimeProjectionManifest(authority *controlplanev1.CredentialProjectionAuthority, request *secretbrokerv1.MaterializeRuntimeCredentialsRequest, resolved *controlplanev1.ResolveRuntimeCredentialProjectionResponse) (kubernetesstore.CredentialProjectionManifest, error) {
	if authority != nil && authority.GetRpcProfile() == transportprofile.TrustedCluster {
		owner := resolved.GetAuthority()
		if owner == nil || owner.GetRpcProfile() != transportprofile.TrustedCluster || owner.GetProofJti() != "" ||
			owner.GetCallerWorkloadId() != authority.GetCallerWorkloadId() || owner.GetCallerFullMethod() != authority.GetCallerFullMethod() ||
			owner.GetSourceRevision() != uint64(request.GetGeneration()) || owner.GetSourceDigestSha256() != request.GetRuntimeRevisionDigest() ||
			uuid.Validate(owner.GetActorId()) != nil || uuid.Validate(owner.GetTenantId()) != nil ||
			owner.GetCallerCredentialRevision() == 0 || owner.GetCallerCredentialRevision() > maximumAuthorityRevision {
			return kubernetesstore.CredentialProjectionManifest{}, status.Error(codes.FailedPrecondition, "trusted projection owner binding is invalid")
		}
		authority = owner
	} else if authority != nil && authority.GetRpcProfile() != "" || resolved.GetAuthority() != nil {
		return kubernetesstore.CredentialProjectionManifest{}, status.Error(codes.FailedPrecondition, "projection profile binding is invalid")
	}
	if authority == nil || request == nil || resolved == nil || resolved.GetProviderCredential() == nil || resolved.GetExpiresAt() == nil ||
		authority.GetExpiresAt() == nil || authority.GetExpiresAt().CheckValid() != nil ||
		resolved.GetExpiresAt().CheckValid() != nil || !resolved.GetExpiresAt().AsTime().After(time.Now()) ||
		resolved.GetExpiresAt().AsTime().After(authority.GetExpiresAt().AsTime()) {
		return kubernetesstore.CredentialProjectionManifest{}, status.Error(codes.FailedPrecondition, "runtime credential projection binding is invalid")
	}
	provider := resolved.GetProviderCredential()
	manifest := kubernetesstore.CredentialProjectionManifest{
		Authority: kubernetesstore.ProjectionAuthority{
			RPCProfile: authority.GetRpcProfile(),
			ActorID:    authority.GetActorId(), TenantID: authority.GetTenantId(), ProjectID: authority.GetProjectId(),
			SourceRevision: authority.GetSourceRevision(), SourceDigestSHA256: authority.GetSourceDigestSha256(),
			ProofJTI: authority.GetProofJti(), CallerWorkloadID: authority.GetCallerWorkloadId(),
			CallerFullMethod: authority.GetCallerFullMethod(), CallerCredentialRevision: authority.GetCallerCredentialRevision(),
			ExpiresAt: authority.GetExpiresAt().AsTime().UTC(),
		},
		WorkloadInstance: request.GetWorkloadInstance(), LeaseRef: request.GetLeaseRef(), Generation: request.GetGeneration(),
		RuntimeRevisionRef: request.GetRuntimeRevisionRef(), RuntimeRevisionDigest: request.GetRuntimeRevisionDigest(),
		SessionRef: request.GetSessionRef(), TurnRef: request.GetTurnRef(), Attempt: request.GetAttempt(), InputDigest: request.GetInputDigest(),
		ProviderCredential: kubernetesstore.ProviderProjectionBinding{
			AccountRef: provider.GetAccountRef(), CredentialRevisionRef: provider.GetCredentialRevisionRef(),
			CredentialRevision: provider.GetCredentialRevision(), SecretName: provider.GetSecretName(), SecretUID: provider.GetSecretUid(),
			SecretResourceVersion: provider.GetSecretResourceVersion(), ContentSHA256: provider.GetContentSha256(),
		},
		ExpiresAt: resolved.GetExpiresAt().AsTime().UTC(),
	}
	for _, item := range resolved.GetRuntimeSecrets() {
		if item == nil {
			return kubernetesstore.CredentialProjectionManifest{}, status.Error(codes.FailedPrecondition, "runtime credential projection binding is invalid")
		}
		manifest.RuntimeSecrets = append(manifest.RuntimeSecrets, kubernetesstore.RuntimeSecretProjectionBinding{
			Name: item.GetName(), SecretRef: item.GetSecretRef(), Revision: item.GetRevision(), Namespace: item.GetNamespace(),
			SecretName: item.GetSecretName(), SecretKey: item.GetSecretKey(), SecretUID: item.GetSecretUid(),
			SecretResourceVersion: item.GetSecretResourceVersion(), ContentSHA256: item.GetContentSha256(),
		})
	}
	return manifest, nil
}

// runtimeProjectionAuthority accepts only the exact server-established trusted admission selector or a
// verified authorization context for the fixed runtime caller/method/permission; trusted owner fields are
// resolved later, not copied from request metadata.
// runtimeProjectionAuthority принимает лишь exact server-established trusted admission selector либо
// verified authorization context fixed runtime caller/method/permission; trusted owner fields разрешаются
// позже, не копируются из request metadata.
func runtimeProjectionAuthority(ctx context.Context, method, operation string) (*controlplanev1.CredentialProjectionAuthority, error) {
	if admission, ok := serviceidentity.FromContext(ctx); ok && admission.RPCProfile == transportprofile.TrustedCluster {
		if admission.Peer.SPIFFEID != runtimeControllerSPIFFEID || admission.TargetSPIFFEID != secretBrokerSPIFFEID ||
			admission.FullMethod != method || admission.OperationID != operation || admission.Permission != operation {
			return nil, status.Error(codes.PermissionDenied, "trusted runtime projection admission rejected")
		}
		return &controlplanev1.CredentialProjectionAuthority{RpcProfile: transportprofile.TrustedCluster,
			CallerWorkloadId: runtimeControllerWorkloadID, CallerFullMethod: method}, nil
	}
	authority, _, err := verifiedProjectionAuthority(ctx, runtimeControllerWorkloadID, runtimeControllerSPIFFEID, method, operation)
	return authority, err
}

// projectedAPIKey decodes one apikey-mode JSON object with unknown fields/trailing data rejected and
// validates extracted byte bounds/edge whitespace/no CR-LF-NUL. It does not reject duplicate keys or
// verify provider credentials; clearing returned bytes does not erase decoded immutable strings.
// projectedAPIKey декодирует один apikey-mode JSON object, отклоняя unknown fields/trailing data, и
// проверяет extracted byte bounds/edge whitespace/отсутствие CR-LF-NUL. Duplicate keys и provider
// credentials не проверяет; очистка возвращённых bytes не стирает decoded immutable strings.
func projectedAPIKey(raw []byte) ([]byte, error) {
	var credential struct {
		APIKey   string `json:"OPENAI_API_KEY"`
		AuthMode string `json:"auth_mode"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&credential); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("provider credential JSON contains trailing data")
	}
	key := []byte(credential.APIKey)
	if credential.AuthMode != "apikey" || len(key) < minimumProjectedAPIKeyBytes || len(key) > maximumProjectedAPIKeyBytes ||
		strings.TrimSpace(credential.APIKey) != credential.APIKey || strings.ContainsAny(credential.APIKey, "\r\n\x00") {
		clear(key)
		return nil, errors.New("provider API key is invalid")
	}
	return key, nil
}

// projectionStorageError maps input/conflict/stale-source failures to fixed gRPC errors and every other
// failure to Unavailable without raw diagnostics; it is a failure-path mapper, not a nil-success
// pass-through.
// projectionStorageError отображает input/conflict/stale-source failures в fixed gRPC errors, остальные
// failures — в Unavailable без raw diagnostics; это failure-path mapper, не nil-success pass-through.
func projectionStorageError(err error) error {
	switch {
	case errors.Is(err, kubernetesstore.ErrCredentialProjectionInvalid), errors.Is(err, kubernetesstore.ErrProviderCredentialInputInvalid):
		return status.Error(codes.InvalidArgument, "credential projection input is invalid")
	case errors.Is(err, kubernetesstore.ErrCredentialProjectionConflict), errors.Is(err, kubernetesstore.ErrProviderCredentialConflict),
		errors.Is(err, kubernetesstore.ErrMaterializationConflict), errors.Is(err, kubernetesstore.ErrMaterializationNotFound),
		errors.Is(err, kubernetesstore.ErrExactDeletePreconditionsRequired):
		return status.Error(codes.FailedPrecondition, "credential projection source binding is no longer current")
	default:
		return status.Error(codes.Unavailable, "credential projection storage is unavailable")
	}
}
