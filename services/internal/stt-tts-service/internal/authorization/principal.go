// Package authorization преобразует только проверенный internal RPC context.
package authorization

import (
	"context"
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
)

const (
	expectedAudience         = "urn:kodex:internal-rpc:stt-tts-service"
	expectedWorkloadID       = "stt-tts-service"
	expectedCaller           = "control-api-gateway"
	transcribeOperation      = "platform.stt.transcribe"
	maximumAuthorityRevision = uint64(1<<53 - 1)
)

func Principal(ctx context.Context, fullMethod string) (value.Principal, error) {
	verified, ok := authorityclient.VerifiedAuthorizationContext(ctx)
	if !ok || fullMethod != sttv1.SpeechToTextService_Transcribe_FullMethodName ||
		verified.GetContractVersion() != 1 || verified.GetAudience() != expectedAudience ||
		verified.GetTargetWorkloadId() != expectedWorkloadID || verified.GetCallerWorkloadId() != expectedCaller ||
		verified.GetFullMethod() != fullMethod || verified.GetOperationId() != transcribeOperation ||
		verified.GetPermission() != value.PermissionTranscribe || verified.GetAuthority() == nil ||
		verified.GetAuthority().GetActor() == nil || verified.GetAuthority().GetTenant() == nil ||
		verified.GetAuthority().GetProject() == nil || verified.GetExpiresAt() == nil ||
		!verified.GetExpiresAt().IsValid() || verified.GetSourceRevision() == 0 ||
		verified.GetSourceRevision() > maximumAuthorityRevision ||
		verified.GetSourceDigestSha256() == "" || verified.GetJti() == "" {
		return value.Principal{}, errors.New("verified STT authorization context is invalid")
	}
	principal := value.Principal{
		ActorID:    strings.TrimSpace(verified.GetAuthority().GetActor().GetId()),
		TenantID:   strings.TrimSpace(verified.GetAuthority().GetTenant().GetId()),
		ProjectID:  strings.TrimSpace(verified.GetAuthority().GetProject().GetId()),
		RequestID:  verified.GetJti(),
		Permission: verified.GetPermission(), AuthorityRevision: verified.GetSourceRevision(),
		AuthorityDigestSHA256: verified.GetSourceDigestSha256(), ExpiresAt: verified.GetExpiresAt().AsTime().UTC(),
	}
	if principal.ActorID == "" || principal.TenantID == "" || principal.ProjectID == "" {
		return value.Principal{}, errors.New("verified STT identity is incomplete")
	}
	return principal, nil
}
