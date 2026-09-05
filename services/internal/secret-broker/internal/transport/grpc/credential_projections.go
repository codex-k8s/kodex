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
	credentialProjectionAudience = "urn:kodex:internal-rpc:secret-broker"
	secretBrokerWorkloadID       = "secret-broker"
	secretBrokerSPIFFEID         = "spiffe://kodex.local/ns/kodex-system/sa/secret-broker"
	runtimeControllerWorkloadID  = "runtime-controller"
	runtimeControllerSPIFFEID    = "spiffe://kodex.local/ns/kodex-system/sa/runtime-controller"
	sttWorkloadID                = "stt-tts-service"
	sttSPIFFEID                  = "spiffe://kodex.local/ns/kodex-system/sa/stt-tts-service"
	runtimeProjectionOperation   = "platform.runtime.credentials.materialize"
	assistantProjectionOperation = "platform.runtime.credentials.system-assistant.materialize"
	runtimeReadinessOperation    = "platform.runtime.credentials.readiness.check"
	sttCredentialOperation       = "platform.stt.credential.project"
	maximumAuthorityRevision     = uint64(1<<53 - 1)
	maximumProjectedAPIKeyBytes  = 16 << 10
	minimumProjectedAPIKeyBytes  = 8
)

func (server *Server) MaterializeRuntimeCredentials(ctx context.Context, request *secretbrokerv1.MaterializeRuntimeCredentialsRequest) (*secretbrokerv1.MaterializeRuntimeCredentialsResponse, error) {
	authority, _, err := verifiedProjectionAuthority(ctx, runtimeControllerWorkloadID, runtimeControllerSPIFFEID,
		secretbrokerv1.RuntimeCredentialProjectionService_MaterializeRuntimeCredentials_FullMethodName, runtimeProjectionOperation)
	if err != nil {
		return nil, err
	}
	return server.materializeRuntimeCredentials(ctx, request, authority)
}

func (server *Server) MaterializeSystemAssistantCredentials(ctx context.Context, request *secretbrokerv1.MaterializeSystemAssistantCredentialsRequest) (*secretbrokerv1.MaterializeSystemAssistantCredentialsResponse, error) {
	authority, _, err := verifiedProjectionAuthority(ctx, runtimeControllerWorkloadID, runtimeControllerSPIFFEID,
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

func (server *Server) materializeRuntimeCredentials(ctx context.Context, request *secretbrokerv1.MaterializeRuntimeCredentialsRequest, authority *controlplanev1.CredentialProjectionAuthority) (*secretbrokerv1.MaterializeRuntimeCredentialsResponse, error) {
	resolved, err := server.owner.ResolveRuntimeCredentialProjection(ctx, &controlplanev1.ResolveRuntimeCredentialProjectionRequest{
		Authority: authority, WorkloadInstance: request.GetWorkloadInstance(), LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(),
		Generation: request.GetGeneration(), RuntimeRevisionRef: request.GetRuntimeRevisionRef(),
		RuntimeRevisionDigest: request.GetRuntimeRevisionDigest(), SessionRef: request.GetSessionRef(), TurnRef: request.GetTurnRef(),
		Attempt: request.GetAttempt(), InputDigest: request.GetInputDigest(),
	})
	if err != nil {
		return nil, preserveOwnerError(err)
	}
	manifest, err := runtimeProjectionManifest(authority, request, resolved)
	if err != nil {
		return nil, err
	}
	projection, err := server.store.MaterializeRuntimeCredentialProjection(ctx, manifest)
	if err != nil {
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

func (server *Server) CheckRuntimeCredentialProjectionReadiness(ctx context.Context, _ *secretbrokerv1.CheckRuntimeCredentialProjectionReadinessRequest) (*secretbrokerv1.CheckRuntimeCredentialProjectionReadinessResponse, error) {
	if _, _, err := verifiedProjectionAuthority(ctx, runtimeControllerWorkloadID, runtimeControllerSPIFFEID,
		secretbrokerv1.RuntimeCredentialProjectionService_CheckRuntimeCredentialProjectionReadiness_FullMethodName, runtimeReadinessOperation); err != nil {
		return nil, err
	}
	if err := errors.Join(server.owner.CheckCredentialProjection(ctx), server.store.Check(ctx), server.recovery.Check(ctx)); err != nil {
		return nil, status.Error(codes.Unavailable, "runtime credential projection path is not ready")
	}
	return &secretbrokerv1.CheckRuntimeCredentialProjectionReadinessResponse{Ready: true}, nil
}

func (server *Server) ProjectTranscriptionCredential(ctx context.Context, request *sttv1.ProjectTranscriptionCredentialRequest) (*sttv1.ProjectTranscriptionCredentialResponse, error) {
	authority, verified, err := verifiedProjectionAuthority(ctx, sttWorkloadID, sttSPIFFEID,
		sttv1.TranscriptionCredentialProjectionService_ProjectTranscriptionCredential_FullMethodName, sttCredentialOperation)
	if err != nil {
		return nil, err
	}
	if !sameDelegatedAuthorityLocator(request.GetAuthority(), verified) {
		return nil, status.Error(codes.PermissionDenied, "transcription delegated authority locator is invalid")
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

func validProjectionProject(method string, project *internalrpcauthorityv1.AuthorityIdentity) bool {
	if method == secretbrokerv1.RuntimeCredentialProjectionService_MaterializeSystemAssistantCredentials_FullMethodName {
		return project == nil
	}
	if method == sttv1.TranscriptionCredentialProjectionService_ProjectTranscriptionCredential_FullMethodName && project == nil {
		return true
	}
	return validProjectionIdentity(project)
}

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

func sameDelegatedAuthorityLocator(locator *sttv1.DelegatedAuthorityLocator, verified *internalrpcauthorityv1.VerifiedAuthorizationContext) bool {
	if locator == nil || verified == nil || locator.GetExpiresAt() == nil || locator.GetExpiresAt().CheckValid() != nil ||
		uuid.Validate(locator.GetRequestId()) != nil || !validCorrelation(locator.GetCorrelationId()) {
		return false
	}
	authority := verified.GetAuthority()
	return locator.GetRootActorId() == authority.GetActor().GetId() && locator.GetTenantId() == authority.GetTenant().GetId() &&
		locator.GetProjectId() == authority.GetProject().GetId() && locator.GetSourceRevision() == verified.GetSourceRevision() &&
		locator.GetSourceDigestSha256() == verified.GetSourceDigestSha256() &&
		sameProjectionProvenance(locator.GetActor(), authority.GetActor().GetProvenance()) &&
		sameProjectionProvenance(locator.GetTenant(), authority.GetTenant().GetProvenance()) &&
		((authority.GetProject() == nil && locator.GetProject() == nil) || sameProjectionProvenance(locator.GetProject(), authority.GetProject().GetProvenance())) &&
		locator.GetExpiresAt().AsTime().Equal(verified.GetExpiresAt().AsTime())
}

func sameProjectionProvenance(locator *sttv1.AuthorityIdentityProvenance, verified *internalrpcauthorityv1.AuthorityProvenance) bool {
	return locator != nil && verified != nil && locator.GetSource() == int32(verified.GetSource()) &&
		locator.GetReference() == verified.GetReference() && locator.GetRevision() == verified.GetRevision() &&
		locator.GetDigestSha256() == verified.GetDigestSha256()
}

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

func runtimeProjectionManifest(authority *controlplanev1.CredentialProjectionAuthority, request *secretbrokerv1.MaterializeRuntimeCredentialsRequest, resolved *controlplanev1.ResolveRuntimeCredentialProjectionResponse) (kubernetesstore.CredentialProjectionManifest, error) {
	if authority == nil || request == nil || resolved == nil || resolved.GetProviderCredential() == nil || resolved.GetExpiresAt() == nil ||
		resolved.GetExpiresAt().CheckValid() != nil || !resolved.GetExpiresAt().AsTime().After(time.Now()) ||
		resolved.GetExpiresAt().AsTime().After(authority.GetExpiresAt().AsTime()) {
		return kubernetesstore.CredentialProjectionManifest{}, status.Error(codes.FailedPrecondition, "runtime credential projection binding is invalid")
	}
	provider := resolved.GetProviderCredential()
	manifest := kubernetesstore.CredentialProjectionManifest{
		Authority: kubernetesstore.ProjectionAuthority{
			ActorID: authority.GetActorId(), TenantID: authority.GetTenantId(), ProjectID: authority.GetProjectId(),
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
