package serviceidentity

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	TrustedClusterProfile = transportprofile.TrustedCluster
	ProfileMetadataKey    = "x-kodex-rpc-profile"
	CallerMetadataKey     = "x-kodex-trusted-caller"
)

// TrustedClusterAuthorizer допускает только явно включённый доверенный кластер.
// Caller — утверждение доверенного workload, НЕ криптографическая identity.
// Сетевая изоляция обязательна; actor и владение ресурсом проверяются доменом.
// Отдельный тип не позволяет включить этот режим заголовком на mTLS-сервере.
type TrustedClusterAuthorizer struct {
	policy *Authorizer
}

// RPCProfile закрепляется конфигурацией сервера, а не входящим заголовком.
func (*TrustedClusterAuthorizer) RPCProfile() string { return TrustedClusterProfile }

func (authorizer *TrustedClusterAuthorizer) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		admission, err := authorizer.Admit(ctx, info.FullMethod)
		if err != nil {
			return nil, err
		}
		return handler(context.WithValue(ctx, admissionKey{}, admission), request)
	}
}

// StreamServerInterceptor проверяет допуск до чтения первого сообщения.
// Actor/tenant не выводятся из payload: их разрешает домен-получатель.
func (authorizer *TrustedClusterAuthorizer) StreamServerInterceptor() grpc.StreamServerInterceptor {
	return func(server any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		admission, err := authorizer.Admit(stream.Context(), info.FullMethod)
		if err != nil {
			return err
		}
		return handler(server, &trustedAdmissionStream{ServerStream: stream, admission: admission})
	}
}

type trustedAdmissionStream struct {
	grpc.ServerStream
	admission Admission
}

func (stream *trustedAdmissionStream) Context() context.Context {
	return context.WithValue(stream.ServerStream.Context(), admissionKey{}, stream.admission)
}

func TrustedClusterFromPolicy(profile string, raw []byte, target string) (*TrustedClusterAuthorizer, error) {
	if profile != TrustedClusterProfile {
		return nil, errors.New("trusted cluster profile must be explicitly configured")
	}
	policy, _, err := FromPolicy(raw, target, CertificateLifetimeBoundary{})
	if err != nil {
		return nil, err
	}
	return &TrustedClusterAuthorizer{policy: policy}, nil
}

func NewTrustedCluster(profile, target string, bindings []Binding) (*TrustedClusterAuthorizer, error) {
	if profile != TrustedClusterProfile {
		return nil, errors.New("trusted cluster profile must be explicitly configured")
	}
	policy, err := New(target, bindings, CertificateLifetimeBoundary{})
	if err != nil {
		return nil, err
	}
	return &TrustedClusterAuthorizer{policy: policy}, nil
}

func (authorizer *TrustedClusterAuthorizer) Admit(ctx context.Context, fullMethod string) (Admission, error) {
	if authorizer == nil || authorizer.policy == nil || ctx == nil {
		return Admission{}, status.Error(codes.Unauthenticated, "trusted cluster admission unavailable")
	}
	if err := ctx.Err(); err != nil {
		return Admission{}, status.FromContextError(err).Err()
	}
	md, _ := metadata.FromIncomingContext(ctx)
	profiles, callers := md.Get(ProfileMetadataKey), md.Get(CallerMetadataKey)
	if len(profiles) != 1 || profiles[0] != TrustedClusterProfile || len(callers) != 1 || len(md.Get("x-kodex-authorization")) != 0 {
		return Admission{}, status.Error(codes.Unauthenticated, "trusted cluster metadata rejected")
	}
	binding, ok := authorizer.policy.bindings[callers[0]+"\x00"+fullMethod]
	if !ok {
		return Admission{}, status.Error(codes.PermissionDenied, "trusted cluster RPC is not permitted")
	}
	// Нулевые поля сертификата намеренны: сертификат здесь не проверялся.
	return Admission{
		RPCProfile:     authorizer.RPCProfile(),
		Peer:           PeerIdentity{SPIFFEID: binding.CallerSPIFFEID},
		TargetSPIFFEID: authorizer.policy.target, FullMethod: fullMethod,
		OperationID: binding.OperationID, Permission: binding.Permission,
		ActorMode: binding.ActorMode, ProjectRequired: binding.ProjectRequired,
	}, nil
}
