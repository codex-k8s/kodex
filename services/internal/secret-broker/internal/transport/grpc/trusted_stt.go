package grpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Broker проверяет transport и форму snapshot. Полномочия принадлежат CP:
// owner повторно проверяет opaque credential и actor/tenant до чтения Secret.
func transcriptionProjectionAuthority(ctx context.Context, locator *sttv1.DelegatedAuthorityLocator) (*cp.CredentialProjectionAuthority, context.Context, error) {
	method := sttv1.TranscriptionCredentialProjectionService_ProjectTranscriptionCredential_FullMethodName
	admission, trusted := serviceidentity.FromContext(ctx)
	if !trusted || admission.RPCProfile != transportprofile.TrustedCluster {
		authority, verified, err := verifiedProjectionAuthority(ctx, sttWorkloadID, sttSPIFFEID, method, sttCredentialOperation)
		if err != nil {
			return nil, nil, err
		}
		if !sameDelegatedAuthorityLocator(locator, verified) {
			return nil, nil, status.Error(codes.PermissionDenied, "transcription delegated authority locator is invalid")
		}
		return authority, ctx, nil
	}
	if admission.Peer.SPIFFEID != sttSPIFFEID || admission.TargetSPIFFEID != secretBrokerSPIFFEID ||
		admission.FullMethod != method || admission.OperationID != sttCredentialOperation || admission.Permission != sttCredentialOperation ||
		admission.ProjectRequired || !validTrustedSTTLocator(locator) {
		return nil, nil, status.Error(codes.PermissionDenied, "trusted transcription projection rejected")
	}
	md, _ := metadata.FromIncomingContext(ctx)
	credentials := md.Get("authorization")
	if len(credentials) != 1 || !strings.HasPrefix(credentials[0], "Bearer ") || len(strings.Fields(credentials[0])) != 2 ||
		strings.ContainsAny(credentials[0], "\r\n\x00") || len(md.Get("x-kodex-project-ref")) != 0 {
		return nil, nil, status.Error(codes.Unauthenticated, "trusted transcription credential rejected")
	}
	bound, err := controlplaneclient.WithApplicationGrant(ctx, strings.TrimPrefix(credentials[0], "Bearer "))
	if err != nil {
		return nil, nil, status.Error(codes.Unauthenticated, "trusted transcription credential rejected")
	}
	return &cp.CredentialProjectionAuthority{
		RpcProfile: transportprofile.TrustedCluster, ActorId: locator.GetRootActorId(), TenantId: locator.GetTenantId(),
		SourceRevision: locator.GetSourceRevision(), SourceDigestSha256: locator.GetSourceDigestSha256(),
		CallerWorkloadId: sttWorkloadID, CallerFullMethod: method, CallerCredentialRevision: locator.GetSourceRevision(), ExpiresAt: locator.GetExpiresAt(),
	}, bound, nil
}

func validTrustedSTTLocator(locator *sttv1.DelegatedAuthorityLocator) bool {
	now := time.Now()
	if locator == nil || uuid.Validate(locator.GetRequestId()) != nil || uuid.Validate(locator.GetRootActorId()) != nil || uuid.Validate(locator.GetTenantId()) != nil ||
		!validCorrelation(locator.GetCorrelationId()) || locator.GetProjectId() != "" || locator.GetActor() != nil || locator.GetTenant() != nil || locator.GetProject() != nil ||
		locator.GetSourceRevision() == 0 || locator.GetSourceRevision() > maximumAuthorityRevision || locator.GetExpiresAt() == nil ||
		!locator.GetExpiresAt().IsValid() || !locator.GetExpiresAt().AsTime().After(now) || locator.GetExpiresAt().AsTime().After(now.Add(30*time.Second)) {
		return false
	}
	snapshot := &sttv1.TrustedTranscriptionAuthority{RpcProfile: transportprofile.TrustedCluster,
		RequestId: locator.GetRequestId(), ActorId: locator.GetRootActorId(), TenantId: locator.GetTenantId(),
		CredentialRevision: locator.GetSourceRevision(), Permission: "stt.transcribe", ExpiresAt: locator.GetExpiresAt()}
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(snapshot)
	if err != nil {
		return false
	}
	digest := sha256.Sum256(raw)
	return locator.GetSourceDigestSha256() == hex.EncodeToString(digest[:])
}
