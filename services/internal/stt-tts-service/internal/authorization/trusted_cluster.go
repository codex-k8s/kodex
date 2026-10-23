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

// trustedSTTCaller is this service's fixed internal identity when resolving owner authority; request metadata cannot select another workload.
// RU: trustedSTTCaller задаёт фиксированную внутреннюю идентичность сервиса при разрешении authority владельцем; metadata запроса не выбирает иной workload.
const trustedSTTCaller = "spiffe://kodex.local/ns/kodex-system/sa/stt-tts-service"

// trustedAuthorityDeadlineMargin reserves local response handling time inside the caller's deadline instead of extending owner RPC lifetime.
// RU: trustedAuthorityDeadlineMargin оставляет время локальной обработки ответа внутри срока клиента, не продлевая жизнь owner RPC.
const trustedAuthorityDeadlineMargin = 10 * time.Millisecond

// trustedPrincipalKey keeps resolved principal context private to this package so ordinary caller metadata cannot install it.
// RU: trustedPrincipalKey сохраняет контекст разрешённого субъекта приватным для пакета, не позволяя установить его обычными metadata клиента.
type trustedPrincipalKey struct{}

// trustedPrincipal binds one owner-derived principal to the exact RPC method that obtained it.
// RU: trustedPrincipal связывает одного выведенного владельцем субъекта с точным RPC методом его получения.
type trustedPrincipal struct {
	method    string
	principal value.Principal
}

// TrustedResolver derives identity only from control-plane after owner ACL; its client is installed by composition rather than selected by an incoming request.
// RU: TrustedResolver выводит идентичность только из control-plane после owner ACL; клиент задаётся композицией, а не выбирается входящим запросом.
type TrustedResolver struct {
	client sttv1.TranscriptionAuthorityServiceClient
}

// NewTrustedResolver requires the explicit trusted-cluster profile and an owner client without performing authority resolution during construction.
// RU: NewTrustedResolver требует явный trusted-cluster профиль и owner клиент без разрешения полномочий при создании.
func NewTrustedResolver(profile string, client sttv1.TranscriptionAuthorityServiceClient) (*TrustedResolver, error) {
	if profile != transportprofile.TrustedCluster || client == nil {
		return nil, errors.New("trusted STT authority configuration is invalid")
	}
	return &TrustedResolver{client: client}, nil
}

// Resolve checks workload admission and one incoming bearer, calls the exact owner method and validates the returned bounded-lived organization principal.
// RU: Resolve проверяет workload admission и единственный входящий bearer, вызывает точный owner метод и проверяет возвращённого субъекта организации с ограниченным сроком.
// Outgoing metadata contains only the fixed profile/caller and original credential, never caller-supplied actor or project fields.
// RU: Исходящие metadata содержат только фиксированные profile/caller и исходный реквизит, без переданных клиентом actor или project полей.
// The deterministic snapshot digest correlates receipts, not a signature or JWS proof; the original request context receives a private method-bound principal.
// RU: Детерминированный дайджест снимка связывает квитанции, а не служит подписью или JWS proof; исходный контекст запроса получает приватного субъекта с привязкой к методу.
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
	ownerContext := ctx
	if deadline, ok := ctx.Deadline(); ok {
		ownerDeadline := deadline.Add(-trustedAuthorityDeadlineMargin)
		if !ownerDeadline.After(time.Now()) {
			return nil, context.DeadlineExceeded
		}
		var cancel context.CancelFunc
		ownerContext, cancel = context.WithDeadline(ctx, ownerDeadline)
		defer cancel()
	}
	outgoing := metadata.NewOutgoingContext(ownerContext, metadata.Pairs(
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
