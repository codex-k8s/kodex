package authorization

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"github.com/google/uuid"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

const trustedSTTCaller = "spiffe://kodex.local/ns/kodex-system/sa/stt-tts-service"

type trustedPrincipalKey struct{}
type trustedPrincipal struct {
	method    string
	principal value.Principal
}

// TrustedResolver получает identity только от CP после owner ACL.
// Соединение с CP создаётся composition root, а не выбирается запросом.
type TrustedResolver struct {
	client sttv1.TranscriptionAuthorityServiceClient
}

func NewTrustedResolver(profile string, client sttv1.TranscriptionAuthorityServiceClient) (*TrustedResolver, error) {
	if profile != transportprofile.TrustedCluster || client == nil {
		return nil, errors.New("trusted STT authority configuration is invalid")
	}
	return &TrustedResolver{client: client}, nil
}

// Resolve возвращает request context с закрытым, method-bound principal.
// Digest — идентификатор owner snapshot для receipt, не подпись или JWS proof.
func (resolver *TrustedResolver) Resolve(ctx context.Context, method string) (context.Context, error) {
	if resolver == nil || resolver.client == nil || ctx == nil {
		return nil, errors.New("trusted STT authority unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	operation, permission, domainPermission := transcribeOperation, value.TransportPermissionTranscribe, value.PermissionTranscribe
	switch method {
	case sttv1.SpeechToTextService_Transcribe_FullMethodName:
	case sttv1.SpeechToTextService_GetModelCatalog_FullMethodName:
		operation, permission, domainPermission = modelCatalogOperation, value.PermissionManageConfiguration, value.PermissionManageConfiguration
	default:
		return nil, errors.New("trusted STT method is invalid")
	}
	admission, ok := serviceidentity.FromContext(ctx)
	if !ok || admission.RPCProfile != transportprofile.TrustedCluster || admission.FullMethod != method ||
		admission.TargetSPIFFEID != trustedSTTCaller || admission.Peer.SPIFFEID != "spiffe://kodex.local/ns/kodex-system/sa/control-api-gateway" ||
		admission.OperationID != operation || admission.Permission != permission || admission.ActorMode != serviceidentity.UserActor || admission.ProjectRequired {
		return nil, errors.New("trusted STT admission is invalid")
	}
	md, _ := metadata.FromIncomingContext(ctx)
	credentials := md.Get("authorization")
	if len(credentials) != 1 || len(credentials[0]) > 16<<10 || !strings.HasPrefix(credentials[0], "Bearer ") ||
		len(strings.Fields(credentials[0])) != 2 || strings.TrimSpace(credentials[0]) != credentials[0] || len(md.Get("x-kodex-project-ref")) != 0 {
		return nil, errors.New("trusted STT credential is invalid")
	}
	outgoing := metadata.NewOutgoingContext(ctx, metadata.Pairs(
		serviceidentity.ProfileMetadataKey, transportprofile.TrustedCluster,
		serviceidentity.CallerMetadataKey, trustedSTTCaller, "authorization", credentials[0],
	))
	var snapshot *sttv1.TrustedTranscriptionAuthority
	if method == sttv1.SpeechToTextService_Transcribe_FullMethodName {
		response, err := resolver.client.ResolveTranscriptionAuthority(outgoing, &sttv1.ResolveTranscriptionAuthorityRequest{})
		if err != nil {
			return nil, err
		}
		snapshot = response.GetAuthority()
	} else {
		response, err := resolver.client.ResolveTranscriptionCatalogAuthority(outgoing, &sttv1.ResolveTranscriptionCatalogAuthorityRequest{})
		if err != nil {
			return nil, err
		}
		snapshot = response.GetAuthority()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if snapshot == nil || snapshot.GetRpcProfile() != transportprofile.TrustedCluster || snapshot.GetPermission() != domainPermission ||
		uuid.Validate(snapshot.GetRequestId()) != nil || uuid.Validate(snapshot.GetActorId()) != nil || uuid.Validate(snapshot.GetTenantId()) != nil ||
		snapshot.GetCredentialRevision() == 0 || snapshot.GetCredentialRevision() > maximumAuthorityRevision || snapshot.GetExpiresAt() == nil ||
		!snapshot.GetExpiresAt().IsValid() || !snapshot.GetExpiresAt().AsTime().After(now) || snapshot.GetExpiresAt().AsTime().After(now.Add(30*time.Second)) {
		return nil, errors.New("trusted STT owner snapshot is invalid")
	}
	if deadline, ok := ctx.Deadline(); ok && snapshot.GetExpiresAt().AsTime().After(deadline) {
		return nil, errors.New("trusted STT owner snapshot exceeds request lifetime")
	}
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(snapshot)
	if err != nil {
		return nil, errors.New("trusted STT owner snapshot encoding failed")
	}
	digest := sha256.Sum256(raw)
	principal := value.Principal{
		RPCProfile: transportprofile.TrustedCluster, ActorID: snapshot.GetActorId(), TenantID: snapshot.GetTenantId(),
		RequestID: snapshot.GetRequestId(), Permission: domainPermission, AuthorityRevision: snapshot.GetCredentialRevision(),
		AuthorityDigestSHA256: hex.EncodeToString(digest[:]), ExpiresAt: snapshot.GetExpiresAt().AsTime().UTC(),
	}
	return context.WithValue(ctx, trustedPrincipalKey{}, trustedPrincipal{method: method, principal: principal}), nil
}
