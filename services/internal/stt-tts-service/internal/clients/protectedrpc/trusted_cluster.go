package protectedrpc

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/authorization"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// trustedCaller fixes this service's outgoing trusted-cluster workload identity, independently of user-supplied identity fields.
// RU: trustedCaller фиксирует исходящую workload-идентичность сервиса в trusted-cluster независимо от пользовательских полей идентичности.
const trustedCaller = "spiffe://kodex.local/ns/kodex-system/sa/stt-tts-service"

// dialTrustedCluster requires the explicit profile, live construction context and exact policy/credential endpoints before creating separate lazy channels.
// RU: dialTrustedCluster требует явный профиль, живой context создания и точные адреса политики/реквизитов до создания отдельных ленивых каналов.
// It forbids a protected issuer in this profile and closes partial setup on failure; successful construction is not a connectivity check.
// RU: Функция запрещает protected issuer в этом профиле и закрывает частичную подготовку при отказе; успешное создание не проверяет связь.
func dialTrustedCluster(ctx context.Context, config Config) (*Client, error) {
	if ctx == nil || ctx.Err() != nil || config.RPCProfile != transportprofile.TrustedCluster ||
		config.Policy.Target != "dns:///control-plane.kodex-system.svc:8443" ||
		config.Credential.Target != "dns:///secret-broker.kodex-system.svc:8443" ||
		config.DialTimeout < 100*time.Millisecond || config.DialTimeout > 5*time.Second || config.Issuer != nil {
		return nil, errors.New("trusted STT dependency configuration rejected")
	}
	policy, err := newTrustedConnection(config.Policy.Target, false)
	if err != nil {
		return nil, err
	}
	credential, err := newTrustedConnection(config.Credential.Target, true)
	if err != nil {
		_ = policy.Close()
		return nil, err
	}
	return &Client{
		Authority:  sttv1.NewTranscriptionAuthorityServiceClient(policy),
		Policy:     sttv1.NewTranscriptionPolicyProjectionServiceClient(policy),
		Credential: sttv1.NewTranscriptionCredentialProjectionServiceClient(credential),
		policy:     policy, credential: credential, trustedCluster: true,
	}, nil
}

// newTrustedConnection creates a plaintext channel only for the separately validated trusted-cluster route with closed unary method/metadata checks.
// RU: newTrustedConnection создаёт открытый канал только для отдельно проверенного trusted-cluster маршрута с закрытыми проверками unary методов/metadata.
// This profile does not provide mTLS; owner authorization remains mandatory and cannot be inferred from connection creation.
// RU: Этот профиль не предоставляет mTLS; авторизация владельцем остаётся обязательной и не выводится из создания соединения.
func newTrustedConnection(target string, credential bool) (*grpc.ClientConn, error) {
	return grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(trustedUnary(credential)),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(1<<20), grpc.MaxCallSendMsgSize(1<<20)))
}

// trustedUnary enforces the destination-specific method set and exactly one fixed profile/caller plus a bounded bearer before invoking transport.
// RU: trustedUnary проверяет набор методов конкретной зависимости и единственные фиксированные profile/caller с ограниченным bearer до вызова транспорта.
// It rebuilds outgoing metadata from those three fields, dropping arbitrary inherited actor or project values.
// RU: Функция заново собирает исходящие metadata из этих трёх полей, отбрасывая произвольные унаследованные actor или project значения.
func trustedUnary(credential bool) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, request, response any, connection *grpc.ClientConn, next grpc.UnaryInvoker, options ...grpc.CallOption) error {
		allowed := method == sttv1.TranscriptionCredentialProjectionService_ProjectTranscriptionCredential_FullMethodName
		if !credential {
			allowed = method == sttv1.TranscriptionPolicyProjectionService_ResolveTranscriptionPolicy_FullMethodName ||
				method == sttv1.TranscriptionAuthorityService_ResolveTranscriptionAuthority_FullMethodName ||
				method == sttv1.TranscriptionAuthorityService_ResolveTranscriptionCatalogAuthority_FullMethodName
		}
		md, _ := metadata.FromOutgoingContext(ctx)
		profiles, callers, credentials := md.Get(serviceidentity.ProfileMetadataKey), md.Get(serviceidentity.CallerMetadataKey), md.Get("authorization")
		if !allowed || len(profiles) != 1 || profiles[0] != transportprofile.TrustedCluster || len(callers) != 1 || callers[0] != trustedCaller ||
			len(credentials) != 1 || !validTrustedCredential(credentials[0]) {
			return status.Error(codes.PermissionDenied, "trusted STT dependency request rejected")
		}
		bound := metadata.NewOutgoingContext(ctx, metadata.Pairs(serviceidentity.ProfileMetadataKey, transportprofile.TrustedCluster,
			serviceidentity.CallerMetadataKey, trustedCaller, "authorization", credentials[0]))
		return next(bound, method, request, response, connection, options...)
	}
}

// validTrustedCredential checks only the bounded two-field Bearer header shape; token authenticity belongs to the receiving owner.
// RU: validTrustedCredential проверяет только ограниченную форму Bearer-заголовка из двух полей; подлинность токена проверяет принимающий владелец.
func validTrustedCredential(credential string) bool {
	return len(credential) <= 16<<10 && strings.HasPrefix(credential, "Bearer ") &&
		len(strings.Fields(credential)) == 2 && strings.TrimSpace(credential) == credential
}

// bindTrustedDelegated matches the complete private transcription principal, request identity and child operation before forwarding the original single incoming bearer.
// RU: bindTrustedDelegated сверяет полного приватного субъекта распознавания, идентичность запроса и дочернюю операцию до передачи исходного единственного входящего bearer.
// It produces only fixed-profile outgoing metadata, not a protected continuation proof or caller-assigned principal.
// RU: Функция создаёт только исходящие metadata фиксированного профиля, а не защищённый continuation proof или назначенного клиентом субъекта.
func bindTrustedDelegated(ctx context.Context, principal value.Principal, requestID, correlationID, method, operation string) (context.Context, error) {
	if ctx == nil || principal.RPCProfile != transportprofile.TrustedCluster || requestID == "" || requestID != principal.RequestID ||
		correlationID == "" || operation == "" || operationFor(method) != operation {
		return nil, errors.New("trusted STT delegated operation rejected")
	}
	resolved, err := authorization.Principal(ctx, sttv1.SpeechToTextService_Transcribe_FullMethodName)
	if err != nil || resolved != principal {
		return nil, errors.New("trusted STT delegated principal rejected")
	}
	md, _ := metadata.FromIncomingContext(ctx)
	credentials := md.Get("authorization")
	if len(credentials) != 1 || !validTrustedCredential(credentials[0]) {
		return nil, errors.New("trusted STT delegated credential rejected")
	}
	return metadata.NewOutgoingContext(ctx, metadata.Pairs(serviceidentity.ProfileMetadataKey, transportprofile.TrustedCluster,
		serviceidentity.CallerMetadataKey, trustedCaller, "authorization", credentials[0])), nil
}

// checkTrustedConnections waits at most five seconds for both internal channels to reach Ready and rejects closed or unavailable channels.
// RU: checkTrustedConnections ждёт не более пяти секунд перехода обоих внутренних каналов в Ready и отклоняет закрытые или недоступные каналы.
// User ACL, policy and credentials are checked separately; connectivity readiness neither proves a transcription outcome nor extends authority.
// RU: User ACL, policy и реквизиты проверяются отдельно; готовность связи не доказывает исход распознавания и не продлевает полномочия.
func (client *Client) checkTrustedConnections(ctx context.Context) error {
	if ctx == nil {
		return errors.New("trusted STT readiness context required")
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for _, connection := range []*grpc.ClientConn{client.policy, client.credential} {
		if connection == nil {
			return errors.New("trusted STT connection unavailable")
		}
		connection.Connect()
		for {
			state := connection.GetState()
			if state == connectivity.Ready {
				break
			}
			if state == connectivity.Shutdown || !connection.WaitForStateChange(bounded, state) {
				return errors.New("trusted STT connection is not ready")
			}
		}
	}
	return bounded.Err()
}
