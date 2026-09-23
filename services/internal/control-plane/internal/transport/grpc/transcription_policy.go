package grpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	authorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/authorization"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func (server *Server) ResolveTranscriptionPolicy(ctx context.Context, request *sttv1.ResolveTranscriptionPolicyRequest) (*sttv1.ResolveTranscriptionPolicyResponse, error) {
	p, err := principal(ctx, sttv1.TranscriptionPolicyProjectionService_ResolveTranscriptionPolicy_FullMethodName)
	if err != nil {
		return nil, err
	}
	verified, ok := authorityclient.VerifiedAuthorizationContext(ctx)
	expires := verified.GetExpiresAt()
	if trusted, err := authorization.TrustedPrincipal(ctx, sttv1.TranscriptionPolicyProjectionService_ResolveTranscriptionPolicy_FullMethodName); err == nil {
		if !trustedTranscriptionLocatorMatches(request.GetAuthority(), trusted) {
			return nil, status.Error(codes.PermissionDenied, "trusted transcription authority binding is invalid")
		}
		expires = request.GetAuthority().GetExpiresAt()
	} else if !ok || p.CallerWorkload != "stt-tts-service" || p.Permission != "platform.stt.policy.resolve" ||
		verified.GetOperationId() != "platform.stt.policy.resolve" ||
		verified.GetCallerSpiffeId() != "spiffe://kodex.local/ns/kodex-system/sa/stt-tts-service" ||
		!transcriptionLocatorMatches(request.GetAuthority(), verified) {
		return nil, status.Error(codes.PermissionDenied, "transcription authority binding is invalid")
	}
	configuration, err := server.service.GetSystemSTTConfiguration(ctx, p)
	if err != nil {
		return nil, transportError(err)
	}
	if !configuration.Ready {
		return nil, status.Error(codes.FailedPrecondition, "transcription configuration is not eligible")
	}
	return &sttv1.ResolveTranscriptionPolicyResponse{
		ConfigRevision: uint64(configuration.Revision), ConfigDigestSha256: configuration.Digest,
		Model: configuration.Model, Language: configuration.Language, MaximumAudioBytes: configuration.MaximumAudioBytes,
		MaximumAudioDurationMilliseconds: configuration.MaximumAudioDurationMilliseconds, ProviderTimeoutMilliseconds: configuration.ProviderTimeoutMilliseconds,
		Parameters: &sttv1.TranscriptionParameters{Languages: append([]string(nil), configuration.Parameters.Languages...),
			Keywords: append([]string(nil), configuration.Parameters.Keywords...), Prompt: configuration.Parameters.Prompt,
			Temperature: configuration.Parameters.Temperature, ChunkingStrategy: configuration.Parameters.ChunkingStrategy, Stream: configuration.Parameters.Stream},
		ProviderAccountRef: configuration.ProviderAccountRef, ProviderCredentialGeneration: uint64(configuration.ProviderCredentialGeneration),
		ExpiresAt: expires, Authority: proto.Clone(request.GetAuthority()).(*sttv1.DelegatedAuthorityLocator),
	}, nil
}

// Locator связывается с повторно разрешённой CP сессией; его payload не
// назначает actor/tenant. Digest проверяет целостность snapshot, не подпись.
func trustedTranscriptionLocatorMatches(locator *sttv1.DelegatedAuthorityLocator, p value.Principal) bool {
	now := time.Now()
	if locator == nil || p.CallerWorkload != "stt-tts-service" || p.Permission != "platform.stt.policy.resolve" || p.ProjectRef != "" ||
		locator.GetRootActorId() != p.ActorID || locator.GetTenantId() != p.AuthorityTenant || locator.GetProjectId() != "" ||
		locator.GetActor() != nil || locator.GetTenant() != nil || locator.GetProject() != nil ||
		uuid.Validate(locator.GetRequestId()) != nil || uuid.Validate(p.ActorID) != nil || uuid.Validate(p.AuthorityTenant) != nil ||
		locator.GetCorrelationId() == "" || len(locator.GetCorrelationId()) > 128 ||
		p.CredentialRevision == 0 || locator.GetSourceRevision() != p.CredentialRevision ||
		locator.GetExpiresAt() == nil || !locator.GetExpiresAt().IsValid() ||
		!locator.GetExpiresAt().AsTime().After(now) || locator.GetExpiresAt().AsTime().After(now.Add(30*time.Second)) {
		return false
	}
	snapshot := &sttv1.TrustedTranscriptionAuthority{
		RpcProfile: transportprofile.TrustedCluster, RequestId: locator.GetRequestId(), ActorId: p.ActorID, TenantId: p.AuthorityTenant,
		CredentialRevision: p.CredentialRevision, Permission: "stt.transcribe", ExpiresAt: locator.GetExpiresAt(),
	}
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(snapshot)
	if err != nil {
		return false
	}
	digest := sha256.Sum256(raw)
	return locator.GetSourceDigestSha256() == hex.EncodeToString(digest[:])
}

func transcriptionLocatorMatches(locator *sttv1.DelegatedAuthorityLocator, verified *authorityv1.VerifiedAuthorizationContext) bool {
	if locator == nil || verified == nil || uuid.Validate(locator.GetRequestId()) != nil || locator.GetCorrelationId() == "" || len(locator.GetCorrelationId()) > 128 ||
		locator.GetExpiresAt() == nil || locator.GetExpiresAt().CheckValid() != nil || verified.GetExpiresAt() == nil ||
		verified.GetExpiresAt().CheckValid() != nil || !verified.GetExpiresAt().AsTime().After(time.Now()) ||
		verified.GetExpiresAt().AsTime().After(locator.GetExpiresAt().AsTime()) {
		return false
	}
	authority := verified.GetAuthority()
	if authority.GetActor() == nil || authority.GetTenant() == nil || locator.GetRootActorId() != authority.GetActor().GetId() ||
		locator.GetTenantId() != authority.GetTenant().GetId() || locator.GetProjectId() != authority.GetProject().GetId() ||
		locator.GetSourceRevision() != verified.GetSourceRevision() || locator.GetSourceDigestSha256() != verified.GetSourceDigestSha256() {
		return false
	}
	return transcriptionProvenanceMatches(locator.GetActor(), authority.GetActor().GetProvenance()) &&
		transcriptionProvenanceMatches(locator.GetTenant(), authority.GetTenant().GetProvenance()) &&
		((authority.GetProject() == nil && locator.GetProject() == nil) || transcriptionProvenanceMatches(locator.GetProject(), authority.GetProject().GetProvenance()))
}

func transcriptionProvenanceMatches(locator *sttv1.AuthorityIdentityProvenance, verified *authorityv1.AuthorityProvenance) bool {
	return locator != nil && verified != nil && locator.GetSource() == int32(verified.GetSource()) &&
		locator.GetReference() == verified.GetReference() && locator.GetRevision() == verified.GetRevision() && locator.GetDigestSha256() == verified.GetDigestSha256()
}
