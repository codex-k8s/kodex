package grpc

import (
	"context"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	authorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
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
	if !ok || p.CallerWorkload != "stt-tts-service" || p.Permission != "platform.stt.policy.resolve" ||
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
		ExpiresAt: verified.GetExpiresAt(), Authority: proto.Clone(request.GetAuthority()).(*sttv1.DelegatedAuthorityLocator),
	}, nil
}

func transcriptionLocatorMatches(locator *sttv1.DelegatedAuthorityLocator, verified *authorityv1.VerifiedAuthorizationContext) bool {
	if locator == nil || verified == nil || uuid.Validate(locator.GetRequestId()) != nil || locator.GetCorrelationId() == "" || len(locator.GetCorrelationId()) > 128 ||
		locator.GetExpiresAt() == nil || locator.GetExpiresAt().CheckValid() != nil || verified.GetExpiresAt() == nil ||
		!locator.GetExpiresAt().AsTime().After(time.Now()) || !proto.Equal(locator.GetExpiresAt(), verified.GetExpiresAt()) {
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
