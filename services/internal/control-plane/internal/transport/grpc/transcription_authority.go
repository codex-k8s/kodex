package grpc

import (
	"context"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/authorization"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ResolveTranscriptionAuthority requests the trusted transcription snapshot with the exact resolve
// operation and stt.transcribe permission.
// ResolveTranscriptionAuthority запрашивает trusted-снимок транскрипции с точной операцией
// разрешения и правом stt.transcribe.
func (server *Server) ResolveTranscriptionAuthority(ctx context.Context, _ *sttv1.ResolveTranscriptionAuthorityRequest) (*sttv1.ResolveTranscriptionAuthorityResponse, error) {
	authority, err := trustedTranscriptionAuthority(ctx, sttv1.TranscriptionAuthorityService_ResolveTranscriptionAuthority_FullMethodName, platformrepo.TrustedSTTAuthorityOperation, "stt.transcribe")
	if err != nil {
		return nil, err
	}
	return &sttv1.ResolveTranscriptionAuthorityResponse{Authority: authority}, nil
}

// ResolveTranscriptionCatalogAuthority requests the trusted catalog snapshot with its distinct
// resolve operation and system.configuration.manage permission.
// ResolveTranscriptionCatalogAuthority запрашивает trusted-снимок каталога с отдельной операцией
// разрешения и правом system.configuration.manage.
func (server *Server) ResolveTranscriptionCatalogAuthority(ctx context.Context, _ *sttv1.ResolveTranscriptionCatalogAuthorityRequest) (*sttv1.ResolveTranscriptionCatalogAuthorityResponse, error) {
	authority, err := trustedTranscriptionAuthority(ctx, sttv1.TranscriptionAuthorityService_ResolveTranscriptionCatalogAuthority_FullMethodName, platformrepo.TrustedSTTCatalogAuthorityOperation, "system.configuration.manage")
	if err != nil {
		return nil, err
	}
	return &sttv1.ResolveTranscriptionCatalogAuthorityResponse{Authority: authority}, nil
}

// trustedTranscriptionAuthority requires a trusted STT workload principal with UUID actor/tenant
// and no project, then issues a snapshot expiring within thirty seconds and the caller deadline.
// trustedTranscriptionAuthority требует trusted principal STT workload с UUID actor и tenant без
// проекта, затем создаёт снимок со сроком не более тридцати секунд и caller deadline.
func trustedTranscriptionAuthority(ctx context.Context, method, operation, permission string) (*sttv1.TrustedTranscriptionAuthority, error) {
	p, err := authorization.TrustedPrincipal(ctx, method)
	if err != nil || p.CallerWorkload != "stt-tts-service" || p.Permission != operation || p.ProjectRef != "" ||
		uuid.Validate(p.ActorID) != nil || uuid.Validate(p.AuthorityTenant) != nil {
		return nil, status.Error(codes.PermissionDenied, "trusted transcription principal is invalid")
	}
	now := time.Now().UTC()
	expires := now.Add(30 * time.Second)
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(expires) {
		expires = deadline
	}
	if !expires.After(now) || ctx.Err() != nil {
		return nil, status.Error(codes.DeadlineExceeded, "trusted transcription authority expired")
	}
	return &sttv1.TrustedTranscriptionAuthority{
		RpcProfile: transportprofile.TrustedCluster, RequestId: uuid.NewString(),
		ActorId: p.ActorID, TenantId: p.AuthorityTenant, CredentialRevision: p.CredentialRevision,
		Permission: permission, ExpiresAt: timestamppb.New(expires),
	}, nil
}
