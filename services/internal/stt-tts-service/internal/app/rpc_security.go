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

// Отсутствие authority connection допустимо только для явного trusted профиля.
// Обёртка сохраняет единый cleanup-контракт всех ошибок запуска и shutdown.
type profileAuthority struct {
	connection *authorityclient.LocalConnection
}

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

func (connection *profileAuthority) Close() error {
	if connection == nil || connection.connection == nil {
		return nil
	}
	return connection.connection.Close()
}

func (connection *profileAuthority) Issuer() internalrpcauthorityv1.AuthorizationIssuerServiceClient {
	if connection == nil || connection.connection == nil {
		return nil
	}
	return connection.connection.Issuer()
}

func (connection *profileAuthority) Verifier() internalrpcauthorityv1.AuthorizationVerifierServiceClient {
	if connection == nil || connection.connection == nil {
		return nil
	}
	return connection.connection.Verifier()
}

type rpcSecurity struct {
	unary   grpc.UnaryServerInterceptor
	stream  grpc.StreamServerInterceptor
	options []grpc.ServerOption
}

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
