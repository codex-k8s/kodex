package app

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/authorization"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/clients/protectedrpc"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// profileAuthority keeps a uniform partial-startup cleanup contract; an absent local authority connection is allowed only for explicit trusted-cluster composition.
// RU: profileAuthority сохраняет единый контракт очистки частичного startup; отсутствие локального authority соединения допустимо только в явной trusted-cluster композиции.
type profileAuthority struct {
	connection *authorityclient.LocalConnection
}

// dialProfileAuthority returns an empty trusted-profile wrapper or dials the exact local authority socket for the protected profile, rejecting unknown profiles.
// RU: dialProfileAuthority возвращает пустую обёртку trusted-профиля либо открывает точный локальный authority сокет защищённого профиля, отклоняя неизвестные профили.
func dialProfileAuthority(ctx context.Context, profile string, config authorityclient.LocalConfig) (*profileAuthority, error) {
	if profile == transportprofile.TrustedCluster {
		return &profileAuthority{}, nil
	}
	if profile != "" {
		return nil, errors.New("STT authority profile is unsupported")
	}
	connection, err := authorityclient.DialLocal(ctx, config)
	if err != nil {
		return nil, err
	}
	return &profileAuthority{connection: connection}, nil
}

// Close releases an existing local authority connection and safely accepts nil or trusted-profile wrappers during cleanup.
// RU: Close освобождает существующее локальное authority соединение и безопасно принимает nil или trusted-обёртку при очистке.
func (connection *profileAuthority) Close() error {
	if connection == nil || connection.connection == nil {
		return nil
	}
	return connection.connection.Close()
}

// Issuer returns the protected profile's issuer client or nil when no local connection exists; nil does not grant authority.
// RU: Issuer возвращает issuer клиент защищённого профиля либо nil при отсутствии локального соединения; nil не выдаёт полномочий.
func (connection *profileAuthority) Issuer() internalrpcauthorityv1.AuthorizationIssuerServiceClient {
	if connection == nil || connection.connection == nil {
		return nil
	}
	return connection.connection.Issuer()
}

// Verifier returns the local verifier client when connected, leaving trusted-profile owner resolution to its separate adapter.
// RU: Verifier возвращает локальный verifier клиент при наличии соединения; разрешение владельца trusted-профиля остаётся отдельному адаптеру.
func (connection *profileAuthority) Verifier() internalrpcauthorityv1.AuthorizationVerifierServiceClient {
	if connection == nil || connection.connection == nil {
		return nil
	}
	return connection.connection.Verifier()
}

// rpcSecurity carries the composed unary/stream authorization interceptors and profile-specific server transport options.
// RU: rpcSecurity содержит собранные unary/stream перехватчики авторизации и профильные транспортные параметры сервера.
type rpcSecurity struct {
	unary   grpc.UnaryServerInterceptor
	stream  grpc.StreamServerInterceptor
	options []grpc.ServerOption
}

// newRPCSecurity selects mTLS plus local verification for protected RPC or closed gateway bindings followed by owner principal resolution for trusted-cluster.
// RU: newRPCSecurity выбирает mTLS с локальной проверкой для защищённого RPC либо закрытые привязки gateway с последующим разрешением субъекта владельцем для trusted-cluster.
// The trusted branch still requires dependencies and admits only transcribe/catalog methods with user actor mode; missing verifier or unknown profiles fail closed.
// RU: Trusted-ветвь требует зависимости и допускает лишь transcribe/catalog с user actor mode; отсутствие verifier или неизвестный профиль приводят к отказу.
func newRPCSecurity(config Config, verifier *profileAuthority, dependencies *protectedrpc.Client) (rpcSecurity, error) {
	if config.RPCProfile != transportprofile.TrustedCluster {
		if config.RPCProfile != "" || verifier.Verifier() == nil {
			return rpcSecurity{}, errors.New("STT protected verifier is unavailable")
		}
		tlsConfig, err := serverTLS(config)
		if err != nil {
			return rpcSecurity{}, err
		}
		return rpcSecurity{
			unary:   authorityclient.VerifierUnaryServerInterceptor(verifier.Verifier()),
			stream:  authorityclient.VerifierStreamServerInterceptor(verifier.Verifier()),
			options: []grpc.ServerOption{grpc.Creds(credentials.NewTLS(tlsConfig))},
		}, nil
	}
	if dependencies == nil {
		return rpcSecurity{}, errors.New("trusted STT dependencies are unavailable")
	}
	resolver, err := authorization.NewTrustedResolver(config.RPCProfile, dependencies.Authority)
	if err != nil {
		return rpcSecurity{}, err
	}
	admission, err := serviceidentity.NewTrustedCluster(config.RPCProfile, "spiffe://kodex.local/ns/kodex-system/sa/stt-tts-service", []serviceidentity.Binding{
		{CallerSPIFFEID: callerSPIFFEID, FullMethod: sttv1.SpeechToTextService_Transcribe_FullMethodName,
			OperationID: "platform.stt.transcribe", Permission: value.TransportPermissionTranscribe, ActorMode: serviceidentity.UserActor},
		{CallerSPIFFEID: callerSPIFFEID, FullMethod: sttv1.SpeechToTextService_GetModelCatalog_FullMethodName,
			OperationID: "platform.stt.model-catalog.get", Permission: value.PermissionManageConfiguration, ActorMode: serviceidentity.UserActor},
	})
	if err != nil {
		return rpcSecurity{}, err
	}
	return rpcSecurity{
		unary: func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			return admission.UnaryServerInterceptor()(ctx, request, info, func(ctx context.Context, request any) (any, error) {
				return resolver.UnaryServerInterceptor()(ctx, request, info, handler)
			})
		},
		stream: func(server any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
			return admission.StreamServerInterceptor()(server, stream, info, func(server any, admitted grpc.ServerStream) error {
				return resolver.StreamServerInterceptor()(server, admitted, info, handler)
			})
		},
	}, nil
}
